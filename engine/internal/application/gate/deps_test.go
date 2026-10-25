package gate

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// fakeManifests reads "pkg@version" lines as a lockfile named lock.txt, and
// "!rule message" lines in any file named manifest.txt as issues.
type fakeManifests struct{ err error }

func (f fakeManifests) Read(p string, content []byte) (port.ManifestFacts, bool, error) {
	switch {
	case strings.HasSuffix(p, "package-lock.json") || strings.HasSuffix(p, "go.sum"):
		if f.err != nil || string(content) == "BROKEN" {
			return port.ManifestFacts{}, true, errors.New("unreadable lockfile")
		}
		facts := port.ManifestFacts{Lockfile: true}
		for _, line := range strings.Fields(string(content)) {
			name, version, _ := domain.SplitKey(strings.TrimSuffix(line, "!"))
			eco := domain.EcosystemNPM
			if strings.HasSuffix(p, "go.sum") {
				eco = domain.EcosystemGo
			}
			facts.Packages = append(facts.Packages, domain.LockedPackage{Package: domain.Package{Ecosystem: eco, Name: name, Version: version}, InstallScript: strings.HasSuffix(line, "!")})
		}
		return facts, true, nil
	case strings.HasSuffix(p, "Dockerfile"):
		return port.ManifestFacts{Issues: []domain.LintIssue{{Line: 1, Code: "unpinned-image", Message: "node:22 has no digest"}, {Line: 9, Code: "unpinned-image", Message: "untouched"}}}, true, nil
	}
	return port.ManifestFacts{}, false, nil
}

type fakeApprovals struct {
	approvals domain.Approvals
	err       error
}

func (f fakeApprovals) Approvals() (domain.Approvals, error) { return f.approvals, f.err }

type fakeRegistry struct {
	published map[string]time.Time
	vulns     map[string][]string
	err       error
	vulnErr   error
	queried   []string
}

func (f *fakeRegistry) PublishedAt(_ context.Context, p domain.Package) (time.Time, error) {
	if f.err != nil {
		return time.Time{}, f.err
	}
	return f.published[p.Key()], nil
}

func (f *fakeRegistry) Vulnerabilities(_ context.Context, packages []domain.Package) (map[string][]string, error) {
	for _, p := range packages {
		f.queried = append(f.queried, p.Key())
	}
	return f.vulns, f.vulnErr
}

type fakeIntegrity struct {
	env    map[string]string
	envErr error
	sigErr error
}

func (f fakeIntegrity) GoEnv(context.Context, string) (map[string]string, error) {
	return f.env, f.envErr
}
func (f fakeIntegrity) NpmAuditSignatures(context.Context, string) error { return f.sigErr }

