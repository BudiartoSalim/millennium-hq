package domain

import "testing"

func TestReportPassed(t *testing.T) {
	clean := GateResult{Gate: "a"}
	dirty := GateResult{Gate: "b", Findings: []Finding{{Gate: "b", Rule: "r", Message: "m"}}}
	tests := []struct {
		name   string
		report Report
		want   bool
	}{
		{"not enabled", Report{}, true},
		{"enabled, every gate clean", Report{Enabled: true, Results: []GateResult{clean}}, true},
		{"enabled, a gate has findings", Report{Enabled: true, Results: []GateResult{clean, dirty}}, false},
		{"enabled, no gate ran", Report{Enabled: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.report.Passed(); got != tt.want {
				t.Fatalf("Passed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReportFindings(t *testing.T) {
	report := Report{Enabled: true, Results: []GateResult{
		{Gate: "a", Findings: []Finding{{Rule: "1"}, {Rule: "2"}}},
		{Gate: "b", Findings: []Finding{{Rule: "3"}}},
	}}
	if got := report.FindingCount(); got != 3 {
		t.Fatalf("FindingCount() = %d, want 3", got)
	}
	findings := report.Findings()
	if len(findings) != 3 || findings[0].Rule != "1" || findings[2].Rule != "3" {
		t.Fatalf("Findings() = %+v, want gate order", findings)
	}
}
