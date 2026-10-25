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

// ConfigSource loads and validates the project config.
type ConfigSource interface {
	Load() (domain.Config, error)
}

// Repository reads change sets and history from version control.
type Repository interface {
	// Changes lists files that differ between base and the working tree,
	// untracked files included. An empty base means HEAD.
	Changes(ctx context.Context, base string) ([]domain.ChangedFile, error)
	// Tracked reports whether path exists in revision rev. A repository
	// without commits tracks nothing.
	Tracked(ctx context.Context, rev, path string) (bool, error)
}

// ToolLocator finds executables. Locate returns an error describing why name
// cannot be run.
type ToolLocator interface {
	Locate(name string) error
}

// GateInput is what every gate receives.
type GateInput struct {
	Config  domain.Config
	Changes []domain.ChangedFile
}

// Gate is one check. Run returns the findings it found; an error means the
// gate could not decide, which the caller turns into a blocking finding.
type Gate interface {
	Name() string
	Run(ctx context.Context, in GateInput) ([]domain.Finding, error)
}
