package packs

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

func TestBuiltinPacksAreValid(t *testing.T) {
	source, err := Builtin()
	if err != nil {
		t.Fatalf("Builtin: %v", err)
	}
	pack, err := source.Pack(domain.ConventionCleanArchitecture)
	if err != nil {
		t.Fatalf("clean-architecture: %v", err)
	}
	if _, ok := pack.Paths[domain.LanguageGo]; !ok || len(pack.Layers) != 4 {
		t.Fatalf("clean-architecture pack = %+v", pack)
	}
}

func TestMissingPackIsUnavailable(t *testing.T) {
	source, err := Builtin()
	if err != nil {
		t.Fatalf("Builtin: %v", err)
	}
	if _, err := source.Pack(domain.ConventionReactFeatureBased); !errors.Is(err, port.ErrPackUnavailable) {
		t.Fatalf("err = %v, want ErrPackUnavailable", err)
	}
}

func TestLoadRejectsBrokenPacks(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{"unknown field", `{"name":"clean-architecture","languages":["go"],"layers":[],"paths":{},"extra":1}`, "unknown field"},
		{"invalid pack", `{"name":"clean-architecture","languages":["go"],"layers":[{"name":"domain","mayImport":["web"]}],"paths":{}}`, `"web" is not a layer`},
		{"no languages", `{"name":"clean-architecture","layers":[],"paths":{}}`, "at least one language"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := fstest.MapFS{"builtin/x.json": {Data: []byte(tt.content)}}
			if _, err := load(files); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}
