//go:build windows

package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// These tests run the walk on Windows' own file system: a directory
// junction, which `mklink /J` makes without any privilege, 8.3 short
// names, and a junction to a volume by its GUID, which os.Readlink gives
// as `\\?\Volume{GUID}\` since Go 1.23.

// longPath is p spelled by its long names, as Windows lists them.
func longPath(t *testing.T, p string) string {
	t.Helper()
	return pathCall(t, p, syscall.GetLongPathName)
}

// shortPath is p spelled by its 8.3 short names, where the volume has
// them.
func shortPath(t *testing.T, p string) string {
	t.Helper()
	return pathCall(t, p, syscall.GetShortPathName)
}

// pathCall is p respelled by call, GetLongPathName or GetShortPathName.
func pathCall(t *testing.T, p string, call func(*uint16, *uint16, uint32) (uint32, error)) string {
	t.Helper()
	in, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, syscall.MAX_LONG_PATH)
	n, err := call(in, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		t.Fatalf("respell %s: %d, %v", p, n, err)
	}
	return syscall.UTF16ToString(buf[:n])
}

// junction makes dir\name a directory junction to target.
func junction(t *testing.T, dir, name, target string) {
	t.Helper()
	// millennium:allow banned-construct -- mklink is a cmd built-in with no program of its own; the arguments are the test's own names and temporary paths
	cmd := exec.Command("cmd", "/c", "mklink", "/J", name, target)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s %s: %v\n%s", name, target, err, out)
	}
}

// tempRoot is a new directory, spelled by its long names.
func tempRoot(t *testing.T) string {
	t.Helper()
	return longPath(t, t.TempDir())
}

// A directory junction is a link the walk follows, for a protected
// location and for a tool's path, though Go reports it as neither a
// directory nor a symlink and filepath.EvalSymlinks walks through it.
func TestWalkFollowsAJunction(t *testing.T) {
	root := tempRoot(t)
	conf := filepath.Join(root, "conf")
	if err := os.Mkdir(conf, 0o755); err != nil {
		t.Fatal(err)
	}
	junction(t, root, ".pi", "conf")
	if kind, err := lstatKind(filepath.Join(root, ".pi")); kind != entryLink || err != nil {
		t.Fatalf("the junction's kind = %v, %v; want a link", kind, err)
	}
	resolver := NewResolver()
	if got, err := resolver.Location(root, ".pi/extensions"); !strings.EqualFold(got, filepath.Join(conf, "extensions")) || err != nil {
		t.Fatalf("Location(.pi/extensions) = %q, %v; want %q", got, err, filepath.Join(conf, "extensions"))
	}
	want := filepath.Join(conf, "extensions", "x.ts")
	if got, err := resolver.Resolve(filepath.Join(root, ".pi", "extensions", "x.ts")); !strings.EqualFold(got, want) || err != nil {
		t.Fatalf("Resolve through the junction = %q, %v; want %q", got, err, want)
	}
}

// A path named by its 8.3 short names is spelled by its long names, so a
// short name is no other spelling of a protected path; a part that does
// not exist yet keeps its name.
func TestNormalizeSpellsShortNamesLong(t *testing.T) {
	long := filepath.Join(tempRoot(t), "a long directory name")
	if err := os.Mkdir(long, 0o755); err != nil {
		t.Fatal(err)
	}
	short := shortPath(t, long)
	if strings.EqualFold(short, long) {
		t.Skip("this volume makes no 8.3 short names")
	}
	got, err := normalize(filepath.Join(short, "missing", "x.ts"))
	if want := filepath.Join(long, "missing", "x.ts"); !strings.EqualFold(got, want) || err != nil {
		t.Fatalf("normalize(%s) = %q, %v; want %q", short, got, err, want)
	}
	if got, err := NewResolver().Resolve(filepath.Join(short, "x.ts")); !strings.EqualFold(got, filepath.Join(long, "x.ts")) || err != nil {
		t.Fatalf("Resolve(%s) = %q, %v", short, got, err)
	}
}

// A junction to a volume by its GUID is followed from where Windows
// mounts the volume, which GetVolumePathNamesForVolumeNameW lists: here
// the volume of the temporary directory, at its drive letter.
func TestWalkFollowsAJunctionToAVolumeByItsGUID(t *testing.T) {
	root := tempRoot(t)
	conf := filepath.Join(root, "conf")
	if err := os.Mkdir(conf, 0o755); err != nil {
		t.Fatal(err)
	}
	volume := volumeOf(t, root)
	junction(t, root, ".pi", volume+strings.TrimPrefix(conf, filepath.VolumeName(conf)+`\`))
	target, err := os.Readlink(filepath.Join(root, ".pi"))
	if err != nil || !strings.HasPrefix(strings.ToLower(target), `\\?\volume{`) {
		t.Skipf("os.Readlink gives %q, %v for a junction to %s, not its GUID path, so there is nothing to map", target, err, volume)
	}
	mounts, err := volumePathNames(volume)
	if err != nil || len(mounts) == 0 {
		t.Fatalf("volumePathNames(%s) = %q, %v", volume, mounts, err)
	}
	if got, err := NewResolver().Location(root, ".pi/extensions"); !strings.EqualFold(got, filepath.Join(conf, "extensions")) || err != nil {
		t.Fatalf("Location(.pi/extensions) = %q, %v; want %q", got, err, filepath.Join(conf, "extensions"))
	}
}

// volumeOf is the GUID path of the volume dir lies on (`\\?\Volume{GUID}\`),
// as mountvol lists it for the drive.
func volumeOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("mountvol", filepath.VolumeName(dir)+`\`, "/L").CombinedOutput()
	volume := strings.TrimSpace(string(out))
	if err != nil || !strings.HasPrefix(volume, `\\?\Volume{`) {
		t.Skipf("mountvol cannot name the volume of %s: %v\n%s", dir, err, out)
	}
	return volume
}
