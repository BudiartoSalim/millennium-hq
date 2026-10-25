package cli

import (
	"context"
	"strings"

	"millennium/internal/application/usecase"
	"millennium/internal/domain"
)

// patternFlags collects a repeatable --allow flag.
type patternFlags []string

func (p *patternFlags) String() string { return strings.Join(*p, ",") }

func (p *patternFlags) Set(value string) error {
	*p = append(*p, value)
	return nil
}

func (a *App) scope(ctx context.Context, args []string) int {
	flags := a.flagSet("scope")
	base := flags.String("base", "", "the revision the branch started from, such as main")
	var allow patternFlags
	flags.Var(&allow, "allow", "a path pattern the work package may change; repeat for several")
	formatName := flags.String("format", "text", "report format: text or json")
	render, ok := a.parse(flags, args, formatName, "changed files")
	if !ok {
		return exitBlocked
	}
	var report domain.Report
	err := a.withDeps(ctx, func(deps Deps) (err error) {
		report, err = deps.Scope.Execute(ctx, usecase.ScopeInput{Base: *base, Allow: allow})
		return err
	})
	return render.outcome(report, err)
}
