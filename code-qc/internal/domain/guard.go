package domain

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Agent roles the guard knows. A plugin agent's type arrives scoped, such as
// "millennium:software-engineer"; the role is the part after the last colon.
const (
	RoleSoftwareEngineer = "software-engineer"
	RoleQATester         = "qa-tester"
)

// ToolCall is a tool use an agent asks for, as the hooks see it.
type ToolCall struct {
	Tool string
	// Role is the calling agent's role, "" for the main session.
	Role string
	// Targets are the absolute paths the call names, slash-separated, as
	// named and with their symlinks resolved. The protected paths are
	// matched in them wherever they lie, in this repository or any other
	// directory: a checkout git no longer lists (moved, or named with a
	// newline) is still a checkout whose rules no agent may change.
	Targets []string
	// Paths are the paths the call names relative to the worktree of the
	// repository each lies in, the agent's own or any other (the main
	// checkout and every linked worktree), and "" for each worktree a
	// searched directory holds whole. The role rules apply to them.
	Paths []string
	// Command is the shell command of a Bash call.
	Command string
}

// PathNeed says what the guard needs to know about the paths a tool call
// names, so the hook works out no more than that.
type PathNeed string

// What the guard needs to know about a call's paths.
const (
	// NeedNoPaths: no rule reads the call's paths.
	NeedNoPaths PathNeed = "none"
	// NeedTargets: the protected paths, matched in the Targets.
	NeedTargets PathNeed = "targets"
	// NeedWorktreePaths: a role rule too, which reads Paths in every
	// worktree of the repository.
	NeedWorktreePaths PathNeed = "worktree paths"
)

// PathNeedOf is what the guard needs to know about the paths of a call of
// tool by role: every write is matched against the protected paths; the
// software engineer's writes and qa-tester's reads also get their role's
// rule, in every worktree of the repository.
func PathNeedOf(tool, role string) PathNeed {
	switch {
	case IsWriteTool(tool) && role == RoleSoftwareEngineer:
		return NeedWorktreePaths
	case IsWriteTool(tool):
		return NeedTargets
	case slices.Contains(readTools(), tool) && role == RoleQATester:
		return NeedWorktreePaths
	}
	return NeedNoPaths
}

// WorktreePaths places each target, an absolute slash-separated path, in
// the worktree it lies in: relative to the innermost of roots that holds
// it, matched without regard to case, since worktrees nest (Claude Code's
// lie under the main checkout's .claude/worktrees) and a file belongs to
// the checkout it is in. A target that holds a root, such as a directory
// searched above a worktree, reaches that worktree whole, so it also gives
// "", its root. Roots are absolute, slash-separated and clean; a target
// outside every root that holds none gives nothing.
func WorktreePaths(targets, roots []string) []string {
	var paths []string
	for _, target := range targets {
		if rel, ok := innermostRelative(target, roots); ok {
			paths = appendMissing(paths, rel)
		}
		if slices.ContainsFunc(roots, func(root string) bool { return WithinFold(root, target) }) {
			paths = appendMissing(paths, "")
		}
	}
	return paths
}

// innermostRelative is target relative to the deepest of roots that holds
// it.
func innermostRelative(target string, roots []string) (string, bool) {
	best, depth := "", -1
	for _, root := range roots {
		rel, ok := RelativeFold(root, target)
		if d := strings.Count(root, "/"); ok && d > depth {
			best, depth = rel, d
		}
	}
	return best, depth >= 0
}

// appendMissing appends p unless list holds it.
func appendMissing(list []string, p string) []string {
	if slices.Contains(list, p) {
		return list
	}
	return append(list, p)
}

// GuardDecision is the guard's answer: allow, or deny with a reason the
// agent sees.
type GuardDecision struct {
	Deny   bool
	Reason string
}

// RoleOf reads the role from a hook's agent type.
func RoleOf(agentType string) string {
	if i := strings.LastIndex(agentType, ":"); i >= 0 {
		return agentType[i+1:]
	}
	return agentType
}

func writeTools() []string { return []string{"Edit", "Write", "MultiEdit", "NotebookEdit"} }

// IsWriteTool reports whether tool changes files.
func IsWriteTool(tool string) bool { return slices.Contains(writeTools(), tool) }

func readTools() []string { return []string{"Read", "Grep", "Glob"} }

// ProtectedPaths are what no agent may write: the rules it is checked
// against, and the settings and code that load the checks: Claude Code's
// project settings, and pi's project settings and extensions (a project
// extension runs inside pi and could switch millennium's off).
func ProtectedPaths() []string {
	return []string{".millennium", ".claude/settings.json", ".claude/settings.local.json", ".pi/settings.json", ".pi/extensions"}
}

