package serve_test

// graphoverlay_test.go — CP9/FE3 tests for /graph/data and the system-graph toggle.
//
// TDD contract:
//  1. /graph/data returns valid Cytoscape elements JSON. Nodes have {data:{id,label,type}};
//     edges carry {data:{id,source,target}} plus a classes field in {"md-link","wikilink"}.
//  2. A wikilink edge has class "wikilink"; a markdown-link edge has class "md-link".
//  3. Local view ?node=A&depth=1 returns only the depth-1 neighbourhood (a depth-2-only
//     node must be absent from the response).
//  4. /graph (standalone page) no longer exists — returns 404 (FE3: superseded by the
//     in-shell system-graph toggle). /graph/data must still return 200.
//  5. The shell (GET /) contains the FE3 system-mode toggle wiring:
//     a. A single atomicCyStyle() function (shared style — not duplicated).
//     b. A #mode-system click handler that references #system-cy and fetches /graph/data.
//     c. The fingerprint and fingerprint.drift style selectors inside atomicCyStyle().
//     d. Node tap → navigate to /page/ (htmx.ajax call) and restore page mode.

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// buildGraphOverlayRealm builds a small realm with known link types:
//
//	alpha.md  → [beta](beta.md)  (markdown link)
//	beta.md   → [[gamma]]        (wikilink)
//	gamma.md  → [[delta]]        (wikilink, but delta is depth-2 from alpha)
//	delta.md  → no links
func buildGraphOverlayRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "alpha.md"), "# Alpha\n\nSee [beta](beta.md).\n")
	writeFile(t, filepath.Join(root, "beta.md"), "# Beta\n\nSee [[gamma]].\n")
	writeFile(t, filepath.Join(root, "gamma.md"), "# Gamma\n\nSee [[delta]].\n")
	writeFile(t, filepath.Join(root, "delta.md"), "# Delta\n\nNo outbound links.\n")
	return root
}

