// Command millennium is the gate engine of the millennium plugin. main is the
// composition root: it builds the adapters, wires the use-cases and hands
// control to the CLI.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"millennium/internal/application/gate"
	"millennium/internal/application/port"
	"millennium/internal/application/usecase"
	"millennium/internal/domain"
	"millennium/internal/infrastructure/analysiscache"
	"millennium/internal/infrastructure/approvals"
	"millennium/internal/infrastructure/buildtools"
	"millennium/internal/infrastructure/configfile"
	"millennium/internal/infrastructure/gitrepo"
	"millennium/internal/infrastructure/goanalyzer"
	"millennium/internal/infrastructure/helperanalyzer"
	"millennium/internal/infrastructure/manifests"
	"millennium/internal/infrastructure/packs"
	"millennium/internal/infrastructure/projectfiles"
	"millennium/internal/infrastructure/registry"
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
	localDir, localPath, err := localConfig(ctx, repo)
	if err != nil {
		return cli.Deps{}, err
	}
	committed, local := configfile.NewLoader(configPath), configfile.NewLoader(localPath)
	loadConfig := usecase.NewLoadConfig(committed, repo).WithLocal(local)
	node := toolpath.Find("node")
	analyzers, closeAnalyzers := newAnalyzers(repo.Root(), node)
	files := usecase.ConfigFiles{
		Committed: committed, CommittedPath: configPath, Local: local, LocalPath: localPath,
		CommittedApprovals: filepath.Join(repo.Root(), filepath.FromSlash(domain.ApprovalsDir)),
		LocalApprovals:     filepath.Join(filepath.Dir(localPath), "deps"),
	}
	g := gates(repo, node, analyzers, builtinPacks, approvals.New(files.CommittedApprovals, files.LocalApprovals))
	runGates := usecase.NewRunGates(loadConfig, repo, g)
	return cli.Deps{
		Gates:          runGates,
		Audit:          usecase.NewRunAudit(loadConfig, repo, g),
		Config:         loadConfig,
		Explain:        usecase.NewExplainFile(loadConfig, builtinPacks),
		Guard:          usecase.NewGuardToolCall(loadConfig),
		Stop:           usecase.NewStopCheck(runGates, statestore.New(stateDir())),
		Scope:          usecase.NewCheckScope(loadConfig, repo),
		Apply:          usecase.NewApplyConfig(configfile.Codec{}, files, projectfiles.New(repo.Root())),
		Root:           repo.Root(),
		LocalConfigDir: localDir,
		RepoPath:       repo.RepoPath,
		Close:          closeAnalyzers,
		EngineCommand:  engineCommand(),
	}, nil
}

// newAnalyzers builds one analyzer per language and the function that stops
// the helper processes. Several gates read the same files; the cache parses
// each version once.
func newAnalyzers(root, node string) (map[domain.Language]port.Analyzer, func()) {
	typeScript := helperanalyzer.TypeScript(root, node)
	python := helperanalyzer.Python(root, toolpath.Python())
	analyzers := map[domain.Language]port.Analyzer{
		domain.LanguageGo:         analysiscache.Wrap(goanalyzer.New(root)),
		domain.LanguageTypeScript: analysiscache.Wrap(typeScript),
		domain.LanguagePython:     analysiscache.Wrap(python),
		domain.LanguageShell:      analysiscache.Wrap(shellanalyzer.Analyzer{}),
	}
	return analyzers, func() {
		typeScript.Close()
		python.Close()
	}
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
func gates(repo *gitrepo.Repository, node string, analyzers map[domain.Language]port.Analyzer, builtinPacks *packs.Source, approved port.ApprovalSource) []port.Gate {
	tools := buildtools.New(repo.Root(), node)
	return []port.Gate{
		gate.NewToolchain(toolpath.Locator{}),
		gate.NewStructure(analyzers, builtinPacks, repo),
		gate.NewShell(repo, shelltools.New(repo.Root())),
		gate.NewCode(analyzers, repo),
		gate.NewBuild(tools, repo),
		gate.NewTests(analyzers, builtinPacks, repo, testrunner.New(repo.Root(), toolpath.Python()), repo),
		gate.NewDeps(repo, manifests.Reader{}, approved, registry.New(registry.Public()), tools, time.Now),
	}
}

// localConfig is where this repository's local config lives: a directory
// per clone, named after the hash of git's common directory so every
// worktree finds the same one, under MILLENNIUM_LOCAL_DIR or the user
// config directory. Sandboxed agents cannot write there, and the hooks deny
// their file tools too.
func localConfig(ctx context.Context, repo *gitrepo.Repository) (base, file string, err error) {
	identity, err := repo.Identity(ctx)
	if err != nil {
		return "", "", err
	}
	base = os.Getenv("MILLENNIUM_LOCAL_DIR")
	if base == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", "", fmt.Errorf("find the user config directory for local configs: %w", err)
		}
		base = filepath.Join(configDir, "millennium", "projects")
	}
	sum := sha256.Sum256([]byte(identity))
	return base, filepath.Join(base, hex.EncodeToString(sum[:8]), "config.json"), nil
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

// engineCommand is how agents' shells reach the engine: hooks run with
// CLAUDE_PLUGIN_ROOT set, and the plugin's launcher lives under it.
func engineCommand() string {
	root := os.Getenv("CLAUDE_PLUGIN_ROOT")
	if root == "" {
		return "millennium"
	}
	return "'" + strings.ReplaceAll(filepath.Join(root, "scripts", "millennium"), "'", `'\''`) + "'"
}
