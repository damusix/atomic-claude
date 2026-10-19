// /graph/data: the realm link graph as Cytoscape elements JSON. With no
// ?node= it emits every node and resolved edge; with ?node=&depth= it emits
// the BFS neighbourhood, inbound and outbound, defaulting to depth 2.
//
// Edges carry one of two classes the client styles on: "md-link" and
// "wikilink".
package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	// cache holds the full-view assembly — element build and marshal — keyed
	// by filesystem fingerprint. Nil alongside a nil store.
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
	cache := newGraphDataCache(store)
	// Warm in the background so the first graph open does not wait on the
	// whole-realm assembly.
	go cache.warm()
	return &GraphDataHandler{root: root, store: store, cache: cache}
}

func (h *GraphDataHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	nodeParam := r.URL.Query().Get("node")

	// The full view covers the whole realm, so it is cached per realm state;
	// an assemble error falls through to the live path.
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

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(elems); err != nil {
		return // headers already sent
	}
}
