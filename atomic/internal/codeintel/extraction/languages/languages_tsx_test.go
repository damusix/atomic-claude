package languages_test

// Tests for EE1: TSX/JSX registration + JSX child references.
//
// Success criteria (from BRIEF / spec):
//  1. LanguageTSX registered: .tsx extracts symbols (function/class/interface),
//     not just a file record.
//  2. LanguageJSX registered: .jsx extracts symbols using the tsx grammar.
//  3. TSX/JSX components emit "references" UnresolvedReferences for PascalCase
//     JSX element tags from the enclosing component node; lowercase host tags
//     (div, span, …) emit NONE; member tags (<Foo.Bar/>) use the last segment.
//  4. No regression: TS/JS also emit JSX refs when JSXElementTypes is populated.
//  5. Node count stable across two extractions.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// TSX fixtures
// ---------------------------------------------------------------------------

// tsxFixture is a .tsx file with a function component that renders children.
// Verified node types from tmp/probe-jsx-nodes:
//   - jsx_element: first named child is jsx_opening_element, whose first named
//     child is identifier (PascalCase tag name) or member_expression (Foo.Bar).
//   - jsx_self_closing_element: first named child is identifier or member_expression.
//   - Host tags (<div>, <span>) are lowercase identifiers — must be skipped.
const tsxFixture = `import React from "react";
import { Panel } from "./Panel";

interface AppProps { title: string; }

export function AppComponent({ title }: AppProps) {
    return (
        <Panel>
            <ChildWidget />
            <Foo.Bar />
            <div className="host" />
        </Panel>
    );
}

export class AppClass {
    render() {
        return <Modal title="hi" />;
    }
}
`

const tsxFixturePath = "src/App.tsx"

// jsxFixture is a .jsx file with similar JSX content — uses the tsx grammar.
const jsxFixture = `import React from "react";

export function JsxApp() {
    return (
        <Container>
            <Button label="ok" />
            <span className="text">hello</span>
        </Container>
    );
}
`

const jsxFixturePath = "src/App.jsx"

// ---------------------------------------------------------------------------
// TSX: registration
// ---------------------------------------------------------------------------

// TestTSX_Registered verifies LanguageTSX is in the registry with LangTSX.
// WHY: Without registration, the orchestrator silently falls through to file-record-only
// extraction for .tsx files — symbols are never indexed.
func TestTSX_Registered(t *testing.T) {
	reg := languages.NewRegistry()
	cfg, lang, ok := reg.For(types.LanguageTSX)
	if !ok {
		t.Fatal("LanguageTSX not registered (For returned ok=false)")
	}
	if lang != extraction.LangTSX {
		t.Errorf("For(LanguageTSX) Lang = %d, want LangTSX (%d)", lang, extraction.LangTSX)
	}
	if len(cfg.FunctionTypes) == 0 {
		t.Errorf("TSX config FunctionTypes is empty")
	}
}

// TestJSX_Registered verifies LanguageJSX is in the registry with LangTSX.
// WHY: .jsx files must use the tsx grammar (the js grammar doesn't parse JSX
// reliably without mode flags); without registration they fall through to
// file-record-only.
func TestJSX_Registered(t *testing.T) {
	reg := languages.NewRegistry()
	cfg, lang, ok := reg.For(types.LanguageJSX)
	if !ok {
		t.Fatal("LanguageJSX not registered (For returned ok=false)")
	}
	if lang != extraction.LangTSX {
		t.Errorf("For(LanguageJSX) Lang = %d, want LangTSX (%d)", lang, extraction.LangTSX)
	}
	if len(cfg.FunctionTypes) == 0 {
		t.Errorf("JSX config FunctionTypes is empty")
	}
}

// ---------------------------------------------------------------------------
// TSX: symbol extraction (not just file record)
// ---------------------------------------------------------------------------

