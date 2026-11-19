package domain

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

var (
	// shellWrite matches shell commands that can change a file: round 4's
	// pattern, and the in-place editors and the file and archive commands
	// it did not name.
	shellWrite = regexp.MustCompile(`>|\btee\b|\bsed\s+(-\S*\s+)*-i|\b(sed|perl|g?awk)\b[^\n;|&]*\s(-i|--in-place)|` +
		`\b(mv|cp|rm|ln|chmod|truncate|install|dd|unlink|link|rsync|tar|patch|sponge)\b|\bgit\s+(checkout|restore|rm|mv|apply)\b`)
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
var gitValueOptions = []string{"-C", "-c", "--config-env", "--git-dir", "--work-tree", "--namespace", "--attr-source"}

// gitConfigOptions start git's own options that set its config or where
// it finds its commands, after which any command name can be an alias of
// a replacing one (`git -c alias.co=checkout co`) or run a program.
var gitConfigOptions = []string{"-c", "--config-env", "--exec-path"}

// maxScriptDepth bounds how deep scripts inside scripts (see scriptsIn)
// are matched, which keeps the work linear in the command's length;
// deeper ones are left to the floor (see shellFloor), which reads the
// whole command's text.
const maxScriptDepth = 4

// guardShell matches a Bash call's command; it reads no paths. A command
// that looks like it changes a protected path (see protectedInCommand) is
// denied unless it provably changes no file (see provablyReadOnly).
func guardShell(_ Config, call ToolCall) GuardDecision {
	command := call.Command
	if applyCommand.MatchString(command) {
		return deny("only the user runs `millennium-code-qc config apply`; write a proposal instead and ask them to apply it")
	}
	if protected, ok := protectedInCommand(command); ok && !provablyReadOnly(command) {
		return deny("this command looks like it changes %s, which no agent may change", protected)
	}
	return GuardDecision{}
}

// protectedInCommand is the protected path a shell command looks like it
// changes, without regard to case: what the matcher finds (see
// matchScript), or else what the floor under it finds (see shellFloor).
// The matcher names the path more precisely; the floor makes sure that
// nothing the round-4 rule denied is missed.
func protectedInCommand(command string) (string, bool) {
	m := matchScript(command, 0)
	if m.protected != "" {
		return m.protected, true
	}
	return shellFloor(command, m)
}

// shellMatch is what matching a shell script found.
type shellMatch struct {
	// protected is what it changes, "" for nothing found.
	protected string
	// writes: it, or a script in it, can write (see writeLike), or a
	// simple command in it puts, moves or removes something (see
	// isReplacing).
	writes bool
	// words are the words of the script and of the scripts inside it,
	// redirection targets included.
	words []string
}

// add adds what matching a script inside the script found.
func (m *shellMatch) add(inner shellMatch) {
	if m.protected == "" {
		m.protected = inner.protected
	}
	m.writes = m.writes || inner.writes
	m.words = append(m.words, inner.words...)
}

// matchScript matches a shell script at depth, the number of scripts it
// lies inside: each simple command that changes a protected path (see
// matchSimpleCommand) changes it. The scripts its words may hold (see
// scriptsIn) are matched too, to maxScriptDepth. What it names otherwise
// is left to the floor, which reads every word found here.
func matchScript(script string, depth int) shellMatch {
	scan := scanShell(script)
	m := shellMatch{words: scan.words, writes: writeLike(script, scan.words)}
	for i := 0; m.protected == "" && i < len(scan.commands); i++ {
		m.add(matchSimpleCommand(scan.commands[i].words))
	}
	inner := scriptsIn(scan)
	for i := 0; m.protected == "" && depth < maxScriptDepth && i < len(inner); i++ {
		m.add(matchScript(inner[i], depth+1))
	}
	return m
}

// writeLike reports whether a script, as written or as its words, can
// change a file.
func writeLike(script string, words []string) bool {
	return shellWrite.MatchString(script) || shellWrite.MatchString(strings.Join(words, " "))
}

// matchSimpleCommand matches a simple command, its words. One that puts,
// moves or removes something (see isReplacing), wherever among its words
// its name stands (`timeout 5 ln -s conf .pi`, `find . -exec mv {} .pi
// \;`), changes any protected path or leading part of one among its words.
func matchSimpleCommand(words []string) shellMatch {
	if !isReplacing(words) {
		return shellMatch{}
	}
	protected, _ := protectedWord(words, protectedOrLeadIn)
	return shellMatch{protected: protected, writes: true}
}

// isReplacing reports whether a simple command, its words, puts, moves or
// removes something: one of replacingCommands is among its words, or a
// git among them replaces (see gitReplaces).
func isReplacing(words []string) bool {
	for i, word := range words {
		name := commandName(word)
		if slices.Contains(replacingCommands, name) || (name == "git" && gitReplaces(words[i+1:])) {
			return true
		}
	}
	return false
}

// gitReplaces reports whether git, with args, puts, moves, removes or
// restores files: its command is one of gitReplacing, or its own options
// set its config (see gitConfigOptions). Every git among a command's
// words is read so (`sudo -u git git checkout`).
func gitReplaces(args []string) bool {
	command, configured := gitSubcommand(args)
	return configured || slices.Contains(gitReplacing, command)
}

// gitSubcommand is the command of git's args, past git's own options, and
// whether one of those options sets its config (see gitConfigOptions).
func gitSubcommand(args []string) (command string, configured bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		configured = configured || slices.ContainsFunc(gitConfigOptions, func(option string) bool { return strings.HasPrefix(arg, option) })
		switch {
		case slices.Contains(gitValueOptions, arg):
			i++
		case !strings.HasPrefix(arg, "-"):
			return arg, configured
		}
	}
	return "", configured
}

