package strictfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRegular(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.json")
	if err := os.WriteFile(file, []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadRegular(file, 5); err != nil || string(data) != "12345" {
		t.Fatalf("ReadRegular = %q, %v", data, err)
	}
	if _, err := ReadRegular(file, 4); err == nil || !strings.Contains(err.Error(), "larger than 4 bytes") {
		t.Fatalf("an oversized file: %v", err)
	}
	if _, err := ReadRegular(filepath.Join(dir, "missing"), 5); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing file wraps fs.ErrNotExist: %v", err)
	}
	if _, err := ReadRegular(dir, 5); err == nil || !strings.Contains(err.Error(), "found a directory") {
		t.Fatalf("a directory: %v", err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(file, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	if _, err := ReadRegular(link, 5); err == nil || !strings.Contains(err.Error(), "found a symlink") {
		t.Fatalf("a symlink: %v", err)
	}
}

func TestReadRegularRefusesSpecialFiles(t *testing.T) {
	if _, err := os.Stat(os.DevNull); err != nil {
		t.Skip("no null device here")
	}
	if _, err := ReadRegular(os.DevNull, 5); err == nil || !strings.Contains(err.Error(), "found a special file") {
		t.Fatalf("a device: %v", err)
	}
}

func TestDecodeJSON(t *testing.T) {
	type doc struct {
		Name string `json:"name"`
		Run  struct {
			Start string `json:"start"`
		} `json:"run"`
	}
	var ok doc
	if err := DecodeJSON([]byte(`{"name":"a","run":{"start":"x"}}`), &ok); err != nil || ok.Run.Start != "x" {
		t.Fatalf("DecodeJSON = %+v, %v", ok, err)
	}
	tests := []struct{ name, data, want string }{
		{"duplicate key", `{"name":"a","name":"b"}`, `"name" appears more than once in $`},
		{"duplicate key differing in case", `{"Name":"a","name":"b"}`, "appears more than once"},
		{"duplicate nested key", `{"run":{"start":"a","start":"b"}}`, `"start" appears more than once in $.run`},
		{"duplicate key in an array", `{"list":[{"a":1,"a":2}]}`, "in $.list[0]"},
		{"unknown field", `{"other":1}`, "unknown field"},
		{"trailing data", `{"name":"a"} {}`, "unexpected data after the object"},
		{"broken", `{"name":`, "parse:"},
		{"not an object", `[1,`, "parse:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v doc
			if err := DecodeJSON([]byte(tt.data), &v); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}
