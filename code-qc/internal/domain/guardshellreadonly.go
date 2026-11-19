package domain

import (
	"regexp"
	"slices"
	"strings"
)

// readOnlyCommands only read files and run nothing else, whatever their
// arguments: none of GNU coreutils' and BSD's ls, cat, head, tail, wc,
// du, stat, readlink, realpath, basename and dirname, GNU grep, or bash's
// and zsh's echo, true, test and [ has an option that writes a file, runs
// a program or sets a shell variable. A command of them changes a file
// only through a redirection of its output. printf is not one (`printf -v
// HOME` sets a variable, and with it where `cd` goes), nor is file
// (`--compile`, abbreviated as `--co`, writes a magic file).
var readOnlyCommands = []string{
	"ls", "cat", "head", "tail", "wc", "grep", "egrep", "fgrep", "du", "stat", "readlink", "realpath",
	"basename", "dirname", "echo", "true", "test", "[",
}

// checkedReadCommands only read files unless an argument makes them run a
// program or write: each is read-only when its check passes on its
// arguments, and none of them is (see shellCommand.globbed), since a glob
// may expand into an option. popd is not one: it has no argument to check,
// and goes wherever the directory stack says.
var checkedReadCommands = map[string]func(args []string) bool{
	"find":  readOnlyFind,
	"rg":    readOnlyRipgrep,
	"git":   readOnlyGit,
	"cd":    readOnlyChdir,
	"pushd": readOnlyChdir,
}

// readOnlyGitCommands are the git commands that only read, unless an
// output option (see isGitOutputOption) makes them write a file.
var readOnlyGitCommands = []string{"status", "log", "diff", "show", "rev-parse", "ls-files"}

// readOnlyGitOptions are git's own options that change neither its config
// nor where it finds its commands; `-C <dir>` is read apart.
var readOnlyGitOptions = []string{"--no-pager", "-P", "--no-optional-locks"}

// gitOutputOptions are the long options that make a git command write a
// file.
var gitOutputOptions = []string{"--output"}

// ripgrepRunning are rg's long options that run a program (a
// preprocessor, a decompressor, the command that names the host), or
// choose which files a preprocessor runs on.
var ripgrepRunning = []string{"--pre", "--pre-glob", "--hostname-bin", "--search-zip"}

// findReadOnly are the options, tests, operators and actions of GNU find
// and bfs that start with a dash and only test files or print to
// standard output; every other one, such as `-exec`, `-delete`,
// `-fprint` or bfs's `-rm`, may run a program or write.
var findReadOnly = []string{
	"-L", "-H", "-P",
	"-name", "-iname", "-path", "-ipath", "-wholename", "-iwholename", "-regex", "-iregex", "-regextype",
	"-lname", "-ilname", "-type", "-xtype", "-maxdepth", "-mindepth", "-depth", "-mount", "-xdev",
	"-newer", "-anewer", "-cnewer", "-mtime", "-mmin", "-atime", "-amin", "-ctime", "-cmin", "-size", "-empty",
	"-perm", "-user", "-group", "-uid", "-gid", "-nouser", "-nogroup", "-links", "-inum", "-samefile",
	"-readable", "-writable", "-executable", "-true", "-false", "-follow", "-noleaf", "-daystart",
	"-prune", "-quit", "-o", "-or", "-a", "-and", "-not", "-print", "-print0", "-printf", "-ls",
}

// namedDescriptor matches a word of the form `{name}` right before a
// redirection operator, as far as the text shows: the shell then assigns
// the descriptor it opens to the variable name (`{PATH}>/dev/null`).
var namedDescriptor = regexp.MustCompile(`\}[<>&]`)

// unscannedSyntax are characters the scanner does not read as the shell
// does: an expansion (`$`, a backquote), a comment, in which quotes do
// not count (`#`), and a backslash, which with a newline after it joins
// lines, even inside double quotes.
const unscannedSyntax = "$`#\\"

// provablyReadOnly reports whether a shell command provably changes no
// protected path, so that the shell guard need not deny it for naming
// one. The scanner must read it as the shell does (see
// scannedAsShellReads); each of its simple commands, in pipelines, lists
// and subshells alike, is one of readOnlyCommands or passes its check in
// checkedReadCommands, named literally; and every redirection writes to
// no protected path (see safeRedirect). Anything else, `eval`, a shell, a
// variable assignment, a shell keyword or a program it does not know, is
// not read-only: in doubt, it is not.
func provablyReadOnly(command string) bool {
	if !scannedAsShellReads(command) {
		return false
	}
	scan := scanShell(command)
	return !slices.ContainsFunc(scan.commands, func(c shellCommand) bool { return !readOnlyCommand(c) }) &&
		!slices.ContainsFunc(scan.redirects, func(r shellRedirect) bool { return !safeRedirect(r) })
}

