package gitrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// The guard resolves the paths a tool names, and the protected locations
// of each checkout, by walking them one name at a time and following every
// link itself, rather than with filepath.EvalSymlinks: since Go 1.23,
// EvalSymlinks on Windows walks through a directory junction without
// following it (Go reports one as neither a directory nor a symlink), while
// Windows follows it, and `mklink /J` needs no privilege. The walk also
// decides what a link that leads nowhere means, which differs between a
// tool's path and a protected location (see danglingLink).

// entryKind is what a path names on the file system, as the walk reads it.
type entryKind int

const (
	// entryMissing is nothing.
	entryMissing entryKind = iota
	// entryDir is a directory, which the walk enters.
	entryDir
	// entryFile is anything else the walk does not follow, such as a file
	// or a device: nothing lies below it.
	entryFile
	// entryLink is a symlink, or on Windows a directory junction or another
	// reparse point that redirects a name: the walk follows it where
	// os.Readlink says it leads, and blocks when it cannot read it.
	entryLink
)

// Windows file attributes, as os.Lstat reports them on Windows in
// syscall.Win32FileAttributeData.
const (
	win32AttributeDirectory    = 0x10
	win32AttributeReparsePoint = 0x400
)

// classify is the kind of an entry from what os.Lstat reports: its mode,
// and on Windows its file attributes (0 elsewhere). Go sets ModeSymlink
// only for a symlink. For the reparse points that redirect a name (a name
// surrogate, such as a directory junction) it sets neither ModeSymlink nor
// ModeDir, though Windows keeps their directory attribute; other reparse
// points, such as cloud-file placeholders, keep the mode of what they are.
// So a directory attribute on a reparse point that Go reports as no
// directory is a link the walk must follow.
func classify(mode fs.FileMode, win32Attributes uint32) entryKind {
	redirect := win32Attributes&win32AttributeReparsePoint != 0 && win32Attributes&win32AttributeDirectory != 0 && !mode.IsDir()
	switch {
	case mode&fs.ModeSymlink != 0 || redirect:
		return entryLink
	case mode.IsDir():
		return entryDir
	}
	return entryFile
}

// danglingLink is what a walk makes of a link that leads to nothing.
type danglingLink int

const (
	// danglingUnknown: where the path leads is unknown, an error. A tool's
	// path is checked as the OS opens it, and the engine does not predict
	// what the OS creates through a link that leads nowhere.
	danglingUnknown danglingLink = iota
	// danglingFollowed: the link is followed as it reads, and the rest of
	// the path joined to that. A protected location is where a write
	// lands once the link's target exists: with `.pi` a link to `conf`
	// that does not exist yet, creating `conf/extensions/x.ts` makes pi
	// load it.
	danglingFollowed
)

// maxLinks bounds the links one walk follows, as Linux does.
const maxLinks = 40

// errBelowFile is a path that goes on below a part that is not a
// directory: nothing can be created there.
var errBelowFile = errors.New("not a directory, so nothing lies below it")

// fileSystem is what a walk reads: the kind of an entry, without following
// it, and where a link leads. osFiles reads the real one.
type fileSystem interface {
	kind(path string) (entryKind, error)
	readlink(path string) (string, error)
}

// walker walks paths over files, following at most maxLinks links in all.
type walker struct {
	files    fileSystem
	dangling danglingLink
	links    int
}

// reached is where a walk ends: the path, with every link in it followed,
// and the kind of what is there. A path with a part that does not exist is
// entryMissing, with the rest joined to it as named.
type reached struct {
	path string
	kind entryKind
}

// resolve walks abs, an absolute clean path, from its volume's root.
func (w *walker) resolve(abs string) (reached, error) {
	volume := filepath.VolumeName(abs)
	return w.walk(volume+string(filepath.Separator), splitPath(abs[len(volume):]))
}

// walk follows parts, names one below the other, from dir, a directory
// whose links are followed already.
func (w *walker) walk(dir string, parts []string) (reached, error) {
	at := reached{path: dir, kind: entryDir}
	for i, part := range parts {
		if at.kind == entryFile {
			return reached{}, fmt.Errorf("inspect %s: %w", filepath.Join(at.path, part), errBelowFile)
		}
		next, err := w.step(at.path, part)
		if err != nil {
			return reached{}, err
		}
		if next.kind == entryMissing {
			return reached{path: filepath.Join(append([]string{next.path}, parts[i+1:]...)...), kind: entryMissing}, nil
		}
		at = next
	}
	return at, nil
}

// step is where one name leads from dir. dir has its links followed, so
// its parent is the directory ".." opens.
func (w *walker) step(dir, part string) (reached, error) {
	switch part {
	case ".":
		return reached{path: dir, kind: entryDir}, nil
	case "..":
		return reached{path: filepath.Dir(dir), kind: entryDir}, nil
	}
	next := filepath.Join(dir, part)
	kind, err := w.files.kind(next)
	if err != nil {
		return reached{}, fmt.Errorf("inspect %s: %w", next, err)
	}
	if kind == entryLink {
		return w.follow(dir, next)
	}
	return reached{path: next, kind: kind}, nil
}

