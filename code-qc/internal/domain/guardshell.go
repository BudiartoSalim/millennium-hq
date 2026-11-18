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

// commandPrefixes run the command after them, so the command that runs is
// the next word; shell keywords that start a command are skipped the same
// way.
var commandPrefixes = []string{"sudo", "doas", "env", "command", "exec", "nohup", "nice", "time", "xargs", "!", "{", "if", "then", "else", "elif", "do", "while", "until"}

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
// changes, without regard to case: one named anywhere in a command that
// can write a file, or a leading part of one (see ProtectedLeadIn) that is
// an operand of a command that puts, moves or removes something there
// (see replacedOperands). A command that only reads `.pi` or `.claude`,
// such as `ls -la .claude 2>/dev/null`, changes neither.
func protectedInCommand(command string) (string, bool) {
	if shellWrite.MatchString(command) {
		for _, protected := range ProtectedPaths() {
			if ContainsFold(command, protected) {
				return protected, true
			}
		}
	}
	for _, words := range simpleCommands(command) {
		for _, operand := range replacedOperands(words) {
			if lead, ok := ProtectedLeadIn(path.Clean(operand)); ok {
				return lead, true
			}
		}
	}
	return "", false
}

// replacedOperands are the operands of a simple command, its words, that
// it may put, move or remove something at: those of ln, mv, cp, rm and
// install, and of `git mv`, after any assignments and command prefixes,
// each option's value (`--target-directory=.pi`) included, and those of
// the commands of a shell's -c script. Other commands have none.
func replacedOperands(words []string) []string {
	i := commandStart(words)
	if i == len(words) {
		return nil
	}
	name, args := path.Base(words[i]), words[i+1:]
	switch {
	case slices.Contains(replacingCommands, name):
		return optionValues(args)
	case name == "git":
		return gitMoveOperands(args)
	case slices.Contains(shells, name):
		return scriptOperands(args)
	}
	return nil
}

// shells run a script given with -c, whose commands are matched as well.
var shells = []string{"sh", "bash", "dash", "zsh", "ksh"}

// scriptOperands are the replaced operands of the commands of the script
// a shell runs with -c (`bash -c '...'`, `sh -ec '...'`): the first word
// after an option holding `c`.
func scriptOperands(args []string) []string {
	i := slices.IndexFunc(args, func(arg string) bool {
		return strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsRune(arg, 'c')
	})
	if i < 0 || i+1 == len(args) {
		return nil
	}
	var operands []string
	for _, words := range simpleCommands(args[i+1]) {
		operands = append(operands, replacedOperands(words)...)
	}
	return operands
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

// gitMoveOperands are the operands of `git mv`, past git's own options
// (`git -C sub mv a .pi`); any other git command has none.
func gitMoveOperands(args []string) []string {
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "-C" || arg == "-c":
			i++
		case strings.HasPrefix(arg, "-"):
		case arg == "mv":
			return optionValues(args[i+1:])
		default:
			return nil
		}
	}
	return nil
}

// optionValues are args, with each `--name=value` option read as its
// value.
func optionValues(args []string) []string {
	values := make([]string, 0, len(args))
	for _, arg := range args {
		if name, value, ok := strings.Cut(arg, "="); ok && strings.HasPrefix(name, "--") {
			arg = value
		}
		values = append(values, arg)
	}
	return values
}

// simpleCommands are the simple commands of a shell command, each as its
// words, as far as matching them needs: split at the control operators
// (`;`, `&&`, `|`, a newline), at the parentheses and backquotes of
// subshells and command substitutions, with quotes and backslashes taken
// as the shell takes them, and without the targets of redirections
// (`2>/dev/null`). Shell syntax beyond that, such as a command
// substitution inside double quotes, is read as plain words.
func simpleCommands(command string) [][]string {
	var s shellScanner
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		i = s.scan(runes, i)
	}
	s.endCommand()
	return s.commands
}

// shellScanner collects the simple commands of a shell command, one rune
// at a time.
type shellScanner struct {
	commands [][]string
	words    []string
	word     strings.Builder
	// inWord: a word has begun, even an empty quoted one.
	inWord bool
	// redirect: the next word is the target of a redirection.
	redirect bool
}

// scan reads the rune at i, with the runes that belong to it, and returns
// the index of the last rune it read.
func (s *shellScanner) scan(runes []rune, i int) int {
	switch r := runes[i]; {
	case r == '\\' && i+1 < len(runes):
		s.add(runes[i+1])
		return i + 1
	case r == '\'' || r == '"':
		return s.quoted(runes, i)
	case strings.ContainsRune(";&|()`\n", r):
		s.endCommand()
	case r == '<' || r == '>':
		s.endWord()
		s.redirect = true
		for i+1 < len(runes) && strings.ContainsRune("<>&|", runes[i+1]) {
			i++
		}
	case unicode.IsSpace(r):
		s.endWord()
	default:
		s.add(r)
	}
	return i
}

// quoted reads the quoted string that starts at i into the word, and
// returns the index of its closing quote. Inside double quotes a
// backslash escapes `"`, `\`, `$` and a backquote; inside single quotes
// nothing is escaped.
func (s *shellScanner) quoted(runes []rune, i int) int {
	quote := runes[i]
	s.inWord = true
	for i++; i < len(runes) && runes[i] != quote; i++ {
		if quote == '"' && runes[i] == '\\' && i+1 < len(runes) && strings.ContainsRune("\"\\$`", runes[i+1]) {
			i++
		}
		s.word.WriteRune(runes[i])
	}
	return i
}

// add adds r to the word being read.
func (s *shellScanner) add(r rune) {
	s.inWord = true
	s.word.WriteRune(r)
}

// endWord ends the word being read: a redirection's target is dropped,
// any other word kept.
func (s *shellScanner) endWord() {
	if !s.inWord {
		return
	}
	if s.redirect {
		s.redirect = false
	} else {
		s.words = append(s.words, s.word.String())
	}
	s.word.Reset()
	s.inWord = false
}

// endCommand ends the simple command being read.
func (s *shellScanner) endCommand() {
	s.endWord()
	if len(s.words) > 0 {
		s.commands = append(s.commands, s.words)
	}
	s.words, s.redirect = nil, false
}
