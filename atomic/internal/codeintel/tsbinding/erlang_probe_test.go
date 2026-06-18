package sitter_test

import (
	"context"
	"testing"

	sitter "github.com/malivvan/tree-sitter"
)

// TestErlang_GrammarLoadsAndParsesNamedNodes is the CP1 gate for Erlang language support.
//
// WHY: Confirms tree_sitter_erlang is exported from lib/ts.wasm, the binding handle
// loads without error, and a minimal Erlang source parses to a non-empty named-node
// tree. If this fails, the grammar is missing from the wasm or the export symbol is
// wrong — nothing else in the Erlang pipeline will work.
func TestErlang_GrammarLoadsAndParsesNamedNodes(t *testing.T) {
	ctx := context.Background()

	ts, err := sitter.New(ctx)
	if err != nil {
		t.Fatalf("sitter.New: %v", err)
	}

	parser, err := ts.NewParser(ctx)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	defer parser.Close(ctx)

	lang, err := ts.LanguageErlang(ctx)
	if err != nil {
		t.Fatalf("LanguageErlang: %v", err)
	}
	if err := parser.SetLanguage(ctx, lang); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}

	// Minimal Erlang source: a module with one exported function.
	const src = `-module(foo).
-export([bar/1]).
bar(X) -> X.
`
	tree, err := parser.ParseString(ctx, src)
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}

	root, err := tree.RootNode(ctx)
	if err != nil {
		t.Fatalf("RootNode: %v", err)
	}

	namedCount, err := root.NamedChildCount(ctx)
	if err != nil {
		t.Fatalf("NamedChildCount: %v", err)
	}
	if namedCount == 0 {
		t.Fatalf("Erlang parse produced 0 named children — grammar loaded but parsed to an empty tree")
	}

	t.Logf("Erlang parse: %d named children at root", namedCount)

	// Spot-check: first named child should be a module attribute.
	first, err := root.NamedChild(ctx, 0)
	if err != nil {
		t.Fatalf("NamedChild(0): %v", err)
	}
	firstKind, err := first.Kind(ctx)
	if err != nil {
		t.Fatalf("first.Kind: %v", err)
	}
	t.Logf("first named child kind: %q", firstKind)
	if firstKind == "" {
		t.Fatalf("first named child has empty kind string; expected a grammar node name")
	}
}
