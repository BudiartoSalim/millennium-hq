// Package cli is the command-line delivery: it parses arguments, calls the
// use-cases and renders reports.
//
// Exit status is 0 only for a pass (or a repository that is not opted in).
// Every other outcome, usage and internal errors included, exits 2: the code
// Claude Code hooks treat as blocking, and non-zero for any other caller.
// Exit 1 is never used because Claude Code treats it as a non-blocking error.
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
      Check the changes against REV (default HEAD): what hooks and the tech
      lead run.
  millennium audit [--path PATH] [--format text|json]
      Check all existing code, or only PATH, against the full rules.
  millennium config validate
      Validate .millennium/config.json.
  millennium config explain PATH
      Show which component, layers and limits apply to PATH.
  millennium version

Paths are relative to the current directory. Exit status is 0 only when the
check passes or the repository is not opted in; every other outcome, errors
included, exits 2.
`

// GateRunner runs the gates over a change; usecase.RunGates implements it.
type GateRunner interface {
	Execute(ctx context.Context, in usecase.RunGatesInput) (domain.Report, error)
}

// AuditRunner runs the gates over existing code; usecase.RunAudit implements it.
type AuditRunner interface {
	Execute(ctx context.Context, in usecase.AuditInput) (domain.Report, error)
}

// ConfigLoader loads the project config; usecase.LoadConfig implements it.
type ConfigLoader interface {
	Execute(ctx context.Context) (domain.Config, bool, error)
}

// Explainer explains which rules apply to a file; usecase.ExplainFile
// implements it.
type Explainer interface {
	Execute(ctx context.Context, path string) (usecase.Explanation, error)
}

// Deps are what the commands call.
type Deps struct {
	Gates   GateRunner
	Audit   AuditRunner
	Config  ConfigLoader
	Explain Explainer
	// RepoPath turns a command-line path, relative to the current
	// directory, into a repository-relative one.
	RepoPath func(arg string) (string, error)
	// Close releases what the dependencies hold, such as analyzer
	// processes; nil when there is nothing to release.
	Close func()
}

// DepsFactory builds Deps on demand, so commands such as version work
// outside a git repository.
type DepsFactory func(ctx context.Context) (Deps, error)

// withDeps builds the dependencies, runs use, and releases them.
func (a *App) withDeps(ctx context.Context, use func(Deps) error) error {
	deps, err := a.deps(ctx)
	if deps.Close != nil {
		defer deps.Close()
	}
	if err != nil {
		return err
	}
	return use(deps)
}

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
	case "audit":
		return a.audit(ctx, args[1:])
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
	flags := a.flagSet("gate")
	base := flags.String("base", "", "revision to compare the working tree against (default HEAD)")
	requireConfig := flags.Bool("require-config", false, "block when the repository has no config")
	formatName := flags.String("format", "text", "report format: text or json")
	render, ok := a.parse(flags, args, formatName, "changed files")
	if !ok {
		return exitBlocked
	}
	in := usecase.RunGatesInput{Base: *base, Config: usecase.ConfigOptional}
	if *requireConfig {
		in.Config = usecase.ConfigRequired
	}
	var report domain.Report
	err := a.withDeps(ctx, func(deps Deps) (err error) {
		report, err = deps.Gates.Execute(ctx, in)
		return err
	})
	return render.outcome(report, err)
}

func (a *App) audit(ctx context.Context, args []string) int {
	flags := a.flagSet("audit")
	pathArg := flags.String("path", "", "directory or file to audit, relative to the current directory (default: everything)")
	formatName := flags.String("format", "text", "report format: text or json")
	render, ok := a.parse(flags, args, formatName, "audited files")
	if !ok {
		return exitBlocked
	}
	var report domain.Report
	err := a.withDeps(ctx, func(deps Deps) (err error) {
		in := usecase.AuditInput{}
		if *pathArg != "" {
			if in.Path, err = deps.RepoPath(*pathArg); err != nil {
				return err
			}
		}
		report, err = deps.Audit.Execute(ctx, in)
		return err
	})
	return render.outcome(report, err)
}

func (a *App) flagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	return flags
}

// parse reads the flags, refuses stray arguments and picks the renderer.
func (a *App) parse(flags *flag.FlagSet, args []string, formatName *string, filesLabel string) (renderer, bool) {
	if err := flags.Parse(args); err != nil {
		return renderer{}, false
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(a.stderr, "millennium: unexpected argument %q\n", flags.Arg(0))
		return renderer{}, false
	}
	render, err := rendererFor(*formatName, a.stdout, a.stderr, filesLabel)
	if err != nil {
		fmt.Fprintf(a.stderr, "millennium: %v\n", err)
		return renderer{}, false
	}
	return render, true
}

func (a *App) config(ctx context.Context, args []string) int {
	switch {
	case len(args) == 1 && args[0] == "validate":
		return a.validateConfig(ctx)
	case len(args) == 2 && args[0] == "explain":
		return a.explain(ctx, args[1])
	}
	fmt.Fprint(a.stderr, usageText)
	return exitBlocked
}

func (a *App) validateConfig(ctx context.Context) int {
	err := a.withDeps(ctx, func(deps Deps) error {
		_, enabled, err := deps.Config.Execute(ctx)
		if err == nil && !enabled {
			err = errors.New("no " + domain.ConfigPath + " in this repository")
		}
		return err
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "millennium: %v\n", err)
		return exitBlocked
	}
	fmt.Fprintf(a.stdout, "millennium: %s is valid\n", domain.ConfigPath)
	return exitPass
}

func (a *App) explain(ctx context.Context, arg string) int {
	var explanation usecase.Explanation
	err := a.withDeps(ctx, func(deps Deps) error {
		path, err := deps.RepoPath(arg)
		if err != nil {
			return err
		}
		explanation, err = deps.Explain.Execute(ctx, path)
		return err
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "millennium: %v\n", err)
		return exitBlocked
	}
	writeExplanation(a.stdout, explanation)
	return exitPass
}
