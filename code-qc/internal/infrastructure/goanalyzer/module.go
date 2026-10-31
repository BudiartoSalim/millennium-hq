package goanalyzer

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"millennium-code-qc/internal/domain"
)

// maxGoModBytes bounds the read of a go.mod; real ones are a few kilobytes.
const maxGoModBytes = 1 << 20

// module is the Go module a file belongs to: its path and the
// repository-relative directory of its go.mod.
type module struct {
	path string
	dir  string
}

// classify sorts an import into the standard library, this repository, or a
// third party. The module's own path is checked first, so a module whose
// path has no dot (common for internal services) is not mistaken for the
// standard library.
func (m module) classify(importPath string) (domain.ImportKind, string) {
	if importPath == m.path || strings.HasPrefix(importPath, m.path+"/") {
		rest := strings.TrimPrefix(strings.TrimPrefix(importPath, m.path), "/")
		return domain.ImportProject, path.Join(m.dir, rest)
	}
	first, _, _ := strings.Cut(importPath, "/")
	if strings.Contains(first, ".") {
		return domain.ImportExternal, ""
	}
	return domain.ImportStandard, ""
}

// moduleFinder locates the go.mod governing a file, caching per directory.
type moduleFinder struct {
	root  string
	cache map[string]module
}

func newModuleFinder(root string) *moduleFinder {
	return &moduleFinder{root: root, cache: map[string]module{}}
}

func (f *moduleFinder) forFile(file string) (module, error) {
	start := path.Dir(file)
	for dir := start; ; dir = path.Dir(dir) {
		if mod, ok := f.cache[dir]; ok {
			return mod, nil
		}
		mod, found, err := f.read(dir)
		if err != nil {
			return module{}, err
		}
		if found {
			f.cache[start], f.cache[dir] = mod, mod
			return mod, nil
		}
		if dir == "." {
			return module{}, fmt.Errorf("%s: no go.mod in its directory or any parent inside the repository", file)
		}
	}
}

func (f *moduleFinder) read(dir string) (module, bool, error) {
	goMod := filepath.Join(f.root, filepath.FromSlash(dir), "go.mod")
	info, err := os.Lstat(goMod)
	if errors.Is(err, fs.ErrNotExist) {
		return module{}, false, nil
	}
	if err != nil {
		return module{}, false, fmt.Errorf("inspect %s: %w", goMod, err)
	}
	if !info.Mode().IsRegular() {
		return module{}, false, fmt.Errorf("%s must be a regular file", goMod)
	}
	data, err := readLimited(goMod)
	if err != nil {
		return module{}, false, err
	}
	modPath, err := modulePath(data)
	if err != nil {
		return module{}, false, fmt.Errorf("%s: %w", goMod, err)
	}
	if dir == "." {
		dir = ""
	}
	return module{path: modPath, dir: dir}, true, nil
}

func readLimited(name string) ([]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxGoModBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if len(data) > maxGoModBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", name, maxGoModBytes)
	}
	return data, nil
}

// ModulePath reads the module directive of a go.mod, for other adapters
// that map import paths to files.
func ModulePath(data []byte) (string, error) { return modulePath(data) }

// modulePath reads the module directive. go.mod parsing proper lives in
// golang.org/x/mod, outside the standard library; the directive is simple
// enough to read directly: "module path", optionally quoted, with an
// optional trailing comment.
func modulePath(data []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "//")
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "module" {
			continue
		}
		value := fields[1]
		if strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "`") {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				return "", fmt.Errorf("malformed module directive %q", value)
			}
			value = unquoted
		}
		return value, nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("no module directive")
}
