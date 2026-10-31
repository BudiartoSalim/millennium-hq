package domain

import "slices"

// LineRange is an inclusive range of line numbers.
type LineRange struct {
	First int
	Last  int
}

// LineSet is the lines of a file a change touched. All means every line,
// for a new file or an audit.
type LineSet struct {
	All    bool
	Ranges []LineRange
}

// LintIssue is one problem an external checker reported. File is the
// repository-relative path when the checker reports on several files, and
// empty when it ran on one.
type LintIssue struct {
	File    string
	Line    int
	Code    string
	Message string
}

// AllLines is the set holding every line.
func AllLines() LineSet { return LineSet{All: true} }

// Contains reports whether line is in the set.
func (s LineSet) Contains(line int) bool {
	if s.All {
		return true
	}
	return slices.ContainsFunc(s.Ranges, func(r LineRange) bool { return line >= r.First && line <= r.Last })
}
