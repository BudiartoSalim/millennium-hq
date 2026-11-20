package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// maxWorktreeList bounds `git worktree list` output: a few hundred bytes a
// worktree, so thousands of them.
const maxWorktreeList = 1 << 20

// OtherWorktrees lists the roots of the repository's other working trees:
// the main checkout and every linked worktree, Root excepted, including
// ones whose directory is gone but that git still lists. Git keeps a
// record of each linked worktree under <git-common-dir>/worktrees; without
// that directory there is none, and git is not asked: listing costs a
// tenth of a second on some file systems.
func (r *Repository) OtherWorktrees(ctx context.Context) ([]string, error) {
	common, err := r.gitCommonDir(ctx)
	if err != nil {
		return nil, err
	}
	recordsDir := filepath.Join(common, "worktrees")
	records, exists, err := worktreeRecords(recordsDir)
	if err != nil || !exists {
		return nil, err
	}
	out, err := runLimited(ctx, r.root, maxWorktreeList, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list the repository's worktrees: %w", err)
	}
	roots, err := parseWorktrees(out, listedRecords(recordsDir, records)+1)
	if err != nil {
		return nil, err
	}
	own := filepath.Clean(r.root)
	return slices.DeleteFunc(roots, func(root string) bool { return root == own }), nil
}

// worktreeRecords lists the entries of git's worktree records directory,
// and whether it exists: when it does not, the repository has no linked
// worktree. Anything else that is not a directory there is an error. Lstat
// decides, not ReadDir: on Windows, reading a file as a directory reports
// the path as not found, which would pass for "no worktrees".
func worktreeRecords(dir string) ([]fs.DirEntry, bool, error) {
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err == nil && !info.IsDir() {
		err = fmt.Errorf("%s is not a directory", dir)
	}
	if err != nil {
		return nil, true, fmt.Errorf("read the repository's worktree records: %w", err)
	}
	records, err := os.ReadDir(dir)
	if err != nil {
		return nil, true, fmt.Errorf("read the repository's worktree records: %w", err)
	}
	return records, true, nil
}

// listedRecords counts the worktree records in dir that git lists: those
// with a gitdir file git reads at least one byte of, which names the
// worktree; git skips every other entry, one whose gitdir it cannot read
// included. Counting those too would leave room in the listing for a
// record forged by a worktree path with a newline. An entry whose gitdir
// cannot be looked at is not counted: counting too few refuses the listing,
// which blocks, and never hides a worktree.
func listedRecords(dir string, entries []fs.DirEntry) int {
	listed := 0
	for _, entry := range entries {
		if hasContent(filepath.Join(dir, entry.Name(), "gitdir")) {
			listed++
		}
	}
	return listed
}

// hasContent reports whether p is a regular file that a read of one byte
// gets a byte from. It is opened only once it is known to be a regular
// file, so a pipe in its place cannot hold the hook. Any failure is
// "no": the record is then not counted (see listedRecords).
func hasContent(p string) bool {
	info, err := os.Stat(p)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	n, _ := f.Read(make([]byte, 1)) // a failed read gets no byte, which is the answer
	return n > 0
}

// gitCommonDir is git's common directory, absolute: the one every worktree
// of a clone shares. It is asked once per repository.
func (r *Repository) gitCommonDir(ctx context.Context) (string, error) {
	if r.commonDir != "" {
		return r.commonDir, nil
	}
	out, err := r.git(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("find the git directory: %w", err)
	}
	dir := trimLine(out)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(r.root, dir)
	}
	r.commonDir = dir
	return dir, nil
}

// worktreeLine is what may follow a line of a `git worktree list
// --porcelain` record: the line kinds, by the kind before them, in the
// order git prints them. "" is the empty line that ends a record.
var worktreeLine = map[string][]string{
	"":         {"worktree"},
	"worktree": {"HEAD", "bare"},
	"HEAD":     {"branch", "detached", "locked", "prunable", ""},
	"bare":     {"locked", "prunable", ""},
	"branch":   {"locked", "prunable", ""},
	"detached": {"locked", "prunable", ""},
	"locked":   {"prunable", ""},
	"prunable": {""},
}

// parseWorktrees reads `git worktree list --porcelain`: per worktree a
// "worktree <path>" line, attribute lines, and an empty line. Only the
// paths matter, and each must be absolute. A path may hold a newline, which
// this format (-z needs git 2.36) prints as is, so the output must be
// unambiguous or it is refused: every line the kind git prints where it
// stands, and no more worktrees than recorded, the most git lists. A path
// made to read as extra lines either breaks that order, or adds a record
// and so a worktree too many; it never passes for other worktrees.
func parseWorktrees(out []byte, recorded int) ([]string, error) {
	text, ok := strings.CutSuffix(string(out), "\n")
	if !ok {
		return nil, fmt.Errorf("git listed no worktree, not even the one at hand, or cut its list short, in %d bytes of output", len(out))
	}
	var roots []string
	previous := ""
	for _, line := range strings.Split(text, "\n") {
		kind, value, _ := strings.Cut(line, " ")
		if !slices.Contains(worktreeLine[previous], kind) {
			return nil, fmt.Errorf("cannot read git's list of worktrees unambiguously (a worktree path may hold a newline): %q does not belong after a %q line", line, previous)
		}
		previous = kind
		if kind != "worktree" {
			continue
		}
		root, err := worktreeRoot(value)
		if err != nil {
			return nil, err
		}
		roots = append(roots, root)
	}
	return checkedRoots(roots, previous, recorded)
}

// worktreeRoot is a worktree path as git lists it, which must be absolute.
func worktreeRoot(listed string) (string, error) {
	root := filepath.Clean(filepath.FromSlash(listed))
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("git listed a worktree that is not an absolute path: %q", listed)
	}
	return root, nil
}

// checkedRoots are the roots of a whole listing: one that ended its last
// record, with no more worktrees than git records.
func checkedRoots(roots []string, last string, recorded int) ([]string, error) {
	if last != "" {
		return nil, fmt.Errorf("git's list of worktrees ends inside a record, after a %q line", last)
	}
	if len(roots) > recorded {
		return nil, fmt.Errorf("cannot read git's list of worktrees unambiguously (a worktree path may hold a newline): it names %d worktrees, and git records %d", len(roots), recorded)
	}
	return roots, nil
}
