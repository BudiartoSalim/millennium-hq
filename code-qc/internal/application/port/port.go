// Package port declares what the application core needs from the outside
// world. Adapters in infrastructure implement these interfaces; main wires
// them together.
package port

import (
	"context"
	"errors"
	"time"

	"millennium-code-qc/internal/domain"
)

// ErrConfigNotFound is returned by a ConfigSource when the repository has no
// config file at all. Any other load failure is a different error.
var ErrConfigNotFound = errors.New("config not found")

// ErrPackUnavailable is returned by a PackSource for a convention this engine
// version does not ship yet.
var ErrPackUnavailable = errors.New("convention pack not available in this engine version")

// ConfigSource loads and validates the project config.
type ConfigSource interface {
	Load() (domain.Config, error)
}

// ConfigFile is a config source backed by a file, which can also return the
// file as it is, for export.
type ConfigFile interface {
	ConfigSource
	Raw() ([]byte, error)
}

// Proposal is a validated change an agent drafted for the user to apply: a
// new config, dependency approvals, or both.
type Proposal struct {
	// Config is nil when the proposal only approves dependencies.
	Config *domain.Config
	// Canonical is the config's indented JSON, as it will be written.
	Canonical []byte
	Approvals []domain.Approval
}

// ProposalCodec reads proposals and formats what `config apply` writes.
type ProposalCodec interface {
	ParseProposal(data []byte) (Proposal, error)
	FormatProposal(config []byte) ([]byte, error)
	FormatApproval(a domain.Approval) ([]byte, error)
}

// ProjectWriter writes the files `config apply` manages.
type ProjectWriter interface {
	// WriteFile writes data to an absolute path.
	WriteFile(target string, data []byte) error
	MergeSettings(pluginID string) error
	SetAgentsSection(section string) error
	Ignore(pattern string) error
}

// Repository reads change sets and history from version control.
type Repository interface {
	// Changes lists files that differ between base and the working tree,
	// untracked files included, with the resolved base revision. An empty
	// base means HEAD.
	Changes(ctx context.Context, base string) (domain.ChangeSet, error)
	// Files lists every file in the working tree that is tracked or not
	// ignored.
	Files(ctx context.Context) ([]string, error)
	// Tracked reports whether path exists in revision rev. A repository
	// without commits tracks nothing.
	Tracked(ctx context.Context, rev, path string) (bool, error)
}

// SourceReader reads file contents for analysis.
type SourceReader interface {
	// ReadWorking reads a file from the working tree.
	ReadWorking(path string) ([]byte, error)
	// ReadAt reads a file as it was in rev, reporting false when it did not
	// exist there.
	ReadAt(ctx context.Context, rev, path string) ([]byte, bool, error)
	// ChangedLines lists the lines of a modified file that differ from rev,
	// as numbered in the working tree. Callers treat added files, and every
	// file of an audit, as wholly changed instead of asking.
	ChangedLines(ctx context.Context, rev, path string) (domain.LineSet, error)
}

// ShellChecker runs the shell toolchain on one script.
type ShellChecker interface {
	// Syntax returns the script's syntax errors from the shell's own parser;
	// none means it parses.
	Syntax(ctx context.Context, path string) ([]domain.LintIssue, error)
	// Lint returns shellcheck's findings. dialect is the shell to assume for
	// a script that does not say ("" lets shellcheck decide).
	Lint(ctx context.Context, path, dialect string) ([]domain.LintIssue, error)
}

// BuildTools runs each language's own compiler, formatter and type checker.
// Directories and files are repository-relative; issues name
// repository-relative files.
type BuildTools interface {
	// Gofmt returns the files that are not gofmt-formatted.
	Gofmt(ctx context.Context, files []string) ([]string, error)
	// GoVet runs go vet on every package of the module in dir.
	GoVet(ctx context.Context, dir string) ([]domain.LintIssue, error)
	// TypeCheck runs the project's own tsc --noEmit for the tsconfig in dir.
	TypeCheck(ctx context.Context, dir string) ([]domain.LintIssue, error)
	// TSConfig returns the effective tsconfig of dir, extends resolved, as
	// tsc --showConfig prints it.
	TSConfig(ctx context.Context, dir string) ([]byte, error)
	// Mypy type-checks files, run from dir so the project's mypy config
	// applies.
	Mypy(ctx context.Context, dir string, files []string) ([]domain.LintIssue, error)
}

