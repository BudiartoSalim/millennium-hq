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

// RebaseFold moves target, slash-separated, from below from to below to:
// when target is from or lies below it, comparing whole segments without
// regard to case and each as Win32 opens it (see win32Name), it returns to
// joined with the rest of target, in target's spelling. It names a target
// reached through a symlink as the path the symlink is reached by.
func RebaseFold(target, from, to string) (string, bool) {
	fromSegments := strings.Split(strings.TrimSuffix(from, "/"), "/")
	targetSegments := strings.Split(target, "/")
	if len(targetSegments) < len(fromSegments) {
		return "", false
	}
	for i, segment := range fromSegments {
		if !strings.EqualFold(win32Name(targetSegments[i]), win32Name(segment)) {
			return "", false
		}
	}
	return strings.Join(append([]string{strings.TrimSuffix(to, "/")}, targetSegments[len(fromSegments):]...), "/"), true
}

// hasSegmentsFold reports whether p, slash-separated, holds the segments of
// seq one after another anywhere, comparing whole segments without regard
// to case and each of p's as Win32 opens it (see win32Name):
// "/x/.Millennium/config.json" holds ".millennium", and
// "a/.claude/settings.json." holds ".claude/settings.json".
func hasSegmentsFold(p, seq string) bool {
	segments, want := strings.Split(p, "/"), strings.Split(seq, "/")
	for i, segment := range segments {
		segments[i] = win32Name(segment)
	}
	for start := 0; start+len(want) <= len(segments); start++ {
		if equalSegmentsFold(segments[start:start+len(want)], want) {
			return true
		}
	}
	return false
}

// endsWithSegmentsFold reports whether p, slash-separated, ends with the
// segments of seq, compared as hasSegmentsFold compares them:
// "sub/.PI" ends with ".pi", "sub/.pi/x" does not.
func endsWithSegmentsFold(p, seq string) bool {
	segments, want := strings.Split(p, "/"), strings.Split(seq, "/")
	if len(segments) < len(want) {
		return false
	}
	tail := segments[len(segments)-len(want):]
	for i, segment := range tail {
		tail[i] = win32Name(segment)
	}
	return equalSegmentsFold(tail, want)
}

// win32Name is the name Win32 opens for a path segment: it cuts the
// segment at its first ':', which starts an alternate data stream
// (`settings.json::$DATA` is the file itself, `.pi::$INDEX_ALLOCATION` the
// directory), and strips trailing dots and spaces (`extensions.` is
// `extensions`). It applies on every OS: the engine cannot tell from a path
// which OS opens it, and elsewhere it only matches a few more names.
func win32Name(segment string) string {
	name, _, _ := strings.Cut(segment, ":")
	return strings.TrimRight(name, ". ")
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
