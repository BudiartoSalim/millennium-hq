package domain

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"
)

func guardConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := NewConfig(ConfigInput{Version: ptr(1), Components: []ComponentInput{
		{Paths: []string{"services/*/api"}, Languages: []string{"go"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// The guard knows agents only by name, so a role that no longer matches its
// agent file would silently stop applying to that agent.
func TestGuardRolesNameThePluginsAgents(t *testing.T) {
	for _, role := range []string{RoleSoftwareEngineer, RoleQATester} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "plugin", "agents", role+".md"))
		if err != nil {
			t.Fatalf("role %q has no agent file: %v", role, err)
		}
		if !strings.Contains(string(data), "\nname: "+role+"\n") {
			t.Fatalf("plugin/agents/%s.md is not named %q", role, role)
		}
	}
}

func TestGuard(t *testing.T) {
	cfg := guardConfig(t)
	tests := []struct {
		name string
		call ToolCall
		want string // "" means allowed
	}{
		{"main session edits code", ToolCall{Tool: "Edit", Paths: []string{"services/a/api/main.go"}}, ""},
		{"main session edits the config", ToolCall{Tool: "Edit", Targets: []string{"/repo/.millennium/config.json"}}, ".millennium is protected"},
		{"engineer writes a dependency approval", ToolCall{Tool: "Write", Role: RoleSoftwareEngineer, Targets: []string{"/repo/.millennium/deps/npm/x.json"}}, ".millennium is protected"},
		{"anyone edits project settings", ToolCall{Tool: "MultiEdit", Targets: []string{"/repo/.claude/settings.json"}}, ".claude/settings.json is protected"},
		{"anyone edits pi's project settings", ToolCall{Tool: "Write", Targets: []string{"/repo/.pi/settings.json"}}, ".pi/settings.json is protected"},
		{"anyone adds a pi extension", ToolCall{Tool: "Write", Targets: []string{"/repo/.pi/extensions/off.ts"}}, ".pi/extensions is protected"},
		{"engineer works in a pi worktree", ToolCall{Tool: "Edit", Role: RoleSoftwareEngineer, Targets: []string{"/repo/.pi/worktrees/p1/main.go"}}, ""},
		{"engineer edits an acceptance test", ToolCall{Tool: "Edit", Role: RoleSoftwareEngineer, Paths: []string{"tests/acceptance/login_test.go"}}, "acceptance test"},
		{"qa-tester writes an acceptance test", ToolCall{Tool: "Write", Role: RoleQATester, Paths: []string{"tests/acceptance/login_test.go"}}, ""},
		{"qa-tester reads code", ToolCall{Tool: "Read", Role: RoleQATester, Paths: []string{"services/a/api/internal/domain/user.go"}}, "implementation code"},
		{"qa-tester greps a service", ToolCall{Tool: "Grep", Role: RoleQATester, Paths: []string{"services/a/api"}}, "implementation code"},
		{"qa-tester reads docs", ToolCall{Tool: "Read", Role: RoleQATester, Paths: []string{"docs/spec.md"}}, ""},
		{"qa-tester reads acceptance tests", ToolCall{Tool: "Glob", Role: RoleQATester, Paths: []string{"tests/acceptance"}}, ""},
		{"engineer reads code", ToolCall{Tool: "Read", Role: RoleSoftwareEngineer, Paths: []string{"services/a/api/main.go"}}, ""},
		{"shell writes the config", ToolCall{Tool: "Bash", Command: "echo '{}' > .millennium/config.json"}, "changes .millennium"},
		{"shell sed on settings", ToolCall{Tool: "Bash", Command: "sed -i 's/x/y/' .claude/settings.json"}, "changes .claude/settings.json"},
		{"shell copies in a pi extension", ToolCall{Tool: "Bash", Command: "cp /tmp/x.ts .pi/extensions/"}, "changes .pi/extensions"},
		{"shell reads the config", ToolCall{Tool: "Bash", Command: "cat .millennium/config.json"}, ""},
		{"agent applies a config", ToolCall{Tool: "Bash", Command: "millennium-code-qc config apply"}, "only the user runs"},
		{"ordinary shell", ToolCall{Tool: "Bash", Command: "go test ./..."}, ""},
		{"other tools", ToolCall{Tool: "WebFetch"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Guard(cfg, tt.call)
			if tt.want == "" && got.Deny {
				t.Fatalf("denied: %s", got.Reason)
			}
			if tt.want != "" && (!got.Deny || !strings.Contains(got.Reason, tt.want)) {
				t.Fatalf("decision %+v, want a denial mentioning %q", got, tt.want)
			}
		})
	}
}

// On a case-insensitive file system a case variant of a protected path is
// that path, so the guard folds case on every OS, for file tools and shell
// commands alike.
func TestGuardIgnoresCaseInProtectedPaths(t *testing.T) {
	cfg := guardConfig(t)
	tests := []struct {
		name string
		call ToolCall
		want string // "" means allowed
	}{
		{"the config, upper case", ToolCall{Tool: "Write", Targets: []string{"/repo/.MILLENNIUM/config.json"}}, ".millennium is protected"},
		{"the config, mixed case", ToolCall{Tool: "Edit", Targets: []string{"/repo/.Millennium/Config.json"}}, ".millennium is protected"},
		{"settings, mixed case", ToolCall{Tool: "Write", Targets: []string{"/repo/.claude/Settings.json"}}, ".claude/settings.json is protected"},
		{"a long s that folds to s", ToolCall{Tool: "Write", Targets: []string{"/repo/.claude/ſettings.json"}}, ".claude/settings.json is protected"},
		{"pi extensions, upper case", ToolCall{Tool: "Write", Targets: []string{"/repo/.PI/EXTENSIONS/off.ts"}}, ".pi/extensions is protected"},
		{"an acceptance test, mixed case", ToolCall{Tool: "Edit", Role: RoleSoftwareEngineer, Paths: []string{"Tests/Acceptance/login_test.go"}}, "acceptance test"},
		{"a name that only starts like one", ToolCall{Tool: "Write", Targets: []string{"/repo/.MILLENNIUMX/config.json"}}, ""},
		{"shell writes the config, upper case", ToolCall{Tool: "Bash", Command: "echo '{}' > .MILLENNIUM/config.json"}, "changes .millennium"},
		{"shell sed on settings, mixed case", ToolCall{Tool: "Bash", Command: "sed -i 's/x/y/' .Claude/Settings.JSON"}, "changes .claude/settings.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertGuard(t, cfg, tt.call, tt.want)
		})
	}
}

// A protected path is protected wherever it lies: matched as whole
// segments anywhere in a path the call names, as named or resolved, in any
// checkout or directory, so a worktree git no longer lists keeps its rules.
func TestGuardMatchesProtectedPathsAnywhere(t *testing.T) {
	cfg := guardConfig(t)
	tests := []struct {
		name string
		call ToolCall
		want string // "" means allowed
	}{
		{"a moved worktree's config", ToolCall{Tool: "Write", Targets: []string{"/tmp/x/moved/.millennium/config.json"}}, ".millennium is protected"},
		{"a worktree named with a newline", ToolCall{Tool: "Edit", Targets: []string{"/repo/.claude/worktrees/a\nb/.millennium/config.json"}}, ".millennium is protected"},
		{"resolved into another checkout", ToolCall{Tool: "Write", Targets: []string{"/repo/src/link.json", "/elsewhere/.Millennium/deps/x.json"}}, ".millennium is protected"},
		{"another directory's settings, upper case", ToolCall{Tool: "Edit", Targets: []string{"/home/u/.CLAUDE/settings.local.json"}}, ".claude/settings.local.json is protected"},
		{"a pi extension anywhere", ToolCall{Tool: "MultiEdit", Targets: []string{"C:/work/app/.pi/extensions/sub/off.ts"}}, ".pi/extensions is protected"},
		{"pi settings in a subdirectory", ToolCall{Tool: "NotebookEdit", Targets: []string{"/repo/vendor/x/.pi/settings.json"}}, ".pi/settings.json is protected"},
		{"a name that only starts like one", ToolCall{Tool: "Write", Targets: []string{"/repo/.millenniumx/config.json", "/repo/.claude/settings.json.bak"}}, ""},
		{"a sequence split by another segment", ToolCall{Tool: "Write", Targets: []string{"/repo/.claude/x/settings.json"}}, ""},
		{"a read names no rule path", ToolCall{Tool: "Read", Targets: []string{"/tmp/x/moved/.millennium/config.json"}}, ""},
		{"a shell command is not a path", ToolCall{Tool: "Bash", Command: "ls", Targets: []string{"/repo/.millennium/config.json"}}, ""},
		{"a relative path from the repository", ToolCall{Tool: "Write", Targets: []string{".millennium/config.json"}}, ".millennium is protected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertGuard(t, cfg, tt.call, tt.want)
		})
	}
}

