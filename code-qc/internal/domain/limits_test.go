package domain

import (
	"strings"
	"testing"
)

func fn(key string, lines, nesting, cognitive int) FunctionFacts {
	return FunctionFacts{Key: key, Line: 10, Lines: lines, Nesting: nesting, Cognitive: cognitive}
}

func TestLimitsNewCodeMustMeetEveryLimit(t *testing.T) {
	file := FileFacts{Path: "a.go", Functions: []FunctionFacts{
		fn("ok", 50, 3, 15),
		fn("long", 51, 1, 1),
		fn("deep", 10, 4, 1),
		fn("complex", 10, 1, 16),
	}}
	findings := DefaultLimits().Check(file, nil)
	want := []string{"function-length", "nesting-depth", "cognitive-complexity"}
	if len(findings) != len(want) {
		t.Fatalf("findings = %+v", findings)
	}
	for i, rule := range want {
		if findings[i].Rule != rule || findings[i].File != "a.go" || findings[i].Line != 10 {
			t.Errorf("finding %d = %+v, want rule %s", i, findings[i], rule)
		}
	}
	if !strings.Contains(findings[0].Message, "long has a length in lines of 51; the limit is 50") {
		t.Errorf("message = %q", findings[0].Message)
	}
}

func TestLimitsExistingCodeMayNotGetWorse(t *testing.T) {
	base := &FileFacts{Path: "a.go", Functions: []FunctionFacts{
		fn("legacy", 120, 5, 30),
		fn("fine", 20, 1, 2),
	}}
	tests := []struct {
		name    string
		changed FunctionFacts
		want    string
	}{
		{"legacy unchanged", fn("legacy", 120, 5, 30), ""},
		{"legacy improved", fn("legacy", 90, 4, 20), ""},
		{"legacy got longer", fn("legacy", 121, 5, 30), "went from a length in lines of 120 to 121; it was already over the limit of 50"},
		{"fine pushed over the limit", fn("fine", 20, 1, 16), "went from a cognitive complexity of 2 to 16; the limit is 15"},
		{"new function next to legacy", fn("brandnew", 60, 1, 1), "brandnew has a length in lines of 60"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := DefaultLimits().Check(FileFacts{Path: "a.go", Functions: []FunctionFacts{tt.changed}}, base)
			switch {
			case tt.want == "" && len(findings) > 0:
				t.Fatalf("want no findings, got %+v", findings)
			case tt.want != "" && (len(findings) != 1 || !strings.Contains(findings[0].Message, tt.want)):
				t.Fatalf("want one finding containing %q, got %+v", tt.want, findings)
			}
		})
	}
}

func TestLimitsExemptions(t *testing.T) {
	long := fn("TestTable", 200, 2, 3)
	if f := DefaultLimits().Check(FileFacts{Test: true, Functions: []FunctionFacts{long}}, nil); len(f) != 0 {
		t.Fatalf("test code is exempt from the length limit, got %+v", f)
	}
	deep := fn("TestDeep", 10, 5, 3)
	if f := DefaultLimits().Check(FileFacts{Test: true, Functions: []FunctionFacts{deep}}, nil); len(f) != 1 || f[0].Rule != "nesting-depth" {
		t.Fatalf("test code still gets the nesting limit, got %+v", f)
	}
	if f := DefaultLimits().Check(FileFacts{Generated: true, Functions: []FunctionFacts{fn("gen", 900, 9, 99)}}, nil); len(f) != 0 {
		t.Fatalf("generated code is skipped, got %+v", f)
	}
}
