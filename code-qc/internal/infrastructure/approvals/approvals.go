// Package approvals reads the dependency approval records: one JSON file
// per approved package under .millennium/deps/<ecosystem>/ (and the same
// layout beside a local config). A record that cannot be read, or whose file
// name does not match what it approves, is an error, never ignored.
package approvals

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"millennium-code-qc/internal/domain"
	"millennium-code-qc/internal/infrastructure/strictfile"
)

const maxRecordBytes = 1 << 20

// Store reads approval records from directories.
type Store struct {
	dirs []string
}

// New returns a store over dirs; a missing directory holds no records.
func New(dirs ...string) *Store { return &Store{dirs: dirs} }

type recordDTO struct {
	Ecosystem     string   `json:"ecosystem"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Decision      string   `json:"decision"`
	Reason        string   `json:"reason"`
	Checked       string   `json:"checked"`
	Transitive    []string `json:"transitive"`
	SecurityPatch string   `json:"securityPatch"`
}

// Approvals reads every record.
func (s *Store) Approvals() (domain.Approvals, error) {
	var all domain.Approvals
	for _, dir := range s.dirs {
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".json") {
				return err
			}
			approval, err := readRecord(dir, path)
			if err != nil {
				return err
			}
			all = append(all, approval)
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return all, nil
}

func readRecord(dir, path string) (domain.Approval, error) {
	// A record is a rule file: a symlink could pull approval from outside
	// the reviewed tree, and an oversized one is not read into memory.
	data, err := strictfile.ReadRegular(path, maxRecordBytes)
	if err != nil {
		return domain.Approval{}, fmt.Errorf("read approval: %w", err)
	}
	approval, err := Decode(data)
	if err != nil {
		return domain.Approval{}, fmt.Errorf("approval %s: %w", path, err)
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil || filepath.ToSlash(rel) != domain.ApprovalFile(approval.Package) {
		return domain.Approval{}, fmt.Errorf("approval %s approves %s, so it must be named %s", path, approval, domain.ApprovalFile(approval.Package))
	}
	return approval, nil
}

// Decode reads one approval record as strictly as a config: no unknown
// fields (an error names the allowed ones), duplicate keys or trailing data.
func Decode(data []byte) (domain.Approval, error) {
	var dto recordDTO
	if err := strictfile.DecodeJSON(data, &dto); err != nil {
		return domain.Approval{}, err
	}
	return domain.NewApproval(domain.ApprovalInput(dto))
}

// Format renders an approval as the record file `config apply` writes.
func Format(a domain.Approval) ([]byte, error) {
	dto := recordDTO{
		Ecosystem: string(a.Ecosystem), Name: a.Name, Version: a.Version, Decision: "approved",
		Reason: a.Reason, Checked: a.Checked, Transitive: a.Transitive, SecurityPatch: a.SecurityPatch,
	}
	data, err := json.MarshalIndent(dto, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
