// Package packs provides the convention packs that ship inside the engine.
// They are embedded in the binary, so an agent cannot weaken a pack by
// editing files on disk.
package packs

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// builtin holds the pack definitions; go:embed requires a package-level
// variable, and the embedded files are read-only.
//
//go:embed builtin/*.json
var builtin embed.FS

// Source serves the builtin packs.
type Source struct {
	packs map[domain.Convention]domain.Pack
}

// Builtin parses and validates every embedded pack.
func Builtin() (*Source, error) {
	return load(builtin)
}

func load(files fs.FS) (*Source, error) {
	names, err := fs.Glob(files, "builtin/*.json")
	if err != nil {
		return nil, fmt.Errorf("list packs: %w", err)
	}
	source := &Source{packs: map[domain.Convention]domain.Pack{}}
	for _, name := range names {
		pack, err := parse(files, name)
		if err != nil {
			return nil, err
		}
		source.packs[pack.Name] = pack
	}
	return source, nil
}

// Pack returns the named pack or port.ErrPackUnavailable.
func (s *Source) Pack(name domain.Convention) (domain.Pack, error) {
	pack, ok := s.packs[name]
	if !ok {
		return domain.Pack{}, fmt.Errorf("%s: %w", name, port.ErrPackUnavailable)
	}
	return pack, nil
}

type packDTO struct {
	Name      string                         `json:"name"`
	Languages []string                       `json:"languages"`
	Layers    []layerDTO                     `json:"layers"`
	Paths     map[string]map[string][]string `json:"paths"`
}

type layerDTO struct {
	Name       string   `json:"name"`
	MayImport  []string `json:"mayImport"`
	ThirdParty bool     `json:"thirdParty"`
	Tested     bool     `json:"tested"`
	Isolated   bool     `json:"isolated"`
}

func parse(files fs.FS, name string) (domain.Pack, error) {
	data, err := fs.ReadFile(files, name)
	if err != nil {
		return domain.Pack{}, fmt.Errorf("read %s: %w", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var dto packDTO
	if err := dec.Decode(&dto); err != nil {
		return domain.Pack{}, fmt.Errorf("parse %s: %w", name, err)
	}
	in := domain.PackInput{Name: dto.Name, Languages: dto.Languages, Paths: dto.Paths}
	for _, layer := range dto.Layers {
		in.Layers = append(in.Layers, domain.PackLayer(layer))
	}
	return domain.NewPack(in)
}
