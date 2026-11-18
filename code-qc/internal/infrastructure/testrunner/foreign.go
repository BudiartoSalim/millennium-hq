package testrunner

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"millennium-code-qc/internal/domain"
	"millennium-code-qc/internal/infrastructure/goanalyzer"
)

// maxGoSourceBytes bounds the read of one Go file for its line directives:
// far above any hand-written file, and above most generated ones.
const maxGoSourceBytes = 64 << 20

// foreignCoverage finds the code cover credits to another file. After a
// //line directive, cover credits the code to the base name the directive
// gives, in the directory of the file that holds it, so code is only ever
// credited within its own directory. Every directory the coverage names is
// read, each .go file in it included (test files, files no build includes),
// since an excluded file may lend coverage too.
func (r *Runner) foreignCoverage(coverage domain.Coverage) ([]domain.ForeignCoverage, error) {
	dirs := map[string]bool{}
	for file := range coverage {
		dirs[path.Dir(file)] = true
	}
	var foreign []domain.ForeignCoverage
	for _, dir := range slices.Sorted(maps.Keys(dirs)) {
		found, err := r.foreignIn(dir)
		if err != nil {
			return nil, fmt.Errorf("read the line directives of %s: %w", dir, err)
		}
		foreign = append(foreign, found...)
	}
	return foreign, nil
}

func (r *Runner) foreignIn(dir string) ([]domain.ForeignCoverage, error) {
	entries, err := os.ReadDir(r.abs(dir))
	if err != nil {
		return nil, err
	}
	var foreign []domain.ForeignCoverage
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		file := path.Join(dir, entry.Name())
		content, err := readGoSource(r.abs(file), maxGoSourceBytes)
		if err != nil {
			return nil, err
		}
		for _, directive := range goanalyzer.LineDirectives(content) {
			if directive.File == "" {
				continue
			}
			if credited := path.Join(dir, filepath.Base(directive.File)); credited != file {
				foreign = append(foreign, domain.ForeignCoverage{File: file, Directive: directive, Credited: credited})
			}
		}
	}
	return foreign, nil
}

// readGoSource reads a Go file as go build does, through a symlink too,
// refusing anything but a regular file (a FIFO would block the read) and
// anything over limit bytes.
func readGoSource(name string, limit int64) ([]byte, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
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
