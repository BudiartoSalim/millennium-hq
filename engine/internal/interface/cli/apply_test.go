package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"millennium/internal/application/usecase"
	"millennium/internal/domain"
)

type fakeApplier struct {
	plan       usecase.ApplyPlan
	prepareErr error
	applyErr   error
	applied    bool
	gotTarget  usecase.ApplyTarget
	exported   []byte
}

func (f *fakeApplier) Prepare(_ []byte, target usecase.ApplyTarget) (usecase.ApplyPlan, error) {
	f.gotTarget = target
	return f.plan, f.prepareErr
}

func (f *fakeApplier) Apply(usecase.ApplyPlan) error {
	f.applied = true
	return f.applyErr
}

func (f *fakeApplier) Export(target usecase.ApplyTarget) ([]byte, error) {
	f.gotTarget = target
	return f.exported, f.applyErr
}

func applyHarness(t *testing.T, plan usecase.ApplyPlan, terminal bool) (*harness, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, usecase.ProposalFile), []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &harness{applier: &fakeApplier{plan: plan}, root: root, terminal: terminal}
	return h, root
}

var loosening = usecase.ApplyPlan{Digest: "abc123def456", Writes: []string{".millennium/config.json"}, Loosenings: []string{"language python is no longer checked"}, Canonical: []byte("{}\n"), Config: &domain.Config{}}

func TestApplyNeedsConfirmation(t *testing.T) {
	h, root := applyHarness(t, loosening, false)
	if code := h.run(t, "config", "apply"); code != 2 || h.applier.applied {
		t.Fatalf("without a terminal or digest nothing is applied: exit %d", code)
	}
	for _, want := range []string{"digest abc123def456", "LOOSENS the rules", "language python is no longer checked", "--confirm abc123def456"} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, h.stderr.String())
		}
	}
	if _, err := os.Stat(filepath.Join(root, usecase.ProposalFile)); err != nil {
		t.Fatal("an unapplied proposal must stay")
	}
}

func TestApplyWithTheDigest(t *testing.T) {
	h, root := applyHarness(t, loosening, false)
	if code := h.run(t, "config", "apply", "--confirm", "abc123def456"); code != 0 || !h.applier.applied {
		t.Fatalf("exit %d; stderr %s", code, h.stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, usecase.ProposalFile)); err == nil {
		t.Fatal("an applied default proposal is removed")
	}
	wrong, _ := applyHarness(t, loosening, false)
	if code := wrong.run(t, "config", "apply", "--confirm", "000000000000"); code != 2 || wrong.applier.applied || !strings.Contains(wrong.stderr.String(), "does not match") {
		t.Fatalf("a wrong digest: exit %d, stderr %s", code, wrong.stderr.String())
	}
}

func TestApplyAtATerminal(t *testing.T) {
	yes, _ := applyHarness(t, loosening, true)
	yes.stdin.WriteString("yes\n")
	if code := yes.run(t, "config", "apply"); code != 0 || !yes.applier.applied {
		t.Fatalf("typed yes: exit %d, stderr %s", code, yes.stderr.String())
	}
	no, _ := applyHarness(t, loosening, true)
	no.stdin.WriteString("y\n")
	if code := no.run(t, "config", "apply"); code != 2 || no.applier.applied {
		t.Fatalf("anything but yes refuses: exit %d", code)
	}
}

func TestApplyLocalFromAFileAndExport(t *testing.T) {
	h, root := applyHarness(t, usecase.ApplyPlan{Digest: "d", Warning: "the current config cannot be read"}, false)
	backup := filepath.Join(root, "backup.json")
	if err := os.WriteFile(backup, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := h.run(t, "config", "apply", "--local", "--confirm", "d", backup); code != 0 || h.applier.gotTarget != usecase.ApplyLocal {
		t.Fatalf("exit %d, target %v; stderr %s", code, h.applier.gotTarget, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "WARNING: the current config cannot be read") {
		t.Fatalf("stderr = %s", h.stderr.String())
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal("a named proposal file is left alone")
	}
	exp, _ := applyHarness(t, usecase.ApplyPlan{}, false)
	exp.applier.exported = []byte("{\"version\": 1}\n")
	if code := exp.run(t, "config", "export", "--local"); code != 0 || exp.stdout.String() != "{\"version\": 1}\n" || exp.applier.gotTarget != usecase.ApplyLocal {
		t.Fatalf("export: exit %d, stdout %q", code, exp.stdout.String())
	}
}

func TestApplyFailures(t *testing.T) {
	missing := &harness{applier: &fakeApplier{}, root: t.TempDir()}
	if code := missing.run(t, "config", "apply"); code != 2 || !strings.Contains(missing.stderr.String(), "read the proposal") {
		t.Fatalf("no proposal: exit %d", code)
	}
	invalid, _ := applyHarness(t, loosening, false)
	invalid.applier.prepareErr = errors.New("proposal: version 2 is not supported")
	if code := invalid.run(t, "config", "apply"); code != 2 || !strings.Contains(invalid.stderr.String(), "version 2") {
		t.Fatalf("invalid proposal: exit %d", code)
	}
	writeFails, _ := applyHarness(t, loosening, false)
	writeFails.applier.applyErr = errors.New("read-only file system")
	if code := writeFails.run(t, "config", "apply", "--confirm", "abc123def456"); code != 2 {
		t.Fatalf("failed write: exit %d", code)
	}
	exportFails := &harness{applier: &fakeApplier{applyErr: errors.New("no config")}}
	if code := exportFails.run(t, "config", "export"); code != 2 {
		t.Fatalf("failed export: exit %d", code)
	}
	for _, args := range [][]string{{"config", "apply", "a", "b"}, {"config", "export", "x"}, {"config", "apply", "--nope"}} {
		if code := (&harness{}).run(t, args...); code != 2 {
			t.Errorf("%q: exit %d", args, code)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	if isTerminal(strings.NewReader("")) {
		t.Fatal("a reader that is not a file is not a terminal")
	}
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Fatal("a regular file is not a terminal")
	}
}
