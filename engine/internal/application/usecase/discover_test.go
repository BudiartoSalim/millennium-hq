package usecase

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"millennium/internal/domain"
)

// mapFiles reads the working tree from a map.
type mapFiles map[string]string

func (m mapFiles) ReadWorking(p string) ([]byte, error) {
	content, ok := m[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(content), nil
}

func (mapFiles) ReadAt(context.Context, string, string) ([]byte, bool, error) { return nil, false, nil }

func (mapFiles) ChangedLines(context.Context, string, string) (domain.LineSet, error) {
	return domain.LineSet{}, nil
}

func TestDiscoverDraftsAValidConfig(t *testing.T) {
	repo := &fakeRepo{files: []string{
		"api/go.mod", "api/internal/domain/user.go", "api/main.go",
		"web/package.json", "web/src/App.tsx",
		"tools/package.json", "tools/gen.ts",
	}}
	source := mapFiles{
		"web/package.json":   `{"dependencies": {"react": "19.0.0"}}`,
		"tools/package.json": `not json`,
	}
	got, err := NewDiscover(repo, source, fakePacks{}).Execute(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Discovery.Modules) != 3 || !got.Discovery.Modules[2].React || got.Discovery.Modules[1].React {
		t.Fatalf("modules = %+v", got.Discovery.Modules)
	}
	if len(got.Components) != 3 || !slices.Equal(got.Components[0].Conventions, []string{"clean-architecture"}) {
		t.Fatalf("components = %+v", got.Components)
	}
}

func TestDiscoverFailures(t *testing.T) {
	listErr := errors.New("git broke")
	if _, err := NewDiscover(&fakeRepo{filesErr: listErr}, mapFiles{}, fakePacks{}).Execute(t.Context()); !errors.Is(err, listErr) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewDiscover(&fakeRepo{}, mapFiles{}, brokenPacks{}).Execute(t.Context()); err == nil {
		t.Fatal("an unreadable pack store must fail discover")
	}
	// A directory name that is not a valid path pattern cannot be drafted.
	if _, err := NewDiscover(&fakeRepo{files: []string{"we[ird/go.mod", "we[ird/a.go"}}, mapFiles{}, fakePacks{}).Execute(t.Context()); err == nil {
		t.Fatal("an undraftable layout must be an error, not a broken proposal")
	}
	empty, err := NewDiscover(&fakeRepo{}, mapFiles{}, fakePacks{}).Execute(t.Context())
	if err != nil || len(empty.Components) != 0 {
		t.Fatalf("an empty repository: %+v, %v", empty, err)
	}
}
