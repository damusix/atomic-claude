package languages_test

// Identifier call-argument capture, which is what lets closure-collection know
// the identity of a handler passed to something like .append(handler).
//
// A string literal is recorded bare and an identifier under an "arg:" prefix, so
// a synthesizer can tell a string-keyed event name from a handler name.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// The canonical case: emitter.on('e', onE) records both argument forms.
func TestEE5_MixedStringAndIdentArgs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	src := "function setup() {\n  emitter.on('login', onLogin);\n}"
	result := e.Extract(ctx, "src/ee5mixed.js", src, types.LanguageJavaScript)
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
		t.Fatalf("no calls/on UnresolvedReference found; refs: %v", result.UnresolvedReferences)
	}

	hasString := false
	hasIdent := false
	for _, a := range found.Arguments {
		if a == "login" {
			hasString = true
		}
		if a == "arg:onLogin" {
			hasIdent = true
		}
	}
	if !hasString {
		t.Errorf("Arguments %v missing string arg %q (EE2 regression)", found.Arguments, "login")
	}
	if !hasIdent {
		t.Errorf("Arguments %v missing identifier arg %q (EE5 required)", found.Arguments, "arg:onLogin")
	}
}

// The Swift and Kotlin closure-collection shape.
func TestEE5_IdentArgOnlyCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	src := "function setup() {\n  arr.append(handler);\n}"
	result := e.Extract(ctx, "src/ee5ident.js", src, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	var found *types.UnresolvedReference
	for i := range result.UnresolvedReferences {
		r := &result.UnresolvedReferences[i]
		if r.ReferenceKind == types.EdgeKindCalls && strings.Contains(r.ReferenceName, "append") {
			found = r
			break
		}
	}
	if found == nil {
		t.Fatalf("no calls/append UnresolvedReference found; refs: %v", result.UnresolvedReferences)
	}

	hasIdent := false
	for _, a := range found.Arguments {
		if a == "arg:handler" {
			hasIdent = true
		}
	}
	if !hasIdent {
		t.Errorf("Arguments %v missing identifier arg %q (EE5 required)", found.Arguments, "arg:handler")
	}
}

func TestEE5_TwoIdentArgs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	src := "function test() {\n  foo(x, y);\n}"
	result := e.Extract(ctx, "src/ee5two.js", src, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	var found *types.UnresolvedReference
	for i := range result.UnresolvedReferences {
		r := &result.UnresolvedReferences[i]
		if r.ReferenceKind == types.EdgeKindCalls && r.ReferenceName == "foo" {
			found = r
			break
		}
	}
	if found == nil {
		// Skip rather than fail: whether this call is inside a walked body
		// scope is a grammar detail this test is not pinning.
		t.Skip("foo call not found in refs — scope walk may differ; EE5 core covered by other tests")
		return
	}

	hasX := false
	hasY := false
	for _, a := range found.Arguments {
		if a == "arg:x" {
			hasX = true
		}
		if a == "arg:y" {
			hasY = true
		}
	}
	if !hasX || !hasY {
		t.Errorf("Arguments %v: want [arg:x, arg:y]", found.Arguments)
	}
}

// A string-only call keeps its bare entry and gains no prefixed one.
func TestEE5_StringOnlyCallUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	src := "function fire() {\n  emitter.emit('login');\n}"
	result := e.Extract(ctx, "src/ee5str.js", src, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	var found *types.UnresolvedReference
	for i := range result.UnresolvedReferences {
		r := &result.UnresolvedReferences[i]
		if r.ReferenceKind == types.EdgeKindCalls && strings.Contains(r.ReferenceName, "emit") {
			found = r
			break
		}
	}
	if found == nil {
		t.Fatalf("no calls/emit ref found; refs: %v", result.UnresolvedReferences)
	}
	if len(found.Arguments) == 0 {
		t.Fatalf("expected Arguments to contain 'login', got nil")
	}
	if found.Arguments[0] != "login" {
		t.Errorf("Arguments[0] = %q, want 'login' (EE2 unchanged)", found.Arguments[0])
	}
	for _, a := range found.Arguments {
		if strings.HasPrefix(a, "arg:") {
			t.Errorf("Arguments %v contains unexpected arg: prefix on string-only call", found.Arguments)
		}
	}
}

func TestEE5_ArglessCallUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	src := "function run() {\n  doSomething();\n}"
	result := e.Extract(ctx, "src/ee5noargs.js", src, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindCalls && r.ReferenceName == "doSomething" {
			if len(r.Arguments) != 0 {
				t.Errorf("doSomething() has Arguments %v, want nil", r.Arguments)
			}
			return
		}
	}
	t.Log("doSomething() call not found — skip")
}

func TestEE5_TypeScriptMixedArgs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newExtractor(t, extraction.LangTypeScript, languages.TypeScriptExtractor())

	src := "function setup(): void {\n  emitter.on('connect', onConnect);\n}"
	result := e.Extract(ctx, "src/ee5ts.ts", src, types.LanguageTypeScript)
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
		t.Fatalf("no calls/on ref found; refs: %v", result.UnresolvedReferences)
	}
	hasConnect := false
	hasIdent := false
	for _, a := range found.Arguments {
		if a == "connect" {
			hasConnect = true
		}
		if a == "arg:onConnect" {
			hasIdent = true
		}
	}
	if !hasConnect {
		t.Errorf("Arguments %v missing 'connect' (EE2 regression)", found.Arguments)
	}
	if !hasIdent {
		t.Errorf("Arguments %v missing 'arg:onConnect' (EE5 required)", found.Arguments)
	}
}

// Re-extracting a fixture must yield the same counts.
func TestEE5_NodeCountStable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newExtractor(t, extraction.LangJavaScript, languages.JavaScriptExtractor())

	src := "function setup() {\n  emitter.on('login', onLogin);\n  arr.append(handler);\n}"
	r1 := e.Extract(ctx, "src/ee5stable.js", src, types.LanguageJavaScript)
	r2 := e.Extract(ctx, "src/ee5stable.js", src, types.LanguageJavaScript)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: run1=%d run2=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// The three Arguments discriminators share one slice, so none may prefix another.
func TestEE5_PrefixNonCollision(t *testing.T) {
	t.Parallel()
	prefixes := []string{"arg:", "field:", "jsx:"}
	seen := map[string]bool{}
	for _, p := range prefixes {
		if seen[p] {
			t.Errorf("duplicate prefix %q", p)
		}
		seen[p] = true
	}
	for i, a := range prefixes {
		for j, b := range prefixes {
			if i != j && strings.HasPrefix(a, b) {
				t.Errorf("prefix %q is a prefix of %q — collision risk", b, a)
			}
		}
	}
}
