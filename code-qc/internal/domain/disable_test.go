package domain

import (
	"slices"
	"testing"
)

func disabledIn(t *testing.T, check string, paths ...string) DisabledCheck {
	t.Helper()
	in := DisableInput{Check: check, Reason: "harness files enable errexit per test"}
	if paths != nil {
		in.Paths = paths
	}
	cfg, err := NewConfig(ConfigInput{Version: ptr(1), Languages: []string{"shell"}, Disable: []DisableInput{in}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	return cfg.Disabled[0]
}

// A disable limited to paths hides only findings about files they cover;
// a finding about no file in particular cannot be placed inside them.
func TestDisabledCheckHides(t *testing.T) {
	everywhere := disabledIn(t, "shell/strict-mode")
	limited := disabledIn(t, "shell/strict-mode", "tests/**", "scripts/run-tests.sh")
	tests := []struct {
		check DisabledCheck
		file  string
		want  bool
	}{
		{everywhere, "deploy.sh", true},
		{everywhere, "", true},
		{limited, "tests/unit/harness.sh", true},
		{limited, "scripts/run-tests.sh", true},
		{limited, "scripts/deploy.sh", false},
		{limited, "", false},
		{limited, "tests", false},
		{limited, "tests/x.sh", true},
		{limited, "../tests/x.sh", false},
		{limited, "tests/../deploy.sh", false},
		{limited, "/abs/tests/x.sh", false},
	}
	for _, tt := range tests {
		if got := tt.check.Hides(tt.file); got != tt.want {
			t.Errorf("%s Hides(%q) = %v, want %v", tt.check.Where(), tt.file, got, tt.want)
		}
	}
	if everywhere.Where() != "everywhere" || limited.Where() != "in tests/**, scripts/run-tests.sh" {
		t.Errorf("Where = %q, %q", everywhere.Where(), limited.Where())
	}
}

// config apply lists every way a disable checks less than before.
func TestDisableLoosenings(t *testing.T) {
	everywhere := disabledIn(t, "shell/strict-mode")
	tests := disabledIn(t, "shell/strict-mode", "tests/**")
	wider := disabledIn(t, "shell/strict-mode", "tests/**", "scripts/**")
	cases := []struct {
		name          string
		current, next []DisabledCheck
		want          []string
	}{
		{"newly disabled in paths", nil, []DisabledCheck{tests},
			[]string{"shell/strict-mode is newly disabled in tests/**: harness files enable errexit per test"}},
		{"a path added", []DisabledCheck{tests}, []DisabledCheck{wider},
			[]string{"shell/strict-mode is newly disabled in scripts/**: harness files enable errexit per test"}},
		{"paths dropped for everywhere", []DisabledCheck{tests}, []DisabledCheck{everywhere},
			[]string{"shell/strict-mode is now disabled everywhere, not only in tests/**: harness files enable errexit per test"}},
		{"narrowed to paths", []DisabledCheck{everywhere}, []DisabledCheck{tests}, nil},
		{"a path removed", []DisabledCheck{wider}, []DisabledCheck{tests}, nil},
		{"unchanged", []DisabledCheck{tests}, []DisabledCheck{tests}, nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := disableLoosenings(tt.current, tt.next); !slices.Equal(got, tt.want) {
				t.Fatalf("disableLoosenings = %q\nwant %q", got, tt.want)
			}
		})
	}
}

// A file glob matches whole paths only: a directory named like a matching
// file covers nothing inside it, so an agent cannot escape a check by
// naming one.
func TestDisabledFileGlobDoesNotCoverDirectories(t *testing.T) {
	tests := disabledIn(t, "structure/function-length", "**/*_test.go")
	for file, want := range map[string]bool{
		"internal/svc/handler_test.go":         true,
		"internal/svc/handler_test.go/impl.go": false,
	} {
		if got := tests.Hides(file); got != want {
			t.Errorf("Hides(%q) = %v, want %v", file, got, want)
		}
	}
}

// Hides never places a finding about a path outside the repository, which a
// tool may report, inside a disable's paths, even one any file would match.
func TestDisabledPathsStayInsideTheRepository(t *testing.T) {
	anyScript := disabledIn(t, "shell/strict-mode", "**/*.sh")
	for file, want := range map[string]bool{
		"x.sh":          true,
		"deep/dir/x.sh": true,
		"../other/x.sh": false,
		"/abs/x.sh":     false,
		"a/../x.sh":     false,
		"a\\x.sh":       false,
	} {
		if got := anyScript.Hides(file); got != want {
			t.Errorf("Hides(%q) = %v, want %v", file, got, want)
		}
	}
}
