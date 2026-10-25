package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"millennium/internal/application/usecase"
)

// errNotApplied is returned when the user did not confirm.
var errNotApplied = errors.New("not applied")

func (a *App) applyConfig(ctx context.Context, args []string) int {
	flags := a.flagSet("config apply")
	local := flags.Bool("local", false, "apply as this machine's local config, outside the repository")
	confirm := flags.String("confirm", "", "the digest shown by a previous run, to confirm without a terminal")
	if err := flags.Parse(args); err != nil || flags.NArg() > 1 {
		fmt.Fprint(a.stderr, usageText)
		return exitBlocked
	}
	err := a.withDeps(ctx, func(deps Deps) error {
		path := filepath.Join(deps.Root, usecase.ProposalFile)
		if flags.NArg() == 1 {
			path = flags.Arg(0)
		}
		return a.apply(deps, path, applyTarget(*local), *confirm, flags.NArg() == 0)
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "millennium: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

func applyTarget(local bool) usecase.ApplyTarget {
	if local {
		return usecase.ApplyLocal
	}
	return usecase.ApplyCommitted
}

func (a *App) apply(deps Deps, path string, target usecase.ApplyTarget, confirm string, defaultProposal bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read the proposal: %w", err)
	}
	plan, err := deps.Apply.Prepare(data, target)
	if err != nil {
		return err
	}
	a.describePlan(path, plan)
	if err := a.confirmPlan(plan, confirm); err != nil {
		return err
	}
	if err := deps.Apply.Apply(plan); err != nil {
		return err
	}
	if defaultProposal {
		// The proposal has done its job; leaving it would invite a re-apply.
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("applied, but the proposal could not be removed: %w", err)
		}
	}
	fmt.Fprintln(a.stderr, "millennium: applied")
	return nil
}

func (a *App) describePlan(path string, plan usecase.ApplyPlan) {
	fmt.Fprintf(a.stderr, "millennium: proposal %s (digest %s)\n\nIt writes:\n", path, plan.Digest)
	for _, w := range plan.Writes {
		fmt.Fprintf(a.stderr, "  - %s\n", w)
	}
	switch {
	case plan.Warning != "":
		fmt.Fprintf(a.stderr, "\nWARNING: %s\n", plan.Warning)
	case len(plan.Loosenings) == 0:
		fmt.Fprintln(a.stderr, "\nIt loosens nothing.")
	default:
		fmt.Fprintln(a.stderr, "\nIt LOOSENS the rules:")
		for _, l := range plan.Loosenings {
			fmt.Fprintf(a.stderr, "  - %s\n", l)
		}
	}
	fmt.Fprintf(a.stderr, "\nThe config it applies:\n%s\n", plan.Canonical)
}

// confirmPlan accepts the digest from a previous run, or a typed "yes" at a
// terminal. Piped input is refused: an agent can pipe "yes", but it cannot
// type at your terminal.
func (a *App) confirmPlan(plan usecase.ApplyPlan, confirm string) error {
	switch {
	case confirm == plan.Digest:
		return nil
	case confirm != "":
		return fmt.Errorf("--confirm %s does not match this proposal's digest %s; it changed since you read it", confirm, plan.Digest)
	case !a.terminal():
		return fmt.Errorf("%w: read the summary above, then re-run with --confirm %s from your own terminal or Claude Code's ! prefix", errNotApplied, plan.Digest)
	}
	fmt.Fprint(a.stderr, "Type yes to apply: ")
	answer, _ := bufio.NewReader(a.stdin).ReadString('\n')
	if strings.TrimSpace(answer) != "yes" {
		return errNotApplied
	}
	return nil
}

func (a *App) exportConfig(ctx context.Context, args []string) int {
	flags := a.flagSet("config export")
	local := flags.Bool("local", false, "export this machine's local config")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		fmt.Fprint(a.stderr, usageText)
		return exitBlocked
	}
	err := a.withDeps(ctx, func(deps Deps) error {
		data, err := deps.Apply.Export(applyTarget(*local))
		if err != nil {
			return err
		}
		_, err = a.stdout.Write(data)
		return err
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "millennium: %v\n", err)
		return exitBlocked
	}
	return exitPass
}
