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
