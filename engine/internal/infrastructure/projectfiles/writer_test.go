package projectfiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestMergeSettingsKeepsOtherSettings(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"model":"opus","permissions":{"allow":["Bash(npm test)"],"deny":["Edit(/.millennium/**)"]},"sandbox":{"filesystem":{"denyWrite":["./secrets"]}},"cleanupPeriodDays":30}`
	if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	w := New(root)
	for range 2 {
		if err := w.MergeSettings("millennium@millennium-hq"); err != nil {
			t.Fatalf("MergeSettings: %v", err)
		}
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(read(t, filepath.Join(root, ".claude", "settings.json"))), &got); err != nil {
		t.Fatal(err)
	}
	sandbox := got["sandbox"].(map[string]any)
	denyWrite := sandbox["filesystem"].(map[string]any)["denyWrite"].([]any)
	deny := got["permissions"].(map[string]any)["deny"].([]any)
	if got["model"] != "opus" || got["cleanupPeriodDays"] != float64(30) || sandbox["enabled"] != true || sandbox["allowUnsandboxedCommands"] != false ||
		len(denyWrite) != 2 || len(deny) != 1 || got["enabledPlugins"].(map[string]any)["millennium@millennium-hq"] != true {
		t.Fatalf("settings = %v", got)
	}
}

func TestMergeSettingsRefusesABrokenFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte("[1,2]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := New(root).MergeSettings("p"); err == nil || !strings.Contains(err.Error(), "not a JSON object") {
		t.Fatalf("err = %v", err)
	}
	if read(t, filepath.Join(root, ".claude", "settings.json")) != "[1,2]" {
		t.Fatal("a file that cannot be merged must be left alone")
	}
}

func TestSetAgentsSection(t *testing.T) {
	root := t.TempDir()
	agents := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("# Project\n\nOur rules."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# Claude notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := New(root)
	if err := w.SetAgentsSection("## Gates\nfirst"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetAgentsSection("## Gates\nsecond"); err != nil {
		t.Fatal(err)
	}
	text := read(t, agents)
	if !strings.HasPrefix(text, "# Project\n\nOur rules.\n\n<!-- millennium:begin") || strings.Contains(text, "first") ||
		!strings.Contains(text, "second\n<!-- millennium:end -->\n") || strings.Count(text, "millennium:begin") != 1 {
		t.Fatalf("AGENTS.md = %q", text)
	}
	if err := w.SetAgentsSection("again"); err != nil {
		t.Fatal(err)
	}
	if claude := read(t, filepath.Join(root, "CLAUDE.md")); strings.Count(claude, "@AGENTS.md") != 1 {
		t.Fatalf("CLAUDE.md must import AGENTS.md exactly once: %q", claude)
	}
}

func TestSetAgentsSectionWithoutExistingFiles(t *testing.T) {
	root := t.TempDir()
	if err := New(root).SetAgentsSection("## Gates"); err != nil {
		t.Fatal(err)
	}
	if text := read(t, filepath.Join(root, "AGENTS.md")); !strings.HasPrefix(text, "<!-- millennium:begin") {
		t.Fatalf("AGENTS.md = %q", text)
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err == nil {
		t.Fatal("no CLAUDE.md may be created: AGENTS.md is read when there is none")
	}
}

func TestIgnoreAndWriteFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := New(root)
	for range 2 {
		if err := w.Ignore("millennium.proposal.json"); err != nil {
			t.Fatal(err)
		}
	}
	if got := read(t, filepath.Join(root, ".gitignore")); got != "node_modules\nmillennium.proposal.json\n" {
		t.Fatalf(".gitignore = %q", got)
	}
	target := filepath.Join(root, "deep", "dir", "config.json")
	if err := w.WriteFile(target, []byte("{}\n")); err != nil || read(t, target) != "{}\n" {
		t.Fatalf("WriteFile: %v", err)
	}
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteFile(filepath.Join(blocker, "x"), nil); err == nil {
		t.Fatal("writing under a file must fail")
	}
}
