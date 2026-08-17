package serve_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// buildGraphOverlayRealm chains alpha -md-> beta -wiki-> gamma -wiki-> delta, so
// each page sits at a known depth from alpha and the link kinds differ per hop.
func buildGraphOverlayRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "alpha.md"), "# Alpha\n\nSee [beta](beta.md).\n")
	writeFile(t, filepath.Join(root, "beta.md"), "# Beta\n\nSee [[gamma]].\n")
	writeFile(t, filepath.Join(root, "gamma.md"), "# Gamma\n\nSee [[delta]].\n")
	writeFile(t, filepath.Join(root, "delta.md"), "# Delta\n\nNo outbound links.\n")
	return root
}

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

	if len(elems.Nodes) < 4 {
		t.Errorf("expected ≥4 nodes, got %d: %+v", len(elems.Nodes), elems.Nodes)
	}

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

	if len(elems.Edges) == 0 {
		t.Fatalf("expected edges, got none; body: %s", body)
	}

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

	if !nodeIDs["alpha.md"] {
		t.Errorf("local depth-1 view: alpha.md (origin) missing; nodes: %v", nodeIDs)
	}
	if !nodeIDs["beta.md"] {
		t.Errorf("local depth-1 view: beta.md (depth-1 neighbour) missing; nodes: %v", nodeIDs)
	}
	if nodeIDs["gamma.md"] {
		t.Errorf("local depth-1 view: gamma.md (depth-2) must be excluded, but found; nodes: %v", nodeIDs)
	}
	if nodeIDs["delta.md"] {
		t.Errorf("local depth-1 view: delta.md (depth-3) must be excluded, but found; nodes: %v", nodeIDs)
	}
}

// Every edge endpoint must be a node: the graph is page-to-page, and one edge
// pointing at a code file blanks the entire render. The sibling page→page link
// is here to catch a filter that over-prunes.
func TestGraphDataNoDanglingCodeFileEdge(t *testing.T) {
	root := t.TempDir()
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

	nodeIDs := make(map[string]bool, len(elems.Nodes))
	for _, n := range elems.Nodes {
		nodeIDs[n.Data.ID] = true
	}

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

	if nodeIDs["search.sh"] {
		t.Errorf("code file search.sh must not appear as a system-graph node")
	}

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

// The hover preview card and click modal read title/description/snippet off
// node.data(); missing fields render an empty card. Snippet must be the first
// prose line, never a heading.
func TestGraphDataNodePreviewFields(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "api-conventions.md"), `---
title: API Conventions
description: Rules for REST endpoint design.
type: Knowledge
---

# API Conventions

These conventions apply to all REST endpoints.
`)
	// No frontmatter: title falls back to the humanized filename.
	writeFile(t, filepath.Join(root, "auth-strategy.md"), `# Auth Strategy

OAuth2 with PKCE for browser clients.
`)
	// Body opens with a heading, so the snippet scanner has to skip past it.
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
		if strings.HasPrefix(m.Snippet, "#") {
			t.Errorf("api-conventions.md snippet must not start with '#', got %q", m.Snippet)
		}
	}

	if m, ok := byID["auth-strategy.md"]; !ok {
		t.Error("auth-strategy.md missing from /graph/data nodes")
	} else {
		if m.Title == "" {
			t.Error("auth-strategy.md title must not be empty (humanized fallback)")
		}
		if m.Snippet == "" {
			t.Error("auth-strategy.md snippet must not be empty")
		}
		if strings.HasPrefix(m.Snippet, "#") {
			t.Errorf("auth-strategy.md snippet starts with '#' — heading must be skipped, got %q", m.Snippet)
		}
	}

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

// Live-reload requires reading through the injected store. The store points at
// an empty dir, so any page.md in the response proves a per-request rebuild.
func TestGraphDataHandlerUsesInjectedGraph(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "page.md"), "# Page\n")

	emptyRoot := t.TempDir()
	store := serve.NewSnapshotStore(emptyRoot)

	handler := serve.NewGraphDataHandlerWithGraph(root, store)

	req := httptest.NewRequest(http.MethodGet, "/graph/data", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, `"nodes"`) {
		t.Fatalf("response missing 'nodes' field; body: %s", body)
	}
	if strings.Contains(body, "page.md") {
		t.Errorf("response contains 'page.md' — handler must use injected graph, not rebuild; body: %s", body)
	}
}

func startOpts(t *testing.T, root string) serve.Options {
	t.Helper()
	return serve.Options{
		Open:         false,
		TargetDir:    root,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
	}
}
