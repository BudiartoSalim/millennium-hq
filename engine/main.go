// Command millennium is the gate engine of the millennium plugin. main is the
// composition root: it builds the adapters, wires the use-cases and hands
// control to the CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"millennium/internal/application/gate"
	"millennium/internal/application/port"
	"millennium/internal/application/usecase"
	"millennium/internal/domain"
	"millennium/internal/infrastructure/analysiscache"
	"millennium/internal/infrastructure/buildtools"
	"millennium/internal/infrastructure/configfile"
	"millennium/internal/infrastructure/gitrepo"
	"millennium/internal/infrastructure/goanalyzer"
	"millennium/internal/infrastructure/helperanalyzer"
	"millennium/internal/infrastructure/packs"
	"millennium/internal/infrastructure/shellanalyzer"
	"millennium/internal/infrastructure/shelltools"
	"millennium/internal/infrastructure/statestore"
	"millennium/internal/infrastructure/testrunner"
	"millennium/internal/infrastructure/toolpath"
	"millennium/internal/interface/cli"
)

// version is set by release builds with -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.New(version, os.Stdin, os.Stdout, os.Stderr, buildDeps).Run(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

func buildDeps(ctx context.Context, dir string) (cli.Deps, error) {
	repo, err := openRepository(ctx, dir)
	if err != nil {
		return cli.Deps{}, err
	}
	builtinPacks, err := packs.Builtin()
	if err != nil {
		return cli.Deps{}, fmt.Errorf("load the builtin convention packs (an engine bug): %w", err)
	}
	configPath := filepath.Join(repo.Root(), filepath.FromSlash(domain.ConfigPath))
	loadConfig := usecase.NewLoadConfig(configfile.NewLoader(configPath), repo)
	node := toolpath.Find("node")
	typeScript := helperanalyzer.TypeScript(repo.Root(), node)
	python := helperanalyzer.Python(repo.Root(), toolpath.Python())
	// Several gates read the same files; the cache parses each version once.
	analyzers := map[domain.Language]port.Analyzer{
		domain.LanguageGo:         analysiscache.Wrap(goanalyzer.New(repo.Root())),
		domain.LanguageTypeScript: analysiscache.Wrap(typeScript),
		domain.LanguagePython:     analysiscache.Wrap(python),
		domain.LanguageShell:      analysiscache.Wrap(shellanalyzer.Analyzer{}),
	}
	runGates := usecase.NewRunGates(loadConfig, repo, gates(repo, node, analyzers, builtinPacks))
	return cli.Deps{
		Gates:    runGates,
		Audit:    usecase.NewRunAudit(loadConfig, repo, gates(repo, node, analyzers, builtinPacks)),
		Config:   loadConfig,
		Explain:  usecase.NewExplainFile(loadConfig, builtinPacks),
		Guard:    usecase.NewGuardToolCall(loadConfig),
		Stop:     usecase.NewStopCheck(runGates, statestore.New(stateDir())),
		Scope:    usecase.NewCheckScope(loadConfig, repo),
		RepoPath: repo.RepoPath,
		Close: func() {
			typeScript.Close()
			python.Close()
		},
	}, nil
}

// openRepository opens the repository containing dir, the current directory
// when dir is empty, and reports a directory outside any repository as
// cli.ErrNoRepository.
func openRepository(ctx context.Context, dir string) (*gitrepo.Repository, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("find working directory: %w", err)
		}
		dir = wd
	}
	repo, err := gitrepo.Open(ctx, dir)
	if errors.Is(err, gitrepo.ErrNotRepository) {
		return nil, fmt.Errorf("%w: %v", cli.ErrNoRepository, err)
	}
	return repo, err
}

// gates lists every gate in the order reports show them.
func gates(repo *gitrepo.Repository, node string, analyzers map[domain.Language]port.Analyzer, builtinPacks *packs.Source) []port.Gate {
	return []port.Gate{
		gate.NewToolchain(toolpath.Locator{}),
		gate.NewStructure(analyzers, builtinPacks, repo),
		gate.NewShell(repo, shelltools.New(repo.Root())),
		gate.NewCode(analyzers, repo),
		gate.NewBuild(buildtools.New(repo.Root(), node), repo),
		gate.NewTests(analyzers, builtinPacks, repo, testrunner.New(repo.Root(), toolpath.Python()), repo),
	}
}

// stateDir is where the hooks keep their counters: MILLENNIUM_STATE_DIR when
// set, the plugin's data directory when run by Claude Code (outside the
// agents' writable area), or the user cache directory.
func stateDir() string {
	if dir := os.Getenv("MILLENNIUM_STATE_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("CLAUDE_PLUGIN_DATA"); dir != "" {
		return filepath.Join(dir, "state")
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "millennium", "state")
	}
	return filepath.Join(os.TempDir(), "millennium-state")
}
