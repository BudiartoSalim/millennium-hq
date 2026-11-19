package domain

import "testing"

// Outside quotes, only letters, digits, spaces, tabs, `. _ / : , @ + -`,
// the globs `*`, `?`, `[` and `]`, `=` and `~` where they are inert, and
// the listed operators are inert text; inside quotes, printable ASCII but
// `$`, a backquote, a backslash, `!`, `#`, `(` and `)`, the quotes
// balanced. Everything else, `<` in every form and zsh's glob operators
// included (R10-SEC-1, R10-SEC-2), is not.
func TestInertShellText(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"", true},
		{"ls -la ./x_y/Z9:a,b@c+d *.go ?x [ab] ]", true},
		{"ls\tx", true},
		{"ls 'a b{}<>^%~*?;&|=' \"x y'{}<>^%~\" ''", true},
		{`ls "it's"`, true},
		{"ls x | y || z; w && v", true},
		{"ls > x >> y 2> z 2>> w >&2 2>&1 1>&2 >&- &> x &>> y", true},
		{"ls>x;ls>>x", true},
		{"[ a = b ] && test x = y", true},
		{"ls --a=b x=y a=", true},
		{"ls ~ ~/x >~/y|~ ;~/z", true},
		{"cd ~;ls", true},

		{"ls 'x", false},
		{`ls "x`, false},
		{"ls 'a''b'", false},
		{"ls ''''", false},
		{"ls '$x'", false},
		{`ls "$x"`, false},
		{"ls '`x`'", false},
		{`ls 'a\b'`, false},
		{"ls '!x'", false},
		{"ls '#x'", false},
		{"ls '('", false},
		{"ls ')'", false},
		{"ls '\tx'", false},
		{"ls 'x\ny'", false},
		{"ls 'café'", false},
		{"ls '\x7f'", false},

		{"ls < x", false},
		{"ls <-> x", false},
		{"ls <1-9>", false},
		{"ls /dev/fd/<->", false},
		{"ls <&0", false},
		{"ls <> x", false},
		{"ls <<< x", false},
		{"ls ^x", false},
		{"ls !x", false},
		{"ls %1", false},
		{"ls {a,b}", false},
		{"ls }", false},
		{"ls (x)", false},
		{"ls )", false},
		{"ls #x", false},
		{"ls $x", false},
		{"ls `x`", false},
		{`ls \x`, false},
		{"ls\nx", false},
		{"ls\rx", false},
		{"ls\x00x", false},
		{"ls\vx", false},
		{"ls\x7fx", false},
		{"ls é", false},

		{"ls &", false},
		{"ls & x", false},
		{"ls |& x", false},
		{"ls ;; x", false},
		{"ls >| x", false},
		{"ls &| x", false},
		{"ls >>& x", false},
		{"ls &&> x", false},
		{"ls >&> x", false},

		{"ls =ls", false},
		{"ls == x", false},
		{"[ a == b ]", false},
		{"ls x==y", false},
		{"ls --a==b", false},
		{"=ls x", false},
		{"ls >=x", false},

		{"ls ~x", false},
		{"ls ~+", false},
		{"ls ~-", false},
		{"ls a~b", false},
		{"ls *~x", false},
		{"ls x/~/y", false},
		{"ls --a=~/x", false},
		{"ls ~'/x'", false},
		{`ls ~"x"`, false},
		{"ls '~'~", false},
	}
	for _, tt := range tests {
		if got := inertShellText(tt.text); got != tt.want {
			t.Errorf("inertShellText(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

// What the whitelist refuses keeps a command that names a protected path
// from passing as read-only, wherever the refused text stands.
func TestProvablyReadOnlyNeedsInertText(t *testing.T) {
	assertProvablyReadOnly(t, false, []string{
		"ls @C <->", "ls @C ^x", "ls @C a~b", "ls @C ~x", "ls @C %1", "ls @C !x", "ls @C =ls", "ls @C &",
		"ls @C 'a''b'", "ls @C 'x",
	})
	assertProvablyReadOnly(t, true, []string{
		"ls @C ~ ~/@C", "ls '@C' \"@P\" > /dev/null", "[ -d @C ] && ls @C", "ls @C &> /dev/null",
	})
	// The examples of docs/hooks.md: denied as written, and passing once
	// rewritten with inert text.
	assertShellDenied(t, []string{
		"grep x < @C/settings.json > /tmp/x", "find @C ! -name x 2>/dev/null", "git log -n 5 -- @C > /tmp/x",
	})
	cfg := guardConfig(t)
	for _, command := range []string{"find @C -not -name x 2>/dev/null", "git log -n5 -- @C > /tmp/x"} {
		command = spell(command)
		if got := Guard(cfg, ToolCall{Tool: "Bash", Command: command}); got.Deny {
			t.Errorf("%q: %s; a read-only command must pass", command, got.Reason)
		}
	}
}
