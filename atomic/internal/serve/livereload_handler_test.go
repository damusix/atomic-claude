package serve_test

// livereload_handler_test.go — CP2 (live-reload): handler migration tests.
//
// CP1 built the shared snapshotStore; CP2 wires nav, page, rail, and
// graph-data to read through it (retiring the startup-frozen BuildLinkGraph
// singleton and the per-request nav walk). These tests exercise that wiring
// through real HTTP round-trips against the full production server
// (startTestServer / RunWithContext), proving a file added after the server
// starts is reflected on the very next request — no restart required.
//
// A newly-written fixture file's mtime is "now", which falls inside the
// snapshotStore's default 2s quiet window (see snapshot.go) and would not
// yet flip the fingerprint. Each test backdates the new file's mtime past
// that window (mirroring snapshot_internal_test.go's writeSnapFile helper)
// so the very next request already observes it, rather than sleeping in the
// test.

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// backdateFile sets path's mtime an hour in the past so a freshly-written
// fixture file clears the snapshot store's quiet window immediately.
func backdateFile(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// TestGraphDataFullView_ReflectsFileAddedAfterStartup verifies F-1: a file
// added to the realm after the server has started appears in the full-view
// /graph/data response on a later request, proving GraphDataHandler no
// longer serves a startup-frozen graph — through real HTTP round-trips
// against the public handler (not a store-internal unit test).
func TestGraphDataFullView_ReflectsFileAddedAfterStartup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "alpha.md"), "# Alpha\n")

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/graph/data")
	if err != nil {
		t.Fatalf("GET /graph/data (baseline): %v", err)
	}
	baseline, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read baseline body: %v", err)
	}
	if strings.Contains(string(baseline), `"beta.md"`) {
		t.Fatalf("baseline /graph/data unexpectedly already contains beta.md: %s", baseline)
	}

	betaPath := filepath.Join(root, "beta.md")
	writeFile(t, betaPath, "# Beta\n")
	backdateFile(t, betaPath)

	resp2, err := http.Get(baseURL + "/graph/data")
	if err != nil {
		t.Fatalf("GET /graph/data (after add): %v", err)
	}
	defer resp2.Body.Close()
	after, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("read after body: %v", err)
	}

	if !strings.Contains(string(after), `"beta.md"`) {
		t.Errorf("expected beta.md in /graph/data after being added post-startup (no restart); got:\n%s", after)
	}
}

// TestNav_ReflectsFileAddedAfterStartup verifies that a repo-scope nav tree
// reflects a docs file created after the server has started, without a
// restart — proving the per-request docs-tree walk is now sourced from the
// shared snapshot instead of a frozen or independently-walked file list.
func TestNav_ReflectsFileAddedAfterStartup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Readme\n")

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/nav")
	if err != nil {
		t.Fatalf("GET /nav (baseline): %v", err)
	}
	baseline, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read baseline nav body: %v", err)
	}
	if strings.Contains(string(baseline), "docs/guide.md") {
		t.Fatalf("baseline nav unexpectedly already contains docs/guide.md: %s", baseline)
	}

	guidePath := filepath.Join(root, "docs", "guide.md")
	writeFile(t, guidePath, "# Guide\n")
	backdateFile(t, guidePath)

	resp2, err := http.Get(baseURL + "/nav")
	if err != nil {
		t.Fatalf("GET /nav (after add): %v", err)
	}
	defer resp2.Body.Close()
	after, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("read after nav body: %v", err)
	}

	if !strings.Contains(string(after), "docs/guide.md") {
		t.Errorf("expected docs/guide.md in nav after being added post-startup (no restart); got:\n%s", after)
	}
}

// TestRail_ReflectsBacklinkFromFileAddedAfterStartup verifies that the right
// rail's inbound-links (#rail-in-content) fragment reflects a backlink from a
// page created after the server started, without a restart.
func TestRail_ReflectsBacklinkFromFileAddedAfterStartup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "hub.md"), "# Hub\n\nNo links yet.\n")

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/rail/hub.md")
	if err != nil {
		t.Fatalf("GET /rail/hub.md (baseline): %v", err)
	}
	baseline, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read baseline rail body: %v", err)
	}
	if strings.Contains(string(baseline), "spoke.md") {
		t.Fatalf("baseline rail unexpectedly already contains spoke.md: %s", baseline)
	}

	spokePath := filepath.Join(root, "spoke.md")
	writeFile(t, spokePath, "# Spoke\n\nSee [[hub]].\n")
	backdateFile(t, spokePath)

	resp2, err := http.Get(baseURL + "/rail/hub.md")
	if err != nil {
		t.Fatalf("GET /rail/hub.md (after add): %v", err)
	}
	defer resp2.Body.Close()
	after, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("read after rail body: %v", err)
	}

	if !strings.Contains(string(after), "spoke.md") {
		t.Errorf("expected spoke.md backlink in #rail-in-content after being added post-startup (no restart); got:\n%s", after)
	}
}

// TestPage_WikilinkToFileAddedAfterStartup_Resolves verifies that a wikilink
// pointing at a page created after the server started resolves (renders as a
// real link, not the wikilink-broken class) on the next /page request.
func TestPage_WikilinkToFileAddedAfterStartup_Resolves(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "hub.md"), "# Hub\n\nSee [[leaf]].\n")

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	req, err := http.NewRequest(http.MethodGet, baseURL+"/page/hub.md", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("HX-Request", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /page/hub.md (baseline): %v", err)
	}
	baseline, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read baseline page body: %v", err)
	}
	if !strings.Contains(string(baseline), "wikilink-broken") {
		t.Fatalf("baseline [[leaf]] must render broken (leaf.md does not exist yet); got:\n%s", baseline)
	}

	leafPath := filepath.Join(root, "leaf.md")
	writeFile(t, leafPath, "# Leaf\n")
	backdateFile(t, leafPath)

	req2, err := http.NewRequest(http.MethodGet, baseURL+"/page/hub.md", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req2.Header.Set("HX-Request", "true")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET /page/hub.md (after add): %v", err)
	}
	defer resp2.Body.Close()
	after, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("read after page body: %v", err)
	}

	if strings.Contains(string(after), "wikilink-broken") {
		t.Errorf("expected [[leaf]] to resolve once leaf.md exists (no restart); got:\n%s", after)
	}
	if !strings.Contains(string(after), `/page/leaf.md`) {
		t.Errorf("expected resolved wikilink to link to /page/leaf.md; got:\n%s", after)
	}
}
