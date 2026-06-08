package languages_test

// EE3 extractor tests — field-assignment capture.
//
// EE3 convention (see also extractor.go FieldAssignmentTypes comment):
//
//	A field-assignment UnresolvedReference is emitted when an assignment_expression
//	inside a function/method body has:
//	  - left = member_expression (this.x, obj.x) — confirms it is a property assignment
//	  - right = a callable node kind (identifier, arrow_function, function_expression)
//	            — non-callable right-hand sides (number, string, …) are silently skipped
//
//	The emitted reference carries:
//	  ReferenceKind = EdgeKindReferences
//	  ReferenceName = the RHS identifier text (e.g. "handleData"); for inline
//	                  arrow/function RHS the callable is anonymous → ReferenceName = ""
//	  Arguments[0]  = "field:<fieldName>" sentinel (e.g. "field:onData")
//	                  — this single-element slice is the discriminator the callback
//	                    synthesizer uses to distinguish field-assignment refs from
//	                    plain JSX refs and ordinary call refs.
//	  FromNodeID    = enclosing method/function node
//
// These tests prove:
//  1. `this.onData = handleData` inside a method emits a field-assignment ref,
//     ReferenceName="handleData", Arguments=["field:onData"], from the method node.
//  2. `this.h = () => {}` emits a ref with ReferenceName="" (anonymous callable),
//     Arguments=["field:h"], from the method node.
//  3. `this.count = 0` (non-callable RHS) emits NOTHING.
//  4. No regression: EE1 JSX refs and EE2 call-arg refs are unaffected.
//  5. Node count is stable across two extractions.
//
// WHY: The callback synthesizer (CP16 batch 4) needs to find the *registration* site
// (`this.onData = handler`) not just the invocation (`this.onData()`). Without EE3
// the synthesizer has no signal to link the assignment to the later call.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ee3Fixture is a TypeScript class with field-assignment patterns covering all cases.
//
// Verified AST (from tmp/ee3_probe): assignment_expression has
//   - left = member_expression (first named child = "this" or identifier, last named
//     child = property_identifier with the field name)
//   - right = identifier (handleData) | arrow_function (() => {}) | number (0)
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

// isFieldAssignmentRef returns true when the ref is an EE3 field-assignment ref.
// A field-assignment ref is a references-kind ref whose Arguments[0] starts with "field:".
func isFieldAssignmentRef(r types.UnresolvedReference) bool {
	return r.ReferenceKind == types.EdgeKindReferences &&
		len(r.Arguments) > 0 &&
		strings.HasPrefix(r.Arguments[0], "field:")
}

// fieldAssignmentRefs filters the full ref list to EE3 field-assignment refs only.
func fieldAssignmentRefs(refs []types.UnresolvedReference) []types.UnresolvedReference {
	out := make([]types.UnresolvedReference, 0, len(refs))
	for _, r := range refs {
		if isFieldAssignmentRef(r) {
			out = append(out, r)
		}
	}
	return out
}

// fieldNameFromRef extracts the field name from Arguments[0] = "field:<name>".
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

// ---------------------------------------------------------------------------
// EE3 core: callable RHS → emit field-assignment ref
// ---------------------------------------------------------------------------