// follow is where the link at link, an entry of dir, leads.
func (w *walker) follow(dir, link string) (reached, error) {
	w.links++
	if w.links > maxLinks {
		return reached{}, fmt.Errorf("resolve %s: more than %d links one after another, which may loop", link, maxLinks)
	}
	target, err := w.files.readlink(link)
	if err != nil {
		return reached{}, fmt.Errorf("resolve %s: it is a link (a symlink, or on Windows a directory junction or another reparse point) whose target cannot be read, so where a path through it leads is unknown: %w", link, err)
	}
	from, parts, err := linkStart(dir, target)
	if err != nil {
		return reached{}, fmt.Errorf("resolve %s: %w", link, err)
	}
	end, err := w.walk(from, parts)
	if err == nil && end.kind == entryMissing && w.dangling == danglingUnknown {
		return reached{}, fmt.Errorf("resolve %s: it leads to %s, which does not exist", link, end.path)
	}
	return end, err
}

// linkStart is where a link's target is walked from, and its names: an
// absolute target from its volume's root, a relative one from the link's
// directory. A target that is neither is an error, as on Windows a rooted
// one without a volume (`\x`) or one with a volume and no root (`C:x`). So
// is a device path (`\\?\`, `\??\`), such as a junction to a volume by its
// GUID: no other spelling of the same file matches it.
func linkStart(dir, target string) (string, []string, error) {
	switch {
	case target == "":
		return "", nil, errors.New("its target is empty")
	case strings.HasPrefix(target, `\\?\`) || strings.HasPrefix(target, `\??\`):
		return "", nil, fmt.Errorf("its target %s is a device path, which no other spelling of the same file matches", target)
	case filepath.IsAbs(target):
		volume := filepath.VolumeName(target)
		return volume + string(filepath.Separator), splitPath(target[len(volume):]), nil
	case filepath.VolumeName(target) != "" || os.IsPathSeparator(target[0]):
		return "", nil, fmt.Errorf("its target %s is neither absolute nor plainly relative", target)
	}
	return dir, splitPath(target), nil
}

// splitPath is the names of p, between any separators the OS accepts.
func splitPath(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r < utf8.RuneSelf && os.IsPathSeparator(byte(r)) })
}

// osFiles is the real file system. It reads each entry once: a hook
// decision resolves the protected locations of every worktree, which share
// most of their paths, and the file system is taken to hold still for that
// long.
type osFiles struct {
	kinds map[string]entryKind
}

func newOSFiles() *osFiles { return &osFiles{kinds: map[string]entryKind{}} }

func (f *osFiles) kind(p string) (entryKind, error) {
	if kind, ok := f.kinds[p]; ok {
		return kind, nil
	}
	kind, err := lstatKind(p)
	if err != nil {
		return 0, err
	}
	f.kinds[p] = kind
	return kind, nil
}

func (f *osFiles) readlink(p string) (string, error) { return os.Readlink(p) }

// lstatKind is the kind of the entry at p, not followed.
func lstatKind(p string) (entryKind, error) {
	info, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return entryMissing, nil
	}
	if err != nil {
		return 0, err
	}
	return entryOf(info)
}

// Resolver resolves paths for one hook decision, reading each entry of the
// file system once however many paths pass through it.
type Resolver struct {
	files *osFiles
}

// NewResolver returns a Resolver that has read nothing yet.
func NewResolver() *Resolver { return &Resolver{files: newOSFiles()} }

// Resolve is abs, an absolute path, cleaned and with its links followed in
// the longest part of it that exists, so a path through a link names what
// it reaches, and one that does not exist yet (a write may create the
// rest) still resolves. abs is cleaned as text, so a caller that cannot
// trust its ".." parts refuses them first. A part that exists but cannot
// be followed or looked at is an error, and so is a link that leads to
// nothing and a part below a file: what the path reaches is unknown.
func (r *Resolver) Resolve(abs string) (string, error) { return resolvePath(r.files, abs) }

// Location is where a write to protected, a protected location such as
// `.pi/extensions` (slash-separated), in the checkout at root lands. root
// is a directory with its links followed, as Resolve gives it. A link that
// leads to nothing is followed as it reads (see danglingFollowed), and ""
// means nothing can be written there, because a part of it is a file. An
// error means where it lands is unknown, as when a part may not be looked
// at or is a link that cannot be read.
func (r *Resolver) Location(root, protected string) (string, error) {
	return location(r.files, root, protected)
}

// resolvePath is Resolver.Resolve over files.
func resolvePath(files fileSystem, abs string) (string, error) {
	if !filepath.IsAbs(abs) {
		return "", fmt.Errorf("cannot resolve %s: it is not an absolute path", abs)
	}
	end, err := (&walker{files: files, dangling: danglingUnknown}).resolve(filepath.Clean(abs))
	if err != nil {
		return "", err
	}
	return normalize(end.path)
}

// location is Resolver.Location over files.
func location(files fileSystem, root, protected string) (string, error) {
	end, err := (&walker{files: files, dangling: danglingFollowed}).walk(root, splitPath(filepath.FromSlash(protected)))
	if errors.Is(err, errBelowFile) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return normalize(end.path)
}