// A path that ends with a leading part of a protected path stands where
// the directory holding protected paths lies; in git it can only be a file
// or a symlink there.
func TestProtectedLeadIn(t *testing.T) {
	tests := []struct{ p, want string }{
		{".pi", ".pi"},
		{".claude", ".claude"},
		{"sub/.Pi", ".pi"},
		{"pkg/.CLAUDE.", ".claude"},
		{"C:/repo/.pi::$DATA", ".pi"},
		{".millennium", ""},
		{".pi/extensions", ""},
		{".claude/worktrees", ""},
		{"sub/x.pi", ""},
		{".pid", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got, ok := ProtectedLeadIn(tt.p)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("ProtectedLeadIn(%q) = %q, %v; want %q", tt.p, got, ok, tt.want)
		}
	}
}

// A shell command that puts a link, a file or a directory where protected
// paths lie (`.pi`, `.claude`), or removes what is there, changes them:
// such a word in a command that can write. A command that provably only
// reads does not, even with a write-like word among its arguments
// (`grep -rn rm .claude`) or a redirection outside protected paths; nor
// does a longer name.
func TestGuardShellCommandsThatReplaceWhatHoldsProtectedPaths(t *testing.T) {
	cfg := guardConfig(t)
	tests := []struct{ command, want string }{
		{"ln -s conf .pi", "changes .pi"},
		{"ln -sfn ../conf sub/.Pi/", "changes .pi"},
		{"mv conf .claude", "changes .claude"},
		{"git mv conf '.pi'", "changes .pi"},
		{"git -C sub mv conf .pi", "changes .pi"},
		{"cp -r conf --target-directory=.pi.", "changes .pi"},
		{"cp -r x .pi", "changes .pi"},
		{"install -d .claude", "changes .claude"},
		{"rm -rf .claude && echo done", "changes .claude"},
		{"rm -rf .claude 2>/dev/null", "changes .claude"},
		{"ln -s conf .p'i'", "changes .pi"},
		{`rm -rf "sub/.claude"`, "changes .claude"},
		{`ln -s conf \.pi`, "changes .pi"},
		{"/bin/mv conf .claude", "changes .claude"},
		{"sudo rm -rf ~/.claude", "changes .claude"},
		{"X=1 env -i ln -s conf .pi", "changes .pi"},
		{"echo ok; ln -s conf .pi", "changes .pi"},
		{"(cd sub && ln -s ../conf .pi)", "changes .pi"},
		{"echo $(mv conf .claude)", "changes .claude"},
		{"echo `mv conf .claude`", "changes .claude"},
		{"find . | xargs -0 rm -rf .claude", "changes .claude"},
		{"if true; then mv conf .pi; fi", "changes .pi"},
		{"bash -c 'ln -s conf .pi'", "changes .pi"},
		{`sh -ec "cd sub && mv conf .claude"`, "changes .claude"},
		// A shell is never provably read-only (round 4 denied it too).
		{"bash -c 'ls .claude 2>/dev/null'", "changes .claude"},
		{"bash script.sh .claude", ""},
		{"ls .pi", ""},
		{"ls -la .claude 2>/dev/null", ""},
		{"find . -path ./.claude -prune -o -name '*.go' -print 2>/dev/null", ""},
		{"grep -rn foo --exclude-dir=.claude . > /tmp/out.txt", ""},
		{"du -sh ~/.claude 2>&1", ""},
		{"cat .pi 2>&1 | head", ""},
		// grep and echo only read, whatever their arguments.
		{"grep -rn rm .claude", ""},
		{"echo mv conf .claude", ""},
		{"git log -- .claude", ""},
		// -c can make status an alias, or set a program git runs (R7-SEC-3).
		{"git -c core.x=y status .pi", "changes .pi"},
		// The redirection writes a file named .pi (round 4 denied it too).
		{"ln -s a b > .pi", "changes .pi"},
		{"cp a.pi b.pi", ""},
		{"mv notes.md .pid", ""},
		{"rm -rf .claude/worktrees/agent", ""},
		{"ls 'unclosed .claude", ""},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			assertGuard(t, cfg, ToolCall{Tool: "Bash", Command: tt.command}, tt.want)
		})
	}
}

