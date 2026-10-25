package domain

// Finding is one problem a gate reported. Every finding blocks.
type Finding struct {
	Gate    string
	Rule    string
	Message string
	File    string
	Line    int
}

// GateResult is what one gate reported during a run.
type GateResult struct {
	Gate     string
	Findings []Finding
}

// Report is the outcome of a gate run.
type Report struct {
	Enabled      bool
	ChangedFiles int
	Results      []GateResult
	// Disabled lists the checks the config switched off, with the findings
	// each hid, so nothing is ever silently off.
	Disabled []DisabledResult
}

// Passed reports whether the run lets the change through. A repository that
// is not opted in passes because nothing was asked of it. An enabled run that
// executed no gate at all does not pass: an empty gate list is a wiring bug,
// not a clean result.
func (r Report) Passed() bool {
	if !r.Enabled {
		return true
	}
	return len(r.Results) > 0 && r.FindingCount() == 0
}

// FindingCount is the number of findings across all gates.
func (r Report) FindingCount() int {
	count := 0
	for _, result := range r.Results {
		count += len(result.Findings)
	}
	return count
}

// Findings returns every finding across all gates, in gate order.
func (r Report) Findings() []Finding {
	var all []Finding
	for _, result := range r.Results {
		all = append(all, result.Findings...)
	}
	return all
}
