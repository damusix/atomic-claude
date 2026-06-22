package extraction_test

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

// goFixture is a representative Go source file used across extractor tests.
// It exercises the main node kinds the Go extractor must handle:
//   - package + import_declaration
//   - struct type (type_declaration > type_spec > struct_type)
//   - interface type (type_declaration > type_spec > interface_type)
//   - top-level function (function_declaration)
//   - method (method_declaration)
//   - field_declaration inside struct
//   - call_expression inside function body
//   - type alias (type_declaration > type_alias)
//   - const_declaration (iota / enum-like)
const goFixture = `package service

import (
	"fmt"
	"strings"
)

// Namer is something that has a name.
type Namer interface {
	Name() string
}

// User holds user data.
type User struct {
	ID   int
	Name string
}

// FullName returns the user's display name.
func (u *User) FullName() string {
	return fmt.Sprintf("User#%d", u.ID)
}

// NewUser constructs a new User with the given id and name.
func NewUser(id int, name string) *User {
	trimmed := strings.TrimSpace(name)
	return &User{ID: id, Name: trimmed}
}

// Status represents the lifecycle state of a record.
type Status int

const (
	StatusPending Status = iota
	StatusActive
	StatusInactive
)

// Label is a display string alias.
type Label = string
`

const goFixturePath = "service/user.go"

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newGoExtractor(t *testing.T) *extraction.TreeSitterExtractor {
	t.Helper()
	ctx := context.Background()
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return extraction.NewTreeSitterExtractor(pool, extraction.LangGo, languages.GoExtractor())
}

func findNode(nodes []types.Node, kind types.NodeKind, namePart string) *types.Node {
	for i := range nodes {
		if nodes[i].Kind == kind && strings.Contains(nodes[i].Name, namePart) {
			return &nodes[i]
		}
	}
	return nil
}

