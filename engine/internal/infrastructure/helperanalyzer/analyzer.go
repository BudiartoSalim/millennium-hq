// Package helperanalyzer runs the TypeScript and Python analyzers. Each is a
// small script embedded in the engine, run with the project's own runtime
// (node with the project's typescript package; python3 with only its
// standard library). One process serves a whole run over a line-delimited
// JSON protocol, and its answers are validated like any untrusted input.
package helperanalyzer

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"millennium/internal/domain"
)

//go:embed scripts/analyze.js
var typeScriptScript string

//go:embed scripts/analyze.py
var pythonScript string

// requestTimeout bounds one file's analysis; a helper that hangs blocks the
// file instead of the run.
const requestTimeout = 60 * time.Second

// maxResponseBytes bounds one answer line.
const maxResponseBytes = 16 << 20

// Analyzer is a lazily started helper process.
type Analyzer struct {
	language string
	argv     []string
	proc     *process
	failure  error
}

// TypeScript returns the TypeScript analyzer for the repository at root, run
// with the node executable at nodePath. An empty nodePath means node was not
// found; every analysis then fails with that reason.
func TypeScript(root, nodePath string) *Analyzer {
	a := &Analyzer{language: "typescript"}
	if nodePath == "" {
		a.failure = errors.New("node was not found on PATH, so TypeScript cannot be analysed")
		return a
	}
	a.argv = []string{nodePath, "-e", typeScriptScript, "--", root}
	return a
}

// Python returns the Python analyzer for the repository at root, run with
// the given interpreter command, for example ["python3"] or ["py", "-3"].
// -I isolates the interpreter from the repository's own modules and the
// PYTHON* environment, so a planted json.py cannot run.
func Python(root string, python []string) *Analyzer {
	a := &Analyzer{language: "python"}
	if len(python) == 0 {
		a.failure = errors.New("python3, python or py was not found on PATH, so Python cannot be analysed")
		return a
	}
	a.argv = append(append([]string{}, python...), "-I", "-c", pythonScript, root)
	return a
}

// Analyze asks the helper for the facts of one file.
func (a *Analyzer) Analyze(ctx context.Context, path string, content []byte) (domain.FileFacts, error) {
	if a.failure != nil {
		return domain.FileFacts{}, a.failure
	}
	if a.proc == nil {
		proc, err := start(a.argv)
		if err != nil {
			a.failure = fmt.Errorf("start the %s analyzer: %w", a.language, err)
			return domain.FileFacts{}, a.failure
		}
		a.proc = proc
	}
	line, err := a.proc.exchange(ctx, request{Path: path, Content: string(content)})
	if err != nil {
		// A helper that died or hung cannot be trusted with the next file.
		a.failure = fmt.Errorf("the %s analyzer stopped: %w", a.language, err)
		a.Close()
		return domain.FileFacts{}, a.failure
	}
	return decode(path, line)
}

// Close stops the helper process.
func (a *Analyzer) Close() {
	if a.proc != nil {
		a.proc.stop()
		a.proc = nil
	}
}

type request struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan readResult
	stderr *limitedBuffer
}

type readResult struct {
	line []byte
	err  error
}

func start(argv []string) (*process, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = os.TempDir()
	cmd.Env = helperEnv()
	stderr := &limitedBuffer{limit: 8 << 10}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &process{cmd: cmd, stdin: stdin, lines: make(chan readResult), stderr: stderr}
	go p.readLines(stdout)
	return p, nil
}

// helperEnv drops variables that would make node or python load extra code.
func helperEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name == "NODE_OPTIONS" || name == "NODE_PATH" || strings.HasPrefix(name, "PYTHON") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func (p *process) readLines(stdout io.Reader) {
	reader := bufio.NewReaderSize(stdout, 64<<10)
	for {
		line, err := readLine(reader)
		p.lines <- readResult{line: line, err: err}
		if err != nil {
			close(p.lines)
			return
		}
	}
}

func readLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxResponseBytes {
			return nil, fmt.Errorf("answer longer than %d bytes", maxResponseBytes)
		}
		if err == nil {
			return line, nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
	}
}

func (p *process) exchange(ctx context.Context, req request) ([]byte, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := p.stdin.Write(append(payload, '\n')); err != nil {
		return nil, p.describe(err)
	}
	timer := time.NewTimer(requestTimeout)
	defer timer.Stop()
	select {
	case result, ok := <-p.lines:
		if !ok {
			return nil, p.describe(io.EOF)
		}
		if result.err != nil {
			return nil, p.describe(result.err)
		}
		return result.line, nil
	case <-timer.C:
		return nil, fmt.Errorf("no answer within %s", requestTimeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *process) describe(err error) error {
	if tail := strings.TrimSpace(p.stderr.String()); tail != "" {
		return fmt.Errorf("%w: %s", err, tail)
	}
	return err
}

func (p *process) stop() {
	_ = p.stdin.Close()
	_ = p.cmd.Process.Kill()
	_ = p.cmd.Wait()
	// Drain so the reader goroutine can finish.
	for range p.lines {
	}
}

// limitedBuffer keeps the first limit bytes of the helper's stderr for error
// messages and discards the rest.
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string { return b.buf.String() }
