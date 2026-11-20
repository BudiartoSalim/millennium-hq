//go:build unix

package gitrepo

import (
	"os"
	"path/filepath"
	"strings"
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

// perProcessUnknown is the error of a walk through a per-process entry.
const perProcessUnknown = "it leads somewhere else for each process that opens it"

// The per-process entries lead somewhere else for each process that opens
// a path through them, so the engine, in its own process, cannot tell
// where they lead for pi or a tool: a walk through one is unknown, for a
// protected location as for a tool's path, however it gets there. Before,
// `.pi` a link to /proc/self/cwd/conf was resolved in the engine's
// working directory, and a write of conf/extensions/x.ts passed.
func TestWalkRefusesPerProcessPaths(t *testing.T) {
	root := t.TempDir()
	at := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	dir, file := fakeEntry{kind: entryDir}, fakeEntry{kind: entryFile}
	link := func(target string) fakeEntry { return fakeEntry{kind: entryLink, target: target} }
	system := func(pi string) map[string]fakeEntry {
		return map[string]fakeEntry{"/proc": dir, "/dev": dir, "/proc/cpuinfo": file, at(".pi"): link(pi)}
	}
	refused := func(entry string) string { return "resolve " + entry + ": " + perProcessUnknown }
	tests := []walkCase{
		{name: "the process's own directory", entries: system("/proc/self/cwd/conf"), rel: ".pi/extensions", wantErr: refused("/proc/self")},
		{name: "the thread's", entries: system("/proc/thread-self/cwd"), rel: ".pi/extensions", wantErr: refused("/proc/thread-self")},
		{name: "a process by its id", entries: system("/proc/4242/root/conf"), rel: ".pi/extensions", wantErr: refused("/proc/4242")},
		{name: "an open file", entries: system("/dev/fd/3"), rel: ".pi/extensions", wantErr: refused("/dev/fd")},
		{name: "standard input", entries: system("/dev/stdin"), rel: ".pi/settings.json", wantErr: refused("/dev/stdin")},
		{name: "standard output", entries: system("/dev/stdout"), rel: ".pi/settings.json", wantErr: refused("/dev/stdout")},
		{name: "standard error", entries: system("/dev/stderr"), rel: ".pi/settings.json", wantErr: refused("/dev/stderr")},
		{name: "reached through '..'", entries: system(strings.Repeat("../", strings.Count(root, "/")) + "proc/self/cwd"), rel: ".pi/extensions", wantErr: refused("/proc/self")},
		{name: "through another link", entries: map[string]fakeEntry{"/proc": dir, at(".pi"): link("l"), at("l"): link("/proc/self/cwd")}, rel: ".pi/extensions", wantErr: refused("/proc/self")},
		{name: "another entry of /proc", entries: system("/proc/cpuinfo"), rel: ".pi/extensions", resolveErr: "not a directory"},
	}
	runWalkCases(t, root, tests)
	if _, err := resolvePath(fakeFiles{root: root, entries: system("x")}, "/proc/self/cwd/conf/extensions/x.ts"); !errorMatches(err, refused("/proc/self")) {
		t.Fatalf("a tool's path through /proc/self: err = %v", err)
	}
}

// Only the per-process entries themselves are refused: what lies below
// one is reached through it.
func TestPerProcess(t *testing.T) {
	tests := map[string]bool{
		"/proc/self": true, "/proc/thread-self": true, "/proc/1": true, "/proc/4242": true,
		"/dev/fd": true, "/dev/stdin": true, "/dev/stdout": true, "/dev/stderr": true,
		"/proc": false, "/proc/": false, "/proc/1x": false, "/proc/cpuinfo": false, "/proc/selfish": false,
		"/dev": false, "/dev/null": false, "/dev/fdx": false, "/repo/proc/self": false, "/repo/dev/fd": false,
	}
	for p, want := range tests {
		if got := perProcess(p); got != want {
			t.Errorf("perProcess(%q) = %v, want %v", p, got, want)
		}
	}
}

// On the real file system, a protected location through a link loop
// reaches nothing, since the OS opens nothing through it, while a tool's
// path through one is unknown; a location through a per-process entry is
// unknown.
func TestLocationOnTheFileSystemThroughLoopsAndPerProcessEntries(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{".pi": ".pi", ".claude": "/dev/fd/0/c"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	resolver := NewResolver()
	if got, err := resolver.Location(root, ".pi/extensions"); got != "" || err != nil {
		t.Fatalf("Location through a loop = %q, %v; want nothing", got, err)
	}
	if _, err := resolver.Resolve(filepath.Join(root, ".pi", "extensions", "x.ts")); !errorMatches(err, tooManyLinks) {
		t.Fatalf("a tool's path through the loop: err = %v", err)
	}
	if _, err := resolver.Location(root, ".claude/settings.json"); !errorMatches(err, "resolve /dev/fd: "+perProcessUnknown) {
		t.Fatalf("Location through /dev/fd: err = %v", err)
	}
}
