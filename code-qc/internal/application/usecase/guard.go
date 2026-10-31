package usecase

import (
	"context"

	"millennium-code-qc/internal/domain"
)

// GuardToolCall decides a tool call an agent asks for (PreToolUse).
type GuardToolCall struct {
	config *LoadConfig
}

// NewGuardToolCall returns the guard use-case.
func NewGuardToolCall(config *LoadConfig) *GuardToolCall {
	return &GuardToolCall{config: config}
}

// Execute allows everything in a repository that is not opted in. When the
// config cannot be read, the protected paths are unknown, so every file
// change is denied until a human fixes the config; reads stay allowed.
func (u *GuardToolCall) Execute(ctx context.Context, call domain.ToolCall) domain.GuardDecision {
	cfg, enabled, err := u.config.Execute(ctx)
	if err != nil {
		if domain.IsWriteTool(call.Tool) {
			return domain.GuardDecision{Deny: true, Reason: "millennium-code-qc: the config cannot be read, so file changes are blocked until a human fixes it: " + err.Error()}
		}
		return domain.GuardDecision{}
	}
	if !enabled {
		return domain.GuardDecision{}
	}
	return domain.Guard(cfg, call)
}
