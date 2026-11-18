package cli

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"millennium-code-qc/internal/domain"
)

// locate fills in where the paths a call names lie, as far as the guard's
// rules need (domain.PathNeedOf), so a call no path rule applies to costs
// no file system or git work: a write gets its targets, each path as named
// and resolved; a role rule also the paths in every worktree.
func locate(ctx context.Context, deps Deps, call domain.ToolCall, named []string) (domain.ToolCall, error) {
	need := domain.PathNeedOf(call.Tool, call.Role)
	if need == domain.NeedNoPaths || len(named) == 0 {
		return call, nil
	}
	targets, err := resolveTargets(deps, named)
	if err != nil {
		return call, err
	}
	call.Targets = targets
	if need == domain.NeedWorktreePaths {
		call.Paths, err = worktreePaths(ctx, deps, targets)
	}
	return call, err
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

// worktreePaths places the targets in the repository's worktrees for the
// role rules, which apply in every worktree. Only an opted-in repository
// has rules, so elsewhere the worktrees are neither listed nor resolved,
// and a broken one cannot block calls there. A config that cannot be read
// needs no paths either: the guard then denies every write and allows
// reads.
func worktreePaths(ctx context.Context, deps Deps, targets []string) ([]string, error) {
	if _, enabled, err := deps.Config.Execute(ctx); err != nil || !enabled {
		return nil, nil
	}
	others, err := deps.OtherWorktrees(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot check the tool's path in every worktree of the repository, so the call is blocked: %w", err)
	}
	return domain.WorktreePaths(targets, treeForms(deps, append([]string{deps.Root}, others...)...)), nil
}

// treeForms are the roots of trees, slash-separated, each as named and,
// where it resolves, with its symlinks resolved, each resolved once. A root
// that cannot be resolved, its parent replaced by a dangling symlink or a
// file, reaches no file through its resolved form, so it is compared as
// named: one broken worktree must not block the calls in every other.
func treeForms(deps Deps, roots ...string) []string {
	var forms []string
	for _, root := range roots {
		forms = appendNew(forms, filepath.ToSlash(filepath.Clean(root)))
		if resolved, err := deps.ResolvePath(root); err == nil {
			forms = appendNew(forms, filepath.ToSlash(resolved))
		}
	}
	return forms
}

// writesLocalConfig reports whether a file tool writes inside the local
// config directory, which lies outside the repository the guard's rules
// cover and holds the local configs of every repository on the machine.
// The targets are compared with the directory as named and resolved,
// without regard to case, so a link in the repository does not reach the
// local configs either.
func writesLocalConfig(deps Deps, call domain.ToolCall) bool {
	if !domain.IsWriteTool(call.Tool) || deps.LocalConfigDir == "" {
		return false
	}
	local := treeForms(deps, deps.LocalConfigDir)
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
