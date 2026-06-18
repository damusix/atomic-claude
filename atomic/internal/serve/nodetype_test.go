package serve_test

// nodetype_test.go — CP2: hybrid node-type resolver tests.
//
// Spec (docs/spec/okf-alignment.md CP2):
//  1. Frontmatter `type` (title-case mapped to short class):
//     `Knowledge` → "knowledge", `Concern` → "concern", `Repo Summary` → "repo",
//     `Bucket` → "bucket". Mapping is case-insensitive ("KNOWLEDGE" → "knowledge").
//  2. Path-convention fallback (when frontmatter type is absent or unknown):
//     path segment `repos/` → "repo", `concerns/` → "concern", `knowledge/` → "knowledge".
//  3. Default: non-.md source-file node → "external"; everything else → "page".
//
// The shared resolver must be exercised through BOTH buildCytoElements (global
// graph) and buildLocalSubgraph (rail mini-graph) — neither may emit the old
// hardcoded "page" when a typed node is present in the graph.

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// TestNodeTypeResolverUnitCases exercises the resolver logic through the
// /graph/data endpoint. Each sub-test builds a minimal realm, starts a server,
// fetches /graph/data, and checks that the named node carries the expected type.
func TestNodeTypeResolverUnitCases(t *testing.T) {
	cases := []struct {
		name       string
		relpath    string            // relative path of the node under realm root
		content    string            // file content (may include YAML frontmatter)
		otherFiles map[string]string // additional files needed in the realm
		wantType   string
	}{
		{
			name:     "frontmatter type Knowledge → knowledge",
			relpath:  "wiki/knowledge/topic.md",
			content:  "---\ntype: Knowledge\n---\n# Topic\n",
			wantType: "knowledge",
		},
		{
			name:     "frontmatter type lowercase knowledge → knowledge",
			relpath:  "wiki/knowledge/topic2.md",
			content:  "---\ntype: knowledge\n---\n# Topic2\n",
			wantType: "knowledge",
		},
		{
			name:     "frontmatter type Concern → concern",
			relpath:  "wiki/concerns/perf.md",
			content:  "---\ntype: Concern\n---\n# Perf\n",
			wantType: "concern",
		},
		{
			name:     "frontmatter type Repo Summary → repo",
			relpath:  "wiki/repos/myrepo.md",
			content:  "---\ntype: Repo Summary\n---\n# My Repo\n",
			wantType: "repo",
		},
		{
			name:     "path convention repos/ → repo (no frontmatter type)",
			relpath:  "wiki/repos/other.md",
			content:  "# Other Repo\n",
			wantType: "repo",
		},
		{
			name:     "path convention concerns/ → concern",
			relpath:  "wiki/concerns/latency.md",
			content:  "# Latency\n",
			wantType: "concern",
		},
		{
			name:     "path convention knowledge/ → knowledge",
			relpath:  "wiki/knowledge/patterns.md",
			content:  "# Patterns\n",
			wantType: "knowledge",
		},
		{
			name:     "unknown frontmatter type with no path match → page",
			relpath:  "notes/random.md",
			content:  "---\ntype: Whatever\n---\n# Random\n",
			wantType: "page",
		},
		{
			name:     "plain .md file with no frontmatter → page",
			relpath:  "notes/plain.md",
			content:  "# Plain\n",
			wantType: "page",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			// Write the target file.
			writeFile(t, filepath.Join(root, filepath.FromSlash(tc.relpath)), tc.content)
			// Write any additional files.
			for rel, body := range tc.otherFiles {
				writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), body)
			}
			// Add an index.md so the realm is non-empty and the server has a
			// landing page; it won't affect the tested node's type.
			if tc.relpath != "index.md" {
				writeFile(t, filepath.Join(root, "index.md"), "# Index\n")
			}

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

			// Find the target node by ID (realm-root-relative path, forward slashes).
			var found *struct {
				Data struct {
					ID    string `json:"id"`
					Label string `json:"label"`
					Type  string `json:"type"`
				} `json:"data"`
			}
			for i := range elems.Nodes {
				if elems.Nodes[i].Data.ID == tc.relpath {
					found = &elems.Nodes[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("node %q not found in /graph/data; nodes: %v", tc.relpath, nodeIDs(elems))
				return
			}
			if found.Data.Type != tc.wantType {
				t.Errorf("node %q: got type %q, want %q", tc.relpath, found.Data.Type, tc.wantType)
			}
		})
	}
}

// TestNodeTypeResolvedInLocalSubgraph verifies that buildLocalSubgraph (the rail
// mini-graph) also emits the resolved node type, not the old hardcoded "page".
// It uses the /graph/data?node=<relpath>&depth=1 endpoint which exercises
// buildLocalSubgraph.
func TestNodeTypeResolvedInLocalSubgraph(t *testing.T) {
	root := t.TempDir()
	// knowledge page with explicit frontmatter type.
	writeFile(t, filepath.Join(root, "wiki", "knowledge", "auth.md"),
		"---\ntype: Knowledge\n---\n# Auth\n\nSee [perf](../concerns/perf.md).\n")
	// concern page, path-convention typed (no frontmatter type).
	writeFile(t, filepath.Join(root, "wiki", "concerns", "perf.md"),
		"# Perf\n")
	writeFile(t, filepath.Join(root, "index.md"), "# Index\n")

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	// Fetch the local subgraph centered on the knowledge page (depth=1 includes
	// the concern it links to).
	resp, err := http.Get(baseURL + "/graph/data?node=wiki/knowledge/auth.md&depth=1")
	if err != nil {
		t.Fatalf("GET /graph/data?node=...: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var elems cytoscapeElements
	if err := json.Unmarshal(body, &elems); err != nil {
		t.Fatalf("JSON unmarshal: %v\nbody: %s", err, body)
	}

	typeByID := make(map[string]string)
	for _, n := range elems.Nodes {
		typeByID[n.Data.ID] = n.Data.Type
	}

	// The knowledge page must carry type "knowledge" (frontmatter).
	if got := typeByID["wiki/knowledge/auth.md"]; got != "knowledge" {
		t.Errorf("knowledge page: got type %q, want %q", got, "knowledge")
	}
	// The concern page must carry type "concern" (path convention).
	if got := typeByID["wiki/concerns/perf.md"]; got != "concern" {
		t.Errorf("concern page: got type %q, want %q", got, "concern")
	}
}

// nodeIDs extracts just the IDs from a cytoscapeElements for error messages.
func nodeIDs(elems cytoscapeElements) []string {
	ids := make([]string, 0, len(elems.Nodes))
	for _, n := range elems.Nodes {
		ids = append(ids, n.Data.ID)
	}
	return ids
}
