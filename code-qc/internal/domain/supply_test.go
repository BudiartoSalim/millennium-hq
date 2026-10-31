package domain

import (
	"slices"
	"strings"
	"testing"
)

func validApproval() ApprovalInput {
	return ApprovalInput{Ecosystem: "npm", Name: "@scope/pkg", Version: "1.2.3", Decision: "approved",
		Reason: "needed for OAuth flows; vetted below", Checked: "2026-10-20", Transitive: []string{"@scope/dep@2.0.0", "ms@2.1.3"}}
}

func TestNewApproval(t *testing.T) {
	a, err := NewApproval(validApproval())
	if err != nil || a.Key() != "@scope/pkg@1.2.3" || a.String() != "npm @scope/pkg@1.2.3" {
		t.Fatalf("approval = %+v, %v", a, err)
	}
	tests := []struct {
		name   string
		modify func(*ApprovalInput)
		want   string
	}{
		{"ecosystem", func(in *ApprovalInput) { in.Ecosystem = "cargo" }, `"cargo"`},
		{"name", func(in *ApprovalInput) { in.Name = "" }, "name and version"},
		{"decision", func(in *ApprovalInput) { in.Decision = "rejected" }, "decision"},
		{"reason", func(in *ApprovalInput) { in.Reason = "ok" }, "reason"},
		{"date", func(in *ApprovalInput) { in.Checked = "yesterday" }, "YYYY-MM-DD"},
		{"transitive", func(in *ApprovalInput) { in.Transitive = []string{"no-version"} }, `"no-version"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validApproval()
			tt.modify(&in)
			if _, err := NewApproval(in); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestApprovalsCovering(t *testing.T) {
	a, err := NewApproval(validApproval())
	if err != nil {
		t.Fatal(err)
	}
	approvals := Approvals{a}
	tests := []struct {
		p    Package
		want bool
	}{
		{Package{Ecosystem: EcosystemNPM, Name: "@scope/pkg", Version: "1.2.3"}, true},
		{Package{Ecosystem: EcosystemNPM, Name: "ms", Version: "2.1.3"}, true},
		{Package{Ecosystem: EcosystemNPM, Name: "ms", Version: "2.1.4"}, false},
		{Package{Ecosystem: EcosystemPyPI, Name: "ms", Version: "2.1.3"}, false},
	}
	for _, tt := range tests {
		if _, ok := approvals.Covering(tt.p); ok != tt.want {
			t.Errorf("Covering(%s) = %v, want %v", tt.p, ok, tt.want)
		}
	}
}

func TestApprovalFileAndKeys(t *testing.T) {
	if got := ApprovalFile(Package{Ecosystem: EcosystemGo, Name: "github.com/redis/go-redis/v9", Version: "v9.7.0"}); got != "go/github.com__redis__go-redis__v9@v9.7.0.json" {
		t.Fatalf("ApprovalFile = %q", got)
	}
	for key, want := range map[string]string{"@scope/pkg@1.0.0": "@scope/pkg", "express@4.21.2": "express"} {
		if name, _, ok := SplitKey(key); !ok || name != want {
			t.Errorf("SplitKey(%q) = %q, %v", key, name, ok)
		}
	}
	for _, bad := range []string{"@scope/pkg", "pkg@", "@1.0.0"} {
		if _, _, ok := SplitKey(bad); ok {
			t.Errorf("SplitKey(%q) accepted", bad)
		}
	}
}

func TestGoIntegrityProblems(t *testing.T) {
	if got := GoIntegrityProblems(map[string]string{"GOSUMDB": "sum.golang.org", "GONOSUMDB": "corp.example.com", "GOFLAGS": "-mod=readonly"}); len(got) != 0 {
		t.Fatalf("a normal environment has no problems: %q", got)
	}
	got := GoIntegrityProblems(map[string]string{"GOSUMDB": "off", "GONOSUMCHECK": "1", "GOINSECURE": "example.com", "GOFLAGS": "-mod=mod"})
	if len(got) != 4 {
		t.Fatalf("problems = %q", got)
	}
}

func TestNpmrcProblems(t *testing.T) {
	if got := NpmrcProblems("save-exact=true\nignore-scripts=true\nfund=false\n"); len(got) != 0 {
		t.Fatalf("problems = %q", got)
	}
	got := NpmrcProblems("# ignore-scripts=true\nsave-exact = false\n")
	if !slices.Equal(got, []string{"ignore-scripts=true, so installing a package cannot run its scripts", "save-exact=true, so new dependencies are pinned"}) {
		t.Fatalf("problems = %q", got)
	}
}
