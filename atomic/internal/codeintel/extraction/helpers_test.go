package extraction

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Node-id golden-vector tests — the R3 CI gate.
//
// WHY these vectors exist: node-id is load-bearing — every edge in the graph
// references node ids by value. Any divergence from the appendix-B formula
// silently corrupts the entire edge table.
//
// Formula (appendix B):
//   id = kind + ":" + hex(sha256("filePath:kind:name:line"))[:32]
// File-node exception:
//   id = "file:" + filePath (no hash)
//
// Goldens derived independently via Python:
//   import hashlib
//   def node_id(fp, k, n, l):
//       h = hashlib.sha256(f"{fp}:{k}:{n}:{l}".encode()).hexdigest()[:32]
//       return f"{k}:{h}"
// ---------------------------------------------------------------------------

type nodeIDCase struct {
	filePath string
	kind     string
	name     string
	line     int
	want     string
}

var nodeIDGoldens = []nodeIDCase{
	// line=1 edge case — must use 1-based line encoding.
	{
		filePath: "src/main.go",
		kind:     "function",
		name:     "main",
		line:     1,
		want:     "function:a9be729e3f1710774db40aa699ff076b",
	},
	// Same file + name, different line — ids must differ (line is load-bearing: R-E).
	{
		filePath: "src/main.go",
		kind:     "function",
		name:     "main",
		line:     10,
		want:     "function:88112bead45af3a69c236a0c9adb0c69",
	},
	// Method with "::" qualified name.
	{
		filePath: "src/auth/token.ts",
		kind:     "method",
		name:     "Token::validate",
		line:     42,
		want:     "method:a1171d6d17c0ca4c4bcf790c54cd182e",
	},
	// Class node.
	{
		filePath: "src/db/pool.go",
		kind:     "class",
		name:     "Pool",
		line:     5,
		want:     "class:584ea4e5872bb717b14bf162337483e0",
	},
	// Python function — ensures no language-specific branching.
	{
		filePath: "src/utils.py",
		kind:     "function",
		name:     "parse_url",
		line:     100,
		want:     "function:c8c838d6e89ea4c0fef591b314239eac",
	},
	// Variable node.
	{
		filePath: "cmd/atomic/main.go",
		kind:     "variable",
		name:     "version",
		line:     3,
		want:     "variable:6640e1d57211803186fd820222adea80",
	},
}

var fileNodeGoldens = []struct {
	filePath string
	want     string
}{
	{"src/main.go", "file:src/main.go"},
	{"src/auth/token.ts", "file:src/auth/token.ts"},
	{"deeply/nested/path/module.go", "file:deeply/nested/path/module.go"},
}

func TestGenerateNodeID_GoldenVectors(t *testing.T) {
	for _, tc := range nodeIDGoldens {
		got := generateNodeID(tc.filePath, tc.kind, tc.name, tc.line)
		if got != tc.want {
			t.Errorf("generateNodeID(%q, %q, %q, %d) = %q, want %q",
				tc.filePath, tc.kind, tc.name, tc.line, got, tc.want)
		}
	}
}

// TestGenerateNodeID_FileException verifies the file-node short-circuit:
// id = "file:" + filePath (no hash, no kind prefix from hash).
func TestGenerateNodeID_FileException(t *testing.T) {
	for _, tc := range fileNodeGoldens {
		// line=0 and name="" are irrelevant for file nodes.
		got := generateNodeID(tc.filePath, "file", "", 0)
		if got != tc.want {
			t.Errorf("generateNodeID(%q, \"file\", \"\", 0) = %q, want %q",
				tc.filePath, got, tc.want)
		}
	}
}

// TestGenerateNodeID_Stability verifies idempotence: sha256 is deterministic;
// repeated calls with the same inputs must produce the same output.
func TestGenerateNodeID_Stability(t *testing.T) {
	const want = "function:a9be729e3f1710774db40aa699ff076b"
	for i := 0; i < 50; i++ {
		got := generateNodeID("src/main.go", "function", "main", 1)
		if got != want {
			t.Fatalf("run %d: generateNodeID returned %q, want %q", i, got, want)
		}
	}
}

