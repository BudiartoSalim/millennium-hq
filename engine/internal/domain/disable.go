package domain

import (
	"regexp"
	"slices"
)

// DisabledCheck is one rule a project switched off: "gate/rule" and why.
type DisabledCheck struct {
	Check  string
	Reason string
}

// DisabledResult is a disabled check as a report shows it: with how many
// findings it hid in this run.
type DisabledResult struct {
	DisabledCheck
	Suppressed int
}

var checkName = regexp.MustCompile(`^[a-z][a-z-]*/[a-z][a-z-]*$`)

func (v *validator) disabled(in []DisabledCheck) []DisabledCheck {
	var out []DisabledCheck
	for _, d := range in {
		switch {
		case !checkName.MatchString(d.Check):
			v.addf("disable: %q must name one rule as gate/rule, such as structure/function-length", d.Check)
		case letters(d.Reason) < minReasonLetters:
			v.addf("disable: %s needs a reason of at least %d letters; every report shows it", d.Check, minReasonLetters)
		case slices.ContainsFunc(out, func(o DisabledCheck) bool { return o.Check == d.Check }):
			v.addf("disable: %s is listed more than once", d.Check)
		default:
			out = append(out, d)
		}
	}
	return out
}
