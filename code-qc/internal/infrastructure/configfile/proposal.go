package configfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
	"millennium-code-qc/internal/infrastructure/approvals"
	"millennium-code-qc/internal/infrastructure/packs"
	"millennium-code-qc/internal/infrastructure/strictfile"
)

// ProposalVersion is the only proposal format version this engine reads.
const ProposalVersion = 1

// proposalDTO is a change an agent drafts and only the user applies:
// {"version": 1, "config": {...}, "packs": {"<name>": "<folder>"},
// "approvals": [...]}, with at least one of config, packs and approvals.
type proposalDTO struct {
	Version   int               `json:"version"`
	Config    json.RawMessage   `json:"config"`
	Packs     map[string]string `json:"packs"`
	Approvals []json.RawMessage `json:"approvals"`
}

// Codec reads proposals and writes configs, packs and approval records.
type Codec struct{}

// ParseProposal validates a proposal as strictly as a config file. The pack
// folders it names are read relative to dir, the proposal's folder.
func (Codec) ParseProposal(data []byte, dir string) (port.Proposal, error) {
	p, err := decodeProposal(data)
	if err != nil {
		return port.Proposal{}, err
	}
	var out port.Proposal
	if len(p.Config) > 0 {
		cfg, canonical, err := parseProposalConfig(p.Config)
		if err != nil {
			return port.Proposal{}, err
		}
		out.Config, out.Canonical = &cfg, canonical
	}
	if out.Packs, err = readProposedPacks(p.Packs, dir); err != nil {
		return port.Proposal{}, err
	}
	for i, raw := range p.Approvals {
		approval, err := approvals.Decode(raw)
		if err != nil {
			return port.Proposal{}, fmt.Errorf("proposal approvals[%d]: %w", i, err)
		}
		out.Approvals = append(out.Approvals, approval)
	}
	return out, nil
}

// readProposedPacks reads each named pack folder, in name order. A folder is
// a relative path below the proposal's folder, and its pack must carry the
// name the proposal gives it.
func readProposedPacks(named map[string]string, dir string) ([]port.ProposedPack, error) {
	names := make([]string, 0, len(named))
	for name := range named {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []port.ProposedPack
	for _, name := range names {
		folder := named[name]
		if !filepath.IsLocal(filepath.FromSlash(folder)) {
			return nil, fmt.Errorf("proposal packs.%s: %q must be a folder below the proposal's own, without '..'", name, folder)
		}
		pack, files, err := packs.ReadFolder(filepath.Join(dir, filepath.FromSlash(folder)), domain.PackProject)
		if err != nil {
			return nil, fmt.Errorf("proposal packs.%s: %w", name, err)
		}
		if string(pack.Name) != name {
			return nil, fmt.Errorf("proposal packs.%s: the folder holds pack %q", name, pack.Name)
		}
		out = append(out, port.ProposedPack{Pack: pack, Files: port.PackFiles(files)})
	}
	return out, nil
}

func decodeProposal(data []byte) (proposalDTO, error) {
	if len(data) > maxConfigBytes {
		return proposalDTO{}, fmt.Errorf("the proposal is larger than %d bytes", maxConfigBytes)
	}
	var p proposalDTO
	if err := strictfile.DecodeJSON(data, &p); err != nil {
		return proposalDTO{}, fmt.Errorf("proposal: %w", err)
	}
	if p.Version != ProposalVersion {
		return proposalDTO{}, fmt.Errorf("proposal: version %d is not supported; this engine reads version %d", p.Version, ProposalVersion)
	}
	if len(p.Config) == 0 && len(p.Packs) == 0 && len(p.Approvals) == 0 {
		return proposalDTO{}, errors.New(`proposal: it needs "config", "packs", "approvals" or several of them`)
	}
	return p, nil
}

func parseProposalConfig(raw json.RawMessage) (domain.Config, []byte, error) {
	in, err := decode(raw)
	if err != nil {
		return domain.Config{}, nil, fmt.Errorf("proposal config: %w", err)
	}
	cfg, err := domain.NewConfig(in)
	if err != nil {
		return domain.Config{}, nil, fmt.Errorf("proposal config: %w", err)
	}
	var canonical bytes.Buffer
	if err := json.Indent(&canonical, raw, "", "  "); err != nil {
		return domain.Config{}, nil, fmt.Errorf("proposal config: %w", err)
	}
	canonical.WriteByte('\n')
	return cfg, canonical.Bytes(), nil
}

// FormatApproval renders an approval record file.
func (Codec) FormatApproval(a domain.Approval) ([]byte, error) { return approvals.Format(a) }

// FormatPack renders a pack's files, every limit spelled out.
func (Codec) FormatPack(pack domain.Pack) (port.PackFiles, error) {
	files, err := packs.Encode(pack)
	return port.PackFiles(files), err
}

// FormatProposal wraps config JSON as a proposal, for exporting a config
// so that applying it again reproduces it.
func (Codec) FormatProposal(config []byte) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString("{\n  \"version\": 1,\n  \"config\": ")
	if err := json.Indent(&out, bytes.TrimSpace(config), "  ", "  "); err != nil {
		return nil, fmt.Errorf("format the proposal: %w", err)
	}
	out.WriteString("\n}\n")
	return out.Bytes(), nil
}
