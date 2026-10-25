package configfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"millennium/internal/application/port"
	"millennium/internal/domain"
	"millennium/internal/infrastructure/approvals"
)

// ProposalVersion is the only proposal format version this engine reads.
const ProposalVersion = 1

// proposalDTO is a change an agent drafts and only the user applies:
// {"version": 1, "config": {...}, "approvals": [...]}, with at least one of
// config and approvals.
type proposalDTO struct {
	Version   int               `json:"version"`
	Config    json.RawMessage   `json:"config"`
	Approvals []json.RawMessage `json:"approvals"`
}

// Codec reads proposals and writes configs and approval records.
type Codec struct{}

// ParseProposal validates a proposal as strictly as a config file.
func (Codec) ParseProposal(data []byte) (port.Proposal, error) {
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
	for i, raw := range p.Approvals {
		approval, err := approvals.Decode(raw)
		if err != nil {
			return port.Proposal{}, fmt.Errorf("proposal approvals[%d]: %w", i, err)
		}
		out.Approvals = append(out.Approvals, approval)
	}
	return out, nil
}

func decodeProposal(data []byte) (proposalDTO, error) {
	if len(data) > maxConfigBytes {
		return proposalDTO{}, fmt.Errorf("the proposal is larger than %d bytes", maxConfigBytes)
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return proposalDTO{}, fmt.Errorf("proposal: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p proposalDTO
	if err := dec.Decode(&p); err != nil {
		return proposalDTO{}, fmt.Errorf("proposal: parse: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return proposalDTO{}, errors.New("proposal: unexpected data after the proposal object")
	}
	if p.Version != ProposalVersion {
		return proposalDTO{}, fmt.Errorf("proposal: version %d is not supported; this engine reads version %d", p.Version, ProposalVersion)
	}
	if len(p.Config) == 0 && len(p.Approvals) == 0 {
		return proposalDTO{}, errors.New(`proposal: it needs "config", "approvals" or both`)
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
