package port

import (
	"context"
	"errors"
	"strings"
	"testing"

	"millennium-code-qc/internal/domain"
)

func known(t *testing.T) domain.RuntimeCode {
	t.Helper()
	runtime, err := domain.NewRuntimeCode([]int{1}, strings.Repeat("a", 64), 1)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// plainOnly reports runtime code from a plain analysis, which it cannot be
// asked for: that is not established, so it must not count.
type plainOnly struct {
	runtime domain.RuntimeCode
	err     error
}

func (p plainOnly) Analyze(_ context.Context, path string, _ []byte) (domain.FileFacts, error) {
	return domain.FileFacts{Path: path, Runtime: p.runtime}, p.err
}

type withRuntime struct{ plainOnly }

func (w withRuntime) AnalyzeRuntime(_ context.Context, path string, _ []byte) (domain.FileFacts, error) {
	return domain.FileFacts{Path: path + " (runtime)", Runtime: w.runtime}, w.err
}

// The shape of ForeignCoverageSource, as the tests gate's fakes and
// goanalyzer.Sources implement it: the code a directory's Go sources lend,
// or why that is unknown, per directory. The port has no logic of its own
// to run; this fails to compile when its shape changes.
var _ ForeignCoverageSource = lenderFunc(nil)

type lenderFunc func(dir string) ([]domain.ForeignCoverage, error)

func (f lenderFunc) GoForeignCoverage(dir string) ([]domain.ForeignCoverage, error) { return f(dir) }

func TestAnalyzeRuntime(t *testing.T) {
	failure := errors.New("syntax error")
	tests := []struct {
		name      string
		analyzer  Analyzer
		wantPath  string
		wantKnown bool
		wantErr   error
	}{
		{"runtime analyzer", withRuntime{plainOnly{runtime: known(t)}}, "a.ts (runtime)", true, nil},
		{"runtime analyzer that fails", withRuntime{plainOnly{err: failure}}, "a.ts (runtime)", false, failure},
		{"plain analyzer", plainOnly{runtime: known(t)}, "a.ts", false, nil},
		{"plain analyzer that fails", plainOnly{runtime: known(t), err: failure}, "a.ts", false, failure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facts, err := AnalyzeRuntime(t.Context(), tt.analyzer, "a.ts", []byte("x"))
			if facts.Path != tt.wantPath || facts.Runtime.Known() != tt.wantKnown || !errors.Is(err, tt.wantErr) {
				t.Fatalf("AnalyzeRuntime = %+v, %v; want path %q, known runtime %v, error %v", facts, err, tt.wantPath, tt.wantKnown, tt.wantErr)
			}
		})
	}
}