// TestTSX_FunctionExtracted asserts .tsx function components are extracted.
// WHY: If only a file record is produced, no function node exists → the
// jsx-render synthesizer has no "from" node to attach the render edge to.
func TestTSX_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTSX)
	if !ok {
		t.Fatal("LanguageTSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsxFixturePath, tsxFixture, types.LanguageTSX)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "AppComponent")
	if fn == nil {
		t.Fatalf("AppComponent function not found; nodes: %s", nodeKindList(result.Nodes))
	}
	// Must have more than just the file node.
	if len(result.Nodes) < 2 {
		t.Fatalf("expected >1 node (file + symbols); got %d", len(result.Nodes))
	}
}

// TestTSX_ClassExtracted asserts class components are extracted.
func TestTSX_ClassExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTSX)
	if !ok {
		t.Fatal("LanguageTSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsxFixturePath, tsxFixture, types.LanguageTSX)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "AppClass")
	if cls == nil {
		t.Fatalf("AppClass not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestTSX_InterfaceExtracted asserts interfaces are extracted.
func TestTSX_InterfaceExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTSX)
	if !ok {
		t.Fatal("LanguageTSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsxFixturePath, tsxFixture, types.LanguageTSX)

	iface := findNode(result.Nodes, types.NodeKindInterface, "AppProps")
	if iface == nil {
		t.Fatalf("AppProps interface not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// ---------------------------------------------------------------------------
// JSX child references: PascalCase → emit ref, lowercase → skip
// ---------------------------------------------------------------------------

// TestTSX_JSXChildRefs_PascalCaseEmitted asserts PascalCase JSX tags emit
// "references" UnresolvedReferences from the enclosing component node.
// WHY: The jsx-render synthesizer needs these refs to build render edges;
// without them it cannot link AppComponent → ChildWidget / Panel.
func TestTSX_JSXChildRefs_PascalCaseEmitted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTSX)
	if !ok {
		t.Fatal("LanguageTSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsxFixturePath, tsxFixture, types.LanguageTSX)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	refs := refsOfKind(result.UnresolvedReferences, types.EdgeKindReferences)
	refNames := refNameSet(refs)

	want := []string{"Panel", "ChildWidget", "Modal"}
	for _, name := range want {
		if !refNames[name] {
			t.Errorf("expected 'references' ref named %q; refs: %v", name, refNameList(refs))
		}
	}
}

// TestTSX_JSXChildRefs_HostTagsSkipped asserts lowercase host tags (<div>, <span>)
// do NOT emit refs.
// WHY: Host elements are DOM primitives, not component usages — emitting refs for
// them would flood the graph with meaningless edges and pollute resolution.
func TestTSX_JSXChildRefs_HostTagsSkipped(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTSX)
	if !ok {
		t.Fatal("LanguageTSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsxFixturePath, tsxFixture, types.LanguageTSX)

	refs := refsOfKind(result.UnresolvedReferences, types.EdgeKindReferences)
	refNames := refNameSet(refs)

	forbidden := []string{"div", "span", "p", "a", "button"}
	for _, name := range forbidden {
		if refNames[name] {
			t.Errorf("host tag %q should NOT emit a ref, but it did", name)
		}
	}
}

// TestTSX_JSXChildRefs_MemberTagLastSegment asserts <Foo.Bar/> emits a ref
// named "Bar" (last segment of the member expression), not "Foo.Bar".
// WHY: Resolution matches refs against component *names* (not qualified paths);
// "Bar" is what the registry has, not "Foo.Bar".
func TestTSX_JSXChildRefs_MemberTagLastSegment(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTSX)
	if !ok {
		t.Fatal("LanguageTSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsxFixturePath, tsxFixture, types.LanguageTSX)

	refs := refsOfKind(result.UnresolvedReferences, types.EdgeKindReferences)
	refNames := refNameSet(refs)

	if !refNames["Bar"] {
		t.Errorf("expected 'references' ref 'Bar' from <Foo.Bar/>; refs: %v", refNameList(refs))
	}
	if refNames["Foo.Bar"] {
		t.Errorf("member tag ref should be last segment 'Bar', not 'Foo.Bar'")
	}
}

// TestTSX_JSXChildRefs_FromEnclosingFunction asserts JSX refs are attributed
// to the enclosing function/component node, not the file node.
// WHY: The jsx-render synthesizer uses FromNodeID to anchor the render edge at
// the component level; file-level attribution would make it unusable.
func TestTSX_JSXChildRefs_FromEnclosingFunction(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTSX)
	if !ok {
		t.Fatal("LanguageTSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsxFixturePath, tsxFixture, types.LanguageTSX)

	refs := refsOfKind(result.UnresolvedReferences, types.EdgeKindReferences)
	// Find the AppComponent function node.
	appFn := findNode(result.Nodes, types.NodeKindFunction, "AppComponent")
	if appFn == nil {
		t.Fatal("AppComponent function not found")
	}

	// At least one JSX ref should have FromNodeID == appFn.ID.
	foundMatch := false
	for _, ref := range refs {
		if ref.FromNodeID == appFn.ID {
			foundMatch = true
			break
		}
	}
	if !foundMatch {
		t.Errorf("no 'references' ref with FromNodeID=%q (AppComponent); refs: %v", appFn.ID, refNameList(refs))
	}
}

// ---------------------------------------------------------------------------
// JSX: .jsx file extraction
// ---------------------------------------------------------------------------

// TestJSX_FunctionExtracted asserts .jsx files extract function components.
// WHY: JSX grammar must work for .jsx files too — uses the same tsx grammar.
func TestJSX_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJSX)
	if !ok {
		t.Fatal("LanguageJSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), jsxFixturePath, jsxFixture, types.LanguageJSX)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "JsxApp")
	if fn == nil {
		t.Fatalf("JsxApp function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestJSX_JSXChildRefs asserts .jsx files also emit PascalCase JSX refs.
func TestJSX_JSXChildRefs(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJSX)
	if !ok {
		t.Fatal("LanguageJSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), jsxFixturePath, jsxFixture, types.LanguageJSX)

	refs := refsOfKind(result.UnresolvedReferences, types.EdgeKindReferences)
	refNames := refNameSet(refs)

	want := []string{"Container", "Button"}
	for _, name := range want {
		if !refNames[name] {
			t.Errorf("expected 'references' ref %q; refs: %v", name, refNameList(refs))
		}
	}
	// <span> must NOT be emitted.
	if refNames["span"] {
		t.Errorf("host tag 'span' should NOT emit a ref")
	}
}

// ---------------------------------------------------------------------------
// Regression: node count stability
// ---------------------------------------------------------------------------

// TestTSX_NodeCountStable asserts extraction is deterministic.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestTSX_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTSX)
	if !ok {
		t.Fatal("LanguageTSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, tsxFixturePath, tsxFixture, types.LanguageTSX)
	r2 := e.Extract(ctx, tsxFixturePath, tsxFixture, types.LanguageTSX)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
	if len(r1.UnresolvedReferences) != len(r2.UnresolvedReferences) {
		t.Errorf("ref count unstable: first=%d second=%d",
			len(r1.UnresolvedReferences), len(r2.UnresolvedReferences))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func refsOfKind(refs []types.UnresolvedReference, kind types.EdgeKind) []types.UnresolvedReference {
	out := make([]types.UnresolvedReference, 0)
	for _, r := range refs {
		if r.ReferenceKind == kind {
			out = append(out, r)
		}
	}
	return out
}

func refNameSet(refs []types.UnresolvedReference) map[string]bool {
	m := make(map[string]bool, len(refs))
	for _, r := range refs {
		m[r.ReferenceName] = true
	}
	return m
}

func refNameList(refs []types.UnresolvedReference) []string {
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		names = append(names, r.ReferenceName)
	}
	return names
}

// findRefByName returns the first UnresolvedReference with the given name and kind.
func findRefByName(refs []types.UnresolvedReference, kind types.EdgeKind, name string) *types.UnresolvedReference {
	for i := range refs {
		if refs[i].ReferenceKind == kind && strings.Contains(refs[i].ReferenceName, name) {
			return &refs[i]
		}
	}
	return nil
}
