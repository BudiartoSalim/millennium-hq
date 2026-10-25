package domain

import "testing"

func mustPattern(t *testing.T, raw string, use PatternUse) Pattern {
	t.Helper()
	p, err := ParsePattern(raw, use)
	if err != nil {
		t.Fatalf("ParsePattern(%q): %v", raw, err)
	}
	return p
}

func TestParsePatternRejects(t *testing.T) {
	tests := []struct {
		raw string
		use PatternUse
	}{
		{"", PrefixPattern},
		{"/abs", PrefixPattern},
		{"a/../b", PrefixPattern},
		{"a/./b", PrefixPattern},
		{"a//b", PrefixPattern},
		{"a/", PrefixPattern},
		{"**/dist", PrefixPattern},
		{"a/x**", RecursivePattern},
		{"a/[ab]", PrefixPattern},
		{`a\b`, PrefixPattern},
	}
	for _, tt := range tests {
		if _, err := ParsePattern(tt.raw, tt.use); err == nil {
			t.Errorf("ParsePattern(%q) accepted", tt.raw)
		}
	}
	for _, raw := range []string{".", "services/*/api", "**/node_modules/**", "@scope/pkg+x", "with space"} {
		mustPattern(t, raw, RecursivePattern)
	}
}

func TestPatternMatchPrefix(t *testing.T) {
	tests := []struct {
		pattern, target, root string
		ok                    bool
	}{
		{".", "a/b.go", "", true},
		{"services/*/api", "services/game/api/main.go", "services/game/api", true},
		{"services/*/api", "services/game/api", "services/game/api", true},
		{"services/*/api", "services/game/web/x.ts", "", false},
		{"services/*/api", "services/api", "", false},
		{"internal/domain", "internal/domain/user.go", "internal/domain", true},
		{"internal/domain", "internal/domainx/user.go", "", false},
		{"*/domain.py", "tts/domain.py", "tts/domain.py", true},
		{"main.go", "main.go", "main.go", true},
	}
	for _, tt := range tests {
		root, ok := mustPattern(t, tt.pattern, PrefixPattern).MatchPrefix(tt.target)
		if ok != tt.ok || root != tt.root {
			t.Errorf("%q.MatchPrefix(%q) = %q, %v; want %q, %v", tt.pattern, tt.target, root, ok, tt.root, tt.ok)
		}
	}
}

func TestPatternCovers(t *testing.T) {
	tests := []struct {
		pattern, target string
		want            bool
	}{
		{"dist", "dist/app.js", true},
		{"dist", "src/dist.go", false},
		{"**/node_modules/**", "web/node_modules/x/index.js", true},
		{"**/node_modules", "node_modules/x.js", true},
		{"**/*.pb.go", "api/gen/user.pb.go", true},
		{"**/*.pb.go", "api/gen/user.go", false},
		{"vendor/**", "vendor/a/b.go", true},
	}
	for _, tt := range tests {
		if got := mustPattern(t, tt.pattern, RecursivePattern).Covers(tt.target); got != tt.want {
			t.Errorf("%q.Covers(%q) = %v, want %v", tt.pattern, tt.target, got, tt.want)
		}
	}
}

func TestPatternSpecificity(t *testing.T) {
	p := mustPattern(t, "services/*/api", PrefixPattern)
	if p.Depth() != 3 || p.Literals() != 2 {
		t.Fatalf("depth %d literals %d, want 3 and 2", p.Depth(), p.Literals())
	}
	if root := RootPattern(); root.Depth() != 0 || root.String() != "." {
		t.Fatalf("root pattern = %+v", root)
	}
}

func TestWithinAndRelative(t *testing.T) {
	if !Within("a/b/c.go", "a/b") || Within("a/bc/d.go", "a/b") || !Within("x.go", "") {
		t.Fatal("Within misclassified a path")
	}
	if got := Relative("a/b/c.go", "a/b"); got != "c.go" {
		t.Fatalf("Relative = %q", got)
	}
	if got := Relative("c.go", ""); got != "c.go" {
		t.Fatalf("Relative with empty root = %q", got)
	}
}
