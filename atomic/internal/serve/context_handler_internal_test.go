package serve

// Tests for the typed-nil graphProvider trap in NewAPIPageHandler (FOLLOWUPS
// F-3): a nil *snapshotStore or nil *Graph passed as the graphProvider
// argument boxes into a non-nil interface value (the interface carries a
// type descriptor even though the pointer it holds is nil), so a naive
// `g == nil` check does not see it.
//
// Why internal: snapshotStore is unexported, so constructing a typed-nil
// *snapshotStore requires package serve rather than serve_test.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// nilMapGraphProvider is a defined map type implementing graphProvider (FOLLOWUPS
// F-5): isNilGraphProvider must catch a typed-nil non-pointer implementor, not
// just nil pointers.
type nilMapGraphProvider map[string]int

func (nilMapGraphProvider) currentGraph() *Graph { return nil }

// TestIsNilGraphProvider_NonPointerTypedNil proves isNilGraphProvider degrades
// for a typed-nil implementor whose underlying kind is not reflect.Ptr — the
// same trap the pointer cases below guard against, just on a map instead.
func TestIsNilGraphProvider_NonPointerTypedNil(t *testing.T) {
	var m nilMapGraphProvider // nil map value
	var g graphProvider = m
	if !isNilGraphProvider(g) {
		t.Error("isNilGraphProvider(nil map-typed provider) = false, want true")
	}
}

// TestNewAPIPageHandler_TypedNilGraphProviderDegradesWithoutPanic verifies
// that a typed-nil *snapshotStore or *Graph does not panic when
// NewAPIPageHandler serves a request — it must degrade to rendering the page
// with no graph-derived data (RenderMarkdownWithGraph tolerates a nil graph)
// rather than dereferencing the nil concrete pointer.
func TestNewAPIPageHandler_TypedNilGraphProviderDegradesWithoutPanic(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "page.md"), []byte("# Page\n"), 0o644); err != nil {
		t.Fatalf("write page.md: %v", err)
	}

	cases := map[string]graphProvider{
		"nil *snapshotStore": (*snapshotStore)(nil),
		"nil *Graph":         (*Graph)(nil),
	}

	for name, g := range cases {
		t.Run(name, func(t *testing.T) {
			handler := NewAPIPageHandler(root, g, "README.md")

			req := httptest.NewRequest(http.MethodGet, "/api/page/page.md", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req) // must not panic

			if rr.Code != http.StatusOK {
				t.Errorf("status: got %d, want 200 (degrade path must still render the page): body=%s", rr.Code, rr.Body.String())
			}
		})
	}
}
