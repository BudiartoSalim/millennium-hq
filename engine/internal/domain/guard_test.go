package domain

import (
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

func TestGuard(t *testing.T) {
	cfg := guardConfig(t)
	tests := []struct {
		name string
		call ToolCall
		want string // "" means allowed
	}{
		{"main session edits code", ToolCall{Tool: "Edit", Paths: []string{"services/a/api/main.go"}}, ""},
		{"main session edits the config", ToolCall{Tool: "Edit", Paths: []string{".millennium/config.json"}}, ".millennium is protected"},
		{"engineer writes a dependency approval", ToolCall{Tool: "Write", Role: RoleEngineer, Paths: []string{".millennium/deps/npm/x.json"}}, ".millennium is protected"},
		{"anyone edits project settings", ToolCall{Tool: "MultiEdit", Paths: []string{".claude/settings.json"}}, ".claude/settings.json is protected"},
		{"engineer edits an acceptance test", ToolCall{Tool: "Edit", Role: RoleEngineer, Paths: []string{"tests/acceptance/login_test.go"}}, "acceptance test"},
		{"qa-tester writes an acceptance test", ToolCall{Tool: "Write", Role: RoleQATester, Paths: []string{"tests/acceptance/login_test.go"}}, ""},
		{"qa-tester reads code", ToolCall{Tool: "Read", Role: RoleQATester, Paths: []string{"services/a/api/internal/domain/user.go"}}, "implementation code"},
		{"qa-tester greps a service", ToolCall{Tool: "Grep", Role: RoleQATester, Paths: []string{"services/a/api"}}, "implementation code"},
		{"qa-tester reads docs", ToolCall{Tool: "Read", Role: RoleQATester, Paths: []string{"docs/spec.md"}}, ""},
		{"qa-tester reads acceptance tests", ToolCall{Tool: "Glob", Role: RoleQATester, Paths: []string{"tests/acceptance"}}, ""},
		{"engineer reads code", ToolCall{Tool: "Read", Role: RoleEngineer, Paths: []string{"services/a/api/main.go"}}, ""},
		{"shell writes the config", ToolCall{Tool: "Bash", Command: "echo '{}' > .millennium/config.json"}, "changes .millennium"},
		{"shell sed on settings", ToolCall{Tool: "Bash", Command: "sed -i 's/x/y/' .claude/settings.json"}, "changes .claude/settings.json"},
		{"shell reads the config", ToolCall{Tool: "Bash", Command: "cat .millennium/config.json"}, ""},
		{"agent applies a config", ToolCall{Tool: "Bash", Command: "millennium config apply"}, "only the user runs"},
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

func TestRoleOf(t *testing.T) {
	for agentType, want := range map[string]string{"millennium:engineer": "engineer", "qa-tester": "qa-tester", "": "", "a:b:qa-tester": "qa-tester"} {
		if got := RoleOf(agentType); got != want {
			t.Errorf("RoleOf(%q) = %q, want %q", agentType, got, want)
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
