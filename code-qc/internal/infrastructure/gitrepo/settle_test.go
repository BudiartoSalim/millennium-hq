package gitrepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"millennium-code-qc/internal/domain"
)

func TestSandboxPlaceholder(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{".mcp.json", true},
		{".bashrc", true},
		{".idea", true},
		{".claude/agents", true},
		{".claude/launch.json", true},
		{".claude/agents/reviewer.md", false},
		{"services/api/.claude/commands", true},
		{"services/api/.mcp.json", true},
		{"services/api/.claude/agents/reviewer.md", false},
		{".npmrc", false},
		{"pkg/.golangci.yml", false},
		{".env", false},
		{"agents", false},
		{"go.mod", false},
		{"src/main.go", false},
	}
	for _, tt := range tests {
		if got := sandboxPlaceholder(tt.path); got != tt.want {
			t.Errorf("sandboxPlaceholder(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// Outside Claude Code's sandbox its placeholders are empty untracked files
// at protected paths; every other empty file is still part of the change,
// since an empty go.mod or .go file changes what builds.
func TestChangesLeaveOutOnlySandboxPlaceholders(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "main.go", "package main")
	commitAll(t, dir, "base")
	write(t, dir, ".mcp.json", "")
	write(t, dir, ".claude/agents", "")
	write(t, dir, "lib/broken.go", "")
	write(t, dir, "lib/.mcp.json", "")
	write(t, dir, "lib/.golangci.yml", "")
	write(t, dir, ".bashrc", "export A=1\n")

	got := changes(t, open(t, dir), "")
	assertChanges(t, got,
		domain.ChangedFile{Path: ".bashrc", Kind: domain.ChangeAdded},
		domain.ChangedFile{Path: "lib/.golangci.yml", Kind: domain.ChangeAdded},
		domain.ChangedFile{Path: "lib/broken.go", Kind: domain.ChangeAdded})
}

// A placeholder can also vanish between git listing it and the engine
// inspecting it. settle is called directly: that race cannot be staged with
// git.
func TestSettle(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "kept.go", "package main")
	write(t, dir, "empty.py", "")
	write(t, dir, ".mcp.json", "")
	repo := open(t, dir)
	added := func(p string) domain.ChangedFile { return domain.ChangedFile{Path: p, Kind: domain.ChangeAdded} }
	tests := []struct {
		name    string
		from    listing
		in      domain.ChangedFile
		want    []domain.ChangedFile
		wantErr string
	}{
		{name: "an untracked file with content is kept", from: listedUntracked, in: added("kept.go"),
			want: []domain.ChangedFile{added("kept.go")}},
		{name: "an empty untracked file is kept", from: listedUntracked, in: added("empty.py"),
			want: []domain.ChangedFile{added("empty.py")}},
		{name: "an empty untracked placeholder is left out", from: listedUntracked, in: added(".mcp.json")},
		{name: "a vanished untracked placeholder is left out", from: listedUntracked, in: added(".claude/agents")},
		{name: "a vanished untracked file blocks", from: listedUntracked, in: added("gone.go"),
			wantErr: "untracked file gone.go vanished while the gate ran"},
		{name: "a vanished untracked placeholder below the root is left out", from: listedUntracked, in: added("lib/.mcp.json")},
		{name: "a vanished untracked file in .claude/agents/ blocks", from: listedUntracked, in: added(".claude/agents/x.md"),
			wantErr: "untracked file .claude/agents/x.md vanished"},
		// Placeholders are never tracked, so the diff's paths get no
		// exemption, whatever their name.
		{name: "a listed empty file at a protected name is kept", from: listedByDiff, in: added(".mcp.json"),
			want: []domain.ChangedFile{added(".mcp.json")}},
		{name: "a listed vanished file at a protected name blocks", from: listedByDiff, in: added(".claude/agents"),
			wantErr: "changed file .claude/agents: git lists it as added"},
		{name: "a listed vanished modification blocks", from: listedByDiff, in: domain.ChangedFile{Path: "gone.go", Kind: domain.ChangeModified},
			wantErr: "changed file gone.go: git lists it as modified"},
		{name: "a listed deletion is kept without inspection", from: listedByDiff, in: domain.ChangedFile{Path: "old.go", Kind: domain.ChangeDeleted},
			want: []domain.ChangedFile{{Path: "old.go", Kind: domain.ChangeDeleted}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.settle([]domain.ChangedFile{tt.in}, tt.from)
			assertSettled(t, got, err, tt.want, tt.wantErr)
		})
	}
}

func assertSettled(t *testing.T, got []domain.ChangedFile, err error, want []domain.ChangedFile, wantErr string) {
	t.Helper()
	switch {
	case wantErr != "" && (err == nil || !strings.Contains(err.Error(), wantErr)):
		t.Fatalf("settle error = %v, want %q", err, wantErr)
	case wantErr == "" && err != nil:
		t.Fatalf("settle: %v", err)
	case wantErr == "":
		assertChanges(t, got, want...)
	}
}

// An audit lists what is there: an untracked placeholder is left out, any
// other empty file and a tracked one at a placeholder's name are audited,
// and a tracked file deleted from the working tree is skipped.
func TestFilesLeavesOutSandboxPlaceholders(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, ".gitmodules", "")
	write(t, dir, "deleted.go", "package main")
	commitAll(t, dir, "base")
	if err := os.Remove(filepath.Join(dir, "deleted.go")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, ".mcp.json", "")
	write(t, dir, "pkg/__init__.py", "")
	files, err := open(t, dir).Files(t.Context())
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if strings.Join(files, ",") != ".gitmodules,pkg/__init__.py" {
		t.Fatalf("Files = %v, want .gitmodules and pkg/__init__.py", files)
	}
}
