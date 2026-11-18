package domain

import (
	"path"
	"strings"
	"unicode"
)

// The guard matches paths without regard to case on every OS: on a
// case-insensitive file system (macOS, Windows, WSL on a Windows drive)
// `.MILLENNIUM/config.json` is the config, and the engine cannot tell from
// a path which kind of file system holds it. On a case-sensitive one this
// only denies a few more paths. Case is folded as Unicode simple folding
// does, which also folds the characters such file systems treat alike,
// such as `ſ` and `s`.

// WithinFold is Within without regard to case: p is root or below it.
func WithinFold(p, root string) bool {
	if root == "" {
		return true
	}
	_, ok := RelativeFold(root, p)
	return ok
}

// RelativeFold returns target relative to base, both slash-separated and
// clean, when target is base or lies below it, comparing whole segments
// without regard to case. The rest keeps target's spelling; base itself
// is "".
func RelativeFold(base, target string) (string, bool) {
	baseSegments := strings.Split(strings.TrimSuffix(base, "/"), "/")
	targetSegments := strings.Split(target, "/")
	if len(targetSegments) < len(baseSegments) || !equalSegmentsFold(baseSegments, targetSegments[:len(baseSegments)]) {
		return "", false
	}
	return strings.Join(targetSegments[len(baseSegments):], "/"), true
}

// hasSegmentsFold reports whether p, slash-separated, holds the segments of
// seq one after another anywhere, comparing whole segments without regard
// to case: "/x/.Millennium/config.json" holds ".millennium", and
// "a/.claude/settings.json" holds ".claude/settings.json".
func hasSegmentsFold(p, seq string) bool {
	segments, want := strings.Split(p, "/"), strings.Split(seq, "/")
	for start := 0; start+len(want) <= len(segments); start++ {
		if equalSegmentsFold(segments[start:start+len(want)], want) {
			return true
		}
	}
	return false
}

// equalSegmentsFold reports whether a and b, of equal length, hold the same
// segments without regard to case.
func equalSegmentsFold(a, b []string) bool {
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}

// matchSegmentFold is path.Match without regard to case, for a pattern
// segment the config allows ('*' and '?' wildcards, no '[' or '\').
func matchSegmentFold(pattern, name string) bool {
	ok, _ := path.Match(foldKey(pattern), foldKey(name))
	return ok
}

// ContainsFold reports whether s contains sub without regard to case.
func ContainsFold(s, sub string) bool {
	return strings.Contains(foldKey(s), foldKey(sub))
}

// foldKey spells s with each character replaced by the smallest one it
// folds to, so two strings that differ only in case have the same key.
func foldKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		b.WriteRune(smallestFold(r))
	}
	return b.String()
}

// smallestFold is the smallest character in r's case-folding orbit.
func smallestFold(r rune) rune {
	smallest := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		smallest = min(smallest, f)
	}
	return smallest
}
