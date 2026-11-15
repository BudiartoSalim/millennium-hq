package domain

import "fmt"

// Limits are the size and complexity limits every function must meet.
type Limits struct {
	FunctionLines int
	Nesting       int
	Cognitive     int
}

// DefaultLimits are the limits from the server-scripts conventions: a
// function fits on one screen, nests at most three levels, and has a
// cognitive complexity of at most 15. A pack or component that sets no
// limit of its own gets these.
func DefaultLimits() Limits {
	return Limits{FunctionLines: 50, Nesting: 3, Cognitive: 15}
}

// maxLimit bounds every configured limit, so a typo such as 15000 for 15 is
// an error rather than a limit that never fires.
const maxLimit = 1000

// LimitsInput is a partial set of limits as read from a pack or a
// component: a nil field keeps the value it would otherwise have.
type LimitsInput struct {
	FunctionLines *int
	Nesting       *int
	Cognitive     *int
}

// IsZero reports whether no limit is set.
func (in LimitsInput) IsZero() bool {
	return in.FunctionLines == nil && in.Nesting == nil && in.Cognitive == nil
}

// LimitOverride is a component's own limits, which replace those of its
// conventions field by field. Every override says why.
type LimitOverride struct {
	Values LimitsInput
	Reason string
}

// With returns l with the limits in set replaced.
func (l Limits) With(set LimitsInput) Limits {
	if set.FunctionLines != nil {
		l.FunctionLines = *set.FunctionLines
	}
	if set.Nesting != nil {
		l.Nesting = *set.Nesting
	}
	if set.Cognitive != nil {
		l.Cognitive = *set.Cognitive
	}
	return l
}

// Strictest returns the lower of each limit of l and other.
func (l Limits) Strictest(other Limits) Limits {
	return Limits{
		FunctionLines: min(l.FunctionLines, other.FunctionLines),
		Nesting:       min(l.Nesting, other.Nesting),
		Cognitive:     min(l.Cognitive, other.Cognitive),
	}
}

// Raised lists each limit of next that is higher than in l, in words.
func (l Limits) Raised(next Limits) []string {
	var out []string
	after := next.metrics(false)
	for i, m := range l.metrics(false) {
		if after[i].limit > m.limit {
			out = append(out, fmt.Sprintf("the %s limit rises from %d to %d", m.noun, m.limit, after[i].limit))
		}
	}
	return out
}

// limits validates a partial set of limits: each one a whole number from 1
// to maxLimit.
func (v *validator) limits(field string, in LimitsInput) {
	for _, f := range []struct {
		name  string
		value *int
	}{{"functionLines", in.FunctionLines}, {"nesting", in.Nesting}, {"cognitive", in.Cognitive}} {
		if f.value != nil && (*f.value < 1 || *f.value > maxLimit) {
			v.addf("%s.%s: %d must be between 1 and %d", field, f.name, *f.value, maxLimit)
		}
	}
}

// limitOverride validates a component's own limits, which need a reason.
func (v *validator) limitOverride(field string, in *LimitOverride) *LimitOverride {
	if in == nil {
		return nil
	}
	if in.Values.IsZero() {
		v.addf("%s: set at least one of functionLines, nesting and cognitive", field)
	}
	v.limits(field, in.Values)
	if letters(in.Reason) < minReasonLetters {
		v.addf("%s.reason: say why this component needs its own limits, in at least %d letters", field, minReasonLetters)
	}
	override := *in
	return &override
}

type metric struct {
	rule  string
	noun  string
	value func(FunctionFacts) int
	limit int
}

func (l Limits) metrics(test bool) []metric {
	all := []metric{
		{"nesting-depth", "nesting depth", func(f FunctionFacts) int { return f.Nesting }, l.Nesting},
		{"cognitive-complexity", "cognitive complexity", func(f FunctionFacts) int { return f.Cognitive }, l.Cognitive},
	}
	if test {
		// Table-driven tests are long by nature; their length is data, not
		// logic, so only test code is exempt from the length limit.
		return all
	}
	return append([]metric{{"function-length", "length in lines", func(f FunctionFacts) int { return f.Lines }, l.FunctionLines}}, all...)
}

// Check applies the limits to file. When base, the file before the change,
// is given, a function that already exceeded a limit may stay over it but
// must not get worse; everything else must meet the limits.
func (l Limits) Check(file FileFacts, base *FileFacts) []Finding {
	if file.Generated {
		return nil
	}
	before := functionsByKey(base)
	var findings []Finding
	for _, fn := range file.Functions {
		previous, existed := before[fn.Key]
		for _, m := range l.metrics(file.Test) {
			if msg := m.violation(fn, previous, existed); msg != "" {
				findings = append(findings, Finding{Rule: m.rule, File: file.Path, Line: fn.Line, Message: msg})
			}
		}
	}
	return findings
}

func (m metric) violation(fn, previous FunctionFacts, existed bool) string {
	value := m.value(fn)
	if value <= m.limit {
		return ""
	}
	if !existed {
		return fmt.Sprintf("%s has a %s of %d; the limit is %d", fn.Key, m.noun, value, m.limit)
	}
	old := m.value(previous)
	if value <= old {
		return ""
	}
	if old > m.limit {
		return fmt.Sprintf("%s went from a %s of %d to %d; it was already over the limit of %d and may not get worse", fn.Key, m.noun, old, value, m.limit)
	}
	return fmt.Sprintf("%s went from a %s of %d to %d; the limit is %d", fn.Key, m.noun, old, value, m.limit)
}

func functionsByKey(file *FileFacts) map[string]FunctionFacts {
	if file == nil {
		return nil
	}
	byKey := make(map[string]FunctionFacts, len(file.Functions))
	for _, fn := range file.Functions {
		byKey[fn.Key] = fn
	}
	return byKey
}
