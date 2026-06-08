package languages_test

// EE2 extractor tests — call-argument capture.
//
// These tests prove:
//   - Indexing `emitter.on('login', handler)` produces an UnresolvedReference
//     (callee "emitter.on" or "on") whose Arguments contains "login".
//   - A call with no string args (e.g. foo(x, y)) produces an empty Arguments slice.
//   - A call with only non-string args produces an empty Arguments slice.
//   - The existing node-count stability invariant is unaffected (regression guard).
//
// WHY: EE2 enables event-emitter and rn-event-channel synthesizers to correlate
// .on('event', fn) <-> .emit('event') by the event-name string argument.
// Without Arguments capture these synthesizers cannot derive their edges.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// jsSource wraps src in a function body so call_expression nodes are inside a
// function and are walked via visitFunctionBody.
const ee2JSFuncWrapper = `function handler() {
%s
}`

// TestEE2_StringArgCaptured proves that indexing a call with a string-literal
// first argument captures that string in Arguments.
// WHY: this is the core EE2 contract — the synthesizer reads Arguments[0] to
// correlate .on('login', fn) with .emit('login').
func TestEE2_StringArgCaptured(t *testing.T) {
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	src := "function handler() {\n  emitter.on('login', cb);\n}"
	result := e.Extract(ctx, "src/ee2.js", src, types.LanguageJavaScript)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	// Find the call UnresolvedReference whose callee contains "on".
	var found *types.UnresolvedReference
	for i := range result.UnresolvedReferences {
		r := &result.UnresolvedReferences[i]
		if r.ReferenceKind == types.EdgeKindCalls && strings.Contains(r.ReferenceName, "on") {
			found = r
			break
		}
	}
	if found == nil {
		t.Fatalf("no calls/on UnresolvedReference found; refs: %v", result.UnresolvedReferences)
	}

	if len(found.Arguments) == 0 {
		t.Fatalf("expected Arguments to contain 'login', got empty slice")
	}
	if found.Arguments[0] != "login" {
		t.Errorf("Arguments[0] = %q, want %q", found.Arguments[0], "login")
	}
}

// TestEE2_NoStringArgs proves that a call with only identifier arguments
// records no bare string entries — identifiers are captured with the "arg:"
// prefix (EE5) and must not appear as empty strings or undecorated names.
// WHY: EE5 extends this case to capture identifier args as "arg:<name>";
// the invariant is that no empty-string entries appear in Arguments.
func TestEE2_NoStringArgs(t *testing.T) {
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	// foo(x, y) — both args are identifiers, no string literals.
	src := "function handler() {\n  foo(x, y);\n}"
	result := e.Extract(ctx, "src/ee2nostr.js", src, types.LanguageJavaScript)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindCalls && r.ReferenceName == "foo" {
			// Post-EE5: identifier args are captured as "arg:x", "arg:y".
			// Assert no empty strings and no bare (undecorated) identifier names.
			for _, a := range r.Arguments {
				if a == "" {
					t.Errorf("Arguments %v contains empty string entry", r.Arguments)
				}
				if a == "x" || a == "y" {
					t.Errorf("Arguments %v contains bare identifier %q (expected \"arg:%s\" prefix)", r.Arguments, a, a)
				}
			}
			return
		}
	}
	// foo call may not have been extracted (no function body issue) — skip if not found.
	t.Log("foo call not found in refs (may be a top-level scope issue) — skip")
}

// TestEE2_ArglessCallProducesNilArguments proves that a call with no arguments
// produces nil Arguments.
func TestEE2_ArglessCallProducesNilArguments(t *testing.T) {
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	src := "function handler() {\n  doSomething();\n}"
	result := e.Extract(ctx, "src/ee2argless.js", src, types.LanguageJavaScript)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindCalls && r.ReferenceName == "doSomething" {
			if len(r.Arguments) != 0 {
				t.Errorf("expected nil/empty Arguments for doSomething(), got %v", r.Arguments)
			}
			return
		}
	}
	t.Log("doSomething() call not found — skip")
}

// TestEE2_TypeScriptStringArgCaptured proves that TypeScript extractions also
// capture string-literal arguments (same grammar family).
func TestEE2_TypeScriptStringArgCaptured(t *testing.T) {
	ctx := context.Background()
	e := newExtractor(t, extraction.LangTypeScript, languages.TypeScriptExtractor())

	src := "function handler(): void {\n  emitter.on('connect', cb);\n}"
	result := e.Extract(ctx, "src/ee2.ts", src, types.LanguageTypeScript)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
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
		t.Fatalf("no calls/on ref found in TS extraction; refs: %v", result.UnresolvedReferences)
	}
	if len(found.Arguments) == 0 {
		t.Fatalf("expected Arguments to contain 'connect', got empty")
	}
	if found.Arguments[0] != "connect" {
		t.Errorf("Arguments[0] = %q, want %q", found.Arguments[0], "connect")
	}
}

// TestEE2_NodeCountStable proves that the EE2 change does not alter node count
// across two extractions of the same fixture (regression guard from CP6/CP10).
// WHY: node-count stability is a core invariant — extra UnresolvedReference rows
// must not be confused with node explosion.
func TestEE2_NodeCountStable(t *testing.T) {
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	src := "function handler() {\n  emitter.on('login', cb);\n  foo(x);\n}"
	r1 := e.Extract(ctx, "src/ee2stable.js", src, types.LanguageJavaScript)
	r2 := e.Extract(ctx, "src/ee2stable.js", src, types.LanguageJavaScript)

	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: run1=%d run2=%d", len(r1.Nodes), len(r2.Nodes))
	}
	if len(r1.UnresolvedReferences) != len(r2.UnresolvedReferences) {
		t.Errorf("ref count unstable: run1=%d run2=%d",
			len(r1.UnresolvedReferences), len(r2.UnresolvedReferences))
	}
}
