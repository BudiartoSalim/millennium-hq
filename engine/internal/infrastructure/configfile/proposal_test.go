package configfile

import (
	"strings"
	"testing"
)

func TestParseProposal(t *testing.T) {
	p, err := Codec{}.ParseProposal([]byte(`{"version":1,"config":{"version":1,"languages":["go"]}}`))
	if err != nil || p.Config == nil || !p.Config.Has("go") || !strings.Contains(string(p.Canonical), "\n  \"languages\": [\n") {
		t.Fatalf("ParseProposal = %+v, %v", p, err)
	}
	exported, err := Codec{}.FormatProposal(p.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Codec{}.ParseProposal(exported)
	if err != nil || !again.Config.Has("go") {
		t.Fatalf("an exported proposal must apply again: %v\n%s", err, exported)
	}
}

func TestParseProposalRejects(t *testing.T) {
	tests := []struct {
		name, data, want string
	}{
		{"wrong version", `{"version":2,"config":{}}`, "version 2"},
		{"empty", `{"version":1}`, `needs "config", "approvals" or both`},
		{"bad approval", `{"version":1,"approvals":[{"ecosystem":"npm"}]}`, "approvals[0]"},
		{"unknown field", `{"version":1,"config":{"version":1,"languages":["go"]},"extra":1}`, "unknown field"},
		{"duplicate key", `{"version":1,"version":1,"config":{}}`, "more than once"},
		{"invalid config", `{"version":1,"config":{"version":1,"languages":["cobol"]}}`, `"cobol"`},
		{"trailing data", `{"version":1,"config":{"version":1,"languages":["go"]}} x`, "unexpected data"},
		{"too large", `{"version":1,"config":{"pad":"` + strings.Repeat("x", maxConfigBytes) + `"}}`, "larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (Codec{}).ParseProposal([]byte(tt.data)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
	if _, err := (Codec{}).FormatProposal([]byte("not json")); err == nil {
		t.Fatal("formatting invalid JSON must fail")
	}
}

func TestParseProposalWithApprovalsOnly(t *testing.T) {
	data := `{"version":1,"approvals":[{"ecosystem":"npm","name":"express","version":"4.21.2","decision":"approved","reason":"HTTP routing for the API","checked":"2026-10-20"}]}`
	p, err := Codec{}.ParseProposal([]byte(data))
	if err != nil || p.Config != nil || len(p.Approvals) != 1 || p.Approvals[0].Key() != "express@4.21.2" {
		t.Fatalf("ParseProposal = %+v, %v", p, err)
	}
	record, err := Codec{}.FormatApproval(p.Approvals[0])
	if err != nil || !strings.Contains(string(record), `"decision": "approved"`) {
		t.Fatalf("FormatApproval = %s, %v", record, err)
	}
}
