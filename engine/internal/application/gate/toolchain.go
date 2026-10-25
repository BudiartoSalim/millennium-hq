// Package gate holds the checks that decide whether a change may pass. A gate
// only reports findings; the run-gates use-case turns a gate's errors and
// panics into blocking findings, so no gate can fail open.
package gate

import (
	"context"
	"fmt"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// Toolchain blocks when an executable the configured checks need is missing.
// It runs for every configured language, changed or not, so a missing tool
// surfaces before the change that needs it.
type Toolchain struct {
	tools port.ToolLocator
}

// NewToolchain returns the toolchain gate.
func NewToolchain(tools port.ToolLocator) *Toolchain {
	return &Toolchain{tools: tools}
}

// Name identifies the gate in findings.
func (*Toolchain) Name() string { return "toolchain" }

// Run reports one finding per missing tool.
func (g *Toolchain) Run(_ context.Context, in port.GateInput) ([]domain.Finding, error) {
	var findings []domain.Finding
	for _, req := range domain.RequiredTools(in.Config) {
		if err := g.tools.Locate(req.Name); err != nil {
			findings = append(findings, domain.Finding{
				Gate:    g.Name(),
				Rule:    "tool-missing",
				Message: fmt.Sprintf("%s is required because %s, but it cannot be run: %v", req.Name, req.Reason, err),
			})
		}
	}
	return findings, nil
}
