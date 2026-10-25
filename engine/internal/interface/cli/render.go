package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"millennium/internal/application/usecase"
	"millennium/internal/domain"
)

type renderer struct {
	report  func(domain.Report)
	failure func(error)
}

func rendererFor(name string, stdout, stderr io.Writer, filesLabel string) (renderer, error) {
	switch name {
	case "text":
		return renderer{
			report:  func(r domain.Report) { writeText(stderr, r, filesLabel) },
			failure: func(err error) { fmt.Fprintf(stderr, "millennium: BLOCKED: %v\n", err) },
		}, nil
	case "json":
		return renderer{
			report:  func(r domain.Report) { writeJSON(stdout, toJSON(r)) },
			failure: func(err error) { writeJSON(stdout, jsonReport{Status: "error", Error: err.Error()}) },
		}, nil
	}
	return renderer{}, fmt.Errorf("unknown format %q: use text or json", name)
}

// outcome renders a use-case result and returns the exit status.
func (r renderer) outcome(report domain.Report, err error) int {
	if err != nil {
		return r.fail(err)
	}
	r.report(report)
	if report.Passed() {
		return exitPass
	}
	return exitBlocked
}

func (r renderer) fail(err error) int {
	r.failure(err)
	return exitBlocked
}

func writeText(w io.Writer, r domain.Report, filesLabel string) {
	switch {
	case !r.Enabled:
		fmt.Fprintf(w, "millennium: not enabled in this repository (no %s); nothing checked\n", domain.ConfigPath)
	case len(r.Results) == 0:
		fmt.Fprintln(w, "millennium: BLOCKED: no gate ran; the engine is miswired, report this as a bug")
	case r.Passed():
		fmt.Fprintf(w, "millennium: passed (gates: %d, %s: %d)\n", len(r.Results), filesLabel, r.ChangedFiles)
		writeDisabled(w, r.Disabled)
	default:
		fmt.Fprintf(w, "millennium: BLOCKED (findings: %d, %s: %d)\n", r.FindingCount(), filesLabel, r.ChangedFiles)
		writeDisabled(w, r.Disabled)
		fmt.Fprintf(w, "  by rule: %s\n", ruleCounts(r.Findings()))
		for _, f := range r.Findings() {
			fmt.Fprintf(w, "  [%s/%s] %s%s\n", f.Gate, f.Rule, location(f), f.Message)
		}
	}
}

// writeDisabled lists the checks the config switched off on every report,
// so a disabled check is never forgotten.
func writeDisabled(w io.Writer, disabled []domain.DisabledResult) {
	for _, d := range disabled {
		fmt.Fprintf(w, "  disabled: %s (hid %d findings): %s\n", d.Check, d.Suppressed, d.Reason)
	}
}

// ruleCounts summarises findings per rule, most frequent first, so a large
// audit shows where the work is before the details.
func ruleCounts(findings []domain.Finding) string {
	counts := map[string]int{}
	for _, f := range findings {
		counts[f.Gate+"/"+f.Rule]++
	}
	rules := make([]string, 0, len(counts))
	for rule := range counts {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if counts[rules[i]] != counts[rules[j]] {
			return counts[rules[i]] > counts[rules[j]]
		}
		return rules[i] < rules[j]
	})
	parts := make([]string, len(rules))
	for i, rule := range rules {
		parts[i] = fmt.Sprintf("%s %d", rule, counts[rule])
	}
	return strings.Join(parts, ", ")
}

func location(f domain.Finding) string {
	switch {
	case f.File == "":
		return ""
	case f.Line > 0:
		return fmt.Sprintf("%s:%d: ", f.File, f.Line)
	}
	return f.File + ": "
}

func writeExplanation(w io.Writer, e usecase.Explanation) {
	fmt.Fprintf(w, "%s\n", e.Path)
	if e.Excluded {
		fmt.Fprintln(w, "  excluded: yes, no check reads this file")
		return
	}
	language := string(e.Language)
	if language == "" {
		language = "none (only checks that apply to any file)"
	}
	fmt.Fprintf(w, "  language:  %s\n", language)
	if e.Component == nil {
		fmt.Fprintln(w, "  component: none; no convention or limit applies")
		return
	}
	fmt.Fprintf(w, "  component: %s (root %q)\n", e.Component.Component.Name(), e.Component.Root)
	if e.Component.Component.Reason != "" {
		fmt.Fprintf(w, "  reason:    %s\n", e.Component.Component.Reason)
	}
	if e.Language == "" || !e.Component.Component.Has(e.Language) {
		fmt.Fprintln(w, "  note:      the component does not list this file's language, so no code check reads it")
		return
	}
	for _, layer := range e.Layers {
		fmt.Fprintf(w, "  %s: %s\n", layer.Convention, describeLayer(layer))
	}
	fmt.Fprintf(w, "  limits:    function length %d lines (test files exempt), nesting depth %d, cognitive complexity %d\n",
		e.Limits.FunctionLines, e.Limits.Nesting, e.Limits.Cognitive)
}

func describeLayer(layer usecase.LayerAssignment) string {
	switch {
	case layer.Problem != "":
		return "cannot be checked: " + layer.Problem
	case layer.Layer == "":
		return "in no layer (layer rules do not restrict it)"
	case layer.Layer == domain.CompositionRoot:
		return "composition root (may import every layer)"
	}
	return "layer " + layer.Layer
}

// jsonReport is the machine-readable report. Status is one of pass, blocked,
// not-enabled or error.
type jsonReport struct {
	Status   string         `json:"status"`
	Files    int            `json:"files"`
	Gates    []string       `json:"gates"`
	Findings []jsonFinding  `json:"findings"`
	Disabled []jsonDisabled `json:"disabled"`
	Error    string         `json:"error,omitempty"`
}

type jsonDisabled struct {
	Check      string `json:"check"`
	Reason     string `json:"reason"`
	Suppressed int    `json:"suppressed"`
}

type jsonFinding struct {
	Gate    string `json:"gate"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
}

func toJSON(r domain.Report) jsonReport {
	out := jsonReport{Status: jsonStatus(r), Files: r.ChangedFiles, Gates: []string{}, Findings: []jsonFinding{}, Disabled: []jsonDisabled{}}
	for _, d := range r.Disabled {
		out.Disabled = append(out.Disabled, jsonDisabled{Check: d.Check, Reason: d.Reason, Suppressed: d.Suppressed})
	}
	for _, result := range r.Results {
		out.Gates = append(out.Gates, result.Gate)
	}
	for _, f := range r.Findings() {
		out.Findings = append(out.Findings, jsonFinding{Gate: f.Gate, Rule: f.Rule, Message: f.Message, File: f.File, Line: f.Line})
	}
	return out
}

func jsonStatus(r domain.Report) string {
	switch {
	case !r.Enabled:
		return "not-enabled"
	case r.Passed():
		return "pass"
	}
	return "blocked"
}

func writeJSON(w io.Writer, report jsonReport) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Encoding these plain structs cannot fail; a write error has nowhere
	// better to go than the exit status, which already reflects the result.
	_ = enc.Encode(report)
}
