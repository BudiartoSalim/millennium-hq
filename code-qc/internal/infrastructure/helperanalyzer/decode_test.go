package helperanalyzer

import (
	"slices"
	"strings"
	"testing"
)

var validDigest = strings.Repeat("0123456789abcdef", 4)

func answer(runtime string) []byte {
	return []byte(`{"facts":{"generated":false,"test":false,"imports":[],"functions":[],"comments":[],"constructs":[]` + runtime + "}}\n")
}

func TestDecodeRuntimeFacts(t *testing.T) {
	const threeLines = "a\nb\nc\n"
	tests := []struct {
		name, content, runtime string
		known                  bool
		holds                  []int
	}{
		{"answer without the fields", threeLines, ``, false, nil},
		{"null facts", threeLines, `,"runtime":null`, false, nil},
		{"known facts", threeLines, `,"runtime":{"lines":[3,1],"digest":"` + validDigest + `"}`, true, []int{1, 3}},
		{"known without runtime lines", threeLines, `,"runtime":{"lines":[],"digest":"` + validDigest + `"}`, true, nil},
		{"last line without a newline", "a\nb", `,"runtime":{"lines":[2],"digest":"` + validDigest + `"}`, true, []int{2}},
		{"CRLF numbers like git", "a\r\nb\r\n", `,"runtime":{"lines":[2],"digest":"` + validDigest + `"}`, true, []int{2}},
		// The helper numbers these lines differently from git, or saw
		// replaced bytes: whatever it says, the facts are unknown.
		{"lone CR", "a\rb\n", `,"runtime":{"lines":[2],"digest":"` + validDigest + `"}`, false, nil},
		{"lone CR at the end", "a\r", `,"runtime":{"lines":[1],"digest":"` + validDigest + `"}`, false, nil},
		{"line separator", "a\u2028b\n", `,"runtime":{"lines":[1],"digest":"` + validDigest + `"}`, false, nil},
		{"paragraph separator", "a\u2029b\n", `,"runtime":{"lines":[1],"digest":"` + validDigest + `"}`, false, nil},
		{"not UTF-8", "a\xff\n", `,"runtime":{"lines":[1],"digest":"` + validDigest + `"}`, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facts, err := decode("a.ts", []byte(tt.content), answer(tt.runtime))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if facts.Runtime.Known() != tt.known {
				t.Fatalf("known = %v, want %v", facts.Runtime.Known(), tt.known)
			}
			if got := heldLines(tt.content, facts.Runtime); tt.known && !slices.Equal(got, tt.holds) {
				t.Fatalf("runtime lines = %v, want %v", got, tt.holds)
			}
		})
	}
}

func TestDecodeRejectsMalformedRuntimeFacts(t *testing.T) {
	const content = "a\nb\nc\n"
	tests := []struct {
		name, runtime, want string
	}{
		{"missing digest", `,"runtime":{"lines":[1]}`, "not 64 lowercase hex"},
		{"short digest", `,"runtime":{"lines":[1],"digest":"` + validDigest[1:] + `"}`, "not 64 lowercase hex"},
		{"uppercase digest", `,"runtime":{"lines":[1],"digest":"` + strings.ToUpper(validDigest) + `"}`, "not 64 lowercase hex"},
		{"non-hex digest", `,"runtime":{"lines":[1],"digest":"` + strings.Repeat("z", 64) + `"}`, "not 64 lowercase hex"},
		{"line zero", `,"runtime":{"lines":[0],"digest":"` + validDigest + `"}`, "line 0 is outside"},
		{"negative line", `,"runtime":{"lines":[-2],"digest":"` + validDigest + `"}`, "line -2 is outside"},
		{"line past the end", `,"runtime":{"lines":[4],"digest":"` + validDigest + `"}`, "line 4 is outside the file's 3 lines"},
		{"fractional line", `,"runtime":{"lines":[1.5],"digest":"` + validDigest + `"}`, "unreadable analyzer answer"},
		{"line as a string", `,"runtime":{"lines":["1"],"digest":"` + validDigest + `"}`, "unreadable analyzer answer"},
		{"unknown runtime field", `,"runtime":{"lines":[],"digest":"` + validDigest + `","extra":1}`, "unreadable analyzer answer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decode("a.ts", []byte(content), answer(tt.runtime))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}
