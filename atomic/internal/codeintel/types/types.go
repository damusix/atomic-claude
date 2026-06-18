// Package types defines the shared data contract for the code-intelligence
// engine. Every layer — extraction, resolution, graph, search, and the engine
// facade — depends on this package and nothing else from within the engine.
//
// # Cross-cutting Go conventions (decided once here)
//
// JSON-in-TEXT columns (decorators, type_parameters, metadata, candidates,
// errors): stored as json.RawMessage. Rationale: these columns are opaque JSON
// blobs at the types layer; their internal schemas belong to the extraction and
// resolution layers, not to the shared contract. json.RawMessage roundtrips
// without mutation, represents SQL NULL as nil, and defers schema decisions to
// the db layer (CP3/CP4) without forcing typed structs into the shared contract
// prematurely. If a layer needs to inspect a specific field it unmarshals into
// a local type; the contract layer stays schema-agnostic.
//
// Integer-bool flags (is_exported, is_async, is_static, is_const): struct
// fields use Go bool. The db layer (CP3) is responsible for scanning SQLite
// INTEGER columns (0/1) into bool — modernc.org/sqlite does not auto-convert,
// so the db layer must do: var n int; rows.Scan(&n); node.IsExported = n != 0.
// No db code lives in this package.
//
// Subgraph stable sort: Subgraph.Nodes is map[string]Node for O(1) lookup,
// but Go map iteration is non-deterministic. Any code that serialises or
// renders a Subgraph must iterate nodes in a stable order. Use
// SubgraphSortedNodes to obtain a []Node sorted ascending by Node.ID. Never
// range over Subgraph.Nodes directly in serialisation paths.
package types

import (
	"encoding/json"
	"sort"
)

// ---------------------------------------------------------------------------
// NodeKind — the 31 node-type strings (appendix C, verbatim)
// ---------------------------------------------------------------------------

// NodeKind is the type of a symbol node in the knowledge graph. The string
// value is stored in SQLite and must never be changed once data is on disk.
type NodeKind string

const (
	NodeKindFile       NodeKind = "file"
	NodeKindModule     NodeKind = "module"
	NodeKindClass      NodeKind = "class"
	NodeKindStruct     NodeKind = "struct"
	NodeKindInterface  NodeKind = "interface"
	NodeKindTrait      NodeKind = "trait"
	NodeKindProtocol   NodeKind = "protocol"
	NodeKindFunction   NodeKind = "function"
	NodeKindMethod     NodeKind = "method"
	NodeKindProperty   NodeKind = "property"
	NodeKindField      NodeKind = "field"
	NodeKindVariable   NodeKind = "variable"
	NodeKindConstant   NodeKind = "constant"
	NodeKindEnum       NodeKind = "enum"
	NodeKindEnumMember NodeKind = "enum_member"
	NodeKindTypeAlias  NodeKind = "type_alias"
	NodeKindNamespace  NodeKind = "namespace"
	NodeKindParameter  NodeKind = "parameter"
	NodeKindImport     NodeKind = "import"
	NodeKindExport     NodeKind = "export"
	NodeKindRoute      NodeKind = "route"
	NodeKindComponent  NodeKind = "component"
	// SQL-specific kinds (added for SQL language support).
	NodeKindTable      NodeKind = "table"
	NodeKindView       NodeKind = "view"
	NodeKindColumn     NodeKind = "column"
	NodeKindProcedure  NodeKind = "procedure"
	NodeKindTrigger    NodeKind = "trigger"
	NodeKindConstraint NodeKind = "constraint"
	NodeKindIndex      NodeKind = "index"
	NodeKindSequence   NodeKind = "sequence"
	NodeKindPolicy     NodeKind = "policy"
)

