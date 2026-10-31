package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"millennium-code-qc/internal/application/usecase"
	"millennium-code-qc/internal/domain"
)

func (a *App) discover(ctx context.Context, args []string) int {
	flags := a.flagSet("discover")
	formatName := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return exitBlocked
	}
	if flags.NArg() > 0 || (*formatName != "text" && *formatName != "json") {
		fmt.Fprintln(a.stderr, "millennium-code-qc: usage: millennium-code-qc discover [--format text|json]")
		return exitBlocked
	}
	var result usecase.DiscoverResult
	err := a.withDeps(ctx, func(deps Deps) (err error) {
		result, err = deps.Discover.Execute(ctx)
		return err
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "millennium-code-qc: %v\n", err)
		return exitBlocked
	}
	if *formatName == "json" {
		err = json.NewEncoder(a.stdout).Encode(discoveryJSON(result))
	} else {
		err = writeDiscovery(a.stdout, result)
	}
	if err != nil {
		fmt.Fprintf(a.stderr, "millennium-code-qc: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

type componentJSON struct {
	Paths       []string `json:"paths"`
	Languages   []string `json:"languages"`
	Conventions []string `json:"conventions"`
	Reason      string   `json:"reason,omitempty"`
}

type fitJSON struct {
	Convention string `json:"convention"`
	Language   string `json:"language"`
	InLayer    int    `json:"inLayer"`
	Total      int    `json:"total"`
	Fits       bool   `json:"fits"`
}

type moduleJSON struct {
	Root    string         `json:"root"`
	Markers []string       `json:"markers"`
	Files   map[string]int `json:"files"`
	Tests   int            `json:"tests"`
	React   bool           `json:"react"`
	Fits    []fitJSON      `json:"fits"`
}

type draftConfigJSON struct {
	Version    int             `json:"version"`
	Components []componentJSON `json:"components"`
}

type draftJSON struct {
	Version int             `json:"version"`
	Config  draftConfigJSON `json:"config"`
}

// proposalJSON is the draft in the format `config apply` reads.
func proposalJSON(components []domain.ComponentInput) draftJSON {
	out := make([]componentJSON, len(components))
	for i, c := range components {
		out[i] = componentJSON{Paths: c.Paths, Languages: c.Languages, Conventions: c.Conventions, Reason: c.Reason}
	}
	return draftJSON{Version: 1, Config: draftConfigJSON{Version: domain.ConfigVersion, Components: out}}
}

func discoveryJSON(r usecase.DiscoverResult) map[string]any {
	modules := make([]moduleJSON, len(r.Discovery.Modules))
	for i, m := range r.Discovery.Modules {
		modules[i] = moduleJSON{Root: rootLabel(m.Root), Markers: nonNil(m.Markers), Files: languageCounts(m.Files), Tests: m.Tests, React: m.React, Fits: []fitJSON{}}
		for _, f := range m.Fits {
			modules[i].Fits = append(modules[i].Fits, fitJSON{string(f.Convention), string(f.Language), f.InLayer, f.Total, f.Fits()})
		}
	}
	out := map[string]any{"modules": modules, "loose": languageCounts(r.Discovery.Loose)}
	if len(r.Components) > 0 {
		out["proposal"] = proposalJSON(r.Components)
	}
	return out
}

func writeDiscovery(w io.Writer, r usecase.DiscoverResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "millennium-code-qc discover: %d modules\n", len(r.Discovery.Modules))
	for _, m := range r.Discovery.Modules {
		fmt.Fprintf(&b, "  %s (%s): %s", rootLabel(m.Root), strings.Join(m.Markers, ", "), countsText(m.Files))
		if m.Tests > 0 {
			fmt.Fprintf(&b, ", %d test files", m.Tests)
		}
		if m.React {
			b.WriteString(", React")
		}
		b.WriteString("\n")
		for _, f := range m.Fits {
			fmt.Fprintf(&b, "    %s: %d of %d %s files in its layers\n", f.Convention, f.InLayer, f.Total, f.Language)
		}
	}
	if len(r.Discovery.Loose) > 0 {
		fmt.Fprintf(&b, "  outside any module: %s\n", countsText(r.Discovery.Loose))
	}
	if len(r.Components) == 0 {
		b.WriteString("\nNo code found to draft a config for.\n")
	} else {
		data, err := json.MarshalIndent(proposalJSON(r.Components), "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "\nDraft proposal (a starting point: review it, save it as %s, dry-run it with\n`millennium-code-qc audit --proposal %s`, then apply it with `millennium-code-qc config apply`):\n%s\n",
			usecase.ProposalFile, usecase.ProposalFile, data)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func rootLabel(root string) string {
	if root == "" {
		return "."
	}
	return root
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func languageCounts(m map[domain.Language]int) map[string]int {
	out := make(map[string]int, len(m))
	for lang, n := range m {
		out[string(lang)] = n
	}
	return out
}

func countsText(m map[domain.Language]int) string {
	if len(m) == 0 {
		return "no code"
	}
	parts := make([]string, 0, len(m))
	for lang, n := range m {
		parts = append(parts, fmt.Sprintf("%s %d files", lang, n))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// proposedConfig reads a proposal for a dry-run audit. Its config is
// validated exactly as `config apply` would, and nothing is written.
func proposedConfig(deps Deps, file string) (*domain.Config, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read the proposal: %w", err)
	}
	plan, err := deps.Apply.Prepare(data, usecase.ApplyCommitted)
	if err != nil {
		return nil, err
	}
	if plan.Config == nil {
		return nil, errors.New("the proposal has no config to audit against")
	}
	return plan.Config, nil
}
