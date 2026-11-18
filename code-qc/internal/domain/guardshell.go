package domain

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

var (
	// shellWrite matches shell commands that can change a file.
	shellWrite = regexp.MustCompile(`>|\btee\b|\bsed\s+(-\S*\s+)*-i|\b(mv|cp|rm|ln|chmod|truncate|install|dd)\b|\bgit\s+(checkout|restore|rm|mv|apply)\b`)
	// applyCommand matches the human-only command that changes the rules.
	applyCommand = regexp.MustCompile(`\bmillennium-code-qc(\.exe)?["']?\s+config\s+apply\b`)
)

// replacingCommands put, move or remove something at their operands: at
// `.pi` or `.claude`, a link or a directory that brings its own protected
// paths (`ln -s conf .pi`, `mv conf .claude`), or removes the real ones.
var replacingCommands = []string{"ln", "mv", "cp", "rm", "install"}

// gitReplacing are the git commands that put, move, remove or restore
// files at their operands (`git checkout main -- .claude`).
var gitReplacing = []string{"rm", "mv", "checkout", "restore"}

// gitValueOptions are git's own options whose value is the next word
// (`git -C sub rm .pi`).
var gitValueOptions = []string{"-C", "-c", "--git-dir", "--work-tree", "--namespace"}

// commandPrefixes run the command after them, so the command that runs is
// the next word; shell keywords that start a command are skipped the same
// way.
var commandPrefixes = []string{"sudo", "doas", "env", "command", "exec", "nohup", "nice", "time", "xargs", "!", "{", "if", "then", "else", "elif", "do", "while", "until"}

// shells run a script given with -c, whose commands are matched as well.
var shells = []string{"sh", "bash", "dash", "zsh", "ksh"}

// maxScriptDepth bounds how deep scripts inside scripts (`eval`, `sh -c`)
// are matched; one deeper is not placed (see protectedInCommand).
const maxScriptDepth = 4

// guardShell matches a Bash call's command; it reads no paths.
func guardShell(_ Config, call ToolCall) GuardDecision {
	command := call.Command
	if applyCommand.MatchString(command) {
		return deny("only the user runs `millennium-code-qc config apply`; write a proposal instead and ask them to apply it")
	}
	if protected, ok := protectedInCommand(command); ok {
		return deny("this command looks like it changes %s, which no agent may change", protected)
	}
	return GuardDecision{}
}

// protectedInCommand is the protected path a shell command looks like it
// changes, without regard to case (see matchScript). Where the matcher
// cannot place what a command changes (see unplaced), it falls back to a
// coarse rule for the whole command: one that can write, and a word or a
// part of one anywhere (`$(echo .pi)`) that is a protected path or a
// leading part of one. A command that only reads `.pi` or `.claude`, such
// as `ls -la .claude 2>/dev/null`, changes neither.
func protectedInCommand(command string) (string, bool) {
	m := matchScript(command, 0)
	if m.protected != "" {
		return m.protected, true
	}
	if m.unplaced && writeLike(command, m.words) {
		return protectedWord(shellFields(m.words), protectedOrLeadIn)
	}
	return "", false
}

// shellMatch is what matching a shell script found.
type shellMatch struct {
	// protected is what it changes, "" for nothing found.
	protected string
	// unplaced: a command in it changes what the matcher cannot place.
	unplaced bool
	// words are the words of the script and of the scripts inside it,
	// redirection targets included.
	words []string
}

// add adds what matching a script inside the script found.
func (m *shellMatch) add(inner shellMatch) {
	if m.protected == "" {
		m.protected = inner.protected
	}
	m.unplaced = m.unplaced || inner.unplaced
	m.words = append(m.words, inner.words...)
}

// matchScript matches a shell script at depth, the number of scripts it
// lies inside: a script that can write and names a protected path, in its
// text or as a word with its quotes removed (`'.claude'/settings.json`),
// a redirection's target included, changes it; so does each simple
// command that changes one (see matchSimpleCommand).
func matchScript(script string, depth int) shellMatch {
	commands, words := scanShell(script)
	m := shellMatch{words: words}
	if writeLike(script, words) {
		m.protected, _ = protectedNamed(script, words)
	}
	for i := 0; m.protected == "" && i < len(commands); i++ {
		m.add(matchSimpleCommand(commands[i], depth))
	}
	return m
}

// writeLike reports whether a script, as written or as its words, can
// change a file.
func writeLike(script string, words []string) bool {
	return shellWrite.MatchString(script) || shellWrite.MatchString(strings.Join(words, " "))
}

// protectedNamed is the protected path a script names, in its text or as
// a word.
func protectedNamed(script string, words []string) (string, bool) {
	for _, protected := range ProtectedPaths() {
		if ContainsFold(script, protected) {
			return protected, true
		}
	}
	return protectedWord(words, ProtectedPathIn)
}

// matchSimpleCommand matches a simple command, its words, of a script at
// depth. One that puts, moves or removes something (see isReplacing),
// wherever among its words its name stands (`timeout 5 ln -s conf .pi`,
// `find . -exec mv {} .pi \;`), changes any protected path or leading part
// of one among its words. The scripts it runs (see scriptsOf) are matched
// too, to maxScriptDepth.
func matchSimpleCommand(words []string, depth int) shellMatch {
	replacing := isReplacing(words)
	if replacing {
		if protected, ok := protectedWord(words, protectedOrLeadIn); ok {
			return shellMatch{protected: protected}
		}
	}
	m := shellMatch{unplaced: unplaced(words, replacing)}
	scripts := scriptsOf(words)
	if depth == maxScriptDepth {
		m.unplaced = m.unplaced || len(scripts) > 0
		return m
	}
	for _, script := range scripts {
		m.add(matchScript(script, depth+1))
	}
	return m
}

