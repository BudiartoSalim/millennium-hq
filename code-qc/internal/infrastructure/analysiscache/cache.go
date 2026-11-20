// Package analysiscache remembers analyzer results for one run, so the gates
// that read the same file parse it once.
package analysiscache

import (
	"context"
	"crypto/sha256"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// depth is what an analysis was asked to establish.
type depth int

const (
	// plain is Analyze: the runtime code may be unknown.
	plain depth = iota
	// runtime is AnalyzeRuntime: the runtime code was asked for.
	runtime
)

type key struct {
	path  string
	sum   [sha256.Size]byte
	depth depth
}

type result struct {
	facts domain.FileFacts
	err   error
}

// Analyzer wraps another analyzer with a cache keyed by path, content and
// what was asked for, so the working and base versions of a file are cached
// separately, and a plain result, which may lack the runtime code, never
// answers a request for it.
type Analyzer struct {
	inner   port.Analyzer
	results map[key]result
}

// Wrap returns a caching analyzer around inner.
func Wrap(inner port.Analyzer) *Analyzer {
	return &Analyzer{inner: inner, results: map[key]result{}}
}

// Analyze returns the cached result or asks the wrapped analyzer. A
// runtime result holds everything a plain one does, so it serves too.
func (a *Analyzer) Analyze(ctx context.Context, path string, content []byte) (domain.FileFacts, error) {
	sum := sha256.Sum256(content)
	if r, ok := a.results[key{path, sum, runtime}]; ok {
		return r.facts, r.err
	}
	return a.cached(key{path, sum, plain}, func() (domain.FileFacts, error) { return a.inner.Analyze(ctx, path, content) })
}

// AnalyzeRuntime returns the cached runtime result or asks the wrapped
// analyzer for the runtime code (see port.AnalyzeRuntime). A cached plain
// failure answers too: a runtime analysis does all a plain one does, so it
// would fail the same way, after the costly part (a TypeScript transpile).
// A plain success never answers, since it may lack the runtime code.
func (a *Analyzer) AnalyzeRuntime(ctx context.Context, path string, content []byte) (domain.FileFacts, error) {
	sum := sha256.Sum256(content)
	if r, ok := a.results[key{path, sum, plain}]; ok && r.err != nil {
		return r.facts, r.err
	}
	return a.cached(key{path, sum, runtime}, func() (domain.FileFacts, error) {
		return port.AnalyzeRuntime(ctx, a.inner, path, content)
	})
}

func (a *Analyzer) cached(k key, analyze func() (domain.FileFacts, error)) (domain.FileFacts, error) {
	if r, ok := a.results[k]; ok {
		return r.facts, r.err
	}
	facts, err := analyze()
	a.results[k] = result{facts: facts, err: err}
	return facts, err
}
