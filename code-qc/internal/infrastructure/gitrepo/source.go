package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"millennium-code-qc/internal/domain"
)

// maxSourceBytes bounds every file read for analysis. A larger file blocks
// with a clear message instead of exhausting memory; generated or vendored
// files that size belong in the config's exclude list.
const maxSourceBytes = 4 << 20

// Files lists every tracked or non-ignored untracked file present in the
// working tree, sorted. Special files, the sandbox's placeholders and files
// gone from the working tree, including untracked ones that vanish while
// they are listed, are left out: an audit checks what is there.
func (r *Repository) Files(ctx context.Context) ([]string, error) {
	tracked, err := r.listPaths(ctx, "--cached")
	if err != nil {
		return nil, err
	}
	untracked, err := r.listPaths(ctx, "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	files, err := r.keepPaths(tracked, func(_ string, state entryState) bool { return state.present() })
	if err != nil {
		return nil, err
	}
	others, err := r.keepPaths(untracked, untrackedKept)
	if err != nil {
		return nil, err
	}
	files = append(files, others...)
	slices.Sort(files)
	return slices.Compact(files), nil
}

// listPaths runs `git ls-files` with the given selection.
func (r *Repository) listPaths(ctx context.Context, selection ...string) ([]string, error) {
	out, err := r.git(ctx, append(append([]string{"ls-files"}, selection...), "-z")...)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	fields := splitNUL(out)
	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		p, err := checkedPath(field)
		if err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

// keepPaths keeps the paths keep accepts in their current state.
func (r *Repository) keepPaths(paths []string, keep func(string, entryState) bool) ([]string, error) {
	var kept []string
	for _, p := range paths {
		state, err := r.inspect(p)
		if err != nil {
			return nil, err
		}
		if keep(p, state) {
			kept = append(kept, p)
		}
	}
	return kept, nil
}

// ReadWorking reads a regular file from the working tree. Symlinks are
// refused, so analysis cannot be pointed at a file outside the repository.
func (r *Repository) ReadWorking(p string) ([]byte, error) {
	if _, err := checkedPath(p); err != nil {
		return nil, err
	}
	info, err := os.Lstat(r.abs(p))
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", p, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file; only regular files are analysed", p)
	}
	file, err := os.Open(r.abs(p))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", p, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	if len(data) > maxSourceBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes; exclude it in the config if it is generated or vendored", p, maxSourceBytes)
	}
	return data, nil
}

// ReadAt reads path as it was in rev, reporting false when it was not a file
// there.
func (r *Repository) ReadAt(ctx context.Context, rev, p string) ([]byte, bool, error) {
	if err := checkRevision(rev); err != nil {
		return nil, false, err
	}
	if _, err := checkedPath(p); err != nil {
		return nil, false, err
	}
	blob, found, err := r.blobAt(ctx, rev, p)
	if err != nil || !found {
		return nil, false, err
	}
	sizeOut, err := r.git(ctx, "cat-file", "-s", blob)
	if err != nil {
		return nil, false, fmt.Errorf("size of %s at %s: %w", p, rev, err)
	}
	size, err := strconv.Atoi(trimLine(sizeOut))
	if err != nil || size > maxSourceBytes {
		return nil, false, fmt.Errorf("%s at %s is larger than %d bytes or has an unreadable size", p, rev, maxSourceBytes)
	}
	data, err := r.git(ctx, "cat-file", "blob", blob)
	if err != nil {
		return nil, false, fmt.Errorf("read %s at %s: %w", p, rev, err)
	}
	return data, true, nil
}

// blobAt finds the blob of p in rev from `git ls-tree` output:
// "<mode> <type> <object>\t<path>". A submodule or tree is not a file.
func (r *Repository) blobAt(ctx context.Context, rev, p string) (string, bool, error) {
	out, err := r.git(ctx, "ls-tree", "-z", rev, "--", p)
	if err != nil {
		return "", false, fmt.Errorf("look up %s in %s: %w", p, rev, err)
	}
	entry := string(bytes.TrimSuffix(out, []byte{0}))
	if entry == "" {
		return "", false, nil
	}
	meta, _, ok := strings.Cut(entry, "\t")
	fields := strings.Fields(meta)
	if !ok || len(fields) != 3 {
		return "", false, fmt.Errorf("unexpected git ls-tree output for %s: %q", p, entry)
	}
	if fields[1] != "blob" {
		return "", false, nil
	}
	return fields[2], true, nil
}

func (r *Repository) abs(p string) string {
	return filepath.Join(r.root, filepath.FromSlash(p))
}

// ErrOutsideRepository means a path lies outside the repository. A caller
// that cares only about the repository's files may skip such a path; any
// other failure to map a path is an error of its own.
var ErrOutsideRepository = errors.New("outside the repository")

