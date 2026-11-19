package domain

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// spell replaces the placeholders of a reviewer's case with the names it
// stands for: @P `.pi`, @C `.claude`, @M `.millennium`.
func spell(command string) string {
	return strings.NewReplacer("@P", ".pi", "@C", ".claude", "@M", ".millennium").Replace(command)
}

// assertShellDenied fails unless the guard denies each command.
func assertShellDenied(t *testing.T, commands []string) {
	t.Helper()
	cfg := guardConfig(t)
	for _, command := range commands {
		command = spell(command)
		t.Run(command, func(t *testing.T) {
			if got := Guard(cfg, ToolCall{Tool: "Bash", Command: command}); !got.Deny {
				t.Errorf("%q: allowed", command)
			}
		})
	}
}

// A protected path or a leading part of one reached through a variable
// or a command substitution is denied whichever command writes (R7-CR-1).
func TestGuardShellDeniesWritesThroughAnExpandedLeadingPart(t *testing.T) {
	assertShellDenied(t, []string{
		"D=@C; echo '{}' > $D/settings.json",
		"D=@C; tee $D/settings.json < evil.json",
		"D=@P; sed -i s/a/b/ $D/settings.json",
		`echo '{}' > "$(printf @C)/settings.json"`,
		`echo '{}' > $(echo @C)/settings.json`,
		"D=@C; cd $D && tee settings.json < x",
		"x=@P; ln -s conf \"$x\"",
		"sh -c 'x=@P; ln -s conf $x'",
		"cmd=ln; $cmd -s conf @P",
	})
}

// A command run in a leading part of a protected path by an option that
// changes directory, as cd does, is denied as cd is (R7-CR-2).
func TestGuardShellDeniesWritesInALeadingPartEnteredByAChdirOption(t *testing.T) {
	assertShellDenied(t, []string{
		"env -C @C sh -c 'cat /tmp/evil > settings.json'",
		"env --chdir=@P tee settings.json < /tmp/evil",
		"sudo -D @C tee settings.json < x",
		"cd @P && ln -s ../conf extensions",
		"cd @C && cat README.md 2>/dev/null",
		"cd ~/@C && ls 2>/dev/null",
		"cd @C && git log --oneline -3 2>&1 | head",
	})
}

// Writers other than the replacing ones, with a leading part of a
// protected path or a protected path as an operand (R7-CR-3), including
// the in-place editors and file commands round 4 did not name.
func TestGuardShellDeniesOtherWritersOfALeadingPart(t *testing.T) {
	assertShellDenied(t, []string{
		"chmod 000 @C",
		"tar -xf evil.tar -C @P 2>/dev/null",
		"rsync -a conf/ @P 2>/dev/null",
		"tar -xf evil.tar @P",
		"rsync -a conf/ @P",
		"unlink @P",
		"link conf @P",
		"perl -i -pe s/a/b/ '@C'/settings.json",
		"awk -i inplace 1 '@C'/settings.json",
		`sed --in-place s/a/b/ "@C"/settings.json`,
		`sed -e s/a/b/ -i "@C"/settings.json`,
		`sed -i'' s/a/b/ "@C"/settings.json`,
		"cat x | sponge '@C'/settings.json",
		"patch '@C'/settings.json < x.diff",
		"dd of=@P/extensions/a.ts",
		"dd of='@P'/extensions/a.ts",
	})
}

// A replacing command in a script that is not an eval or sh -c argument
// is denied: piped or fed to a shell, run by env -S, watch, su, script,
// flock or ssh, set as a git alias, or written to a script file
// (R7-SEC-1).
func TestGuardShellDeniesScriptsRunOtherwise(t *testing.T) {
	assertShellDenied(t, []string{
		"env -S 'ln -s conf @P'",
		"watch -n1 'ln -s conf @P'",
		"su -c 'ln -s conf @P'",
		"script -qc 'mv conf @C' /dev/null",
		"flock /tmp/l -c 'ln -s conf @P'",
		"echo 'ln -s conf @P' | sh",
		"printf 'mv conf @C' | bash",
		"sh <<< 'ln -s conf @P'",
		"bash <<EOF\nln -s conf @P\nEOF",
		"git -c alias.x='!ln -s conf @P' x",
		"ssh localhost 'ln -s conf @P'",
		"busybox sh -c 'ln -s conf @P'",
		"echo 'ln -s conf @P' > /tmp/x.sh; sh /tmp/x.sh",
		`echo hi; eval "$(printf 'ln -s conf @P')"`,
		`eval $'ln -s conf \x2epi'`,
		`bash -c $'ln -s conf \x2epi'`,
	})
}

