package domain

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Agent roles the guard knows. A plugin agent's type arrives scoped, such as
// "millennium:engineer"; the role is the part after the last colon.
const (
	RoleEngineer = "engineer"
	RoleQATester = "qa-tester"
)

// ToolCall is a tool use an agent asks for, as the hooks see it.
type ToolCall struct {
	Tool string
	// Role is the calling agent's role, "" for the main session.
	Role string
	// Paths are the repository-relative paths the call names; paths outside
	// the repository are left out.
	Paths []string
	// Command is the shell command of a Bash call.
	Command string
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
// against, and the settings that load the checks.
func ProtectedPaths() []string {
	return []string{".millennium", ".claude/settings.json", ".claude/settings.local.json"}
}

var (
	// shellWrite matches shell commands that can change a file.
	shellWrite = regexp.MustCompile(`>|\btee\b|\bsed\s+(-\S*\s+)*-i|\b(mv|cp|rm|ln|chmod|truncate|install|dd)\b|\bgit\s+(checkout|restore|rm|mv|apply)\b`)
	// applyCommand matches the human-only command that changes the rules.
	applyCommand = regexp.MustCompile(`\bmillennium-code-qc(\.exe)?["']?\s+config\s+apply\b`)
)

// Guard decides a tool call: no agent may write protected paths or apply a
// config; the engineer may not change the acceptance tests it is checked
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

func guardWrite(cfg Config, call ToolCall) GuardDecision {
	for _, p := range call.Paths {
		if protected, ok := protectedPath(p); ok {
			return deny("%s is protected: agents never change the rules they are checked against; the user changes them with `millennium-code-qc config apply`", protected)
		}
		if call.Role == RoleEngineer && Within(p, cfg.AcceptanceTests) {
			return deny("%s is an acceptance test; the engineer cannot change the tests that judge its work (qa-tester owns them)", p)
		}
	}
	return GuardDecision{}
}

func protectedPath(p string) (string, bool) {
	for _, protected := range ProtectedPaths() {
		if Within(p, protected) {
			return protected, true
		}
	}
	return "", false
}

// guardBlackBox keeps qa-tester out of component code: it may read the
// acceptance tests, anything outside the components, and documents inside
// them (a spec, a README, an API description), but not code, and it may not
// search inside components, since a search reads every file it passes.
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
		if strings.Contains(command, protected) && shellWrite.MatchString(command) {
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
