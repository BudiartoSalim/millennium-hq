package domain

import (
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
)

// moduleMarkers are the files that make a directory a module root.
var moduleMarkers = []string{"go.mod", "package.json", "pyproject.toml", "setup.py", "requirements.txt"}

// packFitThreshold is the share of a module's code files a pack's layers
// must hold before discover suggests the pack.
const packFitThreshold = 0.5

// PackFit is how well a pack's default layout matches a module's files.
type PackFit struct {
	Convention Convention
	Language   Language
	// InLayer counts the files that fall in one of the pack's layers, of
	// Total files in the language.
	InLayer, Total int
}

// Fits reports whether the layout matches well enough to suggest the pack.
func (f PackFit) Fits() bool {
	return f.Total > 0 && float64(f.InLayer) >= packFitThreshold*float64(f.Total)
}

// Module is one module root and what discover found in it.
type Module struct {
	// Root is repository-relative; "" is the repository root.
	Root    string
	Markers []string
	// Files counts code files per language, tests included; Tests counts
	// the test files among them.
	Files map[Language]int
	Tests int
	// React is set when the module's package.json depends on react.
	React bool
	Fits  []PackFit
}

// Discovery is what discover found in a repository.
type Discovery struct {
	Modules []Module
	// Loose counts code files in no module, per language, such as scripts.
	Loose map[Language]int
	// LooseDirs groups the loose files below a top-level directory by that
	// directory; files at the repository root are in Loose only.
	LooseDirs []LooseDir
}

// LooseDir is a top-level directory holding code that is in no module.
type LooseDir struct {
	Dir   string
	Files map[Language]int
}

// DiscoverInput is what discover reads: every file of the working tree, and
// which module roots use React.
type DiscoverInput struct {
	Files []string
	React func(root string) bool
	Packs []Pack
}

// Discover finds the module roots of a repository and what they hold. It
// reads paths only, so it is safe to run on code nobody has vetted.
func Discover(in DiscoverInput) Discovery {
	d := discoverer{
		roots: moduleRoots(in.Files), modules: map[string]*Module{}, files: map[string][]string{},
		loose: map[Language]int{}, looseDirs: map[string]map[Language]int{},
	}
	for _, root := range d.roots {
		d.modules[root] = &Module{Root: root, Files: map[Language]int{}, React: in.React(root)}
	}
	for _, f := range in.Files {
		d.add(f)
	}
	out := Discovery{Loose: d.loose}
	for _, dir := range sortedKeys(d.looseDirs) {
		out.LooseDirs = append(out.LooseDirs, LooseDir{Dir: dir, Files: d.looseDirs[dir]})
	}
	for _, root := range d.roots {
		m := d.modules[root]
		m.Fits = packFits(in.Packs, root, d.files[root])
		out.Modules = append(out.Modules, *m)
	}
	return out
}

// discoverer collects what Discover finds, file by file.
type discoverer struct {
	roots     []string
	modules   map[string]*Module
	files     map[string][]string
	loose     map[Language]int
	looseDirs map[string]map[Language]int
}

func (d *discoverer) add(f string) {
	root, owned := owningRoot(d.roots, f)
	if owned && dirOf(f) == root && slices.Contains(moduleMarkers, path.Base(f)) {
		d.modules[root].Markers = append(d.modules[root].Markers, path.Base(f))
	}
	lang, ok := LanguageOf(f)
	switch {
	case !ok:
	case !owned:
		d.addLoose(f, lang)
	default:
		m := d.modules[root]
		m.Files[lang]++
		if IsTestPath(lang, f) {
			m.Tests++
		}
		d.files[root] = append(d.files[root], f)
	}
}

func (d *discoverer) addLoose(f string, lang Language) {
	d.loose[lang]++
	top, _, nested := strings.Cut(f, "/")
	if !nested {
		return
	}
	if d.looseDirs[top] == nil {
		d.looseDirs[top] = map[Language]int{}
	}
	d.looseDirs[top][lang]++
}

