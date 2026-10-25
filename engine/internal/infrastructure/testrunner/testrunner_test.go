package testrunner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"millennium/internal/domain"
)

func repoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const goLib = `package lib

func Covered(x int) int {
	if x > 0 {
		return 1
	}
	return 0
}

func Uncovered() int {
	return 2
}
`

func TestGoTestCoverage(t *testing.T) {
	root := repoWith(t, map[string]string{
		"svc/go.mod":          "module example.com/svc\n\ngo 1.25\n",
		"svc/lib/lib.go":      goLib,
		"svc/lib/lib_test.go": "package lib\n\nimport \"testing\"\n\nfunc TestCovered(t *testing.T) { if Covered(1) != 1 { t.Fatal() } }\n",
	})
	result, err := New(root, nil).GoTest(t.Context(), "svc")
	if err != nil || result.Failed {
		t.Fatalf("GoTest = %+v, %v", result, err)
	}
	lib := result.Coverage["svc/lib/lib.go"]
	if lib[4] == 0 || lib[5] == 0 || lib[11] != 0 {
		t.Fatalf("coverage of lib.go = %v; want lines 4-5 covered and 11 not (line 7, after the if, ran too: %d)", lib, lib[7])
	}
}

func TestGoTestFailure(t *testing.T) {
	failing := repoWith(t, map[string]string{
		"go.mod":      "module m\n\ngo 1.25\n",
		"lib.go":      "package m\n\nfunc F() int { return 1 }\n",
		"lib_test.go": "package m\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) { if F() != 2 { t.Fatal(\"F is not 2\") } }\n",
	})
	result, err := New(failing, nil).GoTest(t.Context(), "")
	if err != nil || !result.Failed || !strings.Contains(result.Summary, "--- FAIL: TestF") {
		t.Fatalf("GoTest = %+v, %v; want a failure naming TestF", result, err)
	}
	// With -coverpkg every package is built into every test binary, so one
	// package that does not compile fails the whole suite.
	broken := repoWith(t, map[string]string{
		"go.mod":           "module m\n\ngo 1.25\n",
		"ok.go":            "package m\n",
		"broken/b.go":      "package broken\n\nfunc G() { undefinedThing() }\n",
		"broken/b_test.go": "package broken\n",
	})
	result, err = New(broken, nil).GoTest(t.Context(), "")
	if err != nil || !result.Failed || !strings.Contains(result.Summary, "build failed") {
		t.Fatalf("GoTest = %+v, %v; want a build failure", result, err)
	}
}

func pythonCommand(t *testing.T) []string {
	t.Helper()
	path, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("python3 is required: %v", err)
	}
	return []string{path}
}

func TestPythonTest(t *testing.T) {
	root := repoWith(t, map[string]string{
		"app/tts/__init__.py":      "",
		"app/tts/domain.py":        "def covered(x):\n    if x:\n        return 1\n    return 0\n\n\ndef uncovered():\n    return 2\n",
		"app/tests/__init__.py":    "",
		"app/tests/test_domain.py": "import unittest\nfrom tts import domain\n\n\nclass T(unittest.TestCase):\n    def test_covered(self):\n        print('noise on stdout')\n        self.assertEqual(domain.covered(1), 1)\n",
	})
	result, err := New(root, pythonCommand(t)).PythonTest(t.Context(), "app")
	if err != nil || result.Failed {
		t.Fatalf("PythonTest = %+v, %v", result, err)
	}
	domainCov := result.Coverage["app/tts/domain.py"]
	if domainCov[2] == 0 || domainCov[3] == 0 || domainCov[8] != 0 {
		t.Fatalf("coverage of domain.py = %v; want 2-3 covered, 8 not", domainCov)
	}
	if _, ok := domainCov[5]; ok {
		t.Fatalf("blank line 5 must not count as executable: %v", domainCov)
	}
}

func TestPythonTestFailure(t *testing.T) {
	root := repoWith(t, map[string]string{
		"tests/test_x.py":      "import unittest\n\n\nclass T(unittest.TestCase):\n    def test_fails(self):\n        self.fail('nope')\n",
		"tests/test_broken.py": "import does_not_exist\n",
	})
	result, err := New(root, pythonCommand(t)).PythonTest(t.Context(), "")
	if err != nil || !result.Failed || !strings.Contains(result.Summary, "test_fails") {
		t.Fatalf("PythonTest = %+v, %v; want a failure", result, err)
	}
}

func TestCommandWithLCOV(t *testing.T) {
	root := repoWith(t, map[string]string{
		"web/run.sh":             "#!/bin/sh\nmkdir -p coverage\nprintf 'SF:src/a.ts\\nDA:1,1\\nDA:2,0\\nend_of_record\\nSF:/elsewhere/x.ts\\nDA:1,0\\nend_of_record\\n' > coverage/lcov.info\n",
		"web/fail.sh":            "#!/bin/sh\necho 'boom' >&2\nexit 3\n",
		"web/coverage/lcov.info": "SF:src/stale.ts\nDA:1,1\nend_of_record\n",
	})
	runner := New(root, nil)
	result, err := runner.Command(t.Context(), "web", domain.TestRecipe{Command: []string{"sh", "run.sh"}, LCOV: "coverage/lcov.info"})
	if err != nil || result.Failed {
		t.Fatalf("Command = %+v, %v", result, err)
	}
	if got := result.Coverage["web/src/a.ts"]; got[1] != 1 || got[2] != 0 || len(result.Coverage) != 1 {
		t.Fatalf("coverage = %v; want web/src/a.ts only, outside files ignored", result.Coverage)
	}
	failed, err := runner.Command(t.Context(), "web", domain.TestRecipe{Command: []string{"sh", "fail.sh"}, LCOV: "coverage/lcov.info"})
	if err != nil || !failed.Failed || !strings.Contains(failed.Summary, "boom") {
		t.Fatalf("failing command = %+v, %v", failed, err)
	}
	if _, err := runner.Command(t.Context(), "web", domain.TestRecipe{Command: []string{"true"}, LCOV: "coverage/lcov.info"}); err == nil {
		t.Fatal("a command that writes no report must be an error, even with a stale report present before")
	}
}

func TestParseGoProfile(t *testing.T) {
	profile := "mode: set\nexample.com/svc/lib/lib.go:3.24,4.11 1 1\nexample.com/svc/lib/lib.go:4.11,6.3 1 0\nother.com/x/y.go:1.1,2.2 1 1\n"
	coverage, err := parseGoProfile([]byte(profile), "example.com/svc", "svc")
	if err != nil {
		t.Fatalf("parseGoProfile: %v", err)
	}
	lib := coverage["svc/lib/lib.go"]
	if lib[3] != 1 || lib[4] != 1 || lib[5] != 0 || lib[6] != 0 || len(coverage) != 1 {
		t.Fatalf("coverage = %v (line 4 is shared and ran)", coverage)
	}
	if _, err := parseGoProfile([]byte("garbage"), "m", ""); err == nil {
		t.Fatal("a malformed profile must be an error")
	}
}
