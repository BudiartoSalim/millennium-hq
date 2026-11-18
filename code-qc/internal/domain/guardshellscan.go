package domain

import (
	"strings"
	"unicode"
)

// scanShell splits a shell command as far as matching it needs: into its
// simple commands, each as its words without the targets of its
// redirections (`2>/dev/null`), and into every word it reads, those
// targets included. It splits at the control operators (`;`, `&&`, `|`, a
// newline) and at the parentheses and backquotes of subshells and command
// substitutions, takes quotes and backslashes as the shell takes them, and
// decodes `$'…'` strings. Shell syntax beyond that, such as a command
// substitution inside double quotes, is read as plain words.
func scanShell(command string) (commands [][]string, words []string) {
	var s shellScanner
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		i = s.scan(runes, i)
	}
	s.endCommand()
	return s.commands, s.all
}

// shellScanner collects the simple commands and words of a shell command,
// one rune at a time.
type shellScanner struct {
	commands [][]string
	words    []string
	// all are the words read so far, redirection targets included.
	all  []string
	word strings.Builder
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
	case r == '$' && i+1 < len(runes) && strings.ContainsRune(`'"`, runes[i+1]):
		return s.dollarQuoted(runes, i+1)
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

// dollarQuoted reads the string whose quote, after a `$`, is at i: `$"…"`
// is translated text, read as a double-quoted string, and `$'…'` has its
// backslash escapes decoded (`$'\x2epi'` is `.pi`).
func (s *shellScanner) dollarQuoted(runes []rune, i int) int {
	if runes[i] == '"' {
		return s.quoted(runes, i)
	}
	end := i + 1
	for end < len(runes) && runes[end] != '\'' {
		if runes[end] == '\\' {
			end++
		}
		end++
	}
	s.inWord = true
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
	s.all = append(s.all, word)
	if s.redirect {
		s.redirect = false
	} else {
		s.words = append(s.words, word)
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
