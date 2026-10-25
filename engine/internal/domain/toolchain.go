package domain

import "strings"

// ToolRequirement is an executable that the configured checks cannot run
// without. Names lists the names it may be installed under, in preference
// order; any one of them satisfies the requirement.
type ToolRequirement struct {
	Names  []string
	Reason string
}

// Describe names the tool for messages: "go", "python3 or python".
func (r ToolRequirement) Describe() string {
	if len(r.Names) <= 1 {
		return strings.Join(r.Names, "")
	}
	last := len(r.Names) - 1
	return strings.Join(r.Names[:last], ", ") + " or " + r.Names[last]
}

// RequiredTools lists the executables cfg depends on. A missing one blocks:
// a check that cannot run must never count as a pass.
func RequiredTools(cfg Config) []ToolRequirement {
	var tools []ToolRequirement
	for _, lang := range cfg.Languages {
		tools = append(tools, toolsFor(lang)...)
	}
	if cfg.PythonTypeCheck == PythonTypeCheckMypy {
		tools = append(tools, ToolRequirement{Names: []string{"mypy"}, Reason: `python.typeCheck is "mypy"`})
	}
	return tools
}

// toolsFor names the toolchain each language's checks use. Python is
// python3 on Linux and macOS but usually python or the py launcher on native
// Windows. A name can still point at Python 2; the Python helper checks the
// version itself when it runs.
func toolsFor(lang Language) []ToolRequirement {
	reason := "languages includes " + string(lang)
	switch lang {
	case LanguageGo:
		return []ToolRequirement{{Names: []string{"go"}, Reason: reason}, {Names: []string{"gofmt"}, Reason: reason}}
	case LanguageTypeScript:
		return []ToolRequirement{{Names: []string{"node"}, Reason: reason}}
	case LanguagePython:
		return []ToolRequirement{{Names: []string{"python3", "python", "py"}, Reason: reason}}
	}
	return nil
}
