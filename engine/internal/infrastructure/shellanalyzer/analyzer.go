// Package shellanalyzer extracts facts from shell scripts without a full
// parser: comments, and risky constructs found per line. Function length,
// nesting and complexity need a real shell grammar, which no standard library
// has, so shell scripts report no functions and the limits do not apply.
package shellanalyzer

import (
	"bufio"
	"bytes"
	"context"
	"path"
	"regexp"
	"strings"

	"millennium/internal/domain"
)

var (
	// evalCommand matches eval in command position: at the start, or after
	// a separator or a keyword that starts a command.
	evalCommand = regexp.MustCompile(`(^|[;&|(]|\b(then|do|else)\b)\s*eval(\s|$)`)
	// pipeToShell matches downloading straight into a shell.
	pipeToShell = regexp.MustCompile(`\b(curl|wget)\b[^|]*\|\s*(sudo\s+(-\S+\s+)*)?(ba|z|da)?sh\b`)
	// heredocStart captures a here-document delimiter: <<EOF, <<-'EOF'.
	heredocStart = regexp.MustCompile(`<<-?\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)
	generated    = regexp.MustCompile(`@generated|DO NOT EDIT`)
)

// Analyzer analyses shell scripts.
type Analyzer struct{}

// Analyze reads one script.
func (Analyzer) Analyze(_ context.Context, p string, content []byte) (domain.FileFacts, error) {
	facts := domain.FileFacts{Path: p, Test: isTest(p)}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	heredoc := ""
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		if heredoc != "" {
			if strings.TrimSpace(text) == heredoc {
				heredoc = ""
			}
			continue
		}
		code, comment := splitComment(text)
		if comment != "" {
			facts.Comments = append(facts.Comments, domain.Comment{Line: line, Text: comment})
		}
		if line <= 5 && generated.MatchString(comment) {
			facts.Generated = true
		}
		facts.Constructs = append(facts.Constructs, constructs(line, code)...)
		if m := heredocStart.FindStringSubmatch(code); m != nil {
			heredoc = m[1]
		}
	}
	return facts, scanner.Err()
}

func isTest(p string) bool {
	name := path.Base(p)
	return strings.HasSuffix(name, ".bats") || strings.HasSuffix(name, "_test.sh") ||
		strings.HasPrefix(name, "test_") || strings.Contains("/"+p, "/tests/")
}

func constructs(line int, code string) []domain.Construct {
	var out []domain.Construct
	if evalCommand.MatchString(code) {
		out = append(out, domain.Construct{Kind: domain.ConstructDynamicCode, Line: line, Detail: "eval"})
	}
	if pipeToShell.MatchString(code) {
		out = append(out, domain.Construct{Kind: domain.ConstructShellCommand, Line: line, Detail: "download piped into a shell"})
	}
	return out
}

// splitComment separates the code of a line from its comment. A # starts a
// comment only at the start of a word and outside quotes; "${#x}" and "a#b"
// are not comments.
func splitComment(text string) (code, comment string) {
	var quote rune
	escaped := false
	for i, r := range text {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '#' && (i == 0 || text[i-1] == ' ' || text[i-1] == '\t' || text[i-1] == ';'):
			return text[:i], text[i:]
		}
	}
	return text, ""
}