// A shell's -c script is found past any of its options (R7-SEC-2).
func TestGuardShellDeniesScriptsPastAShellsOptions(t *testing.T) {
	assertShellDenied(t, []string{
		"bash -c -- 'ln -s conf @P'",
		"bash -c -e 'ln -s conf @P'",
		"sh -c -e 'mv conf @C'",
		"bash -xc 'ln -s conf @P'",
		"bash -o errexit -c 'ln -s conf @P'",
		"bash --norc -c 'ln -s conf @P'",
		"bash -O extglob -c 'ln -s conf @P'",
	})
}

// A git command that restores files is found past a prefix's value that
// is itself `git`, past git's own options, and through an alias that -c
// or --config-env sets (R7-SEC-3).
func TestGuardShellDeniesGitPastItsOptionsAndAliases(t *testing.T) {
	assertShellDenied(t, []string{
		"sudo -u git git checkout main -- @C",
		"git --config-env x=Y checkout main -- @C",
		"git -c alias.co=checkout co main -- @C",
		"git --attr-source HEAD restore --source=HEAD~3 @P",
	})
}

// The rest of the security engineer's probes of round 6: spellings of
// the target and of the command that round 4 denied.
func TestGuardShellDeniesTheRound6Probes(t *testing.T) {
	assertShellDenied(t, []string{
		"ln -s conf .p''i", `ln -s conf .p""i`, "ln -s conf ./@P/.", "ln -s conf @P:x", "ln -s conf '@P '",
		"ln -T conf @P", "ln -s conf ~+/@P", "ln -s conf $PWD/@P", `ln -s conf "${PWD}/@P"`, `\ln -s conf @P`,
		"'ln' -s conf @P", `l\n -s conf @P`, "$(echo ln) -s conf @P", "echo x >> @M/config.json",
		"echo x > $'@M'/config.json", `echo x > .mill""ennium/config.json`, "echo x 1> @C/settings.local.json",
		"echo x &> @P/extensions/a.ts", `echo x >"@P/extensions"/a.ts`, "ln -sconf @P", "cp -t @P conf/extensions",
		"cp --target-directory @P conf/extensions",
	})
}

// Documented limits (docs/hooks.md): a name spliced from parts the shell
// joins when it runs (R7-SEC-4), a glob or brace expansion, and a script
// in another language. Each still passes; one that starts being denied
// moves to the lists above.
func TestGuardShellDocumentedLimits(t *testing.T) {
	cfg := guardConfig(t)
	for _, command := range []string{
		"ln -s conf .p${X}i", "ln -s conf ${X:-@P}", "ln -s conf .p$@i", "mv -t@P conf/extensions",
		"cp x @C/settings.js*", "cp x @C/settings.js?n", "echo x > @C/settings.{json,}",
		`python3 -c "import os; os.symlink('conf','@P')"`,
	} {
		command = spell(command)
		if got := Guard(cfg, ToolCall{Tool: "Bash", Command: command}); got.Deny {
			t.Errorf("%q: denied (%s); it is no longer a limit", command, got.Reason)
		}
	}
}

