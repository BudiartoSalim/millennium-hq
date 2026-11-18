package gitrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
	"unicode/utf16"
)

// Go reports a Windows directory junction as neither a directory nor a
// symlink, with the directory and reparse-point attributes set; it is a
// link to follow. A reparse point that does not redirect a name, such as a
// cloud-file placeholder, keeps its mode and is what it looks like.
func TestClassify(t *testing.T) {
	const dirAttrs, reparse = win32AttributeDirectory, win32AttributeReparsePoint
	tests := []struct {
		name  string
		mode  fs.FileMode
		attrs uint32
		want  entryKind
	}{
		{"a file", 0o644, 0, entryFile},
		{"a directory", fs.ModeDir | 0o755, 0, entryDir},
		{"a symlink", fs.ModeSymlink | 0o777, 0, entryLink},
		{"a device", fs.ModeDevice | fs.ModeCharDevice, 0, entryFile},
		{"a Windows directory", fs.ModeDir | 0o777, dirAttrs, entryDir},
		{"a Windows directory symlink", fs.ModeSymlink | 0o666, dirAttrs | reparse, entryLink},
		{"a Windows directory junction", fs.ModeIrregular | 0o666, dirAttrs | reparse, entryLink},
		{"a Windows cloud-file directory", fs.ModeDir | fs.ModeIrregular | 0o777, dirAttrs | reparse, entryDir},
		{"a Windows cloud-file placeholder", fs.ModeIrregular | 0o666, reparse, entryFile},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classify(tt.mode, tt.attrs); got != tt.want {
				t.Fatalf("classify(%v, %#x) = %v, want %v", tt.mode, tt.attrs, got, tt.want)
			}
		})
	}
}

// fakeEntry is one entry of a fakeFiles: its kind, and for a link where it
// leads or why that cannot be read.
type fakeEntry struct {
	kind    entryKind
	target  string
	readErr error
}

// fakeFiles is a file system of the entries it holds, by path, below
// root, which is a directory, as its parents are; any other path is
// missing, and a path in errs cannot be looked at. Windows mounts each
// volume in volumes at the paths listed, and none when volumeErr is set.
type fakeFiles struct {
	root      string
	entries   map[string]fakeEntry
	errs      map[string]error
	volumes   map[string][]string
	volumeErr error
}

func (f fakeFiles) volumePaths(volume string) ([]string, error) {
	return f.volumes[volume], f.volumeErr
}

func (f fakeFiles) kind(p string) (entryKind, error) {
	if err := f.errs[p]; err != nil {
		return 0, err
	}
	if rel, err := filepath.Rel(p, f.root); err == nil && (rel == "." || filepath.IsLocal(rel)) {
		return entryDir, nil
	}
	return f.entries[p].kind, nil
}

func (f fakeFiles) readlink(p string) (string, error) {
	e := f.entries[p]
	return e.target, e.readErr
}

// walkCase is one walk over a fakeFiles below root.
type walkCase struct {
	name       string
	entries    map[string]fakeEntry
	errs       map[string]error
	volumes    map[string][]string
	volumeErr  error
	rel        string // slash-separated, below root
	want       string // below root, slash-separated; "" for nothing
	wantErr    string
	resolveErr string // Resolve's, which allows no dangling link nor a part below a file; "" when it agrees
}

// reachesNothing reports whether err says, as callers outside this package
// read it, that its path reaches nothing.
func reachesNothing(err error) bool {
	var nothing interface{ ReachesNothing() bool }
	return errors.As(err, &nothing) && nothing.ReachesNothing()
}

// runWalkCases walks each case's rel below root, as a protected location
// and as a tool's path.
func runWalkCases(t *testing.T, root string, tests []walkCase) {
	t.Helper()
	at := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := fakeFiles{root: root, entries: tt.entries, errs: tt.errs, volumes: tt.volumes, volumeErr: tt.volumeErr}
			want := ""
			if tt.want != "" {
				want = at(tt.want)
			}
			got, err := location(files, root, tt.rel)
			if got != want || !errorMatches(err, tt.wantErr) {
				t.Fatalf("location(%q) = %q, %v; want %q, error %q", tt.rel, got, err, want, tt.wantErr)
			}
			assertResolves(t, files, at(tt.rel), want, tt)
		})
	}
}

