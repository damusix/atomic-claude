// Package codectx implements the context builder and formatter (master CP19).
//
// The context builder turns a natural-language query into an agent-ready
// snapshot of the relevant portion of the code graph. It composes:
//   - search.Searcher (CP18) for multi-channel seed gathering
//   - graph.Manager (CP17) for BFS neighbor expansion
//
// # Reproducibility contract
//
// All serialisation paths sort before iterating: nodes via
// types.SubgraphSortedNodes (ascending Node.ID); edges via sortEdges
// (composite key source+target+kind); roots sorted ascending by ID.
// Go map iteration is non-deterministic — no raw map ranging in serialise paths.
//
// # Diversity caps
//
// FindRelevantContext applies two caps before returning the subgraph:
//   - DefaultMaxPerFile: max nodes from any single file_path in the gathered set.
//   - DefaultMaxPerKind: max nodes of any single node kind.
//
// When either cap drops nodes, FindRelevantContext returns truncated=true.
// Callers should pass that value via BuildOptions.Truncated so BuildContext
// marks Context.Truncated = true.
//
// # Markdown section headings (stable contract — tested)
//
//	# Context: <query>
//	## Symbols
//	## Call paths
//	## Relationships
//
// # JSON shape (stable contract — tested)
//
//	{
//	  "query":     string,
//	  "source":    string,   // "fts" | "like" | "fuzzy"
//	  "truncated": bool,
//	  "nodes":     Node[],   // sorted ascending by id
//	  "edges":     Edge[],   // sorted by source+target+kind composite key
//	  "roots":     string[]  // sorted ascending
//	}
//
// Each Edge in JSON carries "provenance" (empty string for static edges,
// "heuristic" for synthesized edges per appendix G).
package codectx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/graph"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/search"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Diversity cap constants
// ---------------------------------------------------------------------------

// DefaultMaxPerFile is the maximum number of nodes from any single file_path
// that FindRelevantContext will include in the returned subgraph.
// Prevents one large file from crowding out all other context.
const DefaultMaxPerFile = 5

// DefaultMaxPerKind is the maximum number of nodes of any single NodeKind
// that FindRelevantContext will include in the returned subgraph.
// Prevents, e.g., 30 variables swamping functions/classes.
const DefaultMaxPerKind = 8

// DefaultBFSDepth is the BFS expansion depth used when Options.BFSDepth == 0.
const DefaultBFSDepth = 2

// ---------------------------------------------------------------------------
// Format constants
// ---------------------------------------------------------------------------

// Format selects the output format for BuildContext.
type Format int

const (
	FormatMarkdown Format = iota
	FormatJSON
)

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

// Options controls FindRelevantContext behaviour.
type Options struct {
	// BFSDepth is the number of BFS hops to expand from seeds.
	// 0 uses DefaultBFSDepth.
	BFSDepth int
	// Limit caps the search tier result count. 0 uses the search default (20).
	Limit int
}

// BuildOptions controls BuildContext behaviour.
type BuildOptions struct {
	// Format selects markdown or JSON output.
	Format Format
	// Query is the original raw query string — used as the heading label.
	Query string
	// Source is the tier string returned by FindRelevantContext ("fts"/"like"/"fuzzy").
	Source string
	// Truncated, if true, marks the Context as truncated even before size
	// checking. FindRelevantContext callers pass true when diversity capping fired.
	Truncated bool
}

// ---------------------------------------------------------------------------
// Exported JSON structs (stable shape — tested)
// ---------------------------------------------------------------------------

// JSONEdge is the JSON representation of a graph edge. Provenance is always
// present (empty string for static edges, "heuristic" for synthesized edges).
type JSONEdge struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Kind       string `json:"kind"`
	Provenance string `json:"provenance"`
}

// JSONOutput is the top-level JSON document emitted by BuildContext(FormatJSON).
// All slices are sorted for reproducibility:
//   - Nodes: ascending by ID
//   - Edges: ascending by source+target+kind composite key
//   - Roots: ascending
type JSONOutput struct {
	Query     string       `json:"query"`
	Source    string       `json:"source"`
	Truncated bool         `json:"truncated"`
	Nodes     []types.Node `json:"nodes"`
	Edges     []JSONEdge   `json:"edges"`
	Roots     []string     `json:"roots"`
}