// A protected path spelled with quotes or a backslash inside it is still
// that path to the shell, so a command that can write and names it is
// denied, a redirection's target included (R6-CR-1).
func TestGuardShellDeniesProtectedPathsSpelledWithQuotes(t *testing.T) {
	cfg := guardConfig(t)
	tests := []struct{ command, want string }{
		{`cp evil.json '.claude'/settings.json`, "changes .claude/settings.json"},
		{`cp evil.json ".claude"/settings.json`, "changes .claude/settings.json"},
		{`cp x.ts '.pi'/extensions/x.ts`, "changes .pi/extensions"},
		{`echo '{}' > '.claude'/settings.json`, "changes .claude/settings.json"},
		{`echo '{}' >'.Claude'/Settings.json.`, "changes .claude/settings.json"},
		{`sed -i s/a/b/ ".claude"/settings.json`, "changes .claude/settings.json"},
		{`tee ".pi"/extensions/x.ts < x`, "changes .pi/extensions"},
		{`rm -f .claude/"settings.json"`, "changes .claude/settings.json"},
		{`cp x '.claude\settings.local.json'`, "changes .claude/settings.local.json"},
		{`cp x.ts --target-directory='.pi'/extensions`, "changes .pi/extensions"},
		{`chmod 600 '.millennium'/config.json`, "changes .millennium"},
		{`truncate -s0 ./sub/../.pi/"settings.json"`, "changes .pi/settings.json"},
		{`m\v x '.claude'/settings.json`, "changes .claude/settings.json"},
		{`bash -c "cp x '.claude'/settings.json"`, "changes .claude/settings.json"},
		{`cat '.claude'/settings.json`, ""},
		{`cat '.pi'/extensions/x.ts`, ""},
		// cat only reads, and the redirection writes outside protected paths.
		{`cat '.pi'/extensions/x.ts > /tmp/x.ts`, ""},
		// The floor reads `.claude` between the quotes, as round 4 did.
		{`cp '.claude'/settings.json.bak /tmp`, "changes .claude"},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			assertGuard(t, cfg, ToolCall{Tool: "Bash", Command: tt.command}, tt.want)
		})
	}
}