// assertResolves checks resolvePath of abs against a case whose location
// is want: the same, unless the case gives Resolve its own error.
// Resolve's own errors are where the path reaches nothing; the others
// leave where it leads unknown.
func assertResolves(t *testing.T, files fakeFiles, abs, want string, tt walkCase) {
	t.Helper()
	resolveWant, resolveErr := want, tt.wantErr
	if tt.resolveErr != "" {
		resolveWant, resolveErr = "", tt.resolveErr
	}
	got, err := resolvePath(files, abs)
	if got != resolveWant || !errorMatches(err, resolveErr) {
		t.Fatalf("resolvePath(%q) = %q, %v; want %q, error %q", abs, got, err, resolveWant, resolveErr)
	}
	if nothing := reachesNothing(err); nothing != (tt.resolveErr != "") {
		t.Fatalf("resolvePath(%q): error %v reaches nothing = %v, want %v", abs, err, nothing, tt.resolveErr != "")
	}
}

// chain is n links one after another, from c0 to the directory end.
func chain(at func(string) string, n int) map[string]fakeEntry {
	entries := map[string]fakeEntry{at("end"): {kind: entryDir}}
	for i := range n {
		target := fmt.Sprintf("c%d", i+1)
		if i == n-1 {
			target = "end"
		}
		entries[at(fmt.Sprintf("c%d", i))] = fakeEntry{kind: entryLink, target: target}
	}
	return entries
}

// The walk follows every link itself, a Windows junction included, and
// blocks on a link it cannot read. A protected location (Location) follows
// a link that leads nowhere as it reads, and reaches nothing below a file
// or through more links than the OS follows (a loop), since the OS opens
// nothing there; a tool's path (Resolve) is unknown there.
func TestWalkFollowsEveryLink(t *testing.T) {
	root := t.TempDir()
	at := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	dir, file := fakeEntry{kind: entryDir}, fakeEntry{kind: entryFile}
	link := func(target string) fakeEntry { return fakeEntry{kind: entryLink, target: target} }
	tests := []walkCase{
		{name: "nothing there", rel: ".pi/extensions", want: ".pi/extensions"},
		{name: "a directory", entries: map[string]fakeEntry{at(".pi"): dir, at(".pi/extensions"): dir}, rel: ".pi/extensions", want: ".pi/extensions"},
		{name: "a relative link, or a junction", entries: map[string]fakeEntry{at(".pi"): link("conf"), at("conf"): dir}, rel: ".pi/extensions", want: "conf/extensions"},
		{name: "an absolute link", entries: map[string]fakeEntry{at(".pi"): link(at("conf")), at("conf"): dir}, rel: ".pi/extensions", want: "conf/extensions"},
		{name: "a link with '..' and '.'", entries: map[string]fakeEntry{at("a"): dir, at("a/.pi"): link("./../conf"), at("conf"): dir}, rel: "a/.pi/settings.json", want: "conf/settings.json"},
		{name: "links one after another", entries: map[string]fakeEntry{at(".pi"): link("l1"), at("l1"): link("l2"), at("l2"): dir}, rel: ".pi/extensions", want: "l2/extensions"},
		{name: "a dangling link", entries: map[string]fakeEntry{at(".pi"): link("conf")}, rel: ".pi/extensions", want: "conf/extensions", resolveErr: "resolve " + at(".pi") + ": it leads to " + at("conf") + ", which does not exist"},
		{name: "a dangling link as the location", entries: map[string]fakeEntry{at(".millennium"): link("m2")}, rel: ".millennium", want: "m2", resolveErr: "which does not exist"},
		{name: "a link through a dangling link", entries: map[string]fakeEntry{at(".pi"): link("a/b"), at("a"): link("nowhere")}, rel: ".pi/extensions", want: "nowhere/b/extensions", resolveErr: "resolve " + at("a")},
		{name: "a file in place of a directory", entries: map[string]fakeEntry{at(".claude"): file}, rel: ".claude/settings.json", resolveErr: "inspect " + at(".claude/settings.json") + ": not a directory"},
		{name: "a link to a file", entries: map[string]fakeEntry{at(".pi"): link("f"), at("f"): file}, rel: ".pi/extensions", resolveErr: "not a directory"},
		{name: "the file itself", entries: map[string]fakeEntry{at(".claude"): dir, at(".claude/settings.json"): file}, rel: ".claude/settings.json", want: ".claude/settings.json"},
		{name: "a part that cannot be looked at", errs: map[string]error{at(".pi"): errors.New("permission denied")}, rel: ".pi/extensions", wantErr: "inspect " + at(".pi") + ": permission denied"},
		{name: "a link that cannot be read, such as an unknown reparse point", entries: map[string]fakeEntry{at(".pi"): {kind: entryLink, readErr: errors.New("The system cannot find the file specified.")}},
			rel: ".pi/extensions", wantErr: "whose target cannot be read, so where a path through it leads is unknown: The system cannot find"},
		{name: "a loop reaches nothing", entries: map[string]fakeEntry{at(".pi"): link(".pi")}, rel: ".pi/extensions", resolveErr: tooManyLinks},
		{name: "a loop through another link", entries: map[string]fakeEntry{at(".claude"): link("x"), at("x"): link(".claude")}, rel: ".claude/settings.json", resolveErr: tooManyLinks},
		{name: "as many links as the OS follows", entries: chain(at, maxLinks), rel: "c0/extensions", want: "end/extensions"},
		{name: "one more than the OS follows", entries: chain(at, maxLinks+1), rel: "c0/extensions", resolveErr: tooManyLinks},
		{name: "a link to an NT path", entries: map[string]fakeEntry{at(".pi"): link(`\??\C:\conf`)}, rel: ".pi/extensions", wantErr: "is a device path"},
		{name: "a link to a device path that is no volume", entries: map[string]fakeEntry{at(".pi"): link(`\\?\C:\conf`)}, rel: ".pi/extensions", wantErr: "is a device path"},
		{name: "a link with an empty target", entries: map[string]fakeEntry{at(".pi"): link("")}, rel: ".pi/extensions", wantErr: "its target is empty"},
	}
	runWalkCases(t, root, tests)
}

