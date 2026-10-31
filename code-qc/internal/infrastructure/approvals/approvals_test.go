package approvals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"millennium-code-qc/internal/domain"
)

func writeRecord(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const expressRecord = `{"ecosystem":"npm","name":"express","version":"4.21.2","decision":"approved","reason":"HTTP routing for the identity API","checked":"2026-10-20","transitive":["ms@2.0.0"]}`

func TestApprovalsRoundTrip(t *testing.T) {
	committed, local := t.TempDir(), t.TempDir()
	writeRecord(t, committed, "npm/express@4.21.2.json", expressRecord)
	a, err := domain.NewApproval(domain.ApprovalInput{Ecosystem: "go", Name: "github.com/redis/go-redis/v9", Version: "v9.7.0",
		Decision: "approved", Reason: "Valkey client, pure Go, vetted", Checked: "2026-10-21"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := Format(a)
	if err != nil {
		t.Fatal(err)
	}
	writeRecord(t, local, domain.ApprovalFile(a.Package), string(data))
	all, err := New(committed, local, filepath.Join(t.TempDir(), "missing")).Approvals()
	if err != nil || len(all) != 2 {
		t.Fatalf("Approvals = %+v, %v", all, err)
	}
	if _, ok := all.Covering(domain.Package{Ecosystem: domain.EcosystemNPM, Name: "ms", Version: "2.0.0"}); !ok {
		t.Fatal("the transitive package must be covered")
	}
}

func TestApprovalsRejectBadRecords(t *testing.T) {
	tests := []struct {
		name, rel, content, want string
	}{
		{"misnamed", "npm/other@1.0.0.json", expressRecord, "must be named npm/express@4.21.2.json"},
		{"unknown field", "npm/express@4.21.2.json", strings.Replace(expressRecord, `"decision"`, `"extra":1,"decision"`, 1), "unknown field"},
		{"invalid", "npm/express@4.21.2.json", strings.Replace(expressRecord, `"approved"`, `"maybe"`, 1), "decision"},
		{"not json", "npm/express@4.21.2.json", "nope", "approval"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeRecord(t, dir, tt.rel, tt.content)
			if _, err := New(dir).Approvals(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}
