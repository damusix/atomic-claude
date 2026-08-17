package serve

// The same typed-nil graphProvider trap context_handler_internal_test.go covers,
// on the rail handler.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// 404 is the right degrade: with no graph, no page can be confirmed a member,
// which is the handler's existing "page not in graph" branch.
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
