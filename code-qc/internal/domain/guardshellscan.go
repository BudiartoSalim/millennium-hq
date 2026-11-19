package domain

import (
	"strings"
	"unicode"
)

// shellScan is a shell command split as far as the shell guard needs.
type shellScan struct {
	// commands are its simple commands, each without its redirections.
	commands []shellCommand
	// words are every word read, the targets of redirections included.
	words []string
	// redirects are its redirections, in the order read.
	redirects []shellRedirect
}

// shellCommand is a simple command.
type shellCommand struct {
	words []string
	// globbed: a word holds an unquoted `*`, `?`, `[` or `{`, which the
	// shell may expand into any name, an option included (`-del*` into a
	// file named `-delete`).
	globbed bool
}

// shellRedirect is a redirection: its operator (`>`, `>>`, `>&`, `<<<`)
// and its target, "" when the shell gives it none the scanner can read
// (`echo >;`, a process substitution `>(…)`).
type shellRedirect struct {
	op, target string
}

// globChars are the characters that, unquoted, make the shell expand a
// word into names.
const globChars = "*?[{"

// scanShell splits a shell command as far as matching it needs: into its
// simple commands, each as its words without its redirections
// (`2>/dev/null`), the redirections themselves, and every word it reads,
// their targets included. It splits at the control operators (`;`, `&&`,
// `|`, a newline) and at the parentheses and backquotes of subshells and
// command substitutions, takes quotes and backslashes as the shell takes
// them (a backslash-newline joins lines), decodes `$'…'` strings, and reads digits written right before a
// redirection operator as the file descriptor it redirects (`2>`). Shell
// syntax beyond that, such as a command substitution inside double quotes,
// is read as plain words.
func scanShell(command string) shellScan {
	var s shellScanner
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		i = s.scan(runes, i)
	}
	s.endCommand()
	return s.result
}

// shellScanner collects the simple commands, redirections and words of a
// shell command, one rune at a time.
type shellScanner struct {
	result shellScan
	// words are the words of the simple command being read.
	words []string
	// globbed: a word of the simple command being read holds a glob.
	globbed bool
	word    strings.Builder
	// inWord: a word has begun, even an empty quoted one.
	inWord bool
	// quoted: part of the word being read is quoted or escaped.
	quoted bool
	// pending is the operator of a redirection whose target is the next
	// word, "" for none.
	pending string
}

// scan reads the rune at i, with the runes that belong to it, and returns
// the index of the last rune it read.
func (s *shellScanner) scan(runes []rune, i int) int {
	switch r := runes[i]; {
	case r == '\\' && i+1 < len(runes):
		s.escaped(runes[i+1])
		return i + 1
	case r == '\'' || r == '"':
		return s.quotedString(runes, i)
	case r == '$' && i+1 < len(runes) && strings.ContainsRune(`'"`, runes[i+1]):
		return s.dollarQuoted(runes, i+1)
	case strings.ContainsRune(";&|()`\n", r):
		s.endCommand()
	case r == '<' || r == '>':
		return s.redirection(runes, i)
	case unicode.IsSpace(r):
		s.endWord()
	default:
		s.globbed = s.globbed || strings.ContainsRune(globChars, r)
		s.add(r)
	}
	return i
}

// redirection reads the redirection operator that starts at i, all of
// `<>&|` that follows it (`>>`, `>&`, `<<<`, `>|`), and returns the index
// of its last rune. Unquoted digits right before it are the file
// descriptor it redirects, not a word.
func (s *shellScanner) redirection(runes []rune, i int) int {
	if s.inWord && !s.quoted && isDigits(s.word.String()) {
		s.resetWord()
	}
	s.endWord()
	start := i
	for i+1 < len(runes) && strings.ContainsRune("<>&|", runes[i+1]) {
		i++
	}
	s.endRedirection()
	s.pending = string(runes[start : i+1])
	return i
}

// isDigits reports whether s is one or more ASCII digits.
func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// quotedString reads the quoted string that starts at i into the word,
// and returns the index of its closing quote. Inside double quotes a
// backslash escapes `"`, `\`, `$`, a backquote and a newline (see
// escaped); inside single quotes nothing is escaped.
func (s *shellScanner) quotedString(runes []rune, i int) int {
	quote := runes[i]
	s.inWord, s.quoted = true, true
	for i++; i < len(runes) && runes[i] != quote; i++ {
		if quote == '"' && runes[i] == '\\' && i+1 < len(runes) && strings.ContainsRune("\"\\$`\n", runes[i+1]) {
			i++
			s.escaped(runes[i])
			continue
		}
		s.word.WriteRune(runes[i])
	}
	return i
}

