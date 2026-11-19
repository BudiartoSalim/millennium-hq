package domain

import (
	"strings"
	"testing"
)

// An option list allows only its own options, whole, with values where
// they take them; a word an option takes as its value is not read as an
// option, and every word after `--` is an operand. Bundles of short
// options are read only in bundledShorts, and `-<n>` is a number only in
// separateShorts.
func TestReadOnlyOptionsAllow(t *testing.T) {
	options := readOnlyOptions{flags: []string{"-a", "-b", "--all"}, attached: []string{"-u", "--color"}, valued: []string{"-e", "--glob"}}
	bundled, separate := options, options
	bundled.syntax, separate.syntax = bundledShorts, separateShorts
	tests := []struct {
		options readOnlyOptions
		args    string
		want    bool
	}{
		{bundled, "", true},
		{bundled, "x - y", true},
		{bundled, "-a --all x", true},
		{bundled, "-ab -ba -aa", true},
		{bundled, "-ae -x", true},
		{bundled, "-aex -x", false},
		{bundled, "-e", true},
		{bundled, "--glob -x --glob=-y", true},
		{bundled, "--glob= -x", false},
		{bundled, "-- -x --pre", true},
		{bundled, "-ax", false},
		{bundled, "-u", false},
		{bundled, "-3", false},
		{bundled, "--al", false},
		{bundled, "--all=x", false},
		{bundled, "--color --color=x", true},
		{bundled, "--color -x", false},
		{separate, "-a -b -u -uno --color=x -e -x -ex -3 -10", true},
		{separate, "-ab", false},
		{separate, "-ae -x", false},
		{separate, "-e -x -x", false},
		{separate, "--glob -x --all", true},
		{separate, "--glob=x -x", false},
		{separate, "-3x", false},
		{separate, "x --pre", false},
	}
	for _, tt := range tests {
		if got := tt.options.allow(strings.Fields(tt.args)); got != tt.want {
			t.Errorf("%s allow(%q) = %v, want %v", tt.options.syntax, tt.args, got, tt.want)
		}
	}
}

// with adds the options of another list in the syntax of the first.
func TestReadOnlyOptionsWith(t *testing.T) {
	base := readOnlyOptions{syntax: separateShorts, flags: []string{"-a"}, attached: []string{"-u"}, valued: []string{"-e"}}
	merged := base.with(readOnlyOptions{syntax: bundledShorts, flags: []string{"-b"}, attached: []string{"-v"}, valued: []string{"-f"}})
	if merged.syntax != separateShorts {
		t.Errorf("with: syntax %q, want %q", merged.syntax, separateShorts)
	}
	if !merged.allow([]string{"-a", "-b", "-u1", "-v2", "-e", "-x", "-f", "-y"}) {
		t.Errorf("with: the options of both lists must be allowed")
	}
	if base.allow([]string{"-b"}) {
		t.Errorf("with changed the list it was called on")
	}
}
