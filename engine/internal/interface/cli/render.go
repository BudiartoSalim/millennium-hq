package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"millennium/internal/domain"
)

type renderer struct {
	report  func(domain.Report)
	failure func(error)
}

func rendererFor(name string, stdout, stderr io.Writer) (renderer, error) {
	switch name {
	case "text":
		return renderer{
			report:  func(r domain.Report) { writeText(stderr, r) },
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

func writeText(w io.Writer, r domain.Report) {
	switch {
	case !r.Enabled:
		fmt.Fprintf(w, "millennium: not enabled in this repository (no %s); nothing checked\n", domain.ConfigPath)
	case len(r.Results) == 0:
		fmt.Fprintln(w, "millennium: BLOCKED: no gate ran; the engine is miswired, report this as a bug")
	case r.Passed():
		fmt.Fprintf(w, "millennium: passed (gates: %d, changed files: %d)\n", len(r.Results), r.ChangedFiles)
	default:
		fmt.Fprintf(w, "millennium: BLOCKED (findings: %d)\n", r.FindingCount())
		for _, f := range r.Findings() {
			fmt.Fprintf(w, "  [%s/%s] %s%s\n", f.Gate, f.Rule, location(f), f.Message)
		}
	}
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

// jsonReport is the machine-readable report. Status is one of pass, blocked,
// not-enabled or error.
type jsonReport struct {
	Status       string        `json:"status"`
	ChangedFiles int           `json:"changedFiles"`
	Gates        []string      `json:"gates"`
	Findings     []jsonFinding `json:"findings"`
	Error        string        `json:"error,omitempty"`
}

type jsonFinding struct {
	Gate    string `json:"gate"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
}

func toJSON(r domain.Report) jsonReport {
	out := jsonReport{Status: jsonStatus(r), ChangedFiles: r.ChangedFiles, Gates: []string{}, Findings: []jsonFinding{}}
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
