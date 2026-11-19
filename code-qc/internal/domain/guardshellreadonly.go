package domain

import (
	"slices"
	"strings"
	"unicode"
)

// readOnlyCommands only read files, whatever their arguments: a command
// of them changes a file only through a redirection of its output.
var readOnlyCommands = []string{
	"ls", "cat", "head", "tail", "wc", "grep", "egrep", "fgrep", "du", "stat", "readlink", "realpath",
	"basename", "dirname", "echo", "printf", "true", "test", "[",
}

// checkedReadCommands only read files unless an argument makes them run a
// program or write: each is read-only when its check passes on its
// arguments, and none of them is (see shellCommand.globbed), since a glob
// may expand into an option.
var checkedReadCommands = map[string]func(args []string) bool{
	"find":  noArgStarting("-exec", "-ok", "-delete", "-fprint", "-fls"),
	"rg":    noArgStarting("--pre", "--hostname-bin"),
	"file":  readOnlyFile,
	"git":   readOnlyGit,
	"cd":    readOnlyChdir(0, 1),
	"pushd": readOnlyChdir(1, 1),
	"popd":  readOnlyChdir(0, 0),
}

// readOnlyGitCommands are the git commands that only read, unless an
// output option (see isGitOutputOption) makes them write a file.
var readOnlyGitCommands = []string{"status", "log", "diff", "show", "rev-parse", "ls-files"}

// readOnlyGitOptions are git's own options that change neither its config
// nor where it finds its commands; `-C <dir>` is read apart.
var readOnlyGitOptions = []string{"--no-pager", "-P", "--no-optional-locks"}

// provablyReadOnly reports whether a shell command provably changes no
// protected path, so that the shell guard need not deny it for naming
// one: the whole command holds no expansion (no `$`, no backquote) and no
// space the scanner splits at but the shell does not (see
// scannerOnlySpace), each of its simple commands, in pipelines, lists and
// subshells alike, is one of readOnlyCommands or passes its check in
// checkedReadCommands, named literally, and every redirection writes to
// no protected path (see safeRedirect). Anything else, `eval`, a shell, a
// variable assignment, a shell keyword or a program it does not know, is
// not read-only: in doubt, it is not.
func provablyReadOnly(command string) bool {
	if strings.ContainsAny(command, "$`") || strings.ContainsFunc(command, scannerOnlySpace) {
		return false
	}
	scan := scanShell(command)
	return !slices.ContainsFunc(scan.commands, func(c shellCommand) bool { return !readOnlyCommand(c) }) &&
		!slices.ContainsFunc(scan.redirects, func(r shellRedirect) bool { return !safeRedirect(r) })
}

// scannerOnlySpace reports whether r is white space the scanner splits
// words at but the shell does not, which splits only at a space, a tab and
// a newline: with `ls\rx`, the scanner reads `ls`, the shell a command
// named `ls\rx`.
func scannerOnlySpace(r rune) bool {
	return unicode.IsSpace(r) && r != ' ' && r != '\t' && r != '\n'
}

// readOnlyCommand reports whether a simple command only reads files.
func readOnlyCommand(c shellCommand) bool {
	name, args := c.words[0], c.words[1:]
	if slices.Contains(readOnlyCommands, name) {
		return true
	}
	check, ok := checkedReadCommands[name]
	return ok && !c.globbed && check(args)
}

// noArgStarting is a check that passes when no argument starts with one
// of prefixes (`-exec` and `-execdir` of find).
func noArgStarting(prefixes ...string) func([]string) bool {
	return func(args []string) bool {
		return !slices.ContainsFunc(args, func(arg string) bool {
			return slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(arg, prefix) })
		})
	}
}

// readOnlyFile checks file's arguments: `-C` (`--compile`), alone or in a
// bundle of short options, writes a compiled magic file.
func readOnlyFile(args []string) bool {
	return !slices.ContainsFunc(args, func(arg string) bool {
		short := strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--")
		return arg == "--compile" || (short && strings.ContainsRune(arg, 'C'))
	})
}

// readOnlyGit checks git's arguments: only the own options of
// readOnlyGitOptions and `-C <dir>`, one of readOnlyGitCommands, and no
// output option after it.
func readOnlyGit(args []string) bool {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		switch {
		case args[i] == "-C" && i+1 < len(args):
			i += 2
		case slices.Contains(readOnlyGitOptions, args[i]):
			i++
		default:
			return false
		}
	}
	return i < len(args) && slices.Contains(readOnlyGitCommands, args[i]) && !slices.ContainsFunc(args[i+1:], isGitOutputOption)
}

// isGitOutputOption reports whether arg makes a git command write its
// output to a file (`--output=x`, `-o x`).
func isGitOutputOption(arg string) bool {
	return arg == "-o" || strings.HasPrefix(arg, "--output")
}

// readOnlyChdir is the check of cd, pushd or popd: from fewest to most
// arguments, each a directory that is not an option (`cd -` returns to
// where the shell was) and is a safe path (see safeShellPath): a command
// that runs in a leading part of a protected path writes inside it with
// relative paths (`cd .claude && tee settings.json`).
func readOnlyChdir(fewest, most int) func([]string) bool {
	return func(args []string) bool {
		return len(args) >= fewest && len(args) <= most && !slices.ContainsFunc(args, func(arg string) bool {
			return strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "+") || !safeShellPath(arg)
		})
	}
}

// safeRedirect reports whether a redirection writes to no protected path:
// it duplicates or closes a file descriptor (`2>&1`, `>&-`), or its target
// is a safe path (see safeShellPath), such as `/dev/null` or
// `/tmp/out.txt`.
func safeRedirect(r shellRedirect) bool {
	if strings.Contains(r.op, "&") && (isDigits(r.target) || r.target == "-") {
		return true
	}
	return safeShellPath(r.target)
}

// safeShellPath reports whether p, as the shell reads it, names a path
// outside every protected path and every leading part of one: it is
// given, holds no glob or brace the shell could expand (see globChars),
// holds no protected path, and holds no leading part of one as segments
// anywhere (`.claude`, `sub/.pi/x`), matched as ProtectedPathIn matches
// and with backslashes read as Win32 reads them.
func safeShellPath(p string) bool {
	if p == "" || strings.ContainsAny(p, globChars) {
		return false
	}
	slashed := strings.ReplaceAll(p, `\`, "/")
	if _, ok := ProtectedPathIn(slashed); ok {
		return false
	}
	return !slices.ContainsFunc(ProtectedPaths(), func(protected string) bool {
		return slices.ContainsFunc(properLeads(protected), func(lead string) bool { return hasSegmentsFold(slashed, lead) })
	})
}
