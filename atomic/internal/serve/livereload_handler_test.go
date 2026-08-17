package serve_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A fresh file's mtime sits inside the snapshot store's quiet window; backdating
// it lets the next request observe the change without a sleep.
func backdateFile(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

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

func TestNav_ReflectsFileAddedAfterStartup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Readme\n")

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/api/nav")
	if err != nil {
		t.Fatalf("GET /api/nav (baseline): %v", err)
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

	resp2, err := http.Get(baseURL + "/api/nav")
	if err != nil {
		t.Fatalf("GET /api/nav (after add): %v", err)
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

func TestRail_ReflectsBacklinkFromFileAddedAfterStartup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "hub.md"), "# Hub\n\nNo links yet.\n")

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/api/rail/hub.md")
	if err != nil {
		t.Fatalf("GET /api/rail/hub.md (baseline): %v", err)
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

	resp2, err := http.Get(baseURL + "/api/rail/hub.md")
	if err != nil {
		t.Fatalf("GET /api/rail/hub.md (after add): %v", err)
	}
	defer resp2.Body.Close()
	after, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("read after rail body: %v", err)
	}

	if !strings.Contains(string(after), "spoke.md") {
		t.Errorf("expected spoke.md backlink after being added post-startup (no restart); got:\n%s", after)
	}
}

func TestPage_WikilinkToFileAddedAfterStartup_Resolves(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "hub.md"), "# Hub\n\nSee [[leaf]].\n")

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/api/page/hub.md")
	if err != nil {
		t.Fatalf("GET /api/page/hub.md (baseline): %v", err)
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

	resp2, err := http.Get(baseURL + "/api/page/hub.md")
	if err != nil {
		t.Fatalf("GET /api/page/hub.md (after add): %v", err)
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