// A command is provably read-only only when it has no expansion, each of
// its simple commands is on the read-only allowlist with arguments that
// keep it read-only, and every redirection writes outside protected
// paths; in doubt, it is not.
func TestProvablyReadOnly(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"", true},
		{"ls -la @C 2>/dev/null", true},
		{"cat @P/settings.json | head -5 | wc -l; echo done && true || test -d @C; [ -d @P ]", true},
		{"tail -n 3 x; egrep a x; fgrep b x; du -sh ~/@C 2>&1; stat @C; readlink @P; realpath @P", true},
		{"basename @C; dirname @P/x; printf '%s' x; grep -rn rm @C; (ls @C)", true},
		{"ls $HOME/@C", false},
		{"ls `pwd`/@C", false},
		{"ls '$x'", false},
		{"X=1 ls @C", false},
		{"eval ls @C", false},
		{"bash -c 'ls @C'", false},
		{"sudo ls @C", false},
		{"{ ls @C; }", false},
		{"if ls @C; then ls; fi", false},
		{"ls @C | xargs rm", false},
		{"cat <<EOF\nrm -rf @C\nEOF", false},
		{"grep x <<< 'ln -s conf @P'", true},
		{"find . -path ./@C -prune -o -name '*.go' -print 2>/dev/null", true},
		{"find @C -exec rm {} +", false},
		{"find @C -execdir rm {} +", false},
		{"find @C -ok rm {} ;", false},
		{"find @C -delete", false},
		{"find @C -fprint /tmp/x", false},
		{"find @C -fls /tmp/x", false},
		{"find @C -del*", false},
		{"find @C {-delete,}", false},
		{"rg foo @C", true},
		{"rg --pre ./x foo @C", false},
		{"rg --pre=./x foo @C", false},
		{"rg --hostname-bin ./x foo @C", false},
		{"ls\r@C", false},
		{"ls @C", false},
		{"ls\t@C\nls  @P", true},
		{"file @C/settings.json", true},
		{"file -C -m x", false},
		{"file -bC x", false},
		{"file --compile x", false},
		{"git status -- @C", true},
		{"git -C @C --no-pager -P --no-optional-locks log -- x > /tmp/x", true},
		{"git diff main -- @C > /tmp/x", true},
		{"git rev-parse HEAD; git ls-files @P; git show HEAD:@C/settings.json", true},
		{"git -c core.pager=x log", false},
		{"git --config-env=x=Y log", false},
		{"git --exec-path=/tmp log", false},
		{"git --git-dir=x log", false},
		{"git -C", false},
		{"git", false},
		{"git checkout -- @C", false},
		{"git grep x @C", false},
		{"git diff --output=@C/settings.json", false},
		{"git log -o x", false},
		{"git log -- '*.go'", true},
		{"git log -- *.go", false},
		{"cd; ls @C", true},
		{"cd sub && ls @C > /tmp/x", true},
		{"cd ..", true},
		{"cd @C && ls", false},
		{"cd sub/@P/x", false},
		{"cd @M", false},
		{"cd -", false},
		{"cd a b", false},
		{"cd x*", false},
		{"pushd sub; ls @C; popd", true},
		{"pushd", false},
		{"pushd +1", false},
		{"popd x", false},
		{"ls @C 2>&1 >&2 <&0 >&-", true},
		{"ls @C > /dev/null 2> /dev/stderr >> /tmp/out.txt", true},
		{"ls > @C", false},
		{"ls > sub/@P", false},
		{"ls > @C/worktrees/x/out", false},
		{"ls > @C/settings.json", false},
		{"ls > '@C'/settings.json", false},
		{`ls > '@C\settings.json'`, false},
		{"ls > @M/config.json", false},
		{"ls > @C./x", false},
		{"ls >& @P/settings.json", false},
		{"ls > x*", false},
		{"ls > ;", false},
		{"cat <(ls)", false},
		{"ls < @C/settings.json", false},
		{"> /tmp/x", true},
		{"> @P", false},
		{"cp x @C", false},
		{"tee @C/settings.json", false},
		{"LS @C", false},
		{"/bin/ls @C", false},
	}
	for _, tt := range tests {
		command := spell(tt.command)
		if got := provablyReadOnly(command); got != tt.want {
			t.Errorf("provablyReadOnly(%q) = %v, want %v", command, got, tt.want)
		}
	}
}