// Commands the round-4 shell guard denied because they put, move, remove
// or restore something at `.pi` or `.claude`, with the replacing command
// after a prefix and its option values, run by find or xargs, in an eval
// string, or as a git command that restores files (R6-CR-2, R6-SEC-5).
func TestGuardShellStillDeniesReplacingWhatHoldsProtectedPaths(t *testing.T) {
	cfg := guardConfig(t)
	tests := []struct{ command, want string }{
		{"timeout 5 ln -s conf .pi", "changes .pi"},
		{"nice -n 5 ln -s conf .pi", "changes .pi"},
		{"nice -n 5 mv conf .pi", "changes .pi"},
		{"sudo -u root ln -s conf .pi", "changes .pi"},
		{"env -u X ln -s conf .pi", "changes .pi"},
		{`find . -maxdepth 0 -exec ln -s conf .pi \;`, "changes .pi"},
		{`find . -exec ln -s conf .pi \;`, "changes .pi"},
		{`find . -name conf -exec mv {} .pi \;`, "changes .pi"},
		{"for d in .pi; do ln -s conf $d; done", "changes .pi"},
		{"eval 'ln -s conf .pi'", "changes .pi"},
		{`eval "sudo -u root sh -c 'mv conf .claude'"`, "changes .claude"},
		{"mv conf $'.pi'", "changes .pi"},
		{`mv conf $'\x2epi'`, "changes .pi"},
		{"xargs -I {} mv {} .pi", "changes .pi"},
		{"git rm -r .claude", "changes .claude"},
		{"git checkout main -- .claude", "changes .claude"},
		{"git checkout other -- .pi", "changes .pi"},
		{"git restore --source=HEAD~3 .claude", "changes .claude"},
		{"git restore --source=HEAD~3 .pi", "changes .pi"},
		{"git -C sub --git-dir x/.git rm -r --cached .claude", "changes .claude"},
		// No write-like token matches `git -C sub checkout`.
		{"git -C sub checkout main -- '.claude'/settings.json", "changes .claude/settings.json"},
		{"git checkout -b feature", ""},
		{"git switch main", ""},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			assertGuard(t, cfg, ToolCall{Tool: "Bash", Command: tt.command}, tt.want)
		})
	}
}

// Where the matcher cannot place what a replacing command changes, the
// floor reads the whole command: a command that can write and a word, or
// a part of one, that is a leading part of a protected path is denied,
// unless the command is provably read-only.
func TestGuardShellReadsWhatItCannotPlaceCoarsely(t *testing.T) {
	cfg := guardConfig(t)
	tests := []struct{ command, want string }{
		{"for d in .pi; do ln -s conf \"$d\"; done", "changes .pi"},
		{`for d in \.pi; do rm -rf $d; done`, "changes .pi"},
		{"while read d; do mv conf $d; done <<< .claude", "changes .claude"},
		{"echo .pi | xargs ln -s conf", "changes .pi"},
		{`mv conf "$(echo .pi)"`, "changes .pi"},
		{"mv conf \"`echo .claude`\"", "changes .claude"},
		{`cp x "$(echo .claude/settings.json)"`, "changes .claude/settings.json"},
		{"sh -c 'ln -s conf \"$1\"' _ .pi", "changes .pi"},
		{"RM=rm; $RM -rf .claude", "changes .claude"},
		{"cd .claude && cp /tmp/evil settings.json", "changes .claude"},
		{"pushd sub/.pi; ln -s ../conf extensions", "changes .pi"},
		{"cd .claude && ls", ""},
		{"cd .claude/worktrees/x && go test ./... > /tmp/out", ""},
		{"for f in *.go; do rm -f $f.bak; done", ""},
		{"find . -name '*.tmp' | xargs rm", ""},
		// An expansion is never provably read-only (round 4 denied it too).
		{"ls $HOME/.claude 2>/dev/null", "changes .claude"},
		{"rm -rf \"$TMPDIR/x\"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			assertGuard(t, cfg, ToolCall{Tool: "Bash", Command: tt.command}, tt.want)
		})
	}
}

// round4Write is round 4's shellWrite (7f65e61), kept as it was: the
// production pattern has since grown.
var round4Write = regexp.MustCompile(`>|\btee\b|\bsed\s+(-\S*\s+)*-i|\b(mv|cp|rm|ln|chmod|truncate|install|dd)\b|\bgit\s+(checkout|restore|rm|mv|apply)\b`)

