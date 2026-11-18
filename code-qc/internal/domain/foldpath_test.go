package domain

import "testing"

func TestRelativeFold(t *testing.T) {
	tests := []struct {
		base, target string
		want         string
		ok           bool
	}{
		{"/repo", "/repo/.millennium/config.json", ".millennium/config.json", true},
		{"/repo", "/REPO/Src/A.go", "Src/A.go", true},
		{"/repo", "/repo", "", true},
		{"/", "/etc/x", "etc/x", true},
		{"C:/Users/u/repo", "c:/users/U/Repo/x", "x", true},
		{"/repo", "/repository/x", "", false},
		{"/repo/sub", "/repo", "", false},
		{"/repo", "/other/repo/x", "", false},
		{".millennium", ".Millennium/deps/x.json", "deps/x.json", true},
		{".claude/settings.json", ".claude/settings.json.bak", "", false},
	}
	for _, tt := range tests {
		got, ok := RelativeFold(tt.base, tt.target)
		if got != tt.want || ok != tt.ok {
			t.Errorf("RelativeFold(%q, %q) = %q, %v; want %q, %v", tt.base, tt.target, got, ok, tt.want, tt.ok)
		}
	}
}

func TestWithinFold(t *testing.T) {
	for _, tt := range []struct {
		p, root string
		want    bool
	}{
		{".MILLENNIUM/config.json", ".millennium", true},
		{".millennium", ".millennium", true},
		{"anything", "", true},
		{".millenniumx/config.json", ".millennium", false},
		{"docs/.millennium", ".millennium", false},
	} {
		if got := WithinFold(tt.p, tt.root); got != tt.want {
			t.Errorf("WithinFold(%q, %q) = %v, want %v", tt.p, tt.root, got, tt.want)
		}
	}
}

func TestContainsFold(t *testing.T) {
	for _, tt := range []struct {
		s, sub string
		want   bool
	}{
		{"cp x .MILLENNIUM/config.json", ".millennium", true},
		{"tee .claude/\u017fettings.json", ".claude/settings.json", true},
		{"rm -rf .Pi/Extensions", ".pi/extensions", true},
		{"cat README.md", ".millennium", false},
		{"", "", true},
	} {
		if got := ContainsFold(tt.s, tt.sub); got != tt.want {
			t.Errorf("ContainsFold(%q, %q) = %v, want %v", tt.s, tt.sub, got, tt.want)
		}
	}
}
