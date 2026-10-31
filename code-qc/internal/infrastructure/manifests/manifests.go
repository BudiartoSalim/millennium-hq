// Package manifests reads dependency manifests and lockfiles: which
// packages they lock, and where they fail to pin. It understands npm
// (package-lock.json v2/v3, package.json), Go (go.sum), Python
// (requirements*.txt and .in), GitHub Actions workflows, Dockerfiles and
// compose files.
package manifests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

var (
	exactVersion  = regexp.MustCompile(`^\d+\.\d+\.\d+([-+][0-9A-Za-z.-]+)?$`)
	requirement   = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)(\[[^\]]*\])?\s*==\s*([^\s;\\]+)`)
	actionUse     = regexp.MustCompile(`^\s*(-\s*)?uses:\s*["']?([^"'\s#]+)`)
	fullSHA       = regexp.MustCompile(`^[0-9a-f]{40}$`)
	fromLine      = regexp.MustCompile(`(?i)^\s*FROM\s+(--platform=\S+\s+)?(\S+)(\s+AS\s+(\S+))?`)
	composeImage  = regexp.MustCompile(`^\s*image:\s*["']?([^"'\s#]+)`)
	workflowPath  = regexp.MustCompile(`^\.github/workflows/[^/]+\.ya?ml$`)
	composeName   = regexp.MustCompile(`^(docker-)?compose[^/]*\.ya?ml$`)
	requirementFn = regexp.MustCompile(`^requirements[^/]*\.(txt|in)$`)
)

// Reader reads manifests.
type Reader struct{}

// Read returns the facts of a manifest, or false when p is not one.
func (Reader) Read(p string, content []byte) (port.ManifestFacts, bool, error) {
	name := path.Base(p)
	switch {
	case name == "package-lock.json":
		return npmLock(content)
	case name == "package.json":
		return packageJSON(content)
	case name == "go.sum":
		return goSum(content), true, nil
	case requirementFn.MatchString(name):
		return requirements(content, strings.HasSuffix(name, ".txt")), true, nil
	case workflowPath.MatchString(p):
		return workflow(content), true, nil
	case name == "Dockerfile" || strings.HasPrefix(name, "Dockerfile.") || strings.HasSuffix(name, ".Dockerfile"):
		return dockerfile(content), true, nil
	case composeName.MatchString(name):
		return compose(content), true, nil
	}
	return port.ManifestFacts{}, false, nil
}

type npmLockFile struct {
	LockfileVersion int `json:"lockfileVersion"`
	Packages        map[string]struct {
		Version       string `json:"version"`
		Link          bool   `json:"link"`
		HasInstallScr bool   `json:"hasInstallScript"`
	} `json:"packages"`
}

// npmLock reads the "packages" map of lockfile v2 and v3. Keys are install
// paths such as "node_modules/a/node_modules/b"; the package name is the
// part after the last "node_modules/". The root entry ("") and workspace
// links are the project itself, not dependencies.
func npmLock(content []byte) (port.ManifestFacts, bool, error) {
	var lock npmLockFile
	if err := json.Unmarshal(content, &lock); err != nil {
		return port.ManifestFacts{}, true, fmt.Errorf("read package-lock.json: %w", err)
	}
	if lock.LockfileVersion < 2 {
		return port.ManifestFacts{}, true, errors.New("package-lock.json lockfileVersion 1 is not supported; regenerate it with npm 7 or newer")
	}
	facts := port.ManifestFacts{Lockfile: true}
	for key, entry := range lock.Packages {
		i := strings.LastIndex(key, "node_modules/")
		if key == "" || entry.Link || i < 0 {
			continue
		}
		facts.Packages = append(facts.Packages, domain.LockedPackage{
			Package:       domain.Package{Ecosystem: domain.EcosystemNPM, Name: key[i+len("node_modules/"):], Version: entry.Version},
			InstallScript: entry.HasInstallScr,
		})
	}
	sortPackages(facts.Packages)
	return facts, true, nil
}

// packageJSON flags dependency specs that are not exact versions. Local
// packages (file:, workspace:, link:) are the project's own. Peer
// dependencies are ranges by nature and are not checked.
func packageJSON(content []byte) (port.ManifestFacts, bool, error) {
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(content, &manifest); err != nil {
		return port.ManifestFacts{}, true, fmt.Errorf("read package.json: %w", err)
	}
	var facts port.ManifestFacts
	for _, section := range []string{"dependencies", "devDependencies", "optionalDependencies"} {
		var deps map[string]string
		if raw, ok := manifest[section]; ok && json.Unmarshal(raw, &deps) != nil {
			return port.ManifestFacts{}, true, fmt.Errorf("read package.json: %s is not an object of strings", section)
		}
		for _, name := range sortedNames(deps) {
			if spec := deps[name]; !exactVersion.MatchString(spec) && !isLocalSpec(spec) {
				facts.Issues = append(facts.Issues, domain.LintIssue{Line: lineOf(content, `"`+name+`"`), Code: "unpinned",
					Message: fmt.Sprintf("%s %q in %s is not an exact version; pin it (save-exact=true in .npmrc)", name, spec, section)})
			}
		}
	}
	return facts, true, nil
}