// round4Denies is the shell guard of round 4 (7f65e61), the reference the
// current one must not fall behind: a command that can write and names a
// protected path anywhere, or a word that ends with a leading part of one.
func round4Denies(command string) bool {
	if !round4Write.MatchString(command) {
		return false
	}
	for _, protected := range ProtectedPaths() {
		if ContainsFold(command, protected) {
			return true
		}
	}
	for _, word := range strings.FieldsFunc(command, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("'\"`;&|()<>=", r) }) {
		if _, ok := ProtectedLeadIn(path.Clean(word)); ok {
			return true
		}
	}
	return false
}

// Round 5 narrowed the shell guard so that read-only commands pass
// (R5-CR-3), and in doing so let through commands that round 4 denied
// (R6-CR-1, R6-CR-2). Every command round 4 denied in its tests and in the
// reviewers' lists is still denied, every read-only command of R5-CR-3 and
// of the reviewers' lists passes, and across a sweep of replacing
// commands, prefixes, spellings and wrappers nothing round 4 denies is
// allowed (TestShellGuardConverges sweeps wider).
func TestGuardShellDeniesWhatRound4DeniedAndPassesReadOnlyCommands(t *testing.T) {
	cfg := guardConfig(t)
	denied := []string{
		// Round 4's tests.
		"echo '{}' > .millennium/config.json", "sed -i 's/x/y/' .claude/settings.json", "cp /tmp/x.ts .pi/extensions/",
		"echo '{}' > .MILLENNIUM/config.json", "sed -i 's/x/y/' .Claude/Settings.JSON",
		"ln -s conf .pi", "ln -sfn ../conf sub/.Pi/", "mv conf .claude", "git mv conf '.pi'",
		"cp -r conf --target-directory=.pi.", "rm -rf .claude && echo done",
		// The reviewers' lists.
		`cp evil.json '.claude'/settings.json`, `echo '{}' > '.claude'/settings.json`, `sed -i s/a/b/ ".claude"/settings.json`,
		`tee ".pi"/extensions/x.ts < x`, `cp x.ts '.pi'/extensions/x.ts`, `rm -f .claude/"settings.json"`,
		"timeout 5 ln -s conf .pi", "nice -n 5 ln -s conf .pi", "nice -n 5 mv conf .pi", "sudo -u root ln -s conf .pi",
		"env -u X ln -s conf .pi", `find . -exec ln -s conf .pi \;`, `find . -name conf -exec mv {} .pi \;`,
		"for d in .pi; do ln -s conf $d; done", "eval 'ln -s conf .pi'", "mv conf $'.pi'", "xargs -I {} mv {} .pi",
		"git rm -r .claude", "git checkout main -- .claude", "git checkout other -- .pi", "git restore --source=HEAD~3 .claude",
		"git restore --source=HEAD~3 .pi",
	}
	for _, command := range denied {
		if !round4Denies(command) {
			t.Errorf("round 4 allows %q; it does not belong in this list", command)
		}
		if got := Guard(cfg, ToolCall{Tool: "Bash", Command: command}); !got.Deny {
			t.Errorf("%q: allowed, but round 4 denied it", command)
		}
	}
	readOnly := []string{
		"ls -la .claude 2>/dev/null", "find . -path ./.claude -prune -o -name '*.go' -print 2>/dev/null",
		"grep -rn foo --exclude-dir=.claude . > /tmp/out.txt", "du -sh ~/.claude 2>&1", "cat .pi/settings.json",
		"git status -- .claude", "git log -- .pi", "git diff -- .claude",
		"ls .claude | grep x > /tmp/x", "git diff main -- .claude > /tmp/x", "stat .claude >/dev/null 2>&1",
		"git -C .claude log > /tmp/x",
	}
	for _, command := range readOnly {
		if got := Guard(cfg, ToolCall{Tool: "Bash", Command: command}); got.Deny {
			t.Errorf("%q: %s; a read-only command must pass", command, got.Reason)
		}
	}
	for _, command := range replacingSweep() {
		if round4Denies(command) && !Guard(cfg, ToolCall{Tool: "Bash", Command: command}).Deny {
			t.Errorf("%q: allowed, but round 4 denies it", command)
		}
	}
}

