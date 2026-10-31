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

// Comment is one comment, with the line it starts on. Text includes the
// comment markers ("// ...", "# ...").
type Comment struct {
	Line int
	Text string
}

// ConstructKind names a risky language construct, independent of language.
type ConstructKind string

// Construct kinds the analyzers report.
const (
	// ConstructDynamicCode runs code built at run time: eval, new Function,
	// Python exec.
	ConstructDynamicCode ConstructKind = "dynamic-code"
	// ConstructShellCommand runs a command through a shell, where an
	// interpolated value can become a command.
	ConstructShellCommand ConstructKind = "shell-command"
	// ConstructTLSVerifyOff disables certificate verification.
	ConstructTLSVerifyOff ConstructKind = "tls-verify-off"
	// ConstructUnsafeDeserialization decodes data into arbitrary objects:
	// pickle, yaml.load without a safe loader.
	ConstructUnsafeDeserialization ConstructKind = "unsafe-deserialization"
	// ConstructHTMLInjection writes raw HTML: innerHTML,
	// dangerouslySetInnerHTML.
	ConstructHTMLInjection ConstructKind = "html-injection"
	// ConstructExplicitAny is a TypeScript `any`, which switches the type
	// checker off for that value.
	ConstructExplicitAny ConstructKind = "explicit-any"
)

// ConstructKinds lists every construct kind in a stable order.
func ConstructKinds() []ConstructKind {
	return []ConstructKind{
		ConstructDynamicCode, ConstructShellCommand, ConstructTLSVerifyOff,
		ConstructUnsafeDeserialization, ConstructHTMLInjection, ConstructExplicitAny,
	}
}

// Construct is one risky construct found in a file.
type Construct struct {
	Kind ConstructKind
	Line int
	// Detail is what was found, for the message: "eval", "shell=True".
	Detail string
}

// FileFacts is what a language analyzer extracts from one source file. Every
// language produces the same facts, so the rules are written once.
type FileFacts struct {
	Path string
	// Generated marks machine-written code, which the limits skip.
	Generated bool
	// Test marks test code, which the function-length limit skips.
	Test       bool
	Imports    []Import
	Functions  []FunctionFacts
	Comments   []Comment
	Constructs []Construct
}