func isLocalSpec(spec string) bool {
	for _, prefix := range []string{"file:", "workspace:", "link:"} {
		if strings.HasPrefix(spec, prefix) {
			return true
		}
	}
	return false
}

// goSum reads the "module version hash" lines of go.sum: the modules whose
// code the build downloads. "module version/go.mod hash" lines only pin a
// go.mod file read while resolving the module graph; that code never builds,
// so it needs no approval.
func goSum(content []byte) port.ManifestFacts {
	facts := port.ManifestFacts{Lockfile: true}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || strings.HasSuffix(fields[1], "/go.mod") {
			continue
		}
		facts.Packages = append(facts.Packages, domain.LockedPackage{Package: domain.Package{Ecosystem: domain.EcosystemGo, Name: fields[0], Version: fields[1]}})
	}
	sortPackages(facts.Packages)
	return facts
}

// requirements reads pip requirement files: every requirement must be
// pinned with ==, and in a .txt (the resolved set) carry --hash.
func requirements(content []byte, needHashes bool) port.ManifestFacts {
	facts := port.ManifestFacts{Lockfile: needHashes}
	for _, entry := range logicalLines(content) {
		text := strings.TrimSpace(entry.text)
		if text == "" || strings.HasPrefix(text, "#") || strings.HasPrefix(text, "-") || strings.HasPrefix(text, ".") {
			continue
		}
		m := requirement.FindStringSubmatch(text)
		if m == nil {
			facts.Issues = append(facts.Issues, domain.LintIssue{Line: entry.line, Code: "unpinned", Message: fmt.Sprintf("%q is not pinned with ==", text)})
			continue
		}
		if needHashes && !strings.Contains(text, "--hash=") {
			facts.Issues = append(facts.Issues, domain.LintIssue{Line: entry.line, Code: "unhashed", Message: fmt.Sprintf("%s has no --hash; install with --require-hashes", m[1])})
		}
		facts.Packages = append(facts.Packages, domain.LockedPackage{Package: domain.Package{Ecosystem: domain.EcosystemPyPI, Name: strings.ToLower(m[1]), Version: m[3]}})
	}
	return facts
}

type logicalLine struct {
	line int
	text string
}

// logicalLines joins backslash-continued lines, keeping the first line's
// number.
func logicalLines(content []byte) []logicalLine {
	var out []logicalLine
	var current *logicalLine
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for n := 1; scanner.Scan(); n++ {
		text := scanner.Text()
		if current == nil {
			out = append(out, logicalLine{line: n})
			current = &out[len(out)-1]
		}
		current.text += " " + strings.TrimSuffix(text, "\\")
		if !strings.HasSuffix(strings.TrimSpace(text), "\\") {
			current = nil
		}
	}
	return out
}

// workflow flags actions not pinned to a full commit SHA: a tag can be moved
// to malicious code after review.
func workflow(content []byte) port.ManifestFacts {
	var facts port.ManifestFacts
	for n, line := range strings.Split(string(content), "\n") {
		m := actionUse.FindStringSubmatch(line)
		if m == nil || strings.HasPrefix(m[2], "./") || strings.HasPrefix(m[2], "docker://") {
			continue
		}
		if _, ref, ok := strings.Cut(m[2], "@"); !ok || !fullSHA.MatchString(ref) {
			facts.Issues = append(facts.Issues, domain.LintIssue{Line: n + 1, Code: "unpinned-action",
				Message: fmt.Sprintf("%s is not pinned to a full commit SHA", m[2])})
		}
	}
	return facts
}

// dockerfile flags base images without a digest. A FROM naming an earlier
// build stage, or scratch, is not an image to pin.
func dockerfile(content []byte) port.ManifestFacts {
	var facts port.ManifestFacts
	stages := map[string]bool{"scratch": true}
	for n, line := range strings.Split(string(content), "\n") {
		m := fromLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		image := m[2]
		if !stages[strings.ToLower(image)] && !strings.Contains(image, "@sha256:") && !strings.HasPrefix(image, "$") {
			facts.Issues = append(facts.Issues, domain.LintIssue{Line: n + 1, Code: "unpinned-image", Message: fmt.Sprintf("%s has no @sha256 digest", image)})
		}
		if m[4] != "" {
			stages[strings.ToLower(m[4])] = true
		}
	}
	return facts
}

func compose(content []byte) port.ManifestFacts {
	var facts port.ManifestFacts
	for n, line := range strings.Split(string(content), "\n") {
		if m := composeImage.FindStringSubmatch(line); m != nil && !strings.Contains(m[1], "@sha256:") && !strings.HasPrefix(m[1], "$") {
			facts.Issues = append(facts.Issues, domain.LintIssue{Line: n + 1, Code: "unpinned-image", Message: fmt.Sprintf("%s has no @sha256 digest", m[1])})
		}
	}
	return facts
}

func lineOf(content []byte, needle string) int {
	for n, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, needle) {
			return n + 1
		}
	}
	return 0
}

func sortedNames(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortPackages(packages []domain.LockedPackage) {
	sort.Slice(packages, func(i, j int) bool { return packages[i].Key() < packages[j].Key() })
}