// TestGenerateNodeID_LineChangesID verifies that line is load-bearing in the
// id — the same (file, kind, name) at different lines MUST produce different
// ids. This is the property that forces delete-then-reinsert on line shift (R-E).
func TestGenerateNodeID_LineChangesID(t *testing.T) {
	id1 := generateNodeID("src/main.go", "function", "main", 1)
	id2 := generateNodeID("src/main.go", "function", "main", 2)
	if id1 == id2 {
		t.Errorf("different lines produced identical ids: %q", id1)
	}
}

// ---------------------------------------------------------------------------
// Helper tests — work on real parsed snippets via the pool/binding.
//
// We use the pool+binding to get a real sitter.Node, then test each helper.
// ---------------------------------------------------------------------------

// goSnippet is a minimal Go source with a block comment above a function.
const goSnippet = `package main

// Add adds two integers together.
// It is exported for testing purposes.
func Add(a, b int) int {
	return a + b
}
`

// goSnippetNoDoc is a Go source with no comments above the function.
const goSnippetNoDoc = "package main\nfunc noDoc() {}\n"

// findFuncDecl parses src as Go, finds the first function_declaration, and
// returns it. Fails the test if none is found.
func findFuncDecl(t *testing.T, src string) (inst Instance, funcStartByte uint64, funcEndByte uint64, cleanup func()) {
	t.Helper()
	ctx := context.Background()

	pool, err := NewPool(ctx, PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	i, err := pool.Borrow(ctx)
	if err != nil {
		pool.Close()
		t.Fatalf("Borrow: %v", err)
	}

	if err := i.SetLanguage(ctx, LangGo); err != nil {
		pool.Return(i)
		pool.Close()
		t.Fatalf("SetLanguage: %v", err)
	}

	tree, err := i.ParseString(ctx, src)
	if err != nil {
		pool.Return(i)
		pool.Close()
		t.Fatalf("ParseString: %v", err)
	}

	root, err := tree.(*tsTree).rootNode(ctx)
	if err != nil {
		pool.Return(i)
		pool.Close()
		t.Fatalf("RootNode: %v", err)
	}

	namedCount, err := root.NamedChildCount(ctx)
	if err != nil {
		pool.Return(i)
		pool.Close()
		t.Fatalf("NamedChildCount: %v", err)
	}

	for idx := uint64(0); idx < namedCount; idx++ {
		child, err := root.NamedChild(ctx, idx)
		if err != nil {
			pool.Return(i)
			pool.Close()
			t.Fatalf("NamedChild(%d): %v", idx, err)
		}
		kind, err := child.Kind(ctx)
		if err != nil {
			pool.Return(i)
			pool.Close()
			t.Fatalf("Kind: %v", err)
		}
		if kind == "function_declaration" {
			sb, err := child.StartByte(ctx)
			if err != nil {
				pool.Return(i)
				pool.Close()
				t.Fatalf("StartByte: %v", err)
			}
			eb, err := child.EndByte(ctx)
			if err != nil {
				pool.Return(i)
				pool.Close()
				t.Fatalf("EndByte: %v", err)
			}
			return i, sb, eb, func() {
				pool.Return(i)
				pool.Close()
			}
		}
	}

	pool.Return(i)
	pool.Close()
	t.Fatal("did not find function_declaration in parsed tree")
	return nil, 0, 0, nil
}

// TestNodeText verifies that nodeText returns the exact source slice for a
// parsed node's byte range.
// WHY: the extractor uses nodeText to populate name, signature, and docstring
// fields — an off-by-one here corrupts every extracted field.
func TestNodeText(t *testing.T) {
	_, start, end, cleanup := findFuncDecl(t, goSnippet)
	defer cleanup()

	text := nodeText(start, end, goSnippet)

	if !strings.HasPrefix(text, "func Add") {
		t.Errorf("nodeText prefix: got %q, want prefix \"func Add\"", text)
	}
	if !strings.HasSuffix(strings.TrimRight(text, "\n"), "}") {
		t.Errorf("nodeText suffix: got %q, want suffix \"}\"", text)
	}
}

// TestNodeText_EmptyRange verifies that an empty range returns an empty string.
func TestNodeText_EmptyRange(t *testing.T) {
	text := nodeText(5, 5, "hello world")
	if text != "" {
		t.Errorf("nodeText(5, 5, ...) = %q, want \"\"", text)
	}
}

// TestChildByField verifies ChildByField finds the "name" child of a Go
// function_declaration, and returns nil/no-error for a missing field.
// WHY: every language extractor calls childByField to locate the function/
// class name node — returning the wrong child corrupts the node's name in DB.
func TestChildByField(t *testing.T) {
	ctx := context.Background()
	pool, err := NewPool(ctx, PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	inst, err := pool.Borrow(ctx)
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	defer pool.Return(inst)

	if err := inst.SetLanguage(ctx, LangGo); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}

	tree, err := inst.ParseString(ctx, goSnippet)
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}

	root, err := tree.(*tsTree).rootNode(ctx)
	if err != nil {
		t.Fatalf("RootNode: %v", err)
	}

	namedCount, err := root.NamedChildCount(ctx)
	if err != nil {
		t.Fatalf("NamedChildCount: %v", err)
	}

	var funcFound bool
	for idx := uint64(0); idx < namedCount; idx++ {
		child, err := root.NamedChild(ctx, idx)
		if err != nil {
			t.Fatalf("NamedChild(%d): %v", idx, err)
		}
		kind, err := child.Kind(ctx)
		if err != nil {
			t.Fatalf("Kind: %v", err)
		}
		if kind != "function_declaration" {
			continue
		}
		funcFound = true

		// "name" field of a Go function_declaration is the identifier node.
		nameNode, err := childByField(ctx, child, "name")
		if err != nil {
			t.Fatalf("childByField(\"name\"): %v", err)
		}
		if nameNode == nil {
			t.Fatal("childByField(\"name\") returned nil, want identifier node")
		}

		start, err := nameNode.StartByte(ctx)
		if err != nil {
			t.Fatalf("nameNode.StartByte: %v", err)
		}
		end, err := nameNode.EndByte(ctx)
		if err != nil {
			t.Fatalf("nameNode.EndByte: %v", err)
		}
		nameText := nodeText(start, end, goSnippet)
		if nameText != "Add" {
			t.Errorf("\"name\" field text = %q, want \"Add\"", nameText)
		}

		// A non-existent field must return nil, nil (not an error).
		absent, err := childByField(ctx, child, "nonexistent_field_xyz")
		if err != nil {
			t.Fatalf("childByField(nonexistent): unexpected error: %v", err)
		}
		if absent != nil {
			absentStart, _ := absent.StartByte(ctx)
			absentEnd, _ := absent.EndByte(ctx)
			t.Errorf("childByField(nonexistent) = node(%d-%d), want nil",
				absentStart, absentEnd)
		}
		break
	}

	if !funcFound {
		t.Fatal("did not find function_declaration in parsed tree")
	}
}