// ---------------------------------------------------------------------------
// DB interface (seam for testing)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Builder
// ---------------------------------------------------------------------------

// Builder is the entry point for CP19: context gathering and formatting.
// Create with New(db).
type Builder struct {
	mgr      *graph.Manager
	searcher *search.Searcher
}

// New creates a Builder backed by the given database. Both graph.Manager
// (CP17) and search.Searcher (CP18) are constructed from it.
func New(d *db.DB) *Builder {
	return &Builder{
		mgr:      graph.NewManager(d),
		searcher: search.New(d),
	}
}

// ---------------------------------------------------------------------------
// FindRelevantContext
// ---------------------------------------------------------------------------

// FindRelevantContext gathers a relevant subgraph for query by:
//  1. Searching for seed nodes via search.Searcher.Search.
//  2. Looking up any exact-name matches not caught by search (exact-symbol channel).
//  3. BFS-expanding seeds via graph.Manager along calls/references/extends/implements.
//  4. Applying diversity caps (MaxPerFile, MaxPerKind) to avoid one file/kind dominating.
//
// Returns the capped Subgraph, the source tier string ("fts"/"like"/"fuzzy"),
// a truncated bool (true when diversity caps dropped content), and any error.
// Callers pass the truncated bool via BuildOptions.Truncated to BuildContext.
func (b *Builder) FindRelevantContext(ctx context.Context, query string, opts Options) (types.Subgraph, string, bool, error) {
	depth := opts.BFSDepth
	if depth <= 0 {
		depth = DefaultBFSDepth
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	// -----------------------------------------------------------------------
	// Channel A: search.Searcher
	// -----------------------------------------------------------------------
	results, tier, err := b.searcher.Search(ctx, types.SearchOptions{
		Query: query,
		Limit: limit,
	})
	if err != nil {
		return types.Subgraph{}, "", false, fmt.Errorf("codectx: search: %w", err)
	}

	tierStr := tier.String() // "fts", "like", or "fuzzy"

	// Union seeds, dedup by node ID.
	seeds := make(map[string]types.Node)
	for _, r := range results {
		seeds[r.Node.ID] = r.Node
	}

	// -----------------------------------------------------------------------
	// Channel B: exact-name resolution
	// -----------------------------------------------------------------------
	// Extract the bare query text (strip field: tokens) for exact matching.
	pq := search.ParseQuery(query)
	nameTarget := pq.FTSText
	if nameTarget == "" {
		nameTarget = query
	}
	nameTarget = strings.TrimSpace(nameTarget)
	// We already have search results; exact-name channel adds nodes whose Name
	// matches exactly but may have been missed by FTS tokenisation (e.g. short
	// or special-character names). Search with name: filter.
	if nameTarget != "" {
		nameResults, _, nameErr := b.searcher.Search(ctx, types.SearchOptions{
			Query: "name:" + nameTarget,
			Limit: limit,
		})
		// Best-effort graceful degradation: the exact-name channel is additive;
		// if it errors, the primary search results are still used as seeds.
		if nameErr == nil {
			for _, r := range nameResults {
				if _, ok := seeds[r.Node.ID]; !ok {
					seeds[r.Node.ID] = r.Node
				}
			}
		}
	}

	if len(seeds) == 0 {
		// No seeds: return empty subgraph with tier.
		return types.Subgraph{Nodes: make(map[string]types.Node)}, tierStr, false, nil
	}

	// -----------------------------------------------------------------------
	// BFS expansion from all seeds
	// -----------------------------------------------------------------------
	// The subgraph accumulates all visited nodes + edges across all seed expansions.
	// nodeDepth tracks the minimum BFS distance from any seed for each node.
	// Seeds have depth 0; BFS neighbors get depth 1 (outgoing callee path first,
	// then incoming caller path). Depth is used for diversity-cap priority so that
	// closer nodes survive capping over distant ones.
	combined := types.Subgraph{
		Nodes: make(map[string]types.Node),
	}
	nodeDepth := make(map[string]int) // node ID → min BFS distance from any seed

	// Seed roots at depth 0.
	for id, n := range seeds {
		combined.Nodes[id] = n
		nodeDepth[id] = 0
		combined.Roots = append(combined.Roots, id)
	}
	sort.Strings(combined.Roots)

	// F-55: iterate seeds in sorted order so BFS expansion is deterministic.
	sortedSeedIDs := make([]string, 0, len(seeds))
	for id := range seeds {
		sortedSeedIDs = append(sortedSeedIDs, id)
	}
	sort.Strings(sortedSeedIDs)

	for _, seedID := range sortedSeedIDs {
		// Use GetCallees for outgoing calls/references context.
		// Callees are assigned depth 1 (direct dependency context — the seed
		// calls/uses them). In diversity capping, callees are preferred over
		// callers because they represent what the queried symbol depends on.
		calleeSG, err := b.mgr.GetCallees(ctx, seedID, depth)
		// Best-effort graceful degradation: BFS callee expansion is additive context;
		// if it errors for a seed, we proceed with whatever seeds we have.
		if err == nil {
			for id, n := range calleeSG.Nodes {
				combined.Nodes[id] = n
				if _, ok := nodeDepth[id]; !ok {
					nodeDepth[id] = 1 // callee priority
				}
			}
			combined.Edges = append(combined.Edges, calleeSG.Edges...)
		}

		// Use GetCallers for incoming calls/references context.
		// Callers are assigned depth 2: they are important for impact context
		// but subordinate to direct callees in diversity-cap priority.
		callerSG, err := b.mgr.GetCallers(ctx, seedID, depth)
		// Best-effort graceful degradation: BFS caller expansion is additive context;
		// if it errors for a seed, we proceed with callee results already gathered.
		if err == nil {
			for id, n := range callerSG.Nodes {
				combined.Nodes[id] = n
				if _, ok := nodeDepth[id]; !ok {
					nodeDepth[id] = 2 // caller priority (lower than callee)
				}
			}
			combined.Edges = append(combined.Edges, callerSG.Edges...)
		}
	}

	// Dedup edges by composite key (source+target+kind).
	combined.Edges = deduplicateEdges(combined.Edges)

	// -----------------------------------------------------------------------
	// Diversity caps
	// -----------------------------------------------------------------------
	truncated := false

	// Count nodes per file and per kind.
	fileCount := make(map[string]int)
	kindCount := make(map[types.NodeKind]int)
	for _, n := range combined.Nodes {
		fileCount[n.FilePath]++
		kindCount[n.Kind]++
	}

	// Determine which files/kinds exceed their cap.
	fileCapped := make(map[string]bool)
	kindCapped := make(map[types.NodeKind]bool)
	for fp, cnt := range fileCount {
		if cnt > DefaultMaxPerFile {
			fileCapped[fp] = true
		}
	}
	for k, cnt := range kindCount {
		if cnt > DefaultMaxPerKind {
			kindCapped[k] = true
		}
	}

	if len(fileCapped) > 0 || len(kindCapped) > 0 {
		// Apply caps: for each capped group, keep only the top-N nodes.
		// Priority order: (depth asc, ID asc) — closer nodes to seeds survive.
		// This ensures seeds (depth=0) and their direct BFS neighbors (depth=1)
		// are preferred over more distant accumulated nodes.
		allNodes := types.SubgraphSortedNodes(combined) // sorted by ID first
		sort.SliceStable(allNodes, func(i, j int) bool {
			di := nodeDepth[allNodes[i].ID]
			dj := nodeDepth[allNodes[j].ID]
			if di != dj {
				return di < dj // closer to seed wins
			}
			return allNodes[i].ID < allNodes[j].ID // stable tiebreak by ID
		})

		kept := make(map[string]types.Node)
		fileUsed := make(map[string]int)
		kindUsed := make(map[types.NodeKind]int)

		for _, n := range allNodes {
			isSeed := nodeDepth[n.ID] == 0
			if isSeed {
				// Seeds always kept regardless of cap.
				kept[n.ID] = n
				fileUsed[n.FilePath]++
				kindUsed[n.Kind]++
				continue
			}
			// Check caps.
			if fileCapped[n.FilePath] && fileUsed[n.FilePath] >= DefaultMaxPerFile {
				truncated = true
				continue
			}
			if kindCapped[n.Kind] && kindUsed[n.Kind] >= DefaultMaxPerKind {
				truncated = true
				continue
			}
			kept[n.ID] = n
			fileUsed[n.FilePath]++
			kindUsed[n.Kind]++
		}

		// Filter edges to only those where both endpoints are in kept.
		var keptEdges []types.Edge
		for _, e := range combined.Edges {
			if _, srcOK := kept[e.Source]; !srcOK {
				continue
			}
			if _, tgtOK := kept[e.Target]; !tgtOK {
				continue
			}
			keptEdges = append(keptEdges, e)
		}
		combined.Nodes = kept
		combined.Edges = keptEdges
	}

	return combined, tierStr, truncated, nil
}

// ---------------------------------------------------------------------------
// BuildContext
// ---------------------------------------------------------------------------

// BuildContext renders the gathered subgraph into the chosen format and
// returns a types.Context ready for an AI agent. Sets NodeCount, EdgeCount,
// Source, and Truncated.
//
// Context.Truncated is set to true when opts.Truncated is true (which callers
// set from the bool returned by FindRelevantContext when diversity capping fired).
func (b *Builder) BuildContext(ctx context.Context, sg types.Subgraph, opts BuildOptions) (types.Context, error) {
	truncated := opts.Truncated

	var content string
	var err error
	switch opts.Format {
	case FormatMarkdown:
		content, err = formatMarkdown(sg, opts.Query, opts.Source)
	case FormatJSON:
		content, err = formatJSON(sg, opts.Query, opts.Source, truncated)
	default:
		content, err = formatMarkdown(sg, opts.Query, opts.Source)
	}
	if err != nil {
		return types.Context{}, fmt.Errorf("codectx: BuildContext: %w", err)
	}

	return types.Context{
		Content:   content,
		Truncated: truncated,
		Source:    opts.Source,
		NodeCount: len(sg.Nodes),
		EdgeCount: len(sg.Edges),
	}, nil
}

// ---------------------------------------------------------------------------
// Markdown formatter
// ---------------------------------------------------------------------------

// formatMarkdown renders the subgraph as markdown with stable section headings:
//
//	# Context: <query>
//	## Symbols
//	## Call paths
//	## Relationships
//
// Section headings are the tested contract; their order and exact text must not
// change without updating the tests.
//
// Heuristic edges (Provenance=="heuristic") are marked with "(heuristic)" in
// the Relationships section per appendix G.
func formatMarkdown(sg types.Subgraph, query, source string) (string, error) {
	var b bytes.Buffer

	// Section 1: title
	fmt.Fprintf(&b, "# Context: %s\n\n", query)
	if source != "" {
		fmt.Fprintf(&b, "_Source: %s_\n\n", source)
	}

	// Section 2: Symbols
	// Nodes grouped by file_path then kind then name (stable: sorted by ID).
	fmt.Fprintln(&b, "## Symbols")
	b.WriteString("\n")
	nodes := types.SubgraphSortedNodes(sg) // sorted ascending by Node.ID
	if len(nodes) == 0 {
		fmt.Fprintln(&b, "_No symbols found._")
	} else {
		// Group by file for readability; process in sorted order.
		// Build a map of filePath → []Node, then sort file paths.
		byFile := make(map[string][]Node)
		for _, n := range nodes {
			byFile[n.FilePath] = append(byFile[n.FilePath], n)
		}
		filePaths := make([]string, 0, len(byFile))
		for fp := range byFile {
			filePaths = append(filePaths, fp)
		}
		sort.Strings(filePaths)
		for _, fp := range filePaths {
			fileNodes := byFile[fp]
			// Already in ID-order from SubgraphSortedNodes iteration above;
			// secondary sort by kind then name for readability within a file.
			sort.Slice(fileNodes, func(i, j int) bool {
				if fileNodes[i].Kind != fileNodes[j].Kind {
					return fileNodes[i].Kind < fileNodes[j].Kind
				}
				return fileNodes[i].Name < fileNodes[j].Name
			})
			fmt.Fprintf(&b, "### %s\n\n", fp)
			for _, n := range fileNodes {
				sig := n.Signature
				if sig == "" {
					sig = fmt.Sprintf("%s %s", n.Kind, n.Name)
				}
				fmt.Fprintf(&b, "- **%s** `%s` (%s:%d)\n", n.Name, sig, n.FilePath, n.StartLine)
			}
			b.WriteString("\n")
		}
	}

	// Section 3: Call paths
	fmt.Fprintln(&b, "## Call paths")
	b.WriteString("\n")
	callEdges := edgesOfKind(sg.Edges, types.EdgeKindCalls)
	sortEdges(callEdges)
	if len(callEdges) == 0 {
		fmt.Fprintln(&b, "_No call paths in gathered subgraph._")
	} else {
		// Render chains: find representative chains by following calls.
		chains := buildCallChains(sg, callEdges)
		for _, chain := range chains {
			fmt.Fprintf(&b, "- %s\n", strings.Join(chain, " → "))
		}
	}
	b.WriteString("\n")

	// Section 4: Relationships
	fmt.Fprintln(&b, "## Relationships")
	b.WriteString("\n")
	allEdges := make([]types.Edge, len(sg.Edges))
	copy(allEdges, sg.Edges)
	sortEdges(allEdges)
	if len(allEdges) == 0 {
		fmt.Fprintln(&b, "_No edges in gathered subgraph._")
	} else {
		for _, e := range allEdges {
			line := fmt.Sprintf("- %s → %s (%s)", nodeName(sg, e.Source), nodeName(sg, e.Target), e.Kind)
			if e.Provenance == "heuristic" {
				line += " (heuristic)"
			}
			fmt.Fprintln(&b, line)
		}
	}

	return b.String(), nil
}

// buildCallChains finds representative call chains among the gathered nodes by
// following calls edges. Returns chains of node names (longest / starting from
// nodes with no incoming call edges in the subgraph). Deterministic: stable
// sort on chain start node ID.
func buildCallChains(sg types.Subgraph, callEdges []types.Edge) [][]string {
	if len(callEdges) == 0 {
		return nil
	}

	// Build adjacency (source → []target) and in-degree within subgraph.
	adj := make(map[string][]string)
	hasIncoming := make(map[string]bool)
	for _, e := range callEdges {
		if _, ok := sg.Nodes[e.Source]; !ok {
			continue
		}
		if _, ok := sg.Nodes[e.Target]; !ok {
			continue
		}
		adj[e.Source] = append(adj[e.Source], e.Target)
		hasIncoming[e.Target] = true
	}

	// Sort adjacency lists for determinism.
	for src := range adj {
		sort.Strings(adj[src])
	}

	// Find roots: nodes in the subgraph with outgoing calls but no incoming calls.
	var roots []string
	for src := range adj {
		if !hasIncoming[src] {
			roots = append(roots, src)
		}
	}
	sort.Strings(roots)

	// DFS from each root to build chains (depth-limited to avoid explosion).
	const maxChainDepth = 6
	var chains [][]string
	seen := make(map[string]bool)

	var dfs func(id string, path []string)
	dfs = func(id string, path []string) {
		if len(path) > maxChainDepth {
			chains = append(chains, appendNodeNames(sg, path))
			return
		}
		nexts := adj[id]
		if len(nexts) == 0 {
			chains = append(chains, appendNodeNames(sg, path))
			return
		}
		for _, next := range nexts {
			if seen[next] {
				// Cycle: emit the path and stop this branch.
				chains = append(chains, appendNodeNames(sg, append(path, next)))
				continue
			}
			seen[next] = true
			dfs(next, append(path, next))
			seen[next] = false
		}
	}

	for _, root := range roots {
		seen[root] = true
		dfs(root, []string{root})
		seen[root] = false
	}

	return chains
}

// nodeName resolves a node ID (the graph's foreign key) to its human-readable
// name, falling back to the raw ID when the node is absent from the subgraph or
// has no name, so rendered output is never blank.
func nodeName(sg types.Subgraph, id string) string {
	if n, ok := sg.Nodes[id]; ok && n.Name != "" {
		return n.Name
	}
	return id
}

func appendNodeNames(sg types.Subgraph, ids []string) []string {
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = nodeName(sg, id)
	}
	return names
}

