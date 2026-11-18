package goanalyzer

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"millennium-code-qc/internal/domain"
)

// maxGoSourceBytes bounds the read of one Go file for its line directives:
// far above any hand-written file, and above most generated ones.
const maxGoSourceBytes = 64 << 20

// Sources reads the Go sources of a repository's working tree.
type Sources struct {
	root  string
	limit int64
}

// NewSources returns the Go sources of the repository at root.
func NewSources(root string) *Sources { return &Sources{root: root, limit: maxGoSourceBytes} }

// GoForeignCoverage finds the code of the Go sources in the
// repository-relative directory dir that cover credits to another file.
// After a //line directive, cover credits the code to the base name the
// directive gives, in the directory of the file that holds it, so code is
// only ever credited within its own directory. Every source go build may
// read counts, test files and files a build constraint leaves out
// included, since an excluded file may lend coverage too. What go build
// never reads lends nothing: a name starting with _ or ., and a directory,
// through a symlink too.
func (s *Sources) GoForeignCoverage(dir string) ([]domain.ForeignCoverage, error) {
	if dir != "." && !domain.IsRepoRelative(dir) {
		return nil, fmt.Errorf("%q is not a repository-relative directory", dir)
	}
	entries, err := os.ReadDir(s.abs(dir))
	if err != nil {
		return nil, fmt.Errorf("list the Go files of %s: %w", dir, err)
	}
	var foreign []domain.ForeignCoverage
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		file := path.Join(dir, name)
		content, err := readGoSource(s.abs(file), s.limit)
		if err != nil {
			return nil, fmt.Errorf("read the line directives of %s: %w", file, err)
		}
		foreign = append(foreign, lentBy(dir, file, content)...)
	}
	return foreign, nil
}

// lentBy lists the code of file, in dir, that cover credits to another
// file.
func lentBy(dir, file string, content []byte) []domain.ForeignCoverage {
	var foreign []domain.ForeignCoverage
	for _, directive := range LineDirectives(content) {
		if directive.File == "" {
			continue
		}
		if credited := path.Join(dir, filepath.Base(directive.File)); credited != file {
			foreign = append(foreign, domain.ForeignCoverage{File: file, Directive: directive, Credited: credited})
		}
	}
	return foreign
}

func (s *Sources) abs(p string) string { return filepath.Join(s.root, filepath.FromSlash(p)) }

// readGoSource reads a Go file as go build does, through a symlink too. A
// directory is no source, so it gives nothing; anything else but a regular
// file (a FIFO would block the read) and anything over limit bytes is an
// error.
func readGoSource(name string, limit int64) ([]byte, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, nil
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file; remove it, or give it a name go build ignores (one starting with _)", name)
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes, so whether its line directives lend coverage to the files beside it cannot be checked; split it", name, limit)
	}
	return data, nil
}
