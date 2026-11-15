// Package configfile reads .millennium/config.json strictly: unknown fields,
// duplicate keys, trailing data, oversized files and anything that is not a
// regular file are all errors, never ignored.
package configfile

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
	"millennium-code-qc/internal/infrastructure/packs"
	"millennium-code-qc/internal/infrastructure/strictfile"
)

// maxConfigBytes bounds what the engine reads; a real config is a few hundred
// bytes.
const maxConfigBytes = 64 << 10

// PacksDir is the folder beside a config that holds the project's own
// convention packs: .millennium/conventions for a committed config.
const PacksDir = "conventions"

// Loader loads the config from one path, with the packs beside it.
type Loader struct {
	path    string
	builtin []domain.Pack
	origin  domain.PackOrigin
}

// NewLoader returns a loader for the config file at path. builtin are the
// engine's packs; origin says whose the packs beside the config are.
func NewLoader(path string, builtin []domain.Pack, origin domain.PackOrigin) *Loader {
	return &Loader{path: path, builtin: builtin, origin: origin}
}

// PacksPath is the folder of the packs beside the config at configPath.
func PacksPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), PacksDir)
}

// Raw returns the config file's bytes as they are, read as strictly as Load
// reads them.
func (l *Loader) Raw() ([]byte, error) { return readRegularFile(l.path) }

// Packs returns the engine's packs and those beside the config.
func (l *Loader) Packs() (domain.Catalog, error) {
	project, err := packs.ReadDir(PacksPath(l.path), l.origin)
	if err != nil {
		return nil, err
	}
	return domain.NewCatalog(l.builtin, project)
}

// Load reads, decodes and validates the config, and resolves its
// conventions against the packs. It returns port.ErrConfigNotFound only
// when the file does not exist.
func (l *Loader) Load() (domain.Config, error) {
	data, err := readRegularFile(l.path)
	if err != nil {
		return domain.Config{}, err
	}
	in, err := decode(data)
	if err != nil {
		return domain.Config{}, fmt.Errorf("%s: %w", l.path, err)
	}
	cfg, err := domain.NewConfig(in)
	if err != nil {
		return domain.Config{}, fmt.Errorf("%s: %w", l.path, err)
	}
	catalog, err := l.Packs()
	if err != nil {
		return domain.Config{}, fmt.Errorf("%s: %w", PacksPath(l.path), err)
	}
	resolved, err := cfg.Resolve(catalog)
	if err != nil {
		return domain.Config{}, fmt.Errorf("%s: %w", l.path, err)
	}
	return resolved, nil
}

// readRegularFile reads the config, refusing anything but a regular file,
// so it cannot be redirected to a file outside the reviewed tree.
func readRegularFile(path string) ([]byte, error) {
	data, err := strictfile.ReadRegular(path, maxConfigBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, port.ErrConfigNotFound
	}
	return data, err
}

func decode(data []byte) (domain.ConfigInput, error) {
	var dto configDTO
	if err := strictfile.DecodeJSON(data, &dto); err != nil {
		return domain.ConfigInput{}, err
	}
	return dto.toInput(), nil
}
