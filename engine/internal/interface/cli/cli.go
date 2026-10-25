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
	"os"
	"slices"

	"millennium/internal/application/usecase"
	"millennium/internal/domain"
)

const (
	exitPass    = 0
	exitBlocked = 2
)

const usageText = `millennium: the gate engine of the millennium plugin.

Usage:
  millennium gate [--base REV] [--require-config] [--tier fast|full] [--format text|json]
      Check the changes against REV (default HEAD). Hooks run the fast tier;
      the full tier, the default, also runs the tests and measures coverage.
  millennium audit [--path PATH] [--tier fast|full] [--format text|json]
      Check all existing code, or only PATH, against the full rules.
  millennium scope --base REV --allow PATTERN [--allow PATTERN ...]
      Check that a branch changes only its work package's paths, and no
      protected path or acceptance test. The tech lead runs it before merging.
  millennium hook
      Handle a Claude Code hook event read from stdin (the plugin's hooks).
  millennium config validate
      Validate .millennium/config.json.
  millennium config apply [--local] [--confirm DIGEST] [PROPOSAL]
      Apply a proposal (default millennium.proposal.json) after showing what it
      changes and loosens. Only you run it: it asks you to confirm.
  millennium config export [--local]
      Print the config as a proposal, to back it up or move it.
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

// ToolGuard decides an agent's tool call; usecase.GuardToolCall implements it.
type ToolGuard interface {
	Execute(ctx context.Context, call domain.ToolCall) domain.GuardDecision
}

// StopChecker decides whether an agent may stop; usecase.StopCheck
// implements it.
type StopChecker interface {
	Execute(ctx context.Context, key string) (usecase.StopVerdict, error)
}

// ScopeChecker checks a branch against its work package; usecase.CheckScope
// implements it.
type ScopeChecker interface {
	Execute(ctx context.Context, in usecase.ScopeInput) (domain.Report, error)
}

// ConfigApplier validates, applies and exports configs; usecase.ApplyConfig
// implements it.
type ConfigApplier interface {
	Prepare(proposal []byte, target usecase.ApplyTarget) (usecase.ApplyPlan, error)
	Apply(plan usecase.ApplyPlan) error
	Export(target usecase.ApplyTarget) ([]byte, error)
}

// ErrNoRepository is what a DepsFactory returns outside a git repository;
// hooks then do nothing, since nothing there is opted in.
var ErrNoRepository = errors.New("not inside a git repository")

// Deps are what the commands call.
type Deps struct {
	Gates   GateRunner
	Audit   AuditRunner
	Config  ConfigLoader
	Explain Explainer
	Guard   ToolGuard
	Stop    StopChecker
	Scope   ScopeChecker
	Apply   ConfigApplier
	// Root is the repository's absolute path.
	Root string
	// LocalConfigDir holds the local configs; no agent may write there.
	LocalConfigDir string
	// RepoPath turns a command-line path, relative to the current
	// directory, into a repository-relative one.
	RepoPath func(arg string) (string, error)
	// Close releases what the dependencies hold, such as analyzer
	// processes; nil when there is nothing to release.
	Close func()
}

// DepsFactory builds Deps on demand for the repository containing dir ("" is
// the current directory), so commands such as version work outside a git
// repository.
type DepsFactory func(ctx context.Context, dir string) (Deps, error)

// withDeps builds the dependencies for the current directory, runs use, and
// releases them.
func (a *App) withDeps(ctx context.Context, use func(Deps) error) error {
	return a.withDepsIn(ctx, "", use)
}

func (a *App) withDepsIn(ctx context.Context, dir string, use func(Deps) error) error {
	deps, err := a.deps(ctx, dir)
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
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
	deps    DepsFactory
	// terminal reports whether stdin is an interactive terminal, where
	// `config apply` may ask for confirmation.
	terminal func() bool
}

// New returns the application. Text reports go to stderr, where Claude Code
// hooks pick them up; JSON reports go to stdout; hook events arrive on stdin.
func New(version string, stdin io.Reader, stdout, stderr io.Writer, deps DepsFactory) *App {
	return &App{version: version, stdin: stdin, stdout: stdout, stderr: stderr, deps: deps, terminal: func() bool { return isTerminal(stdin) }}
}

// isTerminal reports whether r is a character device, such as a terminal.
// /dev/null is one too, but a confirmation read from it gets no "yes".
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
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
	case "scope":
		return a.scope(ctx, args[1:])
	case "hook":
		return a.hook(ctx)
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
	tierName := flags.String("tier", "full", "fast (hooks: no test runs) or full (also runs the tests and measures coverage)")
	render, ok := a.parse(flags, args, formatName, "changed files")
	if !ok {
		return exitBlocked
	}
	tier, ok := a.tier(*tierName)
	if !ok {
		return exitBlocked
	}
	in := usecase.RunGatesInput{Base: *base, Config: usecase.ConfigOptional, Tier: tier}
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
	tierName := flags.String("tier", "full", "fast (no test runs) or full")
	render, ok := a.parse(flags, args, formatName, "audited files")
	if !ok {
		return exitBlocked
	}
	tier, ok := a.tier(*tierName)
	if !ok {
		return exitBlocked
	}
	var report domain.Report
	err := a.withDeps(ctx, func(deps Deps) (err error) {
		in := usecase.AuditInput{Tier: tier}
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

func (a *App) tier(name string) (domain.Tier, bool) {
	tier := domain.Tier(name)
	if !slices.Contains(domain.Tiers(), tier) {
		fmt.Fprintf(a.stderr, "millennium: unknown tier %q: use fast or full\n", name)
		return "", false
	}
	return tier, true
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
	case len(args) >= 1 && args[0] == "apply":
		return a.applyConfig(ctx, args[1:])
	case len(args) >= 1 && args[0] == "export":
		return a.exportConfig(ctx, args[1:])
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