// replacingSweep is every replacing command, with each prefix, target
// spelling and wrapper.
func replacingSweep() []string {
	prefixes := []string{"", "timeout 5 ", "nice -n 5 ", "sudo -u root ", "env -u X ", "X=1 ", "command ", "nohup ", "xargs -I {} ", "time -p ", "/usr/bin/"}
	commands := []string{"ln -s conf", "mv conf", "cp -r conf", "rm -rf", "install -d", "git rm -r", "git checkout main --", "git restore --source=HEAD~3", "git mv conf"}
	targets := []string{".pi", ".claude", "'.pi'", `".claude"`, `\.pi`, "sub/.Pi/", "$'.pi'", "~/.claude", "--target-directory=.pi", ".claude/settings.json", "'.pi'/extensions"}
	wrappers := []func(string) string{
		func(c string) string { return c },
		func(c string) string { return "bash -c " + shellQuote(c) },
		func(c string) string { return "eval " + shellQuote(c) },
		func(c string) string { return "(" + c + ")" },
		func(c string) string { return "echo ok; " + c + " 2>/dev/null" },
		func(c string) string { return "find . -exec " + c + ` \;` },
		func(c string) string { return "for x in 1; do " + c + "; done" },
	}
	var sweep []string
	for _, command := range joinEach(joinEach(prefixes, commands, ""), targets, " ") {
		for _, wrap := range wrappers {
			sweep = append(sweep, wrap(command))
		}
	}
	return sweep
}

// joinEach is each of heads joined with each of tails by sep.
func joinEach(heads, tails []string, sep string) []string {
	var joined []string
	for _, head := range heads {
		for _, tail := range tails {
			joined = append(joined, head+sep+tail)
		}
	}
	return joined
}

// Win32 strips a name's trailing dots and spaces and opens `name:stream`
// as a stream of name, so on Windows these spellings create or change the
// protected files themselves. The guard reads every segment that way on
// every OS.
func TestGuardReadsProtectedNamesAsWin32Does(t *testing.T) {
	cfg := guardConfig(t)
	tests := []struct {
		name string
		call ToolCall
		want string // "" means allowed
	}{
		{"local settings with a trailing dot", ToolCall{Tool: "Write", Targets: []string{`C:/repo/.claude/settings.local.json.`}}, ".claude/settings.local.json is protected"},
		{"settings with a trailing space", ToolCall{Tool: "Write", Targets: []string{`C:/repo/.claude/settings.json `}}, ".claude/settings.json is protected"},
		{"extensions with a trailing dot", ToolCall{Tool: "Write", Targets: []string{`C:/repo/.pi/extensions./x.ts`}}, ".pi/extensions is protected"},
		{"the data stream of settings", ToolCall{Tool: "Edit", Targets: []string{`C:/repo/.claude/settings.json::$DATA`}}, ".claude/settings.json is protected"},
		{"a named stream of pi settings", ToolCall{Tool: "Write", Targets: []string{`C:/repo/.pi/settings.json:x`}}, ".pi/settings.json is protected"},
		{"the config directory with dots and spaces", ToolCall{Tool: "Write", Targets: []string{`C:/repo/.Millennium. ./config.json`}}, ".millennium is protected"},
		{"a directory stream of .pi", ToolCall{Tool: "Write", Targets: []string{`C:/repo/.pi::$INDEX_ALLOCATION/extensions/x.ts`}}, ".pi/extensions is protected"},
		{"another name that only starts like one", ToolCall{Tool: "Write", Targets: []string{`C:/repo/.claude/settings.json.bak`, `C:/repo/.millennium-old/x`}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertGuard(t, cfg, tt.call, tt.want)
		})
	}
}

// The role rules read the paths in every worktree of the repository, with
// the acting session's config: the software engineer writes no acceptance
// test of any checkout, and qa-tester reads no component code through
// another checkout's path.
func TestGuardAppliesRoleRulesInEveryWorktree(t *testing.T) {
	cfg := guardConfig(t)
	tests := []struct {
		name string
		call ToolCall
		want string // "" means allowed
	}{
		{"another worktree's acceptance tests", ToolCall{Tool: "Edit", Role: RoleSoftwareEngineer, Paths: []string{".claude/worktrees/a/tests/acceptance/a_test.go", "tests/acceptance/a_test.go"}}, "acceptance test"},
		{"code in another worktree", ToolCall{Tool: "Edit", Role: RoleSoftwareEngineer, Paths: []string{"services/a/api/main.go"}}, ""},
		{"qa-tester reads code there", ToolCall{Tool: "Read", Role: RoleQATester, Paths: []string{"docs/spec.md", "services/a/api/main.go"}}, "services/a/api/main.go is implementation code"},
		{"qa-tester searches a directory holding a worktree", ToolCall{Tool: "Grep", Role: RoleQATester, Paths: []string{".claude", ""}}, "the repository root is implementation code"},
		{"qa-tester reads acceptance tests there", ToolCall{Tool: "Read", Role: RoleQATester, Paths: []string{"tests/acceptance/login_test.go"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertGuard(t, cfg, tt.call, tt.want)
		})
	}
}

// On a case-insensitive file system qa-tester could otherwise read
// component code under another spelling. The acceptance tests, an
// exception that allows, still match only as spelled.
func TestQATesterReadRuleIgnoresCase(t *testing.T) {
	cfg := guardConfig(t)
	tests := []struct {
		name string
		call ToolCall
		want string // "" means allowed
	}{
		{"code, upper case", ToolCall{Tool: "Read", Role: RoleQATester, Paths: []string{"SERVICES/A/API/main.go"}}, "implementation code"},
		{"code, mixed case", ToolCall{Tool: "Read", Role: RoleQATester, Paths: []string{"Services/b/Api/internal/x.go"}}, "implementation code"},
		{"a search above a component, mixed case", ToolCall{Tool: "Glob", Role: RoleQATester, Paths: []string{"Services/A"}}, "implementation code"},
		{"a document, upper case", ToolCall{Tool: "Read", Role: RoleQATester, Paths: []string{"SERVICES/A/API/README.MD"}}, ""},
		{"elsewhere, upper case", ToolCall{Tool: "Grep", Role: RoleQATester, Paths: []string{"DOCS"}}, ""},
		{"acceptance tests in another case inside a component", ToolCall{Tool: "Read", Role: RoleQATester, Paths: []string{"Services/A/API/tests/acceptance/x_test.go"}}, "implementation code"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertGuard(t, cfg, tt.call, tt.want)
		})
	}
	single, err := NewConfig(minimalInput())
	if err != nil {
		t.Fatal(err)
	}
	assertGuard(t, single, ToolCall{Tool: "Read", Role: RoleQATester, Paths: []string{"Tests/Acceptance/login_test.go"}}, "implementation code")
}