// AllNodeKinds is the complete set of NodeKind values, ordered as in
// appendix C. Use for iteration and validation; do not append to this slice.
var AllNodeKinds = []NodeKind{
	NodeKindFile,
	NodeKindModule,
	NodeKindClass,
	NodeKindStruct,
	NodeKindInterface,
	NodeKindTrait,
	NodeKindProtocol,
	NodeKindFunction,
	NodeKindMethod,
	NodeKindProperty,
	NodeKindField,
	NodeKindVariable,
	NodeKindConstant,
	NodeKindEnum,
	NodeKindEnumMember,
	NodeKindTypeAlias,
	NodeKindNamespace,
	NodeKindParameter,
	NodeKindImport,
	NodeKindExport,
	NodeKindRoute,
	NodeKindComponent,
	NodeKindTable,
	NodeKindView,
	NodeKindColumn,
	NodeKindProcedure,
	NodeKindTrigger,
	NodeKindConstraint,
	NodeKindIndex,
	NodeKindSequence,
	NodeKindPolicy,
}

// ---------------------------------------------------------------------------
// EdgeKind — the 12 edge-type strings (appendix C, verbatim)
// ---------------------------------------------------------------------------

// EdgeKind is the relationship type between two nodes in the knowledge graph.
// The string value is stored in SQLite and must never be changed once data is
// on disk.
type EdgeKind string

const (
	EdgeKindContains     EdgeKind = "contains"
	EdgeKindCalls        EdgeKind = "calls"
	EdgeKindImports      EdgeKind = "imports"
	EdgeKindExports      EdgeKind = "exports"
	EdgeKindExtends      EdgeKind = "extends"
	EdgeKindImplements   EdgeKind = "implements"
	EdgeKindReferences   EdgeKind = "references"
	EdgeKindTypeOf       EdgeKind = "type_of"
	EdgeKindReturns      EdgeKind = "returns"
	EdgeKindInstantiates EdgeKind = "instantiates"
	EdgeKindOverrides    EdgeKind = "overrides"
	EdgeKindDecorates    EdgeKind = "decorates"
	// EdgeKindWrites records a routine→table mutation: INSERT INTO / UPDATE /
	// DELETE FROM / MERGE INTO in a function or procedure body. Lets
	// code-impact distinguish writers from readers (references).
	EdgeKindWrites EdgeKind = "writes"
)

// AllEdgeKinds is the complete set of EdgeKind values, ordered as in
// appendix C. Use for iteration and validation; do not append to this slice.
var AllEdgeKinds = []EdgeKind{
	EdgeKindContains,
	EdgeKindCalls,
	EdgeKindImports,
	EdgeKindExports,
	EdgeKindExtends,
	EdgeKindImplements,
	EdgeKindReferences,
	EdgeKindTypeOf,
	EdgeKindReturns,
	EdgeKindInstantiates,
	EdgeKindOverrides,
	EdgeKindDecorates,
	EdgeKindWrites,
}

// ---------------------------------------------------------------------------
// Language — the 31 language strings (appendix C, verbatim)
// ---------------------------------------------------------------------------

// Language identifies the programming language of a file or node.
// The string value is stored in SQLite and must never be changed once data is
// on disk.
type Language string

const (
	LanguageTypeScript Language = "typescript"
	LanguageJavaScript Language = "javascript"
	LanguageTSX        Language = "tsx"
	LanguageJSX        Language = "jsx"
	LanguagePython     Language = "python"
	LanguageGo         Language = "go"
	LanguageRust       Language = "rust"
	LanguageJava       Language = "java"
	LanguageC          Language = "c"
	LanguageCpp        Language = "cpp"
	LanguageCSharp     Language = "csharp"
	LanguagePHP        Language = "php"
	LanguageRuby       Language = "ruby"
	LanguageSwift      Language = "swift"
	LanguageKotlin     Language = "kotlin"
	LanguageDart       Language = "dart"
	LanguageSvelte     Language = "svelte"
	LanguageVue        Language = "vue"
	LanguageLiquid     Language = "liquid"
	LanguagePascal     Language = "pascal"
	LanguageScala      Language = "scala"
	LanguageLua        Language = "lua"
	LanguageLuau       Language = "luau"
	LanguageObjC       Language = "objc"
	LanguageYAML       Language = "yaml"
	LanguageTwig       Language = "twig"
	LanguageXML        Language = "xml"
	LanguageProperties Language = "properties"
	LanguageUnknown    Language = "unknown"
	LanguageSQL        Language = "sql"
	LanguageElixir     Language = "elixir"
	LanguageErlang     Language = "erlang"
)

