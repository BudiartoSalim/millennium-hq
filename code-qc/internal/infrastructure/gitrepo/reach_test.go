package gitrepo

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
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
// missing, and a path in errs cannot be looked at.
type fakeFiles struct {
	root    string
	entries map[string]fakeEntry
	errs    map[string]error
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
	rel        string // slash-separated, below root
	want       string // below root, slash-separated; "" for nothing
	wantErr    string
	resolveErr string // Resolve's, which allows no dangling link nor a part below a file; "" when it agrees
}

// The walk follows every link itself, a Windows junction included, and
// blocks on a link it cannot read. A protected location (Location) follows
// a link that leads nowhere as it reads and reaches nothing below a file;
// a tool's path (Resolve) is unknown there.
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
		{name: "a loop", entries: map[string]fakeEntry{at(".pi"): link(".pi")}, rel: ".pi/extensions", wantErr: "more than 40 links one after another"},
		{name: "a link to a device path", entries: map[string]fakeEntry{at(".pi"): link(`\\?\Volume{1}\conf`)}, rel: ".pi/extensions", wantErr: "is a device path"},
		{name: "a link to an NT path", entries: map[string]fakeEntry{at(".pi"): link(`\??\C:\conf`)}, rel: ".pi/extensions", wantErr: "is a device path"},
		{name: "a link with an empty target", entries: map[string]fakeEntry{at(".pi"): link("")}, rel: ".pi/extensions", wantErr: "its target is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := fakeFiles{root: root, entries: tt.entries, errs: tt.errs}
			want := ""
			if tt.want != "" {
				want = at(tt.want)
			}
			got, err := location(files, root, tt.rel)
			if got != want || !errorMatches(err, tt.wantErr) {
				t.Fatalf("location(%q) = %q, %v; want %q, error %q", tt.rel, got, err, want, tt.wantErr)
			}
			resolveWant, resolveErr := want, tt.wantErr
			if tt.resolveErr != "" {
				resolveWant, resolveErr = "", tt.resolveErr
			}
			if got, err := resolvePath(files, at(tt.rel)); got != resolveWant || !errorMatches(err, resolveErr) {
				t.Fatalf("resolvePath(%q) = %q, %v; want %q, error %q", tt.rel, got, err, resolveWant, resolveErr)
			}
		})
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
