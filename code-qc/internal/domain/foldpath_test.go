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

func TestHasSegmentsFold(t *testing.T) {
	for _, tt := range []struct {
		p, seq string
		want   bool
	}{
		{"/tmp/x/moved/.millennium/config.json", ".millennium", true},
		{".millennium", ".millennium", true},
		{"/home/u/.CLAUDE/Settings.json", ".claude/settings.json", true},
		{"C:/work/.pi/extensions", ".pi/extensions", true},
		{"/repo/.claude/\u017fettings.json", ".claude/settings.json", true},
		{"/repo/.millenniumx/config.json", ".millennium", false},
		{"/repo/x.millennium/config.json", ".millennium", false},
		{"/repo/.claude/x/settings.json", ".claude/settings.json", false},
		{"/repo/.claude", ".claude/settings.json", false},
		{"", ".millennium", false},
		{"C:/repo/.claude/settings.local.json.", ".claude/settings.local.json", true},
		{"C:/repo/.pi/extensions./x.ts", ".pi/extensions", true},
		{"C:/repo/.claude/settings.json::$DATA", ".claude/settings.json", true},
		{"C:/repo/.pi::$INDEX_ALLOCATION/settings.json", ".pi/settings.json", true},
		{"C:/repo/.millennium . ./config.json", ".millennium", true},
		{"C:/repo/.claude/settings.json.bak", ".claude/settings.json", false},
		{"C:/repo/.claude/settings:json", ".claude/settings.json", false},
	} {
		if got := hasSegmentsFold(tt.p, tt.seq); got != tt.want {
			t.Errorf("hasSegmentsFold(%q, %q) = %v, want %v", tt.p, tt.seq, got, tt.want)
		}
	}
}

// A target below where a symlink leads is named below the symlink, in any
// spelling that reaches it.
func TestRebaseFold(t *testing.T) {
	tests := []struct {
		target, from, to string
		want             string
		ok               bool
	}{
		{"/repo/conf/extensions/evil.ts", "/repo/conf/extensions", "/repo/.pi/extensions", "/repo/.pi/extensions/evil.ts", true},
		{"/repo/conf/extensions", "/repo/conf/extensions", "/repo/.pi/extensions", "/repo/.pi/extensions", true},
		{"/REPO/Conf/Extensions/A/b.ts", "/repo/conf/extensions", "/repo/.pi/extensions", "/repo/.pi/extensions/A/b.ts", true},
		{"C:/repo/conf/extensions./x.ts", "C:/repo/conf/extensions", "C:/repo/.pi/extensions", "C:/repo/.pi/extensions/x.ts", true},
		{"C:/repo/conf.json::$DATA", "C:/repo/conf.json", "C:/repo/.claude/settings.json", "C:/repo/.claude/settings.json", true},
		{"/etc/passwd", "/", "/repo/.millennium", "/repo/.millennium/etc/passwd", true},
		{"/repo/conf/extensionsx/x.ts", "/repo/conf/extensions", "/repo/.pi/extensions", "", false},
		{"/repo/conf", "/repo/conf/extensions", "/repo/.pi/extensions", "", false},
		{"/other/conf/extensions/x.ts", "/repo/conf/extensions", "/repo/.pi/extensions", "", false},
	}
	for _, tt := range tests {
		got, ok := RebaseFold(tt.target, tt.from, tt.to)
		if got != tt.want || ok != tt.ok {
			t.Errorf("RebaseFold(%q, %q, %q) = %q, %v; want %q, %v", tt.target, tt.from, tt.to, got, ok, tt.want, tt.ok)
		}
	}
}

// Win32 opens `name:stream` as a stream of name and strips trailing dots
// and spaces, so those spellings name the protected files on Windows.
func TestWin32Name(t *testing.T) {
	for segment, want := range map[string]string{
		"settings.json":          "settings.json",
		"settings.json.":         "settings.json",
		"settings.json .. ":      "settings.json",
		"settings.json::$DATA":   "settings.json",
		"settings.json:x:$DATA":  "settings.json",
		"settings.json. :stream": "settings.json",
		".millennium":            ".millennium",
		".pi.":                   ".pi",
		"..":                     "",
		"":                       "",
	} {
		if got := win32Name(segment); got != want {
			t.Errorf("win32Name(%q) = %q, want %q", segment, got, want)
		}
	}
}

func TestMatchSegmentFold(t *testing.T) {
	for _, tt := range []struct {
		pattern, name string
		want          bool
	}{
		{"api", "API", true},
		{"svc-*", "SVC-Billing", true},
		{"?pi", "Api", true},
		{"services", "\u017fervices", true},
		{"api", "apis", false},
		{"svc-*", "web", false},
	} {
		if got := matchSegmentFold(tt.pattern, tt.name); got != tt.want {
			t.Errorf("matchSegmentFold(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
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
