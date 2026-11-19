package domain

import "testing"

// assertProvablyReadOnly fails unless provablyReadOnly reports want for
// each command, its placeholders spelled out (see spell).
func assertProvablyReadOnly(t *testing.T, want bool, commands []string) {
	t.Helper()
	for _, command := range commands {
		command = spell(command)
		if got := provablyReadOnly(command); got != want {
			t.Errorf("provablyReadOnly(%q) = %v, want %v", command, got, want)
		}
	}
}

// A command the scanner may read otherwise than the shell does is not
// provably read-only: a comment, in which a quote does not count
// (R8-SEC-1, R8-CR-1), a here-document, whose body the shell reads by
// lines (R8-SEC-2), a backslash, with which a newline joins lines
// (R8-SEC-3), a here-string, a second line, a character outside
// printable ASCII, and a variable a redirection assigns.
func TestProvablyReadOnlyRefusesWhatTheScannerMayMisread(t *testing.T) {
	assertProvablyReadOnly(t, false, []string{
		"ls #'\nprintf x > @C/settings.json\n#'",
		"ls # it's",
		"ls @C#x",
		"ls @C > /tmp/x # list",
		"cat <<X\nls '\nX\nprintf y > @C/settings.json\n#'",
		"cat <<-X",
		"grep x <<< y",
		"echo x >\\\n@C/settings.json",
		`ls \@C`,
		"ls @C\nls",
		"ls @C\rls",
		"ls\v@C",
		"ls\x00@C",
		"ls @C\x7f",
		"ls café @C",
		"ls @C",
		"ls @C {PATH}>/dev/null",
		"ls @C {fd}<x",
		"ls @C {fd}&>x",
	})
	assertProvablyReadOnly(t, true, []string{
		"ls @C",
		"ls\t@C ; ls  @P",
		"ls '{x}' @C > /dev/null",
		"ls @C < x > /tmp/y",
	})
}

// find is read-only only with the primaries and options of findReadOnly,
// `-newerXY` and numbers: bfs's `-rm` and every other action is not
// (R8-SEC-4).
func TestProvablyReadOnlyFind(t *testing.T) {
	assertProvablyReadOnly(t, true, []string{
		"find @C",
		"find -L @C -H -P",
		"find . -path ./@C -prune -o -name '*.go' -print 2>/dev/null",
		"find @C -type f -mtime -1 -size +10k -perm -644 -maxdepth 2 -print0",
		"find @C ! -iname x -a '(' -newer y -or -newermt 2024-01-01 ')' -not -empty -ls",
		"find @C -newerBt x -printf '%p'",
	})
	assertProvablyReadOnly(t, false, []string{
		"find @M -name config.json -rm",
		"find @C -name settings.json -rm 2>/dev/null",
		"find @P/extensions -type f -rm",
		"find @C -exec rm {} +",
		"find @C -execdir rm {} +",
		"find @C -ok rm {} ;",
		"find @C -okdir rm {} ;",
		"find @C -delete",
		"find @C -fprint /tmp/x",
		"find @C -fprint0 /tmp/x",
		"find @C -fprintf /tmp/x %p",
		"find @C -fls /tmp/x",
		"find @C -exit",
		"find @C -D tree",
		"find @C -newerxy x",
		"find @C -newermtt x",
		"find @C -",
		"find @C -name -rm",
		"find @C -del*",
	})
}

// rg is read-only without an option that runs a program, spelled whole,
// with a value, abbreviated, or as a short option in a bundle.
func TestProvablyReadOnlyRipgrep(t *testing.T) {
	assertProvablyReadOnly(t, true, []string{
		"rg foo @C",
		"rg -n --hidden --no-search-zip -g '*.go' foo @C -- -x",
	})
	assertProvablyReadOnly(t, false, []string{
		"rg --pre ./x foo @C",
		"rg --pre=./x foo @C",
		"rg --pr=./x foo @C",
		"rg --pre-glob '*.gz' foo @C",
		"rg --hostname-bin ./x foo @C",
		"rg --hostname-bin=./x foo @C",
		"rg --host ./x foo @C",
		"rg -z foo @C",
		"rg -uz foo @C",
		"rg --search-zip foo @C",
		"rg --search foo @C",
	})
}

// git is read-only only with a read-only command and no output option
// after it, abbreviated or not.
func TestProvablyReadOnlyGitOutput(t *testing.T) {
	assertProvablyReadOnly(t, true, []string{
		"git log --oneline -- @C",
		"git diff --stat -- @C > /tmp/x",
	})
	assertProvablyReadOnly(t, false, []string{
		"git log --outp=@C/settings.json 2>/dev/null",
		"git log --out @C/settings.json",
		"git log --ou=x -- @C",
		"git diff --output=x -- @C",
		"git diff --output-indicator-new=x -- @C",
		"git log -o x -- @C",
	})
}

// namesLongOption reads an option whole, with a value, abbreviated, or
// extended, and nothing that is not a long option.
func TestNamesLongOption(t *testing.T) {
	longs := []string{"--pre", "--hostname-bin"}
	tests := []struct {
		arg  string
		want bool
	}{
		{"--pre", true},
		{"--pre=x", true},
		{"--pr", true},
		{"--p", true},
		{"--pre-glob", true},
		{"--h=x", true},
		{"--", false},
		{"-", false},
		{"", false},
		{"-pre", false},
		{"pre", false},
		{"--hidden", false},
		{"--no-pre", false},
	}
	for _, tt := range tests {
		if got := namesLongOption(tt.arg, longs); got != tt.want {
			t.Errorf("namesLongOption(%q) = %v, want %v", tt.arg, got, tt.want)
		}
	}
}

