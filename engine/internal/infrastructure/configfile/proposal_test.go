package configfile

import (
	"strings"
	"testing"
)

func TestParseProposal(t *testing.T) {
	cfg, canonical, err := Codec{}.ParseProposal([]byte(`{"version":1,"config":{"version":1,"languages":["go"]}}`))
	if err != nil || !cfg.Has("go") || !strings.Contains(string(canonical), "\n  \"languages\": [\n") {
		t.Fatalf("ParseProposal = %+v, %q, %v", cfg, canonical, err)
	}
	exported, err := Codec{}.FormatProposal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := Codec{}.ParseProposal(exported)
	if err != nil || !again.Has("go") {
		t.Fatalf("an exported proposal must apply again: %v\n%s", err, exported)
	}
}

func TestParseProposalRejects(t *testing.T) {
	tests := []struct {
		name, data, want string
	}{
		{"wrong version", `{"version":2,"config":{}}`, "version 2"},
		{"no config", `{"version":1}`, `"config" is required`},
		{"unknown field", `{"version":1,"config":{"version":1,"languages":["go"]},"extra":1}`, "unknown field"},
		{"duplicate key", `{"version":1,"version":1,"config":{}}`, "more than once"},
		{"invalid config", `{"version":1,"config":{"version":1,"languages":["cobol"]}}`, `"cobol"`},
		{"trailing data", `{"version":1,"config":{"version":1,"languages":["go"]}} x`, "unexpected data"},
		{"too large", `{"version":1,"config":{"pad":"` + strings.Repeat("x", maxConfigBytes) + `"}}`, "larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := (Codec{}).ParseProposal([]byte(tt.data)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
	if _, err := (Codec{}).FormatProposal([]byte("not json")); err == nil {
		t.Fatal("formatting invalid JSON must fail")
	}
}
