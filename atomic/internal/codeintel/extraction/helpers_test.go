package extraction

import (
	"context"
	"strings"
	"testing"
)

// Every edge references node ids by value, so drift in the formula
// (kind + ":" + hex(sha256("filePath:kind:name:line"))[:32]) silently corrupts
// the whole edge table. These goldens were derived independently, outside Go.
type nodeIDCase struct {
	filePath string
	kind     string
	name     string
	line     int
	want     string
}

var nodeIDGoldens = []nodeIDCase{
	// Line encoding is 1-based.
	{
		filePath: "src/main.go",
		kind:     "function",
		name:     "main",
		line:     1,
		want:     "function:a9be729e3f1710774db40aa699ff076b",
	},
	{
		filePath: "src/main.go",
		kind:     "function",
		name:     "main",
		line:     10,
		want:     "function:88112bead45af3a69c236a0c9adb0c69",
	},
	{
		filePath: "src/auth/token.ts",
		kind:     "method",
		name:     "Token::validate",
		line:     42,
		want:     "method:a1171d6d17c0ca4c4bcf790c54cd182e",
	},
	{
		filePath: "src/db/pool.go",
		kind:     "class",
		name:     "Pool",
		line:     5,
		want:     "class:584ea4e5872bb717b14bf162337483e0",
	},
	// Python: the formula must not branch on language.
	{
		filePath: "src/utils.py",
		kind:     "function",
		name:     "parse_url",
		line:     100,
		want:     "function:c8c838d6e89ea4c0fef591b314239eac",
	},
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

// File nodes short-circuit the hash: id = "file:" + filePath, so name and line
// are irrelevant.
func TestGenerateNodeID_FileException(t *testing.T) {
	for _, tc := range fileNodeGoldens {
		got := generateNodeID(tc.filePath, "file", "", 0)
		if got != tc.want {
			t.Errorf("generateNodeID(%q, \"file\", \"\", 0) = %q, want %q",
				tc.filePath, got, tc.want)
		}
	}
}

// Package nodes take the same shape of exception as file nodes:
// id = "package:npm/" + name. See docs/spec/code-intel-package-nodes.md.
var packageNodeGoldens = []struct {
	name string
	want string
}{
	{"@hapi/hapi", "package:npm/@hapi/hapi"},
	{"vitest", "package:npm/vitest"},
}

func TestGenerateNodeID_PackageException(t *testing.T) {
	for _, tc := range packageNodeGoldens {
		got := generateNodeID("", "package", tc.name, 0)
		if got != tc.want {
			t.Errorf("generateNodeID(\"\", \"package\", %q, 0) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestGenerateNodeID_Stability(t *testing.T) {
	const want = "function:a9be729e3f1710774db40aa699ff076b"
	for i := 0; i < 50; i++ {
		got := generateNodeID("src/main.go", "function", "main", 1)
		if got != want {
			t.Fatalf("run %d: generateNodeID returned %q, want %q", i, got, want)
		}
	}
}

// Line is load-bearing in the id — this is the property that forces
// delete-then-reinsert when a symbol shifts lines.
func TestGenerateNodeID_LineChangesID(t *testing.T) {
	id1 := generateNodeID("src/main.go", "function", "main", 1)
	id2 := generateNodeID("src/main.go", "function", "main", 2)
	if id1 == id2 {
		t.Errorf("different lines produced identical ids: %q", id1)
	}
}

// goSnippet carries a two-line doc comment above the function; the helper tests
// below run against a real parse tree rather than a hand-built node.
const goSnippet = `package main

// Add adds two integers together.
// It is exported for testing purposes.
func Add(a, b int) int {
	return a + b
}
`

const goSnippetNoDoc = "package main\nfunc noDoc() {}\n"

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

// nodeText fills name, signature, and docstring; an off-by-one on the byte
// range corrupts every extracted field.
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

func TestNodeText_EmptyRange(t *testing.T) {
	text := nodeText(5, 5, "hello world")
	if text != "" {
		t.Errorf("nodeText(5, 5, ...) = %q, want \"\"", text)
	}
}

// Every language extractor locates function and class names through
// childByField, so a wrong child corrupts the stored node name. A missing field
// must come back nil with no error, not an error.
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

// Docstrings feed the FTS5-indexed nodes.docstring column, so a dropped line in
// the contiguous comment block above a declaration costs search recall.
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

func TestPrecedingDocstring_NoneWhenAbsent(t *testing.T) {
	_, funcStart, _, cleanup := findFuncDecl(t, goSnippetNoDoc)
	defer cleanup()

	docstring := precedingDocstring(funcStart, goSnippetNoDoc)
	if docstring != "" {
		t.Errorf("precedingDocstring = %q, want empty string when no comment present", docstring)
	}
}
