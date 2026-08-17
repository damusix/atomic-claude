// /graph/data: the realm link graph as Cytoscape elements JSON. With no
// ?node= it emits every node and resolved edge; with ?node=&depth= it emits
// the BFS neighbourhood, inbound and outbound, defaulting to depth 2.
//
// Edges carry one of three classes the client styles on: "md-link",
// "wikilink", and "fingerprint" for provenance.
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

type cytoEdge struct {
	Data    cytoEdgeData `json:"data"`
	Classes string       `json:"classes"`
}

type cytoEdgeData struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type cytoElements struct {
	Nodes []cytoNode `json:"nodes"`
	Edges []cytoEdge `json:"edges"`
}

func edgeClassFor(k mdlink.LinkKind) string {
	if k == mdlink.Wikilink {
		return "wikilink"
	}
	return "md-link"
}

// buildCytoElements emits the whole graph, skipping broken and external edges.
func buildCytoElements(g *Graph) cytoElements {
	nodes := g.Nodes()
	elems := cytoElements{
		Nodes: make([]cytoNode, 0, len(nodes)),
		Edges: make([]cytoEdge, 0),
	}

	for _, n := range nodes {
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
	for _, src := range nodes {
		for _, edge := range g.Outbound(src) {
			if edge.Broken || edge.ResolvedPath == "" {
				continue
			}
			// This is a page-to-page graph; a source file has no page node, so
			// an edge to one would dangle and Cytoscape drops the entire
			// element set on a dangling reference.
			if edge.CodeFile {
				continue
			}
			if !g.Has(edge.ResolvedPath) {
				continue
			}
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

// buildLocalSubgraph returns the subgraph induced by a BFS from nodeID, in
// both directions, out to depth hops.
func buildLocalSubgraph(g *Graph, nodeID string, depth int) cytoElements {
	visited := make(map[string]bool)
	frontier := []string{nodeID}
	visited[nodeID] = true

	for hop := 0; hop < depth; hop++ {
		var next []string
		for _, cur := range frontier {
			// Code files are not page nodes, so they must never enter the
			// visited set or they become dangling edge targets.
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
			for _, src := range g.Backlinks(cur) {
				if !visited[src] {
					visited[src] = true
					next = append(next, src)
				}
			}
		}
		frontier = next
	}

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
				continue // leaves the subgraph
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

// GraphDataHandler serves GET /graph/data, optionally scoped by ?node=&depth=.
type GraphDataHandler struct {
	root string
	// store is nil for the per-request constructor, which rebuilds the graph
	// on every request instead of tracking realm changes.
	store *snapshotStore
	// cache holds the full-view assembly — provenance walk, element build,
	// marshal — keyed by filesystem fingerprint. Nil alongside a nil store.
	cache *graphDataCache
}

// NewGraphDataHandler builds the link graph on every request. Prefer
// NewGraphDataHandlerWithGraph when a shared store is available.
func NewGraphDataHandler(root string) http.Handler {
	return &GraphDataHandler{root: root}
}

// NewGraphDataHandlerWithGraph reads through store, so a realm change on disk
// is reflected on the next request rather than at the next restart. store must
// not be nil.
func NewGraphDataHandlerWithGraph(root string, store *snapshotStore) http.Handler {
	cache := newGraphDataCache(root, store)
	// Warm in the background so the first graph open does not wait on the
	// provenance walk.
	go cache.warm()
	return &GraphDataHandler{root: root, store: store, cache: cache}
}

func (h *GraphDataHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	nodeParam := r.URL.Query().Get("node")

	// The full view runs a provenance walk over the whole realm, so it is
	// cached per realm state; an assemble error falls through to the live path.
	if nodeParam == "" && h.cache != nil {
		if b, fp, err := h.cache.fullJSON(); err == nil {
			w.Header().Set("Content-Type", "application/json")
			// The client keys its IndexedDB layout cache off this.
			if fp != "" {
				w.Header().Set("X-Graph-Fingerprint", fp)
			}
			_, _ = w.Write(b)
			return
		}
	}

	var g *Graph
	if h.store != nil {
		g = h.store.currentGraph()
	} else {
		g = BuildLinkGraph(h.root)
	}

	wikiDir := filepath.Join(h.root, "wiki")
	provDAG := BuildProvenanceDAG(h.root, wikiDir)

	var elems cytoElements

	if nodeParam != "" {
		depth := 2
		if dStr := r.URL.Query().Get("depth"); dStr != "" {
			if d, err := strconv.Atoi(dStr); err == nil && d > 0 {
				depth = d
			}
		}
		elems = buildLocalSubgraph(g, nodeParam, depth)
	} else {
		elems = buildCytoElements(g)
	}

	injectProvenanceEdges(&elems, provDAG, nodeParam != "")

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(elems); err != nil {
		return // headers already sent
	}
}

// injectProvenanceEdges merges the provenance DAG into the elements. Drifted
// edges get an extra class so the client can colour them.
//
// A scoped call may only decorate what is already present — no new nodes, and
// only edges with both endpoints in scope. Injecting the whole realm DAG into
// a one-node neighbourhood filled the rail mini-graph with unrelated clusters
// that had no path to the page being viewed.
func injectProvenanceEdges(elems *cytoElements, dag ProvenanceDAG, scoped bool) {
	existing := make(map[string]bool, len(elems.Nodes))
	for _, n := range elems.Nodes {
		existing[n.Data.ID] = true
	}

	for _, n := range dag.Nodes {
		if scoped {
			break
		}
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

	seen := make(map[string]bool)
	for _, e := range dag.Edges {
		// Cytoscape drops the whole element set on one dangling reference.
		if scoped && (!existing[e.Source] || !existing[e.Target]) {
			continue
		}
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
