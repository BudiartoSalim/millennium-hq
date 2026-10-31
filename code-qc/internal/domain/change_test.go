package domain

import "testing"

func TestLanguageOf(t *testing.T) {
	tests := []struct {
		path string
		want Language
		ok   bool
	}{
		{"cmd/main.go", LanguageGo, true},
		{"src/App.tsx", LanguageTypeScript, true},
		{"src/types.d.ts", LanguageTypeScript, true},
		{"lib/mod.MTS", LanguageTypeScript, true},
		{"app/service.py", LanguagePython, true},
		{"README.md", "", false},
		{"web/app.js", "", false},
		{"Makefile", "", false},
		{"dir.go/file", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := LanguageOf(tt.path)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("LanguageOf(%q) = %q, %v; want %q, %v", tt.path, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestIsRepoRelative(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"a/b/c.txt", true},
		{"with space/ünïcode.md", true},
		{".github/workflows/ci.yml", true},
		{"", false},
		{"/etc/passwd", false},
		{"../outside", false},
		{"a/../b", false},
		{"./a", false},
		{"a//b", false},
		{`a\b`, false},
		{"a\x00b", false},
		{"a/", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsRepoRelative(tt.path); got != tt.want {
				t.Fatalf("IsRepoRelative(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path, head string
		want       Language
		ok         bool
	}{
		{"scripts/install.sh", "", LanguageShell, true},
		{"lib.bash", "", LanguageShell, true},
		{"bin/deploy", "#!/usr/bin/env bash\nset -e\n", LanguageShell, true},
		{"bin/run", "#!/bin/sh\n", LanguageShell, true},
		{"bin/tool", "#!/usr/bin/env -S bash -e\n", LanguageShell, true},
		{"bin/script", "#!/usr/bin/env python3\n", "", false},
		{"bin/data", "hello\n", "", false},
		{"notes.txt", "#!/bin/bash\n", "", false},
		{"main.go", "", LanguageGo, true},
	}
	for _, tt := range tests {
		got, ok := DetectLanguage(tt.path, []byte(tt.head))
		if got != tt.want || ok != tt.ok {
			t.Errorf("DetectLanguage(%q) = %q, %v; want %q, %v", tt.path, got, ok, tt.want, tt.ok)
		}
	}
}
