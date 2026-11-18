package gitrepo

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// maxWorktreeList bounds `git worktree list` output: a few hundred bytes a
// worktree, so thousands of them.
const maxWorktreeList = 1 << 20

// OtherWorktrees lists the roots of the repository's other working trees:
// the main checkout and every linked worktree, Root excepted, including
// ones whose directory is gone but that git still lists. The guard
// protects the rules of every one of them, whichever the agent is in.
func (r *Repository) OtherWorktrees(ctx context.Context) ([]string, error) {
	out, err := runLimited(ctx, r.root, maxWorktreeList, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list the repository's worktrees: %w", err)
	}
	roots, err := parseWorktrees(out)
	if err != nil {
		return nil, err
	}
	own := filepath.Clean(r.root)
	return slices.DeleteFunc(roots, func(root string) bool { return root == own }), nil
}

// parseWorktrees reads `git worktree list --porcelain`: per worktree a
// "worktree <path>" line, then attribute lines, and an empty line. Only the
// paths matter, and each must be absolute. -z would also allow a newline in
// a path, but needs git 2.36; without it, such a path, which only someone
// set on confusing this listing would create, reads as an extra root, or as
// a relative one, which is refused, and never hides another worktree.
func parseWorktrees(out []byte) ([]string, error) {
	var roots []string
	for _, line := range strings.Split(string(out), "\n") {
		p, ok := strings.CutPrefix(line, "worktree ")
		if !ok {
			continue
		}
		root := filepath.Clean(filepath.FromSlash(p))
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("git listed a worktree that is not an absolute path: %q", p)
		}
		if !slices.Contains(roots, root) {
			roots = append(roots, root)
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("git listed no worktree, not even the one at hand, in %d bytes of output", len(out))
	}
	return roots, nil
}
