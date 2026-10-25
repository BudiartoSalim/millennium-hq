package configfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadValidConfig(t *testing.T) {
	path := writeConfig(t, `{
		"$schema": "../schema/millennium.schema.json",
		"version": 1,
		"languages": ["go", "python"],
		"python": {"typeCheck": "mypy"},
		"paths": {"acceptanceTests": "qa/acceptance"},
		"run": {"start": "make run", "healthUrl": "http://127.0.0.1:8080/health"}
	}`)
	cfg, err := NewLoader(path).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Has(domain.LanguagePython) || cfg.PythonTypeCheck != domain.PythonTypeCheckMypy ||
		cfg.AcceptanceTests != "qa/acceptance" || cfg.Run == nil || cfg.Run.Start != "make run" {
		t.Fatalf("unexpected config %+v", cfg)
	}
}

func TestLoadMissingConfig(t *testing.T) {
	_, err := NewLoader(filepath.Join(t.TempDir(), "absent.json")).Load()
	if !errors.Is(err, port.ErrConfigNotFound) {
		t.Fatalf("err = %v, want ErrConfigNotFound", err)
	}
}

func TestLoadRejects(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"unknown field", `{"version":1,"languages":["go"],"lint":"off"}`, `unknown field "lint"`},
		{"unknown nested field", `{"version":1,"languages":["go"],"python":{"typecheckr":"off"}}`, `unknown field "typecheckr"`},
		{"duplicate key", `{"version":1,"languages":["go"],"testing":"tests-required","testing":"tests-required"}`, `"testing" appears more than once`},
		{"duplicate key differing in case", `{"version":1,"languages":["go"],"Testing":"tests-required","testing":"tests-required"}`, "appears more than once"},
		{"ci is no longer a setting", `{"version":1,"languages":["go"],"ci":"none"}`, `unknown field "ci"`},
		{"duplicate nested key", `{"version":1,"languages":["go"],"run":{"start":"a","start":"b","healthUrl":"http://localhost/"}}`, `"start" appears more than once in $.run`},
		{"trailing data", `{"version":1,"languages":["go"]} {"version":1}`, "unexpected data after"},
		{"not an object", `["go"]`, "parse"},
		{"wrong type", `{"version":"1","languages":["go"]}`, "parse"},
		{"empty file", ``, "parse"},
		{"invalid values", `{"version":1,"languages":["cobol"]}`, `"cobol" is not one of`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLoader(writeConfig(t, tt.content)).Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
			if errors.Is(err, port.ErrConfigNotFound) {
				t.Fatal("a broken config must not look like a missing one")
			}
		})
	}
}

func TestLoadRejectsOversizedConfig(t *testing.T) {
	padding := strings.Repeat(" ", maxConfigBytes)
	path := writeConfig(t, `{"version":1,"languages":["go"]}`+padding)
	if _, err := NewLoader(path).Load(); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v, want size error", err)
	}
}

func TestLoadRejectsNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	target := writeConfig(t, `{"version":1,"languages":["go"]}`)
	link := filepath.Join(dir, "config.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	if _, err := NewLoader(link).Load(); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("err = %v, want regular-file error for a symlink", err)
	}
	if _, err := NewLoader(dir).Load(); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("err = %v, want regular-file error for a directory", err)
	}
}
