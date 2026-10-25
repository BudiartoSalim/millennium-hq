package usecase

import (
	"fmt"
	"slices"
	"strings"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// checkDisabled refuses disabled checks that name no rule of the running
// gates, or a rule that may not be switched off, so a typo can never leave a
// check running while the author believes it off, or a broken check off.
func checkDisabled(cfg domain.Config, gates []port.Gate) error {
	for _, d := range cfg.Disabled {
		gateName, ruleName, _ := strings.Cut(d.Check, "/")
		rule, ok := findRule(gates, gateName, ruleName)
		if !ok {
			return fmt.Errorf("invalid config: disable: %s is not a rule of any gate", d.Check)
		}
		if !rule.Disableable {
			return fmt.Errorf("invalid config: disable: %s reports a check that could not run, and cannot be switched off", d.Check)
		}
	}
	return nil
}

func findRule(gates []port.Gate, gateName, ruleName string) (port.Rule, bool) {
	for _, g := range gates {
		if g.Name() != gateName {
			continue
		}
		i := slices.IndexFunc(g.Rules(), func(r port.Rule) bool { return r.Name == ruleName })
		if i >= 0 {
			return g.Rules()[i], true
		}
	}
	return port.Rule{}, false
}

// applyDisabled moves the findings of disabled checks out of the results
// and into the report's list of disabled checks.
func applyDisabled(report *domain.Report, disabled []domain.DisabledCheck) {
	for _, d := range disabled {
		result := domain.DisabledResult{DisabledCheck: d}
		for i := range report.Results {
			var hidden int
			report.Results[i].Findings, hidden = without(report.Results[i].Findings, d.Check)
			result.Suppressed += hidden
		}
		report.Disabled = append(report.Disabled, result)
	}
}

// without drops the findings of one gate/rule and counts them.
func without(findings []domain.Finding, check string) ([]domain.Finding, int) {
	kept := findings[:0]
	for _, f := range findings {
		if f.Gate+"/"+f.Rule != check {
			kept = append(kept, f)
		}
	}
	return kept, len(findings) - len(kept)
}
