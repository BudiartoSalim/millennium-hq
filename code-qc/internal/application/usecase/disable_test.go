package usecase

import (
	"strings"
	"testing"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

func disabledConfig(t *testing.T, check string) domain.Config {
	t.Helper()
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"go"},
		Disable: []domain.DisabledCheck{{Check: check, Reason: "the project accepts this deliberately"}}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	return cfg
}

func TestDisabledChecksHideTheirFindingsVisibly(t *testing.T) {
	repo := &fakeRepo{}
	gate := &fakeGate{name: "g", findings: []domain.Finding{{Gate: "g", Rule: "r"}, {Gate: "g", Rule: "r"}}}
	uc := NewRunGates(NewLoadConfig(fakeSource{cfg: disabledConfig(t, "g/r")}, repo), repo, []port.Gate{gate})
	report, err := uc.Execute(t.Context(), RunGatesInput{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !report.Passed() || len(report.Disabled) != 1 || report.Disabled[0].Suppressed != 2 || report.Disabled[0].Check != "g/r" {
		t.Fatalf("report = %+v", report)
	}
}

func TestDisableNamesAreChecked(t *testing.T) {
	tests := []struct {
		check, want string
	}{
		{"g/nope", "not a rule of any gate"},
		{"other/r", "not a rule of any gate"},
		{"g/broken", "cannot be switched off"},
	}
	for _, tt := range tests {
		repo := &fakeRepo{}
		uc := NewRunGates(NewLoadConfig(fakeSource{cfg: disabledConfig(t, tt.check)}, repo), repo, []port.Gate{&fakeGate{name: "g"}})
		if _, err := uc.Execute(t.Context(), RunGatesInput{}); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.check, err, tt.want)
		}
	}
}

func TestUndeclaredRulesAreAnEngineBug(t *testing.T) {
	repo := &fakeRepo{}
	gate := &fakeGate{name: "g", findings: []domain.Finding{{Gate: "g", Rule: "made-up"}}}
	report, err := NewRunGates(NewLoadConfig(enabledSource(), repo), repo, []port.Gate{gate}).Execute(t.Context(), RunGatesInput{})
	if err != nil || report.Passed() || !strings.Contains(report.Findings()[0].Message, `undeclared rule "made-up"`) {
		t.Fatalf("report = %+v, %v", report, err)
	}
}

func TestDisablingOneRuleKeepsTheOthers(t *testing.T) {
	repo := &fakeRepo{}
	gate := &fakeGate{name: "g", findings: []domain.Finding{{Gate: "g", Rule: "r"}, {Gate: "g", Rule: "broken"}}}
	uc := NewRunGates(NewLoadConfig(fakeSource{cfg: disabledConfig(t, "g/r")}, repo), repo, []port.Gate{gate})
	report, err := uc.Execute(t.Context(), RunGatesInput{})
	if err != nil || report.Passed() || report.FindingCount() != 1 || report.Findings()[0].Rule != "broken" || report.Disabled[0].Suppressed != 1 {
		t.Fatalf("report = %+v, %v", report, err)
	}
}
