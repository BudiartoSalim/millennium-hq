package usecase

import (
	"errors"
	"strings"
	"testing"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

type failingPackFormat struct{ jsonCodec }

func (failingPackFormat) FormatPack(domain.Pack) (port.PackFiles, error) {
	return port.PackFiles{}, errors.New("format failed")
}

func conventions(t *testing.T, source mapFiles, codec port.ProposalCodec) (*Conventions, *recordingWriter) {
	t.Helper()
	w := &recordingWriter{}
	committed := fakeConfigFile{project: []domain.Pack{flatPack(t, 15)}}
	local := fakeConfigFile{}
	return NewConventions(committed, local, source, codec, w, "/repo"), w
}

func TestConventionsListAndShow(t *testing.T) {
	uc, _ := conventions(t, mapFiles{}, jsonCodec{})
	committed, err := uc.List(ApplyCommitted)
	if err != nil || len(committed) != 3 || committed[1].Name != "flat" {
		t.Fatalf("List = %+v, %v", committed, err)
	}
	local, err := uc.List(ApplyLocal)
	if err != nil || len(local) != 2 {
		t.Fatalf("the local catalog has only the built-in packs: %+v, %v", local, err)
	}
	pack, err := uc.Show(ApplyCommitted, "flat")
	if err != nil || pack.Guide != "Keep it flat." {
		t.Fatalf("Show = %+v, %v", pack, err)
	}
	if _, err := uc.Show(ApplyCommitted, "hexagonal"); !errors.Is(err, ErrUnknownConvention) {
		t.Fatalf("an unknown convention: %v", err)
	}
}

func TestConventionsNew(t *testing.T) {
	uc, w := conventions(t, mapFiles{}, jsonCodec{})
	draft, err := uc.New(ApplyCommitted, "hexagonal", "clean-architecture")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if draft.Dir != "millennium.proposal/conventions/hexagonal" || draft.Pack.Origin != domain.PackProject ||
		!strings.Contains(draft.Proposal, `"packs": {"hexagonal": "millennium.proposal/conventions/hexagonal"}`) {
		t.Fatalf("draft = %+v", draft)
	}
	want := []string{
		"write /repo/millennium.proposal/conventions/hexagonal/pack.json pack hexagonal",
		"write /repo/millennium.proposal/conventions/hexagonal/guide.md Business rules live in domain.",
	}
	if len(w.calls) != 2 || w.calls[0] != want[0] || w.calls[1] != want[1] {
		t.Fatalf("writes = %q", w.calls)
	}
}

func TestConventionsNewRefuses(t *testing.T) {
	drafted := mapFiles{"millennium.proposal/conventions/hexagonal/pack.json": "{}"}
	tests := []struct {
		name       string
		source     mapFiles
		codec      port.ProposalCodec
		draft      domain.Convention
		from       domain.Convention
		want       string
		wantWrites int
	}{
		{"bad name", mapFiles{}, jsonCodec{}, "Hexagonal", "clean-architecture", "not a convention name", 0},
		{"taken name", mapFiles{}, jsonCodec{}, "flat", "clean-architecture", "convention flat already exists", 0},
		{"unknown source", mapFiles{}, jsonCodec{}, "hexagonal", "onion", "no convention pack has this name: onion", 0},
		{"existing draft", drafted, jsonCodec{}, "hexagonal", "clean-architecture", "already holds a draft", 0},
		{"format failure", mapFiles{}, failingPackFormat{}, "hexagonal", "clean-architecture", "format failed", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc, w := conventions(t, tt.source, tt.codec)
			if _, err := uc.New(ApplyCommitted, tt.draft, tt.from); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if len(w.calls) != tt.wantWrites {
				t.Fatalf("writes = %q", w.calls)
			}
		})
	}
}

func TestConventionsFailures(t *testing.T) {
	broken := NewConventions(fakeConfigFile{packsErr: errors.New("bad pack")}, fakeConfigFile{}, mapFiles{}, jsonCodec{}, &recordingWriter{}, "/repo")
	if _, err := broken.List(ApplyCommitted); err == nil || !strings.Contains(err.Error(), "read the convention packs: bad pack") {
		t.Fatalf("List: %v", err)
	}
	if _, err := broken.Show(ApplyCommitted, "flat"); err == nil {
		t.Fatal("Show must fail with the packs unreadable")
	}
	if _, err := broken.New(ApplyCommitted, "hexagonal", "clean-architecture"); err == nil {
		t.Fatal("New must fail with the packs unreadable")
	}
	uc, w := conventions(t, mapFiles{}, jsonCodec{})
	w.err = errors.New("read-only file system")
	if _, err := uc.New(ApplyCommitted, "hexagonal", "clean-architecture"); err == nil || len(w.calls) != 1 {
		t.Fatalf("a failed pack write: %v, %q", err, w.calls)
	}
	w.calls, w.failAt = nil, 2
	if _, err := uc.New(ApplyCommitted, "onion", "clean-architecture"); err == nil || len(w.calls) != 2 {
		t.Fatalf("a failed guide write: %v, %q", err, w.calls)
	}
}
