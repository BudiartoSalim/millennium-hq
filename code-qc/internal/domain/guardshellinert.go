package domain

import (
	"slices"
	"strings"
)

// inertWordChars are the characters, besides ASCII letters and digits,
// that a word may hold outside quotes and that bash and zsh both read as
// themselves, or as a glob the globbed-word rules see (`*`, `?`, `[`,
// `]`). `=` and `~` are read apart (see inertEquals and inertTilde).
const inertWordChars = "._/:,@+-*?[] \t"

// operatorChars are the characters of the control and redirection
// operators the whitelist reads (see inertOperators).
const operatorChars = "|;&>"

// inertOperators are the operators, as runs of operatorChars, whose
// reading bash and zsh agree on and the scanner reads alike: a pipe, a
// list, an output redirection and its descriptor forms (`2>`, `>&2`,
// `2>&1`, `>&-`; see inertOperator). Anything else is not: `&` alone,
// `|&`, `;;`, `>|`, zsh's `&|` and `&!`, and `&>` and `&>>`, at whose `&`
// the scanner ends the command while bash and zsh read a redirection
// that does not end it (`find x &>/dev/null echo -delete` runs `find x
// echo -delete`, R11-SEC-1).
var inertOperators = []string{"|", "||", ";", "&&", ">", ">>", ">&"}

// inertShellText reports whether command is made only of text whose
// reading bash and zsh agree on with each other and with the scanner, and
// that expands into nothing but itself and the names a glob matches, so
// that what the scanner reads is what the shell runs. It accepts only
// what it knows to be inert rather than refusing what it knows to be
// dangerous: each round of review found another syntax the scanner read
// otherwise than a shell (comments, here-documents, line continuations,
// zsh's glob qualifiers, its numeric glob `<->`, extendedglob's `^` and
// `~`). Outside quotes, only ASCII letters and digits, spaces, tabs,
// inertWordChars, `=` and `~` where they are inert, and inertOperators;
// so no `<`, `^`, `!`, `%`, `{`, `}`, `(`, `)`, `#`, `$`, backquote,
// backslash, newline or other control or non-ASCII character. Inside
// quotes, see inertQuoted.
func inertShellText(command string) bool {
	for i := 0; i < len(command); {
		next, ok := inertToken(command, i)
		if !ok {
			return false
		}
		i = next
	}
	return true
}

// inertToken reports whether the text that starts at i, outside quotes,
// is inert: a quoted string, an operator, or one character. It returns
// the index after it.
func inertToken(text string, i int) (int, bool) {
	switch c := text[i]; {
	case c == '\'' || c == '"':
		return inertQuoted(text, i)
	case strings.IndexByte(operatorChars, c) >= 0:
		return inertOperator(text, i)
	case c == '=':
		return i + 1, inertEquals(text, i)
	case c == '~':
		return i + 1, inertTilde(text, i)
	}
	return i + 1, isASCIIAlphanumeric(text[i]) || strings.IndexByte(inertWordChars, text[i]) >= 0
}

// inertOperator reports whether the run of operatorChars that starts at
// i is inert, and returns the index after it. It must be one of
// inertOperators; a redirection may have at most one digit right before
// it, since zsh reads only one as its file descriptor (`10>x` is the
// word `10` and `>x` there, and bash's descriptor 10, R11-SEC-2); and
// `>&` must duplicate or close a descriptor, one digit or `-` that ends
// the word (`2>&1`, `>&-`), never name a file (`>& x`).
func inertOperator(text string, i int) (int, bool) {
	end := i + 1
	for end < len(text) && strings.IndexByte(operatorChars, text[end]) >= 0 {
		end++
	}
	operator := text[i:end]
	switch {
	case !slices.Contains(inertOperators, operator):
		return end, false
	case operator[0] == '>' && i >= 2 && isASCIIDigit(text[i-1]) && isASCIIDigit(text[i-2]):
		return end, false
	case operator == ">&":
		return end + 1, end < len(text) && (isASCIIDigit(text[end]) || text[end] == '-') && atWordEnd(text, end+1)
	}
	return end, true
}

// isASCIIDigit reports whether c is an ASCII digit.
func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// isASCIIAlphanumeric reports whether c is an ASCII letter or digit.
func isASCIIAlphanumeric(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || isASCIIDigit(c)
}

// inertQuoted reports whether the quoted string whose quote is at i is
// inert, and returns the index after its closing quote. It must be
// closed, and hold only printable ASCII and spaces but `$`, a backquote
// and a backslash (which double quotes expand or read as an escape), `!`
// (history expansion in an interactive shell), `#`, `(` and `)`. A single
// quote right after its closing quote is not inert either: zsh's
// RC_QUOTES reads two single quotes in a row inside single quotes as one
// quote, the scanner as two strings.
func inertQuoted(text string, i int) (int, bool) {
	quote := text[i]
	end := i + 1
	for end < len(text) && text[end] != quote {
		if c := text[end]; c < ' ' || c > '~' || strings.IndexByte("$`\\!#()", c) >= 0 {
			return end, false
		}
		end++
	}
	if end == len(text) || (quote == '\'' && end+1 < len(text) && text[end+1] == '\'') {
		return end, false
	}
	return end + 1, true
}

// inertEquals reports whether the `=` at i is inert: not at a word's
// start unless it is the whole word (`[ a = b ]`), and not right after
// another `=` or a `:`. zsh replaces a word that starts with `=` by the
// path of the command it names (`=ls`, and `==` names a command `=`), and
// with MAGIC_EQUAL_SUBST does so after the `=` of an argument too
// (`x==ls`) and after each `:` in its value (`x=a:=rm`, R11-SEC-4).
func inertEquals(text string, i int) bool {
	if i > 0 && (text[i-1] == '=' || text[i-1] == ':') {
		return false
	}
	return !atWordStart(text, i) || atWordEnd(text, i+1)
}

// inertTilde reports whether the `~` at i is inert enough: at a word's
// start and followed by `/` or the word's end, so that it names the
// home directory. Anywhere else zsh may read it as an extendedglob
// operator (`a~b`), and `~x`, `~+` and `~-` name another user's home, a
// named directory, the working directory or the previous one.
func inertTilde(text string, i int) bool {
	return atWordStart(text, i) && (atWordEnd(text, i+1) || text[i+1] == '/')
}

// atWordStart reports whether i, outside quotes, starts a word: it is the
// start of the text, or white space or an operator comes before it.
func atWordStart(text string, i int) bool {
	return i == 0 || strings.IndexByte(" \t"+operatorChars, text[i-1]) >= 0
}

// atWordEnd reports whether i, outside quotes, ends a word: it is the end
// of the text, or white space or an operator stands at it.
func atWordEnd(text string, i int) bool {
	return i == len(text) || strings.IndexByte(" \t"+operatorChars, text[i]) >= 0
}
