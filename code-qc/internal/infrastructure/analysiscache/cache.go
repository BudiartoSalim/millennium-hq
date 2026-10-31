// Package analysiscache remembers analyzer results for one run, so the gates
// that read the same file parse it once.
package analysiscache

import (
	"context"
	"crypto/sha256"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

type key struct {
	path string
	sum  [sha256.Size]byte
}

type result struct {
	facts domain.FileFacts
	err   error
}

// Analyzer wraps another analyzer with a cache keyed by path and content, so
// the working and base versions of a file are cached separately.
type Analyzer struct {
	inner   port.Analyzer
	results map[key]result
}

// Wrap returns a caching analyzer around inner.
func Wrap(inner port.Analyzer) *Analyzer {
	return &Analyzer{inner: inner, results: map[key]result{}}
}

// Analyze returns the cached result or asks the wrapped analyzer.
func (a *Analyzer) Analyze(ctx context.Context, path string, content []byte) (domain.FileFacts, error) {
	k := key{path: path, sum: sha256.Sum256(content)}
	if r, ok := a.results[k]; ok {
		return r.facts, r.err
	}
	facts, err := a.inner.Analyze(ctx, path, content)
	a.results[k] = result{facts: facts, err: err}
	return facts, err
}
