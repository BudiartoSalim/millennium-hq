package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"millennium-code-qc/internal/application/usecase"
	"millennium-code-qc/internal/domain"
)

// maxHookInput bounds the event read from stdin; real events are small.
const maxHookInput = 4 << 20

// hookInput is the part of a hook event the engine uses. The format is
// Claude Code's; millennium's pi extension translates pi's events into it.
// Unknown fields are ignored: the event format grows over time.
type hookInput struct {
	SessionID string         `json:"session_id"`
	Cwd       string         `json:"cwd"`
	Event     string         `json:"hook_event_name"`
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
	AgentID   string         `json:"agent_id"`
	AgentType string         `json:"agent_type"`
}

// hook handles one hook event. It answers with JSON on stdout
// and exit status 0; anything it cannot decide exits 2, which blocks the
// tool call or keeps the agent working, so a failure never lets work pass.
func (a *App) hook(ctx context.Context) int {
	in, err := readHookInput(a.stdin)
	if err != nil {
		fmt.Fprintf(a.stderr, "millennium-code-qc: %v\n", err)
		return exitBlocked
	}
	err = a.withHookDeps(ctx, in.Cwd, func(deps Deps) error {
		switch in.Event {
		case "PreToolUse":
			return a.preToolUse(ctx, deps, in)
		case "Stop", "SubagentStop":
			return a.stopHook(ctx, deps, in)
		case "SessionStart", "SubagentStart":
			return a.brief(ctx, deps, in.Event)
		}
		return nil
	})
	if errors.Is(err, ErrNoRepository) {
		return exitPass
	}
	if err != nil {
		fmt.Fprintf(a.stderr, "millennium-code-qc: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

// ErrNoDirectory is what a DepsFactory returns when dir does not exist.
var ErrNoDirectory = errors.New("the directory does not exist")

// maxVanishedLevels bounds the walk up from a hook event's cwd that no
// longer exists. A removed worktree is one level; a session deep inside one
// a few more.
const maxVanishedLevels = 32

// withHookDeps is withDepsIn for the event's cwd.
func (a *App) withHookDeps(ctx context.Context, cwd string, use func(Deps) error) error {
	deps, err := a.hookDeps(ctx, cwd)
	if deps.Close != nil {
		defer deps.Close()
	}
	if err != nil {
		return err
	}
	return use(deps)
}

// hookDeps builds the dependencies for the event's cwd. A cwd that no longer
// exists, such as a worktree removed while the session was in it, is
// evaluated in its nearest existing parent; otherwise every tool call, cd
// included, would block. Each step is the lexical parent of an absolute,
// clean path, so the walk reaches only directories the event names, and it
// moves up only past a directory that does not exist: the first one that
// does exist decides, and any other failure blocks.
func (a *App) hookDeps(ctx context.Context, cwd string) (Deps, error) {
	walkable := filepath.IsAbs(cwd) && filepath.Clean(cwd) == cwd
	dir := cwd
	for range maxVanishedLevels + 1 {
		deps, err := a.deps(ctx, dir)
		if !walkable || !errors.Is(err, ErrNoDirectory) {
			return deps, err
		}
		if deps.Close != nil {
			deps.Close()
		}
		dir = filepath.Dir(dir)
	}
	return Deps{}, fmt.Errorf("the hook's cwd %s does not exist, nor do its %d nearest parents: change to a directory that exists", cwd, maxVanishedLevels)
}

func readHookInput(r io.Reader) (hookInput, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxHookInput+1))
	if err != nil {
		return hookInput{}, fmt.Errorf("read the hook event: %w", err)
	}
	if len(data) > maxHookInput {
		return hookInput{}, errors.New("the hook event is too large")
	}
	var in hookInput
	if err := json.Unmarshal(data, &in); err != nil {
		return hookInput{}, fmt.Errorf("read the hook event: %w", err)
	}
	if in.Cwd == "" || in.Event == "" {
		return hookInput{}, errors.New("the hook event has no cwd or hook_event_name")
	}
	return in, nil
}

func (a *App) preToolUse(ctx context.Context, deps Deps, in hookInput) error {
	call := domain.ToolCall{Tool: in.ToolName, Role: domain.RoleOf(in.AgentType), Command: stringField(in.ToolInput, "command")}
	named := toolPaths(in)
	if domain.IsWriteTool(call.Tool) && writesLocalConfig(deps.LocalConfigDir, named) {
		return a.deny("millennium-code-qc: local configs are protected: agents never change the rules they are checked against; the user changes them with `millennium-code-qc config apply --local`")
	}
	for _, raw := range named {
		paths, err := guardedPaths(deps, raw)
		if err != nil {
			return err
		}
		call.Paths = appendNew(call.Paths, paths...)
	}
	decision := deps.Guard.Execute(ctx, call)
	if !decision.Deny {
		return nil
	}
	return a.deny(decision.Reason)
}

func (a *App) deny(reason string) error {
	return a.writeHookJSON(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason,
	}})
}

