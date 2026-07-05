package serve

// Tests for the typed-nil graphProvider trap in NewPageHandlerWithGraph
// (FOLLOWUPS F-3): a nil *snapshotStore or nil *Graph passed as the
// graphProvider argument boxes into a non-nil interface value (the interface
// carries a type descriptor even though the pointer it holds is nil), so a
// naive `g == nil` check does not see it. The constructor's own doc comment
// promises "g may be nil ... degrades to NewPageHandler" — this test proves
// that promise actually holds for every concrete graphProvider implementor,
// not just a bare untyped nil.
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

// TestNewPageHandlerWithGraph_TypedNilGraphProviderDegradesWithoutPanic verifies
// that a typed-nil *snapshotStore or *Graph takes the exact same no-graph
// degrade path as NewPageHandlerWithGraph(root, nil) — not a panic when the
// handler later reads through the nil concrete pointer.
func TestNewPageHandlerWithGraph_TypedNilGraphProviderDegradesWithoutPanic(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "page.md"), []byte("# Page\n"), 0o644); err != nil {
		t.Fatalf("write page.md: %v", err)
	}

	// Baseline: what the documented no-graph degrade produces.
	want := NewPageHandler(root)
	reqWant := httptest.NewRequest(http.MethodGet, "/page/page.md", nil)
	reqWant.Header.Set("HX-Request", "true")
	rrWant := httptest.NewRecorder()
	want.ServeHTTP(rrWant, reqWant)

	cases := map[string]graphProvider{
		"nil *snapshotStore": (*snapshotStore)(nil),
		"nil *Graph":         (*Graph)(nil),
	}

	for name, g := range cases {
		t.Run(name, func(t *testing.T) {
			got := NewPageHandlerWithGraph(root, g)

			req := httptest.NewRequest(http.MethodGet, "/page/page.md", nil)
			req.Header.Set("HX-Request", "true")
			rr := httptest.NewRecorder()
			got.ServeHTTP(rr, req) // must not panic

			if rr.Code != rrWant.Code {
				t.Errorf("status: got %d, want %d (degrade path must match NewPageHandler)", rr.Code, rrWant.Code)
			}
			if rr.Body.String() != rrWant.Body.String() {
				t.Errorf("body must match the plain NewPageHandler degrade path:\ngot:  %s\nwant: %s", rr.Body.String(), rrWant.Body.String())
			}
		})
	}
}
