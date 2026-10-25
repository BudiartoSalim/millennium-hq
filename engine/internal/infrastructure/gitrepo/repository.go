// Package gitrepo reads change sets and history from git. The engine runs on
// repositories that agents modify, so every git call runs without a shell,
// refuses revisions that look like options, and disables core.fsmonitor: a
// repository's own config can name an fsmonitor program, and git would run it
// for whoever calls git.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"

	"millennium/internal/domain"
)

// specialFile matches entries that cannot hold source code. Claude Code's
// sandbox mounts character devices over protected paths, and git then lists
// them as untracked files.
const specialFile = fs.ModeDevice | fs.ModeCharDevice | fs.ModeNamedPipe | fs.ModeSocket

// Repository is a git working tree.
type Repository struct {
	root string
}

// Open finds the repository containing dir.
func Open(ctx context.Context, dir string) (*Repository, error) {
	out, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("find repository root: %w", err)
	}
	return &Repository{root: trimLine(out)}, nil
}

// Root is the absolute path of the working tree.
func (r *Repository) Root() string { return r.root }

// Changes lists files that differ between base and the working tree, staged,
// unstaged and untracked alike, sorted by path, with the tree they were
// compared against. An empty base means HEAD, or the empty tree in a
// repository without commits.
func (r *Repository) Changes(ctx context.Context, base string) (domain.ChangeSet, error) {
	tree, err := r.baseTree(ctx, base)
	if err != nil {
		return domain.ChangeSet{}, err
	}
	files, err := r.changedFiles(ctx, base, tree)
	if err != nil {
		return domain.ChangeSet{}, err
	}
	return domain.ChangeSet{Base: tree, Files: files}, nil
}

func (r *Repository) changedFiles(ctx context.Context, base, tree string) ([]domain.ChangedFile, error) {
	diff, err := r.git(ctx, "diff", "--name-status", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", tree, "--")
	if err != nil {
		return nil, fmt.Errorf("diff against %s: %w", describeBase(base), err)
	}
	files, err := parseNameStatus(diff)
	if err != nil {
		return nil, err
	}
	untracked, err := r.git(ctx, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("list untracked files: %w", err)
	}
	added, err := parseAdded(untracked)
	if err != nil {
		return nil, err
	}
	files, err = r.dropSpecialFiles(append(files, added...))
	if err != nil {
		return nil, err
	}
	slices.SortFunc(files, func(a, b domain.ChangedFile) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

// Tracked reports whether path exists in revision rev.
func (r *Repository) Tracked(ctx context.Context, rev, path string) (bool, error) {
	if err := checkRevision(rev); err != nil {
		return false, err
	}
	exists, err := r.hasCommit(ctx, rev)
	if err != nil || !exists {
		return false, err
	}
	out, err := r.git(ctx, "ls-tree", "--name-only", "-z", rev, "--", path)
	if err != nil {
		return false, fmt.Errorf("look up %s in %s: %w", path, rev, err)
	}
	return len(out) > 0, nil
}

func (r *Repository) baseTree(ctx context.Context, base string) (string, error) {
	if base == "" {
		exists, err := r.hasCommit(ctx, "HEAD")
		if err != nil {
			return "", err
		}
		if !exists {
			return r.emptyTree(ctx)
		}
		base = "HEAD"
	}
	if err := checkRevision(base); err != nil {
		return "", err
	}
	out, err := r.git(ctx, "rev-parse", "--verify", "--quiet", base+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("resolve base %q: %w", base, err)
	}
	return trimLine(out), nil
}

// emptyTree asks git for the empty tree's id, which differs between SHA-1
// and SHA-256 repositories.
func (r *Repository) emptyTree(ctx context.Context) (string, error) {
	out, err := r.git(ctx, "hash-object", "-t", "tree", "--stdin")
	if err != nil {
		return "", fmt.Errorf("compute empty tree: %w", err)
	}
	return trimLine(out), nil
}

func (r *Repository) hasCommit(ctx context.Context, rev string) (bool, error) {
	_, err := r.git(ctx, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("resolve %s: %w", rev, err)
	}
	return true, nil
}

func (r *Repository) dropSpecialFiles(files []domain.ChangedFile) ([]domain.ChangedFile, error) {
	kept := make([]domain.ChangedFile, 0, len(files))
	for _, f := range files {
		if f.Kind == domain.ChangeDeleted {
			kept = append(kept, f)
			continue
		}
		info, err := os.Lstat(r.abs(f.Path))
		if err != nil {
			return nil, fmt.Errorf("inspect changed file %s: %w", f.Path, err)
		}
		if info.Mode()&specialFile == 0 {
			kept = append(kept, f)
		}
	}
	return kept, nil
}

func (r *Repository) git(ctx context.Context, args ...string) ([]byte, error) {
	return run(ctx, r.root, args...)
}

func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false", "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, &gitError{args: args, stderr: strings.TrimSpace(stderr.String()), err: err}
	}
	return out, nil
}

// checkRevision refuses values git would parse as options, so a base passed
// on the command line cannot smuggle in flags such as --output.
func checkRevision(rev string) error {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return fmt.Errorf("revision %q is not allowed: it must not be empty or start with '-'", rev)
	}
	return nil
}

func describeBase(base string) string {
	if base == "" {
		return "HEAD"
	}
	return base
}

func trimLine(out []byte) string { return strings.TrimRight(string(out), "\r\n") }

type gitError struct {
	args   []string
	stderr string
	err    error
}

func (e *gitError) Error() string {
	msg := fmt.Sprintf("git %s: %v", strings.Join(e.args, " "), e.err)
	if e.stderr != "" {
		msg += ": " + e.stderr
	}
	return msg
}

func (e *gitError) Unwrap() error { return e.err }
