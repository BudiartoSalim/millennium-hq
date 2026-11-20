package domain

import (
	"slices"
	"strings"
	"testing"
)

// A simple command puts, moves or removes something when a replacing
// command or a replacing git command stands anywhere among its words,
// past any prefix and its option values; git's subcommand is found past
// git's own options and their values, after every git among the words,
// and a git whose own options set its config replaces whatever its
// command, since that command can be an alias.
func TestIsReplacing(t *testing.T) {
	tests := []struct {
		words []string
		want  bool
	}{
		{nil, false},
		{[]string{"ls", ".pi"}, false},
		{[]string{"ln", "-s", "conf", ".pi"}, true},
		{[]string{"timeout", "5", "/usr/bin/cp", "a", "b"}, true},
		{[]string{`C:\bin\RM.EXE`, "a"}, true},
		{[]string{"find", ".", "-exec", "install", "{}", ";"}, true},
		{[]string{"mvn", "rmdir", "cpp"}, false},
		{[]string{"git", "rm", "a"}, true},
		{[]string{"sudo", "git", "--git-dir", "x", "-C", "sub", "--no-pager", "restore", "a"}, true},
		{[]string{"git", "checkout", "main"}, true},
		{[]string{"git", "status", "mv"}, true},
		{[]string{"git", "log", "--", ".pi"}, false},
		{[]string{"git", "--work-tree"}, false},
		{[]string{"git"}, false},
		{[]string{"sudo", "-u", "git", "git", "checkout", "main", "--", ".claude"}, true},
		{[]string{"sudo", "-u", "git", "git", "log"}, false},
		{[]string{"git", "-c", "alias.co=checkout", "co", "main"}, true},
		{[]string{"git", "-c", "core.x=y", "status"}, true},
		{[]string{"git", "--config-env", "x=Y", "checkout"}, true},
		{[]string{"git", "--config-env=alias.x=Y", "x"}, true},
		{[]string{"git", "--exec-path=/tmp/x", "status"}, true},
		{[]string{"git", "-C", "-c", "status"}, false},
	}
	for _, tt := range tests {
		if got := isReplacing(tt.words); got != tt.want {
			t.Errorf("isReplacing(%q) = %v, want %v", tt.words, got, tt.want)
		}
	}
}

// The scripts a simple command may run are eval's arguments, joined, and
// every word before them that holds shell syntax, whatever runs it: a
// shell's -c past any of its options (R7-SEC-2), env -S, ssh, a command
// substitution in double quotes.
func TestScriptsOf(t *testing.T) {
	tests := []struct {
		words, want []string
	}{
		{nil, nil},
		{[]string{"ls", "-c", "x", "a.pi", "--x=y", "~/{a,b}*"}, nil},
		{[]string{"eval", "ln", "-s", "conf .pi"}, []string{"ln -s conf .pi"}},
		{[]string{"eval"}, []string{""}},
		{[]string{"sudo", "-u", "root", "/bin/bash", "--norc", "-lc", "rm a", "b"}, []string{"rm a"}},
		{[]string{"find", ".", "-exec", "sh", "-ec", "mv {} x", ";"}, []string{"mv {} x", ";"}},
		{[]string{"x 'y'", "eval", "sh", "-c", "x"}, []string{"sh -c x", "x 'y'"}},
		{[]string{"bash", "-c", "--", "rm a"}, []string{"rm a"}},
		{[]string{"sh", "-o", "errexit", "-c", "-e", "rm a"}, []string{"rm a"}},
		{[]string{"env", "-S", "ln -s conf .pi"}, []string{"ln -s conf .pi"}},
		{[]string{"cp", "x", "$(printf .pi)"}, []string{"$(printf .pi)"}},
		{[]string{"a\tb", "a\nb", "a'b", `a"b`, `a\b`, "a$b", "a`b", "a;b", "a&b", "a|b", "a(b", "a)b", "a<b", "a>b"},
			[]string{"a\tb", "a\nb", "a'b", `a"b`, `a\b`, "a$b", "a`b", "a;b", "a&b", "a|b", "a(b", "a)b", "a<b", "a>b"}},
	}
	for _, tt := range tests {
		if got := scriptsOf(tt.words); !slices.Equal(got, tt.want) {
			t.Errorf("scriptsOf(%q) = %q, want %q", tt.words, got, tt.want)
		}
	}
}

// A redirection's target that holds shell syntax is a script too (a
// here-string fed to a shell); one without is not.
func TestScriptsIn(t *testing.T) {
	tests := []struct {
		command string
		want    []string
	}{
		{"ls > out", nil},
		{"sh <<< 'ln -s conf .pi' > out", []string{"ln -s conf .pi"}},
		{"echo 'a b' | sh; eval x", []string{"a b", "x"}},
	}
	for _, tt := range tests {
		if got := scriptsIn(scanShell(tt.command)); !slices.Equal(got, tt.want) {
			t.Errorf("scriptsIn(%q) = %q, want %q", tt.command, got, tt.want)
		}
	}
}

// Each line of a script of more than one line is matched as a script as
// well, so a quote in a comment or a here-document's body, which the
// scanner reads as one, hides no line the shell runs; a backslash-newline
// joins a name the shell joins.
func TestMatchScriptReadsEachLine(t *testing.T) {
	tests := []struct{ script, want string }{
		{"ls #'\nln -s conf $'\\x2epi'\n#'", ".pi"},
		{"cat <<EOF\nit's\nEOF\nmv conf $'\\056claude'", ".claude"},
		{"ln -s conf .p\\\ni", ".pi"},
		{"ls\nls", ""},
	}
	for _, tt := range tests {
		if got := matchScript(tt.script, 0); got.protected != tt.want {
			t.Errorf("matchScript(%q).protected = %q, want %q", tt.script, got.protected, tt.want)
		}
	}
	if got := linesOf("ls"); got != nil {
		t.Errorf("linesOf(%q) = %q, want none", "ls", got)
	}
	assertShellDenied(t, []string{"echo x > .p\\\ni/settings.json", "echo x > \"@C/settings.js\\\non\""})
}

// Scripts inside scripts are matched to maxScriptDepth; a script one
// deeper is not, and the floor, which reads the whole command's text,
// still denies what lies in it.
func TestMatchScriptStopsAtTheDepthLimit(t *testing.T) {
	// The eval's script names `.pi` only once its `$'…'` string is decoded.
	script := `eval "ln -s conf \$'\\x2epi'"`
	if at := matchScript(script, maxScriptDepth); at.protected != "" {
		t.Errorf("a script at the limit: %+v, want its inner script unmatched", at)
	}
	if above := matchScript(script, maxScriptDepth-1); above.protected != ".pi" {
		t.Errorf("a script above the limit: %+v, want .pi", above)
	}
	// Scripts nested past the limit, each a shell -c inside the last.
	command := "ln -s conf $d"
	for range maxScriptDepth + 1 {
		command = "sh -c " + shellQuote(command)
	}
	command = "d=.pi; " + command
	got, ok := protectedInCommand(command)
	if !ok || got != ".pi" {
		t.Errorf("protectedInCommand(%q) = %q, %v; want .pi", command, got, ok)
	}
}

// shellQuote quotes s as one shell word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
