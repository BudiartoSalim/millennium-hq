package domain

import (
	"fmt"
	"path"
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
	// named and with their symlinks resolved, and one that lies inside
	// where a protected location of a checkout resolves to (`.pi` a
	// symlink to `conf`) also as that location spells it. The protected
	// paths are matched in them wherever they lie, in this repository or
	// any other directory: a checkout git no longer lists (moved, or named
	// with a newline) is still a checkout whose rules no agent may change.
	Targets []string
	// Paths are the paths the call names relative to the worktree of the
	// repository each lies in, the agent's own or any other (the main
	// checkout and every linked worktree), and "" for each worktree a
	// searched directory holds whole. The role rules apply to them.
	Paths []string
	// Command is the shell command of a Bash call.
	Command string
}

// PathNeed says which of a call's located paths the guard's rules read, so
// the hook works out no more than that. Both are false for a call no path
// rule applies to.
type PathNeed struct {
	// Targets: a rule matches the protected paths in the Targets.
	Targets bool
	// Paths: a role rule reads the Paths, in every worktree of the
	// repository; they are placed from the Targets.
	Paths bool
}

// Any reports whether a rule reads any of the call's paths.
func (n PathNeed) Any() bool { return n.Targets || n.Paths }

// PathNeedOf is what the guard's rules read of the paths of a call of tool
// by role, from the rules that apply to it (guardRules), the same ones
// Guard runs: every write is matched against the protected paths; the
// software engineer's writes and qa-tester's reads also get their role's
// rule, in every worktree of the repository.
func PathNeedOf(tool, role string) PathNeed {
	var need PathNeed
	for _, rule := range guardRules() {
		if rule.appliesTo(tool, role) {
			need.Targets = need.Targets || rule.reads.Targets
			need.Paths = need.Paths || rule.reads.Paths
		}
	}
	return need
}

// guardRule is one rule of the guard: the calls it applies to, which of
// their located paths it reads, and its check.
type guardRule struct {
	tools []string
	// roles are the roles it applies to; nil is every role, the main
	// session's included.
	roles []string
	reads PathNeed
	check func(Config, ToolCall) GuardDecision
}

// appliesTo reports whether the rule applies to a call of tool by role.
func (r guardRule) appliesTo(tool, role string) bool {
	return slices.Contains(r.tools, tool) && (r.roles == nil || slices.Contains(r.roles, role))
}

// guardRules are the guard's rules, in the order Guard runs them. Each
// declares what it reads of a call's paths, and PathNeedOf derives from
// these declarations what the hook locates, so a rule cannot be given
// fewer paths than it reads.
func guardRules() []guardRule {
	return []guardRule{
		{tools: writeTools(), reads: PathNeed{Targets: true}, check: guardProtected},
		{tools: writeTools(), roles: []string{RoleSoftwareEngineer}, reads: PathNeed{Paths: true}, check: guardAcceptanceTests},
		{tools: readTools(), roles: []string{RoleQATester}, reads: PathNeed{Paths: true}, check: guardBlackBox},
		{tools: []string{"Bash"}, check: guardShell},
	}
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

// Guard decides a tool call: no agent may write protected paths or apply a
// config; the software engineer may not change the acceptance tests it is checked
// against; qa-tester may not read the implementation it tests black-box.
// Shell commands are matched best-effort: the OS sandbox, not this, is what
// stops a determined command.
func Guard(cfg Config, call ToolCall) GuardDecision {
	for _, rule := range guardRules() {
		if !rule.appliesTo(call.Tool, call.Role) {
			continue
		}
		if decision := rule.check(cfg, call); decision.Deny {
			return decision
		}
	}
	return GuardDecision{}
}

// guardProtected denies a write that names a protected path anywhere in a
// target (see ProtectedPathIn). The Paths are matched too, for a caller
// that gives only those; the hook places them from the Targets, so they
// hold no protected path the Targets do not, and the rule needs only the
// Targets located.
func guardProtected(_ Config, call ToolCall) GuardDecision {
	for _, p := range slices.Concat(call.Targets, call.Paths) {
		if protected, ok := ProtectedPathIn(p); ok {
			return deny("%s is protected: agents never change the rules they are checked against; the user changes them with `millennium-code-qc config apply`", protected)
		}
	}
	return GuardDecision{}
}

// guardAcceptanceTests keeps the software engineer out of the acceptance
// tests in every worktree of the repository, matched without regard to
// case (see WithinFold).
func guardAcceptanceTests(cfg Config, call ToolCall) GuardDecision {
	for _, p := range call.Paths {
		if WithinFold(p, cfg.AcceptanceTests) {
			return deny("%s is an acceptance test; the software-engineer agent cannot change the tests that judge its work (qa-tester owns them)", p)
		}
	}
	return GuardDecision{}
}

// ProtectedPathIn reports the protected path that p, slash-separated, holds
// as whole segments, anywhere in it, matched without regard to case and
// with each segment read as Win32 opens it (a trailing dot or space, a
// `:stream`): a protected path is protected in every checkout and every
// directory, wherever it lies, and in whatever spelling reaches it.
func ProtectedPathIn(p string) (string, bool) {
	for _, protected := range ProtectedPaths() {
		if hasSegmentsFold(p, protected) {
			return protected, true
		}
	}
	return "", false
}

// ProtectedLeadIn reports the leading part of a protected path that p,
// slash-separated, ends with, a proper part such as `.pi` of
// `.pi/extensions`, anywhere and matched as ProtectedPathIn matches
// (`sub/.Pi`, `.claude.`). Whatever stands there holds protected paths:
// a symlink or another directory put there brings its own (`.pi` a link
// to `conf`, whose `extensions/x.ts` pi then loads), and in git such a
// path can only be a file or a symlink.
func ProtectedLeadIn(p string) (string, bool) {
	for _, protected := range ProtectedPaths() {
		for _, lead := range properLeads(protected) {
			if endsWithSegmentsFold(p, lead) {
				return lead, true
			}
		}
	}
	return "", false
}

// properLeads are the leading parts of protected, a slash-separated path,
// shorter than it: `.claude` of `.claude/settings.json`.
func properLeads(protected string) []string {
	segments := strings.Split(protected, "/")
	leads := make([]string, 0, len(segments)-1)
	for n := 1; n < len(segments); n++ {
		leads = append(leads, strings.Join(segments[:n], "/"))
	}
	return leads
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

func deny(format string, args ...any) GuardDecision {
	return GuardDecision{Deny: true, Reason: "millennium-code-qc: " + fmt.Sprintf(format, args...)}
}

func displayPath(p string) string {
	if p == "" {
		return "the repository root"
	}
	return p
}
