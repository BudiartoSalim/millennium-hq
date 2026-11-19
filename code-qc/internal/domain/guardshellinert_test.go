package domain

import "testing"

// Outside quotes, only letters, digits, spaces, tabs, `. _ / : , @ + -`,
// the globs `*`, `?`, `[` and `]`, `=` and `~` where they are inert, and
// the listed operators are inert text; inside quotes, printable ASCII but
// `$`, a backquote, a backslash, `!`, `#`, `(` and `)`, the quotes
// balanced. Everything else, an unquoted `<` in every form and zsh's
// glob operators included (R10-SEC-1, R10-SEC-2), `&>` (R11-SEC-1), a
// descriptor of more than one digit (R11-SEC-2) and `=` after a `:`
// (R11-SEC-4), is not.
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
		{"ls > x >> y 2> z 2>> w >&2 2>&1 1>&2 >&- 1>&- 9> v 0>> u", true},
		{"ls 2>&1|x; ls >&2&&ls 1>&2>x", true},
		{"ls>x;ls>>x", true},
		{"ls x=a:b c:d=e", true},
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

		// `&>` and `&>>`, at whose `&` the scanner ends the command
		// (R11-SEC-1), and `>&` to a file.
		{"ls &> x", false},
		{"ls &>x", false},
		{"ls &>> x", false},
		{"ls 2&> x", false},
		{"ls 1&>> x", false},
		{"ls >& x", false},
		{"ls >&x", false},
		{"ls >&", false},
		{"ls >&12", false},
		{"ls >&2x", false},
		{"ls >&-x", false},
		{"ls 2>& 1", false},

		// More than one digit before a redirection, which zsh reads as a
		// word (R11-SEC-2).
		{"ls 10> x", false},
		{"ls 12>> x", false},
		{"ls 99>&1", false},
		{"ls 007>x", false},
		{"ls x10>y", false},

		{"ls =ls", false},
		{"ls == x", false},
		{"[ a == b ]", false},
		{"ls x==y", false},
		{"ls --a==b", false},
		{"=ls x", false},
		{"ls >=x", false},
		// zsh's MAGIC_EQUAL_SUBST expands `=cmd` after a `:` (R11-SEC-4).
		{"ls x=a:=rm", false},
		{"ls a:=b", false},
		{"ls x=a:b:=c", false},

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
		"ls @C 'a''b'", "ls @C 'x", "ls @C &> /dev/null", "ls @C 10>/dev/null", "ls x=a:=rm @C > /tmp/x",
	})
	assertProvablyReadOnly(t, true, []string{
		"ls @C ~ ~/@C", "ls '@C' \"@P\" > /dev/null", "[ -d @C ] && ls @C", "ls @C > /dev/null 2>&1",
		// A quoted `<` is literal (R11-CR-2).
		"grep '<x>' @C/settings.json > /tmp/x",
	})
	// The examples of docs/hooks.md: denied as written, and passing once
	// rewritten with inert text.
	assertShellDenied(t, []string{
		"grep x < @C/settings.json > /tmp/x", "find @C ! -name x 2>/dev/null", "git log -n 5 -- @C > /tmp/x",
		"ls @C &>/dev/null",
	})
	cfg := guardConfig(t)
	for _, command := range []string{
		"find @C -not -name x 2>/dev/null", "git log -n5 -- @C > /tmp/x", "ls @C >/dev/null 2>&1",
	} {
		command = spell(command)
		if got := Guard(cfg, ToolCall{Tool: "Bash", Command: command}); got.Deny {
			t.Errorf("%q: %s; a read-only command must pass", command, got.Reason)
		}
	}
}
