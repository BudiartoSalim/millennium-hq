// Package testrunner runs test suites and reads their line coverage: go test
// with a coverage profile, an embedded Python helper around unittest, and a
// component's own command writing an lcov report. Commands run without a
// shell and with a time limit.
package testrunner

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"millennium/internal/application/port"
	"millennium/internal/domain"
	"millennium/internal/infrastructure/goanalyzer"
)

//go:embed scripts/run_tests.py
var pythonRunner string

// suiteTimeout bounds one suite: a hanging test blocks instead of hanging
// the run.
const suiteTimeout = 15 * time.Minute

// Runner runs the suites of the repository at root.
type Runner struct {
	root   string
	python []string
}

// New returns a runner; python is the interpreter command, nil when none was
// found.
func New(root string, python []string) *Runner { return &Runner{root: root, python: python} }

// GoTest runs `go test -cover` for every package of the module in dir.
// -coverpkg=./... counts a line as covered when any test of the module runs
// it, not only the tests of its own package.
func (r *Runner) GoTest(ctx context.Context, dir string) (port.TestResult, error) {
	profile, cleanup, err := tempFile("cover-*.out")
	if err != nil {
		return port.TestResult{}, err
	}
	defer cleanup()
	out, code, err := r.exec(ctx, dir, []string{"GOFLAGS=-mod=readonly"}, "go", "test", "-count=1", "-covermode=set", "-coverpkg=./...", "-coverprofile="+profile, "./...")
	if err != nil {
		return port.TestResult{}, err
	}
	result := port.TestResult{Failed: code != 0, Summary: goSummary(out, code)}
	modulePath, err := r.modulePath(dir)
	if err != nil {
		return port.TestResult{}, err
	}
	data, err := os.ReadFile(profile)
	if err != nil {
		if result.Failed {
			// A suite that does not compile writes no profile.
			return result, nil
		}
		return port.TestResult{}, fmt.Errorf("read the coverage profile: %w", err)
	}
	result.Coverage, err = parseGoProfile(data, modulePath, dir)
	return result, err
}

// PythonTest runs the embedded unittest helper for the project in dir.
func (r *Runner) PythonTest(ctx context.Context, dir string) (port.TestResult, error) {
	if len(r.python) == 0 {
		return port.TestResult{}, errors.New("python3, python or py was not found on PATH, so the Python tests cannot run")
	}
	report, cleanup, err := tempFile("pytest-*.json")
	if err != nil {
		return port.TestResult{}, err
	}
	defer cleanup()
	args := append(append([]string{}, r.python[1:]...), "-I", "-c", pythonRunner, r.abs(dir), report)
	out, _, err := r.exec(ctx, dir, nil, r.python[0], args...)
	if err != nil {
		return port.TestResult{}, err
	}
	data, err := os.ReadFile(report)
	if err != nil || len(data) == 0 {
		return port.TestResult{}, fmt.Errorf("the Python test runner wrote no report: %s", tail(out))
	}
	return parsePythonReport(data, dir)
}

// Command runs a component's own test recipe and reads its lcov report.
func (r *Runner) Command(ctx context.Context, dir string, recipe domain.TestRecipe) (port.TestResult, error) {
	lcov := filepath.Join(r.abs(dir), filepath.FromSlash(recipe.LCOV))
	// A stale report from an earlier run must not pass for this one.
	if err := os.Remove(lcov); err != nil && !errors.Is(err, os.ErrNotExist) {
		return port.TestResult{}, fmt.Errorf("remove the old coverage report: %w", err)
	}
	out, code, err := r.exec(ctx, dir, nil, recipe.Command[0], recipe.Command[1:]...)
	if err != nil {
		return port.TestResult{}, err
	}
	result := port.TestResult{Failed: code != 0, Summary: tail(out)}
	data, err := os.ReadFile(lcov)
	if err != nil {
		if result.Failed {
			return result, nil
		}
		return port.TestResult{}, fmt.Errorf("the test command did not write %s: %w", recipe.LCOV, err)
	}
	result.Coverage, err = parseLCOV(data, r.root, r.abs(dir))
	return result, err
}

func (r *Runner) exec(ctx context.Context, dir string, env []string, name string, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, suiteTimeout)
	defer cancel()
	var output bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = r.abs(dir)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return "", 0, fmt.Errorf("%s did not finish within %s", name, suiteTimeout)
	case errors.As(err, &exitErr):
		return output.String(), exitErr.ExitCode(), nil
	case err != nil:
		return "", 0, fmt.Errorf("run %s: %w", name, err)
	}
	return output.String(), 0, nil
}

func (r *Runner) modulePath(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.abs(dir), "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	return goanalyzer.ModulePath(data)
}

func (r *Runner) abs(p string) string { return filepath.Join(r.root, filepath.FromSlash(p)) }