// cd and pushd are read-only only with one explicit directory outside
// protected paths: never none, `~`, `-` or a bare name, which go where
// HOME, OLDPWD or CDPATH say (R8-SEC-5); popd never is. A variable
// assignment is never read-only, so the command cannot set them.
func TestProvablyReadOnlyChdir(t *testing.T) {
	assertProvablyReadOnly(t, true, []string{
		"cd ./x && cat y",
		"cd ../x; ls @C",
		"cd /tmp && ls @C > out.txt",
		"cd . && ls @C",
		"cd .. && ls @C",
		"pushd ./sub; ls @C",
	})
	assertProvablyReadOnly(t, false, []string{
		"cd; ls @C",
		"cd ~ && ls @C",
		"cd ~/x && ls @C",
		"cd ~- && ls @C",
		"cd - && ls @C",
		"cd '' && ls @C",
		"cd x && ls @C",
		"cd .x && ls @C",
		"cd ..x && ls @C",
		"cd +1 && ls @C",
		"cd -P ./x && ls @C",
		"cd ./x ./y && ls @C",
		"cd ./@C && ls",
		"cd ./x/../@P && ls",
		"cd ./x* && ls @C",
		"pushd x; ls @C",
		"pushd; ls @C",
		"popd; ls @C",
		"popd +1; ls @C",
		"HOME=./@C cd ./x; ls",
		"CDPATH=@P cd ./x; ls",
		"ls @C; X=1",
		"printf -v HOME %s @C && cd && printf {} > settings.json",
		"printf -v CDPATH %s @P && cd extensions && printf evil > x.ts",
		"printf -v HOME %s @C && cd ~ && echo {} > settings.json",
	})
}

// zsh reads `>!` and `>>!` as operators that write the word after them,
// so a redirection target that starts with `!` is not safe.
func TestSafeRedirectRefusesZshClobber(t *testing.T) {
	tests := []struct {
		redirect shellRedirect
		want     bool
	}{
		{shellRedirect{op: ">", target: "/tmp/x"}, true},
		{shellRedirect{op: ">", target: "!"}, false},
		{shellRedirect{op: ">>", target: "!/tmp/x"}, false},
		{shellRedirect{op: ">&", target: "2"}, true},
		{shellRedirect{op: ">&", target: "-"}, true},
		{shellRedirect{op: ">&", target: "!"}, false},
	}
	for _, tt := range tests {
		if got := safeRedirect(tt.redirect); got != tt.want {
			t.Errorf("safeRedirect(%+v) = %v, want %v", tt.redirect, got, tt.want)
		}
	}
	assertProvablyReadOnly(t, false, []string{"ls >! @C/settings.json", "ls >>! @C/settings.json"})
}

// The reviewers' probes of round 7 (R8-SEC-1 to R8-SEC-6, R8-CR-1), each
// of which the shell runs as a write of a protected path: each is
// denied. The read-only commands among their controls still pass.
func TestGuardShellDeniesTheRound7Probes(t *testing.T) {
	assertShellDenied(t, []string{
		"ls #'\nprintf x > @C/settings.json\n#'",
		"ls #\"\nprintf x > @C/settings.json\n#\"",
		"ls #'\nln -s conf @P\n#'",
		"ls # it's\nln -s conf @P",
		"ls #'\necho {} > @C/settings.json",
		"ls # x\\\nln -s conf @P",
		"cat <<X\nls '\nX\nprintf y > @C/settings.json\n#'",
		"cat <<EOF\necho it's\nEOF\nln -s conf @P",
		"cat <<'EOF'\necho '\nEOF\necho {} > @C/settings.json",
		"file --co -m @M/x 2>/dev/null",
		"file --compile -m @M/x 2>/dev/null",
		"git log --outp=@C/settings.json 2>/dev/null",
		"git log --out @C/settings.json",
		"git diff --output=@C/settings.json",
		"printf -v PATH %s /tmp/e && cat @C/settings.json > /dev/null",
		"cd ~- && printf x > settings.json; ls @C >/dev/null",
		"ls &> @C/settings.json",
		"ls > >(cat) @C",
		"ls 2>&1 > /dev/null; cat <> @C/settings.json",
		"find . -fprint0 @C/settings.json",
		"find . -fprintf @C/settings.json x",
		"find . -fprint @C/settings.json",
		"find . -fls @C/settings.json",
		"find @C -delete",
		"rg --pre=x . @C >/dev/null",
		"echo x >\\\n@C/settings.json",
		"ls \\\n> @C/settings.json",
		"echo {} 1>/dev/null >>@C/settings.json",
		"printf -v HOME %s @C && cd && printf {} > settings.json",
		"printf -v CDPATH %s @P && cd extensions && printf evil > x.ts",
		"printf -v HOME %s @C && cd ~ && echo {} > settings.json",
		"find @M -name config.json -rm",
		"find @C -name settings.json -rm 2>/dev/null",
		"find @P/extensions -type f -rm",
		"cd \\\n@C && printf {} > settings.json",
		"echo x > \"\\\n@C/settings.json\"",
		"ls; ln -s conf @P",
		"ls @C {PATH}>/dev/null; ls > /tmp/x",
		"ls >! @C/settings.json",
	})
	cfg := guardConfig(t)
	for _, command := range []string{
		"ls -la @C 2>/dev/null", "git -C @C log > /tmp/x", "cat @P/extensions/x.ts > /tmp/x.ts", "ls >&- @C",
	} {
		command = spell(command)
		if got := Guard(cfg, ToolCall{Tool: "Bash", Command: command}); got.Deny {
			t.Errorf("%q: %s; a read-only command must pass", command, got.Reason)
		}
	}
}