// tooManyLinks is the error of a walk through more links than the OS
// follows.
var tooManyLinks = fmt.Sprintf("more than %d links one after another", maxLinks)

// Since Go 1.23, os.Readlink on Windows gives a volume mount point's
// target, and a junction's to a volume, as the volume's GUID path
// (`\\?\Volume{GUID}\`). The walk goes on from where Windows mounts the
// volume: the mount point itself when it is one of the volume's paths, so
// a repository below C:\dev keeps its spelling, and otherwise the first
// path listed. A volume Windows lists no path for, or whose paths cannot
// be listed, is unknown.
func TestWalkFollowsVolumesWhereWindowsMountsThem(t *testing.T) {
	root := t.TempDir()
	at := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	sep := string(filepath.Separator)
	dir := fakeEntry{kind: entryDir}
	link := func(target string) fakeEntry { return fakeEntry{kind: entryLink, target: target} }
	const volume = `\\?\Volume{0a1b}\`
	mountedAtDev := map[string][]string{volume: {at("e") + sep, at("dev") + sep}}
	tests := []walkCase{
		{name: "a folder the volume is mounted at", entries: map[string]fakeEntry{at("dev"): link(volume), at("dev/repo"): dir}, volumes: mountedAtDev,
			rel: "dev/repo/.pi/extensions", want: "dev/repo/.pi/extensions"},
		{name: "a mount point named in another case", entries: map[string]fakeEntry{at("Dev"): link(volume)}, volumes: map[string][]string{volume: {at("dev") + sep}},
			rel: "Dev/.pi", want: "Dev/.pi"},
		{name: "a junction into a volume", entries: map[string]fakeEntry{at(".pi"): link(volume + "conf"), at("e"): dir, at("e/conf"): dir}, volumes: mountedAtDev,
			rel: ".pi/extensions", want: "e/conf/extensions"},
		{name: "a junction to a volume's root", entries: map[string]fakeEntry{at(".pi"): link(`\\?\volume{0a1b}`), at("e"): dir}, volumes: map[string][]string{`\\?\volume{0a1b}\`: {at("e") + sep}},
			rel: ".pi/extensions", want: "e/extensions"},
		{name: "a volume mounted nowhere", entries: map[string]fakeEntry{at(".pi"): link(volume + "conf")}, rel: ".pi/extensions",
			wantErr: "is a volume with no drive letter or mounted folder"},
		{name: "a volume whose paths cannot be listed", entries: map[string]fakeEntry{at("dev"): link(volume)}, volumeErr: errors.New("access is denied"), rel: "dev/x",
			wantErr: "resolve " + at("dev") + ": its target " + volume + " is a volume whose mount paths cannot be read, so no other spelling of its files is known: access is denied"},
		{name: "a volume mounted at a relative path", entries: map[string]fakeEntry{at(".pi"): link(volume)}, volumes: map[string][]string{volume: {"e" + sep}}, rel: ".pi/extensions",
			wantErr: "which is not an absolute path"},
		{name: "a GUID path with more after the brace", entries: map[string]fakeEntry{at(".pi"): link(`\\?\Volume{0a1b}x`)}, volumes: mountedAtDev, rel: ".pi/extensions",
			wantErr: "is a device path"},
		{name: "a GUID path without a closing brace", entries: map[string]fakeEntry{at(".pi"): link(`\\?\Volume{0a1b`)}, volumes: mountedAtDev, rel: ".pi/extensions",
			wantErr: "is a device path"},
	}
	runWalkCases(t, root, tests)
}

// A Windows multi-string is a list of NUL-ended strings ended by an empty
// one, as GetVolumePathNamesForVolumeNameW gives a volume's paths.
func TestMultiString(t *testing.T) {
	encode := func(s string) []uint16 { return utf16.Encode([]rune(s)) }
	tests := []struct {
		name string
		buf  []uint16
		want []string
	}{
		{"none", encode("\x00\x00"), nil},
		{"one", encode("E:\\\x00\x00"), []string{`E:\`}},
		{"several, after the end too", encode("E:\\\x00C:\\dev\\\x00\x00junk\x00"), []string{`E:\`, `C:\dev\`}},
		{"beyond the basic plane", encode("C:\\𝄞\x00\x00"), []string{`C:\𝄞`}},
		{"cut off", encode("E:\\\x00C:\\de"), []string{`E:\`, `C:\de`}},
		{"empty", nil, nil},
	}
	for _, tt := range tests {
		if got := multiString(tt.buf); !slices.Equal(got, tt.want) {
			t.Errorf("%s: multiString = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// A walk starts from the volume's root, and only an absolute path is
// resolved.
func TestResolvePathNeedsAnAbsolutePath(t *testing.T) {
	if _, err := resolvePath(fakeFiles{}, filepath.Join("relative", "x")); !errorMatches(err, "it is not an absolute path") {
		t.Fatalf("err = %v", err)
	}
}

// The real file system is read once per entry for one Resolver, however
// many paths pass through it.
func TestOSFilesReadsEachEntryOnce(t *testing.T) {
	dir := t.TempDir()
	files := newOSFiles()
	if kind, err := files.kind(dir); kind != entryDir || err != nil {
		t.Fatalf("kind(%q) = %v, %v", dir, kind, err)
	}
	gone := filepath.Join(dir, "gone")
	if kind, err := files.kind(gone); kind != entryMissing || err != nil {
		t.Fatalf("kind(%q) = %v, %v", gone, kind, err)
	}
	writeFile(t, gone, "now here")
	if kind, err := files.kind(gone); kind != entryMissing || err != nil {
		t.Fatalf("kind(%q) read again = %v, %v; want the first answer", gone, kind, err)
	}
	if kind, err := newOSFiles().kind(gone); kind != entryFile || err != nil {
		t.Fatalf("a fresh read of %q = %v, %v", gone, kind, err)
	}
}
