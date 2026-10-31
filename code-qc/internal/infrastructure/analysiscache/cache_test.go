package analysiscache

import (
	"context"
	"errors"
	"testing"

	"millennium-code-qc/internal/domain"
)

type counting struct{ calls int }

func (c *counting) Analyze(_ context.Context, path string, content []byte) (domain.FileFacts, error) {
	c.calls++
	if string(content) == "bad" {
		return domain.FileFacts{}, errors.New("parse error")
	}
	return domain.FileFacts{Path: path}, nil
}

func TestCacheAnalysesEachVersionOnce(t *testing.T) {
	inner := &counting{}
	a := Wrap(inner)
	for range 3 {
		if _, err := a.Analyze(t.Context(), "a.go", []byte("v1")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Analyze(t.Context(), "a.go", []byte("v2")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Analyze(t.Context(), "a.go", []byte("bad")); err == nil {
		t.Fatal("errors must be cached and returned too")
	}
	if _, err := a.Analyze(t.Context(), "a.go", []byte("bad")); err == nil {
		t.Fatal("a cached error must still be an error")
	}
	if inner.calls != 3 {
		t.Fatalf("inner analyzer called %d times, want 3 (v1, v2, bad)", inner.calls)
	}
}
