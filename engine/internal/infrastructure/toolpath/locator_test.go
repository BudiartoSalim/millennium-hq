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
