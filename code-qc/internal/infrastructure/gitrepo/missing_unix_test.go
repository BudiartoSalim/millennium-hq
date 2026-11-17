//go:build unix

package gitrepo

import (
	"os"
	"path/filepath"
	"testing"
)

// A path that exists in some form, or that cannot be looked at, is never
// ErrNoDirectory, so callers do not walk past it. Unix only: Windows reports
// a non-directory parent as "path not found", and its permissions and
// symlinks differ; there the walk stops at the file, and git fails on it.
func TestOpenDoesNotReportAnUnusablePathAsMissing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permissions these cases rely on")
	}
	dir := newRepo(t)
	write(t, dir, "file.txt", "not a directory")
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "dangling")); err != nil {
		t.Fatal(err)
	}
	locked := lockedDir(t, dir)
	tests := []struct {
		name string
		path string
	}{
		{"below a file", filepath.Join(dir, "file.txt", "sub")},
		{"a dangling symlink", filepath.Join(dir, "dangling")},
		{"a directory git cannot enter", locked},
		{"below a directory that cannot be searched", filepath.Join(locked, "sub")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertOpenFailure(t, tt.path, false)
		})
	}
}

// lockedDir makes a directory nobody but root may enter, and unlocks it
// again so that t.TempDir can remove it.
func lockedDir(t *testing.T, parent string) string {
	t.Helper()
	locked := filepath.Join(parent, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(locked, 0o755); err != nil {
			t.Errorf("unlock %s: %v", locked, err)
		}
	})
	return locked
}
