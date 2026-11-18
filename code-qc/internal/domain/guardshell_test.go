package domain

import (
	"slices"
	"strings"
	"testing"
)

// A simple command puts, moves or removes something when a replacing
// command or a replacing git command stands anywhere among its words,
// past any prefix and its option values; git's subcommand is found past
// git's own options and their values.
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
		{[]string{"sudo", "git", "--git-dir", "x", "-C", "sub", "-c", "k=v", "--no-pager", "restore", "a"}, true},
		{[]string{"git", "checkout", "main"}, true},
		{[]string{"git", "status", "mv"}, true},
		{[]string{"git", "log", "--", ".pi"}, false},
		{[]string{"git", "--work-tree"}, false},
		{[]string{"git"}, false},
	}
	for _, tt := range tests {
		if got := isReplacing(tt.words); got != tt.want {
			t.Errorf("isReplacing(%q) = %v, want %v", tt.words, got, tt.want)
		}
	}
}

// The scripts a simple command runs are eval's arguments and a shell's -c
// script, wherever among its words the eval or the shell stands.
func TestScriptsOf(t *testing.T) {
	tests := []struct {
		words, want []string
	}{
		{nil, nil},
		{[]string{"ls", "-c", "x"}, nil},
		{[]string{"eval", "ln", "-s", "conf .pi"}, []string{"ln -s conf .pi"}},
		{[]string{"eval"}, []string{""}},
		{[]string{"sudo", "-u", "root", "/bin/bash", "--norc", "-lc", "rm a", "b"}, []string{"rm a"}},
		{[]string{"find", ".", "-exec", "sh", "-ec", "mv {} x", ";"}, []string{"mv {} x"}},
		{[]string{"eval", "sh", "-c", "x"}, []string{"sh -c x", "x"}},
		{[]string{"bash", "-c"}, nil},
		{[]string{"bash", "--norc", "script.sh"}, nil},
	}
	for _, tt := range tests {
		if got := scriptsOf(tt.words); !slices.Equal(got, tt.want) {
			t.Errorf("scriptsOf(%q) = %q, want %q", tt.words, got, tt.want)
		}
	}
}

// What a simple command changes cannot be placed when its replacing
// command takes operands from an expansion or from xargs' input, when the
// command is named by an expansion, or when it changes into a leading part
// of a protected path.
func TestUnplaced(t *testing.T) {
	tests := []struct {
		words []string
		want  bool
	}{
		{[]string{"ln", "-s", "conf", "x"}, false},
		{[]string{"ln", "-s", "conf", "$d"}, true},
		{[]string{"mv", "`x`", "y"}, true},
		{[]string{"xargs", "rm", "-rf"}, true},
		{[]string{"ls", "$HOME/.claude"}, false},
		{[]string{"X=1", "$RM", "-rf", "x"}, true},
		{[]string{"X=1"}, false},
		{[]string{"cd", "sub/.claude/"}, true},
		{[]string{"pushd", ".pi"}, true},
		{[]string{"cd", ".claude/worktrees/x"}, false},
		{[]string{"ls", ".claude"}, false},
	}
	for _, tt := range tests {
		replacing := isReplacing(tt.words)
		if got := unplaced(tt.words, replacing); got != tt.want {
			t.Errorf("unplaced(%q, %v) = %v, want %v", tt.words, replacing, got, tt.want)
		}
	}
}

// Scripts inside scripts are matched to maxScriptDepth; a script one
// deeper is not placed, so the coarse fallback reads the whole command.
func TestMatchScriptStopsAtTheDepthLimit(t *testing.T) {
	inner := matchScript("eval ln -s conf x", maxScriptDepth)
	if inner.protected != "" || !inner.unplaced {
		t.Errorf("a script at the limit: %+v, want unplaced", inner)
	}
	if above := matchScript("eval ln -s conf x", maxScriptDepth-1); above.unplaced {
		t.Errorf("a script above the limit: %+v, want placed", above)
	}
	if at := matchScript("ls x", maxScriptDepth); at.unplaced {
		t.Errorf("a command that runs no script at the limit: %+v, want placed", at)
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
