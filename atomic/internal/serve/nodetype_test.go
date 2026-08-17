package serve_test

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// Node typing has two routes, frontmatter and path convention; these cases pin
// both, plus the fallbacks when neither applies.
func TestNodeTypeResolverUnitCases(t *testing.T) {
	cases := []struct {
		name       string
		relpath    string            // realm-root-relative
		content    string            // may open with YAML frontmatter
		otherFiles map[string]string // extra realm files, keyed by relpath
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
			writeFile(t, filepath.Join(root, filepath.FromSlash(tc.relpath)), tc.content)
			for rel, body := range tc.otherFiles {
				writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), body)
			}
			// The server needs a landing page; it cannot affect the tested type.
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

			// Node IDs are realm-root-relative with forward slashes.
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

// The rail mini-graph runs through a different builder than the global graph, so
// it needs its own proof that node types survive.
func TestNodeTypeResolvedInLocalSubgraph(t *testing.T) {
	root := t.TempDir()
	// Typed by frontmatter.
	writeFile(t, filepath.Join(root, "wiki", "knowledge", "auth.md"),
		"---\ntype: Knowledge\n---\n# Auth\n\nSee [perf](../concerns/perf.md).\n")
	// Typed by path convention.
	writeFile(t, filepath.Join(root, "wiki", "concerns", "perf.md"),
		"# Perf\n")
	writeFile(t, filepath.Join(root, "index.md"), "# Index\n")

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	// depth=1 pulls in the concern the knowledge page links to.
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

	if got := typeByID["wiki/knowledge/auth.md"]; got != "knowledge" {
		t.Errorf("knowledge page: got type %q, want %q", got, "knowledge")
	}
	if got := typeByID["wiki/concerns/perf.md"]; got != "concern" {
		t.Errorf("concern page: got type %q, want %q", got, "concern")
	}
}

// nodeIDs flattens node ids for error messages.
func nodeIDs(elems cytoscapeElements) []string {
	ids := make([]string, 0, len(elems.Nodes))
	for _, n := range elems.Nodes {
		ids = append(ids, n.Data.ID)
	}
	return ids
}
