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
// cognitive complexity of at most 15.
func DefaultLimits() Limits {
	return Limits{FunctionLines: 50, Nesting: 3, Cognitive: 15}
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
