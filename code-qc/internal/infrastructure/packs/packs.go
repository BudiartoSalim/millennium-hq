// Package packs reads and writes convention packs: a folder holding
// pack.json (the rules) and guide.md (the guidance for agents). The engine's
// own packs are embedded in the binary; a project's packs live in its
// .millennium/conventions, or beside a local config.
package packs

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"millennium-code-qc/internal/domain"
	"millennium-code-qc/internal/infrastructure/strictfile"
)

// The files of a pack folder.
const (
	PackFile  = "pack.json"
	GuideFile = "guide.md"
)

// Format is the only pack.json format version this engine reads.
const Format = 1

// Bounds on what the engine reads: a real pack.json is a few kilobytes, a
// guide at most domain.MaxGuideRunes characters of up to four bytes each.
const (
	maxPackBytes  = 64 << 10
	maxGuideBytes = 4 * domain.MaxGuideRunes
	maxPacks      = 32
)

// builtin holds the engine's packs; go:embed requires a package-level
// variable, and the embedded files are read-only.
//
//go:embed builtin
var builtin embed.FS

// Files are a pack's two files as they are on disk.
type Files struct {
	Pack  []byte
	Guide []byte
}

// Builtin parses and validates every embedded pack.
func Builtin() ([]domain.Pack, error) {
	return loadFS(builtin, "builtin")
}

func loadFS(files fs.FS, root string) ([]domain.Pack, error) {
	entries, err := fs.ReadDir(files, root)
	if err != nil {
		return nil, fmt.Errorf("list the built-in packs: %w", err)
	}
	var out []domain.Pack
	for _, entry := range entries {
		dir := root + "/" + entry.Name()
		pack, err := fs.ReadFile(files, dir+"/"+PackFile)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", dir, err)
		}
		guide, err := fs.ReadFile(files, dir+"/"+GuideFile)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", dir, err)
		}
		parsed, err := Decode(entry.Name(), Files{Pack: pack, Guide: guide}, domain.PackBuiltin)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

// ReadDir reads every pack folder in dir. A missing dir holds no packs;
// anything else in it that is not a pack folder is an error, so a broken or
// planted pack never goes unnoticed.
func ReadDir(dir string, origin domain.PackOrigin) ([]domain.Pack, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	if len(entries) > maxPacks {
		return nil, fmt.Errorf("%s holds %d entries; at most %d packs are read", dir, len(entries), maxPacks)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var out []domain.Pack
	for _, entry := range entries {
		pack, _, err := ReadFolder(filepath.Join(dir, entry.Name()), origin)
		if err != nil {
			return nil, err
		}
		out = append(out, pack)
	}
	return out, nil
}

// ReadFolder reads one pack folder, which must be a real directory (not a
// symlink) named like its pack, and returns the pack with its files.
func ReadFolder(dir string, origin domain.PackOrigin) (domain.Pack, Files, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return domain.Pack{}, Files{}, fmt.Errorf("inspect %s: %w", dir, err)
	}
	if !info.IsDir() {
		return domain.Pack{}, Files{}, fmt.Errorf("%s must be a pack folder holding %s and %s", dir, PackFile, GuideFile)
	}
	var files Files
	if files.Pack, err = strictfile.ReadRegular(filepath.Join(dir, PackFile), maxPackBytes); err != nil {
		return domain.Pack{}, Files{}, err
	}
	if files.Guide, err = strictfile.ReadRegular(filepath.Join(dir, GuideFile), maxGuideBytes); err != nil {
		return domain.Pack{}, Files{}, err
	}
	pack, err := Decode(filepath.Base(dir), files, origin)
	if err != nil {
		return domain.Pack{}, Files{}, fmt.Errorf("%s: %w", dir, err)
	}
	return pack, files, nil
}

// packDTO mirrors pack.json. schema/millennium-pack.schema.json must list
// exactly these fields.
type packDTO struct {
	Schema    string                         `json:"$schema,omitempty"`
	Format    int                            `json:"format"`
	Name      string                         `json:"name"`
	Languages []string                       `json:"languages"`
	Layers    []layerDTO                     `json:"layers"`
	Paths     map[string]map[string][]string `json:"paths"`
	Limits    *limitsDTO                     `json:"limits,omitempty"`
	Suggest   *suggestDTO                    `json:"suggest,omitempty"`
}

type layerDTO struct {
	Name       string   `json:"name"`
	MayImport  []string `json:"mayImport"`
	ThirdParty bool     `json:"thirdParty"`
	Tested     bool     `json:"tested"`
	Isolated   bool     `json:"isolated"`
}

type limitsDTO struct {
	FunctionLines *int `json:"functionLines,omitempty"`
	Nesting       *int `json:"nesting,omitempty"`
	Cognitive     *int `json:"cognitive,omitempty"`
}

type suggestDTO struct {
	Dependencies []string `json:"dependencies"`
}

// Decode validates a pack's files. folder is the name of the folder they
// came from, which must be the pack's name.
func Decode(folder string, files Files, origin domain.PackOrigin) (domain.Pack, error) {
	var dto packDTO
	if err := strictfile.DecodeJSON(files.Pack, &dto); err != nil {
		return domain.Pack{}, fmt.Errorf("%s: %w", PackFile, err)
	}
	if dto.Format != Format {
		return domain.Pack{}, fmt.Errorf("%s: format %d is not supported; this engine reads format %d", PackFile, dto.Format, Format)
	}
	if dto.Name != folder {
		return domain.Pack{}, fmt.Errorf("%s: the pack is named %q but its folder is %q; they must match", PackFile, dto.Name, folder)
	}
	in := domain.PackInput{Name: dto.Name, Origin: origin, Languages: dto.Languages, Paths: dto.Paths, Guide: string(files.Guide)}
	for _, layer := range dto.Layers {
		in.Layers = append(in.Layers, domain.PackLayer(layer))
	}
	if dto.Limits != nil {
		in.Limits = domain.LimitsInput(*dto.Limits)
	}
	if dto.Suggest != nil {
		in.Suggest = dto.Suggest.Dependencies
	}
	return domain.NewPack(in)
}

// Encode writes a pack as its files, every limit spelled out, so a copy shows
// all the settings it can change.
func Encode(pack domain.Pack) (Files, error) {
	dto := packDTO{Format: Format, Name: string(pack.Name), Paths: map[string]map[string][]string{}}
	for _, lang := range pack.Languages {
		dto.Languages = append(dto.Languages, string(lang))
	}
	for _, layer := range pack.Layers {
		l := layerDTO(layer)
		if l.MayImport == nil {
			l.MayImport = []string{}
		}
		dto.Layers = append(dto.Layers, l)
	}
	for lang, layers := range pack.Paths {
		dto.Paths[string(lang)] = map[string][]string{}
		for name, patterns := range layers {
			for _, p := range patterns {
				dto.Paths[string(lang)][name] = append(dto.Paths[string(lang)][name], p.String())
			}
		}
	}
	limits := pack.Limits
	dto.Limits = &limitsDTO{FunctionLines: &limits.FunctionLines, Nesting: &limits.Nesting, Cognitive: &limits.Cognitive}
	if len(pack.Suggest) > 0 {
		dto.Suggest = &suggestDTO{Dependencies: pack.Suggest}
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(dto); err != nil {
		return Files{}, fmt.Errorf("encode pack %s: %w", pack.Name, err)
	}
	return Files{Pack: out.Bytes(), Guide: []byte(pack.Guide)}, nil
}
