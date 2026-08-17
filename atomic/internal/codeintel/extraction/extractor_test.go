package extraction_test

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// goFixture covers every Go node kind the extractor classifies: import, struct,
// interface, method, function, field, call, type alias, named type, iota const.
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

func newTSExtractor(t *testing.T) *extraction.TreeSitterExtractor {
	t.Helper()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	ctx := context.Background()
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return extraction.NewTreeSitterExtractor(pool, extLang, cfg)
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

// The file: node roots every contains edge; malformed, the graph loses its origin.
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

// Top-level functions are the primary call targets; unextracted, no call edge
// resolves and callers/callees queries return nothing.
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
	if !strings.Contains(fn.QualifiedName, "NewUser") {
		t.Errorf("NewUser.QualifiedName = %q, does not contain \"NewUser\"", fn.QualifiedName)
	}
}

// A method stored as NodeKindFunction breaks kind-based routing in resolution
// and search.
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

// Structural queries (fields of X, extends/implements) key off NodeKindStruct.
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

// contains edges are the containment hierarchy (file→function, struct→field);
// without them the explorer cannot walk the tree.
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

// Calls emit UnresolvedReference, never edges: a direct edge bypasses the
// resolution layer's confidence scoring and kind promotion, freezing wrong
// provenance. See docs/spec/code-intel-extraction.md.
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

	callEdges := countEdges(result.Edges, types.EdgeKindCalls)
	if callEdges != 0 {
		t.Errorf("found %d calls edges — calls must be UnresolvedReferences, NOT edges", callEdges)
	}

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

// The name matcher resolves bare symbol names, so a selector call must store
// the final segment ("Sprintf"), not the dotted receiver chain — the latter is
// permanently unresolvable.
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

// Same rule down a TypeScript member chain: every invoked segment stored bare,
// never the whole "db.connect().query(...).execute" subtree text.
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

// Extraction is best-effort: one unparseable file must not panic and take the
// whole index down with it.
func TestExtractor_BestEffortBrokenSource(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	garbage := "}{{{THIS IS NOT GO CODE @#$%^&*()_+<>?:[]\\/.,;'\"!~`\x00\xff"

	var result types.ExtractionResult
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Extract panicked on garbage input: %v", r)
			}
		}()
		result = e.Extract(ctx, "bad/file.go", garbage, types.LanguageGo)
	}()

	// tree-sitter error-recovery may still yield a partial tree, so an empty
	// Errors slice is a valid outcome; only a blank message is a defect.
	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			if err == "" {
				t.Errorf("empty error string in result.Errors")
			}
		}
	}
}

// Differing counts across two extractions of one file mean non-determinism —
// usually stack mismanagement or a missed skipChildren double-extracting.
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

// Imports seed the resolution layer's import resolver; without them no
// cross-file reference resolves.
func TestExtractor_ImportExtracted(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture imports fmt and strings")
	}
}

// Qualified names ("User::FullName") are the name matcher's lookup key; a wrong
// separator or hierarchy fails every cross-reference lookup.
func TestExtractor_QualifiedNameHierarchy(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// Go methods sit outside the struct lexically, so FullName stays bare;
	// fields are the nodes that must carry the "User::" prefix.
	idField := findNode(result.Nodes, types.NodeKindField, "ID")
	if idField != nil && !strings.Contains(idField.QualifiedName, "::") {
		t.Errorf("field ID qualified name = %q, expected to contain \"::\"", idField.QualifiedName)
	}
}

// Node ids are kind:hex32; any deviation breaks every edge referencing the node.
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

// Resolution's kind promotion (calls→instantiates for struct targets,
// extends→implements for interface targets) reads the node kind, so an
// interface misclassified as a struct silently breaks edge promotion.
func TestExtractor_InterfaceExtracted(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	iface := findNode(result.Nodes, types.NodeKindInterface, "Namer")
	if iface == nil {
		t.Fatalf("Namer interface not found as NodeKindInterface; nodes: %v", nodeKindList(result.Nodes))
	}
	if iface.Kind != types.NodeKindInterface {
		t.Errorf("Namer node kind = %q, want %q", iface.Kind, types.NodeKindInterface)
	}
}

// Search assigns kindBonus=6 to type_alias, so a misclassified alias silently
// shifts search ranking and resolution confidence.
func TestExtractor_TypeAliasExtracted(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

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

// A named type (type Status int) is neither struct, interface, nor alias, but
// there is no named_type NodeKind — it lands on NodeKindTypeAlias, the closest.
func TestExtractor_NamedTypeExtracted(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	namedType := findNode(result.Nodes, types.NodeKindTypeAlias, "Status")
	if namedType == nil {
		t.Fatalf("Status named type not found as NodeKindTypeAlias; nodes: %v", nodeKindList(result.Nodes))
	}
}

// IsExported drives resolution's +10 exported-symbol bonus. GoIsExportedByName
// is tested on its own; this only proves it reaches the node field.
func TestExtractor_IsExported(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

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

	// Fields take the same uppercase-first rule, on a different node kind.
	idField := findNode(result.Nodes, types.NodeKindField, "ID")
	if idField != nil && !idField.IsExported {
		t.Errorf("field ID: IsExported = false, want true (starts with uppercase)")
	}
}

// A per-node error inside visitChildren must leave the partial result intact —
// file: node first, already-extracted nodes kept — not a fresh empty struct;
// callers read Nodes[0] for provenance. The error cannot be injected without
// touching extractor internals, so the invariant is pinned on the happy path.
func TestExtractor_BestEffortPartialResultSurvives(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)

	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)

	fileNode := findNode(result.Nodes, types.NodeKindFile, goFixturePath)
	if fileNode == nil {
		t.Fatalf("file: node missing from result — partial result contract violated; nodes: %v", nodeKindList(result.Nodes))
	}

	if len(result.Nodes) > 0 && result.Nodes[0].Kind != types.NodeKindFile {
		t.Errorf("result.Nodes[0].Kind = %q, want NodeKindFile — file: node must be first", result.Nodes[0].Kind)
	}
}

