// Package usecase holds the engine's use-cases: loading the project config
// and running the gates over a change set.
package usecase

import (
	"context"
	"errors"
	"fmt"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// Errors that mean the gates cannot decide; callers must treat them as a block.
var (
	ErrConfigRemoved  = errors.New(domain.ConfigPath + " is tracked at HEAD but missing from the working tree; restore it, the gates cannot be switched off by deleting it")
	ErrConfigRequired = errors.New(domain.ConfigPath + " is required here but missing")
)

// LoadConfig reads the project config and decides whether the repository is
// opted in: by its committed config or, for a repository the user does not
// own, by a local config kept outside it.
type LoadConfig struct {
	source port.ConfigSource
	local  port.ConfigSource
	repo   port.Repository
}

// NewLoadConfig returns the load-config use-case for the committed config.
func NewLoadConfig(source port.ConfigSource, repo port.Repository) *LoadConfig {
	return &LoadConfig{source: source, repo: repo}
}

// WithLocal adds a local config, used when the repository has none of its
// own. A committed config always wins.
func (u *LoadConfig) WithLocal(local port.ConfigSource) *LoadConfig {
	u.local = local
	return u
}

// Execute returns the config and true when the repository is opted in, or a
// zero Config and false when it never was. A config that is missing from the
// working tree while still tracked at HEAD is ErrConfigRemoved, not "never
// opted in", so deleting the file cannot disable the gates.
func (u *LoadConfig) Execute(ctx context.Context) (domain.Config, bool, error) {
	cfg, err := u.source.Load()
	if err == nil {
		return cfg, true, nil
	}
	if !errors.Is(err, port.ErrConfigNotFound) {
		return domain.Config{}, false, fmt.Errorf("load config: %w", err)
	}
	tracked, err := u.repo.Tracked(ctx, "HEAD", domain.ConfigPath)
	if err != nil {
		return domain.Config{}, false, fmt.Errorf("check whether %s is tracked: %w", domain.ConfigPath, err)
	}
	if tracked {
		return domain.Config{}, false, ErrConfigRemoved
	}
	return u.loadLocal()
}

func (u *LoadConfig) loadLocal() (domain.Config, bool, error) {
	if u.local == nil {
		return domain.Config{}, false, nil
	}
	cfg, err := u.local.Load()
	if errors.Is(err, port.ErrConfigNotFound) {
		return domain.Config{}, false, nil
	}
	if err != nil {
		return domain.Config{}, false, fmt.Errorf("load the local config: %w", err)
	}
	return cfg, true, nil
}