// scriptsIn are the scripts the words of a scanned script may hold, each
// matched as a script: those of its simple commands (see scriptsOf), and
// each redirection target that holds shell syntax (`sh <<< '...'`).
func scriptsIn(scan shellScan) []string {
	var scripts []string
	for _, c := range scan.commands {
		scripts = append(scripts, scriptsOf(c.words)...)
	}
	for _, r := range scan.redirects {
		if holdsShellSyntax(r.target) {
			scripts = append(scripts, r.target)
		}
	}
	return scripts
}

// scriptsOf are the scripts a simple command, its words, may run: eval's
// arguments, joined as eval joins them, and each word before them that
// holds shell syntax (see holdsShellSyntax). Which program runs a word as
// a script cannot be listed (a shell's -c, whatever its options, a shell
// reading its input, env -S, su -c, ssh, watch, a git alias), so every
// such word is read as one; a command substitution inside double quotes
// (`"$(printf .pi)"`) is read so too.
func scriptsOf(words []string) []string {
	var scripts []string
	end := len(words)
	if i := slices.IndexFunc(words, func(word string) bool { return commandName(word) == "eval" }); i >= 0 {
		end = i
		scripts = append(scripts, strings.Join(words[i+1:], " "))
	}
	for _, word := range words[:end] {
		if holdsShellSyntax(word) {
			scripts = append(scripts, word)
		}
	}
	return scripts
}

// holdsShellSyntax reports whether a word, read as a script, could read
// differently from the word itself: it holds white space, a quote, a
// backslash, an expansion or an operator.
func holdsShellSyntax(word string) bool {
	return strings.ContainsAny(word, " \t\n'\"\\$`;&|()<>")
}

// commandName is the command a word names, by its base name, in lower
// case and without a `.exe` (`/bin/mv`, `RM.EXE` name `mv` and `rm`).
func commandName(word string) string {
	name := path.Base(strings.ReplaceAll(word, `\`, "/"))
	return strings.TrimSuffix(strings.ToLower(name), ".exe")
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
