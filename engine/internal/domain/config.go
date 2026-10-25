package domain

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// ConfigPath is where a project's config lives, relative to the repository
// root. Its presence is what opts a repository in to the gates.
const ConfigPath = ".millennium/config.json"

// ConfigVersion is the only config format version this engine reads.
const ConfigVersion = 1

// Language is a programming language the engine can check.
type Language string

// Supported languages.
const (
	LanguageGo         Language = "go"
	LanguageTypeScript Language = "typescript"
	LanguagePython     Language = "python"
)

// Languages lists every supported language in a stable order.
func Languages() []Language {
	return []Language{LanguageGo, LanguageTypeScript, LanguagePython}
}

// Convention is a convention pack a project can select.
type Convention string

// Supported conventions.
const (
	ConventionCleanArchitecture Convention = "clean-architecture"
	ConventionReactFeatureBased Convention = "react-feature-based"
)

// Conventions lists every supported convention in a stable order.
func Conventions() []Convention {
	return []Convention{ConventionCleanArchitecture, ConventionReactFeatureBased}
}

// TestingProfile is the testing discipline the gates enforce.
type TestingProfile string

// TestingTestsRequired requires unit tests for changed production code in
// the layers a convention marks as tested. Writing them first is not required.
const TestingTestsRequired TestingProfile = "tests-required"

// TestingProfiles lists every supported testing profile.
func TestingProfiles() []TestingProfile { return []TestingProfile{TestingTestsRequired} }

// DependencyPolicy is how strictly new dependencies are gated.
type DependencyPolicy string

// DependencyPolicyStrict requires a vetting record for every dependency.
const DependencyPolicyStrict DependencyPolicy = "strict"

// DependencyPolicies lists every supported dependency policy.
func DependencyPolicies() []DependencyPolicy { return []DependencyPolicy{DependencyPolicyStrict} }

// PythonTypeCheck selects the Python type checker. Type checking is optional
// because the standard library has none.
type PythonTypeCheck string

// Supported Python type-check settings.
const (
	PythonTypeCheckOff  PythonTypeCheck = "off"
	PythonTypeCheckMypy PythonTypeCheck = "mypy"
)

// PythonTypeChecks lists every supported Python type-check setting.
func PythonTypeChecks() []PythonTypeCheck {
	return []PythonTypeCheck{PythonTypeCheckOff, PythonTypeCheckMypy}
}

// Defaults for settings a config may leave out.
const (
	DefaultTesting          = TestingTestsRequired
	DefaultDependencyPolicy = DependencyPolicyStrict
	DefaultPythonTypeCheck  = PythonTypeCheckOff
	DefaultAcceptanceTests  = "tests/acceptance"
)

// Config is a validated project configuration. Build one with NewConfig.
type Config struct {
	Languages        []Language
	Conventions      []Convention
	Testing          TestingProfile
	DependencyPolicy DependencyPolicy
	PythonTypeCheck  PythonTypeCheck
	AcceptanceTests  string
	Run              *RunRecipe
}

// RunRecipe tells qa-tester how to start the software under test and where
// to wait for it to become healthy.
type RunRecipe struct {
	Start     string
	HealthURL string
}

// ConfigInput is an unvalidated config as read from a file. A nil field was
// absent and takes its default; an explicitly empty value is validated like
// any other and rejected.
type ConfigInput struct {
	Version          *int
	Languages        []string
	Conventions      []string
	Testing          *string
	DependencyPolicy *string
	PythonTypeCheck  *string
	AcceptanceTests  *string
	Run              *RunRecipe
}

// ConfigError lists every problem found in a config, so a single run reports
// all of them instead of one per attempt.
type ConfigError struct {
	Problems []string
}

func (e *ConfigError) Error() string {
	return "invalid config: " + strings.Join(e.Problems, "; ")
}

// repoDirPattern allowlists the characters of a configured directory. Segment
// checks for "." and ".." happen separately.
var repoDirPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)

