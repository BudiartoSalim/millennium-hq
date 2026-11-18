package cli

import (
	"bytes"
	"cmp"
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
	deps, err := a.deps(ctx, cwd)
	if !errors.Is(err, ErrNoDirectory) {
		return deps, err
	}
	release(deps)
	if !filepath.IsAbs(cwd) || filepath.Clean(cwd) != cwd {
		return Deps{}, fmt.Errorf("the hook's cwd %s does not exist, and the engine looks for an existing parent only of an absolute path in clean form "+
			"(no . or .. parts, no trailing separator), so the hook blocks: change to a directory that exists, such as the repository root", cwd)
	}
	dir := cwd
	for range maxVanishedLevels {
		dir = filepath.Dir(dir)
		deps, err := a.deps(ctx, dir)
		if !errors.Is(err, ErrNoDirectory) {
			return deps, err
		}
		release(deps)
	}
	return Deps{}, fmt.Errorf("the hook's cwd %s does not exist, nor do its %d nearest parents: change to a directory that exists", cwd, maxVanishedLevels)
}

// release frees what deps hold, if anything.
func release(deps Deps) {
	if deps.Close != nil {
		deps.Close()
	}
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
	named, err := toolPaths(in)
	if err != nil {
		return err
	}
	local, err := writesLocalConfig(deps, call.Tool, named)
	if err != nil {
		return err
	}
	if local {
		return a.deny("millennium-code-qc: local configs are protected: agents never change the rules they are checked against; the user changes them with `millennium-code-qc config apply --local`")
	}
	if call.Paths, call.OtherWorktreePaths, err = worktreePaths(ctx, deps, named); err != nil {
		return err
	}
	decision := deps.Guard.Execute(ctx, call)
	if !decision.Deny {
		return nil
	}
	return a.deny(decision.Reason)
}

// worktreePaths maps the named paths into the working tree the agent is in
// and into every other worktree of the repository, so that no agent writes
// the rules of a checkout it is not in.
func worktreePaths(ctx context.Context, deps Deps, named []string) (own, others []string, err error) {
	if len(named) == 0 {
		return nil, nil, nil
	}
	roots, err := deps.OtherWorktrees(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot check the tool's path in every worktree of the repository, so the call is blocked: %w", err)
	}
	if own, err = pathsIn(deps, []string{deps.Root}, named); err != nil {
		return nil, nil, err
	}
	if others, err = pathsIn(deps, roots, named); err != nil {
		return nil, nil, err
	}
	return own, others, nil
}

// writesLocalConfig reports whether a file tool writes inside the local
// config directory, which lies outside the repository the guard's rules
// cover. The paths are mapped as for the guard, symlinks resolved, so a
// link in the repository does not reach the local configs either.
func writesLocalConfig(deps Deps, tool string, named []string) (bool, error) {
	if !domain.IsWriteTool(tool) || deps.LocalConfigDir == "" {
		return false, nil
	}
	local, err := pathsIn(deps, []string{deps.LocalConfigDir}, named)
	return len(local) > 0, err
}

// pathsIn maps each named path into each tree root, leaving out the paths
// outside it.
func pathsIn(deps Deps, roots, named []string) ([]string, error) {
	var paths []string
	for _, root := range roots {
		for _, raw := range named {
			mapped, err := guardedPaths(deps, root, raw)
			if err != nil {
				return nil, err
			}
			paths = appendNew(paths, mapped...)
		}
	}
	return paths, nil
}

func (a *App) deny(reason string) error {
	return a.writeHookJSON(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason,
	}})
}

// guardedPaths maps a path a tool call names, absolute, into the tree at
// root for the guard, both as named and with its symlinks resolved:
// resolved catches a symlink that leads to a protected path, and as named a
// protected path that is itself a symlink. A path outside the tree either
// way is left out, since the guard's rules cover only what is inside. Any
// other failure blocks the call: an unmapped path might be a protected one.
func guardedPaths(deps Deps, root, raw string) ([]string, error) {
	if !filepath.IsAbs(raw) {
		return nil, fmt.Errorf("cannot check the tool's path %s: it is not absolute, so the hook event's cwd is not either; run the session from an absolute directory", raw)
	}
	var paths []string
	if named, ok := namedPath(root, raw); ok {
		paths = append(paths, named)
	}
	resolved, err := deps.MapPath(root, raw)
	switch {
	case errors.Is(err, ErrOutsideRepository):
		return paths, nil
	case err != nil:
		return nil, fmt.Errorf("cannot check the tool's path, so the call is blocked: %w", err)
	}
	return appendNew(paths, resolved), nil
}

// namedPath is raw relative to root as written, without resolving any
// symlink, if it lies inside; root's part is matched without regard to
// case, as a case-insensitive file system would.
func namedPath(root, raw string) (string, bool) {
	rel, ok := domain.RelativeFold(filepath.ToSlash(filepath.Clean(root)), filepath.ToSlash(raw))
	return rel, ok && (rel == "" || domain.IsRepoRelative(rel))
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

// toolPaths lists the paths a tool call names, absolute or relative to the
// session's cwd, cleaned. Grep and Glob without a path search the cwd.
func toolPaths(in hookInput) ([]string, error) {
	var p string
	switch in.ToolName {
	case "Edit", "Write", "MultiEdit", "Read":
		p = stringField(in.ToolInput, "file_path")
	case "NotebookEdit":
		p = stringField(in.ToolInput, "notebook_path")
	case "Grep", "Glob":
		p = cmp.Or(stringField(in.ToolInput, "path"), ".")
	}
	if p == "" {
		return nil, nil
	}
	if !filepath.IsAbs(p) {
		p = in.Cwd + string(filepath.Separator) + p
	}
	if hasParentPart(p) {
		return nil, fmt.Errorf("the tool's path %s has a '..' part, which the hook cannot check: the OS follows a symlink before '..', so the path may lead elsewhere than it reads; name the path without '..', such as its absolute path", p)
	}
	return []string{filepath.Clean(p)}, nil
}

// hasParentPart reports whether p has a ".." part, between any separators
// the OS accepts.
func hasParentPart(p string) bool {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == filepath.Separator })
	return slices.Contains(parts, "..")
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
