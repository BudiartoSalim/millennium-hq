package statestore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStore(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "state"))
	for want := 1; want <= 3; want++ {
		if got, err := s.Increment("session/agent"); err != nil || got != want {
			t.Fatalf("Increment = %d, %v; want %d", got, err, want)
		}
	}
	if got, err := s.Increment("../../etc/passwd"); err != nil || got != 1 {
		t.Fatalf("keys are independent and safe as file names: %d, %v", got, err)
	}
	if err := s.Reset("session/agent"); err != nil {
		t.Fatal(err)
	}
	if err := s.Reset("never-set"); err != nil {
		t.Fatalf("resetting an unknown key is fine: %v", err)
	}
	if got, _ := s.Increment("session/agent"); got != 1 {
		t.Fatalf("after Reset the count restarts, got %d", got)
	}
}

func TestStoreRejectsCorruptCounters(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := os.WriteFile(s.path("k"), []byte("not a number"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Increment("k"); err == nil {
		t.Fatal("a corrupt counter must be an error, not a silent reset")
	}
	blocked := New(filepath.Join(dir, "file-not-dir"))
	if err := os.WriteFile(filepath.Join(dir, "file-not-dir"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := blocked.Increment("k"); err == nil {
		t.Fatal("an unusable state directory must be an error")
	}
}
