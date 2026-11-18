package domain

import (
	"slices"
	"testing"
)

// A shell command splits into simple commands at control operators,
// subshells and command substitutions; quotes and backslashes join and
// escape as the shell has them; a redirection's target is no word of the
// simple command, but is a word read.
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
		{"ls .claude 2>/dev/null", [][]string{{"ls", ".claude", "2"}}, []string{"ls", ".claude", "2", "/dev/null"}},
		{"rm 2>&1 x", [][]string{{"rm", "2", "x"}}, []string{"rm", "2", "1", "x"}},
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
		commands, words := scanShell(tt.command)
		if !slices.EqualFunc(commands, tt.commands, slices.Equal[[]string]) {
			t.Errorf("scanShell(%q) commands = %q, want %q", tt.command, commands, tt.commands)
		}
		if !slices.Equal(words, tt.words) {
			t.Errorf("scanShell(%q) words = %q, want %q", tt.command, words, tt.words)
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
		_, words := scanShell(tt.quoted)
		if !slices.Equal(words, []string{tt.want}) {
			t.Errorf("scanShell(%q) = %q, want %q", tt.quoted, words, tt.want)
		}
	}
}