// guardedPaths maps a path a tool call names, absolute, into the repository
// for the guard, both as named and with its symlinks resolved: resolved
// catches a symlink that leads to a protected path, and as named a
// protected path that is itself a symlink. A path outside the repository
// either way is left out, since the guard's rules cover only the
// repository. Any other failure blocks the call: an unmapped path might be
// a protected one.
func guardedPaths(deps Deps, raw string) ([]string, error) {
	if !filepath.IsAbs(raw) {
		return nil, fmt.Errorf("cannot check the tool's path %s: it is not absolute, so the hook event's cwd is not either; run the session from an absolute directory", raw)
	}
	var paths []string
	if named, ok := namedPath(deps.Root, raw); ok {
		paths = append(paths, named)
	}
	resolved, err := deps.RepoPath(raw)
	switch {
	case errors.Is(err, ErrOutsideRepository):
		return paths, nil
	case err != nil:
		return nil, fmt.Errorf("cannot check the tool's path, so the call is blocked: %w", err)
	}
	return appendNew(paths, resolved), nil
}

// namedPath is raw relative to the repository root as written, without
// resolving any symlink, if it lies inside.
func namedPath(root, raw string) (string, bool) {
	rel, err := filepath.Rel(root, raw)
	if err != nil {
		return "", false
	}
	if rel = filepath.ToSlash(rel); rel == "." {
		return "", true
	}
	return rel, domain.IsRepoRelative(rel)
}

// appendNew appends each path list does not hold yet.
func appendNew(list []string, paths ...string) []string {
	for _, p := range paths {
		if !slices.Contains(list, p) {
			list = append(list, p)
		}
	}
	return list
}