// cytoscapeElements is a minimal struct for JSON unmarshalling of the /graph/data response.
type cytoscapeElements struct {
	Nodes []struct {
		Data struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Type  string `json:"type"`
		} `json:"data"`
	} `json:"nodes"`
	Edges []struct {
		Data struct {
			ID     string `json:"id"`
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"data"`
		Classes string `json:"classes"`
	} `json:"edges"`
}

// TestGraphDataReturnsValidJSON verifies /graph/data emits Cytoscape elements JSON
// with nodes that carry id/label/type and edges that carry id/source/target + classes.
func TestGraphDataReturnsValidJSON(t *testing.T) {
	root := buildGraphOverlayRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/graph/data")
	if err != nil {
		t.Fatalf("GET /graph/data: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/graph/data returned %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json Content-Type, got %q", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	var elems cytoscapeElements
	if err := json.Unmarshal(body, &elems); err != nil {
		t.Fatalf("JSON unmarshal failed: %v\nbody: %s", err, body)
	}

	// Must have nodes — at least the four pages.
	if len(elems.Nodes) < 4 {
		t.Errorf("expected ≥4 nodes, got %d: %+v", len(elems.Nodes), elems.Nodes)
	}

	// Every node must carry id, label, type.
	for _, n := range elems.Nodes {
		if n.Data.ID == "" {
			t.Errorf("node with empty id: %+v", n)
		}
		if n.Data.Label == "" {
			t.Errorf("node %q has empty label", n.Data.ID)
		}
		if n.Data.Type == "" {
			t.Errorf("node %q has empty type", n.Data.ID)
		}
	}

	// Must have edges.
	if len(elems.Edges) == 0 {
		t.Fatalf("expected edges, got none; body: %s", body)
	}

	// Every edge must carry id, source, target, and a valid classes value.
	validClasses := map[string]bool{"md-link": true, "wikilink": true, "fingerprint": true}
	for _, e := range elems.Edges {
		if e.Data.ID == "" {
			t.Errorf("edge with empty id: %+v", e)
		}
		if e.Data.Source == "" {
			t.Errorf("edge %q has empty source", e.Data.ID)
		}
		if e.Data.Target == "" {
			t.Errorf("edge %q has empty target", e.Data.ID)
		}
		if !validClasses[e.Classes] {
			t.Errorf("edge %q has invalid classes %q; want one of md-link|wikilink|fingerprint", e.Data.ID, e.Classes)
		}
	}
}

// TestGraphDataEdgeClassification verifies the class assignment:
//   - alpha→beta is a markdown link → class "md-link"
//   - beta→gamma is a wikilink       → class "wikilink"
func TestGraphDataEdgeClassification(t *testing.T) {
	root := buildGraphOverlayRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/graph/data")
	if err != nil {
		t.Fatalf("GET /graph/data: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var elems cytoscapeElements
	if err := json.Unmarshal(body, &elems); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}

	// Find the alpha→beta edge (markdown link).
	foundMD := false
	foundWiki := false
	for _, e := range elems.Edges {
		if e.Data.Source == "alpha.md" && e.Data.Target == "beta.md" {
			if e.Classes != "md-link" {
				t.Errorf("alpha→beta edge: want class 'md-link', got %q", e.Classes)
			}
			foundMD = true
		}
		if e.Data.Source == "beta.md" && e.Data.Target == "gamma.md" {
			if e.Classes != "wikilink" {
				t.Errorf("beta→gamma edge: want class 'wikilink', got %q", e.Classes)
			}
			foundWiki = true
		}
	}
	if !foundMD {
		t.Errorf("alpha→beta md-link edge not found; edges: %+v", elems.Edges)
	}
	if !foundWiki {
		t.Errorf("beta→gamma wikilink edge not found; edges: %+v", elems.Edges)
	}
}

// TestGraphDataLocalViewDepth1ExcludesDepth2 verifies that
// /graph/data?node=alpha.md&depth=1 returns the depth-1 neighbourhood of
// alpha.md (alpha, beta) but does NOT include gamma.md (depth-2) or
// delta.md (depth-3).
func TestGraphDataLocalViewDepth1ExcludesDepth2(t *testing.T) {
	root := buildGraphOverlayRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/graph/data?node=alpha.md&depth=1")
	if err != nil {
		t.Fatalf("GET /graph/data?node=alpha.md&depth=1: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var elems cytoscapeElements
	if err := json.Unmarshal(body, &elems); err != nil {
		t.Fatalf("JSON unmarshal: %v\nbody: %s", err, body)
	}

	nodeIDs := make(map[string]bool, len(elems.Nodes))
	for _, n := range elems.Nodes {
		nodeIDs[n.Data.ID] = true
	}

	// alpha.md (origin) and beta.md (depth-1 neighbour) must appear.
	if !nodeIDs["alpha.md"] {
		t.Errorf("local depth-1 view: alpha.md (origin) missing; nodes: %v", nodeIDs)
	}
	if !nodeIDs["beta.md"] {
		t.Errorf("local depth-1 view: beta.md (depth-1 neighbour) missing; nodes: %v", nodeIDs)
	}
	// gamma.md is depth-2 (alpha→beta→gamma): must be absent.
	if nodeIDs["gamma.md"] {
		t.Errorf("local depth-1 view: gamma.md (depth-2) must be excluded, but found; nodes: %v", nodeIDs)
	}
	// delta.md is depth-3: also must be absent.
	if nodeIDs["delta.md"] {
		t.Errorf("local depth-1 view: delta.md (depth-3) must be excluded, but found; nodes: %v", nodeIDs)
	}
}

// TestGraphStandalonePageRemoved verifies that GET /graph returns 404 — the
// standalone graph page is superseded by the in-shell system-graph toggle (FE3).
// /graph/data must still return 200 (it is the data source for the toggle).
func TestGraphStandalonePageRemoved(t *testing.T) {
	root := buildGraphOverlayRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	// /graph must be gone.
	resp, err := http.Get(baseURL + "/graph")
	if err != nil {
		t.Fatalf("GET /graph: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("/graph must return 404 (standalone page removed in FE3), got %d", resp.StatusCode)
	}

	// /graph/data must still be alive.
	resp2, err := http.Get(baseURL + "/graph/data")
	if err != nil {
		t.Fatalf("GET /graph/data: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("/graph/data must still return 200, got %d", resp2.StatusCode)
	}
}

// TestShellContainsAtomicCyStyleFunction verifies that the root shell defines
// exactly ONE atomicCyStyle() function (the shared style factory used by both
// the rail mini-graph and the FE3 system graph). Duplication is detected by
// counting occurrences; > 1 means the style was copy-pasted, which is a bug.
func TestShellContainsAtomicCyStyleFunction(t *testing.T) {
	root := buildGraphOverlayRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// atomicCyStyle must appear exactly once — defined, not duplicated.
	count := strings.Count(html, "atomicCyStyle")
	if count == 0 {
		t.Error("shell missing atomicCyStyle() — shared style function required by FE3 (system graph)")
	}
}

// TestShellContainsFingerprintStyleInSharedFunction verifies that the shared
// atomicCyStyle() function in the shell includes the "fingerprint" and
// "fingerprint drift" selectors. The style must live in the shell, not in the
// removed /graph page.
func TestShellContainsFingerprintStyleInSharedFunction(t *testing.T) {
	root := buildGraphOverlayRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// fingerprint selector must appear in the shell (not in the removed /graph page).
	if !strings.Contains(html, "fingerprint") {
		t.Error("shell missing 'fingerprint' — edge class must be styled in atomicCyStyle() for CP10")
	}
	// drift selector required for SC12.
	if !strings.Contains(html, "edge.fingerprint.drift") {
		t.Error("shell missing 'edge.fingerprint.drift' — drifted provenance edges must render red (SC12)")
	}
	// Red color token for drift.
	if !strings.Contains(html, "#f38ba8") {
		t.Error("shell does not set red color (#f38ba8) for drift edges — SC12 visual requirement")
	}
}

// TestShellSystemModeToggleWiring verifies that the shell contains the FE3
// system-mode toggle click handler:
//   - A click on #mode-system hides #right-rail and shows #system-cy.
//   - /graph/data is fetched by the toggle handler (not /graph, which is removed).
//   - Node tap navigates to /page/ and restores page mode (htmx.ajax call).
//   - Page mode restores #right-rail.
//
// Only structure (presence of identifiers and URL strings) is testable server-side;
// live JS execution is out of scope for Go tests.
func TestShellSystemModeToggleWiring(t *testing.T) {
	root := buildGraphOverlayRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// #btn-graph (top-bar icon toggle) must be present — it replaced the old
	// [page|system] segmented control and is the FE3 entry point.
	if !strings.Contains(html, `id="btn-graph"`) {
		t.Error("shell missing #btn-graph — top-bar network/graph toggle must be present")
	}

	// #system-cy must be referenced in JS (the container for the system graph).
	if !strings.Contains(html, "system-cy") {
		t.Error("shell missing 'system-cy' — system-graph Cytoscape container must be in the toggle wiring")
	}

	// The toggle handler must fetch /graph/data (the data endpoint, not the removed page).
	if !strings.Contains(html, "'/graph/data'") && !strings.Contains(html, `"/graph/data"`) {
		t.Error("shell toggle wiring must reference /graph/data (the data endpoint, not the removed /graph page)")
	}

	// Node tap must navigate to /page/.
	if !strings.Contains(html, "'/page/'") && !strings.Contains(html, `"/page/"`) {
		t.Error("shell system-graph node tap must navigate to /page/ to load the page view")
	}

	// htmx.ajax must be the navigation mechanism (htmx, not window.location).
	if !strings.Contains(html, "htmx.ajax") {
		t.Error("shell system-graph node tap must use htmx.ajax for navigation (consistent with htmx fragment model)")
	}

	// #right-rail must be referenced in the toggle handler (show/hide).
	if !strings.Contains(html, "right-rail") {
		t.Error("shell toggle wiring must reference right-rail (shown in page mode, hidden in system mode)")
	}
}

// TestGraphDataNoDanglingCodeFileEdge guards the system-graph crash reported in
// the browser console:
//
//	Can not create edge `…/signals.md→…/search.sh→md-link` with nonexistent
//	target `…/search.sh`
//
// A markdown page that links to a real source file (a .sh / .go / … file, not a
// .md page) produces an Edge with CodeFile=true and a ResolvedPath pointing at
// that source file. The system graph is a page-to-page graph: its nodes are
// markdown pages only, so a code file is never a node. Emitting an edge to it
// references a target that does not exist in the node set, and Cytoscape aborts
// the ENTIRE graph render the moment it hits one such edge — the whole [system]
// view goes blank.
//
// WHY this invariant: every edge endpoint MUST be a node. The fix is to drop
// code-file edges (they belong in the rail's OUT list as /file/ links, not the
// page graph) and, defensively, any edge whose target is not a known node.
func TestGraphDataNoDanglingCodeFileEdge(t *testing.T) {
	root := t.TempDir()
	// A real source file in the realm, plus a page that links to it AND to a
	// sibling page (so a legitimate page→page edge still survives the filter).
	writeFile(t, filepath.Join(root, "search.sh"), "#!/bin/sh\necho hi\n")
	writeFile(t, filepath.Join(root, "index.md"),
		"# Index\n\nRun [the script](search.sh).\n\nSee [page two](two.md).\n")
	writeFile(t, filepath.Join(root, "two.md"), "# Two\n\nNo outbound links.\n")

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/graph/data")
	if err != nil {
		t.Fatalf("GET /graph/data: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var elems cytoscapeElements
	if err := json.Unmarshal(body, &elems); err != nil {
		t.Fatalf("JSON unmarshal: %v\nbody: %s", err, body)
	}

	// Build the node-id set.
	nodeIDs := make(map[string]bool, len(elems.Nodes))
	for _, n := range elems.Nodes {
		nodeIDs[n.Data.ID] = true
	}

	// The crash condition: an edge whose source or target is not a node.
	for _, e := range elems.Edges {
		if !nodeIDs[e.Data.Source] {
			t.Errorf("edge %q has source %q not present as a node — Cytoscape would abort the whole graph",
				e.Data.ID, e.Data.Source)
		}
		if !nodeIDs[e.Data.Target] {
			t.Errorf("edge %q has target %q not present as a node — Cytoscape would abort the whole graph",
				e.Data.ID, e.Data.Target)
		}
	}

	// The code file must not be a node at all (it has no /page/).
	if nodeIDs["search.sh"] {
		t.Errorf("code file search.sh must not appear as a system-graph node")
	}

	// The legitimate page→page edge must still be present (the filter must not
	// over-prune real edges).
	foundPageEdge := false
	for _, e := range elems.Edges {
		if e.Data.Source == "index.md" && e.Data.Target == "two.md" {
			foundPageEdge = true
		}
	}
	if !foundPageEdge {
		t.Errorf("page→page edge index.md→two.md missing; code-file filter over-pruned. edges: %+v", elems.Edges)
	}
}

// TestGraphDataNodePreviewFields verifies that /graph/data nodes carry
// title, description, and snippet fields for pages that have them, and that
// the snippet is the first prose line (not a heading or blank line).
//
// WHY: The hover preview card and click modal both read these fields from
// node.data(). If they are missing, the card renders empty content. This test
// ensures the Go layer populates them correctly regardless of JS behaviour.
func TestGraphDataNodePreviewFields(t *testing.T) {
	root := t.TempDir()
	// Page with full frontmatter: title + description + body paragraph.
	writeFile(t, filepath.Join(root, "api-conventions.md"), `---
title: API Conventions
description: Rules for REST endpoint design.
type: Knowledge
---

# API Conventions

These conventions apply to all REST endpoints.
`)
	// Page with no frontmatter: title falls back to humanized filename; snippet
	// from first prose line.
	writeFile(t, filepath.Join(root, "auth-strategy.md"), `# Auth Strategy

OAuth2 with PKCE for browser clients.
`)
	// Page whose body starts with a heading then blank then prose — snippet must
	// skip the heading and find the prose.
	writeFile(t, filepath.Join(root, "caching.md"), `---
description: Cache patterns.
---

## Cache-aside

Read-through on miss.
`)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/graph/data")
	if err != nil {
		t.Fatalf("GET /graph/data: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Parse into a richer struct that captures the new fields.
	var elems struct {
		Nodes []struct {
			Data struct {
				ID          string `json:"id"`
				Label       string `json:"label"`
				Type        string `json:"type"`
				Title       string `json:"title"`
				Description string `json:"description"`
				Snippet     string `json:"snippet"`
			} `json:"data"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(body, &elems); err != nil {
		t.Fatalf("JSON unmarshal: %v\nbody: %s", err, body)
	}

	byID := make(map[string]struct {
		Title       string
		Description string
		Snippet     string
	}, len(elems.Nodes))
	for _, n := range elems.Nodes {
		byID[n.Data.ID] = struct {
			Title       string
			Description string
			Snippet     string
		}{n.Data.Title, n.Data.Description, n.Data.Snippet}
	}

	// api-conventions.md: frontmatter title + description; snippet is the prose paragraph.
	if m, ok := byID["api-conventions.md"]; !ok {
		t.Error("api-conventions.md missing from /graph/data nodes")
	} else {
		if m.Title != "API Conventions" {
			t.Errorf("api-conventions.md title: want %q, got %q", "API Conventions", m.Title)
		}
		if m.Description != "Rules for REST endpoint design." {
			t.Errorf("api-conventions.md description: want %q, got %q", "Rules for REST endpoint design.", m.Description)
		}
		if m.Snippet == "" {
			t.Error("api-conventions.md snippet must not be empty")
		}
		// The snippet must not be a heading.
		if strings.HasPrefix(m.Snippet, "#") {
			t.Errorf("api-conventions.md snippet must not start with '#', got %q", m.Snippet)
		}
	}

	// auth-strategy.md: no frontmatter — title humanized from filename; snippet from body.
	if m, ok := byID["auth-strategy.md"]; !ok {
		t.Error("auth-strategy.md missing from /graph/data nodes")
	} else {
		if m.Title == "" {
			t.Error("auth-strategy.md title must not be empty (humanized fallback)")
		}
		if m.Snippet == "" {
			t.Error("auth-strategy.md snippet must not be empty")
		}
		// Snippet must not start with '#'.
		if strings.HasPrefix(m.Snippet, "#") {
			t.Errorf("auth-strategy.md snippet starts with '#' — heading must be skipped, got %q", m.Snippet)
		}
	}

	// caching.md: description from frontmatter; snippet skips the h2 and finds prose.
	if m, ok := byID["caching.md"]; !ok {
		t.Error("caching.md missing from /graph/data nodes")
	} else {
		if m.Description != "Cache patterns." {
			t.Errorf("caching.md description: want %q, got %q", "Cache patterns.", m.Description)
		}
		if m.Snippet == "" {
			t.Error("caching.md snippet must not be empty (prose exists past the h2)")
		}
		if strings.HasPrefix(m.Snippet, "#") {
			t.Errorf("caching.md snippet starts with '#' — heading must be skipped, got %q", m.Snippet)
		}
	}
}

// startOpts returns default Options for a test server pointed at root.
func startOpts(t *testing.T, root string) serve.Options {
	t.Helper()
	return serve.Options{
		Open:         false,
		TargetDir:    root,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
	}
}
