package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"millennium-code-qc/internal/domain"
)

// locate fills in where the paths a call names lie, as far as the guard's
// rules read them (domain.PathNeedOf), so a call no path rule applies to
// costs no file system or git work: its targets, each path as named and
// resolved; for a role rule the paths in every worktree; for the protected
// paths, in an opted-in repository, also the protected spelling of a
// target that a protected location of any worktree leads to.
func locate(ctx context.Context, deps Deps, call domain.ToolCall, named []string) (domain.ToolCall, error) {
	need := domain.PathNeedOf(call.Tool, call.Role)
	if !need.Any() || len(named) == 0 {
		return call, nil
	}
	targets, err := resolveTargets(deps, named)
	if err != nil {
		return call, err
	}
	call.Targets = targets
	if !optedIn(ctx, deps) {
		return call, nil
	}
	trees, err := checkouts(ctx, deps)
	if err != nil {
		return call, err
	}
	if need.Paths {
		call.Paths = domain.WorktreePaths(targets, treeForms(trees))
	}
	if need.Targets {
		call.Targets, err = protectedSpellings(deps, trees, targets)
	}
	return call, err
}

// optedIn reports whether the repository has rules for the guard to apply.
// Elsewhere the guard allows everything, so the worktrees are neither
// listed nor resolved, and a broken one cannot block calls there. A config
// that cannot be read needs no paths either: the guard then denies every
// write and allows reads.
func optedIn(ctx context.Context, deps Deps) bool {
	_, enabled, err := deps.Config.Execute(ctx)
	return err == nil && enabled
}

// resolveTargets are the named paths, absolute and clean, slash-separated,
// each resolved once: as named, which catches a protected path that is
// itself a symlink, and with its symlinks resolved, which catches a symlink
// that leads to one. A path that cannot be resolved blocks the call: it
// might lead to a protected path.
func resolveTargets(deps Deps, named []string) ([]string, error) {
	var targets []string
	for _, p := range named {
		resolved, err := deps.ResolvePath(p)
		if err != nil {
			return nil, fmt.Errorf("cannot check the tool's path, so the call is blocked: %w", err)
		}
		targets = appendNew(targets, filepath.ToSlash(p), filepath.ToSlash(resolved))
	}
	return targets, nil
}

// checkouts are the repository's worktrees, the acting one first: the role
// rules apply in each, and a protected location of each may lead where a
// write lands (`wt/.pi` a symlink to `wt/conf`), whichever checkout the
// session acts in. The listing costs nothing in a repository without
// linked worktrees (see Deps.OtherWorktrees).
func checkouts(ctx context.Context, deps Deps) ([]tree, error) {
	others, err := deps.OtherWorktrees(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot check the tool's path in every worktree of the repository, so the call is blocked: %w", err)
	}
	return resolveTrees(deps, append([]string{deps.Root}, others...))
}

// tree is the root of a tree of files, slash-separated, as named and with
// its symlinks resolved; resolved is "" when the root reaches nothing.
type tree struct {
	named, resolved string
}

// resolveTrees resolves each worktree root once, and an error naming each
// root that resolveTree cannot compare.
func resolveTrees(deps Deps, roots []string) ([]tree, error) {
	trees := make([]tree, 0, len(roots))
	var unknown []error
	for _, root := range roots {
		t, err := resolveTree(deps, root)
		if err != nil {
			unknown = append(unknown, err)
			continue
		}
		trees = append(trees, t)
	}
	return trees, errors.Join(unknown...)
}

// resolveTree resolves a worktree root. A root that reaches nothing, its
// parent replaced by a dangling symlink or a file, holds no file, nor
// protected location, through its resolved form, so it is compared as
// named: a worktree whose root is gone must not block the calls in every
// other. A root that cannot be resolved otherwise, as one that may not be
// looked at, may hold a protected location that leads anywhere, so it is
// an error, and the call is blocked (see locationSpellings).
func resolveTree(deps Deps, root string) (tree, error) {
	t := tree{named: filepath.ToSlash(filepath.Clean(root))}
	resolved, err := deps.ResolvePath(root)
	switch {
	case err == nil:
		t.resolved = filepath.ToSlash(resolved)
	case !reachesNothing(err):
		return tree{}, fmt.Errorf("cannot resolve the worktree %s, so its protected locations cannot be checked and every write is blocked until it can be: %w; restore access to it (such as with chmod), or, if it is a linked worktree you no longer need, remove it (`git worktree remove --force %s`, or `git worktree prune` once its directory is gone)", t.named, err, t.named)
	}
	return t, nil
}

// reachesNothing reports whether err, from Deps.ResolvePath, says that the
// path reaches nothing (see Deps.ResolvePath).
func reachesNothing(err error) bool {
	var nothing interface{ ReachesNothing() bool }
	return errors.As(err, &nothing) && nothing.ReachesNothing()
}

// treeForms are the roots of trees, each as named and, where it resolves,
// as resolved.
func treeForms(trees []tree) []string {
	var forms []string
	for _, t := range trees {
		forms = appendNew(forms, t.named)
		if t.resolved != "" {
			forms = appendNew(forms, t.resolved)
		}
	}
	return forms
}

// protectedSpellings are the targets, and for each one that lies where a
// protected location of one of trees leads, also that location's spelling
// of it: with `.pi` a symlink to `conf`, a write of `conf/extensions/x.ts`
// is a write of `.pi/extensions/x.ts`, which pi loads. The guard matches
// the protected paths in what the call names, so without this the real
// path of a protected location would be writable. A location of any
// checkout whose lead is unknown, as one that may not be looked at, blocks
// the call, whichever checkout the target lies in: a link from any
// worktree may lead anywhere, the main checkout's `conf` included.
func protectedSpellings(deps Deps, trees []tree, targets []string) ([]string, error) {
	spellings := slices.Clone(targets)
	for _, t := range trees {
		found, err := t.locationSpellings(deps, targets)
		if err != nil {
			return nil, err
		}
		spellings = appendNew(spellings, found...)
	}
	return spellings, nil
}

