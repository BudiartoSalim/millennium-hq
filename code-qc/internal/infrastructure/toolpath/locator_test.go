package toolpath

import "testing"

func TestLocate(t *testing.T) {
	var locator Locator
	if err := locator.Locate("go"); err != nil {
		t.Fatalf("go runs these tests, so it must be found: %v", err)
	}
	if err := locator.Locate("millennium-no-such-tool"); err == nil {
		t.Fatal("want an error for a missing tool")
	}
}

func TestFindAndPython(t *testing.T) {
	if Find("go") == "" {
		t.Fatal("go runs these tests, so Find must locate it")
	}
	if Find("millennium-no-such-tool") != "" {
		t.Fatal("a missing tool must yield an empty path")
	}
	// The value of Python depends on the machine; it must be nil or a
	// runnable command, never an empty program name.
	if cmd := Python(); cmd != nil && cmd[0] == "" {
		t.Fatalf("Python() = %q", cmd)
	}
}
