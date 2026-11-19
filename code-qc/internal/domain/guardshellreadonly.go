package domain

import (
	"regexp"
	"slices"
	"strings"
)

// readOnlyCommands only read files and run nothing else, whatever their
// arguments: none of GNU coreutils' and BSD's ls, cat, head, tail, wc,
// du, stat, readlink, realpath, basename and dirname, GNU grep, or bash's
// and zsh's echo and true has an option that writes a file, runs a
// program or sets a shell variable. A command of them changes a file only
// through a redirection of its output. printf is not one (`printf -v
// HOME` sets a variable, and with it where `cd` goes), nor is file
// (`--compile`, abbreviated as `--co`, writes a magic file).
var readOnlyCommands = []string{
	"ls", "cat", "head", "tail", "wc", "grep", "egrep", "fgrep", "du", "stat", "readlink", "realpath",
	"basename", "dirname", "echo", "true",
}

// checkedReadCommands only read files unless an argument makes them run a
// program, write or set a variable: each is read-only when its check
// passes on its arguments, and none of them is (see shellCommand.globbed),
// since a glob may expand into an option. popd is not one: it has no
// argument to check, and goes wherever the directory stack says.
var checkedReadCommands = map[string]func(args []string) bool{
	"find":  readOnlyFind,
	"rg":    readOnlyRipgrep,
	"git":   readOnlyGit,
	"cd":    readOnlyChdir,
	"pushd": readOnlyChdir,
	"test":  readOnlyTest,
	"[":     readOnlyTest,
}

// gitDiffOutput are the read-only options that shape the diff git log,
// diff and show print.
var gitDiffOutput = readOnlyOptions{
	syntax:   separateShorts,
	flags:    []string{"-p", "--patch", "--name-only", "--name-status", "--no-color"},
	attached: []string{"--stat", "-U", "--unified", "--word-diff", "--color"},
}

// readOnlyGitCommands are the git commands that only read, each with the
// options that keep it so; any other, such as `--output` or
// `--ext-diff`, may write a file or run a program.
var readOnlyGitCommands = map[string]readOnlyOptions{
	"status": {
		syntax:   separateShorts,
		flags:    []string{"-s", "--short", "-b", "--branch", "--long"},
		attached: []string{"--porcelain", "-u", "--untracked-files", "--ignored"},
	},
	"log": gitDiffOutput.with(readOnlyOptions{
		flags:    []string{"--oneline", "--graph", "--all", "--follow"},
		attached: []string{"--decorate", "--format", "--pretty"},
		valued:   []string{"-n", "--max-count", "--since", "--until", "--after", "--before", "--author", "--grep"},
	}),
	"diff": gitDiffOutput.with(readOnlyOptions{flags: []string{"--cached", "--staged"}}),
	"show": gitDiffOutput.with(readOnlyOptions{
		flags:    []string{"--oneline"},
		attached: []string{"--format", "--pretty"},
	}),
	"rev-parse": {
		syntax:   separateShorts,
		flags:    []string{"--show-toplevel", "--verify", "-q", "--quiet"},
		attached: []string{"--abbrev-ref", "--short"},
	},
	"ls-files": {
		syntax: separateShorts,
		flags: []string{
			"-o", "--others", "-m", "--modified", "-d", "--deleted", "-c", "--cached", "--exclude-standard", "-z",
		},
	},
}

// readOnlyGitOptions are git's own options that change neither its config
// nor where it finds its commands; `-C <dir>` is read apart.
var readOnlyGitOptions = []string{"--no-pager", "-P", "--no-optional-locks"}

// ripgrepReadOnly are rg's options that only choose what it searches and
// how it prints; any other, such as `--pre` (runs a preprocessor),
// `--hostname-bin` (runs a program) or `-z` (runs decompressors), is not
// read-only.
var ripgrepReadOnly = readOnlyOptions{
	syntax: bundledShorts,
	flags: []string{
		"-n", "--line-number", "-i", "--ignore-case", "-S", "--smart-case", "-l", "--files-with-matches",
		"-c", "--count", "-w", "--word-regexp", "-F", "--fixed-strings", "--hidden", "--no-ignore", "-u",
		"--unrestricted", "--files", "--json", "-0", "--null",
	},
	valued: []string{
		"-e", "--regexp", "-g", "--glob", "-t", "--type", "-A", "--after-context", "-B", "--before-context",
		"-C", "--context", "-m", "--max-count", "--max-depth", "--color",
	},
}

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
// not count (`#`), a backslash, which with a newline after it joins
// lines, even inside double quotes, and a parenthesis, quoted or not.
// zsh reads parentheses after a word as glob qualifiers, some of which
// run code (`ls *(e:'rm x':)`); bash and zsh read them as a subshell, an
// arithmetic command (`(( PATH = 0 ))`) or a function definition
// (`ls () …`), which set variables or define functions.
const unscannedSyntax = "$`#\\()"

// provablyReadOnly reports whether a shell command provably changes no
// protected path, so that the shell guard need not deny it for naming
// one. The scanner must read it as the shell does (see
// scannedAsShellReads); each of its simple commands, in pipelines and
// lists alike, is one of readOnlyCommands or passes its check in
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

// readOnlyRipgrep checks rg's arguments: only the options of
// ripgrepReadOnly.
func readOnlyRipgrep(args []string) bool {
	return ripgrepReadOnly.allow(args)
}

// readOnlyTest is the check of test and [: no `-v`, and no argument that
// holds a `[`. bash evaluates the subscript of an array element `-v`
// tests as arithmetic, which can assign (`[ -v 'a[HOME=7]' ]` sets
// HOME), so neither command sets a variable.
func readOnlyTest(args []string) bool {
	return !slices.ContainsFunc(args, func(arg string) bool { return arg == "-v" || strings.Contains(arg, "[") })
}

// readOnlyGit checks git's arguments: only the own options of
// readOnlyGitOptions and `-C <dir>`, then one of readOnlyGitCommands with
// only the options that keep it read-only.
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
	if i == len(args) {
		return false
	}
	options, ok := readOnlyGitCommands[args[i]]
	return ok && options.allow(args[i+1:])
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