func countEdges(edges []types.Edge, kind types.EdgeKind) int {
	n := 0
	for _, e := range edges {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func countUnresolved(refs []types.UnresolvedReference, kind types.EdgeKind) int {
	n := 0
	for _, r := range refs {
		if r.ReferenceKind == kind {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// TestExtractor_FileNode
// WHY: The file: node is the root of every extraction. If it is missing or
// wrongly formed, every contains edge loses its origin and the graph is broken.
// ---------------------------------------------------------------------------

func TestExtractor_FileNode(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fileNode := findNode(result.Nodes, types.NodeKindFile, goFixturePath)
	if fileNode == nil {
		t.Fatalf("file: node not found; nodes: %v", nodeKindList(result.Nodes))
	}
	wantID := "file:" + goFixturePath
	if fileNode.ID != wantID {
		t.Errorf("file node ID = %q, want %q", fileNode.ID, wantID)
	}
	if fileNode.Language != types.LanguageGo {
		t.Errorf("file node Language = %q, want %q", fileNode.Language, types.LanguageGo)
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_FunctionExtracted
// WHY: Top-level functions are the primary call targets in Go. If they are not
// extracted, call edges can't be resolved and the graph is useless for
// callers/callees queries.
// ---------------------------------------------------------------------------

func TestExtractor_FunctionExtracted(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "NewUser")
	if fn == nil {
		t.Fatalf("NewUser function not found; nodes: %v", nodeKindList(result.Nodes))
	}
	if fn.FilePath != goFixturePath {
		t.Errorf("NewUser.FilePath = %q, want %q", fn.FilePath, goFixturePath)
	}
	if fn.StartLine == 0 {
		t.Errorf("NewUser.StartLine = 0, want > 0")
	}
	// QualifiedName must contain the function name.
	if !strings.Contains(fn.QualifiedName, "NewUser") {
		t.Errorf("NewUser.QualifiedName = %q, does not contain \"NewUser\"", fn.QualifiedName)
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_MethodExtracted
// WHY: Methods are the primary call targets for object-oriented code. If they
// are not extracted with NodeKindMethod (not function), the kind-based routing
// in resolution and search breaks.
// ---------------------------------------------------------------------------

func TestExtractor_MethodExtracted(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	method := findNode(result.Nodes, types.NodeKindMethod, "FullName")
	if method == nil {
		t.Fatalf("FullName method not found; nodes: %v", nodeKindList(result.Nodes))
	}
	if method.Kind != types.NodeKindMethod {
		t.Errorf("FullName node kind = %q, want %q", method.Kind, types.NodeKindMethod)
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_StructExtracted
// WHY: Structs are the container types in Go. If they are not extracted as
// NodeKindStruct, structural queries (find all fields of X, extends/implements
// edges) silently break.
// ---------------------------------------------------------------------------

func TestExtractor_StructExtracted(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	st := findNode(result.Nodes, types.NodeKindStruct, "User")
	if st == nil {
		t.Fatalf("User struct not found; nodes: %v", nodeKindList(result.Nodes))
	}
	if st.FilePath != goFixturePath {
		t.Errorf("User.FilePath = %q, want %q", st.FilePath, goFixturePath)
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_ContainsEdges
// WHY: contains edges are the backbone of the containment hierarchy
// (file→function, struct→field). Without them the explorer cannot walk the
// tree and callers/callees queries lose context.
// ---------------------------------------------------------------------------

func TestExtractor_ContainsEdges(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	containsCount := countEdges(result.Edges, types.EdgeKindContains)
	if containsCount == 0 {
		t.Fatalf("no contains edges; want at least one (file→symbol)")
	}

	// Every non-file node must have a contains edge pointing at it.
	// Build target set from edges.
	targets := map[string]bool{}
	for _, e := range result.Edges {
		if e.Kind == types.EdgeKindContains {
			targets[e.Target] = true
		}
	}
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindFile {
			continue
		}
		if !targets[n.ID] {
			t.Errorf("node %s (%s) has no contains edge pointing at it", n.ID, n.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_CallEmitsUnresolvedReference
// WHY: The extractor contract (appendix E §9) mandates that calls emit
// UnresolvedReference, NOT edges. Emitting edges directly would bypass the
// resolution layer, which is responsible for confidence scoring and kind
// promotion. A single premature edge here means wrong provenance forever.
// ---------------------------------------------------------------------------

func TestExtractor_CallEmitsUnresolvedReference(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls-kind UnresolvedReferences; fixture has fmt.Sprintf and strings.TrimSpace calls")
	}

	// Verify that NO call edges were directly emitted.
	callEdges := countEdges(result.Edges, types.EdgeKindCalls)
	if callEdges != 0 {
		t.Errorf("found %d calls edges — calls must be UnresolvedReferences, NOT edges", callEdges)
	}

	// Spot-check: fixture calls fmt.Sprintf and strings.TrimSpace.
	var refNames []string
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindCalls {
			refNames = append(refNames, r.ReferenceName)
		}
	}
	foundFmt := false
	for _, n := range refNames {
		if strings.Contains(n, "fmt") || strings.Contains(n, "Sprintf") {
			foundFmt = true
			break
		}
	}
	if !foundFmt {
		t.Errorf("expected fmt.Sprintf call reference; got refs: %v", refNames)
	}
}

// TestExtractor_CalleeNameIsBareFinalSegment_Go proves a member/selector call
// (fmt.Sprintf, strings.TrimSpace) emits the BARE final invoked segment
// (Sprintf, TrimSpace) as the call ref name — not the dotted "fmt.Sprintf"
// receiver-chain text. The resolution name matcher resolves bare symbol names;
// storing the whole callee subtree text makes the ref permanently unresolvable.
func TestExtractor_CalleeNameIsBareFinalSegment_Go(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)
	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	names := map[string]bool{}
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind != types.EdgeKindCalls {
			continue
		}
		names[r.ReferenceName] = true
		if strings.Contains(r.ReferenceName, ".") {
			t.Errorf("call ref name %q contains '.'; expected the bare final callee segment", r.ReferenceName)
		}
	}
	if !names["Sprintf"] {
		t.Errorf("expected bare callee 'Sprintf' (from fmt.Sprintf); got %v", mapKeys(names))
	}
	if !names["TrimSpace"] {
		t.Errorf("expected bare callee 'TrimSpace' (from strings.TrimSpace); got %v", mapKeys(names))
	}
}

// TestExtractor_CalleeNameIsBareFinalSegment_TS proves the same for a TypeScript
// member-call chain: each invoked segment is stored bare, never as the whole
// "db.connect().query(...).execute" subtree text.
func TestExtractor_CalleeNameIsBareFinalSegment_TS(t *testing.T) {
	ctx := context.Background()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("no TypeScript extractor in registry")
	}
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	e := extraction.NewTreeSitterExtractor(pool, extLang, cfg)

	const src = `function run(db: any) {
  return db.connect().query("SELECT 1").execute();
}`
	result := e.Extract(ctx, "src/run.ts", src, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	names := map[string]bool{}
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind != types.EdgeKindCalls {
			continue
		}
		names[r.ReferenceName] = true
		if strings.Contains(r.ReferenceName, ".") || strings.Contains(r.ReferenceName, "(") {
			t.Errorf("call ref name %q is not a bare segment; expected the final invoked method name", r.ReferenceName)
		}
	}
	for _, want := range []string{"connect", "query", "execute"} {
		if !names[want] {
			t.Errorf("expected bare callee %q in the member chain; got %v", want, mapKeys(names))
		}
	}
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// TestExtractor_BestEffortBrokenSource
// WHY: The brief mandates best-effort extraction — a broken file must record
// the error and return a partial (or empty) result, never panic. If the
// extractor panics on garbage input, one bad file brings down the whole index.
// ---------------------------------------------------------------------------

func TestExtractor_BestEffortBrokenSource(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	// Feed truly garbage input.
	garbage := "}{{{THIS IS NOT GO CODE @#$%^&*()_+<>?:[]\\/.,;'\"!~`\x00\xff"

	// Must not panic.
	var result types.ExtractionResult
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Extract panicked on garbage input: %v", r)
			}
		}()
		result = e.Extract(ctx, "bad/file.go", garbage, types.LanguageGo)
	}()

	// The file: node may or may not be present (partial result is OK).
	// What matters: the extractor didn't panic and Errors may be populated.
	// If Errors is populated, it must contain some message.
	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			if err == "" {
				t.Errorf("empty error string in result.Errors")
			}
		}
	}
	// Note: tree-sitter is resilient and may produce a partial parse tree even
	// for garbage input (it uses error-recovery). So Errors may be empty and
	// result.Nodes may have the file: node. Either is acceptable.
}

// ---------------------------------------------------------------------------
// TestExtractor_NodeCountStable
// WHY: "Node-count stable across re-extract" is an explicit success criterion.
// If extracting the same file twice produces different node counts, there is
// non-determinism in the extractor — likely a bug in stack management or a
// missed skipChildren that causes double-extraction.
// ---------------------------------------------------------------------------

func TestExtractor_NodeCountStable(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	r1 := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)
	r2 := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
	if len(r1.Edges) != len(r2.Edges) {
		t.Errorf("edge count unstable: first=%d second=%d", len(r1.Edges), len(r2.Edges))
	}
	if len(r1.UnresolvedReferences) != len(r2.UnresolvedReferences) {
		t.Errorf("unresolved-ref count unstable: first=%d second=%d",
			len(r1.UnresolvedReferences), len(r2.UnresolvedReferences))
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_ImportExtracted
// WHY: Imports are the starting point for the resolution layer's import
// resolver. If they are not extracted, cross-file references can't be resolved.
// ---------------------------------------------------------------------------

func TestExtractor_ImportExtracted(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// Fixture imports "fmt" and "strings".
	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture imports fmt and strings")
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_QualifiedNameHierarchy
// WHY: Qualified names ("User::FullName") are the lookup key for the resolution
// layer's name matcher (appendix F §matchReference qualifiedName). If the ::
// separator is missing or the hierarchy is wrong, cross-reference lookups fail.
// ---------------------------------------------------------------------------

func TestExtractor_QualifiedNameHierarchy(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// The FullName method should be qualified as something containing "::"
	// when the extractor is inside the struct scope.
	// (Go methods are not lexically inside the struct, so FullName may just be
	// "FullName" — but the struct's fields should be "User::ID", "User::Name".)
	idField := findNode(result.Nodes, types.NodeKindField, "ID")
	if idField != nil && !strings.Contains(idField.QualifiedName, "::") {
		t.Errorf("field ID qualified name = %q, expected to contain \"::\"", idField.QualifiedName)
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_NodeIDFormat
// WHY: Node IDs follow the appendix-B formula (kind:hex32). Any deviation
// breaks every edge in the graph that references this node.
// ---------------------------------------------------------------------------

func TestExtractor_NodeIDFormat(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindFile {
			// file: nodes use "file:"+path, not the hash formula.
			if !strings.HasPrefix(n.ID, "file:") {
				t.Errorf("file node ID %q does not start with \"file:\"", n.ID)
			}
			continue
		}
		// Non-file nodes must be "kind:hex32".
		prefix := string(n.Kind) + ":"
		if !strings.HasPrefix(n.ID, prefix) {
			t.Errorf("node %q (kind=%s) ID %q does not start with %q", n.Name, n.Kind, n.ID, prefix)
		}
		rest := strings.TrimPrefix(n.ID, prefix)
		if len(rest) != 32 {
			t.Errorf("node %q ID hex part len=%d, want 32 (ID=%q)", n.Name, len(rest), n.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_InterfaceExtracted
// WHY: Go interfaces (type Namer interface{}) must be stored as NodeKindInterface,
// not NodeKindStruct. Resolution's kind-promotion (calls→instantiates when target
// is class/struct, extends→implements when target is interface) depends on the
// correct kind. A misclassified interface silently breaks edge promotion at CP13.
// ---------------------------------------------------------------------------

func TestExtractor_InterfaceExtracted(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// The fixture defines: type Namer interface { Name() string }
	iface := findNode(result.Nodes, types.NodeKindInterface, "Namer")
	if iface == nil {
		t.Fatalf("Namer interface not found as NodeKindInterface; nodes: %v", nodeKindList(result.Nodes))
	}
	if iface.Kind != types.NodeKindInterface {
		t.Errorf("Namer node kind = %q, want %q", iface.Kind, types.NodeKindInterface)
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_TypeAliasExtracted
// WHY: Go type aliases (type Label = string) must be stored as NodeKindTypeAlias.
// The search layer assigns kindBonus=6 to type_alias — wrong classification
// silently changes search ranking and resolution confidence scores.
// ---------------------------------------------------------------------------

func TestExtractor_TypeAliasExtracted(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	// goFixture already contains: type Label = string
	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	alias := findNode(result.Nodes, types.NodeKindTypeAlias, "Label")
	if alias == nil {
		t.Fatalf("Label type alias not found as NodeKindTypeAlias; nodes: %v", nodeKindList(result.Nodes))
	}
	if alias.Kind != types.NodeKindTypeAlias {
		t.Errorf("Label node kind = %q, want %q", alias.Kind, types.NodeKindTypeAlias)
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_NamedTypeExtracted
// WHY: Go named types (type Status int) that are neither struct nor interface
// nor alias should be stored as NodeKindTypeAlias (closest kind per appendix C
// — no "named_type" kind exists). Consistent classification is required so
// search rankings and resolution scores behave predictably.
// ---------------------------------------------------------------------------

func TestExtractor_NamedTypeExtracted(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	// goFixture contains: type Status int
	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// Status is a named type (type Status int) — stored as NodeKindTypeAlias
	// because there is no dedicated "named_type" NodeKind in appendix C.
	namedType := findNode(result.Nodes, types.NodeKindTypeAlias, "Status")
	if namedType == nil {
		t.Fatalf("Status named type not found as NodeKindTypeAlias; nodes: %v", nodeKindList(result.Nodes))
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_IsExported
// WHY: IsExported drives the resolution +10 scoring bonus for exported symbols
// (appendix F). If always false, every exported Go symbol loses the bonus and
// cross-file resolution confidence degrades. GoIsExportedByName is the real
// test — this test verifies it is wired through to the node's IsExported field.
// ---------------------------------------------------------------------------

func TestExtractor_IsExported(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// Exported: NewUser (function), User (struct), FullName (method), Namer (interface)
	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindFunction, "NewUser", true},
		{types.NodeKindStruct, "User", true},
		{types.NodeKindMethod, "FullName", true},
		{types.NodeKindInterface, "Namer", true},
		{types.NodeKindTypeAlias, "Label", true},
		{types.NodeKindTypeAlias, "Status", true},
	} {
		node := findNode(result.Nodes, tc.kind, tc.name)
		if node == nil {
			t.Errorf("node %s/%s not found; nodes: %v", tc.kind, tc.name, nodeKindList(result.Nodes))
			continue
		}
		if node.IsExported != tc.want {
			t.Errorf("%s %s: IsExported = %v, want %v", tc.kind, tc.name, node.IsExported, tc.want)
		}
	}

	// Fields are lower-case by convention in the fixture: ID, Name are capitalized
	// but they are field_declaration nodes — test one that must be exported.
	idField := findNode(result.Nodes, types.NodeKindField, "ID")
	if idField != nil && !idField.IsExported {
		t.Errorf("field ID: IsExported = false, want true (starts with uppercase)")
	}
}

// ---------------------------------------------------------------------------
// TestExtractor_BestEffortPartialResultSurvives
// WHY: When visitChildren encounters a top-level error on a subsequent node,
// the extractor must return the partial result (file node + already-extracted
// nodes) with the error appended, not a fresh empty struct. The file: node
// surviving is the minimum signal that partial extraction is working — callers
// rely on it for provenance tracking.
// ---------------------------------------------------------------------------

func TestExtractor_BestEffortPartialResultSurvives(t *testing.T) {
	// This test uses a real extraction of valid Go source to verify that the
	// file: node is always present in the result, regardless of any per-node
	// errors encountered during visitChildren. Since we cannot inject a
	// top-level error without modifying the extractor internals, we verify the
	// invariant on the happy path: result.Nodes[0] is always the file: node.
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	// File: node must survive even when visitChildren appends per-node errors.
	fileNode := findNode(result.Nodes, types.NodeKindFile, goFixturePath)
	if fileNode == nil {
		t.Fatalf("file: node missing from result — partial result contract violated; nodes: %v", nodeKindList(result.Nodes))
	}

	// Verify it is always the first node (callers depend on this for provenance).
	if len(result.Nodes) > 0 && result.Nodes[0].Kind != types.NodeKindFile {
		t.Errorf("result.Nodes[0].Kind = %q, want NodeKindFile — file: node must be first", result.Nodes[0].Kind)
	}
}

// ---------------------------------------------------------------------------
// helper — format node kinds for test output
// ---------------------------------------------------------------------------

func nodeKindList(nodes []types.Node) string {
	sb := strings.Builder{}
	for _, n := range nodes {
		sb.WriteString(string(n.Kind))
		sb.WriteByte(':')
		sb.WriteString(n.Name)
		sb.WriteByte(' ')
	}
	return sb.String()
}
