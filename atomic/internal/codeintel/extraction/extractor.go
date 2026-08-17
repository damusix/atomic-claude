// Package extraction drives one file through its tree-sitter grammar and produces
// an ExtractionResult.
//
// A LanguageExtractor maps grammar node-type strings to semantic roles and
// supplies nil-safe hooks for the language-specific details. Calls,
// instantiations and heritage leave as UnresolvedReference, never as edges —
// resolution turns them into edges once every file is indexed. Extraction is
// best-effort: an error is recorded in result.Errors and the partial result is
// still returned.
package extraction

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// --- LanguageExtractor: the per-language configuration object ---

// LanguageExtractor configures the generic extractor for one grammar. The type
// sets classify grammar node-type strings by semantic role; every hook is
// nil-safe. Node-type strings must match what the grammar actually emits, so
// probe a real parse before committing a new config.
type LanguageExtractor struct {
	// Checked against each node in the order declared here.
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

	// FunctionScopeTypes open a function scope without being a FunctionTypes
	// match (TS/JS arrow and function expressions). visitChildren tracks depth
	// through them and VariableTypes mints only at depth 0, which is what
	// suppresses locals inside callbacks; a named function body is already
	// covered by visitFunctionBody having no VariableTypes arm. Nil elsewhere.
	FunctionScopeTypes map[string]struct{}

	// MacroDoBlockTypes are do-block children of a macro call (Elixir "do_block").
	// When a StructTypes node resolves to the call-reference sentinel kind, the
	// extractor emits that reference and still descends into these children, so a
	// def nested in a macro block is found without the macro itself becoming a
	// definition. Nil elsewhere.
	MacroDoBlockTypes map[string]struct{}

	// JSXElementTypes are JSX element usages. Only a PascalCase tag emits a
	// reference — a lowercase tag is a host element, not a component. A member tag
	// (<Foo.Bar/>) uses its last segment.
	JSXElementTypes map[string]struct{}

	// FieldAssignmentTypes are property assignments into an object receiver
	// ("assignment_expression"). A reference is emitted only when the left side is
	// a member expression and the right side is callable; an inline arrow gives an
	// empty ReferenceName. Arguments[0] carries a "field:<name>" sentinel — the
	// discriminator the callback synthesizer uses to tell these from JSX and call
	// refs. Any grammar with left/right fields on that node type works.
	FieldAssignmentTypes map[string]struct{}

	// ExportStatementTypes are export wrappers ("export_statement"). Every direct
	// semantic child is marked exported regardless of the IsExported hook: a text
	// lookback on "export default function" only ever sees "default ".
	ExportStatementTypes map[string]struct{}

	// Grammar field names; empty means the grammar has no such field.
	NameField   string // e.g. "name"
	BodyField   string // e.g. "body"
	ParamsField string // e.g. "parameters"
	ReturnField string // e.g. "result" (Go), "return_type" (TS)

	// ResolveBody unwraps a matched node to its real body node (Go's
	// type_declaration → type_spec), returning it unchanged when there is nothing
	// to unwrap.
	ResolveBody func(ctx context.Context, node sitter.Node, source string) (sitter.Node, error)

	// ResolveKind gives the real NodeKind for a StructTypes match, for grammars
	// where one node type covers several kinds (Go's type_declaration is struct,
	// interface and alias at once). Nil means always NodeKindStruct.
	ResolveKind func(ctx context.Context, node sitter.Node, source string) types.NodeKind

	// GetName overrides the NameField lookup, taking the pre-ResolveBody node and
	// returning "" to fall through. Needed where the name lives in a different
	// child than the body ResolveBody walks to, as in Elixir's def macros.
	GetName func(ctx context.Context, node sitter.Node, source string) string

	// GetSignature returns a human-readable signature, or "" when not applicable.
	GetSignature func(ctx context.Context, node sitter.Node, source string) string

	// GetVisibility returns "public", "private", …, or "" when not applicable.
	GetVisibility func(ctx context.Context, node sitter.Node, source string) string

	// IsExported reports whether the node is exported. Prefer IsExportedByName
	// where export status is a pure name predicate — it runs after name
	// extraction, so the resolved name is guaranteed correct.
	IsExported func(ctx context.Context, node sitter.Node, source string) bool

	// IsExportedByName runs after IsExported and overwrites its result. For
	// languages where the name decides, as in Go's leading uppercase.
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

	// ExtractHeritage returns the base types of a class, struct, or interface, one
	// HeritageRef each. The extractor emits an UnresolvedReference per ref from
	// the node just created; resolution promotes extends to implements when the
	// target turns out to be an interface. Nil emits nothing.
	ExtractHeritage func(ctx context.Context, node sitter.Node, source string) []HeritageRef
}

