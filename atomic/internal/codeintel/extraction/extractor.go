// Package extraction provides the generic TreeSitterExtractor that drives one
// file through the tree-sitter grammar and produces an ExtractionResult.
//
// The extractor is configured by a LanguageExtractor, which maps grammar
// node-type strings to semantic roles (function, class, import, call, …) and
// supplies optional hook functions for language-specific details (visibility,
// signature, export status, etc.).
//
// Contract (appendix E):
//  1. Receive (filePath, source, language).
//  2. Parse via a pooled parser instance.
//  3. Create the file: node, push onto nodeStack.
//  4. visitNode walks named children, checking type arrays in appendix-E order
//     (function→class→module→method→interface→struct→enum→typeAlias→property→field→
//     variable→import→call→instantiation), calls the matching extract*, sets
//     skipChildren for matched nodes.
//  5. createNode = generateNodeID + "::-joined qualified-name" + contains edge
//     to parent.
//  6. Functions push onto stack, extract type refs (references) + decorators
//     (decorates) + visitFunctionBody → calls/instantiations emit
//     UnresolvedReference (NOT edges).
//  7. Classes extract inheritance (extends/implements).
//  8. Calls emit UnresolvedReference — NOT edges. Resolution makes edges later.
//  9. Return ExtractionResult{Nodes, Edges, UnresolvedReferences, Errors}.
//     Best-effort: errors recorded, never abort.
package extraction

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// LanguageExtractor — the per-language configuration object
// ---------------------------------------------------------------------------

// LanguageExtractor configures the generic extractor for one grammar.
//
// Type sets (maps for O(1) lookup) classify grammar node-type strings by
// semantic role. The field names mirror the reference's TypeScript config
// arrays. Hook functions are nil-safe; the extractor checks for nil before
// calling.
//
// AST node-type strings must match what the grammar actually emits — CP0
// verifies these per language. Use a real-parse probe (see
// tmp/verify_go_grammar.go) before committing a new config.
type LanguageExtractor struct {
	// Type sets — all O(1) via map[string]struct{}.
	// The extractor checks each node against these sets in appendix-E order.
	FunctionTypes      map[string]struct{}
	ClassTypes         map[string]struct{}
	ModuleTypes        map[string]struct{}
	MethodTypes        map[string]struct{}
	InterfaceTypes     map[string]struct{}
	StructTypes        map[string]struct{}
	EnumTypes          map[string]struct{}
	TypeAliasTypes     map[string]struct{}
	PropertyTypes      map[string]struct{}
	FieldTypes         map[string]struct{}
	VariableTypes      map[string]struct{}
	ImportTypes        map[string]struct{}
	CallTypes          map[string]struct{}
	InstantiationTypes map[string]struct{}

	// MacroDoBlockTypes is an optional set of grammar node-type strings that
	// represent a do-block child inside a macro call (e.g. "do_block" in Elixir).
	// When set and a StructTypes node resolves to NodeKind("") (the call-reference
	// sentinel via ResolveKind), the extractor still emits the call reference but
	// ALSO descends into any named children whose kind is in this set. This allows
	// definitions nested inside macro do-blocks (e.g. `on_ee do def foo ... end`)
	// to be discovered without treating the macro as a definition itself.
	//
	// Nil for all non-Elixir languages — no behavior change when unset.
	MacroDoBlockTypes map[string]struct{}

	// JSXElementTypes lists the grammar node-type strings that represent JSX
	// element usages: typically "jsx_element" (paired <Foo>...</Foo>) and
	// "jsx_self_closing_element" (<Foo/>). When visitNode or visitFunctionBody
	// encounters a node of one of these types, it emits a "references"
	// UnresolvedReference for PascalCase tag names only (lowercase host tags
	// like <div> are skipped). Member tags (<Foo.Bar/>) use the last segment.
	// Set on the TSX, JSX, and optionally TS/JS configs.
	JSXElementTypes map[string]struct{}

	// FieldAssignmentTypes lists the grammar node-type strings that represent
	// property/field assignments into an object receiver
	// (e.g. "assignment_expression" in TS/JS/TSX). When visitFunctionBody
	// encounters a node of one of these types it checks:
	//   - left child (field "left") must be a member_expression (this.x, obj.x)
	//   - right child (field "right") must be a callable kind:
	//       identifier, arrow_function, function_expression
	//
	// When both conditions hold, a "references" UnresolvedReference is emitted:
	//   ReferenceName = RHS identifier text (empty for inline arrow/function —
	//                   anonymous callable)
	//   Arguments[0]  = "field:<fieldName>" sentinel — the discriminator the
	//                   callback synthesizer uses to distinguish field-assignment
	//                   refs from plain JSX refs and call refs
	//   FromNodeID    = enclosing method/function node
	//
	// Non-callable RHS values (number, string, template_string, …) are silently
	// skipped — only callable assignments are useful to the synthesizer.
	//
	// Language-agnostic at the assignment_expression level: any grammar that
	// uses "assignment_expression" with left/right fields works without
	// language-specific hooks. Set on TS/JS/TSX configs at minimum.
	FieldAssignmentTypes map[string]struct{}

	// ExportStatementTypes is an optional set of grammar node-type strings that
	// act as export wrappers (e.g. "export_statement" in TypeScript/JavaScript).
	// When visitNode encounters a node of this type, it marks all direct semantic
	// children as exported (IsExported=true) regardless of the IsExported hook.
	// This is the AST-based replacement for text-prefix export detection — it
	// correctly handles "export default function" where the text lookback would
	// only see "default " rather than "export ".
	ExportStatementTypes map[string]struct{}

	// Field names used to locate child nodes in the grammar.
	// Empty string means "not present in this grammar" (nil-safe).
	NameField   string // e.g. "name"
	BodyField   string // e.g. "body"
	ParamsField string // e.g. "parameters"
	ReturnField string // e.g. "result" (Go), "return_type" (TS)

	// Hook functions — all nil-safe; the extractor checks for nil before calling.

	// ResolveBody maps the matched node to its actual body node (e.g.
	// type_declaration → type_spec in Go). Returns the same node when no
	// unwrapping is needed.
	ResolveBody func(ctx context.Context, node sitter.Node, source string) (sitter.Node, error)

	// ResolveKind maps a grammar node matched by StructTypes to its actual
	// semantic NodeKind. Used when one grammar node-type covers multiple semantic
	// kinds (e.g. Go's type_declaration covers struct, interface, and type alias).
	// If nil, nodes matched by StructTypes are always stored as NodeKindStruct.
	// If set and returns NodeKindInterface, extractClass is called with
	// NodeKindInterface; if it returns NodeKindTypeAlias, extractTypeAlias is
	// called; for NodeKindStruct (or any other value), extractStruct is called.
	ResolveKind func(ctx context.Context, node sitter.Node, source string) types.NodeKind

	// GetName returns the name string for a node, overriding the normal NameField
	// fallback path in nameFromNode. It is called with the original (pre-ResolveBody)
	// grammar node. Returns "" to fall through to the normal name extraction.
	// Use this when the grammar requires looking at sibling children to determine
	// the symbol name (e.g. Elixir, where def/defmodule macros encode the name
	// in a different child than what ResolveBody navigates to for body traversal).
	GetName func(ctx context.Context, node sitter.Node, source string) string

	// GetSignature returns the human-readable signature string for a node.
	// Returns "" when not applicable.
	GetSignature func(ctx context.Context, node sitter.Node, source string) string

	// GetVisibility returns the visibility string ("public", "private", …).
	// Returns "" when not applicable.
	GetVisibility func(ctx context.Context, node sitter.Node, source string) string

	// IsExported reports whether the node is exported / public.
	// Called with the raw sitter.Node and full source. For languages where
	// export status is determined solely by the symbol name, prefer
	// IsExportedByName — it is called after name extraction so the resolved
	// name is guaranteed to be available and correct.
	IsExported func(ctx context.Context, node sitter.Node, source string) bool

	// IsExportedByName reports whether a symbol with the given resolved name is
	// exported. When set, it runs after IsExported and overwrites its result.
	// Use this for languages where export status is a pure name predicate (e.g.
	// Go: first rune uppercase → exported).
	IsExportedByName func(name string) bool

	// IsAsync reports whether the node is asynchronous.
	IsAsync func(ctx context.Context, node sitter.Node, source string) bool

	// IsStatic reports whether the node is static.
	IsStatic func(ctx context.Context, node sitter.Node, source string) bool

	// IsConst reports whether the node is constant.
	IsConst func(ctx context.Context, node sitter.Node, source string) bool

	// ExtractImport extracts the import path / module name from an import node.
	// Returns ("", "") when it cannot extract a usable name.
	ExtractImport func(ctx context.Context, node sitter.Node, source string) (name string, path string)

	// ExtractHeritage extracts the base types (superclasses and implemented
	// interfaces) from a class, struct, or interface node. It is called after
	// the class/struct/interface node is created, with the raw AST node.
	//
	// Returns a slice of HeritageRef — one per base type. Each entry carries:
	//   - Name: the simple/last-segment base type name (e.g. "Animal", "Speaker")
	//   - Kind: EdgeKindExtends for superclasses, EdgeKindImplements for interfaces
	//
	// The extractor emits one UnresolvedReference per returned HeritageRef, from
	// the class node just created. Resolution then turns these into extends/implements
	// edges (with appendix-F extends→implements promotion when the target is an
	// interface node).
	//
	// Nil-safe: when ExtractHeritage is nil, no heritage refs are emitted.
	ExtractHeritage func(ctx context.Context, node sitter.Node, source string) []HeritageRef
}

