package gate

import (
	"testing"

	"millennium-code-qc/internal/application/port"
)

// Every gate lists its rules; the config's disable entries are checked
// against these lists, and the run treats an undeclared rule as a bug.
func TestEveryGateDeclaresItsRules(t *testing.T) {
	gates := []port.Gate{
		NewToolchain(nil), NewStructure(nil, nil, nil), NewShell(nil, nil),
		NewHygiene(nil, nil), NewBuild(nil, nil), NewTests(nil, nil, nil, nil, nil), NewDeps(nil, nil, nil, nil, nil, nil),
	}
	for _, g := range gates {
		rules := g.Rules()
		if len(rules) == 0 {
			t.Errorf("%s declares no rules", g.Name())
		}
		seen := map[string]bool{}
		for _, r := range rules {
			if r.Name == "" || seen[r.Name] {
				t.Errorf("%s: rule %q is empty or declared twice", g.Name(), r.Name)
			}
			seen[r.Name] = true
		}
	}
}