// HeritageRef is one base-type reference returned by ExtractHeritage.
type HeritageRef struct {
	// Name is the simple (last-segment) name of the base type, e.g. "Animal".
	Name string
	// Kind is EdgeKindExtends for superclasses, EdgeKindImplements for interfaces.
	Kind types.EdgeKind
}

// TypeSet builds an O(1) lookup set from the given node-type strings.
func TypeSet(strs ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(strs))
	for _, s := range strs {
		m[s] = struct{}{}
	}
	return m
}

// --- TreeSitterExtractor ---

// TreeSitterExtractor parses one file, borrowing a parser instance per call.
type TreeSitterExtractor struct {
	pool *Pool
	lang Lang
	cfg  LanguageExtractor
}

// NewTreeSitterExtractor creates an extractor backed by the given pool.
func NewTreeSitterExtractor(pool *Pool, lang Lang, cfg LanguageExtractor) *TreeSitterExtractor {
	return &TreeSitterExtractor{pool: pool, lang: lang, cfg: cfg}
}

// Extract parses one file. Best-effort: a failure is appended to result.Errors
// and the partial result is returned rather than discarded.
func (e *TreeSitterExtractor) Extract(ctx context.Context, filePath, src string, language types.Language) types.ExtractionResult {
	result, err := e.extract(ctx, filePath, src, language)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("extract %s: %v", filePath, err))
	}
	return result
}

// extract is Extract's inner form, which may return an error.
func (e *TreeSitterExtractor) extract(ctx context.Context, filePath, src string, language types.Language) (types.ExtractionResult, error) {
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

	if err := v.visitChildren(ctx, root); err != nil {
		return v.result, fmt.Errorf("visitChildren: %w", err)
	}

	return v.result, nil
}

// --- visitor: DFS state machine ---

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
	scopeDepth    int  // incremented while descending into a FunctionScopeTypes node
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

// visitChildren walks direct named children, checking each against the type sets
// in declaration order. A matched node is not descended into; an unmatched one is,
// so nested symbols such as methods in a struct body are still found.
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
			v.result.Errors = append(v.result.Errors, fmt.Sprintf("visitNode(%s): %v", kind, err))
			continue
		}
		if skip {
			continue
		}

		// A scope-opening node suppresses VariableTypes matches beneath it — see
		// extractSimpleNode.
		isScope := false
		if v.cfg.FunctionScopeTypes != nil {
			if _, ok := v.cfg.FunctionScopeTypes[kind]; ok {
				isScope = true
			}
		}
		if isScope {
			v.scopeDepth++
		}
		if err := v.visitChildren(ctx, child); err != nil {
			v.result.Errors = append(v.result.Errors, fmt.Sprintf("visitChildren: %v", err))
		}
		if isScope {
			v.scopeDepth--
		}
	}
	return nil
}

