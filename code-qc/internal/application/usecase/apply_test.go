package usecase

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

type fakeConfigFile struct {
	cfg      domain.Config
	raw      []byte
	err      error
	rawErr   error
	project  []domain.Pack
	packsErr error
}

func (f fakeConfigFile) Load() (domain.Config, error) { return f.cfg, f.err }
func (f fakeConfigFile) Raw() ([]byte, error)         { return f.raw, f.rawErr }

func (f fakeConfigFile) Packs() (domain.Catalog, error) {
	if f.packsErr != nil {
		return nil, f.packsErr
	}
	return testPacks().With(f.project)
}

// flatPack is a project pack with one tested layer and the given cognitive
// complexity limit.
func flatPack(t *testing.T, cognitive int) domain.Pack {
	t.Helper()
	pack, err := domain.NewPack(domain.PackInput{
		Name: "flat", Origin: domain.PackProject, Languages: []string{"go"},
		Layers: []domain.PackLayer{{Name: "core", Tested: true}},
		Paths:  map[string]map[string][]string{"go": {"core": {"core"}}},
		Limits: domain.LimitsInput{Cognitive: &cognitive}, Guide: "Keep it flat.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

// jsonCodec parses {"config": {"languages": [...], "conventions": [...]},
// "packs": [{"name": ..., "cognitive": ...}], "approve": ["name@version"]}
// proposals.
type jsonCodec struct{}

type jsonPack struct {
	Name      string `json:"name"`
	Cognitive *int   `json:"cognitive"`
}

func (jsonCodec) ParseProposal(data []byte, _ string) (port.Proposal, error) {
	var p struct {
		Config *struct {
			Languages   []string `json:"languages"`
			Conventions []string `json:"conventions"`
		} `json:"config"`
		Packs   []jsonPack `json:"packs"`
		Approve []string   `json:"approve"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return port.Proposal{}, err
	}
	var out port.Proposal
	if p.Config != nil {
		cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: p.Config.Languages, Conventions: p.Config.Conventions})
		if err != nil {
			return port.Proposal{}, err
		}
		out.Config, out.Canonical = &cfg, []byte(strings.Join(p.Config.Languages, ","))
	}
	for _, jp := range p.Packs {
		pack, err := domain.NewPack(domain.PackInput{
			Name: jp.Name, Origin: domain.PackProject, Languages: []string{"go"},
			Layers: []domain.PackLayer{{Name: "core", Tested: true}},
			Paths:  map[string]map[string][]string{"go": {"core": {"core"}}},
			Limits: domain.LimitsInput{Cognitive: jp.Cognitive}, Guide: "Keep it flat.",
		})
		if err != nil {
			return port.Proposal{}, err
		}
		out.Packs = append(out.Packs, port.ProposedPack{Pack: pack, Files: port.PackFiles{Pack: []byte("pack " + jp.Name), Guide: []byte(pack.Guide)}})
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

func (jsonCodec) FormatPack(p domain.Pack) (port.PackFiles, error) {
	return port.PackFiles{Pack: []byte("pack " + p.Name), Guide: []byte(p.Guide)}, nil
}

type recordingWriter struct {
	calls []string
	err   error
	// failAt fails the n-th write (1-based) when err is set; 0 fails every
	// write.
	failAt int
}

func (w *recordingWriter) WriteFile(target string, data []byte) error {
	w.calls = append(w.calls, "write "+target+" "+string(data))
	if w.failAt > 0 && len(w.calls) != w.failAt {
		return nil
	}
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
		CommittedApprovals: "/repo/.millennium/deps", LocalApprovals: "/home/u/.config/millennium/projects/k/deps",
		CommittedPacks: "/repo/.millennium/conventions", LocalPacks: "/home/u/.config/millennium/projects/k/conventions"}
	return NewApplyConfig(jsonCodec{}, files, w), w
}

func TestApplyCommittedConfig(t *testing.T) {
	current, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"go", "python"}})
	if err != nil {
		t.Fatal(err)
	}
	uc, w := applyFixture(t, fakeConfigFile{cfg: current}, fakeConfigFile{err: port.ErrConfigNotFound})
	plan, err := uc.Prepare([]byte(`{"config":{"languages":["go"]}}`), "/repo", ApplyCommitted)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !slices.Equal(plan.Loosenings, []string{"language python is no longer checked"}) || len(plan.Digest) != 12 || len(plan.Writes) != 4 {
		t.Fatalf("plan = %+v", plan)
	}
	if err := uc.Apply(plan); err != nil {
		t.Fatal(err)
	}
	want := []string{"write /repo/.millennium/config.json go", "settings " + PluginID, "agents", "ignore " + ProposalFile, "ignore " + ProposalDir + "/", "ignore .claude/worktrees/", "ignore .pi/worktrees/"}
	if !slices.Equal(w.calls, want) {
		t.Fatalf("writes = %q, want %q", w.calls, want)
	}
}

func TestApplyLocalConfigAndExport(t *testing.T) {
	uc, w := applyFixture(t, fakeConfigFile{err: port.ErrConfigNotFound}, fakeConfigFile{err: port.ErrConfigNotFound, raw: []byte("{}")})
	plan, err := uc.Prepare([]byte(`{"config":{"languages":["go"]}}`), "/repo", ApplyLocal)
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
	plan, err := uc.Prepare([]byte(`{"config":{"languages":["go"]}}`), "/repo", ApplyCommitted)
	if err != nil || !strings.Contains(plan.Warning, "cannot be listed: bad json") {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
}

func TestApplyFailures(t *testing.T) {
	uc, w := applyFixture(t, fakeConfigFile{err: port.ErrConfigNotFound}, fakeConfigFile{rawErr: errors.New("no local config")})
	if _, err := uc.Prepare([]byte("not json"), "/repo", ApplyCommitted); err == nil {
		t.Fatal("an invalid proposal must be refused")
	}
	if _, err := uc.Export(ApplyLocal); err == nil {
		t.Fatal("exporting a missing config must fail")
	}
	plan, err := uc.Prepare([]byte(`{"config":{"languages":["go"]}}`), "/repo", ApplyCommitted)
	if err != nil {
		t.Fatal(err)
	}
	w.err = errors.New("read-only file system")
	if err := uc.Apply(plan); err == nil || len(w.calls) != 1 {
		t.Fatalf("a failed write must stop the apply: %v, calls %q", err, w.calls)
	}
}

func TestAgentsSection(t *testing.T) {
	cfg := resolvedWith(t, domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{
		{Paths: []string{"services/*/api"}, Languages: []string{"go"}, Conventions: []string{"clean-architecture"}},
		{Paths: []string{"scripts"}, Languages: []string{"shell"}},
	}})
	section := AgentsSection(cfg)
	for _, want := range []string{"`services/*/api`: go (clean-architecture); functions at most 50 lines", "`scripts`: shell; functions", "millennium-code-qc gate --tier full", "convention show <name>"} {
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
	plan, err := uc.Prepare([]byte(`{"approve":["express@4.21.2"]}`), "/repo", ApplyCommitted)
	if err != nil || plan.Config != nil || len(plan.Writes) != 1 || !strings.Contains(plan.Writes[0], "approves npm express@4.21.2 (vetted in the test)") {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	if err := uc.Apply(plan); err != nil || !slices.Equal(w.calls, []string{"write /repo/.millennium/deps/npm/express@4.21.2.json express@4.21.2"}) {
		t.Fatalf("writes = %q, %v", w.calls, err)
	}
	local, lw := applyFixture(t, fakeConfigFile{}, fakeConfigFile{err: port.ErrConfigNotFound})
	plan, err = local.Prepare([]byte(`{"config":{"languages":["go"]},"approve":["ms@2.0.0"]}`), "/repo", ApplyLocal)
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
	plan, err := uc.Prepare([]byte(`{"approve":["x@1.0.0"]}`), "/repo", ApplyCommitted)
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Apply(plan); err == nil || len(w.calls) != 0 {
		t.Fatalf("err = %v, writes %q", err, w.calls)
	}
}

func resolvedWith(t *testing.T, in domain.ConfigInput, project ...domain.Pack) domain.Config {
	t.Helper()
	cfg, err := domain.NewConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := testPacks().With(project)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := cfg.Resolve(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestApplyAddsAPackWithTheConfigThatUsesIt(t *testing.T) {
	uc, w := applyFixture(t, fakeConfigFile{err: port.ErrConfigNotFound}, fakeConfigFile{})
	plan, err := uc.Prepare([]byte(`{"packs":[{"name":"flat"}],"config":{"languages":["go"],"conventions":["flat"]}}`), "/repo", ApplyCommitted)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if plan.Config == nil || plan.Config.Packs["flat"].Name != "flat" || plan.Writes[0] != "/repo/.millennium/conventions/flat: convention flat (pack.json, guide.md)" {
		t.Fatalf("plan = %+v", plan)
	}
	if err := uc.Apply(plan); err != nil {
		t.Fatal(err)
	}
	want := []string{"write /repo/.millennium/conventions/flat/pack.json pack flat", "write /repo/.millennium/conventions/flat/guide.md Keep it flat.", "write /repo/.millennium/config.json go"}
	if !slices.Equal(w.calls[:3], want) {
		t.Fatalf("writes = %q", w.calls)
	}
}

func TestApplyReplacesAPackAndListsWhatItLoosens(t *testing.T) {
	current := resolvedWith(t, domain.ConfigInput{Version: ptr(1), Languages: []string{"go"}, Conventions: []string{"flat"}}, flatPack(t, 15))
	uc, w := applyFixture(t, fakeConfigFile{cfg: current, project: []domain.Pack{flatPack(t, 15)}}, fakeConfigFile{})
	plan, err := uc.Prepare([]byte(`{"packs":[{"name":"flat","cognitive":20}]}`), "/repo", ApplyCommitted)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !slices.Equal(plan.Loosenings, []string{"component .: the cognitive complexity limit rises from 15 to 20"}) ||
		plan.Canonical != nil || plan.Writes[len(plan.Writes)-1] != "AGENTS.md: the millennium section, with the packs' limits" {
		t.Fatalf("plan = %+v", plan)
	}
	if err := uc.Apply(plan); err != nil || w.calls[len(w.calls)-1] != "agents" || len(w.calls) != 3 {
		t.Fatalf("writes = %q, %v", w.calls, err)
	}
}

func TestApplyPackOnlyProposals(t *testing.T) {
	uc, w := applyFixture(t, fakeConfigFile{err: port.ErrConfigNotFound}, fakeConfigFile{cfg: resolvedWith(t, domain.ConfigInput{Version: ptr(1), Languages: []string{"go"}})})
	plan, err := uc.Prepare([]byte(`{"packs":[{"name":"flat"}]}`), "/repo", ApplyCommitted)
	if err != nil || plan.Config != nil || len(plan.Writes) != 1 {
		t.Fatalf("a pack with no config yet: %+v, %v", plan, err)
	}
	if err := uc.Apply(plan); err != nil || len(w.calls) != 2 {
		t.Fatalf("writes = %q, %v", w.calls, err)
	}
	local, lw := applyFixture(t, fakeConfigFile{}, fakeConfigFile{cfg: resolvedWith(t, domain.ConfigInput{Version: ptr(1), Languages: []string{"go"}})})
	plan, err = local.Prepare([]byte(`{"packs":[{"name":"flat"}]}`), "/repo", ApplyLocal)
	if err != nil || plan.Config == nil || !strings.HasPrefix(plan.Writes[0], "/home/u/.config/millennium/projects/k/conventions/flat") || len(plan.Writes) != 1 {
		t.Fatalf("a local pack: %+v, %v", plan, err)
	}
	if err := local.Apply(plan); err != nil || len(lw.calls) != 2 {
		t.Fatalf("a local pack writes only the pack: %q, %v", lw.calls, err)
	}
}

func TestApplyRefusesPacksItCannotCheck(t *testing.T) {
	tests := []struct {
		name      string
		committed fakeConfigFile
		proposal  string
		want      string
	}{
		{"built-in name", fakeConfigFile{err: port.ErrConfigNotFound}, `{"packs":[{"name":"clean-architecture"}]}`, "convention clean-architecture is built in"},
		{"unreadable packs", fakeConfigFile{packsErr: errors.New("bad pack")}, `{"packs":[{"name":"flat"}]}`, "the current convention packs cannot be read"},
		{"unreadable config", fakeConfigFile{err: errors.New("bad json")}, `{"packs":[{"name":"flat"}]}`, "the current config cannot be read, so the proposed packs cannot be checked"},
		{"unknown convention", fakeConfigFile{err: port.ErrConfigNotFound}, `{"config":{"languages":["go"],"conventions":["flat"]}}`, "with the proposed packs: invalid config: component .: convention flat does not exist"},
		{"bad pack", fakeConfigFile{err: port.ErrConfigNotFound}, `{"packs":[{"name":"flat","cognitive":0}]}`, "limits.cognitive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc, _ := applyFixture(t, tt.committed, fakeConfigFile{})
			if _, err := uc.Prepare([]byte(tt.proposal), "/repo", ApplyCommitted); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestApplyStopsWhenAPackCannotBeWritten(t *testing.T) {
	uc, w := applyFixture(t, fakeConfigFile{err: port.ErrConfigNotFound}, fakeConfigFile{})
	plan, err := uc.Prepare([]byte(`{"packs":[{"name":"flat"}]}`), "/repo", ApplyCommitted)
	if err != nil {
		t.Fatal(err)
	}
	w.err = errors.New("read-only file system")
	if err := uc.Apply(plan); err == nil || len(w.calls) != 1 {
		t.Fatalf("a failed pack write must stop the apply: %v, %q", err, w.calls)
	}
	w.calls, w.failAt = nil, 2
	if err := uc.Apply(plan); err == nil || len(w.calls) != 2 {
		t.Fatalf("a failed guide write must stop the apply: %v, %q", err, w.calls)
	}
}

func TestDigestCoversThePackFiles(t *testing.T) {
	pack := port.ProposedPack{Pack: flatPack(t, 15), Files: port.PackFiles{Pack: []byte("{}"), Guide: []byte("Keep it flat.")}}
	edited := pack
	edited.Files.Guide = []byte("Anything goes.")
	proposal := []byte(`{"packs":{"flat":"millennium.proposal/conventions/flat"}}`)
	if digest(proposal, []port.ProposedPack{pack}) == digest(proposal, []port.ProposedPack{edited}) {
		t.Fatal("a guide edited after the user read the plan must change the digest")
	}
}