func TestPathNeedOf(t *testing.T) {
	targets, paths, both := PathNeed{Targets: true}, PathNeed{Paths: true}, PathNeed{Targets: true, Paths: true}
	tests := []struct {
		tool, role string
		want       PathNeed
	}{
		{"Write", "", targets},
		{"Edit", RoleQATester, targets},
		{"NotebookEdit", RoleSoftwareEngineer, both},
		{"MultiEdit", RoleSoftwareEngineer, both},
		{"Read", RoleQATester, paths},
		{"Glob", RoleQATester, paths},
		{"Read", "", PathNeed{}},
		{"Grep", RoleSoftwareEngineer, PathNeed{}},
		{"Bash", RoleQATester, PathNeed{}},
		{"WebFetch", "", PathNeed{}},
	}
	for _, tt := range tests {
		got := PathNeedOf(tt.tool, tt.role)
		if got != tt.want || got.Any() != (tt.want != PathNeed{}) {
			t.Errorf("PathNeedOf(%q, %q) = %+v (any %v), want %+v", tt.tool, tt.role, got, got.Any(), tt.want)
		}
	}
}

// What the hook locates (PathNeedOf) must cover everything the guard
// reads: a rule that reads the Paths or the Targets of a call that
// PathNeedOf says needs none would get none from the hook and allow
// silently. So for every tool and role, a path a rule would deny must not
// change the decision through a field PathNeedOf leaves out, and must
// change it through a field it names, which shows the probes reach the
// rules. The Paths are probed with what only a role rule denies: the
// protected paths in them are also in the Targets they are placed from.
func TestPathNeedOfCoversWhatGuardReads(t *testing.T) {
	cfg := guardConfig(t)
	tools := slices.Concat(writeTools(), readTools(), []string{"Bash", "WebFetch", "Task", ""})
	roles := []string{"", RoleSoftwareEngineer, RoleQATester, "tech-lead"}
	roleProbes := []string{"tests/acceptance/a_test.go", "services/a/api/main.go", ""}
	targetProbes := append([]string{"/repo/.millennium/config.json", "/repo/.pi/extensions/x.ts"}, roleProbes...)
	for _, tool := range tools {
		for _, role := range roles {
			need := PathNeedOf(tool, role)
			base := ToolCall{Tool: tool, Role: role, Command: "ls"}
			assertProbes(t, cfg, base, need.Paths, func(c *ToolCall, p string) { c.Paths = []string{p} }, roleProbes)
			assertProbes(t, cfg, base, need.Targets, func(c *ToolCall, p string) { c.Targets = []string{p} }, targetProbes)
		}
	}
}

// assertProbes sets each probe in a field of base with set: when the field
// is not needed no probe may change the decision; when it is, some probe
// must deny.
func assertProbes(t *testing.T, cfg Config, base ToolCall, needed bool, set func(*ToolCall, string), probes []string) {
	t.Helper()
	if Guard(cfg, base).Deny {
		t.Fatalf("%s by %q: denied with no paths", base.Tool, base.Role)
	}
	denied := 0
	for _, p := range probes {
		call := base
		set(&call, p)
		if Guard(cfg, call).Deny {
			denied++
		}
	}
	if needed != (denied > 0) {
		t.Errorf("%s by %q: %d of %d probes denied, but PathNeedOf says the field is needed: %v", base.Tool, base.Role, denied, len(probes), needed)
	}
}

