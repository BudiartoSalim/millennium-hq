// Command millennium is the gate engine of the millennium plugin. main is the
// composition root: it builds the adapters, wires the use-cases and hands
// control to the CLI.
package main

import (
	"context"
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
	"millennium/internal/infrastructure/configfile"
	"millennium/internal/infrastructure/gitrepo"
	"millennium/internal/infrastructure/goanalyzer"
	"millennium/internal/infrastructure/helperanalyzer"
	"millennium/internal/infrastructure/packs"
	"millennium/internal/infrastructure/shellanalyzer"
	"millennium/internal/infrastructure/shelltools"
	"millennium/internal/infrastructure/toolpath"
	"millennium/internal/interface/cli"
)

// version is set by release builds with -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.New(version, os.Stdout, os.Stderr, buildDeps).Run(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

func buildDeps(ctx context.Context) (cli.Deps, error) {
	wd, err := os.Getwd()
	if err != nil {
		return cli.Deps{}, fmt.Errorf("find working directory: %w", err)
	}
	repo, err := gitrepo.Open(ctx, wd)
	if err != nil {
		return cli.Deps{}, err
	}
	builtinPacks, err := packs.Builtin()
	if err != nil {
		return cli.Deps{}, fmt.Errorf("load the builtin convention packs (an engine bug): %w", err)
	}
	configPath := filepath.Join(repo.Root(), filepath.FromSlash(domain.ConfigPath))
	loadConfig := usecase.NewLoadConfig(configfile.NewLoader(configPath), repo)
	typeScript := helperanalyzer.TypeScript(repo.Root(), toolpath.Find("node"))
	python := helperanalyzer.Python(repo.Root(), toolpath.Python())
	// Several gates read the same files; the cache parses each version once.
	analyzers := map[domain.Language]port.Analyzer{
		domain.LanguageGo:         analysiscache.Wrap(goanalyzer.New(repo.Root())),
		domain.LanguageTypeScript: analysiscache.Wrap(typeScript),
		domain.LanguagePython:     analysiscache.Wrap(python),
		domain.LanguageShell:      analysiscache.Wrap(shellanalyzer.Analyzer{}),
	}
	gates := []port.Gate{
		gate.NewToolchain(toolpath.Locator{}),
		gate.NewStructure(analyzers, builtinPacks, repo),
		gate.NewShell(repo, shelltools.New(repo.Root())),
		gate.NewCode(analyzers, repo),
	}
	return cli.Deps{
		Gates:    usecase.NewRunGates(loadConfig, repo, gates),
		Audit:    usecase.NewRunAudit(loadConfig, repo, gates),
		Config:   loadConfig,
		Explain:  usecase.NewExplainFile(loadConfig, builtinPacks),
		RepoPath: repo.RepoPath,
		Close: func() {
			typeScript.Close()
			python.Close()
		},
	}, nil
}