// ---------------------------------------------------------------------------
// JSON formatter
// ---------------------------------------------------------------------------

// formatJSON renders the subgraph as deterministic JSON per the documented shape.
func formatJSON(sg types.Subgraph, query, source string, truncated bool) (string, error) {
	nodes := types.SubgraphSortedNodes(sg) // ascending by ID

	edges := make([]JSONEdge, 0, len(sg.Edges))
	rawEdges := make([]types.Edge, len(sg.Edges))
	copy(rawEdges, sg.Edges)
	sortEdges(rawEdges)
	for _, e := range rawEdges {
		edges = append(edges, JSONEdge{
			Source:     e.Source,
			Target:     e.Target,
			Kind:       string(e.Kind),
			Provenance: e.Provenance,
		})
	}

	roots := make([]string, len(sg.Roots))
	copy(roots, sg.Roots)
	sort.Strings(roots)

	out := JSONOutput{
		Query:     query,
		Source:    source,
		Truncated: truncated,
		Nodes:     nodes,
		Edges:     edges,
		Roots:     roots,
	}

	data, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("codectx: json.Marshal: %w", err)
	}
	return string(data), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// Node is a local alias — defined here to avoid import cycle in byFile grouping.
type Node = types.Node

// sortEdges sorts edges in-place by composite key: source + "\x00" + target + "\x00" + kind.
// This is the stable key used for JSON serialisation and markdown rendering.
func sortEdges(edges []types.Edge) {
	sort.SliceStable(edges, func(i, j int) bool {
		ki := edges[i].Source + "\x00" + edges[i].Target + "\x00" + string(edges[i].Kind)
		kj := edges[j].Source + "\x00" + edges[j].Target + "\x00" + string(edges[j].Kind)
		return ki < kj
	})
}