// writesLocalConfig reports whether any path is inside the local config
// directory, which lies outside the repository the guard's rules cover.
func writesLocalConfig(dir string, paths []string) bool {
	if dir == "" {
		return false
	}
	for _, p := range paths {
		if rel, err := filepath.Rel(dir, p); err == nil && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

// toolPaths lists the paths a tool call names, absolute or relative to the
// session's cwd. Grep and Glob without a path search the cwd.
func toolPaths(in hookInput) []string {
	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(in.Cwd, p)
	}
	switch in.ToolName {
	case "Edit", "Write", "MultiEdit", "Read":
		return nonEmpty(resolve, stringField(in.ToolInput, "file_path"))
	case "NotebookEdit":
		return nonEmpty(resolve, stringField(in.ToolInput, "notebook_path"))
	case "Grep", "Glob":
		if p := stringField(in.ToolInput, "path"); p != "" {
			return []string{resolve(p)}
		}
		return []string{in.Cwd}
	}
	return nil
}

func nonEmpty(resolve func(string) string, p string) []string {
	if p == "" {
		return nil
	}
	return []string{resolve(p)}
}

func stringField(fields map[string]any, name string) string {
	value, _ := fields[name].(string)
	return value
}

func (a *App) stopHook(ctx context.Context, deps Deps, in hookInput) error {
	verdict, err := deps.Stop.Execute(ctx, in.SessionID+"/"+in.AgentID)
	if err != nil {
		return err
	}
	switch {
	case verdict.Block:
		return a.writeHookJSON(map[string]any{"decision": "block", "reason": stopMessage(verdict, "fix them before stopping")})
	case verdict.Escalate:
		return a.writeHookJSON(map[string]any{"systemMessage": stopMessage(verdict,
			fmt.Sprintf("still failing after %d attempts; the work is NOT done, decide how to proceed", usecase.MaxStopAttempts))})
	}
	return nil
}

func stopMessage(v usecase.StopVerdict, ending string) string {
	var body bytes.Buffer
	if v.Problem != "" {
		fmt.Fprintf(&body, "millennium-code-qc: BLOCKED: %s\n", v.Problem)
	} else {
		writeText(&body, v.Report, "changed files")
	}
	return fmt.Sprintf("%smillennium-code-qc gates (attempt %d): %s.", body.String(), v.Attempt, ending)
}

// sessionContext tells every session in an opted-in repository how the work
// is checked, so agents do not discover the gates by tripping over them. The
// plugin's launcher is not on PATH, so the context names the command to run.
func sessionContext(engine string) string {
	if engine == "" {
		engine = "millennium-code-qc"
	}
	return fmt.Sprintf("millennium is active in this repository. The engine command is %[1]s (wherever the docs write `millennium-code-qc`, use that command). "+
		"Gates check every change: hooks run the fast tier when an agent stops and block it while anything fails; before calling work done, run `%[1]s gate --tier full` "+
		"(tests and coverage included) and fix every finding. Agents never change .millennium/, the project's harness settings (.claude/settings*.json, .pi/settings.json, .pi/extensions/), or (as the software-engineer agent) the acceptance tests; "+
		"rule changes are proposals the user applies with `millennium-code-qc config apply`. Use `%[1]s config explain <file>` when a finding is surprising.", engine)
}

// maxBriefingRunes keeps the briefing under Claude Code's 10,000-character
// cap on hook context, beyond which it shows the agent only a preview.
const maxBriefingRunes = 9500

// briefing is the session context with the guides of the conventions the
// repository uses, whole guides only, as many as fit; the rest are named
// with the command that prints them.
func briefing(engine string, cfg domain.Config) string {
	var b strings.Builder
	b.WriteString(sessionContext(engine))
	guides := cfg.Guides()
	if len(guides) == 0 {
		return b.String()
	}
	if engine == "" {
		engine = "millennium-code-qc"
	}
	b.WriteString("\n\nConventions, by component:")
	for _, c := range cfg.Components {
		if len(c.Conventions) > 0 {
			fmt.Fprintf(&b, " %s: %s;", c.Name(), strings.Join(conventionNames(c.Conventions), ", "))
		}
	}
	b.WriteString(" follow each convention's guide when working on its components.")
	var omitted []string
	for _, pack := range guides {
		block := fmt.Sprintf("\n\n<convention-guide name=%q>\n%s\n</convention-guide>", pack.Name, strings.TrimSpace(pack.Guide))
		if utf8.RuneCountInString(b.String())+utf8.RuneCountInString(block) > maxBriefingRunes-200 {
			omitted = append(omitted, string(pack.Name))
			continue
		}
		b.WriteString(block)
	}
	if len(omitted) > 0 {
		fmt.Fprintf(&b, "\n\nThe guides of %s did not fit here: read each with `%s convention show <name>` before working on its components.", strings.Join(omitted, ", "), engine)
	}
	return b.String()
}

// brief tells a new session or subagent in an opted-in repository how its
// work is checked, with the guides of the repository's conventions.
func (a *App) brief(ctx context.Context, deps Deps, event string) error {
	cfg, enabled, err := deps.Config.Execute(ctx)
	if err != nil || !enabled {
		// A broken config is reported by the gates on the first stop.
		return nil
	}
	return a.writeHookJSON(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": event, "additionalContext": briefing(deps.EngineCommand, cfg),
	}})
}

func (a *App) writeHookJSON(v any) error {
	enc := json.NewEncoder(a.stdout)
	return enc.Encode(v)
}
