package domain

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
)

// Ecosystem is a package registry the supply-chain gate understands.
type Ecosystem string

// Supported ecosystems.
const (
	EcosystemNPM  Ecosystem = "npm"
	EcosystemGo   Ecosystem = "go"
	EcosystemPyPI Ecosystem = "pypi"
)

// Ecosystems lists every supported ecosystem.
func Ecosystems() []Ecosystem { return []Ecosystem{EcosystemNPM, EcosystemGo, EcosystemPyPI} }

// ReleaseCooldown is how old a release must be before it may enter: most
// compromised releases are found and pulled within days.
const ReleaseCooldown = 7 * 24 * time.Hour

// Package is one released version of a dependency.
type Package struct {
	Ecosystem Ecosystem
	Name      string
	Version   string
}

// Key is "name@version", the form approvals list transitive packages in.
func (p Package) Key() string { return p.Name + "@" + p.Version }

func (p Package) String() string { return fmt.Sprintf("%s %s", p.Ecosystem, p.Key()) }

// LockedPackage is a package as a lockfile pins it.
type LockedPackage struct {
	Package
	// InstallScript marks an npm package that runs code when installed.
	InstallScript bool
}

// Approval is the record of a human-approved dependency: what was vetted,
// why it is needed, and the transitive packages accepted with it.
type Approval struct {
	Package
	Reason  string
	Checked string
	// Transitive lists, as name@version, the packages approved with this
	// one: its dependency tree at the time of vetting.
	Transitive []string
	// SecurityPatch names the advisory a release fixes, which lets it in
	// before the release cooldown ends.
	SecurityPatch string
}

// ApprovalInput is an unvalidated approval record.
type ApprovalInput struct {
	Ecosystem     string
	Name          string
	Version       string
	Decision      string
	Reason        string
	Checked       string
	Transitive    []string
	SecurityPatch string
}

// NewApproval validates an approval record.
func NewApproval(in ApprovalInput) (Approval, error) {
	var problems []string
	eco := Ecosystem(in.Ecosystem)
	if !slices.Contains(Ecosystems(), eco) {
		problems = append(problems, fmt.Sprintf("ecosystem %q is not one of npm, go, pypi", in.Ecosystem))
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Version) == "" {
		problems = append(problems, "name and version are required")
	}
	if in.Decision != "approved" {
		problems = append(problems, `decision must be "approved"; a rejected dependency needs no record`)
	}
	if letters(in.Reason) < minReasonLetters {
		problems = append(problems, fmt.Sprintf("the reason needs at least %d letters", minReasonLetters))
	}
	if _, err := time.Parse(time.DateOnly, in.Checked); err != nil {
		problems = append(problems, fmt.Sprintf("checked %q must be the vetting date, YYYY-MM-DD", in.Checked))
	}
	for _, t := range in.Transitive {
		if _, _, ok := SplitKey(t); !ok {
			problems = append(problems, fmt.Sprintf("transitive entry %q must be name@version", t))
		}
	}
	if len(problems) > 0 {
		return Approval{}, errors.New("invalid approval: " + strings.Join(problems, "; "))
	}
	return Approval{
		Package: Package{Ecosystem: eco, Name: in.Name, Version: in.Version},
		Reason:  in.Reason, Checked: in.Checked, Transitive: slices.Clone(in.Transitive), SecurityPatch: in.SecurityPatch,
	}, nil
}

// SplitKey splits "name@version" at the last '@', so npm scoped names
// ("@scope/pkg@1.0.0") keep their leading '@'.
func SplitKey(key string) (name, version string, ok bool) {
	i := strings.LastIndex(key, "@")
	if i <= 0 || i == len(key)-1 {
		return "", "", false
	}
	return key[:i], key[i+1:], true
}

// ApprovalsDir is where committed approval records live.
const ApprovalsDir = ".millennium/deps"

// ApprovalFile is the record's file name inside its ecosystem directory: the
// name with "/" spelled "__", so Go module paths and npm scopes stay one
// file name.
func ApprovalFile(p Package) string {
	return path.Join(string(p.Ecosystem), strings.ReplaceAll(p.Name, "/", "__")+"@"+p.Version+".json")
}

// Approvals is the set of approval records in force.
type Approvals []Approval

// Covering finds the approval that lets p in: its own record, or a record
// that lists it as transitive in the same ecosystem.
func (a Approvals) Covering(p Package) (Approval, bool) {
	for _, approval := range a {
		if approval.Ecosystem != p.Ecosystem {
			continue
		}
		if approval.Key() == p.Key() || slices.Contains(approval.Transitive, p.Key()) {
			return approval, true
		}
	}
	return Approval{}, false
}

// GoIntegrityProblems lists Go settings that switch off the module
// checksum verification which catches a tampered module.
func GoIntegrityProblems(env map[string]string) []string {
	var problems []string
	if env["GOSUMDB"] == "off" {
		problems = append(problems, "GOSUMDB=off switches off the checksum database")
	}
	if env["GONOSUMCHECK"] != "" || env["GONOSUMDB"] == "*" {
		problems = append(problems, "GONOSUMCHECK or GONOSUMDB=* skips checksum verification for every module")
	}
	if env["GOINSECURE"] != "" {
		problems = append(problems, "GOINSECURE allows fetching modules without TLS: "+env["GOINSECURE"])
	}
	if strings.Contains(env["GOFLAGS"], "-insecure") || strings.Contains(env["GOFLAGS"], "-mod=mod") {
		problems = append(problems, "GOFLAGS "+env["GOFLAGS"]+" lets builds fetch or change dependencies unchecked")
	}
	return problems
}

// NpmrcProblems lists what an .npmrc beside a lockfile must say and does
// not: install scripts off, exact versions saved.
func NpmrcProblems(content string) []string {
	settings := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && !strings.HasPrefix(key, "#") && !strings.HasPrefix(key, ";") {
			settings[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	var problems []string
	if settings["ignore-scripts"] != "true" {
		problems = append(problems, "ignore-scripts=true, so installing a package cannot run its scripts")
	}
	if settings["save-exact"] != "true" {
		problems = append(problems, "save-exact=true, so new dependencies are pinned")
	}
	return problems
}
