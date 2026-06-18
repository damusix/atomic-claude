package sitter_test

import (
	"context"
	"testing"

	sitter "github.com/malivvan/tree-sitter"
)

// TestElixirProbe verifies that tree_sitter_elixir is exported from lib/ts.wasm
// and can parse a minimal Elixir snippet into a non-empty named-node tree.
//
// This is the Checkpoint 1 parse probe — grammar-into-wasm only.
func TestElixirProbe(t *testing.T) {
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

	lang, err := ts.LanguageElixir(ctx)
	if err != nil {
		t.Fatalf("LanguageElixir: %v", err)
	}
	if err := parser.SetLanguage(ctx, lang); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}

	src := "defmodule Foo do\n  def bar(x), do: x\nend\n"
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
		t.Fatal("root has 0 named children — grammar parsed to empty tree")
	}
	t.Logf("root named child count: %d", namedCount)

	rootKind, err := root.Kind(ctx)
	if err != nil {
		t.Fatalf("Kind: %v", err)
	}
	t.Logf("root node kind: %s", rootKind)

	child, err := root.NamedChild(ctx, 0)
	if err != nil {
		t.Fatalf("NamedChild(0): %v", err)
	}
	childKind, err := child.Kind(ctx)
	if err != nil {
		t.Fatalf("child.Kind: %v", err)
	}
	t.Logf("first named child kind: %s", childKind)
}