var now = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func approval(t *testing.T, eco, key string, transitive ...string) domain.Approval {
	t.Helper()
	name, version, _ := domain.SplitKey(key)
	a, err := domain.NewApproval(domain.ApprovalInput{Ecosystem: eco, Name: name, Version: version, Decision: "approved",
		Reason: "vetted for the test suite", Checked: "2026-10-01", Transitive: transitive})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

type depsFixture struct {
	source    *fakeSource
	manifests fakeManifests
	approvals fakeApprovals
	registry  *fakeRegistry
	integrity fakeIntegrity
}

func (f depsFixture) run(t *testing.T, tier domain.Tier, base string, files ...domain.ChangedFile) []domain.Finding {
	t.Helper()
	if f.registry == nil {
		f.registry = &fakeRegistry{}
	}
	g := NewDeps(f.source, f.manifests, f.approvals, f.registry, f.integrity, func() time.Time { return now })
	findings, err := g.Run(t.Context(), port.GateInput{Changes: domain.ChangeSet{Base: base, Files: files}, Tier: tier})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return findings
}

const safeNpmrc = "ignore-scripts=true\nsave-exact=true\n"

func TestDepsApprovalsAndInstallScripts(t *testing.T) {
	f := depsFixture{
		source: &fakeSource{
			working: map[string]string{"web/package-lock.json": "express@4.21.2 ms@2.0.0 left-pad@1.3.0 husky@9.0.0!", "web/.npmrc": safeNpmrc},
			base:    map[string]string{"web/package-lock.json": "left-pad@1.3.0"},
		},
		approvals: fakeApprovals{approvals: domain.Approvals{approval(t, "npm", "express@4.21.2", "ms@2.0.0")}},
	}
	findings := f.run(t, domain.TierFast, "tree", modified("web/package-lock.json"))
	got := rules(findings)
	if want := []string{"deps/unapproved-dependency", "deps/install-script"}; !slices.Equal(got, want) {
		t.Fatalf("findings %q, want %q (express and ms are approved, left-pad was already locked)", got, want)
	}
	if !strings.Contains(findings[0].Message, "npm husky@9.0.0 has no approval record") {
		t.Fatalf("message = %q", findings[0].Message)
	}
}

func TestDepsPinningOnTouchedLinesAndNpmrc(t *testing.T) {
	f := depsFixture{source: &fakeSource{
		working: map[string]string{"Dockerfile": "x", "web/package-lock.json": "", "web/.npmrc": "save-exact=true\n"},
		changed: map[string]domain.LineSet{"Dockerfile": {Ranges: []domain.LineRange{{First: 1, Last: 1}}}},
	}}
	findings := f.run(t, domain.TierFast, "tree", modified("Dockerfile"), added("web/package-lock.json"))
	got := rules(findings)
	if want := []string{"deps/unpinned-image", "deps/npmrc-unsafe"}; !slices.Equal(got, want) {
		t.Fatalf("findings %q, want %q (line 9 of the Dockerfile is untouched)", got, want)
	}
	if !strings.Contains(findings[1].Message, "ignore-scripts=true") || findings[1].File != "web/.npmrc" {
		t.Fatalf("npmrc finding = %+v", findings[1])
	}
}

func TestDepsFullTierAsksTheRegistries(t *testing.T) {
	registry := &fakeRegistry{
		published: map[string]time.Time{"fresh@1.0.0": now.Add(-48 * time.Hour), "patched@2.0.0": now.Add(-24 * time.Hour), "old@1.0.0": now.Add(-90 * 24 * time.Hour)},
		vulns:     map[string][]string{"npm old@1.0.0": {"GHSA-1"}},
	}
	patched := approval(t, "npm", "patched@2.0.0")
	patched.SecurityPatch = "GHSA-2 fixed in 2.0.0"
	f := depsFixture{
		source:    &fakeSource{working: map[string]string{"package-lock.json": "fresh@1.0.0 patched@2.0.0 old@1.0.0", ".npmrc": safeNpmrc, "go.sum": "example.com/m@v1.0.0"}},
		approvals: fakeApprovals{approvals: domain.Approvals{approval(t, "npm", "fresh@1.0.0"), patched, approval(t, "npm", "old@1.0.0"), approval(t, "go", "example.com/m@v1.0.0")}},
		registry:  registry,
		integrity: fakeIntegrity{env: map[string]string{"GOSUMDB": "off"}, sigErr: errors.New("1 package has an invalid signature")},
	}
	findings := f.run(t, domain.TierFull, "", added("package-lock.json"), added("go.sum"))
	got := rules(findings)
	want := []string{"deps/too-new", "deps/known-vulnerability", "deps/integrity-weakened", "deps/signatures-failed"}
	if !slices.Equal(got, want) {
		t.Fatalf("findings %q, want %q", got, want)
	}
	if !strings.Contains(findings[0].Message, "npm fresh@1.0.0 was released 48h0m0s ago") {
		t.Fatalf("too-new message = %q", findings[0].Message)
	}
	if len(registry.queried) != 4 {
		t.Fatalf("OSV must be asked about every locked package, asked %q", registry.queried)
	}
}

func TestDepsFastTierSkipsTheNetwork(t *testing.T) {
	registry := &fakeRegistry{err: errors.New("no network")}
	f := depsFixture{
		source:    &fakeSource{working: map[string]string{"package-lock.json": "a@1.0.0", ".npmrc": safeNpmrc}},
		approvals: fakeApprovals{approvals: domain.Approvals{approval(t, "npm", "a@1.0.0")}},
		registry:  registry,
	}
	if findings := f.run(t, domain.TierFast, "", added("package-lock.json")); len(findings) != 0 || len(registry.queried) != 0 {
		t.Fatalf("findings %+v, queried %q", findings, registry.queried)
	}
}

func TestDepsFailuresBlock(t *testing.T) {
	lock := map[string]string{"package-lock.json": "a@1.0.0", ".npmrc": safeNpmrc}
	tests := []struct {
		name string
		f    depsFixture
		tier domain.Tier
		want string
	}{
		{"unreadable manifest", depsFixture{source: &fakeSource{working: lock}, manifests: fakeManifests{err: errors.New("lockfileVersion 1")}}, domain.TierFast, "deps/manifest-unreadable"},
		{"invalid approval", depsFixture{source: &fakeSource{working: lock}, approvals: fakeApprovals{err: errors.New("misnamed")}}, domain.TierFast, "deps/invalid-approval"},
		{"registry down", depsFixture{source: &fakeSource{working: lock}, approvals: fakeApprovals{approvals: domain.Approvals{approval(t, "npm", "a@1.0.0")}},
			registry: &fakeRegistry{err: errors.New("timeout"), vulnErr: errors.New("timeout")}}, domain.TierFull, "deps/registry-failed"},
		{"go env fails", depsFixture{source: &fakeSource{working: map[string]string{"go.sum": ""}}, integrity: fakeIntegrity{envErr: errors.New("go missing")}}, domain.TierFull, "deps/registry-failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := added("package-lock.json")
			if strings.HasPrefix(tt.name, "go") {
				file = added("go.sum")
			}
			got := rules(tt.f.run(t, tt.tier, "", file))
			if !slices.Contains(got, tt.want) {
				t.Fatalf("findings %q, want %s", got, tt.want)
			}
		})
	}
}

