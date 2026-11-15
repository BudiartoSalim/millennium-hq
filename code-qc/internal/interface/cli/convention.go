package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"millennium-code-qc/internal/application/usecase"
	"millennium-code-qc/internal/domain"
)

func (a *App) convention(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.stderr, usageText)
		return exitBlocked
	}
	flags := a.flagSet("convention " + args[0])
	local := flags.Bool("local", false, "use the packs beside this machine's local config")
	from := flags.String("from", string(usecase.DefaultDraftSource), "the convention a new one is copied from")
	positional, err := parseInterleaved(flags, args[1:])
	if err != nil {
		return exitBlocked
	}
	run, ok := a.conventionCommand(args[0], positional, applyTarget(*local), domain.Convention(*from))
	if !ok {
		fmt.Fprint(a.stderr, usageText)
		return exitBlocked
	}
	if err := a.withDeps(ctx, func(deps Deps) error { return run(deps.Conventions) }); err != nil {
		fmt.Fprintf(a.stderr, "millennium-code-qc: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

// parseInterleaved parses flags wherever they appear among the arguments,
// so `convention new NAME --from PACK` works as well as the flags first.
func parseInterleaved(flags *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		if flags.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, flags.Arg(0))
		args = flags.Args()[1:]
	}
}

// conventionCommand picks the subcommand; false means the arguments do not
// fit it.
func (a *App) conventionCommand(name string, args []string, target usecase.ApplyTarget, from domain.Convention) (func(ConventionManager) error, bool) {
	switch {
	case name == "list" && len(args) == 0:
		return func(c ConventionManager) error { return a.listConventions(c, target) }, true
	case name == "show" && len(args) == 1:
		return func(c ConventionManager) error { return a.showConvention(c, target, domain.Convention(args[0])) }, true
	case name == "new" && len(args) == 1:
		return func(c ConventionManager) error { return a.newConvention(c, target, domain.Convention(args[0]), from) }, true
	}
	return nil, false
}

func (a *App) listConventions(c ConventionManager, target usecase.ApplyTarget) error {
	packs, err := c.List(target)
	if err != nil {
		return err
	}
	for _, p := range packs {
		fmt.Fprintf(a.stdout, "%-24s %-8s %s\n", p.Name, p.Origin, joinLanguages(p.Languages))
	}
	return nil
}

func (a *App) showConvention(c ConventionManager, target usecase.ApplyTarget, name domain.Convention) error {
	pack, err := c.Show(target, name)
	if err != nil {
		return err
	}
	writePack(a.stdout, pack)
	return nil
}

func (a *App) newConvention(c ConventionManager, target usecase.ApplyTarget, name, from domain.Convention) error {
	draft, err := c.New(target, name, from)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, `Drafted convention %[1]s, a copy of %[2]s, in %[3]s/:
  pack.json   the rules the gates enforce: languages, layers and what each may
              import, tested and isolated layers, default paths, function limits
  guide.md    what every agent is told about the convention

Edit both. Then propose the pack: this proposal adds it as it is (save it as
millennium.proposal.json, or add its "packs" entry to your proposal):

%[4]s
To use it, name %[1]s in a component's "conventions" in the proposal's config,
and apply it yourself: millennium-code-qc config apply
`, draft.Pack.Name, from, draft.Dir, draft.Proposal)
	return nil
}

// writePack prints a pack's rules and guide.
func writePack(w io.Writer, p domain.Pack) {
	fmt.Fprintf(w, "convention %s (%s)\n", p.Name, p.Origin)
	fmt.Fprintf(w, "languages: %s\n", joinLanguages(p.Languages))
	fmt.Fprintf(w, "limits:    functions at most %d lines, nesting depth %d, cognitive complexity %d\n",
		p.Limits.FunctionLines, p.Limits.Nesting, p.Limits.Cognitive)
	if len(p.Suggest) > 0 {
		fmt.Fprintf(w, "suggested for modules that depend on: %s\n", strings.Join(p.Suggest, ", "))
	}
	fmt.Fprintln(w, "layers:")
	for _, layer := range p.Layers {
		fmt.Fprintf(w, "  %s: %s\n", layer.Name, describePackLayer(layer))
	}
	fmt.Fprintln(w, "paths (from the component root; a config can map them with \"layers\"):")
	langs := make([]string, 0, len(p.Paths))
	for lang := range p.Paths {
		langs = append(langs, string(lang))
	}
	sort.Strings(langs)
	for _, lang := range langs {
		fmt.Fprintf(w, "  %s:\n", lang)
		writeLayerPaths(w, p.Paths[domain.Language(lang)])
	}
	fmt.Fprintf(w, "\nguide:\n\n%s\n", strings.TrimRight(p.Guide, "\n"))
}

func writeLayerPaths(w io.Writer, layers map[string][]domain.Pattern) {
	names := make([]string, 0, len(layers))
	for name := range layers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		patterns := make([]string, len(layers[name]))
		for i, p := range layers[name] {
			patterns[i] = p.String()
		}
		fmt.Fprintf(w, "    %s: %s\n", name, strings.Join(patterns, ", "))
	}
}

func describePackLayer(l domain.PackLayer) string {
	parts := []string{"may import nothing of the project"}
	if len(l.MayImport) > 0 {
		parts[0] = "may import " + strings.Join(l.MayImport, ", ")
	}
	if l.ThirdParty {
		parts = append(parts, "third-party code allowed")
	} else {
		parts = append(parts, "no third-party code")
	}
	if l.Tested {
		parts = append(parts, "tests required")
	}
	if l.Isolated {
		parts = append(parts, "units isolated from each other")
	}
	return strings.Join(parts, "; ")
}

func joinLanguages(langs []domain.Language) string {
	names := make([]string, len(langs))
	for i, l := range langs {
		names[i] = string(l)
	}
	return strings.Join(names, ", ")
}
