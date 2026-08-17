package languages_test

// Field-assignment capture, which is how the callback synthesizer finds a
// registration site ("this.onData = handler") and not merely the later call.
//
// The reference is emitted only when an assignment has a member_expression on
// the left and a callable on the right; a primitive right-hand side is skipped.
// ReferenceName is the callable's name, empty for an inline function, and
// Arguments[0] holds a "field:<name>" sentinel — the discriminator that keeps
// these apart from JSX and call references, which share the same kind.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Covers every right-hand-side shape: named callable, inline callable, primitive.
const ee3Fixture = `
class EventSource {
  constructor() {
    this.onData = handleData;
    this.h = () => {};
    this.count = 0;
    this.process = function() {};
  }
}
`

const ee3FixturePath = "src/EventSource.ts"

// isFieldAssignmentRef keys on the "field:" sentinel in Arguments[0].
func isFieldAssignmentRef(r types.UnresolvedReference) bool {
	return r.ReferenceKind == types.EdgeKindReferences &&
		len(r.Arguments) > 0 &&
		strings.HasPrefix(r.Arguments[0], "field:")
}

// fieldAssignmentRefs keeps only the field-assignment references.
func fieldAssignmentRefs(refs []types.UnresolvedReference) []types.UnresolvedReference {
	out := make([]types.UnresolvedReference, 0, len(refs))
	for _, r := range refs {
		if isFieldAssignmentRef(r) {
			out = append(out, r)
		}
	}
	return out
}

// fieldNameFromRef strips the "field:" sentinel prefix.
func fieldNameFromRef(r types.UnresolvedReference) string {
	if len(r.Arguments) == 0 {
		return ""
	}
	return strings.TrimPrefix(r.Arguments[0], "field:")
}

func ee3Extractor(t *testing.T) *extraction.TreeSitterExtractor {
	t.Helper()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("LanguageTypeScript not registered")
	}
	return newExtractor(t, extLang, cfg)
}

