package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// DiscoverResult is a discovery and the config it suggests.
type DiscoverResult struct {
	Discovery  domain.Discovery
	Components []domain.ComponentInput
}

// Discover inspects a repository that has no config yet: module roots,
// languages, tests, and which convention packs fit. It reads file names and
// package.json only, and runs nothing from the repository.
type Discover struct {
	repo   port.Repository
	source port.SourceReader
	packs  port.PackSource
}

// NewDiscover returns the discover use-case.
func NewDiscover(repo port.Repository, source port.SourceReader, packs port.PackSource) *Discover {
	return &Discover{repo: repo, source: source, packs: packs}
}

// Execute discovers the repository and drafts components, validated as a
// config so the draft is always one `config apply` would accept.
func (u *Discover) Execute(ctx context.Context) (DiscoverResult, error) {
	files, err := u.repo.Files(ctx)
	if err != nil {
		return DiscoverResult{}, fmt.Errorf("list files: %w", err)
	}
	var packs []domain.Pack
	for _, name := range domain.Conventions() {
		pack, err := u.packs.Pack(name)
		if err != nil {
			if errors.Is(err, port.ErrPackUnavailable) {
				continue
			}
			return DiscoverResult{}, err
		}
		packs = append(packs, pack)
	}
	d := domain.Discover(domain.DiscoverInput{Files: files, React: u.usesReact, Packs: packs})
	components := d.Suggest()
	if len(components) > 0 {
		version := domain.ConfigVersion
		if _, err := domain.NewConfig(domain.ConfigInput{Version: &version, Components: components}); err != nil {
			return DiscoverResult{}, fmt.Errorf("cannot draft a config for this layout: %w", err)
		}
	}
	return DiscoverResult{Discovery: d, Components: components}, nil
}

// usesReact reports whether the package.json at root depends on react. An
// unreadable or broken package.json counts as no.
func (u *Discover) usesReact(root string) bool {
	data, err := u.source.ReadWorking(path.Join(root, "package.json"))
	if err != nil {
		return false
	}
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return false
	}
	_, dep := manifest.Dependencies["react"]
	_, dev := manifest.DevDependencies["react"]
	return dep || dev
}
