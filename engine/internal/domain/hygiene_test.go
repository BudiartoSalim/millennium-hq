package domain

import (
	"strings"
	"testing"
)

func comments(lines ...string) []Comment {
	out := make([]Comment, 0, len(lines))
	for i, text := range lines {
		if text != "" {
			out = append(out, Comment{Line: i + 1, Text: text})
		}
	}
	return out
}

func TestSuppressionFindings(t *testing.T) {
	tests := []struct {
		name     string
		comments []Comment
		want     int
	}{
		{"eslint with reason", comments("// eslint-disable-next-line no-console -- the CLI prints to stdout"), 0},
		{"eslint without reason", comments("// eslint-disable-next-line no-console"), 1},
		{"nolint with trailing comment", comments("//nolint:gosec // the hash is a cache key, not a secret"), 0},
		{"nolint bare", comments("//nolint:gosec"), 1},
		{"ts-expect-error with reason", comments("// @ts-expect-error -- upstream types miss the overload"), 0},
		{"ts-ignore bare", comments("// @ts-ignore"), 1},
		{"reason too short", comments("// @ts-ignore -- temp"), 1},
		{"type ignore with reason", comments("# type: ignore[attr-defined]  # stub package lacks this attribute"), 0},
		{"noqa bare", comments("# noqa: E501"), 1},
		{"shellcheck with reason", comments("# shellcheck disable=SC2016 # printed for the user to copy"), 0},
		{"reason on the line above", comments("// The SDK returns any for this callback.", "// eslint-disable-next-line @typescript-eslint/no-explicit-any"), 0},
		{"directive above is not a reason", comments("// eslint-disable-next-line a", "// eslint-disable-next-line b"), 2},
		{"ordinary comment", comments("// nothing to see here"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SuppressionFindings("a.ts", tt.comments, AllLines()); len(got) != tt.want {
				t.Fatalf("findings = %+v, want %d", got, tt.want)
			}
		})
	}
	untouched := LineSet{Ranges: []LineRange{{First: 5, Last: 5}}}
	if got := SuppressionFindings("a.ts", comments("// @ts-ignore"), untouched); len(got) != 0 {
		t.Fatalf("an untouched line is not checked, got %+v", got)
	}
}

func TestTodoFindings(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{"// TODO: handle retries", 1},
		{"// TODO(#42): handle retries", 0},
		{"# FIXME see PROJ-17", 0},
		{"// XXX https://github.com/org/repo/issues/9", 0},
		{"// HACK until the API exists", 1},
		{"// todos are fine in lower case prose", 0},
		{"// TODO(alice): ask", 1},
		{"// TodoFindings reports TODO-style markers in prose", 0},
		{"// it lists TODO items found elsewhere", 0},
		{"# TODO", 1},
		{"/* FIXME broken */", 1},
	}
	for _, tt := range tests {
		if got := TodoFindings("a.go", comments(tt.text), AllLines()); len(got) != tt.want {
			t.Errorf("%q: findings = %+v, want %d", tt.text, got, tt.want)
		}
	}
}

func TestWaiversAndConstructs(t *testing.T) {
	content := []byte("a\n// millennium:allow banned-construct -- the test server uses a self-signed certificate\nb\nc // millennium:allow banned-construct -- short\n")
	waivers := ReadWaivers(content)
	constructs := []Construct{
		{Kind: ConstructTLSVerifyOff, Line: 3, Detail: "InsecureSkipVerify: true"},
		{Kind: ConstructDynamicCode, Line: 4, Detail: "eval"},
		{Kind: ConstructShellCommand, Line: 9, Detail: "shell=True"},
	}
	findings := ConstructFindings("a.go", constructs, LineSet{Ranges: []LineRange{{First: 1, Last: 5}}}, waivers)
	if len(findings) != 1 || findings[0].Line != 4 || !strings.Contains(findings[0].Message, "dynamic-code (eval)") {
		t.Fatalf("findings = %+v; want only line 4 (its waiver reason is too short; line 9 is untouched)", findings)
	}
	if waivers.Covers("secret", 3) {
		t.Fatal("a waiver covers only the rule it names")
	}
}

func TestSecretFindings(t *testing.T) {
	tests := []struct {
		name, line string
		want       bool
	}{
		{"aws key", `key = "AKIAIOSFODNN7EXAMPL3"`, true},                                          // millennium:allow secret -- fake credential, test input for the secret scanner
		{"private key", "-----BEGIN OPENSSH PRIVATE KEY-----", true},                               // millennium:allow secret -- fake credential, test input for the secret scanner
		{"github token", "token: ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789", true},                  // millennium:allow secret -- fake credential, test input for the secret scanner
		{"anthropic key", `ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz012345`, true}, // millennium:allow secret -- fake credential, test input for the secret scanner
		{"random assigned secret", `password = "q8Zr!t2vLx9#Pw4m"`, true},                          // millennium:allow secret -- fake credential, test input for the secret scanner
		{"placeholder", `password = "changeme-please-now"`, false},
		{"env reference", `api_key: "${API_KEY_FROM_ENV}"`, false},
		{"low-entropy word", `secret = "aaaaaaaaaaaaaaaa"`, false},
		{"ordinary code", `const tokenCount = countTokens(text)`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SecretFindings("cfg", []byte(tt.line), AllLines(), Waivers{})
			if (len(got) == 1) != tt.want {
				t.Fatalf("findings = %+v, want secret %v", got, tt.want)
			}
		})
	}
	waived := []byte("# millennium:allow secret -- documented test key from the AWS docs\nkey = \"AKIAIOSFODNN7EXAMPL3\"\n")
	if got := SecretFindings("cfg", waived, AllLines(), ReadWaivers(waived)); len(got) != 0 {
		t.Fatalf("a waived secret is not reported, got %+v", got)
	}
}

func TestIsBinary(t *testing.T) {
	if IsBinary([]byte("plain text")) || !IsBinary([]byte("PNG\x00\x01")) {
		t.Fatal("IsBinary misclassified content")
	}
}