// TestEE3_IdentifierRHS_EmitsRef proves that `this.onData = handleData` emits
// a field-assignment ref with ReferenceName="handleData" and Arguments=["field:onData"].
// WHY: The callback synthesizer reads Arguments[0] to locate which field was
// assigned and ReferenceName to know which callable was stored.
func TestEE3_IdentifierRHS_EmitsRef(t *testing.T) {
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

// TestEE3_ArrowFunctionRHS_EmitsRef proves that `this.h = () => {}` emits
// a field-assignment ref with ReferenceName="" (anonymous) and Arguments=["field:h"].
// WHY: An inline arrow function is still a callable — the callback synthesizer
// must know the field `h` was assigned a callback even if the callable has no name.
func TestEE3_ArrowFunctionRHS_EmitsRef(t *testing.T) {
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

// TestEE3_FunctionExpressionRHS_EmitsRef proves that `this.process = function() {}`
// emits a field-assignment ref (function_expression is callable).
// WHY: The pattern `this.handler = function() { ... }` is common in older JS — EE3
// must capture it the same as arrow functions.
func TestEE3_FunctionExpressionRHS_EmitsRef(t *testing.T) {
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

// TestEE3_NonCallableRHS_EmitsNothing proves `this.count = 0` (non-callable RHS)
// does NOT emit a field-assignment ref.
// WHY: Primitive assignments are data, not callbacks — recording them would add
// noise without value and could mislead the synthesizer into treating numeric
// properties as callback-bearing fields.
func TestEE3_NonCallableRHS_EmitsNothing(t *testing.T) {
	e := ee3Extractor(t)
	result := e.Extract(context.Background(), ee3FixturePath, ee3Fixture, types.LanguageTypeScript)

	faRefs := fieldAssignmentRefs(result.UnresolvedReferences)
	for _, r := range faRefs {
		if fieldNameFromRef(r) == "count" {
			t.Errorf("field 'count' (non-callable RHS=0) should NOT emit a ref, but got: %v", r)
		}
	}
}

// TestEE3_FromEnclosingMethod proves the field-assignment ref's FromNodeID
// is the enclosing method node, not the file node.
// WHY: The callback synthesizer anchors the registration edge at the method that
// does the assignment. File-level attribution would be unusable.
func TestEE3_FromEnclosingMethod(t *testing.T) {
	e := ee3Extractor(t)
	result := e.Extract(context.Background(), ee3FixturePath, ee3Fixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	faRefs := fieldAssignmentRefs(result.UnresolvedReferences)
	if len(faRefs) == 0 {
		t.Fatal("no field-assignment refs found")
	}

	// All field-assignment refs must come from a non-file node.
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

// ---------------------------------------------------------------------------
// Discriminability: EE3 refs must not be confused with EE1/EE2 refs
// ---------------------------------------------------------------------------

// TestEE3_DistinguishableFromJSXRefs proves EE3 field-assignment refs are
// distinguishable from EE1 JSX refs by the Arguments[0] = "field:<name>" sentinel.
// WHY: The callback synthesizer and JSX synthesizer both read EdgeKindReferences
// refs. Without a discriminator, the wrong synthesizer fires on the wrong ref.
func TestEE3_DistinguishableFromJSXRefs(t *testing.T) {
	// EE3 fixture has no JSX. JSX refs would have no "field:" prefix in Arguments.
	// This test simply confirms the sentinel is present on all EE3 refs.
	e := ee3Extractor(t)
	result := e.Extract(context.Background(), ee3FixturePath, ee3Fixture, types.LanguageTypeScript)

	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind != types.EdgeKindReferences {
			continue
		}
		// Every references ref in this fixture must be a field-assignment ref
		// (no JSX in ee3Fixture) — verify all have the sentinel.
		if !isFieldAssignmentRef(r) {
			t.Errorf("references ref %q lacks field: sentinel — would be confused with EE1 JSX ref; Arguments=%v",
				r.ReferenceName, r.Arguments)
		}
	}
}

// ---------------------------------------------------------------------------
// Regression: existing EE1/EE2 refs unaffected, node count stable
// ---------------------------------------------------------------------------

// TestEE3_NodeCountStable proves extraction is deterministic after adding EE3.
// WHY: node-count stability is a core invariant (CP6/CP10) — field-assignment
// UnresolvedReference rows must not cause node count to vary between runs.
func TestEE3_NodeCountStable(t *testing.T) {
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

// TestEE3_EE2CallRefsUnaffected proves EE2 call-argument capture still works
// after adding the EE3 code path.
// WHY: EE3 adds a new branch in visitFunctionBody; a bug there could break the
// existing CallTypes arm that EE2 depends on.
func TestEE3_EE2CallRefsUnaffected(t *testing.T) {
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

// TestEE3_NestedCallInAssignmentRHS_EE2ArgsStillCaptured proves that a
// call_expression nested inside an assignment RHS still has its EE2 string
// arguments captured, even when the LHS is NOT a member_expression.
//
// Scenario: `x = factory('evt')` — plain identifier LHS, so extractFieldAssignment
// returns false/emits nothing. The EE3 branch must NOT skip recursion in that case;
// it must fall through so the nested call_expression (`factory('evt')`) is visited
// by the CallTypes arm and its string arg "evt" is captured.
//
// WHY: before the fix, the EE3 branch did `continue` unconditionally after calling
// extractFieldAssignment — even when extractFieldAssignment emitted nothing. That
// silently dropped EE2 argument capture for any call inside an assignment RHS.
func TestEE3_NestedCallInAssignmentRHS_EE2ArgsStillCaptured(t *testing.T) {
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	// Plain LHS (not member_expression) — extractFieldAssignment will emit nothing.
	// The nested call `factory('evt')` must still surface via the CallTypes path.
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

// TestEE3_EE1JSXRefsUnaffected proves EE1 JSX refs still work after adding EE3.
func TestEE3_EE1JSXRefsUnaffected(t *testing.T) {
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