// scannedAsShellReads reports whether the scanner provably reads command
// as the shell does: it is one line of printable ASCII, spaces and tabs
// (outside ASCII, a shell in another locale may read a byte after one
// as part of a character, and the scanner splits only at spaces and
// tabs), and holds none of unscannedSyntax, no here-document or
// here-string (`<<`, whose body the shell reads by lines), and no
// variable a redirection assigns (see namedDescriptor).
func scannedAsShellReads(command string) bool {
	return !strings.ContainsFunc(command, unscannedRune) && !strings.ContainsAny(command, unscannedSyntax) &&
		!strings.Contains(command, "<<") && !namedDescriptor.MatchString(command)
}

// unscannedRune reports whether r is neither printable ASCII, a space
// nor a tab.
func unscannedRune(r rune) bool {
	return (r < ' ' || r > '~') && r != '\t'
}

// readOnlyCommand reports whether a simple command only reads files. A
// variable assignment (`HOME=x cd`) is never a command's name, so a
// read-only command sets no variable.
func readOnlyCommand(c shellCommand) bool {
	name, args := c.words[0], c.words[1:]
	if slices.Contains(readOnlyCommands, name) {
		return true
	}
	check, ok := checkedReadCommands[name]
	return ok && !c.globbed && check(args)
}

// readOnlyFind checks find's arguments: each that starts with a dash is
// one of findReadOnly, `-newerXY`, or a number (`-mtime -1`). One that
// does not is a starting point or a value; values that start with a dash
// are read as primaries, which only refuses more.
func readOnlyFind(args []string) bool {
	return !slices.ContainsFunc(args, func(arg string) bool {
		return strings.HasPrefix(arg, "-") && !slices.Contains(findReadOnly, arg) && !isFindNewerXY(arg) && !isNegativeNumber(arg)
	})
}

// isFindNewerXY reports whether arg is find's `-newerXY`, which compares
// times (`-newermt`).
func isFindNewerXY(arg string) bool {
	kinds, ok := strings.CutPrefix(arg, "-newer")
	return ok && len(kinds) == 2 && strings.Trim(kinds, "aBcmt") == ""
}

// isNegativeNumber reports whether arg starts with a dash and a digit, as
// the value of a numeric test does (`-1`, `-10k`); no option does.
func isNegativeNumber(arg string) bool {
	return len(arg) > 1 && arg[0] == '-' && arg[1] >= '0' && arg[1] <= '9'
}

// readOnlyRipgrep checks rg's arguments: none of ripgrepRunning, and no
// bundle of short options with `-z` (`--search-zip`) in it.
func readOnlyRipgrep(args []string) bool {
	return !slices.ContainsFunc(args, func(arg string) bool {
		return namesLongOption(arg, ripgrepRunning) || (isShortOptions(arg) && strings.ContainsRune(arg, 'z'))
	})
}

// namesLongOption reports whether arg is one of longs, with or without a
// value, an option that starts with one, or an abbreviation of one:
// getopt_long and git accept any unambiguous prefix (`--outp` for
// `--output`).
func namesLongOption(arg string, longs []string) bool {
	name, _, _ := strings.Cut(arg, "=")
	if len(name) <= len("--") || !strings.HasPrefix(name, "--") {
		return false
	}
	return slices.ContainsFunc(longs, func(long string) bool {
		return strings.HasPrefix(long, name) || strings.HasPrefix(name, long)
	})
}

// isShortOptions reports whether arg is a bundle of short options (`-z`,
// `-uz`).
func isShortOptions(arg string) bool {
	return len(arg) > 1 && arg[0] == '-' && arg[1] != '-'
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
// output to a file (`--output=x`, its abbreviation `--outp=x`, `-o x`).
func isGitOutputOption(arg string) bool {
	return arg == "-o" || namesLongOption(arg, gitOutputOptions)
}

// readOnlyChdir is the check of cd and pushd: one directory, given
// explicitly (see explicitDirectory), that is a safe path (see
// safeShellPath). A command that runs in a leading part of a protected
// path writes inside it with relative paths (`cd .claude && tee
// settings.json`); with no directory, `~`, `-` or a bare name, the shell
// goes where HOME, OLDPWD or CDPATH say, which the command may have set.
func readOnlyChdir(args []string) bool {
	return len(args) == 1 && explicitDirectory(args[0]) && safeShellPath(args[0])
}

// explicitDirectory reports whether dir is a path the shell enters as
// written, never searching CDPATH for it: absolute, `.`, `..`, or
// starting with `./` or `../`.
func explicitDirectory(dir string) bool {
	return dir == "." || dir == ".." || strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, "./") || strings.HasPrefix(dir, "../")
}

// safeRedirect reports whether a redirection writes to no protected path:
// it duplicates or closes a file descriptor (`2>&1`, `>&-`), or its target
// is a safe path (see safeShellPath), such as `/dev/null` or
// `/tmp/out.txt`, that does not start with `!` (zsh reads `>!` as an
// operator that writes the word after it).
func safeRedirect(r shellRedirect) bool {
	if strings.Contains(r.op, "&") && (isDigits(r.target) || r.target == "-") {
		return true
	}
	return !strings.HasPrefix(r.target, "!") && safeShellPath(r.target)
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
