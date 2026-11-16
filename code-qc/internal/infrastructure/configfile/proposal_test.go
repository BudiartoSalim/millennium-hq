package configfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseProposal(t *testing.T) {
	p, err := Codec{}.ParseProposal([]byte(`{"version":1,"config":{"version":1,"languages":["go"]}}`), t.TempDir())
	if err != nil || p.Config == nil || !p.Config.Has("go") || !strings.Contains(string(p.Canonical), "\n  \"languages\": [\n") {
		t.Fatalf("ParseProposal = %+v, %v", p, err)
	}
	exported, err := Codec{}.FormatProposal(p.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Codec{}.ParseProposal(exported, t.TempDir())
	if err != nil || !again.Config.Has("go") {
		t.Fatalf("an exported proposal must apply again: %v\n%s", err, exported)
	}
}

func TestParseProposalRejects(t *testing.T) {
	tests := []struct {
		name, data, want string
	}{
		{"wrong version", `{"version":2,"config":{}}`, "version 2"},
		{"empty", `{"version":1}`, `needs "config", "packs", "approvals" or several of them`},
		{"bad approval", `{"version":1,"approvals":[{"ecosystem":"npm"}]}`, "approvals[0]"},
		{"unknown field", `{"version":1,"config":{"version":1,"languages":["go"]},"extra":1}`, "unknown field"},
		{"duplicate key", `{"version":1,"version":1,"config":{}}`, "more than once"},
		{"invalid config", `{"version":1,"config":{"version":1,"languages":["cobol"]}}`, `"cobol"`},
		{"trailing data", `{"version":1,"config":{"version":1,"languages":["go"]}} x`, "unexpected data"},
		{"too large", `{"version":1,"config":{"pad":"` + strings.Repeat("x", maxConfigBytes) + `"}}`, "larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (Codec{}).ParseProposal([]byte(tt.data), t.TempDir()); err == nil || !strings.Contains(err.Error(), tt.want) {
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
	p, err := Codec{}.ParseProposal([]byte(data), t.TempDir())
	if err != nil || p.Config != nil || len(p.Approvals) != 1 || p.Approvals[0].Key() != "express@4.21.2" {
		t.Fatalf("ParseProposal = %+v, %v", p, err)
	}
	record, err := Codec{}.FormatApproval(p.Approvals[0])
	if err != nil || !strings.Contains(string(record), `"decision": "approved"`) {
		t.Fatalf("FormatApproval = %s, %v", record, err)
	}
}

func TestParseProposalWithPacks(t *testing.T) {
	dir := t.TempDir()
	writePack(t, filepath.Join(dir, "millennium.proposal", "config.json"), "flat", flatPack, "Keep it flat.")
	data := `{"version":1,"packs":{"flat":"millennium.proposal/conventions/flat"},
		"config":{"version":1,"languages":["go"],"conventions":["flat"]}}`
	p, err := Codec{}.ParseProposal([]byte(data), dir)
	if err != nil || len(p.Packs) != 1 || p.Packs[0].Pack.Name != "flat" || string(p.Packs[0].Files.Guide) != "Keep it flat." {
		t.Fatalf("ParseProposal = %+v, %v", p, err)
	}
	if len(p.Config.Packs) != 0 {
		t.Fatal("the proposal's config is resolved by apply, against the packs it would leave in place")
	}
	files, err := Codec{}.FormatPack(p.Packs[0].Pack)
	if err != nil || !strings.Contains(string(files.Pack), `"nesting": 3`) {
		t.Fatalf("FormatPack = %s, %v", files.Pack, err)
	}
}

func TestParseProposalRejectsBadPacks(t *testing.T) {
	dir := t.TempDir()
	writePack(t, filepath.Join(dir, "drafts", "config.json"), "flat", flatPack, "Keep it flat.")
	if err := os.WriteFile(filepath.Join(dir, "outside"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, packs, want string }{
		{"outside the proposal folder", `{"flat":"../flat"}`, "must be a folder below the proposal's own"},
		{"absolute", `{"flat":"/etc"}`, "must be a folder below the proposal's own"},
		{"missing", `{"flat":"drafts/conventions/none"}`, "proposal packs.flat:"},
		{"another pack's folder", `{"other":"drafts/conventions/flat"}`, `the folder holds pack "flat"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := `{"version":1,"packs":` + tt.packs + `}`
			if _, err := (Codec{}).ParseProposal([]byte(data), dir); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// A proposal named on the command line is read like a config: bounded, and
// only from a regular file, so /dev/zero or a directory is refused unread.
func TestReadProposal(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "p.json")
	large := filepath.Join(dir, "large.json")
	if err := os.WriteFile(good, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(large, make([]byte, maxConfigBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadProposal(good); err != nil || string(data) != `{"version":1}` {
		t.Fatalf("ReadProposal = %q, %v", data, err)
	}
	for _, bad := range []string{large, dir, os.DevNull, filepath.Join(dir, "none.json")} {
		if _, err := ReadProposal(bad); err == nil || !strings.HasPrefix(err.Error(), "read the proposal: ") {
			t.Errorf("ReadProposal(%s) = %v, want a refusal", bad, err)
		}
	}
}
