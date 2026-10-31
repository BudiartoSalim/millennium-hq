// Package buildtools runs each language's own formatter, compiler and type
// checker, and turns their output into issues on repository-relative files.
// A tool that fails without output it can read is an error, never a pass.
package buildtools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"millennium-code-qc/internal/domain"
)

var (
	// goIssue is "file.go:12:3: message" or "file.go:12: message"; go vet
	// prefixes compile errors with "vet: ".
	goIssue = regexp.MustCompile(`^(?:vet:\s*)?(\S+\.go):(\d+)(?::\d+)?:\s*(.*)$`)
	// tscIssue is "src/a.ts(3,5): error TS2322: message".
	tscIssue = regexp.MustCompile(`^(.+?)\((\d+),\d+\): error (TS\d+): (.*)$`)
	// mypyIssue is "pkg/a.py:7: error: message  [code]".
	mypyIssue = regexp.MustCompile(`^(.+?\.pyi?):(\d+):(?:\d+:)?\s*error:\s*(.*?)(?:\s+\[([a-z-]+)\])?$`)
)

// Tools runs the build tools of the repository at root.
type Tools struct {
	root string
	node string
}

// New returns the tools for the repository at root; node is the node
// executable, "" when it was not found.
func New(root, node string) *Tools { return &Tools{root: root, node: node} }

type output struct {
	stdout, stderr string
	exitCode       int
}

func (t *Tools) run(ctx context.Context, dir string, env []string, name string, args ...string) (output, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = t.abs(dir)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := output{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		out.exitCode = exitErr.ExitCode()
		return out, nil
	}
	return out, err
}

// Gofmt lists the files gofmt would change.
func (t *Tools) Gofmt(ctx context.Context, files []string) ([]string, error) {
	args := []string{"-l"}
	for _, f := range files {
		args = append(args, t.abs(f))
	}
	out, err := t.run(ctx, "", nil, "gofmt", args...)
	if err != nil || out.exitCode != 0 {
		return nil, toolFailure("gofmt", out, err)
	}
	var unformatted []string
	for _, line := range lines(out.stdout) {
		unformatted = append(unformatted, t.rel(line))
	}
	return unformatted, nil
}

// GoVet runs go vet on the module in dir. Compile errors come back the same
// way and are issues too.
func (t *Tools) GoVet(ctx context.Context, dir string) ([]domain.LintIssue, error) {
	out, err := t.run(ctx, dir, []string{"GOFLAGS=-mod=readonly"}, "go", "vet", "./...")
	if err != nil {
		return nil, toolFailure("go vet", out, err)
	}
	issues := t.parse(dir, out.stderr+out.stdout, goIssue, func(m []string) domain.LintIssue {
		return domain.LintIssue{File: m[1], Line: atoi(m[2]), Code: "vet", Message: m[3]}
	})
	if out.exitCode != 0 && len(issues) == 0 {
		return nil, toolFailure("go vet", out, nil)
	}
	return issues, nil
}

// TypeCheck runs the project's own tsc.
func (t *Tools) TypeCheck(ctx context.Context, dir string) ([]domain.LintIssue, error) {
	tsc, err := t.tsc(dir)
	if err != nil {
		return nil, err
	}
	out, err := t.run(ctx, dir, nil, t.node, tsc, "--noEmit", "--pretty", "false", "-p", ".")
	if err != nil {
		return nil, toolFailure("tsc", out, err)
	}
	issues := t.parse(dir, out.stdout, tscIssue, func(m []string) domain.LintIssue {
		return domain.LintIssue{File: m[1], Line: atoi(m[2]), Code: m[3], Message: m[4]}
	})
	if out.exitCode != 0 && len(issues) == 0 {
		return nil, toolFailure("tsc", out, nil)
	}
	return issues, nil
}

// TSConfig prints the effective configuration with tsc --showConfig.
func (t *Tools) TSConfig(ctx context.Context, dir string) ([]byte, error) {
	tsc, err := t.tsc(dir)
	if err != nil {
		return nil, err
	}
	out, err := t.run(ctx, dir, nil, t.node, tsc, "--showConfig", "-p", ".")
	if err != nil || out.exitCode != 0 {
		return nil, toolFailure("tsc --showConfig", out, err)
	}
	return []byte(out.stdout), nil
}

