// Package projectfiles writes the files `millennium config apply` manages:
// the config, the project's Claude settings, the AGENTS.md section and a
// .gitignore entry. Every write replaces the file atomically, and existing
// content outside millennium's entries is kept.
package projectfiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Markers around the AGENTS.md section the writer owns.
const (
	sectionBegin = "<!-- millennium:begin (managed by `millennium config apply`; change the config, not this block) -->"
	sectionEnd   = "<!-- millennium:end -->"
)

// Writer writes into the repository at root.
type Writer struct {
	root string
}

// New returns a writer for the repository at root.
func New(root string) *Writer { return &Writer{root: root} }

// WriteFile writes data to target, an absolute path, creating its directory.
func (w *Writer) WriteFile(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".millennium-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", target, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}

// MergeSettings adds millennium's entries to the project's
// .claude/settings.json: the plugin enabled, the sandbox on with
// unsandboxed retries off, writes to .millennium denied for every command,
// and Edit denied on it for every agent. Other settings are kept.
func (w *Writer) MergeSettings(pluginID string) error {
	path := filepath.Join(w.root, ".claude", "settings.json")
	settings, err := readJSONObject(path)
	if err != nil {
		return err
	}
	object(settings, "enabledPlugins")[pluginID] = true
	sandbox := object(settings, "sandbox")
	sandbox["enabled"] = true
	sandbox["allowUnsandboxedCommands"] = false
	appendUnique(object(sandbox, "filesystem"), "denyWrite", "./.millennium")
	appendUnique(object(settings, "permissions"), "deny", "Edit(/.millennium/**)")
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("format %s: %w", path, err)
	}
	return w.WriteFile(path, append(data, '\n'))
}

func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var settings map[string]any
	if err := dec.Decode(&settings); err != nil || settings == nil {
		return nil, fmt.Errorf("%s is not a JSON object, so it is left as it is; fix it and apply again", path)
	}
	return settings, nil
}

// object returns parent[key] as an object, creating it; a non-object value
// there is replaced, since millennium's settings need the object form.
func object(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	created := map[string]any{}
	parent[key] = created
	return created
}

func appendUnique(parent map[string]any, key, value string) {
	list, _ := parent[key].([]any)
	if !slices.Contains(list, any(value)) {
		list = append(list, value)
	}
	parent[key] = list
}

// SetAgentsSection writes section between millennium's markers in
// AGENTS.md, replacing an earlier one and keeping everything else. When the
// repository has a CLAUDE.md, Claude Code reads it instead of AGENTS.md, so
// CLAUDE.md gains an @AGENTS.md import if it lacks one.
func (w *Writer) SetAgentsSection(section string) error {
	agents := filepath.Join(w.root, "AGENTS.md")
	text, err := readOptional(agents)
	if err != nil {
		return err
	}
	block := sectionBegin + "\n" + strings.TrimSpace(section) + "\n" + sectionEnd + "\n"
	if err := w.WriteFile(agents, []byte(replaceSection(text, block))); err != nil {
		return err
	}
	return w.importAgents()
}

func replaceSection(text, block string) string {
	start := strings.Index(text, sectionBegin)
	end := strings.Index(text, sectionEnd)
	if start >= 0 && end > start {
		return text[:start] + block + strings.TrimPrefix(text[end+len(sectionEnd):], "\n")
	}
	if text != "" && !strings.HasSuffix(text, "\n\n") {
		text = strings.TrimRight(text, "\n") + "\n\n"
	}
	return text + block
}

func (w *Writer) importAgents() error {
	claude := filepath.Join(w.root, "CLAUDE.md")
	text, err := readOptional(claude)
	if err != nil || text == "" || strings.Contains(text, "@AGENTS.md") {
		return err
	}
	return w.WriteFile(claude, []byte(strings.TrimRight(text, "\n")+"\n\n@AGENTS.md\n"))
}

// Ignore adds pattern to .gitignore when no line already says it.
func (w *Writer) Ignore(pattern string) error {
	path := filepath.Join(w.root, ".gitignore")
	text, err := readOptional(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return w.WriteFile(path, []byte(text+pattern+"\n"))
}

func readOptional(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}