var (
	// shellWrite matches shell commands that can change a file.
	shellWrite = regexp.MustCompile(`>|\btee\b|\bsed\s+(-\S*\s+)*-i|\b(mv|cp|rm|ln|chmod|truncate|install|dd)\b|\bgit\s+(checkout|restore|rm|mv|apply)\b`)
	// applyCommand matches the human-only command that changes the rules.
	applyCommand = regexp.MustCompile(`\bmillennium-code-qc(\.exe)?["']?\s+config\s+apply\b`)
)

// Guard decides a tool call: no agent may write protected paths or apply a
// config; the software engineer may not change the acceptance tests it is checked
// against; qa-tester may not read the implementation it tests black-box.
// Shell commands are matched best-effort: the OS sandbox, not this, is what
// stops a determined command.
func Guard(cfg Config, call ToolCall) GuardDecision {
	switch {
	case slices.Contains(writeTools(), call.Tool):
		return guardWrite(cfg, call)
	case slices.Contains(readTools(), call.Tool) && call.Role == RoleQATester:
		return guardBlackBox(cfg, call)
	case call.Tool == "Bash":
		return guardShell(call.Command)
	}
	return GuardDecision{}
}

// guardWrite matches paths without regard to case (see WithinFold): the
// protected paths anywhere in a path the call names, and for the software
// engineer the acceptance tests in every worktree of the repository.
func guardWrite(cfg Config, call ToolCall) GuardDecision {
	for _, p := range slices.Concat(call.Targets, call.Paths) {
		if protected, ok := protectedPath(p); ok {
			return deny("%s is protected: agents never change the rules they are checked against; the user changes them with `millennium-code-qc config apply`", protected)
		}
	}
	if call.Role != RoleSoftwareEngineer {
		return GuardDecision{}
	}
	for _, p := range call.Paths {
		if WithinFold(p, cfg.AcceptanceTests) {
			return deny("%s is an acceptance test; the software-engineer agent cannot change the tests that judge its work (qa-tester owns them)", p)
		}
	}
	return GuardDecision{}
}

// protectedPath reports the protected path that p holds as whole segments,
// anywhere in it: a protected path is protected in every checkout and
// every directory, wherever it lies.
func protectedPath(p string) (string, bool) {
	for _, protected := range ProtectedPaths() {
		if hasSegmentsFold(p, protected) {
			return protected, true
		}
	}
	return "", false
}

// guardBlackBox keeps qa-tester out of component code: it may read the
// acceptance tests, anything outside the components, and documents inside
// them (a spec, a README, an API description), but not code, and it may not
// search inside components, since a search reads every file it passes.
// The paths are those in every worktree of the repository, judged by the
// acting session's config. Components match without regard to case (see
// Reaches); the acceptance tests match as spelled here: they are an
// exception that allows, and folding case would only allow more.
func guardBlackBox(cfg Config, call ToolCall) GuardDecision {
	for _, p := range call.Paths {
		if Within(p, cfg.AcceptanceTests) || (call.Tool == "Read" && IsDocument(p)) {
			continue
		}
		if cfg.Reaches(p) {
			return deny("%s is implementation code; qa-tester tests black-box, from the spec, the documents and the running software (it may Read documents such as specs and READMEs by path)", displayPath(p))
		}
	}
	return GuardDecision{}
}

// documentExtensions are the files qa-tester may read inside a component:
// prose, API descriptions and images, never code or data the code embeds.
var documentExtensions = map[string]bool{
	".md": true, ".markdown": true, ".mdx": true, ".txt": true, ".rst": true, ".adoc": true,
	".json": true, ".yaml": true, ".yml": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".pdf": true,
}

// IsDocument reports whether a file is a document rather than code: one of
// documentExtensions, or an extensionless README, LICENSE or CHANGELOG.
func IsDocument(p string) bool {
	base := path.Base(p)
	if ext := strings.ToLower(path.Ext(base)); ext != "" {
		return documentExtensions[ext]
	}
	switch strings.ToUpper(base) {
	case "README", "LICENSE", "CHANGELOG":
		return true
	}
	return false
}

func guardShell(command string) GuardDecision {
	if applyCommand.MatchString(command) {
		return deny("only the user runs `millennium-code-qc config apply`; write a proposal instead and ask them to apply it")
	}
	for _, protected := range ProtectedPaths() {
		if ContainsFold(command, protected) && shellWrite.MatchString(command) {
			return deny("this command looks like it changes %s, which no agent may change", protected)
		}
	}
	return GuardDecision{}
}

func deny(format string, args ...any) GuardDecision {
	return GuardDecision{Deny: true, Reason: "millennium-code-qc: " + fmt.Sprintf(format, args...)}
}

func displayPath(p string) string {
	if p == "" {
		return "the repository root"
	}
	return p
}
