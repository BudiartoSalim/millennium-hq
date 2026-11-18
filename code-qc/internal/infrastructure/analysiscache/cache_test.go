package analysiscache

import (
	"context"
	"errors"
	"strings"
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

// layered establishes runtime code only when asked, like the TypeScript
// analyzer, and counts each kind of request.
type layered struct {
	plain, runtime int
}

func knownRuntime() domain.RuntimeCode {
	r, err := domain.NewRuntimeCode([]int{1}, strings.Repeat("a", 64), 1)
	if err != nil {
		panic(err)
	}
	return r
}

func (l *layered) Analyze(_ context.Context, path string, _ []byte) (domain.FileFacts, error) {
	l.plain++
	return domain.FileFacts{Path: path}, nil
}

func (l *layered) AnalyzeRuntime(_ context.Context, path string, _ []byte) (domain.FileFacts, error) {
	l.runtime++
	return domain.FileFacts{Path: path, Runtime: knownRuntime()}, nil
}

// A plain result never answers a request for runtime code (PERF-1): the
// cache key holds what was asked. A runtime result answers both.
func TestCacheKeepsPlainAndRuntimeResultsApart(t *testing.T) {
	inner := &layered{}
	a := Wrap(inner)
	if facts, _ := a.Analyze(t.Context(), "a.ts", []byte("v1")); facts.Runtime.Known() {
		t.Fatal("a plain analysis has no runtime code here")
	}
	if facts, _ := a.AnalyzeRuntime(t.Context(), "a.ts", []byte("v1")); !facts.Runtime.Known() {
		t.Fatal("the cached plain result answered a request for runtime code")
	}
	if facts, _ := a.AnalyzeRuntime(t.Context(), "a.ts", []byte("v1")); !facts.Runtime.Known() {
		t.Fatal("the runtime result must be cached")
	}
	if facts, _ := a.Analyze(t.Context(), "a.ts", []byte("v2")); facts.Runtime.Known() {
		t.Fatal("v2 was not analysed yet")
	}
	if _, err := a.AnalyzeRuntime(t.Context(), "a.ts", []byte("v3")); err != nil {
		t.Fatal(err)
	}
	if facts, _ := a.Analyze(t.Context(), "a.ts", []byte("v3")); !facts.Runtime.Known() {
		t.Fatal("a runtime result holds everything a plain one does, so it serves a plain request")
	}
	if inner.plain != 2 || inner.runtime != 2 {
		t.Fatalf("inner analyses: %d plain, %d runtime; want 2 and 2", inner.plain, inner.runtime)
	}
}

// plainWithRuntime cannot be asked for runtime code, though its plain
// analysis reports some.
type plainWithRuntime struct{}

func (plainWithRuntime) Analyze(_ context.Context, path string, _ []byte) (domain.FileFacts, error) {
	return domain.FileFacts{Path: path, Runtime: knownRuntime()}, nil
}

func TestCacheGivesUnknownRuntimeForAnAnalyzerThatCannotEstablishIt(t *testing.T) {
	a := Wrap(plainWithRuntime{})
	facts, err := a.AnalyzeRuntime(t.Context(), "a.sh", []byte("echo"))
	if err != nil || facts.Runtime.Known() || facts.Path != "a.sh" {
		t.Fatalf("AnalyzeRuntime = %+v, %v; want the facts with unknown runtime code", facts, err)
	}
}
