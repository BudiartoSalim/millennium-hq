// Package shelltools runs bash's own parser and shellcheck on scripts. Every
// run is a plain exec with explicit arguments; shellcheck runs with --norc,
// so a .shellcheckrc in the repository cannot switch checks off, and with -x
// and --source-path=SCRIPTDIR so sourced files resolve as in an editor.
package shelltools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"millennium/internal/domain"
)

// bashLine finds the line number in a bash -n message:
// "x.sh: line 3: syntax error near unexpected token `fi'".
var bashLine = regexp.MustCompile(`line (\d+): (.*)`)

// Tools runs the shell toolchain for scripts of the repository at root.
type Tools struct {
	root string
}

// New returns the tools for the repository at root.
func New(root string) *Tools { return &Tools{root: root} }

// Syntax runs `bash -n`, which parses without executing.
func (t *Tools) Syntax(ctx context.Context, p string) ([]domain.LintIssue, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "bash", "-n", t.abs(p))
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return nil, nil
	case !errors.As(err, &exitErr):
		return nil, fmt.Errorf("run bash -n on %s: %w", p, err)
	}
	return syntaxIssues(stderr.String()), nil
}

func syntaxIssues(stderr string) []domain.LintIssue {
	var issues []domain.LintIssue
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if m := bashLine.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			issues = append(issues, domain.LintIssue{Line: n, Code: "syntax", Message: m[2]})
		}
	}
	if len(issues) == 0 {
		issues = append(issues, domain.LintIssue{Code: "syntax", Message: strings.TrimSpace(stderr)})
	}
	return issues
}

type shellcheckReport struct {
	Comments []struct {
		Line    int    `json:"line"`
		Code    int    `json:"code"`
		Level   string `json:"level"`
		Message string `json:"message"`
	} `json:"comments"`
}

// Lint runs shellcheck. It exits 1 when it has findings, which is not a
// failure; any other non-zero status is.
func (t *Tools) Lint(ctx context.Context, p, dialect string) ([]domain.LintIssue, error) {
	args := []string{"--format=json1", "--norc", "--external-sources", "--source-path=SCRIPTDIR"}
	if dialect != "" {
		args = append(args, "--shell="+dialect)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "shellcheck", append(args, t.abs(p))...)
	cmd.Dir = t.root
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !(errors.As(err, &exitErr) && exitErr.ExitCode() == 1) {
		return nil, fmt.Errorf("run shellcheck on %s: %w: %s", p, err, strings.TrimSpace(stderr.String()))
	}
	var report shellcheckReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		return nil, fmt.Errorf("read shellcheck output for %s: %w", p, err)
	}
	issues := make([]domain.LintIssue, 0, len(report.Comments))
	for _, c := range report.Comments {
		issues = append(issues, domain.LintIssue{
			Line:    c.Line,
			Code:    fmt.Sprintf("SC%d", c.Code),
			Message: fmt.Sprintf("%s (%s)", c.Message, c.Level),
		})
	}
	return issues, nil
}

func (t *Tools) abs(p string) string { return filepath.Join(t.root, filepath.FromSlash(p)) }