// isReplacing reports whether a simple command, its words, puts, moves or
// removes something: one of replacingCommands, or one of gitReplacing
// after git, is among its words.
func isReplacing(words []string) bool {
	if slices.ContainsFunc(words, isNamed(replacingCommands...)) {
		return true
	}
	i := slices.IndexFunc(words, isNamed("git"))
	return i >= 0 && slices.Contains(gitReplacing, gitSubcommand(words[i+1:]))
}

// gitSubcommand is the command of git's args, past git's own options.
func gitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case slices.Contains(gitValueOptions, arg):
			i++
		case !strings.HasPrefix(arg, "-"):
			return arg
		}
	}
	return ""
}

// unplaced reports whether the matcher cannot tell what a simple command,
// its words, changes: a replacing one with an expansion among its words
// (`ln -s conf $d` in a `for` loop) or run by xargs, whose operands come
// from its input; one whose name is an expansion (`$RM -rf .claude`); or
// one that changes into a leading part of a protected path (`cd .claude`),
// after which relative paths lie inside it.
func unplaced(words []string, replacing bool) bool {
	if replacing && slices.ContainsFunc(words, func(word string) bool { return hasExpansion(word) || isNamed("xargs")(word) }) {
		return true
	}
	if i := commandStart(words); i < len(words) && hasExpansion(words[i]) {
		return true
	}
	_, intoLead := protectedWord(words, ProtectedLeadIn)
	return intoLead && slices.ContainsFunc(words, isNamed("cd", "pushd"))
}

// hasExpansion reports whether a word holds what the shell expands before
// running the command (`$d`, `$(...)`, a backquote).
func hasExpansion(word string) bool { return strings.ContainsAny(word, "$`") }

// scriptsOf are the scripts a simple command, its words, runs that are
// matched as well, each read from the first such word among them: eval's
// arguments, joined as eval joins them, and the script a shell runs with
// -c (`sudo bash -c '...'`, `find . -exec sh -c '...' \;`).
func scriptsOf(words []string) []string {
	var scripts []string
	if i := slices.IndexFunc(words, isNamed("eval")); i >= 0 {
		scripts = append(scripts, strings.Join(words[i+1:], " "))
	}
	if i := slices.IndexFunc(words, isNamed(shells...)); i >= 0 {
		scripts = append(scripts, shellScript(words[i+1:])...)
	}
	return scripts
}

// shellScript is the script a shell's args give it with -c (`-c`,
// `-ec`, `-lc`): the word after the first short option holding `c`.
func shellScript(args []string) []string {
	i := slices.IndexFunc(args, func(arg string) bool {
		return strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsRune(arg, 'c')
	})
	if i < 0 || i+1 == len(args) {
		return nil
	}
	return args[i+1 : i+2]
}

// isNamed matches a word that names one of the commands names, by its
// base name, without regard to case or a `.exe` (`/bin/mv`, `RM.EXE`).
func isNamed(names ...string) func(string) bool {
	return func(word string) bool {
		name := path.Base(strings.ReplaceAll(word, `\`, "/"))
		return slices.Contains(names, strings.TrimSuffix(strings.ToLower(name), ".exe"))
	}
}

// protectedWord is what match reports for the first of words, read as a
// path, that it matches: each word and each part of it around `=`
// (`--target-directory=.pi`), with backslashes read as Win32 reads them
// and cleaned (`sub/.pi/`, `.claude\settings.json`).
func protectedWord(words []string, match func(string) (string, bool)) (string, bool) {
	for _, word := range words {
		for _, part := range strings.Split(word, "=") {
			if protected, ok := match(path.Clean(strings.ReplaceAll(part, `\`, "/"))); ok {
				return protected, true
			}
		}
	}
	return "", false
}

// protectedOrLeadIn reports the protected path p holds (ProtectedPathIn),
// or the leading part of one it ends with (ProtectedLeadIn).
func protectedOrLeadIn(p string) (string, bool) {
	if protected, ok := ProtectedPathIn(p); ok {
		return protected, true
	}
	return ProtectedLeadIn(p)
}

// shellFields are the parts of words between white space, quotes and the
// shell's operators and expansions, as the coarse fallback reads them:
// `$(echo .pi)` holds `.pi`.
func shellFields(words []string) []string {
	var fields []string
	for _, word := range words {
		fields = append(fields, strings.FieldsFunc(word, func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune("'\"`;&|()<>=${}", r)
		})...)
	}
	return fields
}

// commandStart is the index of the word that names the command: past
// variable assignments (`X=1`), command prefixes and their options.
func commandStart(words []string) int {
	i := 0
	for i < len(words) && (isAssignment(words[i]) || slices.Contains(commandPrefixes, words[i])) {
		i++
		for i < len(words) && strings.HasPrefix(words[i], "-") {
			i++
		}
	}
	return i
}

// isAssignment reports whether word assigns a shell variable, `NAME=value`.
func isAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	return ok && name != "" && strings.IndexFunc(name, func(r rune) bool { return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) }) < 0
}
