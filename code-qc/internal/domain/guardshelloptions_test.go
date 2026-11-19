package domain

import (
	"strings"
	"testing"
)

// An option list allows only its own options, whole, with values where
// they take them; a word an option takes as its value is not read as an
// option, but one that starts with a dash is not read-only (R10-CR-1);
// an option whose value must be attached is not read-only without it;
// and every word after `--` is an operand. Bundles of short options are
// read only in bundledShorts, and `-<n>` is a number only in
// separateShorts.
func TestReadOnlyOptionsAllow(t *testing.T) {
	options := readOnlyOptions{
		flags:         []string{"-a", "-b", "--all"},
		attached:      []string{"-u", "--color"},
		attachedValue: []string{"-n", "--since"},
		valued:        []string{"-e", "--glob"},
	}
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
		{bundled, "-ae x -ex", true},
		{bundled, "-ae -x", false},
		{bundled, "-ae -", false},
		{bundled, "-aex -x", false},
		{bundled, "-e", true},
		{bundled, "--glob x --glob=-y", true},
		{bundled, "--glob -x", false},
		{bundled, "--glob --all", false},
		{bundled, "--glob= -x", false},
		{bundled, "-- -x --pre", true},
		{bundled, "-ax", false},
		{bundled, "-u", false},
		{bundled, "-n5", false},
		{bundled, "-3", false},
		{bundled, "--al", false},
		{bundled, "--all=x", false},
		{bundled, "--color --color=x", true},
		{bundled, "--color -x", false},
		{bundled, "--since=x --since=-y", true},
		{bundled, "--since x", false},
		{separate, "-a -b -u -uno --color=x -n5 -n-1 -3 -10 --since=x --since=", true},
		{separate, "-n 5", false},
		{separate, "-n", false},
		{separate, "--since x", false},
		{separate, "--since", false},
		{separate, "--since --all", false},
		{separate, "-ab", false},
		{separate, "-e x", false},
		{separate, "-ex", false},
		{separate, "--glob x --all", true},
		{separate, "--glob -x", false},
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
	base := readOnlyOptions{
		syntax: separateShorts, flags: []string{"-a"}, attached: []string{"-u"}, attachedValue: []string{"-n"}, valued: []string{"--e"},
	}
	merged := base.with(readOnlyOptions{
		syntax: bundledShorts, flags: []string{"-b"}, attached: []string{"-v"}, attachedValue: []string{"-m"}, valued: []string{"--f"},
	})
	if merged.syntax != separateShorts {
		t.Errorf("with: syntax %q, want %q", merged.syntax, separateShorts)
	}
	if !merged.allow([]string{"-a", "-b", "-u1", "-v2", "-n3", "-m4", "--e", "x", "--f", "y"}) {
		t.Errorf("with: the options of both lists must be allowed")
	}
	for _, more := range []string{"-b", "-v", "-m4", "--f"} {
		if base.allow([]string{more}) {
			t.Errorf("with changed the list it was called on: %s allowed", more)
		}
	}
}