// dollarQuoted reads the string whose quote, after a `$`, is at i: `$"…"`
// is translated text, read as a double-quoted string, and `$'…'` has its
// backslash escapes decoded (`$'\x2epi'` is `.pi`).
func (s *shellScanner) dollarQuoted(runes []rune, i int) int {
	if runes[i] == '"' {
		return s.quotedString(runes, i)
	}
	end := i + 1
	for end < len(runes) && runes[end] != '\'' {
		if runes[end] == '\\' {
			end++
		}
		end++
	}
	s.inWord, s.quoted = true, true
	s.word.WriteString(decodeANSIC(runes[i+1 : min(end, len(runes))]))
	return end
}

// decodeANSIC decodes the backslash escapes of the content of a `$'…'`
// string.
func decodeANSIC(content []rune) string {
	var b strings.Builder
	for i := 0; i < len(content); i++ {
		if content[i] == '\\' && i+1 < len(content) {
			decoded, n := ansiEscape(content[i+1:])
			b.WriteString(decoded)
			i += n
			continue
		}
		b.WriteRune(content[i])
	}
	return b.String()
}

// ansiSimpleEscapes are the one-letter escapes of a `$'…'` string.
var ansiSimpleEscapes = map[rune]rune{
	'a': '\a', 'b': '\b', 'e': 0x1b, 'E': 0x1b, 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v',
	'\\': '\\', '\'': '\'', '"': '"', '?': '?',
}

// ansiEscape decodes the escape of a `$'…'` string that follows a
// backslash, as bash decodes it, and returns the text and the number of
// runes of rest it took: a letter (`\n`), up to three octal digits, `\x`
// with up to two hex digits, `\u` four, `\U` eight, and `\cX` a control
// character. Anything else stays as written, its backslash included.
func ansiEscape(rest []rune) (string, int) {
	if r, ok := ansiSimpleEscapes[rest[0]]; ok {
		return string(r), 1
	}
	switch c := rest[0]; {
	case c >= '0' && c <= '7':
		r, n := numericEscape(rest, 8, 3)
		return string(r), n
	case c == 'x' || c == 'u' || c == 'U':
		return hexEscape(rest, map[rune]int{'x': 2, 'u': 4, 'U': 8}[c])
	case c == 'c' && len(rest) > 1:
		return string(rest[1] & 0x1f), 2
	}
	return `\` + string(rest[0]), 1
}

// hexEscape decodes `\x`, `\u` or `\U`, whose letter starts rest, with up
// to digits hex digits; without any, it stays as written.
func hexEscape(rest []rune, digits int) (string, int) {
	r, n := numericEscape(rest[1:], 16, digits)
	if n == 0 {
		return `\` + string(rest[0]), 1
	}
	return string(r), n + 1
}

// numericEscape reads up to limit digits of base from the start of
// digits, and returns their value and how many it read.
func numericEscape(digits []rune, base, limit int) (rune, int) {
	value, n := 0, 0
	for ; n < len(digits) && n < limit; n++ {
		d := strings.IndexRune("0123456789abcdef"[:base], unicode.ToLower(digits[n]))
		if d < 0 {
			break
		}
		value = value*base + d
	}
	return rune(value), n
}

// escaped adds r, which a backslash escapes, to the word being read; a
// backslash-newline joins lines and adds nothing (`.p\⏎i` is `.pi`).
func (s *shellScanner) escaped(r rune) {
	if r != '\n' {
		s.quoted = true
		s.add(r)
	}
}

// add adds r to the word being read.
func (s *shellScanner) add(r rune) {
	s.inWord = true
	s.word.WriteRune(r)
}

// endWord ends the word being read: a redirection's target is no word of
// the simple command, though it is a word read.
func (s *shellScanner) endWord() {
	if !s.inWord {
		return
	}
	word := s.word.String()
	s.result.words = append(s.result.words, word)
	if s.pending != "" {
		s.result.redirects = append(s.result.redirects, shellRedirect{op: s.pending, target: word})
		s.pending = ""
	} else {
		s.words = append(s.words, word)
	}
	s.resetWord()
}

// resetWord drops the word being read.
func (s *shellScanner) resetWord() {
	s.word.Reset()
	s.inWord, s.quoted = false, false
}

// endRedirection records a redirection still waiting for its target as
// one without a target the scanner can read.
func (s *shellScanner) endRedirection() {
	if s.pending != "" {
		s.result.redirects = append(s.result.redirects, shellRedirect{op: s.pending})
		s.pending = ""
	}
}

// endCommand ends the simple command being read.
func (s *shellScanner) endCommand() {
	s.endWord()
	s.endRedirection()
	if len(s.words) > 0 {
		s.result.commands = append(s.result.commands, shellCommand{words: s.words, globbed: s.globbed})
	}
	s.words, s.globbed = nil, false
}
