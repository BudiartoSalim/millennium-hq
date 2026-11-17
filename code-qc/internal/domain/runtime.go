package domain

import (
	"bytes"
	"fmt"
	"slices"
)

// digestLength is the length of a runtime digest: a SHA-256 sum in hex.
const digestLength = 64

// RuntimeCode is what an analyzer knows about the code of a file that runs:
// which lines hold runtime tokens, and a digest of the runtime token
// sequence. Comments, whitespace and (in TypeScript) erased type syntax are
// not runtime; anything an analyzer cannot positively identify as one of
// them is.
//
// The zero value is unknown: an analyzer that reports nothing, a helper
// answer without the facts, or an analysis that failed. Unknown errs toward
// runtime, so every line may hold runtime code and the file counts as
// changed. A known file with no runtime lines is a different thing: it holds
// only comments, whitespace or types.
type RuntimeCode struct {
	known  bool
	lines  []int
	digest string
}

// NewRuntimeCode returns known runtime facts for a file of lineCount lines,
// as git numbers them (see LineCount). The digest must be 64 lowercase hex
// characters and every line must be within the file: an analyzer answer that
// breaks either is not trusted at all.
func NewRuntimeCode(lines []int, digest string, lineCount int) (RuntimeCode, error) {
	if !isDigest(digest) {
		return RuntimeCode{}, fmt.Errorf("runtime digest %q is not 64 lowercase hex characters", digest)
	}
	for _, line := range lines {
		if line < 1 || line > lineCount {
			return RuntimeCode{}, fmt.Errorf("runtime line %d is outside the file's %d lines", line, lineCount)
		}
	}
	sorted := slices.Clone(lines)
	slices.Sort(sorted)
	return RuntimeCode{known: true, lines: slices.Compact(sorted), digest: digest}, nil
}

func isDigest(s string) bool {
	if len(s) != digestLength {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// Known reports whether an analyzer established the facts.
func (r RuntimeCode) Known() bool { return r.known }

// Holds reports whether line may hold runtime code: always, when the facts
// are unknown.
func (r RuntimeCode) Holds(line int) bool {
	if !r.known {
		return true
	}
	_, found := slices.BinarySearch(r.lines, line)
	return found
}

// SameRuntime reports whether two versions of a file run the same code,
// token for token. Unknown facts never compare equal, not even to each
// other, so a file whose facts are missing on either side counts as changed.
func SameRuntime(before, after RuntimeCode) bool {
	return before.known && after.known && before.digest == after.digest
}

// LineCount is the number of lines in content as git numbers them: only a
// line feed ends a line, and a last line without one still counts.
func LineCount(content []byte) int {
	n := bytes.Count(content, []byte("\n"))
	if len(content) > 0 && content[len(content)-1] != '\n' {
		n++
	}
	return n
}
