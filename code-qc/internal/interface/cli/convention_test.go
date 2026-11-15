package cli

import (
	"errors"
	"strings"
	"testing"

	"millennium-code-qc/internal/application/usecase"
	"millennium-code-qc/internal/domain"
)

type fakeConventions struct {
	packs     []domain.Pack
	draft     usecase.Draft
	err       error
	gotTarget usecase.ApplyTarget
	gotFrom   domain.Convention
}

func (f *fakeConventions) List(target usecase.ApplyTarget) ([]domain.Pack, error) {
	f.gotTarget = target
	return f.packs, f.err
}

func (f *fakeConventions) Show(target usecase.ApplyTarget, name domain.Convention) (domain.Pack, error) {
	f.gotTarget = target
	for _, p := range f.packs {
		if p.Name == name {
			return p, nil
		}
	}
	return domain.Pack{}, errors.Join(f.err, usecase.ErrUnknownConvention)
}

func (f *fakeConventions) New(target usecase.ApplyTarget, name, from domain.Convention) (usecase.Draft, error) {
	f.gotTarget, f.gotFrom = target, from
	return f.draft, f.err
}

func ptr[T any](v T) *T { return &v }

func samplePack(t *testing.T) domain.Pack {
	t.Helper()
	pack, err := domain.NewPack(domain.PackInput{
		Name: "flat", Origin: domain.PackProject, Languages: []string{"go", "typescript"},
		Layers: []domain.PackLayer{
			{Name: "core", Tested: true},
			{Name: "features", MayImport: []string{"core"}, ThirdParty: true, Isolated: true},
		},
		Paths: map[string]map[string][]string{
			"go":         {"core": {"core"}, "features": {"features/*"}, "composition": {"main.go", "cmd"}},
			"typescript": {"core": {"src/core"}},
		},
		Limits:  domain.LimitsInput{Cognitive: ptr(10)},
		Suggest: []string{"chi"},
		Guide:   "# Flat\n\nKeep it flat.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

func TestConventionListAndShow(t *testing.T) {
	h := &harness{conventions: &fakeConventions{packs: []domain.Pack{samplePack(t)}}}
	if code := h.run(t, "convention", "list", "--local"); code != 0 || !strings.Contains(h.stdout.String(), "flat") ||
		!strings.Contains(h.stdout.String(), "project  go, typescript") || h.conventions.gotTarget != usecase.ApplyLocal {
		t.Fatalf("exit %d: %q %s", code, h.stdout.String(), h.stderr.String())
	}
	show := &harness{conventions: &fakeConventions{packs: []domain.Pack{samplePack(t)}}}
	if code := show.run(t, "convention", "show", "flat"); code != 0 {
		t.Fatalf("exit %d: %s", code, show.stderr.String())
	}
	assertContainsAll(t, show.stdout.String(), []string{
		"convention flat (project)", "limits:    functions at most 50 lines, nesting depth 3, cognitive complexity 10",
		"suggested for modules that depend on: chi",
		"core: may import nothing of the project; no third-party code; tests required",
		"features: may import core; third-party code allowed; units isolated from each other",
		"  go:\n    composition: main.go, cmd\n    core: core\n    features: features/*\n  typescript:\n    core: src/core",
		"guide:\n\n# Flat\n\nKeep it flat.\n",
	})
}

func TestConventionNew(t *testing.T) {
	draft := usecase.Draft{Pack: samplePack(t), Dir: "millennium.proposal/conventions/flat", Proposal: "{\"version\": 1}\n"}
	h := &harness{conventions: &fakeConventions{draft: draft}}
	if code := h.run(t, "convention", "new", "flat", "--from", "react-feature-based"); code != 0 || h.conventions.gotFrom != "react-feature-based" {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	assertContainsAll(t, h.stdout.String(), []string{
		"Drafted convention flat, a copy of react-feature-based, in millennium.proposal/conventions/flat/",
		"{\"version\": 1}", "millennium-code-qc config apply",
	})
	defaults := &harness{conventions: &fakeConventions{draft: draft}}
	if code := defaults.run(t, "convention", "new", "flat"); code != 0 || defaults.conventions.gotFrom != usecase.DefaultDraftSource {
		t.Fatalf("exit %d, from %q", code, defaults.conventions.gotFrom)
	}
	before := &harness{conventions: &fakeConventions{draft: draft}}
	if code := before.run(t, "convention", "new", "--from", "flat", "--local", "onion"); code != 0 || before.conventions.gotFrom != "flat" || before.conventions.gotTarget != usecase.ApplyLocal {
		t.Fatalf("exit %d, from %q", code, defaults.conventions.gotFrom)
	}
}

func TestConventionFailures(t *testing.T) {
	for _, args := range [][]string{
		{"convention"}, {"convention", "list", "extra"}, {"convention", "show"}, {"convention", "new"},
		{"convention", "rename", "a"}, {"convention", "list", "--nope"}, {"convention", "new", "a", "--nope"}, {"convention", "new", "a", "b"},
	} {
		if code := (&harness{}).run(t, args...); code != 2 {
			t.Errorf("%q: exit %d", args, code)
		}
	}
	broken := errors.New("bad pack")
	for _, args := range [][]string{{"convention", "list"}, {"convention", "show", "flat"}, {"convention", "new", "flat"}} {
		h := &harness{conventions: &fakeConventions{err: broken}}
		if code := h.run(t, args...); code != 2 || !strings.Contains(h.stderr.String(), "bad pack") {
			t.Errorf("%q: exit %d: %s", args, code, h.stderr.String())
		}
	}
}
