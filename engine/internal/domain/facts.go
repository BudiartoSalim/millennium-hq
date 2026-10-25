package domain

// ImportKind classifies an import by where the imported code lives.
type ImportKind string

// Import kinds.
const (
	// ImportStandard is the language's standard library.
	ImportStandard ImportKind = "standard"
	// ImportProject is code of this repository; Target says where.
	ImportProject ImportKind = "project"
	// ImportExternal is a third-party dependency.
	ImportExternal ImportKind = "external"
)

// Import is one import statement of a source file.
type Import struct {
	// Path is the import as written.
	Path string
	Line int
	Kind ImportKind
	// Target is the repository-relative directory of a project import.
	Target string
}

// FunctionFacts are the measurements of one function or method.
type FunctionFacts struct {
	// Key identifies the function within its file across revisions, for
	// example "Repository.Changes" or "init#2".
	Key       string
	Line      int
	Lines     int
	Nesting   int
	Cognitive int
}

// FileFacts is what a language analyzer extracts from one source file. Every
// language produces the same facts, so the rules are written once.
type FileFacts struct {
	Path string
	// Generated marks machine-written code, which the limits skip.
	Generated bool
	// Test marks test code, which the function-length limit skips.
	Test      bool
	Imports   []Import
	Functions []FunctionFacts
}