// visitNode handles one grammar node, returning true when the caller must not
// recurse into it.
func (v *visitor) visitNode(ctx context.Context, node sitter.Node, kind string) (skipChildren bool, err error) {
	cfg := v.cfg

	// An export wrapper marks its children exported: a text-prefix lookback on
	// "export default" only ever sees "default ".
	if cfg.ExportStatementTypes != nil {
		if _, ok := cfg.ExportStatementTypes[kind]; ok {
			prev := v.forceExported
			v.forceExported = true
			err := v.visitChildren(ctx, node)
			v.forceExported = prev
			return true, err
		}
	}

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
				// Grammars where definitions and plain calls share one node kind.
				case types.NodeKindFunction:
					return true, v.extractFunction(ctx, node)
				case types.NodeKindModule:
					return true, v.extractClass(ctx, node, types.NodeKindModule)
				case types.NodeKindImport:
					return true, v.extractImport(ctx, node)
				case types.NodeKind(""):
					// ResolveKind asks for a call reference, not a declaration.
					v.extractCall(ctx, node, false)
					// Descend into do-blocks so a def nested in a non-definition
					// macro is still found; the call stays a call reference.
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
			// Do not skip: a callee or argument list can hold further calls
			// (a.b().c(), f(g()), callbacks), and each belongs in the graph.
			return false, nil
		}
	}
	if cfg.InstantiationTypes != nil {
		if _, ok := cfg.InstantiationTypes[kind]; ok {
			v.extractCall(ctx, node, true)
			// Constructor arguments may contain calls: new Foo(bar()).
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

// --- Extract helpers, one per semantic role ---

// createNode builds a Node and a contains edge to the current parent. It does not
// push onto nodeStack — a caller that needs a frame pushes it.
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

	if v.cfg.GetSignature != nil {
		n.Signature = v.cfg.GetSignature(ctx, sitterNode, v.src)
	}
	if v.cfg.GetVisibility != nil {
		n.Visibility = v.cfg.GetVisibility(ctx, sitterNode, v.src)
	}
	if v.cfg.IsExported != nil {
		n.IsExported = v.cfg.IsExported(ctx, sitterNode, v.src)
	}
	// A name-based rule overrides IsExported.
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
	// An enclosing export wrapper is authoritative and beats both hooks.
	if v.forceExported {
		n.IsExported = true
	}

	v.result.Nodes = append(v.result.Nodes, n)

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

	nodeKind := types.NodeKindFunction
	if v.cfg.MethodTypes != nil {
		if _, ok := v.cfg.MethodTypes[kind]; ok {
			nodeKind = types.NodeKindMethod
		}
	}

	// The hook takes the unresolved node so it can navigate to its own child.
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

	// A frame here is what qualifies nested symbols' names.
	v.nodeStack = append(v.nodeStack, stackEntry{id: n.ID, name: name})
	defer func() {
		v.nodeStack = v.nodeStack[:len(v.nodeStack)-1]
	}()

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
		// A const block has no name of its own.
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

// extractSimpleNode handles property, field, and variable nodes. resolveBody runs
// first so a TS/JS lexical_declaration is unwrapped to its declarator, where the
// name field actually lives. visitFunctionBody then scans the initializer, so a
// call embedded in one ("local x = require('y')") is still captured.
//
// A variable mints a node only at scopeDepth 0 and only when its name is a single
// identifier rather than a destructuring pattern's rendered text ("{ a, b }").
// Either gate failing still runs visitFunctionBody.
func (v *visitor) extractSimpleNode(ctx context.Context, node sitter.Node, kind types.NodeKind) error {
	resolved, err := v.resolveBody(ctx, node)
	if err != nil {
		resolved = node // best-effort: use original on hook error
	}

	name, err := v.nameFromNode(ctx, resolved)
	if err != nil || name == "" {
		name, _ = v.nameFromNode(ctx, node)
	}
	if name == "" {
		return nil // skip unnamed nodes
	}

	mint := true
	if kind == types.NodeKindVariable {
		if v.scopeDepth > 0 || !isSingleIdentifierVariableName(name) {
			mint = false
		}
	}

	if mint {
		if _, err := v.createNode(ctx, node, kind, name); err != nil {
			return err
		}
	}

	// Runs regardless of mint: a suppressed local's initializer can still hold a
	// call, a JSX usage or a field assignment. The walk stops at a nested function
	// literal, which becomes its own node.
	v.visitFunctionBody(ctx, node)
	return nil
}

// isSingleIdentifierVariableName rejects the rendered text of a destructuring
// pattern ("{ a, b }"), which has no single identity to hang a variable node on.
func isSingleIdentifierVariableName(name string) bool {
	return !strings.ContainsAny(name, "{[, \t\n\r")
}

// extractImport emits an import node plus a reference to its path.
func (v *visitor) extractImport(ctx context.Context, node sitter.Node) error {
	name := ""
	path := ""
	if v.cfg.ExtractImport != nil {
		name, path = v.cfg.ExtractImport(ctx, node, v.src)
	}
	if name == "" {
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

// extractCall records a call or instantiation as an UnresolvedReference, never an
// edge — resolution is what makes call edges.
func (v *visitor) extractCall(ctx context.Context, node sitter.Node, isInstantiation bool) {
	// calleeName is the bare invoked segment resolution matches; calleeExpr is the
	// full expression the callback synthesizers match on ("emitter.on").
	calleeName, calleeExpr := v.calleeNameAndExpr(ctx, node)
	if calleeName == "" {
		return
	}
	// A plain "foo()" has no receiver worth keeping, so store NULL.
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

// extractCallArgs returns a call's arguments in positional order. A string
// literal is recorded as bare content; a plain identifier takes an "arg:" prefix
// so a synthesizer can tell the two apart. Compound arguments (member
// expressions, arrows, nested calls) are skipped — they offer no stable name to
// correlate on.
//
// The prefixes must not collide: "arg:" here, "field:" in extractFieldAssignment,
// "jsx:" in extractJSXRef. nil means no capturable arguments (NULL in SQLite).
func (v *visitor) extractCallArgs(ctx context.Context, callNode sitter.Node) []string {
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
			// A JS/TS grammar nests the unquoted content in a string_fragment child.
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
			asb, _ := arg.StartByte(ctx)
			aeb, _ := arg.EndByte(ctx)
			name := nodeText(asb, aeb, v.src)
			if name != "" {
				result = append(result, "arg:"+name)
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// calleeNameAndExpr splits a call's callee into the bare invoked segment and the
// full expression: "obj.method()" gives ("method", "obj.method").
func (v *visitor) calleeNameAndExpr(ctx context.Context, node sitter.Node) (bare, expr string) {
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
	// The name matcher resolves the final segment; the full "a.b.method" text never
	// matches a node and would stay permanently unresolved. extractJSXRef does the
	// same last-segment reduction.
	bare = finalCalleeSegment(ctx, *fnNode, v.src)
	if bare == "" {
		bare = expr // plain identifier callee (e.g. "foo") — bare == full.
	}
	return bare, expr
}

// Callee node kind → the field holding the final invoked segment. Probed against
// the live grammars; kinds are distinct enough across them to need no language key.
var memberAccessFields = map[string]string{
	"member_expression":   "property", // JavaScript, TypeScript, JSX, TSX
	"selector_expression": "field",    // Go
}

// finalCalleeSegment reduces a member-access callee to its last segment
// ("a.b.method" → "method"), returning "" for a plain identifier callee.
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

// extractJSXRef emits a reference for a PascalCase JSX tag; a lowercase tag is a
// host element and is skipped. A member tag (<Foo.Bar/>) uses its last segment.
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

	// A lowercase tag is a host element; PascalCase marks a component.
	if componentName == "" || !isUpperRune(componentName[0]) {
		return
	}

	startLine := v.byteToLine(sb)
	fromID := v.parentID()
	// Arguments[0] "jsx:<Tag>" marks a JSX-origin reference; resolution carries it
	// onto the edge Metadata so synthesis can tell it from a type-annotation ref.
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

// jsxTagNode returns the node holding a JSX tag's name, or nil when the shape is
// unexpected: the first named child, except for jsx_element, where it is the first
// named child of the jsx_opening_element.
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

	return &first, nil
}

// isUpperRune is enough for PascalCase detection: an HTML or SVG tag never starts
// with an ASCII uppercase letter, and a React component always does.
func isUpperRune(b byte) bool {
	return b >= 'A' && b <= 'Z'
}

// extractFieldAssignment emits a reference for an assignment whose left side is a
// member expression and whose right side is callable, reporting whether it did.
// ReferenceName is the RHS identifier, empty for an anonymous arrow or function.
//
// Arguments[0] is a "field:<property>" sentinel. A synthesizer must check both
// gates — ReferenceKind is references AND that prefix — since a call ref such as
// emitter.on("field:x", cb) carries a similar string under EdgeKindCalls.
func (v *visitor) extractFieldAssignment(ctx context.Context, node sitter.Node) bool {
	leftNode, err := childByField(ctx, node, "left")
	if err != nil || leftNode == nil {
		return false
	}
	leftKind, err := leftNode.Kind(ctx)
	if err != nil || leftKind != "member_expression" {
		return false
	}

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

	rightNode, err := childByField(ctx, node, "right")
	if err != nil || rightNode == nil {
		return false
	}
	rightKind, err := rightNode.Kind(ctx)
	if err != nil {
		return false
	}

	var callableName string
	switch rightKind {
	case "identifier":
		rsb, _ := rightNode.StartByte(ctx)
		reb, _ := rightNode.EndByte(ctx)
		callableName = nodeText(rsb, reb, v.src)
	case "arrow_function", "function_expression":
		// Anonymous callable — callableName stays "".
	default:
		// A non-callable RHS carries no signal for the synthesizer.
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
		Arguments:     []string{"field:" + fieldName},
	})
	return true
}

// visitFunctionBody walks a body for call, instantiation, JSX and
// field-assignment nodes, stopping at a nested function, which is extracted as
// its own node.
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

		if v.cfg.CallTypes != nil {
			if _, ok := v.cfg.CallTypes[kind]; ok {
				// Where one node kind is both a definition and a call (Elixir's "call"
				// covers defmodule and def as well as User.new), ResolveKind decides;
				// without the check a definition would be emitted as a calls edge.
				if v.cfg.StructTypes != nil && v.cfg.ResolveKind != nil {
					if _, inStruct := v.cfg.StructTypes[kind]; inStruct {
						resolved := v.cfg.ResolveKind(ctx, child, v.src)
						if resolved != types.NodeKind("") {
							// A definition: visitNode's StructTypes dispatch owns it.
							continue
						}
					}
				}
				v.extractCall(ctx, child, false)
				// A callee or argument can hold further calls (a.b().c(), f(g())), so
				// recurse rather than capture only the outermost call per statement.
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
		if v.cfg.JSXElementTypes != nil {
			if _, ok := v.cfg.JSXElementTypes[kind]; ok {
				v.extractJSXRef(ctx, child, kind)
				// Recurse so nested JSX elements (children of this element) are found.
				v.visitFunctionBody(ctx, child)
				continue
			}
		}
		// Only skip recursion when a ref was actually emitted; on false nothing was
		// emitted, so fall through and let the CallTypes arm still catch a call
		// nested in the RHS, as in `x = factory('evt')`.
		if v.cfg.FieldAssignmentTypes != nil {
			if _, ok := v.cfg.FieldAssignmentTypes[kind]; ok {
				if v.extractFieldAssignment(ctx, child) {
					continue
				}
			}
		}

		v.visitFunctionBody(ctx, child)
	}
}
