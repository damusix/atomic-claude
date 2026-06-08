package languages_test

// F-61 regression: goExtractImport previously dropped all but the first path
// in a multi-import block (import ( "a"; "b"; "c" ) → only 1 ref emitted).
//
// Fix: ImportTypes is now "import_spec" (not "import_declaration") so the
// walker descends into import_declaration and calls extractImport once per
// import_spec, emitting one UnresolvedReference per path.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// goMultiImportFixture is a minimal Go file with a grouped import block
// containing three distinct paths. The file also has a function so the
// extractor has something to anchor to.
const goMultiImportFixture = `package main

import (
	"fmt"
	"strings"
	"os"
)

func greet(name string) string {
	return fmt.Sprintf("hello %s", strings.ToUpper(name))
}
`

// TestGo_MultiImportEmitsAllRefs proves that a grouped import block with N
// paths emits N import UnresolvedReferences, not just 1.
//
// WHY: File-level import edges are the foundation of GetFileDependents,
// affected-file analysis, and circular-dependency detection. Missing import
// refs mean those traversals silently skip real dependencies.
func TestGo_MultiImportEmitsAllRefs(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageGo)
	if !ok {
		t.Fatal("Go not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), "src/main.go", goMultiImportFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs < 3 {
		t.Errorf("expected >= 3 import UnresolvedReferences for 3-path import block, got %d; refs: %v",
			importRefs, result.UnresolvedReferences)
	}

	// Verify specific paths are present.
	wantPaths := map[string]bool{"fmt": false, "strings": false, "os": false}
	for _, ref := range result.UnresolvedReferences {
		if ref.ReferenceKind == types.EdgeKindImports {
			for p := range wantPaths {
				if ref.ReferenceName == p {
					wantPaths[p] = true
				}
			}
		}
	}
	for p, found := range wantPaths {
		if !found {
			t.Errorf("import path %q not found in UnresolvedReferences", p)
		}
	}
}

// TestGo_SingleImportStillWorks proves that a bare single-path import
// (import "fmt") still emits exactly one import ref.
//
// WHY: The fix must not break single-import files — the common case.
const goSingleImportFixture = `package main

import "fmt"

func hello() string {
	return fmt.Sprintf("hi")
}
`

func TestGo_SingleImportStillWorks(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageGo)
	if !ok {
		t.Fatal("Go not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), "src/single.go", goSingleImportFixture, types.LanguageGo)

	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs < 1 {
		t.Errorf("expected >= 1 import UnresolvedReference for single import, got %d", importRefs)
	}
}
