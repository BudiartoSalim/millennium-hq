// Package toolpath finds executables on PATH.
package toolpath

import "os/exec"

// Locator looks executables up with exec.LookPath, which refuses results that
// resolve relative to the current directory.
type Locator struct{}

// Locate returns an error when name cannot be found or run.
func (Locator) Locate(name string) error {
	_, err := exec.LookPath(name)
	return err
}

// Find returns the path of the named executable, or "" when it cannot be
// found.
func Find(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

// Python returns the command that runs Python 3: python3 on Linux and macOS,
// python or the py launcher on native Windows, or nil when none is found.
func Python() []string {
	for _, name := range []string{"python3", "python"} {
		if path := Find(name); path != "" {
			return []string{path}
		}
	}
	if path := Find("py"); path != "" {
		return []string{path, "-3"}
	}
	return nil
}