// HeritageRef is one base-type reference returned by ExtractHeritage.
type HeritageRef struct {
	// Name is the simple (last-segment) name of the base type, e.g. "Animal".
	Name string
	// Kind is EdgeKindExtends for superclasses, EdgeKindImplements for interfaces.
	Kind types.EdgeKind
}

// TypeSet returns a map[string]struct{} containing the given strings.
// Used by LanguageExtractor constructors to build O(1) type-lookup sets.
func TypeSet(strs ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(strs))
	for _, s := range strs {
		m[s] = struct{}{}
	}
	return m
}

// ---------------------------------------------------------------------------
// TreeSitterExtractor
// ---------------------------------------------------------------------------

// TreeSitterExtractor parses one file and extracts nodes/edges using a
// LanguageExtractor config. It uses the pool to borrow an Instance per call.
type TreeSitterExtractor struct {
	pool *Pool
	lang Lang
	cfg  LanguageExtractor
}

// NewTreeSitterExtractor creates an extractor backed by the given pool.
func NewTreeSitterExtractor(pool *Pool, lang Lang, cfg LanguageExtractor) *TreeSitterExtractor {
	return &TreeSitterExtractor{pool: pool, lang: lang, cfg: cfg}
}

// Extract parses filePath (source bytes in src, language ident in language) and
// returns an ExtractionResult. Best-effort: if parsing or extraction fails, the
// error is appended to result.Errors and the partial result (file node + any
// already-extracted nodes) is returned — it never panics or discards partial
// work.
func (e *TreeSitterExtractor) Extract(ctx context.Context, filePath, src string, language types.Language) types.ExtractionResult {
	result, err := e.extract(ctx, filePath, src, language)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("extract %s: %v", filePath, err))
	}
	return result
}

// extract is the inner implementation that can return an error (which Extract
// wraps into result.Errors).
func (e *TreeSitterExtractor) extract(ctx context.Context, filePath, src string, language types.Language) (types.ExtractionResult, error) {
	// Borrow a parser instance from the pool.
	inst, err := e.pool.Borrow(ctx)
	if err != nil {
		return types.ExtractionResult{}, fmt.Errorf("borrow: %w", err)
	}
	defer e.pool.Return(inst)

	if err := inst.SetLanguage(ctx, e.lang); err != nil {
		return types.ExtractionResult{}, fmt.Errorf("SetLanguage: %w", err)
	}

	tree, err := inst.ParseString(ctx, src)
	if err != nil {
		return types.ExtractionResult{}, fmt.Errorf("ParseString: %w", err)
	}

	root, err := tree.(*tsTree).rootNode(ctx)
	if err != nil {
		return types.ExtractionResult{}, fmt.Errorf("rootNode: %w", err)
	}

	v := &visitor{
		cfg:         e.cfg,
		filePath:    filePath,
		src:         src,
		language:    language,
		lineOffsets: buildLineOffsets(src),
	}

	// Step 3: create the file: node and push onto nodeStack.
	fileNodeID := "file:" + filePath
	fileNode := types.Node{
		ID:            fileNodeID,
		Kind:          types.NodeKindFile,
		Name:          filePath,
		QualifiedName: filePath,
		FilePath:      filePath,
		Language:      language,
		StartLine:     1,
		EndLine:       strings.Count(src, "\n") + 1,
	}
	v.result.Nodes = append(v.result.Nodes, fileNode)
	v.nodeStack = append(v.nodeStack, stackEntry{id: fileNodeID, name: filePath})

	// Walk named children of root.
	if err := v.visitChildren(ctx, root); err != nil {
		return v.result, fmt.Errorf("visitChildren: %w", err)
	}

	return v.result, nil
}

// ---------------------------------------------------------------------------
// visitor — DFS state machine
// ---------------------------------------------------------------------------

// stackEntry is one frame in the node stack (used to build qualified names).
type stackEntry struct {
	id   string
	name string
}

// visitor holds state for one file extraction pass.
type visitor struct {
	cfg           LanguageExtractor
	filePath      string
	src           string
	language      types.Language
	lineOffsets   []int // byte offset of line N (0-based index → 1-based line)
	result        types.ExtractionResult
	nodeStack     []stackEntry
	forceExported bool // set when visiting children of an ExportStatementTypes node
}

// parentID returns the ID of the current parent (top of nodeStack).
func (v *visitor) parentID() string {
	if len(v.nodeStack) == 0 {
		return ""
	}
	return v.nodeStack[len(v.nodeStack)-1].id
}

// qualifiedName builds the "::-joined" qualified name from the stack + name.
func (v *visitor) qualifiedName(name string) string {
	parts := make([]string, 0, len(v.nodeStack)+1)
	for _, e := range v.nodeStack {
		if e.name != "" && e.name != v.filePath {
			parts = append(parts, e.name)
		}
	}
	if name != "" {
		parts = append(parts, name)
	}
	return strings.Join(parts, "::")
}

