package usecase

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

func TestGuardToolCall(t *testing.T) {
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	edit := domain.ToolCall{Tool: "Edit", Paths: []string{".millennium/config.json"}}
	read := domain.ToolCall{Tool: "Read", Paths: []string{".millennium/config.json"}}
	tests := []struct {
		name   string
		source fakeSource
		repo   *fakeRepo
		call   domain.ToolCall
		deny   bool
	}{
		{"opted in: protected", fakeSource{cfg: cfg}, &fakeRepo{}, edit, true},
		{"not opted in: allowed", fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{}, edit, false},
		{"broken config: writes blocked", fakeSource{err: errors.New("bad json")}, &fakeRepo{}, domain.ToolCall{Tool: "Write", Paths: []string{"main.go"}}, true},
		{"broken config: reads allowed", fakeSource{err: errors.New("bad json")}, &fakeRepo{}, read, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewGuardToolCall(NewLoadConfig(tt.source, tt.repo)).Execute(t.Context(), tt.call)
			if got.Deny != tt.deny {
				t.Fatalf("decision %+v, want deny %v", got, tt.deny)
			}
		})
	}
}

type memoryAttempts struct {
	counts map[string]int
	err    error
}

func (m *memoryAttempts) Increment(key string) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	m.counts[key]++
	return m.counts[key], nil
}

func (m *memoryAttempts) Reset(key string) error {
	delete(m.counts, key)
	return nil
}

func TestStopCheckBlocksThenEscalates(t *testing.T) {
	repo := &fakeRepo{}
	failing := &fakeGate{name: "g", findings: []domain.Finding{{Gate: "g", Rule: "r", Message: "broken"}}}
	attempts := &memoryAttempts{counts: map[string]int{}}
	uc := NewStopCheck(NewRunGates(NewLoadConfig(enabledSource(), repo), repo, []port.Gate{failing}), attempts)
	var verdicts []string
	for range MaxStopAttempts + 1 {
		v, err := uc.Execute(t.Context(), "session/agent")
		if err != nil {
			t.Fatal(err)
		}
		verdicts = append(verdicts, map[bool]string{true: "block", false: "escalate"}[v.Block])
		if v.Escalate == v.Block {
			t.Fatalf("a verdict either blocks or escalates: %+v", v)
		}
	}
	if want := []string{"block", "block", "block", "escalate"}; !slices.Equal(verdicts, want) {
		t.Fatalf("verdicts = %q, want %q", verdicts, want)
	}
	if failing.got.Tier != domain.TierFast {
		t.Fatalf("the stop check must run the fast tier, ran %q", failing.got.Tier)
	}
	failing.findings = nil
	if v, err := uc.Execute(t.Context(), "session/agent"); err != nil || v.Block || v.Escalate || attempts.counts["session/agent"] != 0 {
		t.Fatalf("a clean stop passes and resets the count: %+v, %v, %v", v, err, attempts.counts)
	}
}

func TestStopCheckProblems(t *testing.T) {
	repo := &fakeRepo{tracked: true}
	broken := NewRunGates(NewLoadConfig(fakeSource{err: port.ErrConfigNotFound}, repo), repo, []port.Gate{&fakeGate{name: "g"}})
	v, err := NewStopCheck(broken, &memoryAttempts{counts: map[string]int{}}).Execute(t.Context(), "k")
	if err != nil || !v.Block || !strings.Contains(v.Problem, "tracked at HEAD") {
		t.Fatalf("a gate run that cannot decide blocks with the problem: %+v, %v", v, err)
	}
	failingStore := &memoryAttempts{err: errors.New("disk full")}
	if _, err := NewStopCheck(broken, failingStore).Execute(t.Context(), "k"); err == nil {
		t.Fatal("an attempt store failure must be an error")
	}
}

func TestCheckScope(t *testing.T) {
	repo := &fakeRepo{changes: []domain.ChangedFile{
		{Path: "svc/internal/domain/user.go", Kind: domain.ChangeModified},
		{Path: "svc/internal/infrastructure/db.go", Kind: domain.ChangeModified},
		{Path: "tests/acceptance/login_test.go", Kind: domain.ChangeAdded},
		{Path: ".millennium/config.json", Kind: domain.ChangeModified},
	}}
	uc := NewCheckScope(NewLoadConfig(enabledSourceWithDefaults(t), repo), repo)
	report, err := uc.Execute(t.Context(), ScopeInput{Base: "main", Allow: []string{"svc/internal/domain/**"}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got []string
	for _, f := range report.Findings() {
		got = append(got, f.Rule+" "+f.File)
	}
	want := []string{"out-of-scope svc/internal/infrastructure/db.go", "acceptance-test-changed tests/acceptance/login_test.go", "protected-path .millennium/config.json"}
	if !slices.Equal(got, want) || repo.gotBase != "main" {
		t.Fatalf("findings %q, want %q (base %q)", got, want, repo.gotBase)
	}
}

func TestCheckScopeFailures(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewCheckScope(NewLoadConfig(enabledSourceWithDefaults(t), repo), repo)
	if _, err := uc.Execute(t.Context(), ScopeInput{}); err == nil || !strings.Contains(err.Error(), "--allow") {
		t.Fatalf("no allowed paths: %v", err)
	}
	if _, err := uc.Execute(t.Context(), ScopeInput{Allow: []string{"../x"}}); err == nil {
		t.Fatal("a bad pattern must be an error")
	}
	missing := NewCheckScope(NewLoadConfig(fakeSource{err: port.ErrConfigNotFound}, repo), repo)
	if _, err := missing.Execute(t.Context(), ScopeInput{Allow: []string{"x"}}); !errors.Is(err, ErrConfigRequired) {
		t.Fatalf("err = %v", err)
	}
	loadErr := errors.New("bad")
	broken := NewCheckScope(NewLoadConfig(fakeSource{err: loadErr}, repo), repo)
	if _, err := broken.Execute(t.Context(), ScopeInput{Allow: []string{"x"}}); !errors.Is(err, loadErr) {
		t.Fatalf("err = %v", err)
	}
	gitErr := errors.New("git failed")
	failingRepo := &fakeRepo{changesErr: gitErr}
	if _, err := NewCheckScope(NewLoadConfig(enabledSourceWithDefaults(t), failingRepo), failingRepo).Execute(t.Context(), ScopeInput{Allow: []string{"x"}}); !errors.Is(err, gitErr) {
		t.Fatalf("err = %v", err)
	}
}

func enabledSourceWithDefaults(t *testing.T) fakeSource {
	t.Helper()
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	return fakeSource{cfg: cfg}
}
