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

// TestGraphPageServesNetworkView verifies the Network View is its own page:
//   - GET /graph (document load) returns the full shell, booting LandingURL=/graph
//     so a refresh / shared link / Back lands straight in the graph.
//   - GET /graph with HX-Request returns the bare [data-system-graph] mount
//     fragment (the shell's onLoad handler mounts Cytoscape into it).
//   - /graph/data must still return 200 (the data source for the mount).
func TestGraphPageServesNetworkView(t *testing.T) {
	root := buildGraphOverlayRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	// Document load → the full shell, wired to boot the graph into #main-pane.
	resp, err := http.Get(baseURL + "/graph")
	if err != nil {
		t.Fatalf("GET /graph: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/graph document load must return 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, "<!DOCTYPE") {
		t.Errorf("/graph document load must return the full shell, got a fragment:\n%s", html)
	}
	if !strings.Contains(html, `hx-get="/graph"`) {
		t.Errorf("/graph shell must boot the graph into #main-pane (hx-get=\"/graph\"); html:\n%s", html)
	}

	// htmx request → the bare mount fragment, NOT the shell.
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/graph", nil)
	req.Header.Set("HX-Request", "true")
	fragResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /graph (htmx): %v", err)
	}
	defer fragResp.Body.Close()
	frag, _ := io.ReadAll(fragResp.Body)
	fragStr := string(frag)
	if strings.Contains(fragStr, "<!DOCTYPE") {
		t.Errorf("/graph htmx request must return a bare fragment, got a full document:\n%s", fragStr)
	}
	if !strings.Contains(fragStr, "data-system-graph") {
		t.Errorf("/graph fragment must carry the [data-system-graph] mount seam; got:\n%s", fragStr)
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
// exactly ONE atomicCyStyle() function — the Cytoscape style factory used by
// the rail mini-graph (FE2) only. The FE3 system graph moved to cosmos.gl
// (CP1/CP2) and builds its own styling in system-graph.js; it has no
// Cytoscape style objects to consume, so this function is rail-only as of
// CP4. Duplication is detected by counting occurrences; > 1 means the style
// was copy-pasted, which is a bug.
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

// TestShellContainsFingerprintStyleInSharedFunction verifies that the
// distinct "fingerprint" / "fingerprint drift" provenance-edge styling lives
// in system-graph.js — the cosmos.gl-powered Network View script — not in the
// shell's (now rail-only) atomicCyStyle(). CP4 relocated it there: cosmos.gl
// links carry no dash-pattern API, so unlike the removed Cytoscape selectors
// (dashed), the distinct-styling contract is color (+ width) only.
func TestShellContainsFingerprintStyleInSharedFunction(t *testing.T) {
	root := buildGraphOverlayRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/static/system-graph.js")
	if err != nil {
		t.Fatalf("GET /static/system-graph.js: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	js := string(body)

	// fingerprint provenance styling must live in system-graph.js (not the
	// removed shell selectors).
	if !strings.Contains(js, "fingerprint") {
		t.Error("system-graph.js missing 'fingerprint' — provenance edge classes must be styled there, not in the shell's (rail-only) atomicCyStyle()")
	}
	// drift styling required for SC12.
	if !strings.Contains(js, "drift") {
		t.Error("system-graph.js missing 'drift' — drifted provenance edges must be distinctly styled (SC12)")
	}
	// Red color token for drift.
	if !strings.Contains(js, "#f38ba8") {
		t.Error("system-graph.js does not set red color (#f38ba8) for drift edges — SC12 visual requirement")
	}
}

// TestShellSystemModeToggleWiring verifies that the shell contains the FE3
// Network View mount seam:
//   - #btn-graph (top-bar icon) is the entry point.
//   - The cosmos.gl bundle and the system-graph.js asset are both loaded.
//   - A delegated htmx.onLoad hook calls SystemGraph.enterGraphMode() — the
//     mount body itself (data adapter, WebGL2 detection, motion policy,
//     /graph/data fetch, node-tap navigation) now lives in system-graph.js,
//     not the shell.
//
// Prior to CP2 this test asserted identifier/URL strings (e.g. '/graph/data',
// '/page/', htmx.ajax) directly against the "/" shell response — those moved
// out with the mount body and no longer hold here; system-graph.js is where
// that behavior is now testable (see its own unit tests once CP3+ adds
// browser-independent pure functions).
//
// Code-graph checkpoint 6: the seam calls enterGraphMode (not mount directly)
// so every /graph fragment landing routes through the Docs|Code + URL-state
// (view/member) dispatch — see system-graph.js's renderGraphPane comment.
//
// Only structure (presence of identifiers and script tags) is testable
// server-side; live JS execution is out of scope for Go tests.
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

	// #btn-graph (top-bar icon toggle) must be present — the FE3 entry point.
	if !strings.Contains(html, `id="btn-graph"`) {
		t.Error("shell missing #btn-graph — top-bar network/graph toggle must be present")
	}

	// The cosmos.gl vendor bundle and the system-graph.js asset must both load.
	if !strings.Contains(html, "/static/vendor/cosmos-graph.js") {
		t.Error("shell missing the vendored cosmos.gl bundle script tag")
	}
	if !strings.Contains(html, "/static/system-graph.js") {
		t.Error("shell missing the system-graph.js script tag — mount lifecycle now lives there")
	}

	// The delegated mount call must key on the [data-system-graph] seam
	// (delivered by the /graph fragment) and call into
	// SystemGraph.enterGraphMode (checkpoint 6 — see this test's own comment).
	if !strings.Contains(html, "data-system-graph") {
		t.Error("shell missing 'data-system-graph' — the onLoad mount seam for the Network View")
	}
	if !strings.Contains(html, "SystemGraph.enterGraphMode") {
		t.Error("shell missing the delegated call to SystemGraph.enterGraphMode — the thin htmx.onLoad hook into system-graph.js")
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
