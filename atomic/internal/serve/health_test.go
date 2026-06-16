package serve_test

// health_test.go — CP6: realm-health front page tests (TDD, written before implementation).
//
// Covers:
//  1. Realm with stale items → dashboard shows stale repo/concern/bucket badges.
//  2. Fully-fresh realm → "all fresh" state rendered.
//  3. Code-index health badge → severity + named repos from injected seam.
//  4. Repo/bare scope (no realm wiki) → renders without crashing (graceful no-realm).
//  5. Production wiring: HealthOptions.WikiStalenessSeam and IndexHealthSeam nil →
//     real engines are called (production-default functions are non-nil in New constructor).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// TestHealthDashboardStaleRealm verifies that when the staleness seam reports
// stale items, the dashboard renders the stale repo/concern/bucket badges.
func TestHealthDashboardStaleRealm(t *testing.T) {
	handler := serve.NewHealthHandler(serve.HealthOptions{
		RealmRoot:    "/fake/realm",
		IsRealmScope: true,
		WikiStalenessSeam: func(realmRoot string) serve.WikiStaleResult {
			return serve.WikiStaleResult{
				StaleRepos:     []string{"alpha", "beta"},
				StaleConcerns:  []string{"concern-x"},
				StaleBuckets:   []string{"research"},
				BucketDiffKeys: []string{"research"},
			}
		},
		IndexHealthSeam: func(realmRoot string) serve.IndexHealthResult {
			return serve.IndexHealthResult{
				Severity:   "PASS",
				Detail:     "code index: 3 fresh",
				FreshCount: 3,
			}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Stale repos must appear.
	if !strings.Contains(body, "alpha") {
		t.Error("dashboard missing stale repo 'alpha'")
	}
	if !strings.Contains(body, "beta") {
		t.Error("dashboard missing stale repo 'beta'")
	}

	// Stale concern must appear.
	if !strings.Contains(body, "concern-x") {
		t.Error("dashboard missing stale concern 'concern-x'")
	}

	// Bucket diff must appear.
	if !strings.Contains(body, "research") {
		t.Error("dashboard missing bucket 'research'")
	}

	// Should show a stale/warning indicator (not all-fresh).
	if !strings.Contains(body, "stale") && !strings.Contains(body, "STALE") && !strings.Contains(body, "warning") {
		t.Error("dashboard missing stale indicator for stale realm")
	}
}

// TestHealthDashboardFreshRealm verifies that a fully-fresh realm renders an
// "all fresh" state — no stale badges, a positive freshness indicator.
func TestHealthDashboardFreshRealm(t *testing.T) {
	handler := serve.NewHealthHandler(serve.HealthOptions{
		RealmRoot:    "/fake/realm",
		IsRealmScope: true,
		WikiStalenessSeam: func(realmRoot string) serve.WikiStaleResult {
			return serve.WikiStaleResult{} // nothing stale
		},
		IndexHealthSeam: func(realmRoot string) serve.IndexHealthResult {
			return serve.IndexHealthResult{
				Severity:   "PASS",
				Detail:     "code index: 5 fresh",
				FreshCount: 5,
			}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Must show an all-fresh or OK indicator.
	hasFresh := strings.Contains(body, "fresh") ||
		strings.Contains(body, "all clear") ||
		strings.Contains(body, "All fresh") ||
		strings.Contains(body, "OK") ||
		strings.Contains(body, "ok") ||
		strings.Contains(body, "healthy")
	if !hasFresh {
		t.Errorf("dashboard missing 'all fresh' state indicator; body:\n%s", body)
	}
}

// TestHealthDashboardCodeIndexHealth verifies that the code-index health badge
// renders the severity and named repos from the injected seam.
func TestHealthDashboardCodeIndexHealth(t *testing.T) {
	handler := serve.NewHealthHandler(serve.HealthOptions{
		RealmRoot:    "/fake/realm",
		IsRealmScope: true,
		WikiStalenessSeam: func(realmRoot string) serve.WikiStaleResult {
			return serve.WikiStaleResult{}
		},
		IndexHealthSeam: func(realmRoot string) serve.IndexHealthResult {
			return serve.IndexHealthResult{
				Severity:     "WARN",
				Detail:       "code index: 2 fresh; stale: foo, bar (run atomic code sync); not indexed: baz",
				FreshCount:   2,
				StaleMembers: []string{"foo", "bar"},
				NotIndexed:   []string{"baz"},
			}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Detail line with named repos must appear somewhere in the body.
	if !strings.Contains(body, "foo") {
		t.Error("dashboard missing stale member 'foo' from code-index health")
	}
	if !strings.Contains(body, "bar") {
		t.Error("dashboard missing stale member 'bar' from code-index health")
	}
	if !strings.Contains(body, "baz") {
		t.Error("dashboard missing not-indexed member 'baz' from code-index health")
	}

	// WARN severity must be surfaced.
	if !strings.Contains(body, "WARN") && !strings.Contains(body, "warn") && !strings.Contains(body, "warning") && !strings.Contains(body, "stale") {
		t.Error("dashboard missing WARN indicator from code-index health")
	}
}

// TestHealthDashboardRepoScope verifies that a repo/bare scope (no realm wiki)
// renders without crashing and shows a graceful no-realm message or just code-index health.
func TestHealthDashboardRepoScope(t *testing.T) {
	handler := serve.NewHealthHandler(serve.HealthOptions{
		RealmRoot:    "/fake/repo",
		IsRealmScope: false, // repo scope — no wiki
		// WikiStalenessSeam not set: should not be called in repo scope.
		IndexHealthSeam: func(realmRoot string) serve.IndexHealthResult {
			return serve.IndexHealthResult{
				Severity:   "PASS",
				Detail:     "code index: 1 fresh",
				FreshCount: 1,
			}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Must not 500.
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for repo scope, got %d", rr.Code)
	}

	body := rr.Body.String()
	if len(body) == 0 {
		t.Error("dashboard returned empty body for repo scope")
	}
}

// TestHealthDashboardBucketDiffCount verifies the bucket diff count is rendered
// when multiple buckets have diffs.
func TestHealthDashboardBucketDiffCount(t *testing.T) {
	handler := serve.NewHealthHandler(serve.HealthOptions{
		RealmRoot:    "/fake/realm",
		IsRealmScope: true,
		WikiStalenessSeam: func(realmRoot string) serve.WikiStaleResult {
			return serve.WikiStaleResult{
				BucketDiffKeys: []string{"research", "raw", "tickets"},
			}
		},
		IndexHealthSeam: func(realmRoot string) serve.IndexHealthResult {
			return serve.IndexHealthResult{Severity: "PASS", Detail: "code index: 0 fresh"}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// At least one of the bucket names or the count "3" must appear.
	hasCount := strings.Contains(body, "3") || strings.Contains(body, "research") ||
		strings.Contains(body, "raw") || strings.Contains(body, "tickets")
	if !hasCount {
		t.Error("dashboard missing bucket diff count or bucket names")
	}
}

// TestHealthDashboardProductionSeamsAreNotNil verifies that when seams are nil,
// NewHealthHandler still works (production path doesn't panic on nil seams;
// the constructor sets defaults). We call the handler and assert it returns 200
// without panicking — the actual production engines may error gracefully.
//
// Note: we use a non-existent realmRoot so the real wiki.Stale returns a hard
// error (StaleCodeError), which the production WikiStalenessSeam must handle
// gracefully (return empty WikiStaleResult rather than panic).
func TestHealthDashboardProductionSeamsAreNotNil(t *testing.T) {
	dir := t.TempDir() // exists on disk; wiki.Stale will error (no wiki/)

	handler := serve.NewHealthHandler(serve.HealthOptions{
		RealmRoot:    dir,
		IsRealmScope: true,
		// Both seams are nil → production defaults must be wired.
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	// Must not panic; must return 200 even when real engines find nothing.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("production health handler panicked: %v", r)
		}
	}()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 from production handler, got %d", rr.Code)
	}
	if len(rr.Body.String()) == 0 {
		t.Error("production health handler returned empty body")
	}
}
