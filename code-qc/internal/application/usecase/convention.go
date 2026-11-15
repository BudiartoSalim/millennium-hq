package usecase

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// DefaultDraftSource is the pack `convention new` copies when none is named.
const DefaultDraftSource domain.Convention = "clean-architecture"

// ErrUnknownConvention means no available pack has the name.
var ErrUnknownConvention = errors.New("no convention pack has this name")

// Draft is a new convention pack written for the user to edit and propose.
type Draft struct {
	Pack domain.Pack
	// Dir is the draft's folder, relative to the repository root.
	Dir string
	// Proposal is a proposal that adds the draft as it is.
	Proposal string
}

// Conventions lists, shows and drafts convention packs.
type Conventions struct {
	committed port.PackCatalog
	local     port.PackCatalog
	source    port.SourceReader
	codec     port.ProposalCodec
	writer    port.ProjectWriter
	root      string
}

// NewConventions returns the conventions use-case for the repository at
// root.
func NewConventions(committed, local port.PackCatalog, source port.SourceReader, codec port.ProposalCodec, writer port.ProjectWriter, root string) *Conventions {
	return &Conventions{committed: committed, local: local, source: source, codec: codec, writer: writer, root: root}
}

// List returns every pack available beside the target's config, in name
// order.
func (u *Conventions) List(target ApplyTarget) ([]domain.Pack, error) {
	catalog, err := u.catalog(target)
	if err != nil {
		return nil, err
	}
	return catalog.Packs(), nil
}

// Show returns one pack.
func (u *Conventions) Show(target ApplyTarget, name domain.Convention) (domain.Pack, error) {
	catalog, err := u.catalog(target)
	if err != nil {
		return domain.Pack{}, err
	}
	pack, ok := catalog[name]
	if !ok {
		return domain.Pack{}, fmt.Errorf("%w: %s; available: %v", ErrUnknownConvention, name, catalog.Names())
	}
	return pack, nil
}

// New writes a copy of the pack from under a new name to
// millennium.proposal/conventions/<name>, for the user to edit and propose.
// It never overwrites a draft or reuses an existing pack's name.
func (u *Conventions) New(target ApplyTarget, name, from domain.Convention) (Draft, error) {
	if !domain.ValidConventionName(string(name)) {
		return Draft{}, fmt.Errorf("%q is not a convention name: use lowercase letters, digits and dashes", name)
	}
	catalog, err := u.catalog(target)
	if err != nil {
		return Draft{}, err
	}
	if _, taken := catalog[name]; taken {
		return Draft{}, fmt.Errorf("convention %s already exists; choose a new name for the copy", name)
	}
	pack, ok := catalog[from]
	if !ok {
		return Draft{}, fmt.Errorf("%w: %s; available: %v", ErrUnknownConvention, from, catalog.Names())
	}
	pack.Name, pack.Origin = name, domain.PackProject
	dir := path.Join(ProposalDir, "conventions", string(name))
	if _, err := u.source.ReadWorking(path.Join(dir, "pack.json")); err == nil {
		return Draft{}, fmt.Errorf("%s already holds a draft; edit it, or remove it first", dir)
	}
	files, err := u.codec.FormatPack(pack)
	if err != nil {
		return Draft{}, err
	}
	abs := filepath.Join(u.root, filepath.FromSlash(dir))
	if err := u.writer.WriteFile(filepath.Join(abs, "pack.json"), files.Pack); err != nil {
		return Draft{}, err
	}
	if err := u.writer.WriteFile(filepath.Join(abs, "guide.md"), files.Guide); err != nil {
		return Draft{}, err
	}
	proposal := fmt.Sprintf("{\n  \"version\": 1,\n  \"packs\": {\"%s\": \"%s\"}\n}\n", name, dir)
	return Draft{Pack: pack, Dir: dir, Proposal: proposal}, nil
}

func (u *Conventions) catalog(target ApplyTarget) (domain.Catalog, error) {
	source := u.committed
	if target == ApplyLocal {
		source = u.local
	}
	catalog, err := source.Packs()
	if err != nil {
		return nil, fmt.Errorf("read the convention packs: %w", err)
	}
	return catalog, nil
}