// locationSpellings are the spellings of targets below where the tree's
// protected locations lead (see respelled), each location resolved once,
// and an error naming each location whose lead is unknown. A tree whose
// root reaches nothing is not looked at (see resolveTree), and a
// location below a file or through a link loop reaches nothing.
func (t tree) locationSpellings(deps Deps, targets []string) ([]string, error) {
	if t.resolved == "" {
		return nil, nil
	}
	var spellings []string
	var unknown []error
	for _, protected := range domain.ProtectedPaths() {
		location := t.named + "/" + protected
		lead, err := deps.ResolveLocation(filepath.FromSlash(t.resolved), protected)
		if err != nil {
			unknown = append(unknown, fmt.Errorf("cannot resolve the protected location %s, so every write is blocked until it can be: %w; restore access to it (such as with chmod), fix or remove the link it leads through, or, if %s is a linked worktree you no longer need, remove it (`git worktree remove --force %s`)", location, err, t.named, t.named))
		}
		if lead != "" {
			spellings = append(spellings, respelled(targets, filepath.ToSlash(lead), location)...)
		}
	}
	return spellings, errors.Join(unknown...)
}

// respelled is each target that lies at or below resolved, spelled below
// location instead (see domain.RebaseFold).
func respelled(targets []string, resolved, location string) []string {
	var out []string
	for _, target := range targets {
		if spelling, ok := domain.RebaseFold(target, resolved, location); ok {
			out = append(out, spelling)
		}
	}
	return out
}

// writesLocalConfig reports whether a file tool writes inside the local
// config directory, which lies outside the repository the guard's rules
// cover and holds the local configs of every repository on the machine.
// The targets are compared with the directory as named and, where it
// resolves, as resolved, without regard to case, so a link in the
// repository does not reach the local configs either.
func writesLocalConfig(deps Deps, call domain.ToolCall) bool {
	if !domain.IsWriteTool(call.Tool) || deps.LocalConfigDir == "" {
		return false
	}
	local := []string{filepath.ToSlash(filepath.Clean(deps.LocalConfigDir))}
	if resolved, err := deps.ResolvePath(deps.LocalConfigDir); err == nil {
		local = appendNew(local, filepath.ToSlash(resolved))
	}
	return slices.ContainsFunc(call.Targets, func(target string) bool {
		return slices.ContainsFunc(local, func(dir string) bool { return domain.WithinFold(target, dir) })
	})
}

// appendNew appends each path list does not hold yet.
func appendNew(list []string, paths ...string) []string {
	for _, p := range paths {
		if !slices.Contains(list, p) {
			list = append(list, p)
		}
	}
	return list
}

// toolPaths lists the paths a tool call names, absolute or relative to the
// session's cwd, cleaned. Grep and Glob without a path search the cwd.
func toolPaths(in hookInput, rules pathRules) ([]string, error) {
	var p string
	switch in.ToolName {
	case "Edit", "Write", "MultiEdit", "Read":
		p = stringField(in.ToolInput, "file_path")
	case "NotebookEdit":
		p = stringField(in.ToolInput, "notebook_path")
	case "Grep", "Glob":
		p = cmp.Or(stringField(in.ToolInput, "path"), ".")
	}
	if p == "" {
		return nil, nil
	}
	p, err := joinToolPath(in.Cwd, p, rules)
	if err != nil {
		return nil, err
	}
	if !rules.isAbs(p) {
		return nil, fmt.Errorf("cannot check the tool's path %s: it is not absolute, so the hook event's cwd is not either; run the session from an absolute directory", p)
	}
	if hasParentPart(p) {
		return nil, fmt.Errorf("the tool's path %s has a '..' part, which the hook cannot check: the OS follows a symlink before '..', so the path may lead elsewhere than it reads; name the path without '..', such as its absolute path", p)
	}
	return []string{filepath.Clean(p)}, nil
}

// pathRules are how an OS reads a path: hostPaths for the OS the engine
// runs on. Tests give Windows' rules on any OS.
type pathRules struct {
	isAbs       func(string) bool
	volumeName  func(string) string
	isSeparator func(byte) bool
	separator   byte
}

var hostPaths = pathRules{isAbs: filepath.IsAbs, volumeName: filepath.VolumeName, isSeparator: os.IsPathSeparator, separator: filepath.Separator}

// joinToolPath is p as the OS reads it from cwd: an absolute path as it
// is, a plainly relative one joined to cwd. Anything else is refused, as
// Windows' rooted `\repo\x` (on the drive of the process, not of cwd) and
// drive-relative `C:x` (from drive C's own working directory): the OS does
// not read them from cwd, so a joined path would check another file than
// the tool opens.
func joinToolPath(cwd, p string, rules pathRules) (string, error) {
	switch {
	case rules.isAbs(p):
		return p, nil
	case rules.volumeName(p) != "" || rules.isSeparator(p[0]):
		return "", fmt.Errorf("the tool's path %s is neither absolute nor plainly relative (it names a drive or starts at a root without one), so the hook cannot tell which file the OS opens; name the path in full, such as C:\\repo\\file", p)
	}
	return cwd + string(rules.separator) + p, nil
}

// hasParentPart reports whether p has a ".." part, between any separators
// the OS accepts.
func hasParentPart(p string) bool {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == filepath.Separator })
	return slices.Contains(parts, "..")
}
