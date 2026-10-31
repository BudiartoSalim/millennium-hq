package usecase

import (
	"context"
	"fmt"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// MaxStopAttempts is how many times in a row an agent is sent back to fix
// failing gates before the user is told instead. The gates are not passed:
// the work stays not done, and the user decides.
const MaxStopAttempts = 3

// StopVerdict is what the stop hook does.
type StopVerdict struct {
	// Block sends the agent back to fix the findings.
	Block bool
	// Escalate lets the turn end but tells the user the work is not done.
	Escalate bool
	Attempt  int
	Report   domain.Report
	// Problem is set when the gates could not run at all.
	Problem string
}

// StopCheck runs the fast tier when an agent wants to stop (Stop and
// SubagentStop hooks).
type StopCheck struct {
	gates    *RunGates
	attempts port.AttemptStore
}

// NewStopCheck returns the stop-check use-case.
func NewStopCheck(gates *RunGates, attempts port.AttemptStore) *StopCheck {
	return &StopCheck{gates: gates, attempts: attempts}
}

// Execute checks the working tree for the agent identified by key.
func (u *StopCheck) Execute(ctx context.Context, key string) (StopVerdict, error) {
	report, err := u.gates.Execute(ctx, RunGatesInput{Config: ConfigOptional, Tier: domain.TierFast})
	verdict := StopVerdict{Report: report}
	if err != nil {
		verdict.Problem = err.Error()
	}
	if err == nil && report.Passed() {
		return verdict, u.attempts.Reset(key)
	}
	attempt, err := u.attempts.Increment(key)
	if err != nil {
		return StopVerdict{}, fmt.Errorf("count stop attempts: %w", err)
	}
	verdict.Attempt = attempt
	verdict.Block = attempt <= MaxStopAttempts
	verdict.Escalate = !verdict.Block
	return verdict, nil
}
