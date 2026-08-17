// Package types is the shared data contract every engine layer depends on, and
// the only engine package it may depend on in turn.
//
// Three conventions are settled here rather than per-layer.
//
// JSON-in-TEXT columns stay json.RawMessage: their schemas belong to extraction
// and resolution, so the contract layer stays schema-agnostic. A layer needing a
// field unmarshals into a local type.
//
// Integer-bool columns are Go bool here, but modernc.org/sqlite will not convert
// them, so the db layer must scan into an int and compare against 0 itself.
//
// Subgraph.Nodes is a map, so anything that serialises or renders a Subgraph
// must go through SubgraphSortedNodes rather than ranging it directly.
package types

import (
	"encoding/json"
	"fmt"
	"sort"
)

// NodeKind is the type of a symbol node. These string values are persisted in
// SQLite, so none may ever be renamed once an index exists on disk.
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
	// SQL
	NodeKindTable      NodeKind = "table"
	NodeKindView       NodeKind = "view"
	NodeKindColumn     NodeKind = "column"
	NodeKindProcedure  NodeKind = "procedure"
	NodeKindTrigger    NodeKind = "trigger"
	NodeKindConstraint NodeKind = "constraint"
	NodeKindIndex      NodeKind = "index"
	NodeKindSequence   NodeKind = "sequence"
	NodeKindPolicy     NodeKind = "policy"
	// Snowflake and dbt
	NodeKindStage      NodeKind = "stage"
	NodeKindStream     NodeKind = "stream"
	NodeKindTask       NodeKind = "task"
	NodeKindModel      NodeKind = "model"
	NodeKindFileFormat NodeKind = "file_format"
	NodeKindMacro      NodeKind = "macro"
	NodeKindScript     NodeKind = "script"
	// Synthesized hub, one per external package identity, so importers converge
	// on a single target instead of each import node orphaning.
	NodeKindPackage NodeKind = "package"
)

// AllNodeKinds is every NodeKind, for iteration and validation. Read-only.
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
	NodeKindStage,
	NodeKindStream,
	NodeKindTask,
	NodeKindModel,
	NodeKindFileFormat,
	NodeKindMacro,
	NodeKindScript,
	NodeKindPackage,
}

// EdgeKind is the relationship between two nodes. Persisted like NodeKind, with
// the same no-rename rule.
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
	// A routine→table mutation, so impact analysis can tell writers from the
	// readers recorded as references.
	EdgeKindWrites EdgeKind = "writes"
)

// AllEdgeKinds is every EdgeKind, for iteration and validation. Read-only.
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

// ReferenceKindSQLString and ReferenceKindSQLFragment are discriminators valid
// only in UnresolvedReference.ReferenceKind — never an Edge.Kind, hence their
// absence from AllEdgeKinds. Both mark speculative references harvested from
// host-language string literals; the standard pipeline drops them, and the
// SQL string-match passes that consume them always emit EdgeKindReferences.
// See docs/spec/sql-string-match.md.
const ReferenceKindSQLString EdgeKind = "sql_string"

const ReferenceKindSQLFragment EdgeKind = "sql_fragment"

// Language identifies the language of a file or node. Persisted like NodeKind,
// with the same no-rename rule.
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

// AllLanguages is every Language, for iteration and validation. Read-only.
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

// Node is a symbol in the graph. JSON tags mirror the schema column names, so
// serialised nodes stay wire-compatible with the reference data model.
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

// Edge is a directed relationship between two nodes. Provenance is empty for
// statically extracted edges and non-empty for synthesized ones; the explore and
// node renderers branch on it.
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

// FileRecord is a row in the files table.
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

// ExtractionResult is one file's extraction output. Never persisted as a unit —
// the db layer splits it into node, edge, and unresolved_ref inserts.
type ExtractionResult struct {
	Nodes                []Node
	Edges                []Edge
	UnresolvedReferences []UnresolvedReference
	Errors               []string
}

// UnresolvedReference is a reference extraction recorded that resolution has not
// yet turned into an Edge. Candidates holds resolution's scored node IDs.
// Arguments holds only the call site's string-literal arguments in positional
// order, with identifiers and expressions skipped; nil means none.
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
	// CalleeExpr is the callee as written ("emitter.on"), where ReferenceName is
	// only the invoked segment ("on") that resolution matches on. Empty for
	// non-call refs and for older indexes, so receiver-sensitive consumers must
	// fall back to ReferenceName.
	CalleeExpr string `json:"callee_expr,omitempty"`
}

// Subgraph is a self-contained view of part of the graph. Nodes is a map for
// O(1) lookup; serialise it only through SubgraphSortedNodes.
type Subgraph struct {
	Nodes      map[string]Node
	Edges      []Edge
	Roots      []string
	Confidence float64 // 0 means unset
}

// SubgraphSortedNodes snapshots sg's nodes sorted ascending by ID, giving every
// serialisation path stable, diff-friendly output. Mutating it does not affect sg.
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

// MergeSubgraphs unions subgraphs: nodes keyed by ID (last write wins), edges
// deduped on source/target/kind/line/col, roots deduped in first-seen order.
//
// One symbol name routinely maps to several definition nodes — overloads, an
// interface and its implementation, two classes declaring the same method — so
// callers/callees/impact must query every match and merge. Taking only the first
// match silently drops every relationship sitting on the siblings.
func MergeSubgraphs(sgs []Subgraph) Subgraph {
	merged := Subgraph{Nodes: make(map[string]Node)}
	seenEdge := make(map[string]bool)
	seenRoot := make(map[string]bool)
	for _, sg := range sgs {
		for id, n := range sg.Nodes {
			merged.Nodes[id] = n
		}
		for _, e := range sg.Edges {
			key := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d", e.Source, e.Target, e.Kind, e.Line, e.Column)
			if seenEdge[key] {
				continue
			}
			seenEdge[key] = true
			merged.Edges = append(merged.Edges, e)
		}
		for _, r := range sg.Roots {
			if seenRoot[r] {
				continue
			}
			seenRoot[r] = true
			merged.Roots = append(merged.Roots, r)
		}
	}
	return merged
}

// TraversalOptions controls how the traversal engine follows edges.
type TraversalOptions struct {
	// 0 means unlimited, which is punishing on dense graphs.
	MaxDepth int
	// Empty follows every kind.
	EdgeKinds []EdgeKind
	// "outgoing" (default), "incoming", or "both".
	Direction string
	// Whether contains edges are followed.
	IncludeContains bool
}

// SearchOptions parameterises a node search query. Zero-valued filters match
// everything; Limit 0 defers to the caller's default.
type SearchOptions struct {
	// May include field: prefixes.
	Query string
	// Empty means all kinds.
	Kind NodeKind
	// Empty means all languages.
	Language Language
	// Case-insensitive substring match on file_path.
	FilePath string
	// 0 means the caller's default.
	Limit int
}

// SearchResult is one ranked result from a node search.
type SearchResult struct {
	Node  Node
	Score float64
}

// Context is the context builder's agent-ready markdown or JSON. Source names
// the search tier that produced it: "fts", "like", or "fuzzy".
type Context struct {
	Content   string
	Truncated bool
	Source    string
	NodeCount int
	EdgeCount int
}

// CodeBlock is a source excerpt with its location.
type CodeBlock struct {
	Content   string
	FilePath  string
	StartLine int
	EndLine   int
	Language  Language
}

// GraphStats summarises the index, as surfaced by `atomic code status`.
type GraphStats struct {
	NodeCount     int
	EdgeCount     int
	FileCount     int
	NodesByKind   map[NodeKind]int
	LastIndexedAt string // ISO8601
}