// scopeSuppressionFixture covers each FunctionScopeTypes member (arrow_function,
// function_expression, generator_function), nesting, and destructuring. Every
// initializer holds a call so the walk-continues contract is exercised on the
// suppressed declarations too. See docs/spec/code-intel-local-variable-suppression.md.
const scopeSuppressionFixture = `export const moduleConst = 1;

items.forEach((item) => {
  const inCallback = item + 1;
  helperCall(inCallback);
});

outerCallback(() => {
  innerCallback(() => {
    const twoDeep = 1;
    console.log(twoDeep);
  });
});

callbackWithFunctionExpr(function() {
  const inFunctionExpr = 1;
  functionExprCall(inFunctionExpr);
});

callbackWithGenerator(function*() {
  const inGenerator = 1;
  generatorCall(inGenerator);
});

const { a, b } = destructureModule();

items.forEach((item) => {
  const { c } = destructureCallback(item);
});
`

const scopeSuppressionFixturePath = "src/scope.ts"

// TS/JS function-body locals used to mint nodes: 51% of a real repo's graph was
// orphaned, and cross-file resolution landed calls edges on test-local names.
// Only module-scope, single-identifier declarations may mint a node.
func TestExtractor_FunctionScopeSuppression_TS(t *testing.T) {
	ctx := context.Background()
	e := newTSExtractor(t)
	result := e.Extract(ctx, scopeSuppressionFixturePath, scopeSuppressionFixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	if n := findNode(result.Nodes, types.NodeKindVariable, "moduleConst"); n == nil {
		t.Errorf("moduleConst not found as a variable node; nodes: %s", nodeKindList(result.Nodes))
	}

	if n := findNode(result.Nodes, types.NodeKindVariable, "inCallback"); n != nil {
		t.Errorf("inCallback minted as a variable node (want suppressed — scopeDepth 1); nodes: %s", nodeKindList(result.Nodes))
	}

	if n := findNode(result.Nodes, types.NodeKindVariable, "twoDeep"); n != nil {
		t.Errorf("twoDeep minted as a variable node (want suppressed — scopeDepth 2); nodes: %s", nodeKindList(result.Nodes))
	}

	if n := findNode(result.Nodes, types.NodeKindVariable, "inFunctionExpr"); n != nil {
		t.Errorf("inFunctionExpr minted as a variable node (want suppressed — function_expression scope); nodes: %s", nodeKindList(result.Nodes))
	}

	if n := findNode(result.Nodes, types.NodeKindVariable, "inGenerator"); n != nil {
		t.Errorf("inGenerator minted as a variable node (want suppressed — generator_function scope); nodes: %s", nodeKindList(result.Nodes))
	}

	// Destructuring is dropped at module scope too — the guard is on the name
	// shape, not the depth.
	for _, name := range []string{"a", "b", "c", "{ a, b }", "{ c }"} {
		if n := findNode(result.Nodes, types.NodeKindVariable, name); n != nil {
			t.Errorf("destructuring pattern minted a variable node (name %q); nodes: %s", n.Name, nodeKindList(result.Nodes))
		}
	}

	varCount := 0
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindVariable {
			varCount++
		}
	}
	if varCount != 1 {
		t.Errorf("variable node count = %d, want 1 (moduleConst only); nodes: %s", varCount, nodeKindList(result.Nodes))
	}

	// Suppressing the node must not stop the walk: initializer calls are still
	// harvested from the declarations that minted nothing.
	wantCalls := []string{
		"forEach", "helperCall", "outerCallback", "innerCallback", "log",
		"callbackWithFunctionExpr", "functionExprCall",
		"callbackWithGenerator", "generatorCall",
		"destructureModule", "destructureCallback",
	}
	gotCalls := map[string]bool{}
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindCalls {
			gotCalls[r.ReferenceName] = true
		}
	}
	for _, want := range wantCalls {
		if !gotCalls[want] {
			t.Errorf("expected calls ref %q from a suppressed/kept declaration's initializer; got refs: %v", want, mapKeys(gotCalls))
		}
	}
}

// Go has no VariableTypes config, so scope-suppression gating is structurally
// unreachable for it. These exact counts pin that it stays inert.
func TestExtractor_GoByteIdenticalAfterScopeSuppression(t *testing.T) {
	ctx := context.Background()
	e := newGoExtractor(t)
	result := e.Extract(ctx, goFixturePath, goFixture, types.LanguageGo)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if len(result.Nodes) != 12 {
		t.Errorf("Go node count = %d, want 12 (byte-identical to pre-change)", len(result.Nodes))
	}
	if len(result.Edges) != 11 {
		t.Errorf("Go edge count = %d, want 11 (byte-identical to pre-change)", len(result.Edges))
	}
	if len(result.UnresolvedReferences) != 4 {
		t.Errorf("Go unresolved-ref count = %d, want 4 (byte-identical to pre-change)", len(result.UnresolvedReferences))
	}
}

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
