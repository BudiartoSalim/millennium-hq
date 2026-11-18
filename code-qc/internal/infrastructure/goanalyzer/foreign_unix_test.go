//go:build unix

package goanalyzer

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A FIFO named like a Go file would block the read, so it is refused
// rather than read.
func TestGoForeignCoverageRefusesAFIFO(t *testing.T) {
	root := treeWith(t, map[string]string{"p/a.go": "package p\n"})
	if err := syscall.Mkfifo(filepath.Join(root, "p", "fifo.go"), 0o644); err != nil {
		t.Skipf("FIFOs unavailable: %v", err)
	}
	if _, err := NewSources(root).GoForeignCoverage("p"); err == nil || !strings.Contains(err.Error(), "fifo.go is not a regular file") {
		t.Fatalf("GoForeignCoverage = %v, want the FIFO refused", err)
	}
}
