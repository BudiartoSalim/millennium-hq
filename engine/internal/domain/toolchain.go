package domain

// ToolRequirement is an executable that the configured checks cannot run
// without, with the reason it is needed.
type ToolRequirement struct {
	Name   string
	Reason string
}

// RequiredTools lists the executables cfg depends on. A missing one blocks:
// a check that cannot run must never count as a pass.
func RequiredTools(cfg Config) []ToolRequirement {
	var tools []ToolRequirement
	for _, lang := range cfg.Languages {
		tools = append(tools, toolsFor(lang)...)
	}
	if cfg.PythonTypeCheck == PythonTypeCheckMypy {
		tools = append(tools, ToolRequirement{Name: "mypy", Reason: `python.typeCheck is "mypy"`})
	}
	return tools
}

// toolsFor names the toolchain each language's checks use. python3 is the
// Linux and macOS name; native Windows is an open item in docs/design.md
// ("To verify before building").
func toolsFor(lang Language) []ToolRequirement {
	reason := "languages includes " + string(lang)
	switch lang {
	case LanguageGo:
		return []ToolRequirement{{Name: "go", Reason: reason}, {Name: "gofmt", Reason: reason}}
	case LanguageTypeScript:
		return []ToolRequirement{{Name: "node", Reason: reason}}
	case LanguagePython:
		return []ToolRequirement{{Name: "python3", Reason: reason}}
	}
	return nil
}
