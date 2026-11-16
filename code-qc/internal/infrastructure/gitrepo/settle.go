package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"

	"millennium-code-qc/internal/domain"
)

// entryState is what a listed path is in the working tree right now.
type entryState int

const (
	// entryUnknown is the zero value, so a state read without its error
	// keeps nothing.
	entryUnknown entryState = iota
	entryAnalysable
	// entryEmpty is an empty regular file: nothing to analyse, but present.
	entryEmpty
	entrySpecial
	entryAbsent
)

// inspect looks at p without following a symlink. Only a path that does
// not exist is absent; any other failure to look is an error.
func (r *Repository) inspect(p string) (entryState, error) {
	info, err := os.Lstat(r.abs(p))
	if errors.Is(err, fs.ErrNotExist) {
		return entryAbsent, nil
	}
	if err != nil {
		return entryUnknown, fmt.Errorf("inspect %s: %w", p, err)
	}
	switch {
	case info.Mode()&specialFile != 0:
		return entrySpecial, nil
	case info.Mode().IsRegular() && info.Size() == 0:
		return entryEmpty, nil
	}
	return entryAnalysable, nil
}

// listing says which git command listed a changed path.
type listing int

const (
	// listedByDiff is a path git's diff against the base reports.
	listedByDiff listing = iota
	// listedUntracked is a path `git ls-files --others` reports.
	listedUntracked
)

// present reports whether a path in this state is in the working tree to
// be checked.
func (s entryState) present() bool { return s == entryAnalysable || s == entryEmpty }

// untrackedKept reports whether an untracked path in this state holds
// content to check: present, and not an empty sandbox placeholder.
func untrackedKept(p string, state entryState) bool {
	return state == entryAnalysable || (state == entryEmpty && !sandboxPlaceholder(p))
}

// untracked lists the non-ignored untracked files, settled.
func (r *Repository) untracked(ctx context.Context) ([]domain.ChangedFile, error) {
	paths, err := r.listPaths(ctx, "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	added := make([]domain.ChangedFile, len(paths))
	for i, p := range paths {
		added[i] = domain.ChangedFile{Path: p, Kind: domain.ChangeAdded}
	}
	return r.settle(added, listedUntracked)
}

// sandboxProtectedNames are the paths outside .claude that Claude Code's
// sandbox protects: shell profiles, git and editor config, and .mcp.json.
// Tool config such as .npmrc or .golangci.yml is not among them: an empty
// one still changes how a tool behaves, so it stays part of a change.
var sandboxProtectedNames = map[string]bool{
	".bashrc": true, ".bash_profile": true, ".zshrc": true, ".zprofile": true, ".profile": true,
	".gitconfig": true, ".gitmodules": true, ".ripgreprc": true, ".mcp.json": true,
	".vscode": true, ".idea": true,
}

// sandboxPlaceholder reports whether p is where Claude Code's sandbox may
// put a placeholder. Where a protected path does not exist, the sandbox
// mounts a device over it while a sandboxed command runs: inside the
// sandbox git lists a device, and outside it an empty file that vanishes
// when the command exits, possibly between git listing it and the engine
// reading it. The sandbox protects paths relative to the session's
// directory, which can be any directory of a monorepo, so they match at any
// depth. Its set of paths inside .claude grows between releases (agents,
// commands, settings, launch.json, ...), so every path directly inside a
// .claude directory matches.
func sandboxPlaceholder(p string) bool {
	dir, name := path.Split(p)
	return sandboxProtectedNames[name] || path.Base(dir) == ".claude"
}

// settle checks listed paths against the working tree as it is now and
// keeps those whose content the gates can check.
func (r *Repository) settle(files []domain.ChangedFile, from listing) ([]domain.ChangedFile, error) {
	kept := make([]domain.ChangedFile, 0, len(files))
	for _, f := range files {
		if f.Kind == domain.ChangeDeleted {
			kept = append(kept, f)
			continue
		}
		state, err := r.inspect(f.Path)
		if err != nil {
			return nil, fmt.Errorf("changed file: %w", err)
		}
		keep, err := settled(f, state, from)
		if err != nil {
			return nil, err
		}
		if keep {
			kept = append(kept, f)
		}
	}
	return kept, nil
}

// settled decides one path. A special file cannot hold code. An untracked
// sandbox placeholder, empty or already gone, is not part of the change.
// Any other path that is gone blocks: its content cannot be checked.
func settled(f domain.ChangedFile, state entryState, from listing) (bool, error) {
	placeholder := from == listedUntracked && sandboxPlaceholder(f.Path)
	switch {
	case state == entryAbsent && placeholder:
		return false, nil
	case state == entryAbsent && from == listedUntracked:
		return false, fmt.Errorf("untracked file %s vanished while the gate ran; run it again, and if your sandbox protects this path, add it to .git/info/exclude", f.Path)
	case state == entryAbsent:
		return false, fmt.Errorf("changed file %s: git lists it as %s, but it is not in the working tree: it changed while the gate ran, or git and the filesystem disagree on its name; check git status", f.Path, f.Kind)
	case from == listedUntracked:
		return untrackedKept(f.Path, state), nil
	}
	return state.present(), nil
}
