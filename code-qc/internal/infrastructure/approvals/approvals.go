// Package approvals reads the dependency approval records: one JSON file
// per approved package under .millennium/deps/<ecosystem>/ (and the same
// layout beside a local config). A record that cannot be read, or whose file
// name does not match what it approves, is an error, never ignored.
package approvals

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"millennium-code-qc/internal/domain"
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
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Approval{}, fmt.Errorf("read approval %s: %w", path, err)
	}
	if len(data) > maxRecordBytes {
		return domain.Approval{}, fmt.Errorf("approval %s is too large", path)
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

// Decode reads one approval record strictly.
func Decode(data []byte) (domain.Approval, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var dto recordDTO
	if err := dec.Decode(&dto); err != nil {
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