// The core contract: the synthesizer reads Arguments[0] for the field assigned
// and ReferenceName for the callable stored in it.
func TestEE3_IdentifierRHS_EmitsRef(t *testing.T) {
	t.Parallel()
	e := ee3Extractor(t)
	result := e.Extract(context.Background(), ee3FixturePath, ee3Fixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	faRefs := fieldAssignmentRefs(result.UnresolvedReferences)
	var found *types.UnresolvedReference
	for i := range faRefs {
		if fieldNameFromRef(faRefs[i]) == "onData" {
			found = &faRefs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no field-assignment ref for field 'onData'; all refs: %v", result.UnresolvedReferences)
	}
	if found.ReferenceName != "handleData" {
		t.Errorf("ReferenceName = %q, want %q", found.ReferenceName, "handleData")
	}
	if found.Arguments[0] != "field:onData" {
		t.Errorf("Arguments[0] = %q, want %q", found.Arguments[0], "field:onData")
	}
}

// An anonymous callable still counts: the synthesizer needs to know the field
// holds a callback even when there is no name to record.
func TestEE3_ArrowFunctionRHS_EmitsRef(t *testing.T) {
	t.Parallel()
	e := ee3Extractor(t)
	result := e.Extract(context.Background(), ee3FixturePath, ee3Fixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	faRefs := fieldAssignmentRefs(result.UnresolvedReferences)
	var found *types.UnresolvedReference
	for i := range faRefs {
		if fieldNameFromRef(faRefs[i]) == "h" {
			found = &faRefs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no field-assignment ref for field 'h'; all field-assignment refs: %v", faRefs)
	}
	if found.ReferenceName != "" {
		t.Errorf("arrow function RHS ReferenceName = %q, want empty string (anonymous callable)", found.ReferenceName)
	}
}

// The older-JS spelling of the same pattern, which must behave like an arrow.
func TestEE3_FunctionExpressionRHS_EmitsRef(t *testing.T) {
	t.Parallel()
	e := ee3Extractor(t)
	result := e.Extract(context.Background(), ee3FixturePath, ee3Fixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	faRefs := fieldAssignmentRefs(result.UnresolvedReferences)
	var found *types.UnresolvedReference
	for i := range faRefs {
		if fieldNameFromRef(faRefs[i]) == "process" {
			found = &faRefs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no field-assignment ref for field 'process'; field-assignment refs: %v", faRefs)
	}
}

// A primitive assignment is data, not a callback; recording it would leave the
// synthesizer treating plain properties as callback-bearing.
func TestEE3_NonCallableRHS_EmitsNothing(t *testing.T) {
	t.Parallel()
	e := ee3Extractor(t)
	result := e.Extract(context.Background(), ee3FixturePath, ee3Fixture, types.LanguageTypeScript)

	faRefs := fieldAssignmentRefs(result.UnresolvedReferences)
	for _, r := range faRefs {
		if fieldNameFromRef(r) == "count" {
			t.Errorf("field 'count' (non-callable RHS=0) should NOT emit a ref, but got: %v", r)
		}
	}
}

// The synthesizer anchors its registration edge at the method doing the
// assignment, so file-level attribution would be unusable.
func TestEE3_FromEnclosingMethod(t *testing.T) {
	t.Parallel()
	e := ee3Extractor(t)
	result := e.Extract(context.Background(), ee3FixturePath, ee3Fixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	faRefs := fieldAssignmentRefs(result.UnresolvedReferences)
	if len(faRefs) == 0 {
		t.Fatal("no field-assignment refs found")
	}

	fileID := "file:" + ee3FixturePath
	for _, r := range faRefs {
		if r.FromNodeID == fileID {
			t.Errorf("ref %q has FromNodeID=file node; must be the enclosing method", r.Arguments)
		}
		if r.FromNodeID == "" {
			t.Errorf("ref has empty FromNodeID")
		}
	}
}

// The callback and JSX synthesizers both read references-kind refs, so without
// the sentinel each would fire on the other's.
func TestEE3_DistinguishableFromJSXRefs(t *testing.T) {
	t.Parallel()
	// The fixture has no JSX, so every references-kind ref in it should carry
	// the sentinel.
	e := ee3Extractor(t)
	result := e.Extract(context.Background(), ee3FixturePath, ee3Fixture, types.LanguageTypeScript)

	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind != types.EdgeKindReferences {
			continue
		}
		if !isFieldAssignmentRef(r) {
			t.Errorf("references ref %q lacks field: sentinel — would be confused with EE1 JSX ref; Arguments=%v",
				r.ReferenceName, r.Arguments)
		}
	}
}

// Re-extracting a fixture must yield the same counts: field-assignment adds
// reference rows, and that must never read as node growth.
func TestEE3_NodeCountStable(t *testing.T) {
	t.Parallel()
	e := ee3Extractor(t)
	ctx := context.Background()
	r1 := e.Extract(ctx, ee3FixturePath, ee3Fixture, types.LanguageTypeScript)
	r2 := e.Extract(ctx, ee3FixturePath, ee3Fixture, types.LanguageTypeScript)

	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: run1=%d run2=%d", len(r1.Nodes), len(r2.Nodes))
	}
	if len(r1.UnresolvedReferences) != len(r2.UnresolvedReferences) {
		t.Errorf("ref count unstable: run1=%d run2=%d",
			len(r1.UnresolvedReferences), len(r2.UnresolvedReferences))
	}
}

// Field assignment added a branch to the body walk, next to the call arm that
// argument capture depends on.
func TestEE3_EE2CallRefsUnaffected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	src := "function handler() {\n  emitter.on('login', cb);\n}"
	result := e.Extract(ctx, "src/ee3reg.js", src, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	var found *types.UnresolvedReference
	for i := range result.UnresolvedReferences {
		r := &result.UnresolvedReferences[i]
		if r.ReferenceKind == types.EdgeKindCalls && strings.Contains(r.ReferenceName, "on") {
			found = r
			break
		}
	}
	if found == nil {
		t.Fatalf("EE2 call ref not found; refs: %v", result.UnresolvedReferences)
	}
	if len(found.Arguments) == 0 || found.Arguments[0] != "login" {
		t.Errorf("EE2 call ref Arguments degraded: got %v, want [login]", found.Arguments)
	}
}

// Regression guard: the field-assignment branch once stopped recursion for every
// assignment it saw, even the ones it emitted nothing for, which silently
// dropped argument capture for any call on the right-hand side.
func TestEE3_NestedCallInAssignmentRHS_EE2ArgsStillCaptured(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	// A plain identifier on the left, so nothing is emitted for the assignment
	// itself and the nested call must still surface.
	src := "function handler() {\n  x = factory('evt');\n}"
	result := e.Extract(ctx, "src/ee3nested.js", src, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	var found *types.UnresolvedReference
	for i := range result.UnresolvedReferences {
		r := &result.UnresolvedReferences[i]
		if r.ReferenceKind == types.EdgeKindCalls && r.ReferenceName == "factory" {
			found = r
			break
		}
	}
	if found == nil {
		t.Fatalf("call ref for 'factory' not found; refs: %v", result.UnresolvedReferences)
	}
	if len(found.Arguments) == 0 || found.Arguments[0] != "evt" {
		t.Errorf("nested call's EE2 Arguments degraded: got %v, want [evt]", found.Arguments)
	}
}

// JSX refs share the reference kind that field assignment now also emits.
func TestEE3_EE1JSXRefsUnaffected(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTSX)
	if !ok {
		t.Fatal("LanguageTSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), "src/app.tsx", tsxFixture, types.LanguageTSX)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	refs := refsOfKind(result.UnresolvedReferences, types.EdgeKindReferences)
	refNames := refNameSet(refs)
	want := []string{"Panel", "ChildWidget", "Modal"}
	for _, name := range want {
		if !refNames[name] {
			t.Errorf("EE1 JSX ref %q missing after EE3 addition; refs: %v", name, refNameList(refs))
		}
	}
}
