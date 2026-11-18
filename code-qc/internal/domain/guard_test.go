package domain

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
		{"main session edits the config", ToolCall{Tool: "Edit", Paths: []string{".millennium/config.json"}}, ".millennium is protected"},
		{"engineer writes a dependency approval", ToolCall{Tool: "Write", Role: RoleSoftwareEngineer, Paths: []string{".millennium/deps/npm/x.json"}}, ".millennium is protected"},
		{"anyone edits project settings", ToolCall{Tool: "MultiEdit", Paths: []string{".claude/settings.json"}}, ".claude/settings.json is protected"},
		{"anyone edits pi's project settings", ToolCall{Tool: "Write", Paths: []string{".pi/settings.json"}}, ".pi/settings.json is protected"},
		{"anyone adds a pi extension", ToolCall{Tool: "Write", Paths: []string{".pi/extensions/off.ts"}}, ".pi/extensions is protected"},
		{"engineer works in a pi worktree", ToolCall{Tool: "Edit", Role: RoleSoftwareEngineer, Paths: []string{".pi/worktrees/p1/main.go"}}, ""},
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
		{"the config, upper case", ToolCall{Tool: "Write", Paths: []string{".MILLENNIUM/config.json"}}, ".millennium is protected"},
		{"the config, mixed case", ToolCall{Tool: "Edit", Paths: []string{".Millennium/Config.json"}}, ".millennium is protected"},
		{"settings, mixed case", ToolCall{Tool: "Write", Paths: []string{".claude/Settings.json"}}, ".claude/settings.json is protected"},
		{"a long s that folds to s", ToolCall{Tool: "Write", Paths: []string{".claude/ſettings.json"}}, ".claude/settings.json is protected"},
		{"pi extensions, upper case", ToolCall{Tool: "Write", Paths: []string{".PI/EXTENSIONS/off.ts"}}, ".pi/extensions is protected"},
		{"an acceptance test, mixed case", ToolCall{Tool: "Edit", Role: RoleSoftwareEngineer, Paths: []string{"Tests/Acceptance/login_test.go"}}, "acceptance test"},
		{"a name that only starts like one", ToolCall{Tool: "Write", Paths: []string{".MILLENNIUMX/config.json"}}, ""},
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
		{"pi settings in a subdirectory", ToolCall{Tool: "NotebookEdit", Paths: []string{"vendor/x/.pi/settings.json"}}, ".pi/settings.json is protected"},
		{"a name that only starts like one", ToolCall{Tool: "Write", Targets: []string{"/repo/.millenniumx/config.json", "/repo/.claude/settings.json.bak"}}, ""},
		{"a sequence split by another segment", ToolCall{Tool: "Write", Targets: []string{"/repo/.claude/x/settings.json"}}, ""},
		{"a read names no rule path", ToolCall{Tool: "Read", Targets: []string{"/tmp/x/moved/.millennium/config.json"}}, ""},
		{"a shell command is not a path", ToolCall{Tool: "Bash", Command: "ls", Targets: []string{"/repo/.millennium/config.json"}}, ""},
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
	tests := []struct {
		tool, role string
		want       PathNeed
	}{
		{"Write", "", NeedTargets},
		{"Edit", RoleQATester, NeedTargets},
		{"NotebookEdit", RoleSoftwareEngineer, NeedWorktreePaths},
		{"MultiEdit", RoleSoftwareEngineer, NeedWorktreePaths},
		{"Read", RoleQATester, NeedWorktreePaths},
		{"Glob", RoleQATester, NeedWorktreePaths},
		{"Read", "", NeedNoPaths},
		{"Grep", RoleSoftwareEngineer, NeedNoPaths},
		{"Bash", RoleQATester, NeedNoPaths},
		{"WebFetch", "", NeedNoPaths},
	}
	for _, tt := range tests {
		if got := PathNeedOf(tt.tool, tt.role); got != tt.want {
			t.Errorf("PathNeedOf(%q, %q) = %q, want %q", tt.tool, tt.role, got, tt.want)
		}
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
