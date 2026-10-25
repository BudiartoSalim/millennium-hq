package usecase

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

type fakeConfigFile struct {
	cfg    domain.Config
	raw    []byte
	err    error
	rawErr error
}

func (f fakeConfigFile) Load() (domain.Config, error) { return f.cfg, f.err }
func (f fakeConfigFile) Raw() ([]byte, error)         { return f.raw, f.rawErr }

// jsonCodec parses {"config": {"languages": [...]}, "approve": ["name@version"]}
// proposals.
type jsonCodec struct{}

func (jsonCodec) ParseProposal(data []byte) (port.Proposal, error) {
	var p struct {
		Config *struct {
			Languages []string `json:"languages"`
		} `json:"config"`
		Approve []string `json:"approve"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return port.Proposal{}, err
	}
	var out port.Proposal
	if p.Config != nil {
		cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: p.Config.Languages})
		if err != nil {
			return port.Proposal{}, err
		}
		out.Config, out.Canonical = &cfg, []byte(strings.Join(p.Config.Languages, ","))
	}
	for _, key := range p.Approve {
		name, version, _ := domain.SplitKey(key)
		a, err := domain.NewApproval(domain.ApprovalInput{Ecosystem: "npm", Name: name, Version: version, Decision: "approved", Reason: "vetted in the test", Checked: "2026-10-01"})
		if err != nil {
			return port.Proposal{}, err
		}
		out.Approvals = append(out.Approvals, a)
	}
	return out, nil
}

func (jsonCodec) FormatProposal(config []byte) ([]byte, error) {
	return append([]byte("proposal:"), config...), nil
}

func (jsonCodec) FormatApproval(a domain.Approval) ([]byte, error) { return []byte(a.Key()), nil }

type recordingWriter struct {
	calls []string
	err   error
}

func (w *recordingWriter) WriteFile(target string, data []byte) error {
	w.calls = append(w.calls, "write "+target+" "+string(data))
	return w.err
}
func (w *recordingWriter) MergeSettings(id string) error {
	w.calls = append(w.calls, "settings "+id)
	return nil
}
func (w *recordingWriter) SetAgentsSection(section string) error {
	w.calls = append(w.calls, "agents")
	return nil
}
func (w *recordingWriter) Ignore(p string) error {
	w.calls = append(w.calls, "ignore "+p)
	return nil
}

func applyFixture(t *testing.T, committed, local fakeConfigFile) (*ApplyConfig, *recordingWriter) {
	t.Helper()
	w := &recordingWriter{}
	files := ConfigFiles{Committed: committed, CommittedPath: "/repo/.millennium/config.json", Local: local, LocalPath: "/home/u/.config/millennium/projects/k/config.json",
		CommittedApprovals: "/repo/.millennium/deps", LocalApprovals: "/home/u/.config/millennium/projects/k/deps"}
	return NewApplyConfig(jsonCodec{}, files, w), w
}

func TestApplyCommittedConfig(t *testing.T) {
	current, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"go", "python"}})
	if err != nil {
		t.Fatal(err)
	}
	uc, w := applyFixture(t, fakeConfigFile{cfg: current}, fakeConfigFile{err: port.ErrConfigNotFound})
	plan, err := uc.Prepare([]byte(`{"config":{"languages":["go"]}}`), ApplyCommitted)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !slices.Equal(plan.Loosenings, []string{"language python is no longer checked"}) || len(plan.Digest) != 12 || len(plan.Writes) != 4 {
		t.Fatalf("plan = %+v", plan)
	}
	if err := uc.Apply(plan); err != nil {
		t.Fatal(err)
	}
	want := []string{"write /repo/.millennium/config.json go", "settings " + PluginID, "agents", "ignore " + ProposalFile, "ignore " + WorktreesDir}
	if !slices.Equal(w.calls, want) {
		t.Fatalf("writes = %q, want %q", w.calls, want)
	}
}

func TestApplyLocalConfigAndExport(t *testing.T) {
	uc, w := applyFixture(t, fakeConfigFile{err: port.ErrConfigNotFound}, fakeConfigFile{err: port.ErrConfigNotFound, raw: []byte("{}")})
	plan, err := uc.Prepare([]byte(`{"config":{"languages":["go"]}}`), ApplyLocal)
	if err != nil || len(plan.Loosenings) != 0 || len(plan.Writes) != 1 || !strings.Contains(plan.Writes[0], "outside the repository") {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	if err := uc.Apply(plan); err != nil || !slices.Equal(w.calls, []string{"write /home/u/.config/millennium/projects/k/config.json go"}) {
		t.Fatalf("writes = %q, %v", w.calls, err)
	}
	exported, err := uc.Export(ApplyLocal)
	if err != nil || string(exported) != "proposal:{}" {
		t.Fatalf("Export = %q, %v", exported, err)
	}
}

func TestApplyWarnsWhenTheCurrentConfigIsUnreadable(t *testing.T) {
	uc, _ := applyFixture(t, fakeConfigFile{err: errors.New("bad json")}, fakeConfigFile{})
	plan, err := uc.Prepare([]byte(`{"config":{"languages":["go"]}}`), ApplyCommitted)
	if err != nil || !strings.Contains(plan.Warning, "cannot be listed: bad json") {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
}

func TestApplyFailures(t *testing.T) {
	uc, w := applyFixture(t, fakeConfigFile{err: port.ErrConfigNotFound}, fakeConfigFile{rawErr: errors.New("no local config")})
	if _, err := uc.Prepare([]byte("not json"), ApplyCommitted); err == nil {
		t.Fatal("an invalid proposal must be refused")
	}
	if _, err := uc.Export(ApplyLocal); err == nil {
		t.Fatal("exporting a missing config must fail")
	}
	plan, err := uc.Prepare([]byte(`{"config":{"languages":["go"]}}`), ApplyCommitted)
	if err != nil {
		t.Fatal(err)
	}
	w.err = errors.New("read-only file system")
	if err := uc.Apply(plan); err == nil || len(w.calls) != 1 {
		t.Fatalf("a failed write must stop the apply: %v, calls %q", err, w.calls)
	}
}

func TestAgentsSection(t *testing.T) {
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{
		{Paths: []string{"services/*/api"}, Languages: []string{"go"}, Conventions: []string{"clean-architecture"}},
		{Paths: []string{"scripts"}, Languages: []string{"shell"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	section := AgentsSection(cfg)
	for _, want := range []string{"`services/*/api`: go (clean-architecture)", "`scripts`: shell\n", "at most 50 lines", "millennium gate --tier full"} {
		if !strings.Contains(section, want) {
			t.Errorf("section lacks %q:\n%s", want, section)
		}
	}
}

func TestLoadConfigFallsBackToTheLocalConfig(t *testing.T) {
	local, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"python"}})
	if err != nil {
		t.Fatal(err)
	}
	committed := enabledSource()
	tests := []struct {
		name     string
		source   fakeSource
		repo     *fakeRepo
		local    fakeSource
		wantLang domain.Language
		enabled  bool
		wantErr  bool
	}{
		{"committed wins", committed, &fakeRepo{}, fakeSource{cfg: local}, domain.LanguageGo, true, false},
		{"local when none is committed", fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{}, fakeSource{cfg: local}, domain.LanguagePython, true, false},
		{"neither", fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{}, fakeSource{err: port.ErrConfigNotFound}, "", false, false},
		{"broken local config", fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{}, fakeSource{err: errors.New("bad")}, "", false, true},
		{"a removed committed config is not replaced by a local one", fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{tracked: true}, fakeSource{cfg: local}, "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, enabled, err := NewLoadConfig(tt.source, tt.repo).WithLocal(tt.local).Execute(t.Context())
			if (err != nil) != tt.wantErr || enabled != tt.enabled || (tt.enabled && !cfg.Has(tt.wantLang)) {
				t.Fatalf("cfg %v enabled %v err %v", cfg.Languages, enabled, err)
			}
		})
	}
}

func TestApplyApprovals(t *testing.T) {
	uc, w := applyFixture(t, fakeConfigFile{err: port.ErrConfigNotFound}, fakeConfigFile{err: port.ErrConfigNotFound})
	plan, err := uc.Prepare([]byte(`{"approve":["express@4.21.2"]}`), ApplyCommitted)
	if err != nil || plan.Config != nil || len(plan.Writes) != 1 || !strings.Contains(plan.Writes[0], "approves npm express@4.21.2 (vetted in the test)") {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	if err := uc.Apply(plan); err != nil || !slices.Equal(w.calls, []string{"write /repo/.millennium/deps/npm/express@4.21.2.json express@4.21.2"}) {
		t.Fatalf("writes = %q, %v", w.calls, err)
	}
	local, lw := applyFixture(t, fakeConfigFile{}, fakeConfigFile{err: port.ErrConfigNotFound})
	plan, err = local.Prepare([]byte(`{"config":{"languages":["go"]},"approve":["ms@2.0.0"]}`), ApplyLocal)
	if err != nil {
		t.Fatal(err)
	}
	if err := local.Apply(plan); err != nil || !slices.Equal(lw.calls, []string{
		"write /home/u/.config/millennium/projects/k/deps/npm/ms@2.0.0.json ms@2.0.0",
		"write /home/u/.config/millennium/projects/k/config.json go",
	}) {
		t.Fatalf("writes = %q, %v", lw.calls, err)
	}
	w.err = errors.New("read-only")
	if err := uc.Apply(ApplyPlan{Approvals: plan.Approvals}); err == nil {
		t.Fatal("a failed approval write must stop the apply")
	}
}

type failingFormat struct{ jsonCodec }

func (failingFormat) FormatApproval(domain.Approval) ([]byte, error) {
	return nil, errors.New("format failed")
}

func TestApplyStopsWhenARecordCannotBeFormatted(t *testing.T) {
	w := &recordingWriter{}
	uc := NewApplyConfig(failingFormat{}, ConfigFiles{CommittedApprovals: "/d"}, w)
	plan, err := uc.Prepare([]byte(`{"approve":["x@1.0.0"]}`), ApplyCommitted)
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Apply(plan); err == nil || len(w.calls) != 0 {
		t.Fatalf("err = %v, writes %q", err, w.calls)
	}
}