// The floor reads a protected path or a leading part of one in every
// field of the command and of its words: as round 4 split the command,
// at expansions too, around `=`, cleaned, as written and with backslashes
// read as Win32 reads them.
func TestShellFloor(t *testing.T) {
	tests := []struct{ command, want string }{
		{"ls @C", ""},
		{"rm x", ""},
		{"rm @C/worktrees/x", ""},
		{"rm -rf @C", ".claude"},
		{"echo x > @C/settings.json", ".claude/settings.json"},
		{"cp x '@C'/settings.json", ".claude/settings.json"},
		{`cp x '@C\settings.json'`, ".claude/settings.json"},
		{`cp x '@P\'`, ".pi"},
		{"rm '@P/x{/..'", ".pi"},
		{"rm a${X}/@P", ".pi"},
		{"tee $D/@C", ".claude"},
		{"x --dir=@P > y", ".pi"},
		{`mv a $'\x2epi'`, ".pi"},
		{"cmd=ln; $cmd -s conf @P", ".pi"},
	}
	for _, tt := range tests {
		command := spell(tt.command)
		m := matchScript(command, 0)
		got, ok := shellFloor(command, m)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("shellFloor(%q) = %q, %v; want %q", command, got, ok, tt.want)
		}
	}
}

// The floor skips a field that holds none of protectedRoots, which is
// sound only while they are the first segments of the protected paths and
// no character outside ASCII folds to one of their letters.
func TestProtectedRootsStartEveryProtectedPath(t *testing.T) {
	var firsts []string
	for _, protected := range ProtectedPaths() {
		first, _, _ := strings.Cut(protected, "/")
		if !slices.Contains(firsts, first) {
			firsts = append(firsts, first)
		}
	}
	if !slices.Equal(firsts, protectedRoots) {
		t.Errorf("protectedRoots = %q, want the first segments of the protected paths, %q", protectedRoots, firsts)
	}
	for _, root := range protectedRoots {
		for _, r := range root {
			if f, ok := foldOutsideASCII(r); ok {
				t.Errorf("%q of %q folds to %q, outside ASCII", r, root, f)
			}
		}
	}
}

// foldOutsideASCII is a character outside ASCII that r folds to, if any.
func foldOutsideASCII(r rune) (rune, bool) {
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f > unicode.MaxASCII {
			return f, true
		}
	}
	return 0, false
}

// A field may name a protected path only if it holds one of their first
// segments, in any ASCII case, anywhere.
func TestMayNameProtected(t *testing.T) {
	tests := []struct {
		field string
		want  bool
	}{
		{"", false},
		{"no dot", false},
		{"./x/.p", false},
		{".pi", true},
		{"a.PI.b", true},
		{"..Claude", true},
		{"x/.MILLENNIUM", true},
		{".millenniu", false},
		{".clau.de", false},
		{".ſettings", false},
	}
	for _, tt := range tests {
		if got := mayNameProtected(tt.field); got != tt.want {
			t.Errorf("mayNameProtected(%q) = %v, want %v", tt.field, got, tt.want)
		}
	}
}

// The shell guard converges by construction: across a corpus of every
// writer, wrapper and target spelling, nothing round 4 denied is allowed
// unless it is provably read-only, and nothing in the corpus, every
// command of which writes, is provably read-only. So the guard denies
// every command of the corpus round 4 denied; it denies the rest of the
// corpus too, spellings round 4 did not read included.
func TestShellGuardConverges(t *testing.T) {
	cfg := guardConfig(t)
	corpus := writerCorpus()
	round4 := 0
	for _, command := range corpus {
		readOnly := provablyReadOnly(command)
		if readOnly {
			t.Errorf("%q writes, but is provably read-only", command)
		}
		denied := Guard(cfg, ToolCall{Tool: "Bash", Command: command}).Deny
		if round4Denies(command) {
			round4++
			if !readOnly && !denied {
				t.Errorf("%q: allowed, but round 4 denies it", command)
			}
		}
		if !denied {
			t.Errorf("%q: allowed, but it writes a protected path", command)
		}
	}
	// A corpus round 4 mostly allowed would prove little of the first
	// property.
	if round4 < len(corpus)/2 {
		t.Errorf("round 4 denies %d of %d commands of the corpus; want most", round4, len(corpus))
	}
}