// TestResult is one test-suite run.
type TestResult struct {
	Failed bool
	// Summary names the failing tests, or the tail of the output.
	Summary  string
	Coverage domain.Coverage
}

// TestRunner runs test suites with line coverage. Directories are
// repository-relative. An error means the suite could not run at all.
type TestRunner interface {
	// GoTest runs go test with a coverage profile for the module in dir.
	GoTest(ctx context.Context, dir string) (TestResult, error)
	// PythonTest runs unittest discovery for the project in dir.
	PythonTest(ctx context.Context, dir string) (TestResult, error)
	// Command runs a component's own test recipe from dir and reads its
	// lcov report.
	Command(ctx context.Context, dir string, recipe domain.TestRecipe) (TestResult, error)
}

// ManifestFacts is what a dependency manifest says.
type ManifestFacts struct {
	// Lockfile marks a file that pins the full dependency set; its packages
	// are what approvals and registry checks apply to.
	Lockfile bool
	Packages []domain.LockedPackage
	// Issues are pinning problems; Code is the rule name.
	Issues []domain.LintIssue
}

// ManifestReader reads dependency manifests and lockfiles.
type ManifestReader interface {
	// Read returns the facts of the manifest at path, or false when the
	// file is not a manifest.
	Read(path string, content []byte) (ManifestFacts, bool, error)
}

// ApprovalSource provides the dependency approvals in force.
type ApprovalSource interface {
	Approvals() (domain.Approvals, error)
}

// Registry answers questions about published packages over the network.
type Registry interface {
	// PublishedAt is when a version was released.
	PublishedAt(ctx context.Context, p domain.Package) (time.Time, error)
	// Vulnerabilities maps each package with known advisories, by its
	// String form, to the advisory ids.
	Vulnerabilities(ctx context.Context, packages []domain.Package) (map[string][]string, error)
}

// IntegrityTools checks the package managers' own integrity settings.
type IntegrityTools interface {
	// GoEnv returns the go environment of the module in dir.
	GoEnv(ctx context.Context, dir string) (map[string]string, error)
	// NpmAuditSignatures verifies registry signatures of the installed
	// packages of the project in dir; with nothing installed there is
	// nothing to verify.
	NpmAuditSignatures(ctx context.Context, dir string) error
}

// FileFinder looks for marker files such as go.mod in the working tree.
type FileFinder interface {
	// NearestUp returns the closest directory at or above from, and inside
	// within, that contains marker.
	NearestUp(from, marker, within string) (string, bool)
}

// Analyzer extracts facts from source files of one language.
type Analyzer interface {
	Analyze(ctx context.Context, path string, content []byte) (domain.FileFacts, error)
}

// PackSource provides convention packs.
type PackSource interface {
	Pack(name domain.Convention) (domain.Pack, error)
}

// AttemptStore counts an agent's consecutive blocked stops. It lives where
// agents cannot write, so an agent cannot reset its own count.
type AttemptStore interface {
	Increment(key string) (int, error)
	Reset(key string) error
}

// ToolLocator finds executables. Locate returns an error describing why name
// cannot be run.
type ToolLocator interface {
	Locate(name string) error
}

// GateInput is what every gate receives.
type GateInput struct {
	Config  domain.Config
	Changes domain.ChangeSet
	// Tier is how thorough the run is; gates that run test suites do so
	// only in the full tier.
	Tier domain.Tier
}

// Rule is one kind of finding a gate reports.
type Rule struct {
	Name string
	// Disableable rules may be switched off in the config with a reason.
	// Rules that report a check failing to run never are: disabling them
	// would let unchecked code pass.
	Disableable bool
}

// Gate is one check. Run returns the findings it found; an error means the
// gate could not decide, which the caller turns into a blocking finding.
type Gate interface {
	Name() string
	// Rules lists every rule the gate can report.
	Rules() []Rule
	Run(ctx context.Context, in GateInput) ([]domain.Finding, error)
}