// Mypy type-checks files from dir.
func (t *Tools) Mypy(ctx context.Context, dir string, files []string) ([]domain.LintIssue, error) {
	args := []string{"--no-color-output", "--no-error-summary", "--show-error-codes"}
	for _, f := range files {
		args = append(args, filepath.FromSlash(domain.Relative(f, dir)))
	}
	out, err := t.run(ctx, dir, nil, "mypy", args...)
	if err != nil || out.exitCode > 1 {
		return nil, toolFailure("mypy", out, err)
	}
	issues := t.parse(dir, out.stdout, mypyIssue, func(m []string) domain.LintIssue {
		return domain.LintIssue{File: m[1], Line: atoi(m[2]), Code: m[4], Message: m[3]}
	})
	if out.exitCode == 1 && len(issues) == 0 {
		return nil, toolFailure("mypy", out, nil)
	}
	return issues, nil
}

// tsc finds the project's TypeScript compiler: in the project's own
// node_modules or, for a workspace that hoists it, in a parent's, never
// outside the repository.
func (t *Tools) tsc(dir string) (string, error) {
	if t.node == "" {
		return "", errors.New("node was not found on PATH, so TypeScript cannot be type-checked")
	}
	for current := dir; ; current = parent(current) {
		candidate := filepath.Join(t.abs(current), "node_modules", "typescript", "bin", "tsc")
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
		if current == "" {
			return "", fmt.Errorf("no node_modules/typescript for %s; install the project's dependencies (npm ci)", displayDir(dir))
		}
	}
}

func (t *Tools) parse(dir, text string, pattern *regexp.Regexp, build func([]string) domain.LintIssue) []domain.LintIssue {
	var issues []domain.LintIssue
	for _, line := range lines(text) {
		if m := pattern.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			issue := build(m)
			issue.File = t.fromDir(dir, issue.File)
			issues = append(issues, issue)
		}
	}
	return issues
}

// fromDir turns a path a tool printed, relative to dir or absolute, into a
// repository-relative one.
func (t *Tools) fromDir(dir, p string) string {
	if filepath.IsAbs(p) {
		return t.rel(p)
	}
	return path.Join(dir, filepath.ToSlash(p))
}

func (t *Tools) rel(abs string) string {
	rel, err := filepath.Rel(t.root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

func (t *Tools) abs(p string) string { return filepath.Join(t.root, filepath.FromSlash(p)) }

func parent(dir string) string {
	if dir == "" || dir == "." {
		return ""
	}
	if p := path.Dir(dir); p != "." {
		return p
	}
	return ""
}

func displayDir(dir string) string {
	if dir == "" {
		return "the repository root"
	}
	return dir
}

func toolFailure(tool string, out output, err error) error {
	detail := strings.TrimSpace(out.stderr + "\n" + out.stdout)
	if len(detail) > 2000 {
		detail = detail[:2000] + "..."
	}
	if err != nil {
		return fmt.Errorf("%s could not run: %w %s", tool, err, detail)
	}
	return fmt.Errorf("%s failed (exit status %d): %s", tool, out.exitCode, detail)
}

func lines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// GoEnv returns the settings that decide how the go command fetches and
// verifies modules, for the module in dir.
func (t *Tools) GoEnv(ctx context.Context, dir string) (map[string]string, error) {
	out, err := t.run(ctx, dir, nil, "go", "env", "-json", "GOSUMDB", "GONOSUMDB", "GONOSUMCHECK", "GOINSECURE", "GOFLAGS")
	if err != nil || out.exitCode != 0 {
		return nil, toolFailure("go env", out, err)
	}
	env := map[string]string{}
	if err := json.Unmarshal([]byte(out.stdout), &env); err != nil {
		return nil, fmt.Errorf("read go env: %w", err)
	}
	return env, nil
}

// NpmAuditSignatures verifies the registry signatures and provenance of the
// installed packages of the project in dir. A project with nothing installed
// has nothing to verify yet; npm ci checks the lockfile's integrity hashes
// when it installs.
func (t *Tools) NpmAuditSignatures(ctx context.Context, dir string) error {
	if info, err := os.Stat(filepath.Join(t.abs(dir), "node_modules")); err != nil || !info.IsDir() {
		return nil
	}
	out, err := t.run(ctx, dir, nil, "npm", "audit", "signatures")
	if err != nil || out.exitCode != 0 {
		return toolFailure("npm audit signatures", out, err)
	}
	return nil
}
