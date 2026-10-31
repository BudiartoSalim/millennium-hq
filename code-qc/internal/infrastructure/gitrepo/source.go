package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
// working tree, sorted. Special files and tracked files deleted from the
// working tree are left out.
func (r *Repository) Files(ctx context.Context) ([]string, error) {
	out, err := r.git(ctx, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	var files []string
	for _, field := range splitNUL(out) {
		p, err := checkedPath(field)
		if err != nil {
			return nil, err
		}
		present, err := r.isAnalysable(p)
		if err != nil {
			return nil, err
		}
		if present {
			files = append(files, p)
		}
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

func (r *Repository) isAnalysable(p string) (bool, error) {
	info, err := os.Lstat(r.abs(p))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", p, err)
	}
	return info.Mode()&specialFile == 0, nil
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

// RepoPath turns a command-line path, relative to the current directory or
// absolute, into a repository-relative one. Both sides have symlinks
// resolved, so a symlinked working directory still maps correctly. The
// repository root itself becomes "".
func (r *Repository) RepoPath(arg string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("find working directory: %w", err)
	}
	return r.relativeTo(cwd, arg)
}

func (r *Repository) relativeTo(cwd, arg string) (string, error) {
	base, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", cwd, err)
	}
	root, err := filepath.EvalSymlinks(r.root)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", r.root, err)
	}
	target := arg
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", fmt.Errorf("%s is not inside the repository: %w", arg, err)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return "", nil
	}
	if !domain.IsRepoRelative(rel) {
		return "", fmt.Errorf("%s is outside the repository", arg)
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
	out, err := r.git(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("find the git directory: %w", err)
	}
	dir := trimLine(out)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(r.root, dir)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve the git directory: %w", err)
	}
	return resolved, nil
}
