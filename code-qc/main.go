// Command millennium-code-qc is the code quality engine of the millennium plugin. main is the
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

	"millennium-code-qc/internal/application/gate"
	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/application/usecase"
	"millennium-code-qc/internal/domain"
	"millennium-code-qc/internal/infrastructure/analysiscache"
	"millennium-code-qc/internal/infrastructure/approvals"
	"millennium-code-qc/internal/infrastructure/buildtools"
	"millennium-code-qc/internal/infrastructure/configfile"
	"millennium-code-qc/internal/infrastructure/gitrepo"
	"millennium-code-qc/internal/infrastructure/goanalyzer"
	"millennium-code-qc/internal/infrastructure/helperanalyzer"
	"millennium-code-qc/internal/infrastructure/manifests"
	"millennium-code-qc/internal/infrastructure/packs"
	"millennium-code-qc/internal/infrastructure/projectfiles"
	"millennium-code-qc/internal/infrastructure/registry"
	"millennium-code-qc/internal/infrastructure/shellanalyzer"
	"millennium-code-qc/internal/infrastructure/shelltools"
	"millennium-code-qc/internal/infrastructure/statestore"
	"millennium-code-qc/internal/infrastructure/testrunner"
	"millennium-code-qc/internal/infrastructure/toolpath"
	"millennium-code-qc/internal/interface/cli"
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
	localDir, localPath, err := localConfig(ctx, repo)
	if err != nil {
		return cli.Deps{}, err
	}
	files := configFiles(repo.Root(), localPath, builtinPacks)
	loadConfig := usecase.NewLoadConfig(files.Committed, repo).WithLocal(files.Local)
	node := toolpath.Find("node")
	analyzers, closeAnalyzers := newAnalyzers(repo.Root(), node)
	g := gates(repo, node, analyzers, approvals.New(files.CommittedApprovals, files.LocalApprovals))
	runGates := usecase.NewRunGates(loadConfig, repo, g)
	writer := projectfiles.New(repo.Root())
	return cli.Deps{
		Gates:          runGates,
		Audit:          usecase.NewRunAudit(loadConfig, repo, g),
		Config:         loadConfig,
		Explain:        usecase.NewExplainFile(loadConfig),
		Guard:          usecase.NewGuardToolCall(loadConfig),
		Stop:           usecase.NewStopCheck(runGates, statestore.New(stateDir())),
		Scope:          usecase.NewCheckScope(loadConfig, repo),
		Apply:          usecase.NewApplyConfig(configfile.Codec{}, files, writer),
		Discover:       usecase.NewDiscover(repo, repo, files.Committed),
		Conventions:    usecase.NewConventions(files.Committed, files.Local, repo, configfile.Codec{}, writer, repo.Root()),
		Root:           repo.Root(),
		LocalConfigDir: localDir,
		RepoPath:       repo.RepoPath,
		OtherWorktrees: repo.OtherWorktrees,
		ResolvePath:    gitrepo.Resolve,
		ReadProposal:   configfile.ReadProposal,
		Close:          closeAnalyzers,
		EngineCommand:  engineCommand(),
	}, nil
}

// configFiles locates the committed and local configs, with the convention
// packs and dependency approvals beside each.
func configFiles(root, localPath string, builtin []domain.Pack) usecase.ConfigFiles {
	configPath := filepath.Join(root, filepath.FromSlash(domain.ConfigPath))
	return usecase.ConfigFiles{
		Committed:          configfile.NewLoader(configPath, builtin, domain.PackProject),
		CommittedPath:      configPath,
		Local:              configfile.NewLoader(localPath, builtin, domain.PackLocal),
		LocalPath:          localPath,
		CommittedApprovals: filepath.Join(root, filepath.FromSlash(domain.ApprovalsDir)),
		LocalApprovals:     filepath.Join(filepath.Dir(localPath), "deps"),
		CommittedPacks:     configfile.PacksPath(configPath),
		LocalPacks:         configfile.PacksPath(localPath),
	}
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
// cli.ErrNoRepository and one that does not exist as cli.ErrNoDirectory.
func openRepository(ctx context.Context, dir string) (*gitrepo.Repository, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("find working directory: %w", err)
		}
		dir = wd
	}
	repo, err := gitrepo.Open(ctx, dir)
	switch {
	case errors.Is(err, gitrepo.ErrNotRepository):
		return nil, fmt.Errorf("%s: %w", dir, cli.ErrNoRepository)
	case errors.Is(err, gitrepo.ErrNoDirectory):
		return nil, fmt.Errorf("%s: %w", dir, cli.ErrNoDirectory)
	}
	return repo, err
}

// gates lists every gate in the order reports show them.
func gates(repo *gitrepo.Repository, node string, analyzers map[domain.Language]port.Analyzer, approved port.ApprovalSource) []port.Gate {
	tools := buildtools.New(repo.Root(), node)
	return []port.Gate{
		gate.NewToolchain(toolpath.Locator{}),
		gate.NewStructure(analyzers, repo),
		gate.NewShell(repo, shelltools.New(repo.Root())),
		gate.NewHygiene(analyzers, repo),
		gate.NewBuild(tools, repo),
		gate.NewTests(analyzers, repo, testrunner.New(repo.Root(), toolpath.Python()), repo, goanalyzer.NewSources(repo.Root())),
		gate.NewDeps(repo, manifests.Reader{}, approved, registry.New(registry.Public()), tools, time.Now),
	}
}

// localConfig is where this repository's local config lives: a directory
// per clone, named after the hash of git's common directory so every
// worktree finds the same one, under MILLENNIUM_LOCAL_DIR or the user
// config directory. Sandboxed agents cannot write there, and the hooks deny
// their file tools too, comparing absolute paths, so the directory must be
// absolute.
func localConfig(ctx context.Context, repo *gitrepo.Repository) (base, file string, err error) {
	identity, err := repo.Identity(ctx)
	if err != nil {
		return "", "", err
	}
	base = os.Getenv("MILLENNIUM_LOCAL_DIR")
	if base != "" && !filepath.IsAbs(base) {
		return "", "", fmt.Errorf("MILLENNIUM_LOCAL_DIR is %s, which is not an absolute path; set it to one, or unset it", base)
	}
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

// engineCommand is how agents' shells reach the engine: the plugin's
// launcher, under the plugin root that Claude Code's hooks (CLAUDE_PLUGIN_ROOT)
// or millennium's pi extension (MILLENNIUM_PLUGIN_ROOT) pass in.
func engineCommand() string {
	root := os.Getenv("MILLENNIUM_PLUGIN_ROOT")
	if root == "" {
		root = os.Getenv("CLAUDE_PLUGIN_ROOT")
	}
	if root == "" {
		return "millennium-code-qc"
	}
	return "'" + strings.ReplaceAll(filepath.Join(root, "scripts", "millennium-code-qc"), "'", `'\''`) + "'"
}
