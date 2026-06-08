package sitter_test

import (
	"context"
	"testing"

	sitter "github.com/malivvan/tree-sitter"
)

// TestNamedChildCount verifies that NamedChildCount calls the named-child wasm
// function, not the all-children function.
//
// WHY this test exists: node.go:91 originally called n.t.nodeChildCount instead
// of n.t.nodeNamedChildCount. For any node that has anonymous-token children
// (punctuation, keywords, operators) the two counts differ. The iterator
// (iter.go:48) calls NamedChildCount to drive named traversal — if it returns
// the wrong (larger) count, NamedChild is called with out-of-range indices,
// producing bogus nodes and breaking the CP0b extractor.
//
// The test parses a tiny Go source and finds the source_file node's first
// named child (a package_clause). That node contains:
//   - the keyword token "package"  (anonymous — not a named child)
//   - the identifier "main"        (named child)
//
// So ChildCount == 2, NamedChildCount == 1. The bug returns 2 for both.
func TestNamedChildCount(t *testing.T) {
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

	lang, err := ts.LanguageGo(ctx)
	if err != nil {
		t.Fatalf("LanguageGo: %v", err)
	}
	if err := parser.SetLanguage(ctx, lang); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}

	// Minimal Go source — one package_clause node: keyword "package" + identifier "main"
	tree, err := parser.ParseString(ctx, "package main\n")
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}

	root, err := tree.RootNode(ctx)
	if err != nil {
		t.Fatalf("RootNode: %v", err)
	}

	// Root is source_file; find its first named child (package_clause)
	namedCount, err := root.NamedChildCount(ctx)
	if err != nil {
		t.Fatalf("root.NamedChildCount: %v", err)
	}
	if namedCount == 0 {
		t.Fatal("expected at least one named child of source_file")
	}

	pkgClause, err := root.NamedChild(ctx, 0)
	if err != nil {
		t.Fatalf("root.NamedChild(0): %v", err)
	}

	allCount, err := pkgClause.ChildCount(ctx)
	if err != nil {
		t.Fatalf("pkgClause.ChildCount: %v", err)
	}
	namedChildCount, err := pkgClause.NamedChildCount(ctx)
	if err != nil {
		t.Fatalf("pkgClause.NamedChildCount: %v", err)
	}

	// package_clause has 2 children total: "package" (anon token) + identifier (named)
	// NamedChildCount MUST be strictly less than ChildCount here.
	if allCount <= namedChildCount {
		t.Errorf(
			"package_clause: ChildCount=%d, NamedChildCount=%d — "+
				"expected NamedChildCount < ChildCount (anonymous tokens not counted); "+
				"this is the regression for node.go:91 calling nodeChildCount instead of nodeNamedChildCount",
			allCount, namedChildCount,
		)
	}

	// Also assert the named count is exactly 1 (only the identifier "main" is named)
	if namedChildCount != 1 {
		t.Errorf("package_clause.NamedChildCount: got %d, want 1", namedChildCount)
	}
}