// TestPrecedingDocstring verifies that precedingDocstring collects the
// contiguous block of line-comments immediately above a declaration.
// WHY: docstrings populate the nodes.docstring column used by FTS5 — a missed
// comment means the symbol is less discoverable in search.
func TestPrecedingDocstring(t *testing.T) {
	_, funcStart, _, cleanup := findFuncDecl(t, goSnippet)
	defer cleanup()

	docstring := precedingDocstring(funcStart, goSnippet)

	if docstring == "" {
		t.Fatal("precedingDocstring returned empty string, expected comment text")
	}
	if !strings.Contains(docstring, "Add adds two integers") {
		t.Errorf("precedingDocstring = %q, want it to contain \"Add adds two integers\"", docstring)
	}
	if !strings.Contains(docstring, "exported for testing") {
		t.Errorf("precedingDocstring = %q, want it to contain \"exported for testing\"", docstring)
	}
}

// TestPrecedingDocstring_NoneWhenAbsent verifies empty return when no comment
// precedes the declaration.
func TestPrecedingDocstring_NoneWhenAbsent(t *testing.T) {
	_, funcStart, _, cleanup := findFuncDecl(t, goSnippetNoDoc)
	defer cleanup()

	docstring := precedingDocstring(funcStart, goSnippetNoDoc)
	if docstring != "" {
		t.Errorf("precedingDocstring = %q, want empty string when no comment present", docstring)
	}
}
