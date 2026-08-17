package serve_test

// api_status_external_test.go — /api/status and /api/external JSON
// shape tests. TDD: written to assert the shapes pinned in the spec's
// ## API contracts table before/alongside implementation.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// ─── /api/status ──────────────────────────────────────────────────────────────

func TestAPIStatus_Shape(t *testing.T) {
	handler := serve.NewAPIStatusHandler(serve.HealthOptions{
		RealmRoot:    "/fake/realm",
		IsRealmScope: true,
		WikiStalenessSeam: func(realmRoot string) serve.WikiStaleResult {
			return serve.WikiStaleResult{
				StaleRepos:     []string{"alpha"},
				StaleConcerns:  []string{"concern-x"},
				StaleBuckets:   []string{"research"},
				BucketDiffKeys: []string{"research"},
			}
		},
		IndexHealthSeam: func(realmRoot string) serve.IndexHealthResult {
			return serve.IndexHealthResult{
				Severity:     "WARN",
				Detail:       "code index: 2 fresh; stale: foo (run atomic code sync); not indexed: baz",
				FreshCount:   2,
				StaleMembers: []string{"foo"},
				NotIndexed:   []string{"baz"},
			}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var got struct {
		IsRealmScope bool `json:"isRealmScope"`
		Wiki         struct {
			StaleRepos     []string `json:"staleRepos"`
			StaleConcerns  []string `json:"staleConcerns"`
			StaleBuckets   []string `json:"staleBuckets"`
			BucketDiffKeys []string `json:"bucketDiffKeys"`
			AllFresh       bool     `json:"allFresh"`
		} `json:"wiki"`
		Index struct {
			Severity     string   `json:"severity"`
			Detail       string   `json:"detail"`
			FreshCount   int      `json:"freshCount"`
			StaleMembers []string `json:"staleMembers"`
			NotIndexed   []string `json:"notIndexed"`
		} `json:"index"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if !got.IsRealmScope {
		t.Error("isRealmScope: got false, want true")
	}
	if len(got.Wiki.StaleRepos) != 1 || got.Wiki.StaleRepos[0] != "alpha" {
		t.Errorf("wiki.staleRepos: got %v", got.Wiki.StaleRepos)
	}
	if got.Wiki.AllFresh {
		t.Error("wiki.allFresh: got true, want false (stale items present)")
	}
	if got.Index.Severity != "WARN" {
		t.Errorf("index.severity: got %q", got.Index.Severity)
	}
	if len(got.Index.StaleMembers) != 1 || got.Index.StaleMembers[0] != "foo" {
		t.Errorf("index.staleMembers: got %v", got.Index.StaleMembers)
	}
	if len(got.Index.NotIndexed) != 1 || got.Index.NotIndexed[0] != "baz" {
		t.Errorf("index.notIndexed: got %v", got.Index.NotIndexed)
	}
}

func TestAPIStatus_AllFresh(t *testing.T) {
	handler := serve.NewAPIStatusHandler(serve.HealthOptions{
		RealmRoot:    "/fake/realm",
		IsRealmScope: true,
		WikiStalenessSeam: func(realmRoot string) serve.WikiStaleResult {
			return serve.WikiStaleResult{}
		},
		IndexHealthSeam: func(realmRoot string) serve.IndexHealthResult {
			return serve.IndexHealthResult{Severity: "PASS", Detail: "code index: 5 fresh", FreshCount: 5}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var got struct {
		Wiki struct {
			AllFresh   bool     `json:"allFresh"`
			StaleRepos []string `json:"staleRepos"`
		} `json:"wiki"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if !got.Wiki.AllFresh {
		t.Error("wiki.allFresh: got false, want true")
	}
	if got.Wiki.StaleRepos == nil {
		t.Error("wiki.staleRepos: got nil, want non-null empty array")
	}
}

// TestAPIStatus_RepoScope_NoWikiSeamCalled verifies repo scope never invokes
// the wiki staleness seam and still returns 200.
func TestAPIStatus_RepoScope_NoWikiSeamCalled(t *testing.T) {
	handler := serve.NewAPIStatusHandler(serve.HealthOptions{
		RealmRoot:    "/fake/repo",
		IsRealmScope: false,
		IndexHealthSeam: func(realmRoot string) serve.IndexHealthResult {
			return serve.IndexHealthResult{Severity: "PASS", Detail: "code index: 1 fresh"}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var got struct {
		IsRealmScope bool `json:"isRealmScope"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if got.IsRealmScope {
		t.Error("isRealmScope: got true, want false")
	}
}

// ─── /api/external ────────────────────────────────────────────────────────────

func TestAPIExternal_Shape(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pageA.md"),
		"# A\n\nSee [example](https://example.com/x) and [foo](http://foo.test).\n")
	writeFile(t, filepath.Join(root, "pageB.md"),
		"# B\n\nAlso see [example](https://example.com/x).\n")

	fixed := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	handler := serve.NewAPIExternalHandler(root, fixedDateFn(fixed), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/external", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var got struct {
		Entries []struct {
			URL       string   `json:"url"`
			Sources   []string `json:"sources"`
			FirstSeen *string  `json:"firstSeen"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if len(got.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(got.Entries), got.Entries)
	}

	var example *struct {
		URL       string   `json:"url"`
		Sources   []string `json:"sources"`
		FirstSeen *string  `json:"firstSeen"`
	}
	for i := range got.Entries {
		if got.Entries[i].URL == "https://example.com/x" {
			example = &got.Entries[i]
		}
	}
	if example == nil {
		t.Fatal("https://example.com/x not found in entries")
	}
	if len(example.Sources) != 2 {
		t.Errorf("sources: got %v, want [pageA.md pageB.md]", example.Sources)
	}
	if example.FirstSeen == nil || *example.FirstSeen != "2024-05-01" {
		t.Errorf("firstSeen: got %v, want 2024-05-01", example.FirstSeen)
	}
}

// TestAPIExternal_ZeroDate_NullFirstSeen verifies a zero-time FirstSeen
// encodes as JSON null, not an empty string or zero-value date.
func TestAPIExternal_ZeroDate_NullFirstSeen(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "page.md"), "# P\n\nSee [x](https://example.com/x).\n")

	// perFileDateFn returns time.Time{} (zero) for any unmapped path.
	handler := serve.NewAPIExternalHandler(root, perFileDateFn(nil), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/external", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var got struct {
		Entries []struct {
			FirstSeen *string `json:"firstSeen"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if len(got.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got.Entries))
	}
	if got.Entries[0].FirstSeen != nil {
		t.Errorf("firstSeen: got %v, want null", *got.Entries[0].FirstSeen)
	}
}

func TestAPIExternal_NoLinks_EmptyEntries(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "page.md"), "# P\n\nNo external links here.\n")

	handler := serve.NewAPIExternalHandler(root, fixedDateFn(time.Now()), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/external", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var got struct {
		Entries []any `json:"entries"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if got.Entries == nil {
		t.Error("entries: got null, want non-null empty array")
	}
	if len(got.Entries) != 0 {
		t.Errorf("entries: got %d, want 0", len(got.Entries))
	}
}
