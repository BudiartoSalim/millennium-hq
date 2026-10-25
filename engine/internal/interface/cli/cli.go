// Package cli is the command-line delivery: it parses arguments, calls the
// use-cases and renders reports.
//
// Exit status is 0 only for a pass (or a repository that is not opted in).
// Every other outcome, usage and internal errors included, exits 2: the code
// Claude Code hooks treat as blocking, and non-zero for git hooks and CI. Exit
// 1 is never used because Claude Code treats it as a non-blocking error.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"millennium/internal/application/usecase"
	"millennium/internal/domain"
)

const (
	exitPass    = 0
	exitBlocked = 2
)

const usageText = `millennium: the gate engine of the millennium plugin.

Usage:
  millennium gate [--base REV] [--require-config] [--format text|json]
  millennium config validate
  millennium version

Exit status is 0 only when the change passes or the repository is not opted
in; every other outcome, errors included, exits 2.
`

// GateRunner runs the gates; usecase.RunGates implements it.
type GateRunner interface {
	Execute(ctx context.Context, in usecase.RunGatesInput) (domain.Report, error)
}

// ConfigLoader loads the project config; usecase.LoadConfig implements it.
type ConfigLoader interface {
	Execute(ctx context.Context) (domain.Config, bool, error)
}

// Deps are the use-cases the commands call.
type Deps struct {
	Gates  GateRunner
	Config ConfigLoader
}

// DepsFactory builds Deps on demand, so commands such as version work
// outside a git repository.
type DepsFactory func(ctx context.Context) (Deps, error)

// App is the command-line application.
type App struct {
	version string
	stdout  io.Writer
	stderr  io.Writer
	deps    DepsFactory
}

// New returns the application. Text reports go to stderr, where Claude Code
// hooks pick them up; JSON reports go to stdout.
func New(version string, stdout, stderr io.Writer, deps DepsFactory) *App {
	return &App{version: version, stdout: stdout, stderr: stderr, deps: deps}
}

// Run executes the command in args and returns the exit status.
func (a *App) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.stderr, usageText)
		return exitBlocked
	}
	switch args[0] {
	case "gate":
		return a.gate(ctx, args[1:])
	case "config":
		return a.config(ctx, args[1:])
	case "version":
		fmt.Fprintln(a.stdout, "millennium", a.version)
		return exitPass
	case "help", "-h", "--help":
		fmt.Fprint(a.stdout, usageText)
		return exitPass
	}
	fmt.Fprintf(a.stderr, "millennium: unknown command %q\n\n%s", args[0], usageText)
	return exitBlocked
}

func (a *App) gate(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("gate", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	base := flags.String("base", "", "revision to compare the working tree against (default HEAD)")
	requireConfig := flags.Bool("require-config", false, "block when the repository has no config; use in CI")
	formatName := flags.String("format", "text", "report format: text or json")
	if err := flags.Parse(args); err != nil {
		return exitBlocked
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(a.stderr, "millennium: unexpected argument %q\n", flags.Arg(0))
		return exitBlocked
	}
	render, err := rendererFor(*formatName, a.stdout, a.stderr)
	if err != nil {
		fmt.Fprintf(a.stderr, "millennium: %v\n", err)
		return exitBlocked
	}
	in := usecase.RunGatesInput{Base: *base, Config: usecase.ConfigOptional}
	if *requireConfig {
		in.Config = usecase.ConfigRequired
	}
	report, err := a.runGates(ctx, in)
	if err != nil {
		render.failure(err)
		return exitBlocked
	}
	render.report(report)
	if report.Passed() {
		return exitPass
	}
	return exitBlocked
}

func (a *App) runGates(ctx context.Context, in usecase.RunGatesInput) (domain.Report, error) {
	deps, err := a.deps(ctx)
	if err != nil {
		return domain.Report{}, err
	}
	return deps.Gates.Execute(ctx, in)
}

func (a *App) config(ctx context.Context, args []string) int {
	if len(args) != 1 || args[0] != "validate" {
		fmt.Fprint(a.stderr, usageText)
		return exitBlocked
	}
	err := a.validateConfig(ctx)
	if err != nil {
		fmt.Fprintf(a.stderr, "millennium: %v\n", err)
		return exitBlocked
	}
	fmt.Fprintf(a.stdout, "millennium: %s is valid\n", domain.ConfigPath)
	return exitPass
}

func (a *App) validateConfig(ctx context.Context) error {
	deps, err := a.deps(ctx)
	if err != nil {
		return err
	}
	_, enabled, err := deps.Config.Execute(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return errors.New("no " + domain.ConfigPath + " in this repository")
	}
	return nil
}