// RepoPath turns a path, relative to the current directory or absolute, into
// a repository-relative one. Only a relative path reads the current
// directory, so an absolute one still maps when the process's working
// directory is gone. Symlinks are resolved on both sides, in the root and in
// the longest part of the path that exists (a write may create the rest),
// so a path through a symlink maps to what it reaches. The repository root
// itself becomes "".
func (r *Repository) RepoPath(arg string) (string, error) {
	cwd := ""
	if !filepath.IsAbs(arg) {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("find the working directory for %s: %w", arg, err)
		}
		cwd = wd
	}
	return r.relativeTo(cwd, arg)
}

// relativeTo maps arg, a relative one taken from cwd. cwd's symlinks are
// resolved before joining, so ".." leaves the directory cwd really is.
func (r *Repository) relativeTo(cwd, arg string) (string, error) {
	target := filepath.Clean(arg)
	if !filepath.IsAbs(target) {
		base, err := filepath.EvalSymlinks(cwd)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", cwd, err)
		}
		target = filepath.Join(base, target)
	}
	return mapResolved(r.root, target, arg)
}

// mapResolved maps target, absolute and clean, into root, both with their
// links followed (see Resolver.Resolve); arg names the path in errors.
func mapResolved(root, target, arg string) (string, error) {
	resolver := NewResolver()
	resolved, err := resolver.Resolve(target)
	if err != nil {
		return "", err
	}
	resolvedRoot, err := resolver.Resolve(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	return insideRoot(resolvedRoot, resolved, arg)
}

// insideRoot makes target relative to root, both resolved and clean,
// matching root's part without regard to case, or reports arg as
// ErrOutsideRepository.
func insideRoot(root, target, arg string) (string, error) {
	rel, ok := domain.RelativeFold(filepath.ToSlash(root), filepath.ToSlash(target))
	if !ok || rel != "" && !domain.IsRepoRelative(rel) {
		return "", fmt.Errorf("%s is %w", arg, ErrOutsideRepository)
	}
	return rel, nil
}

// ChangedLines lists the working-tree lines of p that differ from rev, from
// the hunk headers of a zero-context diff: "@@ -a,b +c,d @@" adds lines c to
// c+d-1, and a missing ",d" means one line.
func (r *Repository) ChangedLines(ctx context.Context, rev, p string) (domain.LineSet, error) {
	if err := checkRevision(rev); err != nil {
		return domain.LineSet{}, err
	}
	if _, err := checkedPath(p); err != nil {
		return domain.LineSet{}, err
	}
	out, err := r.git(ctx, "diff", "-U0", "--no-color", "--no-ext-diff", "--no-textconv", rev, "--", p)
	if err != nil {
		return domain.LineSet{}, fmt.Errorf("diff %s against %s: %w", p, rev, err)
	}
	return parseHunks(out)
}

func parseHunks(diff []byte) (domain.LineSet, error) {
	var set domain.LineSet
	for _, line := range strings.Split(string(diff), "\n") {
		if !strings.HasPrefix(line, "@@ ") {
			continue
		}
		added, ok := hunkAdded(line)
		if !ok {
			return domain.LineSet{}, fmt.Errorf("unexpected diff hunk header %q", line)
		}
		if added.Last >= added.First {
			set.Ranges = append(set.Ranges, added)
		}
	}
	return set, nil
}

func hunkAdded(header string) (domain.LineRange, bool) {
	fields := strings.Fields(header)
	if len(fields) < 3 || !strings.HasPrefix(fields[2], "+") {
		return domain.LineRange{}, false
	}
	startText, countText, hasCount := strings.Cut(strings.TrimPrefix(fields[2], "+"), ",")
	start, err := strconv.Atoi(startText)
	if err != nil {
		return domain.LineRange{}, false
	}
	count := 1
	if hasCount {
		if count, err = strconv.Atoi(countText); err != nil {
			return domain.LineRange{}, false
		}
	}
	return domain.LineRange{First: start, Last: start + count - 1}, true
}

// NearestUp returns the closest directory at or above from, and inside
// within, holding a regular file named marker.
func (r *Repository) NearestUp(from, marker, within string) (string, bool) {
	for dir := from; domain.Within(dir, within); dir = parentDir(dir) {
		info, err := os.Lstat(r.abs(path.Join(dir, marker)))
		if err == nil && info.Mode().IsRegular() {
			return dir, true
		}
		if dir == "" || dir == within {
			return "", false
		}
	}
	return "", false
}

func parentDir(dir string) string {
	if p := path.Dir(dir); p != "." {
		return p
	}
	return ""
}

// Identity names the repository stably across its worktrees: the resolved
// path of git's common directory, which every worktree of a clone shares.
func (r *Repository) Identity(ctx context.Context) (string, error) {
	dir, err := r.gitCommonDir(ctx)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve the git directory: %w", err)
	}
	return resolved, nil
}
