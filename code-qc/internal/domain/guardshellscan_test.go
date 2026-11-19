package domain

import (
	"slices"
	"testing"
)

// commandWords are the words of each simple command of a scan.
func commandWords(scan shellScan) [][]string {
	var words [][]string
	for _, c := range scan.commands {
		words = append(words, c.words)
	}
	return words
}

// A shell command splits into simple commands at control operators,
// subshells and command substitutions; quotes and backslashes join and
// escape as the shell has them; a redirection's target is no word of the
// simple command, but is a word read, and digits written right before a
// redirection are its file descriptor.
func TestScanShell(t *testing.T) {
	tests := []struct {
		command  string
		commands [][]string
		words    []string
	}{
		{"", nil, nil},
		{"  ", nil, nil},
		{"ls -la .claude", [][]string{{"ls", "-la", ".claude"}}, []string{"ls", "-la", ".claude"}},
		{"a; b && c || d | e & f\ng", [][]string{{"a"}, {"b"}, {"c"}, {"d"}, {"e"}, {"f"}, {"g"}}, []string{"a", "b", "c", "d", "e", "f", "g"}},
		{"(cd x && ln a b)", [][]string{{"cd", "x"}, {"ln", "a", "b"}}, []string{"cd", "x", "ln", "a", "b"}},
		{"echo $(mv a b) `rm c`", [][]string{{"echo", "$"}, {"mv", "a", "b"}, {"rm", "c"}}, []string{"echo", "$", "mv", "a", "b", "rm", "c"}},
		{"ls .claude 2>/dev/null", [][]string{{"ls", ".claude"}}, []string{"ls", ".claude", "/dev/null"}},
		{"rm 2>&1 x", [][]string{{"rm", "x"}}, []string{"rm", "1", "x"}},
		{"echo a2>x 2 >y '2'>z", [][]string{{"echo", "a2", "2", "2"}}, []string{"echo", "a2", "x", "2", "y", "2", "z"}},
		{"cat <<EOF >>out x", [][]string{{"cat", "x"}}, []string{"cat", "EOF", "out", "x"}},
		{"ln a >| b c", [][]string{{"ln", "a", "c"}}, []string{"ln", "a", "b", "c"}},
		{`echo '{}' > '.claude'/x`, [][]string{{"echo", "{}"}}, []string{"echo", "{}", ".claude/x"}},
		{"echo > ;", [][]string{{"echo"}}, []string{"echo"}},
		{`ln 'a b' "c d" e'f'"g"`, [][]string{{"ln", "a b", "c d", "efg"}}, []string{"ln", "a b", "c d", "efg"}},
		{`echo "a\"b" "\$x" "\n" 'c\'`, [][]string{{"echo", `a"b`, "$x", `\n`, `c\`}}, []string{"echo", `a"b`, "$x", `\n`, `c\`}},
		{`echo "" ''`, [][]string{{"echo", "", ""}}, []string{"echo", "", ""}},
		{`ln \.pi a\ b \`, [][]string{{"ln", ".pi", "a b", `\`}}, []string{"ln", ".pi", "a b", `\`}},
		{"echo 'unclosed ; rm x", [][]string{{"echo", "unclosed ; rm x"}}, []string{"echo", "unclosed ; rm x"}},
		{`echo "ends with \`, [][]string{{"echo", `ends with \`}}, []string{"echo", `ends with \`}},
		{`mv a $'.p'i $"x y" $ $x`, [][]string{{"mv", "a", ".pi", "x y", "$", "$x"}}, []string{"mv", "a", ".pi", "x y", "$", "$x"}},
		{`echo $'a\'b' $'unclosed`, [][]string{{"echo", "a'b", "unclosed"}}, []string{"echo", "a'b", "unclosed"}},
		{`echo "$'.pi'"`, [][]string{{"echo", "$'.pi'"}}, []string{"echo", "$'.pi'"}},
	}
	for _, tt := range tests {
		scan := scanShell(tt.command)
		if got := commandWords(scan); !slices.EqualFunc(got, tt.commands, slices.Equal[[]string]) {
			t.Errorf("scanShell(%q) commands = %q, want %q", tt.command, got, tt.commands)
		}
		if !slices.Equal(scan.words, tt.words) {
			t.Errorf("scanShell(%q) words = %q, want %q", tt.command, scan.words, tt.words)
		}
	}
}

// Each redirection is read with its operator and its target; one whose
// target the scanner cannot read, at the end of a simple command, before
// another operator or as a process substitution, has none.
func TestScanShellReadsRedirections(t *testing.T) {
	tests := []struct {
		command string
		want    []shellRedirect
	}{
		{"ls", nil},
		{"ls .claude 2>/dev/null", []shellRedirect{{">", "/dev/null"}}},
		{"du -sh x 2>&1 >>out <in", []shellRedirect{{">&", "1"}, {">>", "out"}, {"<", "in"}}},
		{"cat <<<'a b' >| x", []shellRedirect{{"<<<", "a b"}, {">|", "x"}}},
		{"echo x &> f", []shellRedirect{{">", "f"}}},
		{"echo > ; ls", []shellRedirect{{">", ""}}},
		{"echo > > x", []shellRedirect{{">", ""}, {">", "x"}}},
		{"cat <(ls)", []shellRedirect{{"<", ""}}},
		{"> .pi", []shellRedirect{{">", ".pi"}}},
	}
	for _, tt := range tests {
		if got := scanShell(tt.command).redirects; !slices.Equal(got, tt.want) {
			t.Errorf("scanShell(%q) redirects = %q, want %q", tt.command, got, tt.want)
		}
	}
}

// A simple command is globbed when a word of it holds an unquoted glob or
// brace, which the shell may expand into any name; a quoted or escaped
// one is a plain character.
func TestScanShellMarksGlobbedCommands(t *testing.T) {
	tests := []struct {
		command string
		want    []bool
	}{
		{"find . -name '*.go'", []bool{false}},
		{`find . -de\*`, []bool{false}},
		{"find . -de*", []bool{true}},
		{"ls x?; find . {-delete,}", []bool{true, true}},
		{"ls [ab]; cat x", []bool{true, false}},
	}
	for _, tt := range tests {
		var got []bool
		for _, c := range scanShell(tt.command).commands {
			got = append(got, c.globbed)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("scanShell(%q) globbed = %v, want %v", tt.command, got, tt.want)
		}
	}
}

// A `$'…'` string decodes its backslash escapes as bash does, so a
// protected name spelled with them is read as that name.
func TestScanShellDecodesANSICStrings(t *testing.T) {
	tests := []struct{ quoted, want string }{
		{`$'\x2epi'`, ".pi"},
		{`$'\x2EPI'`, ".PI"},
		{`$'\56pi'`, ".pi"},
		{`$'\0567'`, ".7"},
		{`$'.claude'`, ".claude"},
		{`$'\U0000002eclaude'`, ".claude"},
		{`$'a\tb\nc'`, "a\tb\nc"},
		{`$'\a\b\e\E\f\r\v\\\"\?'`, "\a\b\x1b\x1b\f\r\v\\\"?"},
		{`$'\cA\c'`, "\x01\\c"},
		{`$'\xg\u\z'`, `\xg\u\z`},
		{`$'\x2'`, "\x02"},
		{`$'\'`, `'`},
	}
	for _, tt := range tests {
		if words := scanShell(tt.quoted).words; !slices.Equal(words, []string{tt.want}) {
			t.Errorf("scanShell(%q) = %q, want %q", tt.quoted, words, tt.want)
		}
	}
}
