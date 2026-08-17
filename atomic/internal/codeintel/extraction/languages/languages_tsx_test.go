package languages_test

// TSX and JSX registration, and the component references a JSX tag produces.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Covers every tag shape at once: component, host, and member.
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

// The same content in a .jsx file, which also parses with the tsx grammar.
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

// Without a registry entry the orchestrator records the file and indexes no
// symbol in it, silently.
func TestTSX_Registered(t *testing.T) {
	t.Parallel()
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

// .jsx must map to the tsx grammar; the JS one needs mode flags to parse JSX.
func TestJSX_Registered(t *testing.T) {
	t.Parallel()
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

// The render synthesizer anchors its edge at the component, so a file record
// alone leaves it nothing to attach to.
func TestTSX_FunctionExtracted(t *testing.T) {
	t.Parallel()
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
	if len(result.Nodes) < 2 {
		t.Fatalf("expected >1 node (file + symbols); got %d", len(result.Nodes))
	}
}

func TestTSX_ClassExtracted(t *testing.T) {
	t.Parallel()
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

func TestTSX_InterfaceExtracted(t *testing.T) {
	t.Parallel()
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

// These refs are what the render synthesizer turns into component-to-component
// edges.
func TestTSX_JSXChildRefs_PascalCaseEmitted(t *testing.T) {
	t.Parallel()
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

// A lowercase tag is a DOM primitive, not a component use; emitting refs for
// them would bury the real edges.
func TestTSX_JSXChildRefs_HostTagsSkipped(t *testing.T) {
	t.Parallel()
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

// Resolution matches on the component's own name, so a member tag has to be
// reduced to its last segment.
func TestTSX_JSXChildRefs_MemberTagLastSegment(t *testing.T) {
	t.Parallel()
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

// The render edge is anchored at the component, so file-level attribution would
// make it unusable.
func TestTSX_JSXChildRefs_FromEnclosingFunction(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTSX)
	if !ok {
		t.Fatal("LanguageTSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsxFixturePath, tsxFixture, types.LanguageTSX)

	refs := refsOfKind(result.UnresolvedReferences, types.EdgeKindReferences)
	appFn := findNode(result.Nodes, types.NodeKindFunction, "AppComponent")
	if appFn == nil {
		t.Fatal("AppComponent function not found")
	}

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

func TestJSX_FunctionExtracted(t *testing.T) {
	t.Parallel()
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

func TestJSX_JSXChildRefs(t *testing.T) {
	t.Parallel()
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
	if refNames["span"] {
		t.Errorf("host tag 'span' should NOT emit a ref")
	}
}

// Non-determinism means double extraction, corrupt indexes, and unstable IDs.
func TestTSX_NodeCountStable(t *testing.T) {
	t.Parallel()
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

func findRefByName(refs []types.UnresolvedReference, kind types.EdgeKind, name string) *types.UnresolvedReference {
	for i := range refs {
		if refs[i].ReferenceKind == kind && strings.Contains(refs[i].ReferenceName, name) {
			return &refs[i]
		}
	}
	return nil
}