// NewConfig validates in, applies defaults, and returns the config or a
// *ConfigError naming every problem.
func NewConfig(in ConfigInput) (Config, error) {
	v := &validator{}
	v.version(in.Version)
	if len(in.Languages) == 0 {
		v.add("languages: at least one language is required")
	}
	cfg := Config{
		Languages:        parseList(v, "languages", in.Languages, Languages()),
		Conventions:      parseList(v, "conventions", in.Conventions, Conventions()),
		Testing:          parseChoice(v, "testing", in.Testing, TestingProfiles(), DefaultTesting),
		DependencyPolicy: parseChoice(v, "dependencyPolicy", in.DependencyPolicy, DependencyPolicies(), DefaultDependencyPolicy),
		PythonTypeCheck:  parseChoice(v, "python.typeCheck", in.PythonTypeCheck, PythonTypeChecks(), DefaultPythonTypeCheck),
		AcceptanceTests:  v.repoDir("paths.acceptanceTests", in.AcceptanceTests, DefaultAcceptanceTests),
		Run:              v.run(in.Run),
	}
	v.crossCheck(cfg)
	if len(v.problems) > 0 {
		return Config{}, &ConfigError{Problems: v.problems}
	}
	return cfg, nil
}

// Has reports whether the config enables lang.
func (c Config) Has(lang Language) bool { return slices.Contains(c.Languages, lang) }

// Uses reports whether the config selects the convention.
func (c Config) Uses(convention Convention) bool { return slices.Contains(c.Conventions, convention) }

type validator struct {
	problems []string
}

func (v *validator) add(problem string) { v.problems = append(v.problems, problem) }

func (v *validator) addf(format string, args ...any) { v.add(fmt.Sprintf(format, args...)) }

func (v *validator) version(version *int) {
	switch {
	case version == nil:
		v.add("version: required")
	case *version != ConfigVersion:
		v.addf("version: %d is not supported; this engine reads version %d", *version, ConfigVersion)
	}
}

func parseChoice[T ~string](v *validator, field string, raw *string, allowed []T, fallback T) T {
	if raw == nil {
		return fallback
	}
	choice := T(*raw)
	if !slices.Contains(allowed, choice) {
		v.addf("%s: %q is not one of %s", field, *raw, quoteAll(allowed))
	}
	return choice
}

func parseList[T ~string](v *validator, field string, raw []string, allowed []T) []T {
	out := make([]T, 0, len(raw))
	for _, item := range raw {
		choice := T(item)
		switch {
		case !slices.Contains(allowed, choice):
			v.addf("%s: %q is not one of %s", field, item, quoteAll(allowed))
		case slices.Contains(out, choice):
			v.addf("%s: %q is listed more than once", field, item)
		default:
			out = append(out, choice)
		}
	}
	return out
}

func (v *validator) repoDir(field string, raw *string, fallback string) string {
	if raw == nil {
		return fallback
	}
	if !isSafeRepoDir(*raw) {
		v.addf("%s: %q must be a relative path inside the repository using letters, digits, '.', '_', '-' and '/', without '.' or '..' segments", field, *raw)
	}
	return *raw
}

func isSafeRepoDir(p string) bool {
	if !repoDirPattern.MatchString(p) {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func (v *validator) run(in *RunRecipe) *RunRecipe {
	if in == nil {
		return nil
	}
	if strings.TrimSpace(in.Start) == "" {
		v.add("run.start: required when run is set")
	}
	if !isLoopbackHTTP(in.HealthURL) {
		v.addf("run.healthUrl: %q must be an http(s) URL on localhost or a loopback address", in.HealthURL)
	}
	recipe := *in
	return &recipe
}

// isLoopbackHTTP accepts only local health URLs, so a config can never point
// qa-tester at a shared or production deployment.
func isLoopbackHTTP(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// crossCheck rejects settings that are individually valid but meaningless
// together; silently ignoring them would hide a misconfiguration.
func (v *validator) crossCheck(cfg Config) {
	if cfg.Uses(ConventionReactFeatureBased) && !cfg.Has(LanguageTypeScript) {
		v.addf("conventions: %q needs %q in languages", ConventionReactFeatureBased, LanguageTypeScript)
	}
	if cfg.PythonTypeCheck == PythonTypeCheckMypy && !cfg.Has(LanguagePython) {
		v.addf("python.typeCheck: %q needs %q in languages", PythonTypeCheckMypy, LanguagePython)
	}
}

func quoteAll[T ~string](values []T) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = fmt.Sprintf("%q", value)
	}
	return strings.Join(quoted, ", ")
}
