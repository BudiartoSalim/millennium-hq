//go:build unix

package gitrepo

import (
	"os"
	"path/filepath"
	"testing"
)

// On the real file system, a protected location leads where a write lands:
// through a dangling symlink, a relative one included, to where its target
// would be; nowhere below a file; and to an error where a part may not be
// looked at.
func TestLocationOnTheFileSystem(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permissions the last case relies on")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locked := lockedDir(t, root)
	links := map[string]string{".pi": "conf", ".millennium": filepath.Join("..", "gone", "m2"), ".claude": filepath.Join(locked, "c")}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(root, "other")
	mkdir(t, other)
	writeFile(t, filepath.Join(other, ".claude"), "a file where the directory should be")
	tests := []struct{ root, protected, want, wantErr string }{
		{root, ".pi/extensions", filepath.Join(root, "conf", "extensions"), ""},
		{root, ".millennium", filepath.Join(filepath.Dir(root), "gone", "m2"), ""},
		{other, ".claude/settings.json", "", ""},
		{other, ".pi/extensions", filepath.Join(other, ".pi", "extensions"), ""},
		{root, ".claude/settings.json", "", "inspect " + filepath.Join(locked, "c")},
	}
	resolver := NewResolver()
	for _, tt := range tests {
		got, err := resolver.Location(tt.root, tt.protected)
		if got != tt.want || !errorMatches(err, tt.wantErr) {
			t.Errorf("Location(%q, %q) = %q, %v; want %q, error %q", tt.root, tt.protected, got, err, tt.want, tt.wantErr)
		}
	}
	if _, err := resolver.Resolve(filepath.Join(root, ".pi", "extensions", "x.ts")); !errorMatches(err, "which does not exist") {
		t.Fatalf("a tool's path through the dangling link: err = %v", err)
	}
}