func tempFile(pattern string) (string, func(), error) {
	f, err := os.CreateTemp("", "millennium-"+pattern)
	if err != nil {
		return "", nil, fmt.Errorf("create a temporary file: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	return name, func() { _ = os.Remove(name) }, nil
}

// parseGoProfile reads "import/path/file.go:10.2,12.3 2 1" lines: a block
// from line 10 to 12 with 2 statements that ran once. A line is covered
// when any block on it ran.
func parseGoProfile(data []byte, modulePath, moduleDir string) (domain.Coverage, error) {
	coverage := domain.Coverage{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "mode:") || strings.TrimSpace(line) == "" {
			continue
		}
		file, first, last, hits, err := parseProfileLine(line)
		if err != nil {
			return nil, err
		}
		rel, ok := strings.CutPrefix(file, modulePath+"/")
		if !ok {
			continue
		}
		repoPath := path.Join(moduleDir, rel)
		if coverage[repoPath] == nil {
			coverage[repoPath] = domain.FileCoverage{}
		}
		for l := first; l <= last; l++ {
			coverage[repoPath][l] = max(coverage[repoPath][l], hits)
		}
	}
	return coverage, scanner.Err()
}

func parseProfileLine(line string) (file string, first, last, hits int, err error) {
	location, counts, ok := strings.Cut(line, " ")
	file, span, found := strings.Cut(location, ":")
	fields := strings.Fields(counts)
	if !ok || !found || len(fields) != 2 {
		return "", 0, 0, 0, fmt.Errorf("unexpected coverage profile line %q", line)
	}
	start, end, _ := strings.Cut(span, ",")
	first, err1 := strconv.Atoi(strings.Split(start, ".")[0])
	last, err2 := strconv.Atoi(strings.Split(end, ".")[0])
	hits, err3 := strconv.Atoi(fields[1])
	if err := errors.Join(err1, err2, err3); err != nil {
		return "", 0, 0, 0, fmt.Errorf("unexpected coverage profile line %q: %w", line, err)
	}
	return file, first, last, hits, nil
}

type pythonReport struct {
	Failed   bool                      `json:"failed"`
	Summary  string                    `json:"summary"`
	Coverage map[string]map[string]int `json:"coverage"`
}

func parsePythonReport(data []byte, dir string) (port.TestResult, error) {
	var report pythonReport
	if err := json.Unmarshal(data, &report); err != nil {
		return port.TestResult{}, fmt.Errorf("read the Python test report: %w", err)
	}
	coverage := domain.Coverage{}
	for file, lines := range report.Coverage {
		repoPath := path.Join(dir, file)
		if !domain.IsRepoRelative(repoPath) {
			return port.TestResult{}, fmt.Errorf("the Python test report names a file outside the repository: %q", file)
		}
		coverage[repoPath] = domain.FileCoverage{}
		for lineText, hits := range lines {
			line, err := strconv.Atoi(lineText)
			if err != nil {
				return port.TestResult{}, fmt.Errorf("the Python test report has a bad line number %q", lineText)
			}
			coverage[repoPath][line] = hits
		}
	}
	return port.TestResult{Failed: report.Failed, Summary: report.Summary, Coverage: coverage}, nil
}

// parseLCOV reads "SF:<file>" and "DA:<line>,<hits>" records. Source paths
// are absolute or relative to the directory the command ran in.
func parseLCOV(data []byte, root, dir string) (domain.Coverage, error) {
	coverage := domain.Coverage{}
	var current domain.FileCoverage
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "SF:"):
			current = coverageFor(coverage, root, dir, strings.TrimPrefix(line, "SF:"))
		case strings.HasPrefix(line, "DA:") && current != nil:
			lineText, hitsText, _ := strings.Cut(strings.TrimPrefix(line, "DA:"), ",")
			hitsText, _, _ = strings.Cut(hitsText, ",")
			number, err1 := strconv.Atoi(lineText)
			hits, err2 := strconv.Atoi(hitsText)
			if err := errors.Join(err1, err2); err != nil {
				return nil, fmt.Errorf("unexpected lcov line %q", line)
			}
			current[number] = max(current[number], hits)
		case line == "end_of_record":
			current = nil
		}
	}
	return coverage, scanner.Err()
}

// coverageFor returns the coverage of an lcov source file, or nil for one
// outside the repository, whose lines are ignored.
func coverageFor(coverage domain.Coverage, root, dir, source string) domain.FileCoverage {
	abs := source
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(dir, filepath.FromSlash(source))
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil
	}
	key := filepath.ToSlash(rel)
	if coverage[key] == nil {
		coverage[key] = domain.FileCoverage{}
	}
	return coverage[key]
}

func goSummary(out string, code int) string {
	if code == 0 {
		return "all tests passed"
	}
	var failing []string
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--- FAIL") || strings.HasPrefix(trimmed, "FAIL") || strings.HasPrefix(trimmed, "panic:") {
			failing = append(failing, trimmed)
		}
	}
	if len(failing) == 0 {
		return tail(out)
	}
	return strings.Join(failing[:min(len(failing), 20)], "; ")
}

func tail(out string) string {
	out = strings.TrimSpace(out)
	if len(out) > 1500 {
		return "..." + out[len(out)-1500:]
	}
	return out
}