// AllLanguages is the complete set of Language values, ordered as in
// appendix C. Use for iteration and validation; do not append to this slice.
var AllLanguages = []Language{
	LanguageTypeScript,
	LanguageJavaScript,
	LanguageTSX,
	LanguageJSX,
	LanguagePython,
	LanguageGo,
	LanguageRust,
	LanguageJava,
	LanguageC,
	LanguageCpp,
	LanguageCSharp,
	LanguagePHP,
	LanguageRuby,
	LanguageSwift,
	LanguageKotlin,
	LanguageDart,
	LanguageSvelte,
	LanguageVue,
	LanguageLiquid,
	LanguagePascal,
	LanguageScala,
	LanguageLua,
	LanguageLuau,
	LanguageObjC,
	LanguageYAML,
	LanguageTwig,
	LanguageXML,
	LanguageProperties,
	LanguageUnknown,
	LanguageSQL,
	LanguageElixir,
	LanguageErlang,
}

// ---------------------------------------------------------------------------
// Core structs (appendix C + appendix A column names)
// ---------------------------------------------------------------------------

// Node is a symbol in the knowledge graph. Field names follow appendix A
// schema columns (snake_case → CamelCase). JSON tags use the schema column
// names so that any JSON serialisation of nodes stays wire-compatible with the
// reference data model.
//
// JSON-in-TEXT fields (Decorators, TypeParameters, Metadata): see package doc
// for the json.RawMessage convention.
//
// IsExported, IsAsync, IsStatic, IsConst: Go bool; the db layer scans SQLite
// INTEGER (0/1) into bool — see package doc.
type Node struct {
	ID             string          `json:"id"`
	Kind           NodeKind        `json:"kind"`
	Name           string          `json:"name"`
	QualifiedName  string          `json:"qualified_name"`
	FilePath       string          `json:"file_path"`
	Language       Language        `json:"language"`
	StartLine      int             `json:"start_line"`
	EndLine        int             `json:"end_line"`
	StartColumn    int             `json:"start_column"`
	EndColumn      int             `json:"end_column"`
	Docstring      string          `json:"docstring,omitempty"`
	Signature      string          `json:"signature,omitempty"`
	Visibility     string          `json:"visibility,omitempty"`
	IsExported     bool            `json:"is_exported"`
	IsAsync        bool            `json:"is_async"`
	IsStatic       bool            `json:"is_static"`
	IsConst        bool            `json:"is_const"`
	Decorators     json.RawMessage `json:"decorators,omitempty"`
	TypeParameters json.RawMessage `json:"type_parameters,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	UpdatedAt      string          `json:"updated_at,omitempty"`
}

// Edge is a directed relationship between two nodes. Field names follow
// appendix A schema columns.
//
// Metadata (JSON-in-TEXT): see package doc for the json.RawMessage convention.
// Provenance is empty for static edges; "heuristic" for synthesized edges
// (appendix G — the explore/node renderers depend on this value).
type Edge struct {
	ID         int64           `json:"id"`
	Source     string          `json:"source"`
	Target     string          `json:"target"`
	Kind       EdgeKind        `json:"kind"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	Line       int             `json:"line,omitempty"`
	Column     int             `json:"col,omitempty"`
	Provenance string          `json:"provenance,omitempty"`
}

// FileRecord is a row in the files table. Field names follow appendix A.
//
// Errors (JSON-in-TEXT): see package doc for the json.RawMessage convention.
type FileRecord struct {
	Path        string          `json:"path"`
	ContentHash string          `json:"content_hash"`
	Language    Language        `json:"language"`
	Size        int64           `json:"size"`
	ModifiedAt  string          `json:"modified_at"`
	IndexedAt   string          `json:"indexed_at"`
	NodeCount   int             `json:"node_count"`
	Errors      json.RawMessage `json:"errors,omitempty"`
}

// ExtractionResult is the output of one file extraction. It is not persisted
// directly; the db layer breaks it into node/edge/unresolved_ref inserts.
type ExtractionResult struct {
	Nodes                []Node
	Edges                []Edge
	UnresolvedReferences []UnresolvedReference
	Errors               []string
}

