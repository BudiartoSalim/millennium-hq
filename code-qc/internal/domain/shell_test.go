package domain

import (
	"slices"
	"testing"
)

func TestStrictModeMissing(t *testing.T) {
	tests := []struct {
		name, script string
		want         []string
	}{
		{"bash with everything", "#!/usr/bin/env bash\nset -euo pipefail\n", nil},
		{"capital E is fine", "#!/bin/bash\nset -Eeuo pipefail\n", nil},
		{"long options", "#!/bin/bash\nset -o errexit -o nounset -o pipefail\n", nil},
		{"split over lines", "#!/bin/bash\n# strict\nset -e\nset -u; set -o pipefail\n", nil},
		{"bash without pipefail", "#!/bin/bash\nset -eu\n", []string{"pipefail"}},
		{"bash without -e", "#!/usr/bin/env bash\nset -uo pipefail\n", []string{"errexit"}},
		{"nothing", "#!/bin/bash\necho hi\n", []string{"errexit", "nounset", "pipefail"}},
		{"sh needs no pipefail", "#!/bin/sh\nset -eu\n", nil},
		{"sourced library without shebang is exempt", "# shellcheck shell=bash\nhelper() { :; }\n", nil},
		{"set +e does not count", "#!/bin/sh\nset +e -u\n", []string{"errexit"}},
		{"too late in the file", "#!/bin/sh\n" + repeatLine("echo work\n", 45) + "set -eu\n", []string{"errexit", "nounset"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StrictModeMissing([]byte(tt.script)); !slices.Equal(got, tt.want) {
				t.Fatalf("missing = %q, want %q", got, tt.want)
			}
		})
	}
}

func repeatLine(line string, n int) string {
	out := ""
	for range n {
		out += line
	}
	return out
}
