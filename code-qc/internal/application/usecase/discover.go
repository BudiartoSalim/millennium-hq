package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
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
	packs  port.PackCatalog
}

// NewDiscover returns the discover use-case.
func NewDiscover(repo port.Repository, source port.SourceReader, packs port.PackCatalog) *Discover {
	return &Discover{repo: repo, source: source, packs: packs}
}

// Execute discovers the repository and drafts components, validated as a
// config so the draft is always one `config apply` would accept.
func (u *Discover) Execute(ctx context.Context) (DiscoverResult, error) {
	files, err := u.repo.Files(ctx)
	if err != nil {
		return DiscoverResult{}, fmt.Errorf("list files: %w", err)
	}
	catalog, err := u.packs.Packs()
	if err != nil {
		return DiscoverResult{}, fmt.Errorf("read the convention packs: %w", err)
	}
	d := domain.Discover(domain.DiscoverInput{Files: files, Dependencies: u.dependencies, Packs: catalog.Packs()})
	components := d.Suggest()
	if len(components) > 0 {
		version := domain.ConfigVersion
		cfg, err := domain.NewConfig(domain.ConfigInput{Version: &version, Components: components})
		if err == nil {
			_, err = cfg.Resolve(catalog)
		}
		if err != nil {
			return DiscoverResult{}, fmt.Errorf("cannot draft a config for this layout: %w", err)
		}
	}
	return DiscoverResult{Discovery: d, Components: components}, nil
}

// dependencies lists the packages the package.json at root depends on, dev
// dependencies included. An unreadable or broken package.json lists none.
func (u *Discover) dependencies(root string) []string {
	data, err := u.source.ReadWorking(path.Join(root, "package.json"))
	if err != nil {
		return nil
	}
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return nil
	}
	var names []string
	for _, deps := range []map[string]string{manifest.Dependencies, manifest.DevDependencies} {
		for name := range deps {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
