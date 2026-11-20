package domain

// LayerTesting says whether a file sits in a layer that needs unit tests.
// It decides what the file's generated-code marker is worth: the marker is
// file content, which whoever edits the file writes, so it may not take a
// tested-layer file out of the checks that hold logic to its tests and
// limits. There only the config's exclude, which agents cannot change,
// declares generated code.
type LayerTesting string

// Layer testing.
const (
	// LayerUntested is a file in no layer, or in a layer no convention of
	// its component marks as tested.
	LayerUntested LayerTesting = "untested"
	// LayerTested is a file in a layer a convention of its component marks
	// as tested, such as domain and application under clean-architecture.
	LayerTested LayerTesting = "tested"
)

// markerNote tells the user why a marked file in a tested layer was checked
// and how to declare generated code there instead.
const markerNote = `the generated-code marker does not exempt files in a tested layer; declare generated files there with "exclude" in the config`

// TestingOf says whether p, a file of lang in the component match, sits in
// a layer one of the component's conventions marks as tested. A convention
// without a pack, or without paths for lang, tests nothing here: the
// structure gate reports it.
func (c Catalog) TestingOf(lang Language, match ComponentMatch, p string) LayerTesting {
	for _, convention := range match.Component.Conventions {
		pack, ok := c[convention]
		if !ok {
			continue
		}
		if layering, err := pack.For(lang, match); err == nil && layering.Tested(p) {
			return LayerTested
		}
	}
	return LayerUntested
}

// MarkerExempts reports whether file's generated-code marker exempts it from
// the limits: only outside tested layers. Anything but LayerUntested counts
// as tested, so an unset value never exempts a file.
func (t LayerTesting) MarkerExempts(file FileFacts) bool {
	return file.Generated && t == LayerUntested
}

// MarkerNote is what a finding on file adds when the file carries a
// generated-code marker that did not exempt it, starting with "; ". It is
// empty for an unmarked file and for one the marker exempts.
func (t LayerTesting) MarkerNote(file FileFacts) string {
	if !file.Generated || t.MarkerExempts(file) {
		return ""
	}
	return "; " + markerNote
}
