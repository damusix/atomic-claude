package serve

// A nil *snapshotStore or *Graph boxes into a non-nil graphProvider interface —
// the interface carries a type descriptor even when the pointer inside is nil —
// so a naive `g == nil` check never sees it.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// A non-pointer implementor: the same trap reaches types whose kind is not Ptr.
type nilMapGraphProvider map[string]int

func (nilMapGraphProvider) currentGraph() *Graph { return nil }

func TestIsNilGraphProvider_NonPointerTypedNil(t *testing.T) {
	var m nilMapGraphProvider // nil map value
	var g graphProvider = m
	if !isNilGraphProvider(g) {
		t.Error("isNilGraphProvider(nil map-typed provider) = false, want true")
	}
}

// RenderMarkdownWithGraph tolerates a nil graph, so the handler must degrade to
// that rather than dereference the nil concrete pointer.
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
