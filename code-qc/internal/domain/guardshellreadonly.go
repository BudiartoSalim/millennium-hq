package domain

import (
	"slices"
	"strings"
)

// readOnlyCommands only read files and run nothing else, whatever their
// arguments: none of GNU coreutils' and BSD's ls, cat, head, tail, wc,
// du, stat, readlink, realpath, basename and dirname, or bash's and zsh's
// echo and true has an option that writes a file, runs a program or sets
// a shell variable. A command of them changes a file only through a
// redirection of its output. printf is not one (`printf -v HOME` sets a
// variable, and with it where `cd` goes), nor is file (`--compile`,
// abbreviated as `--co`, writes a magic file), nor grep, which is ugrep
// in Claude Code (`--filter` runs a program, `--save-config` writes a
// file; see grepReadOnly).
var readOnlyCommands = []string{
	"ls", "cat", "head", "tail", "wc", "du", "stat", "readlink", "realpath", "basename", "dirname", "echo", "true",
}

// checkedReadCommands only read files unless an argument makes them run a
// program, write or set a variable: each is read-only when its check
// passes on its arguments, and none of them is (see shellCommand.globbed),
// since a glob may expand into an option. popd is not one: it has no
// argument to check, and goes wherever the directory stack says.
var checkedReadCommands = map[string]func(args []string) bool{
	"find":  readOnlyFind,
	"grep":  readOnlyGrep,
	"egrep": readOnlyGrep,
	"fgrep": readOnlyGrep,
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
// `--ext-diff`, may write a file or run a program. An option that takes a
// value takes it only attached (see readOnlyOptions.attachedValue).
var readOnlyGitCommands = map[string]readOnlyOptions{
	"status": {
		syntax:   separateShorts,
		flags:    []string{"-s", "--short", "-b", "--branch", "--long"},
		attached: []string{"--porcelain", "-u", "--untracked-files", "--ignored"},
	},
	"log": readOnlyOptions{
		syntax:   separateShortsAndCount,
		flags:    []string{"--oneline", "--graph", "--all", "--follow"},
		attached: []string{"--decorate", "--pretty"},
		attachedValue: []string{
			"-n", "--max-count", "--since", "--until", "--after", "--before", "--author", "--grep", "--format",
		},
	}.with(gitDiffOutput),
	"diff": gitDiffOutput.with(readOnlyOptions{flags: []string{"--cached", "--staged"}}),
	"show": gitDiffOutput.with(readOnlyOptions{
		flags:         []string{"--oneline"},
		attached:      []string{"--pretty"},
		attachedValue: []string{"--format"},
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

// grepReadOnly are the options of grep, egrep and fgrep that only choose
// what they search and how they print, in both GNU grep and ugrep (which
// Claude Code runs as grep); either reads short options bundled, and a
// value attached or as the next word. Any other is not read-only: ugrep's
// `--filter` runs programs (`--filter-magic-label` picks which),
// `--save-config` writes a file, `--config` and `---FILE` read options
// from one, `--pager` and `--view` start programs, and `-z` and `-Z` mean
// one thing in GNU grep and another in ugrep (decompression, fuzzy
// matching).
var grepReadOnly = readOnlyOptions{
	syntax: bundledShorts,
	flags: []string{
		"-r", "-R", "-n", "-i", "-l", "-L", "-c", "-w", "-x", "-v", "-E", "-F", "-G", "-P", "-s", "-q", "-H", "-h",
		"-o", "-a", "-I", "--null", "--line-number", "--ignore-case", "--files-with-matches", "--files-without-match",
		"--count", "--word-regexp", "--line-regexp", "--invert-match", "--extended-regexp", "--fixed-strings",
		"--perl-regexp", "--recursive", "--dereference-recursive", "--no-messages", "--quiet", "--silent",
		"--with-filename", "--no-filename", "--only-matching", "--text",
	},
	attached: []string{"--color", "--colour"},
	valued: []string{
		"-A", "-B", "-C", "-e", "-f", "-m", "--include", "--exclude", "--exclude-dir", "--include-dir",
		"--binary-files", "--regexp", "--file", "--max-count", "--after-context", "--before-context", "--context",
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

// provablyReadOnly reports whether a shell command provably changes no
// protected path, so that the shell guard need not deny it for naming
// one. Its text must be inert (see inertShellText), so that the scanner
// reads it as bash and zsh do; each of its simple commands, in pipelines
// and lists alike, is one of readOnlyCommands or passes its check in
// checkedReadCommands, named literally; and every redirection writes to
// no protected path (see safeRedirect). Anything else, `eval`, a shell, a
// variable assignment, a shell keyword or a program it does not know, is
// not read-only: in doubt, it is not.
func provablyReadOnly(command string) bool {
	if !inertShellText(command) {
		return false
	}
	scan := scanShell(command)
	return !slices.ContainsFunc(scan.commands, func(c shellCommand) bool { return !readOnlyCommand(c) }) &&
		!slices.ContainsFunc(scan.redirects, func(r shellRedirect) bool { return !safeRedirect(r) })
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

// readOnlyGrep checks the arguments of grep, egrep and fgrep: only the
// options of grepReadOnly (R11-SEC-3).
func readOnlyGrep(args []string) bool {
	return grepReadOnly.allow(args)
}

// readOnlyTest is the check of test and [: no `-v`, and no argument that
// holds a `[`. bash evaluates the subscript of an array element `-v`
// tests as arithmetic, which can assign (`[ -v 'a[HOME=7]' ]` sets
// HOME), so neither command sets a variable.
func readOnlyTest(args []string) bool {
	return !slices.ContainsFunc(args, func(arg string) bool { return arg == "-v" || strings.Contains(arg, "[") })
}

// readOnlyGit checks git's arguments: none is an output option (see
// isGitOutputOption); only the own options of readOnlyGitOptions and
// `-C <dir>`, then one of readOnlyGitCommands with only the options that
// keep it read-only.
func readOnlyGit(args []string) bool {
	if slices.ContainsFunc(args, isGitOutputOption) {
		return false
	}
	i, ok := gitCommandIndex(args)
	if !ok {
		return false
	}
	options, ok := readOnlyGitCommands[args[i]]
	return ok && options.allow(args[i+1:])
}

// gitCommandIndex is the index of git's command among args, past its own
// options, which must be those of readOnlyGitOptions or `-C <dir>` with a
// dir that does not start with a dash; false when one is not, or no
// command follows.
func gitCommandIndex(args []string) (int, bool) {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		switch {
		case args[i] == "-C" && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-"):
			i += 2
		case slices.Contains(readOnlyGitOptions, args[i]):
			i++
		default:
			return 0, false
		}
	}
	return i, i < len(args)
}

// isGitOutputOption reports whether arg, a word after git, is git's
// `--output`, with a value attached or not, or an abbreviation of it
// (`--ou`, `--outp=x`), which writes the diff to a file. The option
// allowlists refuse it already wherever they read an option; this check
// refuses it wherever it stands, after `--` included. git reads its arguments in more than one pass (git log reads
// its own options before its revision options), so a word the allowlist
// reads as an operand or a value may be one git reads as an option; this
// is the defense should the allowlist's reading and git's part again
// (round 10, R10-CR-1).
func isGitOutputOption(arg string) bool {
	name, _, _ := strings.Cut(arg, "=")
	return len(name) > len("--") && strings.HasPrefix("--output", name)
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
