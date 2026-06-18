// graphoverlay.go — CP9/FE3: /graph/data JSON endpoint.
//
// /graph/data emits the realm link graph as Cytoscape elements JSON:
//
//	{ "nodes": [{data:{id,label,type}}], "edges": [{data:{id,source,target}, classes:"..."}] }
//
// Global view: all nodes + all resolved (non-broken, non-external) edges.
// Local view:  ?node=<relpath>&depth=<1|2> → BFS subgraph within depth hops
//
//	(both inbound and outbound). Default depth 2 when node= is given.
//
// Three edge classes (SC11):
//   - "md-link"     — standard markdown link [text](path)
//   - "wikilink"    — Obsidian [[page]] link
//   - "fingerprint" — provenance edges (CP10 fills; styled in layout.html atomicCyStyle())
//
// FE3: the standalone /graph page has been removed. The system-graph toggle in
// layout.html replaces it — clicking [system] in the shell swaps #main-pane to
// a Cytoscape instance that fetches /graph/data directly. The three vendored JS
// scripts and atomicCyStyle() live in layout.html <head> so both the rail
// mini-graph (FE2) and the system graph (FE3) share the same loaded assets.
package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
)

// cytoNode is a Cytoscape node element.
type cytoNode struct {
	Data cytoNodeData `json:"data"`
}

type cytoNodeData struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Snippet     string `json:"snippet,omitempty"`
}

// cytoEdge is a Cytoscape edge element.
type cytoEdge struct {
	Data    cytoEdgeData `json:"data"`
	Classes string       `json:"classes"`
}

type cytoEdgeData struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// cytoElements is the top-level Cytoscape elements object.
type cytoElements struct {
	Nodes []cytoNode `json:"nodes"`
	Edges []cytoEdge `json:"edges"`
}

// edgeClassFor converts a mdlink.LinkKind to a Cytoscape edge class string.
func edgeClassFor(k mdlink.LinkKind) string {
	if k == mdlink.Wikilink {
		return "wikilink"
	}
	return "md-link"
}

// buildCytoElements converts the Graph to Cytoscape elements for the global view.
// Broken and external edges are skipped (no ResolvedPath).
func buildCytoElements(g *Graph) cytoElements {
	nodes := g.Nodes()
	elems := cytoElements{
		Nodes: make([]cytoNode, 0, len(nodes)),
		Edges: make([]cytoEdge, 0),
	}

	for _, n := range nodes {
		label := n
		// Use the base filename (without extension) as a more readable label.
		if idx := strings.LastIndexByte(n, '/'); idx >= 0 {
			label = n[idx+1:]
		}
		label = strings.TrimSuffix(label, ".md")

		meta := g.Meta(n)
		elems.Nodes = append(elems.Nodes, cytoNode{
			Data: cytoNodeData{
				ID:          n,
				Label:       label,
				Type:        g.NodeType(n),
				Title:       meta.Title,
				Description: meta.Description,
				Snippet:     meta.Snippet,
			},
		})
	}

	// Build edges from outbound links for every node.
	seen := make(map[string]bool) // deduplicate edges
	for _, src := range nodes {
		for _, edge := range g.Outbound(src) {
			// Skip broken links and external URLs (no resolved file in realm).
			if edge.Broken || edge.ResolvedPath == "" {
				continue
			}
			// Skip code-file links: the system graph is a page-to-page graph,
			// and a source file is not a node (it has no /page/). Emitting an
			// edge to it would reference a nonexistent target and Cytoscape
			// would abort the whole graph render. Code files surface in the
			// rail OUT list as /file/ links instead.
			if edge.CodeFile {
				continue
			}
			// Defensive: drop any edge whose target is not a known node. The
			// CodeFile check above covers the known case; this guards against
			// any future resolved-but-unwalked target crashing the client.
			if !g.Has(edge.ResolvedPath) {
				continue
			}
			// Skip self-loops.
			if src == edge.ResolvedPath {
				continue
			}
			key := fmt.Sprintf("%s→%s→%s", src, edge.ResolvedPath, edgeClassFor(edge.Kind))
			if seen[key] {
				continue
			}
			seen[key] = true
			id := fmt.Sprintf("%s→%s→%s", src, edge.ResolvedPath, edgeClassFor(edge.Kind))
			elems.Edges = append(elems.Edges, cytoEdge{
				Data: cytoEdgeData{
					ID:     id,
					Source: src,
					Target: edge.ResolvedPath,
				},
				Classes: edgeClassFor(edge.Kind),
			})
		}
	}

	return elems
}