// writerCorpus is every writer with every target spelling, each target
// written plainly, through a variable and through a command
// substitution, inside every wrapper; and every command that writes in a
// directory it enters, with every target spelling as the directory.
func writerCorpus() []string {
	var corpus []string
	for _, writer := range corpusWriters() {
		for _, target := range corpusTargets() {
			corpus = append(corpus, wrapped(targetForms(writer, target))...)
		}
	}
	for _, enter := range corpusChdirs() {
		for _, target := range corpusTargets() {
			corpus = append(corpus, fmt.Sprintf(enter, target))
		}
	}
	return corpus
}

// wrapped is each of commands inside each of corpusWrappers.
func wrapped(commands []string) []string {
	var all []string
	for _, command := range commands {
		for _, wrap := range corpusWrappers() {
			all = append(all, wrap(command))
		}
	}
	return all
}

// corpusWriters are commands that write their operand, %s.
func corpusWriters() []string {
	return []string{
		"echo x > %s", "echo x >> %s", "tee %s < x", "sed -i s/a/b/ %s", "truncate -s0 %s", "dd if=x of=%s",
		"chmod 000 %s", "cp x %s", "mv x %s", "ln -s conf %s", "rm -rf %s", "install -d %s",
		"git rm -r %s", "git mv x %s", "git checkout main -- %s", "git restore --source=HEAD~3 %s",
	}
}

// corpusTargets are spellings of protected paths and of leading parts of
// them.
func corpusTargets() []string {
	return []string{
		".pi", ".claude", ".Pi", ".CLAUDE.", "'.pi'", `".claude"`, `\.pi`, ".p'i'", "sub/.pi/", "./.claude",
		"~/.claude", "$'.pi'", `$'\x2epi'`, `$'\056claude'`, ".claude/settings.json", "'.claude'/settings.json",
		`'.claude\settings.local.json'`, ".pi/extensions/x.ts", `".pi"/extensions`, ".millennium/config.json",
		"'.millennium'/config.json",
	}
}

// targetForms is writer with target written plainly, through a variable,
// and through a command substitution.
func targetForms(writer, target string) []string {
	return []string{
		fmt.Sprintf(writer, target),
		"D=" + target + "; " + fmt.Sprintf(writer, "$D"),
		fmt.Sprintf(writer, `"$(printf %s `+target+`)"`),
	}
}

// corpusWrappers run a command: as it is, in a shell's -c script, through
// eval, a pipe, a here-string or env -S, after a prefix, from find
// -exec, in a loop's body, a subshell or a command substitution, or in a
// directory a prefix enters.
func corpusWrappers() []func(string) string {
	quoted := func(format string) func(string) string {
		return func(c string) string { return fmt.Sprintf(format, shellQuote(c)) }
	}
	prefixed := func(format string) func(string) string {
		return func(c string) string { return fmt.Sprintf(format, c) }
	}
	return []func(string) string{
		prefixed("%s"), quoted("bash -c %s"), quoted("bash -c -- %s"), quoted("sh -c -e %s"), quoted("eval %s"),
		quoted("echo %s | sh"), quoted("sh <<< %s"), quoted("env -S %s"), prefixed("timeout 5 %s"),
		prefixed("nice -n 5 %s"), prefixed("sudo -u root %s"), prefixed(`find . -exec %s \;`),
		prefixed("for x in 1; do %s; done"), prefixed("(%s)"), prefixed(`echo "$(%s)"`), prefixed("env -C sub %s"),
		prefixed("sudo -D sub %s"),
	}
}

// corpusChdirs write in the directory, %s, they enter.
func corpusChdirs() []string {
	return []string{
		"cd %s && tee settings.json < x", "env -C %s tee settings.json < x", "env --chdir=%s tee settings.json < x",
		"sudo -D %s tee settings.json < x", "pushd %s; echo x > settings.json",
	}
}
