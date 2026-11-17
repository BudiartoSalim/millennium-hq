package domain

import (
	"slices"
	"strings"
	"testing"
)

var (
	digestA = strings.Repeat("a", 64)
	digestB = strings.Repeat("0123456789abcdef", 4)
)

func mustRuntime(t *testing.T, lines []int, digest string, lineCount int) RuntimeCode {
	t.Helper()
	r, err := NewRuntimeCode(lines, digest, lineCount)
	if err != nil {
		t.Fatalf("NewRuntimeCode: %v", err)
	}
	return r
}

func TestNewRuntimeCodeRejectsUntrustedFacts(t *testing.T) {
	tests := []struct {
		name      string
		lines     []int
		digest    string
		lineCount int
		want      string
	}{
		{"empty digest", nil, "", 3, "not 64 lowercase hex"},
		{"short digest", nil, digestA[:63], 3, "not 64 lowercase hex"},
		{"long digest", nil, digestA + "a", 3, "not 64 lowercase hex"},
		{"uppercase digest", nil, strings.Repeat("A", 64), 3, "not 64 lowercase hex"},
		{"non-hex digest", nil, strings.Repeat("g", 64), 3, "not 64 lowercase hex"},
		{"line zero", []int{0}, digestA, 3, "line 0 is outside"},
		{"negative line", []int{-1}, digestA, 3, "line -1 is outside"},
		{"line past the end", []int{1, 4}, digestA, 3, "line 4 is outside the file's 3 lines"},
		{"any line in an empty file", []int{1}, digestA, 0, "line 1 is outside"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRuntimeCode(tt.lines, tt.digest, tt.lineCount)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestRuntimeCodeHolds(t *testing.T) {
	known := mustRuntime(t, []int{5, 2, 2, 9}, digestB, 9)
	var got []int
	for line := 1; line <= 10; line++ {
		if known.Holds(line) {
			got = append(got, line)
		}
	}
	if !known.Known() || !slices.Equal(got, []int{2, 5, 9}) {
		t.Fatalf("known facts hold lines %v, want [2 5 9]", got)
	}
	none := mustRuntime(t, nil, digestA, 3)
	if !none.Known() || none.Holds(1) {
		t.Fatal("a known file without runtime lines holds no runtime code")
	}
	var unknown RuntimeCode
	if unknown.Known() || !unknown.Holds(1) || !unknown.Holds(1000) {
		t.Fatal("unknown facts must count every line as runtime")
	}
}

func TestSameRuntime(t *testing.T) {
	a := mustRuntime(t, []int{1}, digestA, 1)
	sameDigest := mustRuntime(t, []int{2}, digestA, 2)
	b := mustRuntime(t, []int{1}, digestB, 1)
	tests := []struct {
		name          string
		before, after RuntimeCode
		want          bool
	}{
		{"equal known digests", a, sameDigest, true},
		{"different digests", a, b, false},
		{"unknown before", RuntimeCode{}, a, false},
		{"unknown after", a, RuntimeCode{}, false},
		{"both unknown", RuntimeCode{}, RuntimeCode{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SameRuntime(tt.before, tt.after); got != tt.want {
				t.Fatalf("SameRuntime = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLineCountFollowsGit(t *testing.T) {
	tests := []struct {
		content string
		want    int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},
		{"a\nb", 2},
		{"a\r\nb\r\n", 2},
		{"a\rb\n", 1},
		{"\n\n", 2},
	}
	for _, tt := range tests {
		if got := LineCount([]byte(tt.content)); got != tt.want {
			t.Errorf("LineCount(%q) = %d, want %d", tt.content, got, tt.want)
		}
	}
}

func TestUncoveredCountsOnlyRuntimeLines(t *testing.T) {
	// A source-mapped lcov gives a leading doc comment and an erased
	// interface zero hits (lines 1 and 2); only the function body runs code.
	coverage := FileCoverage{1: 0, 2: 0, 3: 0, 4: 1, 5: 0}
	runtime := mustRuntime(t, []int{3, 4, 5}, digestA, 5)
	if got := coverage.Uncovered(AllLines(), runtime); !slices.Equal(got, []LineRange{{First: 3, Last: 3}, {First: 5, Last: 5}}) {
		t.Fatalf("known runtime: uncovered = %+v, want lines 3 and 5 only", got)
	}
	if got := coverage.Uncovered(AllLines(), RuntimeCode{}); !slices.Equal(got, []LineRange{{First: 1, Last: 3}, {First: 5, Last: 5}}) {
		t.Fatalf("unknown runtime: uncovered = %+v, want every zero-hit line", got)
	}
}