// moduleRoots are the directories holding a marker file, sorted.
func moduleRoots(files []string) []string {
	set := map[string]bool{}
	for _, f := range files {
		if slices.Contains(moduleMarkers, path.Base(f)) {
			set[dirOf(f)] = true
		}
	}
	roots := make([]string, 0, len(set))
	for root := range set {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots
}

// owningRoot is the deepest module root containing f.
func owningRoot(roots []string, f string) (string, bool) {
	best, found := "", false
	for _, root := range roots {
		if Within(f, root) && (!found || len(root) > len(best)) {
			best, found = root, true
		}
	}
	return best, found
}

func dirOf(f string) string {
	if dir := path.Dir(f); dir != "." {
		return dir
	}
	return ""
}

func packFits(packs []Pack, root string, files []string) []PackFit {
	var fits []PackFit
	match := ComponentMatch{Component: Component{Patterns: []Pattern{RootPattern()}}, Root: root}
	for _, pack := range packs {
		for _, lang := range pack.Languages {
			layering, err := pack.For(lang, match)
			if err != nil {
				continue
			}
			if fit := fitOf(layering, pack.Name, lang, files); fit.Total > 0 {
				fits = append(fits, fit)
			}
		}
	}
	return fits
}

// fitOf counts the non-test files of lang and how many fall in a layer.
func fitOf(layering Layering, convention Convention, lang Language, files []string) PackFit {
	fit := PackFit{Convention: convention, Language: lang}
	for _, f := range files {
		if l, _ := LanguageOf(f); l != lang || IsTestPath(lang, f) {
			continue
		}
		fit.Total++
		if layering.LayerOf(f) != "" {
			fit.InLayer++
		}
	}
	return fit
}

// Suggest drafts components from a discovery: one per module with code, with
// its languages and the best-fitting convention. It is a starting point for
// an agent and the user to refine, never applied as is.
func (d Discovery) Suggest() []ComponentInput {
	var components []ComponentInput
	for _, m := range d.Modules {
		if len(m.Files) == 0 {
			continue
		}
		c := ComponentInput{Paths: []string{rootPath(m.Root)}, Languages: m.languageNames()}
		c.Conventions, c.Reason = m.bestConvention()
		components = append(components, c)
	}
	for _, dir := range d.LooseDirs {
		components = append(components, ComponentInput{
			Paths: []string{dir.Dir}, Languages: languageNames(dir.Files), Conventions: []string{},
			Reason: "code outside any module, such as scripts",
		})
	}
	return components
}

func rootPath(root string) string {
	if root == "" {
		return "."
	}
	return root
}

func (m Module) languageNames() []string { return languageNames(m.Files) }

func languageNames(counts map[Language]int) []string {
	names := make([]string, 0, len(counts))
	for lang := range counts {
		names = append(names, string(lang))
	}
	sort.Strings(names)
	return names
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// bestConvention picks react-feature-based for a React module whose layout
// fits it, else clean-architecture where it fits; with neither, no
// convention and the reason why.
func (m Module) bestConvention() ([]string, string) {
	preferred := []Convention{ConventionCleanArchitecture}
	if m.React {
		preferred = []Convention{ConventionReactFeatureBased}
	}
	var seen []string
	for _, convention := range preferred {
		for _, fit := range m.Fits {
			if fit.Convention != convention {
				continue
			}
			if fit.Fits() {
				return []string{string(convention)}, ""
			}
			seen = append(seen, fmt.Sprintf("%s holds %d of %d %s files", convention, fit.InLayer, fit.Total, fit.Language))
		}
	}
	if len(seen) == 0 {
		return []string{}, "no convention pack governs this module's languages"
	}
	return []string{}, "the default layout does not fit: " + strings.Join(seen, "; ") + "; map the layers or adopt the layout"
}
