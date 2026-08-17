package languages_test

// Regression guard: a grouped import block once emitted a single reference
// instead of one per path.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// The function exists so the extractor has something to anchor to.
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

// Import edges carry dependent-file, affected-file, and cycle analysis, so a
// dropped path makes those traversals silently miss a real dependency.
func TestGo_MultiImportEmitsAllRefs(t *testing.T) {
	t.Parallel()
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

// The common case, which the grouped-import fix must not have broken.
const goSingleImportFixture = `package main

import "fmt"

func hello() string {
	return fmt.Sprintf("hi")
}
`

func TestGo_SingleImportStillWorks(t *testing.T) {
	t.Parallel()
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