// UnresolvedReference is a reference that extraction recorded but resolution
// has not yet turned into an Edge. Field names follow appendix A schema
// columns (unresolved_refs table).
//
// Candidates (JSON-in-TEXT): a JSON array of candidate node IDs / scores
// populated by the resolution layer. See package doc for the json.RawMessage
// convention.
//
// Arguments (EE2): the string-literal arguments of the call site, in positional
// order. Only string-literal args are recorded; non-string args (identifiers,
// expressions) are skipped. nil means no string args (including non-call refs
// such as imports). Persisted as a JSON array in the unresolved_refs.arguments
// TEXT column (added by the v2 forward migration).
type UnresolvedReference struct {
	ID            string          `json:"id"`
	FromNodeID    string          `json:"from_node_id"`
	ReferenceName string          `json:"reference_name"`
	ReferenceKind EdgeKind        `json:"reference_kind"`
	Line          int             `json:"line"`
	Column        int             `json:"col"`
	Candidates    json.RawMessage `json:"candidates,omitempty"`
	FilePath      string          `json:"file_path"`
	Language      Language        `json:"language"`
	Arguments     []string        `json:"arguments,omitempty"`
}

// Subgraph is a self-contained view of a portion of the knowledge graph:
// a node map for O(1) lookup, a flat edge list, the root node IDs for
// rendering, and an optional confidence score.
//
// Stable-sort rule: Subgraph.Nodes is map[string]Node. Go map iteration is
// non-deterministic. Any serialisation or rendering that needs a consistent
// node ordering MUST use SubgraphSortedNodes — never range over Nodes directly.
type Subgraph struct {
	Nodes      map[string]Node
	Edges      []Edge
	Roots      []string
	Confidence float64 // optional; 0 means unset
}

// SubgraphSortedNodes returns the nodes in sg sorted ascending by Node.ID.
// The returned slice is a snapshot; mutations do not affect sg.Nodes.
//
// This helper exists because Subgraph.Nodes is a map and Go map iteration is
// non-deterministic. All serialisation and rendering paths must use this
// function to produce stable, diff-friendly output (see package doc).
func SubgraphSortedNodes(sg Subgraph) []Node {
	nodes := make([]Node, 0, len(sg.Nodes))
	for _, n := range sg.Nodes {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	return nodes
}

// TraversalOptions controls how the graph traversal engine follows edges.
type TraversalOptions struct {
	// MaxDepth limits the number of hops from the starting node. 0 means
	// no limit (use with care on dense graphs).
	MaxDepth int
	// EdgeKinds restricts traversal to edges of the given kinds. An empty
	// slice means follow all edge kinds.
	EdgeKinds []EdgeKind
	// Direction is "outgoing", "incoming", or "both". Defaults to "outgoing".
	Direction string
	// IncludeContains controls whether contains edges are followed.
	IncludeContains bool
}

// SearchOptions parameterises a node search query.
type SearchOptions struct {
	// Query is the raw search string (may include field: prefixes).
	Query string
	// Kind filters results to a specific node kind. Empty means all kinds.
	Kind NodeKind
	// Language filters results to a specific language. Empty means all.
	Language Language
	// FilePath is a case-insensitive substring filter on file_path.
	FilePath string
	// Limit caps the number of results. 0 means the caller's default.
	Limit int
}

// SearchResult is one ranked result from a node search.
type SearchResult struct {
	Node  Node
	Score float64
}

// Context is the formatted output of the context builder: structured markdown
// or JSON ready to hand to an AI agent.
//
// Source is "fts", "like", or "fuzzy" depending on which search tier produced
// the results (appendix J).
type Context struct {
	Content   string
	Truncated bool
	Source    string
	NodeCount int
	EdgeCount int
}

// CodeBlock holds a code excerpt with its location metadata, used when
// including source in context or node details.
type CodeBlock struct {
	Content   string
	FilePath  string
	StartLine int
	EndLine   int
	Language  Language
}

// GraphStats summarises the current state of the index. Returned by
// engine.GetStats and surfaced by `atomic code status`.
type GraphStats struct {
	NodeCount     int
	EdgeCount     int
	FileCount     int
	NodesByKind   map[NodeKind]int
	LastIndexedAt string // ISO8601
}
