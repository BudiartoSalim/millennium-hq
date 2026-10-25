package configfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"millennium/internal/domain"
)

// ProposalVersion is the only proposal format version this engine reads.
const ProposalVersion = 1

// proposalDTO is a change an agent drafts and only the user applies:
// {"version": 1, "config": {...}}.
type proposalDTO struct {
	Version int             `json:"version"`
	Config  json.RawMessage `json:"config"`
}

// Codec reads proposals and writes configs.
type Codec struct{}

// ParseProposal validates a proposal as strictly as a config file and
// returns the config with its canonical, indented JSON.
func (Codec) ParseProposal(data []byte) (domain.Config, []byte, error) {
	if len(data) > maxConfigBytes {
		return domain.Config{}, nil, fmt.Errorf("the proposal is larger than %d bytes", maxConfigBytes)
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return domain.Config{}, nil, fmt.Errorf("proposal: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p proposalDTO
	if err := dec.Decode(&p); err != nil {
		return domain.Config{}, nil, fmt.Errorf("proposal: parse: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return domain.Config{}, nil, errors.New("proposal: unexpected data after the proposal object")
	}
	if p.Version != ProposalVersion {
		return domain.Config{}, nil, fmt.Errorf("proposal: version %d is not supported; this engine reads version %d", p.Version, ProposalVersion)
	}
	if len(p.Config) == 0 {
		return domain.Config{}, nil, errors.New(`proposal: "config" is required`)
	}
	in, err := decode(p.Config)
	if err != nil {
		return domain.Config{}, nil, fmt.Errorf("proposal config: %w", err)
	}
	cfg, err := domain.NewConfig(in)
	if err != nil {
		return domain.Config{}, nil, fmt.Errorf("proposal config: %w", err)
	}
	var canonical bytes.Buffer
	if err := json.Indent(&canonical, p.Config, "", "  "); err != nil {
		return domain.Config{}, nil, fmt.Errorf("proposal config: %w", err)
	}
	canonical.WriteByte('\n')
	return cfg, canonical.Bytes(), nil
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
