package domain

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Pattern is a slash-separated path pattern from the config or a pack. Each
// segment is matched with path.Match, so '*' and '?' stay within one segment.
// "**", accepted only where a caller allows it, matches any number of
// segments. The single pattern "." stands for the repository root.
type Pattern struct {
	raw      string
	segments []string
}

// patternSegment allowlists the characters of one segment. '[' and '\' are
// left out, so path.Match can never report a malformed pattern.
var patternSegment = regexp.MustCompile(`^[A-Za-z0-9._*?@+ -]+$`)

// PatternUse says whether a pattern may contain "**".
type PatternUse int

// Pattern uses.
const (
	// PrefixPattern names a directory or file by a fixed number of segments:
	// component roots and layer paths. Specificity depends on that number.
	PrefixPattern PatternUse = iota
	// RecursivePattern may contain "**": exclusions.
	RecursivePattern
)

// RootPattern matches the repository root.
func RootPattern() Pattern { return Pattern{raw: "."} }

// ParsePattern validates raw for the given use.
func ParsePattern(raw string, use PatternUse) (Pattern, error) {
	if raw == "." {
		return RootPattern(), nil
	}
	if raw == "" || strings.HasPrefix(raw, "/") {
		return Pattern{}, fmt.Errorf("pattern %q must be a relative path", raw)
	}
	segments := strings.Split(raw, "/")
	for _, segment := range segments {
		if err := checkSegment(segment, use); err != nil {
			return Pattern{}, fmt.Errorf("pattern %q: %w", raw, err)
		}
	}
	return Pattern{raw: raw, segments: segments}, nil
}

func checkSegment(segment string, use PatternUse) error {
	switch {
	case segment == "**" && use == RecursivePattern:
		return nil
	case segment == "**":
		return errors.New(`"**" is not allowed here`)
	case segment == "" || segment == "." || segment == "..":
		return errors.New(`empty, "." and ".." segments are not allowed`)
	case strings.Contains(segment, "**"):
		return errors.New(`"**" must be a whole segment`)
	case !patternSegment.MatchString(segment):
		return fmt.Errorf("segment %q may only use letters, digits, spaces and . _ - @ + * ?", segment)
	}
	return nil
}

// String returns the pattern as written.
func (p Pattern) String() string { return p.raw }

// Depth is the number of segments a prefix pattern spans.
func (p Pattern) Depth() int { return len(p.segments) }

// Literals counts the segments without wildcards; among patterns of equal
// depth, more literal segments are more specific.
func (p Pattern) Literals() int {
	count := 0
	for _, segment := range p.segments {
		if !strings.ContainsAny(segment, "*?") {
			count++
		}
	}
	return count
}

// MatchPrefix matches the pattern against the leading segments of p and
// returns the matched prefix. The root pattern matches every path with an
// empty prefix.
func (p Pattern) MatchPrefix(target string) (string, bool) {
	segments := splitPath(target)
	if len(p.segments) > len(segments) {
		return "", false
	}
	for i, segment := range p.segments {
		if ok, _ := path.Match(segment, segments[i]); !ok {
			return "", false
		}
	}
	return strings.Join(segments[:len(p.segments)], "/"), true
}

// Covers reports whether the pattern matches target or any directory that
// contains it, so excluding "dist" also excludes "dist/app.js".
func (p Pattern) Covers(target string) bool {
	segments := splitPath(target)
	for end := 1; end <= len(segments); end++ {
		if matchSegments(p.segments, segments[:end]) {
			return true
		}
	}
	return false
}

func matchSegments(pattern, segments []string) bool {
	if len(pattern) == 0 {
		return len(segments) == 0
	}
	if pattern[0] == "**" {
		for skip := 0; skip <= len(segments); skip++ {
			if matchSegments(pattern[1:], segments[skip:]) {
				return true
			}
		}
		return false
	}
	if len(segments) == 0 {
		return false
	}
	ok, _ := path.Match(pattern[0], segments[0])
	return ok && matchSegments(pattern[1:], segments[1:])
}

func splitPath(p string) []string {
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// Within reports whether p is root itself or lies inside it. The empty root
// contains every path.
func Within(p, root string) bool {
	return root == "" || p == root || strings.HasPrefix(p, root+"/")
}

// Relative returns p relative to root; callers check Within first.
func Relative(p, root string) string {
	if root == "" {
		return p
	}
	return strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
}
