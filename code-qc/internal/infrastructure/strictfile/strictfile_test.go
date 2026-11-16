package strictfile

import (
	"encoding/json"
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

type limitsDoc struct {
	Lines int `json:"lines"`
}

type componentDoc struct {
	Paths  []string             `json:"paths"`
	Limits *limitsDoc           `json:"limits,omitempty"`
	Layers map[string]limitsDoc `json:"layers"`
	Raw    json.RawMessage      `json:"raw"`
	hidden int
	Skip   int `json:"-"`
}

type namedDoc struct {
	Version    int            `json:"version"`
	Components []componentDoc `json:"components"`
	Name       string
}

func TestDecodeJSONNamesTheAllowedFields(t *testing.T) {
	tests := []struct{ name, data, want string }{
		{"top level", `{"version":1,"lnaguages":[]}`, `unknown field "lnaguages" in $; allowed fields there: version, components, Name`},
		{"in an array element", `{"components":[{"paths":[]},{"pahts":[]}]}`, `unknown field "pahts" in $.components[1]; allowed fields there: paths, limits, layers, raw`},
		{"behind a pointer", `{"components":[{"limits":{"line":1}}]}`, `unknown field "line" in $.components[0].limits; allowed fields there: lines`},
		{"in a map value", `{"components":[{"layers":{"domain":{"lines":1,"depth":2}}}]}`, `unknown field "depth" in $.components[0].layers["domain"]`},
		{"a map key with control characters", `{"components":[{"layers":{"a\u001b[2Jb":{"x":1}}}]}`, `layers["a\x1b[2Jb"]`},
		{"a key in other case is named as declared", `{"COMPONENTS":[{"PATHS":[],"limits":{"x":1}}]}`, `in $.components[0].limits;`},
		{"a field json ignores", `{"Skip":1}`, `unknown field "Skip" in $`},
		// encoding/json rejects the first unknown key in the file; that one
		// is explained, not the first in sorted order.
		{"the key json rejected", `{"zz":1,"aa":2}`, `unknown field "zz" in $`},
		{"with trailing data", `{"zz":1} {}`, `unknown field "zz" in $; allowed fields there`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v namedDoc
			if err := DecodeJSON([]byte(tt.data), &v); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

type selfDecoding struct {
	Inner int `json:"inner"`
}

func (s *selfDecoding) UnmarshalJSON([]byte) error { return nil }

type base struct {
	Version int `json:"version"`
}

// Only an unknown-field error is explained, and only where its place is
// certain: keys match with encoding/json's case folding, a raw message or a
// type that decodes itself is left alone, and a struct with embedded fields
// keeps the plain error, since promotion rules decide which field a key
// reaches. An earlier error of another kind is kept rather than replaced.
func TestDecodeJSONKeepsOtherErrors(t *testing.T) {
	type doc struct {
		Version int             `json:"version"`
		Testing string          `json:"testing"`
		Raw     json.RawMessage `json:"raw"`
		Self    selfDecoding    `json:"self"`
	}
	var ok doc
	if err := DecodeJSON([]byte(`{"VERSION":1,"te\u017fting":"x","raw":{"anything":1},"self":{"other":1}}`), &ok); err != nil || ok.Testing != "x" {
		t.Fatalf("DecodeJSON = %+v, %v", ok, err)
	}
	var v doc
	err := DecodeJSON([]byte(`{"version":"one","lnaguages":[]}`), &v)
	if err == nil || !strings.Contains(err.Error(), "cannot unmarshal") || strings.Contains(err.Error(), "allowed fields") {
		t.Fatalf("err = %v, want the type error kept", err)
	}
	type embedding struct {
		base
		Name string `json:"name"`
	}
	var e embedding
	err = DecodeJSON([]byte(`{"version":1,"x":1}`), &e)
	if err == nil || !strings.Contains(err.Error(), `unknown field "x"`) || strings.Contains(err.Error(), "allowed fields") {
		t.Fatalf("err = %v, want the plain unknown-field error", err)
	}
}

// encoding/json fills a Go array from the first items and drops the rest
// unread, so an unknown key past its length is never the one it rejected.
func TestDecodeJSONWalksOnlyAnArraysLength(t *testing.T) {
	type doc struct {
		Pair [1]limitsDoc `json:"pair"`
		Name string       `json:"name"`
	}
	var v doc
	err := DecodeJSON([]byte(`{"pair":[{"lines":1},{"x":1}],"x":2}`), &v)
	if err == nil || !strings.Contains(err.Error(), `unknown field "x" in $;`) {
		t.Fatalf("err = %v, want the top-level x", err)
	}
}