// deduplicateEdges removes duplicate edges (same source+target+kind) from a
// slice. When the same logical edge (source+target+kind) appears with both empty
// and "heuristic" provenance, the heuristic one is kept so the low-confidence
// marker survives (appendix G). This handles both orderings: heuristic-first and
// static-first. If both occurrences have the same provenance, the first wins.
func deduplicateEdges(edges []types.Edge) []types.Edge {
	type key struct {
		src, tgt string
		kind     types.EdgeKind
	}
	// First pass: record the winning provenance for each logical edge.
	// "heuristic" beats empty; among equal provenances, first occurrence wins.
	best := make(map[key]types.Edge, len(edges))
	for _, e := range edges {
		k := key{e.Source, e.Target, e.Kind}
		prev, exists := best[k]
		if !exists {
			best[k] = e
			continue
		}
		// Heuristic provenance wins over empty (static) regardless of arrival order.
		if e.Provenance == "heuristic" && prev.Provenance != "heuristic" {
			best[k] = e
		}
	}
	// Second pass: emit edges in original order, using the best (winning) edge
	// for each logical key and skipping subsequent occurrences.
	emitted := make(map[key]bool, len(edges))
	out := make([]types.Edge, 0, len(best))
	for _, e := range edges {
		k := key{e.Source, e.Target, e.Kind}
		if emitted[k] {
			continue
		}
		emitted[k] = true
		out = append(out, best[k]) // emit the winning edge, not necessarily this one
	}
	return out
}

// edgesOfKind filters edges to those of the given kind.
func edgesOfKind(edges []types.Edge, kind types.EdgeKind) []types.Edge {
	var out []types.Edge
	for _, e := range edges {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}
