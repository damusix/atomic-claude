package serve

// Tests for the typed-nil graphProvider trap in NewAPIRailHandler (same
// footgun as NewAPIPageHandler, see context_handler_internal_test.go): a nil
// *snapshotStore or nil *Graph passed as the graphProvider argument boxes
// into a non-nil interface value, so a naive `g == nil` check does not catch
// it and the handler panics dereferencing the nil concrete pointer.
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

// TestNewAPIRailHandler_TypedNilGraphProviderDegradesWithoutPanic verifies
// that a typed-nil *snapshotStore or *Graph does not panic when
// NewAPIRailHandler serves a request — it must degrade to 404, matching the
// handler's existing "page not in graph" branch (no graph means no page can
// be confirmed a member).
func TestNewAPIRailHandler_TypedNilGraphProviderDegradesWithoutPanic(t *testing.T) {
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
			handler := NewAPIRailHandler(root, g)

			req := httptest.NewRequest(http.MethodGet, "/api/rail/page.md", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req) // must not panic

			if rr.Code != http.StatusNotFound {
				t.Errorf("status: got %d, want %d (degrade path must be 404 — no graph, no confirmed membership)", rr.Code, http.StatusNotFound)
			}
		})
	}
}
