package domain

import (
	"slices"
	"testing"
)

// A shell command splits into simple commands at control operators,
// subshells and command substitutions; quotes and backslashes join and
// escape as the shell has them; a redirection's target is no word of the
// command.
func TestSimpleCommands(t *testing.T) {
	tests := []struct {
		command string
		want    [][]string
	}{
		{"", nil},
		{"  ", nil},
		{"ls -la .claude", [][]string{{"ls", "-la", ".claude"}}},
		{"a; b && c || d | e & f\ng", [][]string{{"a"}, {"b"}, {"c"}, {"d"}, {"e"}, {"f"}, {"g"}}},
		{"(cd x && ln a b)", [][]string{{"cd", "x"}, {"ln", "a", "b"}}},
		{"echo $(mv a b) `rm c`", [][]string{{"echo", "$"}, {"mv", "a", "b"}, {"rm", "c"}}},
		{"ls .claude 2>/dev/null", [][]string{{"ls", ".claude", "2"}}},
		{"rm 2>&1 x", [][]string{{"rm", "2", "x"}}},
		{"cat <<EOF >>out x", [][]string{{"cat", "x"}}},
		{"ln a >| b c", [][]string{{"ln", "a", "c"}}},
		{"echo > ;", [][]string{{"echo"}}},
		{`ln 'a b' "c d" e'f'"g"`, [][]string{{"ln", "a b", "c d", "efg"}}},
		{`echo "a\"b" "\$x" "\n" 'c\'`, [][]string{{"echo", `a"b`, "$x", `\n`, `c\`}}},
		{`echo "" ''`, [][]string{{"echo", "", ""}}},
		{`ln \.pi a\ b \`, [][]string{{"ln", ".pi", "a b", `\`}}},
		{"echo 'unclosed ; rm x", [][]string{{"echo", "unclosed ; rm x"}}},
		{`echo "ends with \`, [][]string{{"echo", `ends with \`}}},
	}
	for _, tt := range tests {
		got := simpleCommands(tt.command)
		if !slices.EqualFunc(got, tt.want, slices.Equal[[]string]) {
			t.Errorf("simpleCommands(%q) = %q, want %q", tt.command, got, tt.want)
		}
	}
}

// Only the operands of a command that puts, moves or removes something
// are those it may replace, found past assignments and command prefixes;
// git's are those of `git mv`, past git's own options.
func TestReplacedOperands(t *testing.T) {
	tests := []struct {
		words, want []string
	}{
		{nil, nil},
		{[]string{"X=1"}, nil},
		{[]string{"ls", ".pi"}, nil},
		{[]string{"ln", "-s", "conf", ".pi"}, []string{"-s", "conf", ".pi"}},
		{[]string{"/usr/bin/cp", "--target-directory=.pi", "x"}, []string{".pi", "x"}},
		{[]string{"rm", "-x=y", "a=b"}, []string{"-x=y", "a=b"}},
		{[]string{"A_1=x", "sudo", "-n", "nice", "-n", "5", "mv", "a", "b"}, nil},
		{[]string{"A_1=x", "sudo", "-n", "mv", "a", "b"}, []string{"a", "b"}},
		{[]string{"=x", "mv", "a"}, nil},
		{[]string{"a-b=x", "mv", "a"}, nil},
		{[]string{"git", "--no-pager", "-C", "sub", "-c", "k=v", "mv", "a", ".pi"}, []string{"a", ".pi"}},
		{[]string{"git", "status", "mv"}, nil},
		{[]string{"git", "-C"}, nil},
		{[]string{"git"}, nil},
		{[]string{"/bin/bash", "--norc", "-lc", "rm -rf a; X=1 ln -s b c"}, []string{"-rf", "a", "-s", "b", "c"}},
		{[]string{"zsh", "-c", "sh -c 'mv d e'"}, []string{"d", "e"}},
		{[]string{"bash", "-c"}, nil},
		{[]string{"bash", "--norc", "script.sh"}, nil},
	}
	for _, tt := range tests {
		if got := replacedOperands(tt.words); !slices.Equal(got, tt.want) {
			t.Errorf("replacedOperands(%q) = %q, want %q", tt.words, got, tt.want)
		}
	}
}