// buildLocalSubgraph performs a BFS from the given node (both inbound and
// outbound) to depth hops and returns the induced subgraph as Cytoscape elements.
func buildLocalSubgraph(g *Graph, nodeID string, depth int) cytoElements {
	// BFS: collect nodes within depth hops.
	visited := make(map[string]bool)
	frontier := []string{nodeID}
	visited[nodeID] = true

	for hop := 0; hop < depth; hop++ {
		var next []string
		for _, cur := range frontier {
			// Outbound neighbours. Skip code-file links — they are not page
			// nodes (no /page/), so they must never enter the visited set or
			// they would render as dangling edge targets.
			for _, edge := range g.Outbound(cur) {
				if edge.Broken || edge.ResolvedPath == "" || edge.CodeFile {
					continue
				}
				nb := edge.ResolvedPath
				if !visited[nb] {
					visited[nb] = true
					next = append(next, nb)
				}
			}
			// Inbound neighbours.
			for _, src := range g.Backlinks(cur) {
				if !visited[src] {
					visited[src] = true
					next = append(next, src)
				}
			}
		}
		frontier = next
	}

	// Build elements restricted to the visited set.
	elems := cytoElements{
		Nodes: make([]cytoNode, 0, len(visited)),
		Edges: make([]cytoEdge, 0),
	}

	for n := range visited {
		label := n
		if idx := strings.LastIndexByte(n, '/'); idx >= 0 {
			label = n[idx+1:]
		}
		label = strings.TrimSuffix(label, ".md")
		meta := g.Meta(n)
		elems.Nodes = append(elems.Nodes, cytoNode{
			Data: cytoNodeData{
				ID:          n,
				Label:       label,
				Type:        g.NodeType(n),
				Title:       meta.Title,
				Description: meta.Description,
				Snippet:     meta.Snippet,
			},
		})
	}

	seen := make(map[string]bool)
	for src := range visited {
		for _, edge := range g.Outbound(src) {
			if edge.Broken || edge.ResolvedPath == "" || edge.CodeFile {
				continue
			}
			tgt := edge.ResolvedPath
			if !visited[tgt] {
				continue // edge crosses outside the subgraph
			}
			if src == tgt {
				continue
			}
			key := fmt.Sprintf("%s→%s→%s", src, tgt, edgeClassFor(edge.Kind))
			if seen[key] {
				continue
			}
			seen[key] = true
			id := fmt.Sprintf("%s_%s_%s", src, tgt, edgeClassFor(edge.Kind))
			elems.Edges = append(elems.Edges, cytoEdge{
				Data: cytoEdgeData{
					ID:     id,
					Source: src,
					Target: tgt,
				},
				Classes: edgeClassFor(edge.Kind),
			})
		}
	}

	return elems
}

// GraphDataHandler handles GET /graph/data.
// It accepts an optional ?node=<relpath>&depth=<1|2> for local views.
type GraphDataHandler struct {
	root string
	// graph is an optional pre-built link graph. When non-nil it is used directly
	// instead of rebuilding on every request (FE8: cache the graph at startup).
	// When nil, BuildLinkGraph is called per-request (original behaviour, kept for
	// NewGraphDataHandler callers that do not have a pre-built graph).
	graph *Graph
}

// NewGraphDataHandler returns an http.Handler for /graph/data that builds the
// link graph on every request. Prefer NewGraphDataHandlerWithGraph when a
// startup-built graph is available to avoid per-request latency.
func NewGraphDataHandler(root string) http.Handler {
	return &GraphDataHandler{root: root}
}

// NewGraphDataHandlerWithGraph returns an http.Handler for /graph/data that
// uses the supplied pre-built graph instead of rebuilding it on every request.
// g must not be nil. This is the preferred constructor when the caller already
// builds a link graph at startup (as serve.go does via BuildLinkGraph).
func NewGraphDataHandlerWithGraph(root string, g *Graph) http.Handler {
	return &GraphDataHandler{root: root, graph: g}
}

func (h *GraphDataHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Use the injected graph when available; fall back to per-request build.
	g := h.graph
	if g == nil {
		g = BuildLinkGraph(h.root)
	}

	// Build the provenance DAG and inject fingerprint-class edges (CP10).
	// wikiDir = <root>/wiki — the conventional wiki directory.
	wikiDir := filepath.Join(h.root, "wiki")
	provDAG := BuildProvenanceDAG(h.root, wikiDir)

	var elems cytoElements

	nodeParam := r.URL.Query().Get("node")
	if nodeParam != "" {
		depth := 2 // default depth for local view
		if dStr := r.URL.Query().Get("depth"); dStr != "" {
			if d, err := strconv.Atoi(dStr); err == nil && d > 0 {
				depth = d
			}
		}
		elems = buildLocalSubgraph(g, nodeParam, depth)
	} else {
		elems = buildCytoElements(g)
	}

	// Inject provenance nodes + fingerprint-class edges.
	injectProvenanceEdges(&elems, provDAG)

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(elems); err != nil {
		// Headers already sent.
		return
	}
}

// injectProvenanceEdges merges the ProvenanceDAG nodes and edges into the
// Cytoscape elements. Nodes not already present are added with their kind as
// type. Provenance edges carry the "fingerprint" class; drifted edges carry
// "fingerprint drift" so the graph page CSS renders them red.
func injectProvenanceEdges(elems *cytoElements, dag ProvenanceDAG) {
	// Build a set of existing node IDs to avoid duplicates.
	existing := make(map[string]bool, len(elems.Nodes))
	for _, n := range elems.Nodes {
		existing[n.Data.ID] = true
	}

	// Add provenance nodes that aren't already in the graph.
	for _, n := range dag.Nodes {
		if !existing[n.ID] {
			label := n.ID
			if idx := strings.LastIndexByte(label, '/'); idx >= 0 {
				label = label[idx+1:]
			}
			label = strings.TrimSuffix(label, ".md")
			elems.Nodes = append(elems.Nodes, cytoNode{
				Data: cytoNodeData{
					ID:    n.ID,
					Label: label,
					Type:  n.Kind,
				},
			})
			existing[n.ID] = true
		}
	}

	// Add provenance edges with "fingerprint" class (or "fingerprint drift").
	seen := make(map[string]bool)
	for _, e := range dag.Edges {
		key := fmt.Sprintf("fp:%s→%s", e.Source, e.Target)
		if seen[key] {
			continue
		}
		seen[key] = true

		classes := "fingerprint"
		if e.Drift {
			classes = "fingerprint drift"
		}
		id := fmt.Sprintf("fp:%s→%s", e.Source, e.Target)
		elems.Edges = append(elems.Edges, cytoEdge{
			Data: cytoEdgeData{
				ID:     id,
				Source: e.Source,
				Target: e.Target,
			},
			Classes: classes,
		})
	}
}