// A path is placed in the worktree it lies in, the innermost one, since
// Claude Code's worktrees lie inside the main checkout; a directory that
// holds a worktree reaches it whole.
func TestWorktreePaths(t *testing.T) {
	roots := []string{"/repo", "/repo/.claude/worktrees/a", "/data/a", "/repo/.claude/worktrees/b"}
	tests := []struct {
		name    string
		targets []string
		want    []string
	}{
		{"a file in the main checkout", []string{"/repo/src/a.go"}, []string{"src/a.go"}},
		{"a file in a nested worktree", []string{"/repo/.claude/worktrees/a/tests/acceptance/t.go"}, []string{"tests/acceptance/t.go"}},
		{"named and resolved", []string{"/link/x.go", "/data/a/src/x.go"}, []string{"src/x.go"}},
		{"another case", []string{"/REPO/.Claude/Worktrees/B/Src"}, []string{"Src"}},
		{"a worktree root", []string{"/repo/.claude/worktrees/b"}, []string{""}},
		{"a directory holding worktrees", []string{"/repo/.claude"}, []string{".claude", ""}},
		{"a directory above every root", []string{"/"}, []string{""}},
		{"outside every root", []string{"/elsewhere/x.go", "/repository/x.go"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WorktreePaths(tt.targets, roots); !slices.Equal(got, tt.want) {
				t.Fatalf("WorktreePaths(%q) = %q, want %q", tt.targets, got, tt.want)
			}
		})
	}
	if got := WorktreePaths([]string{"/repo/x"}, nil); got != nil {
		t.Fatalf("no roots: %q", got)
	}
}

// assertGuard checks a decision: allowed when want is "", otherwise denied
// with a reason containing want.
func assertGuard(t *testing.T, cfg Config, call ToolCall, want string) {
	t.Helper()
	got := Guard(cfg, call)
	if want == "" && got.Deny {
		t.Fatalf("denied: %s", got.Reason)
	}
	if want != "" && (!got.Deny || !strings.Contains(got.Reason, want)) {
		t.Fatalf("decision %+v, want a denial mentioning %q", got, want)
	}
}

func TestRoleOf(t *testing.T) {
	for agentType, want := range map[string]string{"millennium:software-engineer": "software-engineer", "qa-tester": "qa-tester", "": "", "a:b:qa-tester": "qa-tester"} {
		if got := RoleOf(agentType); got != want {
			t.Errorf("RoleOf(%q) = %q, want %q", agentType, got, want)
		}
	}
}

// The launcher's path reaches agents quoted, in the session context, so a
// quoted command must be denied like a bare one.
func TestAgentsCannotApplyAConfigHoweverItIsSpelled(t *testing.T) {
	cfg := guardConfig(t)
	for command, wantDeny := range map[string]bool{
		"millennium-code-qc config apply":                                        true,
		"millennium-code-qc config apply --confirm abc123":                       true,
		"'/home/u/.claude/plugins/m/scripts/millennium-code-qc' config apply":    true,
		`"/home/u/.claude/plugins/m/scripts/millennium-code-qc" config apply`:    true,
		"./code-qc/millennium-code-qc.exe  config   apply":                       true,
		"millennium-code-qc config explain main.go":                              false,
		"'/home/u/.claude/plugins/m/scripts/millennium-code-qc' config validate": false,
		"git config apply.whitespace fix":                                        false,
	} {
		if got := Guard(cfg, ToolCall{Tool: "Bash", Command: command}); got.Deny != wantDeny {
			t.Errorf("%q: deny %v, want %v", command, got.Deny, wantDeny)
		}
	}
}

func TestQATesterAtTheRepositoryRootOfASingleProject(t *testing.T) {
	cfg, err := NewConfig(minimalInput())
	if err != nil {
		t.Fatal(err)
	}
	got := Guard(cfg, ToolCall{Tool: "Grep", Role: RoleQATester, Paths: []string{""}})
	if !got.Deny || !strings.Contains(got.Reason, "the repository root is implementation code") {
		t.Fatalf("decision %+v", got)
	}
	// The whole repository is the component, so the spec and the run
	// instructions must stay readable while the code does not.
	for p, wantDeny := range map[string]bool{
		"docs/specs/cart.md": false, "README": false, "api/openapi.yaml": false, "docs/flow.PNG": false,
		"main.go": true, "src/cart.ts": true, "Makefile": true, "db/001.sql": true, "web/index.html": true,
	} {
		if got := Guard(cfg, ToolCall{Tool: "Read", Role: RoleQATester, Paths: []string{p}}); got.Deny != wantDeny {
			t.Errorf("Read %q: deny %v, want %v", p, got.Deny, wantDeny)
		}
	}
}

func TestQATesterCannotSearchAroundComponentsInAMonorepo(t *testing.T) {
	cfg := guardConfig(t)
	for p, wantDeny := range map[string]bool{"": true, "services": true, "services/a": true, "services/a/api/src": true, "docs": false, "services/a/web": false} {
		got := Guard(cfg, ToolCall{Tool: "Grep", Role: RoleQATester, Paths: []string{p}})
		if got.Deny != wantDeny {
			t.Errorf("Grep in %q: deny %v, want %v", p, got.Deny, wantDeny)
		}
	}
}
