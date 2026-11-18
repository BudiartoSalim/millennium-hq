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
	records, err := os.ReadDir(recordsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the repository's worktree records: %w", err)
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

// listedRecords counts the worktree records in dir that git lists: those
// with a gitdir file that is not empty, which names the worktree; git skips
// every other entry. Counting those too would leave room in the listing for
// a record forged by a worktree path with a newline. An entry whose gitdir
// cannot be looked at is not counted: counting too few refuses the listing,
// which blocks, and never hides a worktree.
func listedRecords(dir string, entries []fs.DirEntry) int {
	listed := 0
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join(dir, entry.Name(), "gitdir"))
		if err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			listed++
		}
	}
	return listed
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
