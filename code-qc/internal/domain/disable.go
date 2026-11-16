package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// DisabledCheck is one rule a project switched off: "gate/rule", why, and
// where. No Paths means everywhere.
type DisabledCheck struct {
	Check  string
	Reason string
	Paths  []Pattern
}

// DisableInput is a disable entry as the config spells it. A nil Paths was
// absent: the rule is off everywhere.
type DisableInput struct {
	Check  string
	Reason string
	Paths  []string
}

// DisabledResult is a disabled check as a report shows it: with how many
// findings it hid in this run.
type DisabledResult struct {
	DisabledCheck
	Suppressed int
}

// Hides reports whether the disable hides a finding of its rule about file.
// One limited to paths hides only findings about a repository file a path
// matches as a whole: never a finding about no file in particular, or about
// a path outside the repository (a tool may report "../x"), which cannot be
// placed inside them, and never a file inside a directory whose name
// happens to match, which an agent could create to escape a check.
func (d DisabledCheck) Hides(file string) bool {
	if !d.Limited() {
		return true
	}
	return IsRepoRelative(file) && slices.ContainsFunc(d.Paths, func(p Pattern) bool { return p.Matches(file) })
}

// Limited reports whether the disable applies only in some paths.
func (d DisabledCheck) Limited() bool { return len(d.Paths) > 0 }

// PathNames lists the disable's paths as written, nil when it applies
// everywhere.
func (d DisabledCheck) PathNames() []string {
	if !d.Limited() {
		return nil
	}
	names := make([]string, len(d.Paths))
	for i, p := range d.Paths {
		names[i] = p.String()
	}
	return names
}

// Where describes the paths a disable covers, for reports.
func (d DisabledCheck) Where() string {
	if !d.Limited() {
		return "everywhere"
	}
	return "in " + strings.Join(d.PathNames(), ", ")
}

var checkName = regexp.MustCompile(`^[a-z][a-z-]*/[a-z][a-z-]*$`)

func (v *validator) disabled(in []DisableInput) []DisabledCheck {
	var out []DisabledCheck
	for _, d := range in {
		switch {
		case !checkName.MatchString(d.Check):
			v.addf("disable: %q must name one rule as gate/rule, such as structure/function-length", d.Check)
		case letters(d.Reason) < minReasonLetters:
			v.addf("disable: %s needs a reason of at least %d letters; every report shows it", d.Check, minReasonLetters)
		case slices.ContainsFunc(out, func(o DisabledCheck) bool { return o.Check == d.Check }):
			v.addf("disable: %s is listed more than once; list all its paths in one entry", d.Check)
		case d.Paths != nil && len(d.Paths) == 0:
			v.addf("disable: %s has an empty paths list; name the paths, or leave paths out to disable it everywhere", d.Check)
		case slices.Contains(d.Paths, "."):
			v.addf("disable: %s: \".\" is not a path to limit it to; leave paths out to disable it everywhere", d.Check)
		case len(slices.Compact(slices.Sorted(slices.Values(d.Paths)))) != len(d.Paths):
			v.addf("disable: %s lists a path more than once", d.Check)
		default:
			out = append(out, DisabledCheck{Check: d.Check, Reason: d.Reason, Paths: v.patterns(fmt.Sprintf("disable: %s paths", d.Check), d.Paths, RecursivePattern)})
		}
	}
	return out
}

// disableLoosenings lists where next disables a rule that current checks:
// a rule newly disabled, a path newly covered, or a rule limited to paths
// now disabled everywhere.
func disableLoosenings(current, next []DisabledCheck) []string {
	var out []string
	for _, d := range next {
		i := slices.IndexFunc(current, func(c DisabledCheck) bool { return c.Check == d.Check })
		switch {
		case i < 0:
			out = append(out, fmt.Sprintf("%s is newly disabled %s: %s", d.Check, d.Where(), d.Reason))
		case len(current[i].Paths) == 0:
			// Already off everywhere: nothing more can be switched off.
		case len(d.Paths) == 0:
			out = append(out, fmt.Sprintf("%s is now disabled everywhere, not only %s: %s", d.Check, current[i].Where(), d.Reason))
		default:
			out = append(out, newPaths(d, current[i])...)
		}
	}
	return out
}

func newPaths(next, current DisabledCheck) []string {
	var out []string
	for _, p := range next.Paths {
		if !slices.ContainsFunc(current.Paths, func(c Pattern) bool { return c.String() == p.String() }) {
			out = append(out, fmt.Sprintf("%s is newly disabled in %s: %s", next.Check, p, next.Reason))
		}
	}
	return out
}
