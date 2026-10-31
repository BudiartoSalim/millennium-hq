package gitrepo

import (
	"bytes"
	"fmt"

	"millennium-code-qc/internal/domain"
)

// parseNameStatus reads `git diff --name-status -z --no-renames` output: a
// status letter and a path per entry, every field NUL-terminated.
func parseNameStatus(out []byte) ([]domain.ChangedFile, error) {
	fields := splitNUL(out)
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("unexpected git diff output: %d fields, want status/path pairs", len(fields))
	}
	files := make([]domain.ChangedFile, 0, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		kind, err := changeKind(fields[i])
		if err != nil {
			return nil, err
		}
		path, err := checkedPath(fields[i+1])
		if err != nil {
			return nil, err
		}
		files = append(files, domain.ChangedFile{Path: path, Kind: kind})
	}
	return files, nil
}

// parseAdded reads `git ls-files --others -z` output as added files.
func parseAdded(out []byte) ([]domain.ChangedFile, error) {
	fields := splitNUL(out)
	files := make([]domain.ChangedFile, 0, len(fields))
	for _, field := range fields {
		path, err := checkedPath(field)
		if err != nil {
			return nil, err
		}
		files = append(files, domain.ChangedFile{Path: path, Kind: domain.ChangeAdded})
	}
	return files, nil
}

// changeKind maps a status letter. A type change (T) and an unmerged path (U)
// still need checking, so both count as modified; anything else is a format
// this parser does not know and is refused.
func changeKind(status string) (domain.ChangeKind, error) {
	switch status {
	case "A":
		return domain.ChangeAdded, nil
	case "D":
		return domain.ChangeDeleted, nil
	case "M", "T", "U":
		return domain.ChangeModified, nil
	}
	return "", fmt.Errorf("unexpected git status %q", status)
}

func checkedPath(p string) (string, error) {
	if !domain.IsRepoRelative(p) {
		return "", fmt.Errorf("git reported a path outside the repository: %q", p)
	}
	return p, nil
}

func splitNUL(out []byte) []string {
	out = bytes.TrimSuffix(out, []byte{0})
	if len(out) == 0 {
		return nil
	}
	parts := bytes.Split(out, []byte{0})
	fields := make([]string, len(parts))
	for i, part := range parts {
		fields[i] = string(part)
	}
	return fields
}