func TestDepsIgnoresWhatIsNotAManifest(t *testing.T) {
	f := depsFixture{source: &fakeSource{working: map[string]string{"main.go": "package main"}}}
	findings := f.run(t, domain.TierFull, "", added("main.go"), added("unreadable.go"), domain.ChangedFile{Path: "old/package-lock.json", Kind: domain.ChangeDeleted})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDepsBaseEdges(t *testing.T) {
	f := depsFixture{source: &fakeSource{working: map[string]string{"package-lock.json": "a@1.0.0", ".npmrc": safeNpmrc}, readAtErr: errInjected}}
	g := NewDeps(f.source, f.manifests, f.approvals, &fakeRegistry{}, f.integrity, func() time.Time { return now })
	if _, err := g.Run(t.Context(), port.GateInput{Changes: domain.ChangeSet{Base: "tree", Files: []domain.ChangedFile{modified("package-lock.json")}}}); !errors.Is(err, errInjected) {
		t.Fatalf("a base read failure must fail the gate: %v", err)
	}
	f.source.readAtErr = nil
	f.source.changedErr = errInjected
	if _, err := g.Run(t.Context(), port.GateInput{Changes: domain.ChangeSet{Base: "tree", Files: []domain.ChangedFile{modified("package-lock.json")}}}); !errors.Is(err, errInjected) {
		t.Fatalf("a changed-lines failure must fail the gate: %v", err)
	}
}

func TestDepsTreatsAnUnreadableBaseLockfileAsEmpty(t *testing.T) {
	f := depsFixture{source: &fakeSource{
		working: map[string]string{"package-lock.json": "a@1.0.0", ".npmrc": safeNpmrc},
		base:    map[string]string{"package-lock.json": "BROKEN"},
	}}
	if got := rules(f.run(t, domain.TierFast, "tree", modified("package-lock.json"))); !slices.Equal(got, []string{"deps/unapproved-dependency"}) {
		t.Fatalf("every package is held to the rules when the old lockfile cannot be read: %q", got)
	}
}