// byteToLine converts a byte offset to a 1-based line number using
// pre-computed lineOffsets. Returns 1 when the offset is 0 or out of range.
func (v *visitor) byteToLine(byteOffset uint64) int {
	off := int(byteOffset)
	// Binary search for the largest lineOffset <= off.
	lo, hi := 0, len(v.lineOffsets)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if v.lineOffsets[mid] <= off {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return hi + 1 // hi is the last index where lineOffsets[hi] <= off, +1 → 1-based
}

// buildLineOffsets builds a slice where index i holds the byte offset of the
// start of line i+1 (i.e. lineOffsets[0] == 0 for line 1).
func buildLineOffsets(src string) []int {
	offsets := []int{0}
	for i, c := range src {
		if c == '\n' && i+1 < len(src) {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

// visitChildren walks the direct named children of node, checking each against
// the LanguageExtractor type sets in appendix-E order. A matched node is
// processed and its subtree is not descended (skipChildren semantics).
// Unmatched nodes at this level are descended recursively so nested symbols
// (methods inside a struct body, etc.) are found.
func (v *visitor) visitChildren(ctx context.Context, node sitter.Node) error {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return err
	}
	for i := uint64(0); i < cnt; i++ {
		child, err := node.NamedChild(ctx, i)
		if err != nil {
			return err
		}
		kind, err := child.Kind(ctx)
		if err != nil {
			return err
		}

		skip, err := v.visitNode(ctx, child, kind)
		if err != nil {
			// Best-effort: record and continue.
			v.result.Errors = append(v.result.Errors, fmt.Sprintf("visitNode(%s): %v", kind, err))
			continue
		}
		if skip {
			continue
		}
		// Descend into unmatched nodes.
		if err := v.visitChildren(ctx, child); err != nil {
			v.result.Errors = append(v.result.Errors, fmt.Sprintf("visitChildren: %v", err))
		}
	}
	return nil
}

// visitNode processes one grammar node in appendix-E order.
// Returns skipChildren=true when the node was handled (caller must not recurse).
func (v *visitor) visitNode(ctx context.Context, node sitter.Node, kind string) (skipChildren bool, err error) {
	cfg := v.cfg

	// ExportStatementTypes: an export wrapper (e.g. export_statement in TS/JS).
	// When matched, mark all immediate semantic children as exported, then recurse
	// into the node's children to extract them with that flag set.
	// This is AST-based export detection — it correctly handles "export default"
	// where text-prefix lookback only sees "default " (not "export ").
	if cfg.ExportStatementTypes != nil {
		if _, ok := cfg.ExportStatementTypes[kind]; ok {
			prev := v.forceExported
			v.forceExported = true
			err := v.visitChildren(ctx, node)
			v.forceExported = prev
			return true, err
		}
	}

	// Appendix-E order: function → class → method → interface → struct → enum →
	// typeAlias → property → field → variable → import → call → instantiation.
	if cfg.FunctionTypes != nil {
		if _, ok := cfg.FunctionTypes[kind]; ok {
			return true, v.extractFunction(ctx, node)
		}
	}
	if cfg.ClassTypes != nil {
		if _, ok := cfg.ClassTypes[kind]; ok {
			return true, v.extractClass(ctx, node, types.NodeKindClass)
		}
	}
	if cfg.ModuleTypes != nil {
		if _, ok := cfg.ModuleTypes[kind]; ok {
			return true, v.extractClass(ctx, node, types.NodeKindModule)
		}
	}
	if cfg.MethodTypes != nil {
		if _, ok := cfg.MethodTypes[kind]; ok {
			return true, v.extractFunction(ctx, node) // methods use same extractor, different kind
		}
	}
	if cfg.InterfaceTypes != nil {
		if _, ok := cfg.InterfaceTypes[kind]; ok {
			return true, v.extractClass(ctx, node, types.NodeKindInterface)
		}
	}
	if cfg.StructTypes != nil {
		if _, ok := cfg.StructTypes[kind]; ok {
			if cfg.ResolveKind != nil {
				switch cfg.ResolveKind(ctx, node, v.src) {
				case types.NodeKindClass:
					return true, v.extractClass(ctx, node, types.NodeKindClass)
				case types.NodeKindInterface:
					return true, v.extractClass(ctx, node, types.NodeKindInterface)
				case types.NodeKindTypeAlias:
					return true, v.extractTypeAlias(ctx, node)
				case types.NodeKindEnum:
					return true, v.extractEnum(ctx, node)
				// Extended dispatch paths for grammars (e.g. Elixir) where definitions
				// and regular calls share a single node kind and are differentiated by
				// child-node text inside ResolveKind.
				case types.NodeKindFunction:
					return true, v.extractFunction(ctx, node)
				case types.NodeKindModule:
					return true, v.extractClass(ctx, node, types.NodeKindModule)
				case types.NodeKindImport:
					return true, v.extractImport(ctx, node)
				case types.NodeKind(""):
					// Empty sentinel: ResolveKind signals "emit as call reference,
					// not a declaration". Used by Elixir to handle regular call nodes
					// (e.g. User.new(params)) that happen to share the "call" node kind
					// with definition macros (defmodule, def, …).
					v.extractCall(ctx, node, false)
					// If MacroDoBlockTypes is set (Elixir), descend into any do_block
					// children of this call node. This handles macros like `on_ee do
					// def foo ... end` where definitions are nested inside a non-
					// definition macro's do-block. The call itself is still emitted as
					// a call reference; only the do-block children are additionally
					// walked for nested definitions.
					if cfg.MacroDoBlockTypes != nil {
						childCnt, _ := node.NamedChildCount(ctx)
						for ci := uint64(0); ci < childCnt; ci++ {
							ch, chErr := node.NamedChild(ctx, ci)
							if chErr != nil {
								continue
							}
							chKind, chErr := ch.Kind(ctx)
							if chErr != nil {
								continue
							}
							if _, isDoBlock := cfg.MacroDoBlockTypes[chKind]; isDoBlock {
								if descErr := v.visitChildren(ctx, ch); descErr != nil {
									v.result.Errors = append(v.result.Errors, fmt.Sprintf("macro do-block descent: %v", descErr))
								}
							}
						}
					}
					return true, nil
				default:
					// NodeKindStruct or any unrecognised value → struct path.
				}
			}
			return true, v.extractStruct(ctx, node)
		}
	}
	if cfg.EnumTypes != nil {
		if _, ok := cfg.EnumTypes[kind]; ok {
			return true, v.extractEnum(ctx, node)
		}
	}
	if cfg.TypeAliasTypes != nil {
		if _, ok := cfg.TypeAliasTypes[kind]; ok {
			return true, v.extractTypeAlias(ctx, node)
		}
	}
	if cfg.PropertyTypes != nil {
		if _, ok := cfg.PropertyTypes[kind]; ok {
			return true, v.extractSimpleNode(ctx, node, types.NodeKindProperty)
		}
	}
	if cfg.FieldTypes != nil {
		if _, ok := cfg.FieldTypes[kind]; ok {
			return true, v.extractSimpleNode(ctx, node, types.NodeKindField)
		}
	}
	if cfg.VariableTypes != nil {
		if _, ok := cfg.VariableTypes[kind]; ok {
			return true, v.extractSimpleNode(ctx, node, types.NodeKindVariable)
		}
	}
	if cfg.ImportTypes != nil {
		if _, ok := cfg.ImportTypes[kind]; ok {
			return true, v.extractImport(ctx, node)
		}
	}
	if cfg.CallTypes != nil {
		if _, ok := cfg.CallTypes[kind]; ok {
			v.extractCall(ctx, node, false)
			// Do NOT skip the subtree: a call's callee and argument list can hold
			// further calls (method chains like a.b().c(), nested calls like
			// f(g()), and callbacks). Descending visits each so the call graph is
			// complete rather than only capturing the outermost call per statement.
			return false, nil
		}
	}
	if cfg.InstantiationTypes != nil {
		if _, ok := cfg.InstantiationTypes[kind]; ok {
			v.extractCall(ctx, node, true)
			// Descend for the same reason — constructor arguments may contain calls
			// (new Foo(bar())).
			return false, nil
		}
	}
	if cfg.JSXElementTypes != nil {
		if _, ok := cfg.JSXElementTypes[kind]; ok {
			v.extractJSXRef(ctx, node, kind)
			// Do not skip children — JSX elements may contain nested JSX.
			return false, nil
		}
	}

	return false, nil
}

// ---------------------------------------------------------------------------
// Extract helpers — one per semantic role
// ---------------------------------------------------------------------------

// createNode builds a Node and emits a contains edge to the current parent.
// It does NOT push onto nodeStack; callers that push must do so explicitly.
func (v *visitor) createNode(ctx context.Context, sitterNode sitter.Node, kind types.NodeKind, name string) (types.Node, error) {
	startByte, err := sitterNode.StartByte(ctx)
	if err != nil {
		return types.Node{}, err
	}
	endByte, err := sitterNode.EndByte(ctx)
	if err != nil {
		return types.Node{}, err
	}
	startLine := v.byteToLine(startByte)
	endLine := v.byteToLine(endByte)

	qname := v.qualifiedName(name)
	id := generateNodeID(v.filePath, string(kind), qname, startLine)
	docstring := precedingDocstring(startByte, v.src)

	n := types.Node{
		ID:            id,
		Kind:          kind,
		Name:          name,
		QualifiedName: qname,
		FilePath:      v.filePath,
		Language:      v.language,
		StartLine:     startLine,
		EndLine:       endLine,
		Docstring:     docstring,
	}

	// Apply hooks if set.
	if v.cfg.GetSignature != nil {
		n.Signature = v.cfg.GetSignature(ctx, sitterNode, v.src)
	}
	if v.cfg.GetVisibility != nil {
		n.Visibility = v.cfg.GetVisibility(ctx, sitterNode, v.src)
	}
	if v.cfg.IsExported != nil {
		n.IsExported = v.cfg.IsExported(ctx, sitterNode, v.src)
	}
	// IsExportedByName overrides IsExported when set. Languages where export
	// status is determined solely by the symbol name (e.g. Go's uppercase rule)
	// set this hook instead of (or in addition to) IsExported.
	if v.cfg.IsExportedByName != nil {
		n.IsExported = v.cfg.IsExportedByName(name)
	}
	if v.cfg.IsAsync != nil {
		n.IsAsync = v.cfg.IsAsync(ctx, sitterNode, v.src)
	}
	if v.cfg.IsStatic != nil {
		n.IsStatic = v.cfg.IsStatic(ctx, sitterNode, v.src)
	}
	if v.cfg.IsConst != nil {
		n.IsConst = v.cfg.IsConst(ctx, sitterNode, v.src)
	}
	// forceExported overrides hook results when the node is inside an export
	// wrapper (e.g. export_statement). This is the AST-based path; it wins over
	// both IsExported and IsExportedByName because the parent node is authoritative.
	if v.forceExported {
		n.IsExported = true
	}

	v.result.Nodes = append(v.result.Nodes, n)

	// Emit contains edge: parent → this node.
	if parentID := v.parentID(); parentID != "" {
		v.result.Edges = append(v.result.Edges, types.Edge{
			Source: parentID,
			Target: id,
			Kind:   types.EdgeKindContains,
		})
	}

	return n, nil
}

// nameFromNode extracts the text of the NameField child, or falls back to the
// full node text when NameField is empty or the child is absent.
func (v *visitor) nameFromNode(ctx context.Context, node sitter.Node) (string, error) {
	if v.cfg.NameField != "" {
		nameChild, err := childByField(ctx, node, v.cfg.NameField)
		if err == nil && nameChild != nil {
			sb, err2 := nameChild.StartByte(ctx)
			eb, err3 := nameChild.EndByte(ctx)
			if err2 == nil && err3 == nil {
				return nodeText(sb, eb, v.src), nil
			}
		}
	}
	// Fallback: first named child that looks like an identifier.
	cnt, _ := node.NamedChildCount(ctx)
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		kind, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if kind == "identifier" || kind == "type_identifier" || kind == "field_identifier" ||
			kind == "name" || strings.HasSuffix(kind, "_identifier") {
			sb, _ := ch.StartByte(ctx)
			eb, _ := ch.EndByte(ctx)
			return nodeText(sb, eb, v.src), nil
		}
	}
	// Last resort: full node text (truncated).
	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	t := nodeText(sb, eb, v.src)
	if len(t) > 64 {
		t = t[:64]
	}
	return t, nil
}

// resolveBody calls cfg.ResolveBody if set, otherwise returns the same node.
func (v *visitor) resolveBody(ctx context.Context, node sitter.Node) (sitter.Node, error) {
	if v.cfg.ResolveBody != nil {
		return v.cfg.ResolveBody(ctx, node, v.src)
	}
	return node, nil
}

// extractFunction handles function and method nodes.
func (v *visitor) extractFunction(ctx context.Context, node sitter.Node) error {
	resolved, err := v.resolveBody(ctx, node)
	if err != nil {
		resolved = node // best-effort: use original
	}

	kind, err := node.Kind(ctx)
	if err != nil {
		return err
	}

	// Determine NodeKind: method vs function.
	nodeKind := types.NodeKindFunction
	if v.cfg.MethodTypes != nil {
		if _, ok := v.cfg.MethodTypes[kind]; ok {
			nodeKind = types.NodeKindMethod
		}
	}

	// GetName hook overrides nameFromNode when set. Called with the original
	// unresolved node so the hook can navigate to the right child (e.g. Elixir
	// def macros encode the function name in a different child than the body).
	var name string
	if v.cfg.GetName != nil {
		name = v.cfg.GetName(ctx, node, v.src)
	}
	if name == "" {
		name, err = v.nameFromNode(ctx, resolved)
		if err != nil || name == "" {
			name, _ = v.nameFromNode(ctx, node)
		}
	}

	n, err := v.createNode(ctx, node, nodeKind, name)
	if err != nil {
		return err
	}

	// Push function onto stack so nested symbols get qualified names.
	v.nodeStack = append(v.nodeStack, stackEntry{id: n.ID, name: name})
	defer func() {
		v.nodeStack = v.nodeStack[:len(v.nodeStack)-1]
	}()

	// Walk the function body for calls and instantiations.
	if v.cfg.BodyField != "" {
		bodyNode, err := childByField(ctx, node, v.cfg.BodyField)
		if err == nil && bodyNode != nil {
			v.visitFunctionBody(ctx, *bodyNode)
		}
	} else {
		// No BodyField: scan all named children for calls.
		v.visitFunctionBody(ctx, node)
	}

	return nil
}

// extractClass handles class and interface nodes.
func (v *visitor) extractClass(ctx context.Context, node sitter.Node, kind types.NodeKind) error {
	resolved, err := v.resolveBody(ctx, node)
	if err != nil {
		resolved = node
	}

	var name string
	if v.cfg.GetName != nil {
		name = v.cfg.GetName(ctx, node, v.src)
	}
	if name == "" {
		name, err = v.nameFromNode(ctx, resolved)
		if err != nil || name == "" {
			name, _ = v.nameFromNode(ctx, node)
		}
	}

	n, err := v.createNode(ctx, node, kind, name)
	if err != nil {
		return err
	}

	// Emit heritage (extends/implements) UnresolvedReferences from the class node.
	if v.cfg.ExtractHeritage != nil {
		sb, _ := node.StartByte(ctx)
		startLine := v.byteToLine(sb)
		for _, ref := range v.cfg.ExtractHeritage(ctx, node, v.src) {
			if ref.Name == "" {
				continue
			}
			v.result.UnresolvedReferences = append(v.result.UnresolvedReferences, types.UnresolvedReference{
				ID:            GenerateRefID(n.ID, ref.Name, string(ref.Kind), startLine, 0),
				FromNodeID:    n.ID,
				ReferenceName: ref.Name,
				ReferenceKind: ref.Kind,
				Line:          startLine,
				FilePath:      v.filePath,
				Language:      v.language,
			})
		}
	}

	// Push class onto stack so member symbols get qualified names.
	v.nodeStack = append(v.nodeStack, stackEntry{id: n.ID, name: name})
	defer func() {
		v.nodeStack = v.nodeStack[:len(v.nodeStack)-1]
	}()

	// Walk members.
	return v.visitChildren(ctx, resolved)
}

// extractStruct handles struct type nodes.
func (v *visitor) extractStruct(ctx context.Context, node sitter.Node) error {
	resolved, err := v.resolveBody(ctx, node)
	if err != nil {
		resolved = node
	}

	var name string
	if v.cfg.GetName != nil {
		name = v.cfg.GetName(ctx, node, v.src)
	}
	if name == "" {
		name, err = v.nameFromNode(ctx, resolved)
		if err != nil || name == "" {
			name, _ = v.nameFromNode(ctx, node)
		}
	}

	n, err := v.createNode(ctx, node, types.NodeKindStruct, name)
	if err != nil {
		return err
	}

	// Emit heritage UnresolvedReferences from the struct node (e.g. C++ struct bases).
	if v.cfg.ExtractHeritage != nil {
		sb, _ := node.StartByte(ctx)
		startLine := v.byteToLine(sb)
		for _, ref := range v.cfg.ExtractHeritage(ctx, node, v.src) {
			if ref.Name == "" {
				continue
			}
			v.result.UnresolvedReferences = append(v.result.UnresolvedReferences, types.UnresolvedReference{
				ID:            GenerateRefID(n.ID, ref.Name, string(ref.Kind), startLine, 0),
				FromNodeID:    n.ID,
				ReferenceName: ref.Name,
				ReferenceKind: ref.Kind,
				Line:          startLine,
				FilePath:      v.filePath,
				Language:      v.language,
			})
		}
	}

	v.nodeStack = append(v.nodeStack, stackEntry{id: n.ID, name: name})
	defer func() {
		v.nodeStack = v.nodeStack[:len(v.nodeStack)-1]
	}()

	return v.visitChildren(ctx, resolved)
}

// extractEnum handles enum / const_declaration nodes.
func (v *visitor) extractEnum(ctx context.Context, node sitter.Node) error {
	resolved, err := v.resolveBody(ctx, node)
	if err != nil {
		resolved = node
	}

	name, err := v.nameFromNode(ctx, resolved)
	if err != nil || name == "" {
		// Enum-like const blocks may not have a simple name; use a generated one.
		sb, _ := node.StartByte(ctx)
		name = fmt.Sprintf("const_block_L%d", v.byteToLine(sb))
	}

	n, err := v.createNode(ctx, node, types.NodeKindEnum, name)
	if err != nil {
		return err
	}

	v.nodeStack = append(v.nodeStack, stackEntry{id: n.ID, name: name})
	defer func() {
		v.nodeStack = v.nodeStack[:len(v.nodeStack)-1]
	}()

	// Walk members (enum_member / const_spec children).
	return v.visitChildren(ctx, resolved)
}

// extractTypeAlias handles type alias nodes.
func (v *visitor) extractTypeAlias(ctx context.Context, node sitter.Node) error {
	resolved, err := v.resolveBody(ctx, node)
	if err != nil {
		resolved = node
	}

	name, err := v.nameFromNode(ctx, resolved)
	if err != nil || name == "" {
		name, _ = v.nameFromNode(ctx, node)
	}

	_, err = v.createNode(ctx, node, types.NodeKindTypeAlias, name)
	return err
}

// extractSimpleNode handles property, field, and variable nodes.
// It calls resolveBody so that ResolveBody hooks (e.g. the TS/JS
// lexical_declaration → variable_declarator unwrap) are applied before name
// extraction — the name field lives on the declarator, not the declaration.
//
// F-15: after creating the node, visitFunctionBody scans the original node's
// named children for any call/instantiation expressions (e.g. the require()
// call in "local x = require('y')"). This captures top-level call sites that
// are embedded in variable initializers — a gap when extractSimpleNode
// returned skipChildren=true without scanning the RHS.
// The parentID at this point is the enclosing scope (file node at top level),
// so the call ref's FromNodeID is correctly attributed to the file.
func (v *visitor) extractSimpleNode(ctx context.Context, node sitter.Node, kind types.NodeKind) error {
	resolved, err := v.resolveBody(ctx, node)
	if err != nil {
		resolved = node // best-effort: use original on hook error
	}

	name, err := v.nameFromNode(ctx, resolved)
	if err != nil || name == "" {
		// Fallback: try name from the original node.
		name, _ = v.nameFromNode(ctx, node)
	}
	if name == "" {
		return nil // skip unnamed nodes
	}
	_, err = v.createNode(ctx, node, kind, name)
	if err != nil {
		return err
	}

	// Scan the node's children for embedded call/instantiation expressions.
	// visitFunctionBody stops at nested function/method boundaries, so nested
	// function literals in the initializer are handled as separate nodes.
	v.visitFunctionBody(ctx, node)
	return nil
}

// extractImport handles import nodes, emitting an import-kind node and calling
// cfg.ExtractImport for the path.
func (v *visitor) extractImport(ctx context.Context, node sitter.Node) error {
	name := ""
	path := ""
	if v.cfg.ExtractImport != nil {
		name, path = v.cfg.ExtractImport(ctx, node, v.src)
	}
	if name == "" {
		// Fallback: use the raw text.
		sb, _ := node.StartByte(ctx)
		eb, _ := node.EndByte(ctx)
		t := nodeText(sb, eb, v.src)
		if len(t) > 80 {
			t = t[:80]
		}
		name = t
	}

	n, err := v.createNode(ctx, node, types.NodeKindImport, name)
	if err != nil {
		return err
	}

	// Also emit an UnresolvedReference for the import path so the resolution
	// layer can link it.
	if path != "" {
		sb, _ := node.StartByte(ctx)
		importLine := v.byteToLine(sb)
		v.result.UnresolvedReferences = append(v.result.UnresolvedReferences, types.UnresolvedReference{
			ID:            GenerateRefID(n.ID, path, string(types.EdgeKindImports), importLine, 0),
			FromNodeID:    n.ID,
			ReferenceName: path,
			ReferenceKind: types.EdgeKindImports,
			Line:          importLine,
			FilePath:      v.filePath,
			Language:      v.language,
		})
	}

	return nil
}

// extractCall records a call or instantiation as an UnresolvedReference.
// It does NOT emit an Edge — resolution makes call edges.
//
// EE2: string-literal arguments are captured into Arguments (positional order).
// Only nodes whose grammar kind is "string" (or "string_literal") are recorded;
// their text content is extracted from a "string_fragment" named child when
// present, otherwise from the node text with surrounding quotes stripped.
// Non-string args (identifiers, expressions, template literals) are skipped.
// This is language-agnostic at the call_expression level — the same walk works
// for JS, TS, and any grammar that uses "string" / "string_fragment" node types.
func (v *visitor) extractCall(ctx context.Context, node sitter.Node, isInstantiation bool) {
	// Determine the callee name. calleeName is the bare invoked segment (what
	// resolution matches); calleeExpr is the full callee expression (what the
	// callback synthesizers match on, e.g. "emitter.on").
	calleeName, calleeExpr := v.calleeNameAndExpr(ctx, node)
	if calleeName == "" {
		return
	}
	// Only retain calleeExpr when it carries more than the bare name (a receiver);
	// for a plain "foo()" call it equals calleeName, so store nothing (NULL).
	if calleeExpr == calleeName {
		calleeExpr = ""
	}

	sb, _ := node.StartByte(ctx)
	startLine := v.byteToLine(sb)

	refKind := types.EdgeKindCalls
	if isInstantiation {
		refKind = types.EdgeKindInstantiates
	}

	fromID := v.parentID()
	v.result.UnresolvedReferences = append(v.result.UnresolvedReferences, types.UnresolvedReference{
		ID:            GenerateRefID(fromID, calleeName, string(refKind), startLine, 0),
		FromNodeID:    fromID,
		ReferenceName: calleeName,
		CalleeExpr:    calleeExpr,
		ReferenceKind: refKind,
		Line:          startLine,
		FilePath:      v.filePath,
		Language:      v.language,
		Arguments:     v.extractCallArgs(ctx, node),
	})
}

// extractCallArgs returns the arguments of a call_expression node in positional
// order, capturing both string literals and identifiers.
//
// EE2 — string-literal args: recorded as their content (e.g. "login") without
// any prefix. Only "string" / "string_literal" grammar nodes are recorded;
// their text is extracted from a "string_fragment" named child when present,
// otherwise the node text is stripped of surrounding quotes.
//
// EE5 — identifier args: recorded with an "arg:" prefix (e.g. "arg:onLogin").
// Only plain "identifier" grammar nodes are captured — not member_expression,
// arrow_function, call_expression, or any other compound form. The prefix
// makes identifier args distinguishable from string args so synthesizers that
// read Arguments can gate on prefix (event-emitter reads plain strings;
// closure-collection reads "arg:" entries).
//
// Non-colliding prefix table:
//
//	"arg:"   — EE5 identifier arg (this function)
//	"field:" — EE3 field-assignment discriminator (extractFieldAssignment)
//	"jsx:"   — EE1 JSX child-component discriminator (extractJSXRef)
//
// Returns nil when there are no capturable arguments, matching the nil
// convention for JSON-in-TEXT columns (NULL in SQLite).
func (v *visitor) extractCallArgs(ctx context.Context, callNode sitter.Node) []string {
	// The "arguments" field of call_expression holds the argument list node.
	argsNode, err := childByField(ctx, callNode, "arguments")
	if err != nil || argsNode == nil {
		return nil
	}
	isNull, _ := argsNode.IsNull(ctx)
	if isNull {
		return nil
	}

	cnt, err := argsNode.NamedChildCount(ctx)
	if err != nil || cnt == 0 {
		return nil
	}

	var result []string
	for i := uint64(0); i < cnt; i++ {
		arg, err := argsNode.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		argKind, err := arg.Kind(ctx)
		if err != nil {
			continue
		}

		switch argKind {
		case "string", "string_literal":
			// EE2: string-literal argument → record content without prefix.
			// Prefer text from a "string_fragment" named child (the content
			// between the delimiters, without quotes). This is the pattern
			// produced by JS/TS grammars for single/double-quoted strings:
			//   string → string_fragment (content)
			fragmentCnt, _ := arg.NamedChildCount(ctx)
			if fragmentCnt > 0 {
				frag, fragErr := arg.NamedChild(ctx, 0)
				if fragErr == nil {
					fragKind, _ := frag.Kind(ctx)
					if fragKind == "string_fragment" {
						fsb, _ := frag.StartByte(ctx)
						feb, _ := frag.EndByte(ctx)
						result = append(result, nodeText(fsb, feb, v.src))
						continue
					}
				}
			}
			// Fallback: strip surrounding quotes from the node text.
			asb, _ := arg.StartByte(ctx)
			aeb, _ := arg.EndByte(ctx)
			text := nodeText(asb, aeb, v.src)
			text = strings.Trim(text, `"'`+"`")
			if text != "" {
				result = append(result, text)
			}

		case "identifier":
			// EE5: plain identifier argument (e.g. handler, onE, cb) → record
			// with "arg:" prefix to distinguish from string-literal args.
			// Compound forms (member_expression, call_expression, arrow_function,
			// etc.) are intentionally skipped — they cannot be used for stable
			// name-based correlation by synthesizers.
			asb, _ := arg.StartByte(ctx)
			aeb, _ := arg.EndByte(ctx)
			name := nodeText(asb, aeb, v.src)
			if name != "" {
				result = append(result, "arg:"+name)
			}
		}
		// All other arg kinds (number, member_expression, arrow_function, etc.)
		// are intentionally skipped — dynamic/compound values add noise and
		// cannot be used for stable correlation.
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// calleeNameFromNode extracts the callee name from a call_expression node.
// For "foo()" → "foo"; for "pkg.Bar()" → "pkg.Bar"; for "obj.method()" →
// "obj.method". Returns "" when the name cannot be determined.
func (v *visitor) calleeNameAndExpr(ctx context.Context, node sitter.Node) (bare, expr string) {
	// The "function" field of call_expression holds the callee.
	fnNode, err := childByField(ctx, node, "function")
	if err != nil || fnNode == nil {
		// Fallback: try first named child.
		cnt, _ := node.NamedChildCount(ctx)
		if cnt == 0 {
			return "", ""
		}
		ch, err := node.NamedChild(ctx, 0)
		if err != nil {
			return "", ""
		}
		fnNode = &ch
	}
	sb, _ := fnNode.StartByte(ctx)
	eb, _ := fnNode.EndByte(ctx)
	expr = nodeText(sb, eb, v.src)
	// bare is the final invoked segment — the method/function actually called.
	// When the callee is a member/selector access (obj.method, a.b.c.method,
	// pkg.Func), the name matcher resolves the bare segment; the full "a.b.method"
	// subtree text never matches a node and is permanently unresolvable. expr keeps
	// the full callee for consumers that need the receiver (callback synthesizers).
	// The JSX tag handler does the same last-segment reduction (see extractJSXRef).
	bare = finalCalleeSegment(ctx, *fnNode, v.src)
	if bare == "" {
		bare = expr // plain identifier callee (e.g. "foo") — bare == full.
	}
	return bare, expr
}

// memberAccessFields maps a callee grammar node kind to the field that holds the
// final invoked segment (the property/method on the right of a member access).
// Probed against the live grammars; extend per language as call resolution is
// validated for it (see scripts/code-eval corpus). Keyed by node kind because
// kinds are distinct enough across grammars that a kind→field map needs no
// language discriminator.
var memberAccessFields = map[string]string{
	"member_expression":   "property", // JavaScript, TypeScript, JSX, TSX
	"selector_expression": "field",    // Go
}

// finalCalleeSegment returns the text of the final invoked segment when the
// callee node is a recognised member-access kind (e.g. "a.b.method" → "method"),
// or "" when it is not — in which case the caller uses the full node text
// (correct for a plain identifier callee like "foo").
func finalCalleeSegment(ctx context.Context, callee sitter.Node, src string) string {
	kind, err := callee.Kind(ctx)
	if err != nil {
		return ""
	}
	field, ok := memberAccessFields[kind]
	if !ok {
		return ""
	}
	seg, err := childByField(ctx, callee, field)
	if err != nil || seg == nil {
		return ""
	}
	if isNull, _ := seg.IsNull(ctx); isNull {
		return ""
	}
	sb, _ := seg.StartByte(ctx)
	eb, _ := seg.EndByte(ctx)
	return nodeText(sb, eb, src)
}

// extractJSXRef emits a "references" UnresolvedReference for a JSX element node
// when the tag name is PascalCase. Lowercase names (host/DOM elements like <div>)
// are silently skipped. Member tags (<Foo.Bar/>) use the last dot-separated segment.
//
// Tag name location (verified against real tsx grammar parse):
//   - jsx_self_closing_element: first named child = identifier | member_expression
//   - jsx_element: first named child = jsx_opening_element, whose first named
//     child = identifier | member_expression
func (v *visitor) extractJSXRef(ctx context.Context, node sitter.Node, kind string) {
	tagNode, err := v.jsxTagNode(ctx, node, kind)
	if err != nil || tagNode == nil {
		return
	}
	isNull, _ := tagNode.IsNull(ctx)
	if isNull {
		return
	}

	tagKind, err := tagNode.Kind(ctx)
	if err != nil {
		return
	}

	sb, _ := tagNode.StartByte(ctx)
	eb, _ := tagNode.EndByte(ctx)
	tagText := nodeText(sb, eb, v.src)
	if tagText == "" {
		return
	}

	var componentName string
	switch tagKind {
	case "identifier":
		componentName = tagText
	case "member_expression":
		// Use last segment: "Foo.Bar" → "Bar"
		parts := strings.Split(tagText, ".")
		componentName = parts[len(parts)-1]
	default:
		return
	}

	// Skip lowercase host/DOM tags (e.g. div, span, p, a, button).
	// PascalCase = first rune is uppercase.
	if componentName == "" || !isUpperRune(componentName[0]) {
		return
	}

	startLine := v.byteToLine(sb)
	fromID := v.parentID()
	// EE1 discriminator: Arguments[0] = "jsx:<TagName>" marks this as a JSX
	// child-component reference. The resolution pipeline propagates this onto the
	// static edge's Metadata so synthesis (jsx-render) can distinguish JSX-origin
	// references edges from type-annotation references edges at synthesis time.
	// This is the general origin-ref discriminator mechanism reused by batches 3–6.
	v.result.UnresolvedReferences = append(v.result.UnresolvedReferences, types.UnresolvedReference{
		ID:            GenerateRefID(fromID, componentName, string(types.EdgeKindReferences), startLine, 0),
		FromNodeID:    fromID,
		ReferenceName: componentName,
		ReferenceKind: types.EdgeKindReferences,
		Line:          startLine,
		FilePath:      v.filePath,
		Language:      v.language,
		Arguments:     []string{"jsx:" + componentName},
	})
}

// jsxTagNode returns the sitter.Node that holds the tag name text for a JSX
// element node. Returns nil when the structure is unexpected.
//
//   - jsx_self_closing_element: first named child is the name node
//   - jsx_element: first named child is jsx_opening_element, whose first named
//     child is the name node
func (v *visitor) jsxTagNode(ctx context.Context, node sitter.Node, kind string) (*sitter.Node, error) {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil || cnt == 0 {
		return nil, err
	}
	first, err := node.NamedChild(ctx, 0)
	if err != nil {
		return nil, err
	}

	if kind == "jsx_element" {
		// first child is jsx_opening_element; tag name is ITS first named child.
		openKind, err := first.Kind(ctx)
		if err != nil || openKind != "jsx_opening_element" {
			return nil, nil
		}
		innerCnt, err := first.NamedChildCount(ctx)
		if err != nil || innerCnt == 0 {
			return nil, nil
		}
		inner, err := first.NamedChild(ctx, 0)
		if err != nil {
			return nil, err
		}
		return &inner, nil
	}

	// jsx_self_closing_element: first named child is the name node.
	return &first, nil
}

// isUpperRune reports whether the first byte of name is an ASCII uppercase letter.
// This is safe for PascalCase detection because HTML/SVG tag names never start
// with ASCII uppercase; React component names always do.
func isUpperRune(b byte) bool {
	return b >= 'A' && b <= 'Z'
}

// extractFieldAssignment emits a "references" UnresolvedReference for an
// assignment_expression node whose LHS is a member_expression (property write)
// and whose RHS is a callable (identifier, arrow_function, function_expression).
// It returns true when a ref was emitted, false when nothing was emitted (e.g.
// non-member-expression LHS or non-callable RHS).
//
// EE3 convention: the emitted ref carries
//   - ReferenceKind = EdgeKindReferences (same as JSX refs)
//   - ReferenceName = the RHS identifier name, or "" for anonymous callables
//     (arrow_function / function_expression without a name)
//   - Arguments[0]  = "field:<propertyName>" — the single-element slice is the
//     discriminator the callback synthesizer uses to distinguish this ref from
//     EE1 JSX refs (no Arguments) and EE2 call refs (Arguments = event name).
//     A synthesizer must check BOTH: ReferenceKind == EdgeKindReferences AND
//     len(Arguments) > 0 && strings.HasPrefix(Arguments[0], "field:"). An EE2
//     ref like emitter.on("field:x", cb) has EdgeKindCalls, not references, so
//     the kind check is the outer gate; the "field:" prefix is the inner gate.
//   - FromNodeID    = v.parentID() at call time (the enclosing method/function)
//
// Non-callable RHS values (number, string, template_string, …) are silently
// skipped — they carry no information useful to the synthesizer.
func (v *visitor) extractFieldAssignment(ctx context.Context, node sitter.Node) bool {
	// Resolve the "left" field — must be a member_expression.
	leftNode, err := childByField(ctx, node, "left")
	if err != nil || leftNode == nil {
		return false
	}
	leftKind, err := leftNode.Kind(ctx)
	if err != nil || leftKind != "member_expression" {
		return false
	}

	// Extract the property name from the member_expression's "property" field.
	propNode, err := childByField(ctx, *leftNode, "property")
	if err != nil || propNode == nil {
		// Fallback: last named child is the property identifier in most grammars.
		cnt, _ := leftNode.NamedChildCount(ctx)
		if cnt == 0 {
			return false
		}
		ch, chErr := leftNode.NamedChild(ctx, cnt-1)
		if chErr != nil {
			return false
		}
		propNode = &ch
	}
	sb, _ := propNode.StartByte(ctx)
	eb, _ := propNode.EndByte(ctx)
	fieldName := nodeText(sb, eb, v.src)
	if fieldName == "" {
		return false
	}

	// Resolve the "right" field — must be a callable kind.
	rightNode, err := childByField(ctx, node, "right")
	if err != nil || rightNode == nil {
		return false
	}
	rightKind, err := rightNode.Kind(ctx)
	if err != nil {
		return false
	}

	// Determine callable name and whether the RHS is a recognized callable.
	var callableName string
	switch rightKind {
	case "identifier":
		// Named callable: `this.onData = handleData` → callableName = "handleData".
		rsb, _ := rightNode.StartByte(ctx)
		reb, _ := rightNode.EndByte(ctx)
		callableName = nodeText(rsb, reb, v.src)
	case "arrow_function", "function_expression":
		// Anonymous callable: `this.h = () => {}` or `this.h = function() {}`.
		// callableName stays "".
	default:
		// Non-callable RHS (number, string, template_string, binary_expression, …).
		// Silently skip — no useful signal for the synthesizer.
		return false
	}

	rsb, _ := rightNode.StartByte(ctx)
	startLine := v.byteToLine(rsb)
	fromID := v.parentID()
	v.result.UnresolvedReferences = append(v.result.UnresolvedReferences, types.UnresolvedReference{
		ID:            GenerateRefID(fromID, callableName, string(types.EdgeKindReferences), startLine, 0),
		FromNodeID:    fromID,
		ReferenceName: callableName,
		ReferenceKind: types.EdgeKindReferences,
		Line:          startLine,
		FilePath:      v.filePath,
		Language:      v.language,
		// Arguments[0] = "field:<fieldName>" is the EE3 discriminator sentinel.
		// A callback synthesizer distinguishes EE3 refs from EE1 JSX refs (no
		// Arguments) and EE2 call refs (Arguments = event-name strings) by
		// checking ReferenceKind == EdgeKindReferences AND
		// len(Arguments) > 0 && strings.HasPrefix(Arguments[0], "field:").
		Arguments: []string{"field:" + fieldName},
	})
	return true
}

// visitFunctionBody scans a body node for call_expression and any other
// configured call/instantiation types, emitting UnresolvedReferences.
// This is a recursive walk that stops at nested function boundaries (those
// are extracted as separate nodes).
func (v *visitor) visitFunctionBody(ctx context.Context, body sitter.Node) {
	cnt, err := body.NamedChildCount(ctx)
	if err != nil {
		return
	}
	for i := uint64(0); i < cnt; i++ {
		child, err := body.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		kind, err := child.Kind(ctx)
		if err != nil {
			continue
		}

		// Stop at nested function/method boundaries.
		if v.cfg.FunctionTypes != nil {
			if _, ok := v.cfg.FunctionTypes[kind]; ok {
				continue // nested function — will be extracted separately
			}
		}
		if v.cfg.MethodTypes != nil {
			if _, ok := v.cfg.MethodTypes[kind]; ok {
				continue
			}
		}

		// Check for calls.
		if v.cfg.CallTypes != nil {
			if _, ok := v.cfg.CallTypes[kind]; ok {
				// When this node kind is ALSO in StructTypes and ResolveKind is set,
				// check whether ResolveKind classifies it as a definition (non-empty
				// non-"" kind) or as a call reference (empty ""). This is required for
				// grammars like Elixir where definition macros and regular function calls
				// share the same "call" node kind: defmodule/def/defp are definitions
				// (ResolveKind returns a real NodeKind), while User.new() is a call
				// (ResolveKind returns ""). Without this check, all "call" nodes in a
				// function body would be treated as call references, incorrectly emitting
				// def/defmodule nodes as EdgeKindCalls entries.
				if v.cfg.StructTypes != nil && v.cfg.ResolveKind != nil {
					if _, inStruct := v.cfg.StructTypes[kind]; inStruct {
						resolved := v.cfg.ResolveKind(ctx, child, v.src)
						if resolved != types.NodeKind("") {
							// It's a definition node — skip the extractCall path.
							// visitChildren (called by the parent extractClass) handles
							// it via the StructTypes/ResolveKind dispatch in visitNode.
							continue
						}
					}
				}
				v.extractCall(ctx, child, false)
				// Recurse: a call's callee and arguments can hold further calls
				// (method chains a.b().c(), nested calls f(g())). Without this only
				// the outermost call per statement is captured. Mirrors the JSX arm
				// below.
				v.visitFunctionBody(ctx, child)
				continue
			}
		}
		if v.cfg.InstantiationTypes != nil {
			if _, ok := v.cfg.InstantiationTypes[kind]; ok {
				v.extractCall(ctx, child, true)
				// Recurse for calls nested in constructor arguments (new Foo(bar())).
				v.visitFunctionBody(ctx, child)
				continue
			}
		}
		// Check for JSX element usages.
		if v.cfg.JSXElementTypes != nil {
			if _, ok := v.cfg.JSXElementTypes[kind]; ok {
				v.extractJSXRef(ctx, child, kind)
				// Recurse so nested JSX elements (children of this element) are found.
				v.visitFunctionBody(ctx, child)
				continue
			}
		}
		// EE3: check for field-assignment expressions (e.g. this.onData = handler).
		// Only skip recursion when a field-assignment ref was actually emitted
		// (member_expression LHS + callable RHS). When extractFieldAssignment
		// returns false it emitted nothing — fall through to the normal recursion
		// so any call_expression nested in the assignment RHS (e.g. the `factory`
		// call in `x = factory('evt')`) is still captured by the CallTypes arm.
		if v.cfg.FieldAssignmentTypes != nil {
			if _, ok := v.cfg.FieldAssignmentTypes[kind]; ok {
				if v.extractFieldAssignment(ctx, child) {
					continue
				}
			}
		}

		// Recurse into this child.
		v.visitFunctionBody(ctx, child)
	}
}
