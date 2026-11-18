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
// analyzer for the runtime code. A wrapped analyzer that cannot establish
// it gives unknown runtime code.
func (a *Analyzer) AnalyzeRuntime(ctx context.Context, path string, content []byte) (domain.FileFacts, error) {
	return a.cached(key{path, sha256.Sum256(content), runtime}, func() (domain.FileFacts, error) {
		if inner, ok := a.inner.(port.RuntimeAnalyzer); ok {
			return inner.AnalyzeRuntime(ctx, path, content)
		}
		facts, err := a.inner.Analyze(ctx, path, content)
		facts.Runtime = domain.RuntimeCode{}
		return facts, err
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
