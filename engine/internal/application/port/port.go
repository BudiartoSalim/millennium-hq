// Package port declares what the application core needs from the outside
// world. Adapters in infrastructure implement these interfaces; main wires
// them together.
package port

import (
	"context"
	"errors"

	"millennium/internal/domain"
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

// ToolLocator finds executables. Locate returns an error describing why name
// cannot be run.
type ToolLocator interface {
	Locate(name string) error
}

// GateInput is what every gate receives.
type GateInput struct {
	Config  domain.Config
	Changes domain.ChangeSet
}

// Gate is one check. Run returns the findings it found; an error means the
// gate could not decide, which the caller turns into a blocking finding.
type Gate interface {
	Name() string
	Run(ctx context.Context, in GateInput) ([]domain.Finding, error)
}
