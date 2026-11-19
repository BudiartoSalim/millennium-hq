package domain

import (
	"path"
	"strings"
	"unicode"
)

// shellFloor is the floor under the shell matcher: the rule of round 4
// (7f65e61), widened to what the shell reads. A command that can write
// (m.writes: it or a script in it is write-like, or a simple command in
// it puts, moves or removes something) and that names a protected path in
// its text, or a protected path or a leading part of one in any of its
// fields (see floorFields), changes it. The matcher's words m.words are
// those of the command and of the scripts inside it, with quotes removed,
// `$'…'` strings decoded and redirection targets included, so a name
// spelled `'.pi'` or `$'\x2epi'`, or reached through a variable
// (`D=.claude; tee $D/settings.json`), is still read. It denies writes
// that go elsewhere too; provablyReadOnly lets the commands through that
// cannot write at all.
func shellFloor(command string, m shellMatch) (string, bool) {
	if !m.writes {
		return "", false
	}
	for _, protected := range ProtectedPaths() {
		if ContainsFold(command, protected) {
			return protected, true
		}
	}
	return floorMatch(floorFields(command, m.words))
}

// floorFields are the parts of a command, and of the words read from it,
// that the floor reads as paths: each word and each part of it around `=`,
// and the command and each word split as round 4 split them (see
// isWordSeparator) and also at expansions (see isFieldSeparator), so
// `$D/settings.json` holds `D/settings.json` and `$(echo .pi)` holds
// `.pi`. Splitting at more characters only adds fields; round 4's own
// split is kept, since cleaning a longer field can reach a name a shorter
// one does not (`.pi/x{/..` is `.pi`).
func floorFields(command string, words []string) []string {
	fields := splitFields(nil, command)
	for _, word := range words {
		if strings.ContainsFunc(word, isFieldSeparator) {
			fields = append(fields, strings.Split(word, "=")...)
			fields = splitFields(fields, word)
		} else {
			// Every split of it is the word itself.
			fields = append(fields, word)
		}
	}
	return fields
}

// splitFields appends to fields text split as round 4 split it and also
// at expansions.
func splitFields(fields []string, text string) []string {
	fields = append(fields, strings.FieldsFunc(text, isWordSeparator)...)
	return append(fields, strings.FieldsFunc(text, isFieldSeparator)...)
}

// isWordSeparator reports whether r ends a word as round 4 split a
// command: white space, quotes, the shell's operators and `=`.
func isWordSeparator(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("'\"`;&|()<>=", r)
}

// isFieldSeparator reports whether r ends a field: a word separator, or a
// character of an expansion (`$`, `{`, `}`).
func isFieldSeparator(r rune) bool {
	return isWordSeparator(r) || strings.ContainsRune("${}", r)
}

// floorMatch is the protected path one of fields holds, or else the
// leading part of one that one of them ends with, each field read as a
// path, cleaned, as written and with backslashes read as Win32 reads them
// (`.claude\settings.json`). A field that cannot name one (see
// mayNameProtected) is skipped unread.
func floorMatch(fields []string) (string, bool) {
	lead := ""
	for _, field := range fields {
		if !mayNameProtected(field) {
			continue
		}
		for _, spelling := range fieldSpellings(field) {
			p := path.Clean(spelling)
			if protected, ok := ProtectedPathIn(p); ok {
				return protected, true
			}
			if found, ok := ProtectedLeadIn(p); ok && lead == "" {
				lead = found
			}
		}
	}
	return lead, lead != ""
}

// protectedRoots are the first segments of the protected paths: every
// protected path and every leading part of one starts with one of them.
var protectedRoots = []string{".millennium", ".claude", ".pi"}

// mayNameProtected reports whether field may hold a protected path or a
// leading part of one: only if it holds one of protectedRoots as written,
// without regard to ASCII case. Cleaning and reading segments as Win32
// does only remove text, and no character outside ASCII folds to a letter
// of these names, so a field without one names none.
func mayNameProtected(field string) bool {
	for i := strings.IndexByte(field, '.'); i >= 0; i = nextDot(field, i) {
		for _, root := range protectedRoots {
			if len(field)-i >= len(root) && strings.EqualFold(field[i:i+len(root)], root) {
				return true
			}
		}
	}
	return false
}

// nextDot is the index of the first dot of s after i, or -1.
func nextDot(s string, i int) int {
	if j := strings.IndexByte(s[i+1:], '.'); j >= 0 {
		return i + 1 + j
	}
	return -1
}

// fieldSpellings are field as written and, if it holds a backslash, with
// backslashes read as Win32 reads them.
func fieldSpellings(field string) []string {
	if !strings.Contains(field, `\`) {
		return []string{field}
	}
	return []string{field, strings.ReplaceAll(field, `\`, "/")}
}
