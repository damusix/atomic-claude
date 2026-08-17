// Package codectx turns a natural-language query into an agent-ready snapshot
// of the relevant portion of the code graph.
//
// Every serialisation path sorts before iterating (nodes by ID, edges by
// source+target+kind, roots ascending): Go map order is non-deterministic and
// the emitted markdown headings and JSON shape are a tested contract.
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

// DefaultMaxPerFile caps nodes drawn from any one file, so a single large file
// cannot crowd out the rest of the context.
const DefaultMaxPerFile = 5

// DefaultMaxPerKind caps nodes of any one NodeKind, so e.g. 30 variables cannot
// swamp the functions and classes.
const DefaultMaxPerKind = 8

// DefaultBFSDepth is used when Options.BFSDepth == 0.
const DefaultBFSDepth = 2

// Format selects the output format for BuildContext.
type Format int

const (
	FormatMarkdown Format = iota
	FormatJSON
)

// Options controls FindRelevantContext behaviour.
type Options struct {
	// BFSDepth is hops to expand from seeds; 0 uses DefaultBFSDepth.
	BFSDepth int
	// Limit caps the search tier result count; 0 uses the search default (20).
	Limit int
}

// BuildOptions controls BuildContext behaviour.
type BuildOptions struct {
	Format Format
	Query  string
	// Source is the tier string returned by FindRelevantContext.
	Source string
	// Truncated marks the Context truncated before size checking; callers pass
	// through the bool FindRelevantContext returns when diversity capping fired.
	Truncated bool
}

// JSONEdge is a graph edge as serialised. Provenance is always present: empty
// for static edges, "heuristic" for synthesized ones.
type JSONEdge struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Kind       string `json:"kind"`
	Provenance string `json:"provenance"`
}

// JSONOutput is the document emitted by BuildContext(FormatJSON). Every slice
// is sorted for reproducibility.
type JSONOutput struct {
	Query     string       `json:"query"`
	Source    string       `json:"source"`
	Truncated bool         `json:"truncated"`
	Nodes     []types.Node `json:"nodes"`
	Edges     []JSONEdge   `json:"edges"`
	Roots     []string     `json:"roots"`
}

// Builder is the entry point for context gathering and formatting.
type Builder struct {
	mgr      *graph.Manager
	searcher *search.Searcher
}

// New creates a Builder backed by the given database.
func New(d *db.DB) *Builder {
	return &Builder{
		mgr:      graph.NewManager(d),
		searcher: search.New(d),
	}
}

// FindRelevantContext searches for seed nodes, BFS-expands them, and applies
// the diversity caps. Returns the capped subgraph, the search tier string, and
// whether capping dropped content (pass through to BuildOptions.Truncated).
func (b *Builder) FindRelevantContext(ctx context.Context, query string, opts Options) (types.Subgraph, string, bool, error) {
	depth := opts.BFSDepth
	if depth <= 0 {
		depth = DefaultBFSDepth
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	results, tier, err := b.searcher.Search(ctx, types.SearchOptions{
		Query: query,
		Limit: limit,
	})
	if err != nil {
		return types.Subgraph{}, "", false, fmt.Errorf("codectx: search: %w", err)
	}

	tierStr := tier.String()

	seeds := make(map[string]types.Node)
	for _, r := range results {
		seeds[r.Node.ID] = r.Node
	}

	pq := search.ParseQuery(query)
	nameTarget := pq.FTSText
	if nameTarget == "" {
		nameTarget = query
	}
	nameTarget = strings.TrimSpace(nameTarget)
	// Second channel: exact-name matches FTS tokenisation misses (short or
	// special-character names). Purely additive, so an error here is ignored.
	if nameTarget != "" {
		nameResults, _, nameErr := b.searcher.Search(ctx, types.SearchOptions{
			Query: "name:" + nameTarget,
			Limit: limit,
		})
		if nameErr == nil {
			for _, r := range nameResults {
				if _, ok := seeds[r.Node.ID]; !ok {
					seeds[r.Node.ID] = r.Node
				}
			}
		}
	}

	if len(seeds) == 0 {
		return types.Subgraph{Nodes: make(map[string]types.Node)}, tierStr, false, nil
	}

	combined := types.Subgraph{
		Nodes: make(map[string]types.Node),
	}
	// Min BFS distance from any seed, used as diversity-cap priority: closer
	// nodes survive capping over distant ones.
	nodeDepth := make(map[string]int)

	for id, n := range seeds {
		combined.Nodes[id] = n
		nodeDepth[id] = 0
		combined.Roots = append(combined.Roots, id)
	}
	sort.Strings(combined.Roots)

	// Sorted so BFS expansion order is deterministic.
	sortedSeedIDs := make([]string, 0, len(seeds))
	for id := range seeds {
		sortedSeedIDs = append(sortedSeedIDs, id)
	}
	sort.Strings(sortedSeedIDs)

	for _, seedID := range sortedSeedIDs {
		// Callees rank above callers under capping: they are what the queried
		// symbol depends on. Expansion is additive, so per-seed errors are ignored.
		calleeSG, err := b.mgr.GetCallees(ctx, seedID, depth)
		if err == nil {
			for id, n := range calleeSG.Nodes {
				combined.Nodes[id] = n
				if _, ok := nodeDepth[id]; !ok {
					nodeDepth[id] = 1
				}
			}
			combined.Edges = append(combined.Edges, calleeSG.Edges...)
		}

		callerSG, err := b.mgr.GetCallers(ctx, seedID, depth)
		if err == nil {
			for id, n := range callerSG.Nodes {
				combined.Nodes[id] = n
				if _, ok := nodeDepth[id]; !ok {
					nodeDepth[id] = 2
				}
			}
			combined.Edges = append(combined.Edges, callerSG.Edges...)
		}
	}

	combined.Edges = deduplicateEdges(combined.Edges)

	truncated := false

	fileCount := make(map[string]int)
	kindCount := make(map[types.NodeKind]int)
	for _, n := range combined.Nodes {
		fileCount[n.FilePath]++
		kindCount[n.Kind]++
	}

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
		// Keep the top-N of each capped group by (depth asc, ID asc), so seeds
		// and their direct neighbors survive over distant accumulated nodes.
		allNodes := types.SubgraphSortedNodes(combined)
		sort.SliceStable(allNodes, func(i, j int) bool {
			di := nodeDepth[allNodes[i].ID]
			dj := nodeDepth[allNodes[j].ID]
			if di != dj {
				return di < dj
			}
			return allNodes[i].ID < allNodes[j].ID
		})

		kept := make(map[string]types.Node)
		fileUsed := make(map[string]int)
		kindUsed := make(map[types.NodeKind]int)

		for _, n := range allNodes {
			isSeed := nodeDepth[n.ID] == 0
			if isSeed {
				// Seeds are exempt from the caps.
				kept[n.ID] = n
				fileUsed[n.FilePath]++
				kindUsed[n.Kind]++
				continue
			}
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

// BuildContext renders the gathered subgraph into the chosen format.
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

// formatMarkdown renders the subgraph as markdown. The section headings and
// their order are a tested contract — changing either breaks the tests.
func formatMarkdown(sg types.Subgraph, query, source string) (string, error) {
	var b bytes.Buffer

	fmt.Fprintf(&b, "# Context: %s\n\n", query)
	if source != "" {
		fmt.Fprintf(&b, "_Source: %s_\n\n", source)
	}

	fmt.Fprintln(&b, "## Symbols")
	b.WriteString("\n")
	nodes := types.SubgraphSortedNodes(sg)
	if len(nodes) == 0 {
		fmt.Fprintln(&b, "_No symbols found._")
	} else {
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

	fmt.Fprintln(&b, "## Call paths")
	b.WriteString("\n")
	callEdges := edgesOfKind(sg.Edges, types.EdgeKindCalls)
	sortEdges(callEdges)
	if len(callEdges) == 0 {
		fmt.Fprintln(&b, "_No call paths in gathered subgraph._")
	} else {
		chains := buildCallChains(sg, callEdges)
		for _, chain := range chains {
			fmt.Fprintf(&b, "- %s\n", strings.Join(chain, " → "))
		}
	}
	b.WriteString("\n")

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

// buildCallChains walks calls edges from each node with no incoming call inside
// the subgraph, returning chains of node names.
func buildCallChains(sg types.Subgraph, callEdges []types.Edge) [][]string {
	if len(callEdges) == 0 {
		return nil
	}

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

	// Sorted for determinism.
	for src := range adj {
		sort.Strings(adj[src])
	}

	var roots []string
	for src := range adj {
		if !hasIncoming[src] {
			roots = append(roots, src)
		}
	}
	sort.Strings(roots)

	// Depth-limited to keep the DFS from exploding on dense graphs.
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
				// Cycle: emit and stop this branch.
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

// nodeName resolves a node ID to its display name, falling back to the raw ID
// so rendered output is never blank.
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

func formatJSON(sg types.Subgraph, query, source string, truncated bool) (string, error) {
	nodes := types.SubgraphSortedNodes(sg)

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

type Node = types.Node

// sortEdges imposes the source+target+kind ordering that both the JSON and
// markdown renderers rely on for reproducible output.
func sortEdges(edges []types.Edge) {
	sort.SliceStable(edges, func(i, j int) bool {
		ki := edges[i].Source + "\x00" + edges[i].Target + "\x00" + string(edges[i].Kind)
		kj := edges[j].Source + "\x00" + edges[j].Target + "\x00" + string(edges[j].Kind)
		return ki < kj
	})
}

// deduplicateEdges collapses edges sharing source+target+kind. Where the same
// logical edge arrives with both empty and "heuristic" provenance, the
// heuristic one wins in either arrival order so the low-confidence marker
// survives; otherwise the first occurrence wins.
func deduplicateEdges(edges []types.Edge) []types.Edge {
	type key struct {
		src, tgt string
		kind     types.EdgeKind
	}
	best := make(map[key]types.Edge, len(edges))
	for _, e := range edges {
		k := key{e.Source, e.Target, e.Kind}
		prev, exists := best[k]
		if !exists {
			best[k] = e
			continue
		}
		if e.Provenance == "heuristic" && prev.Provenance != "heuristic" {
			best[k] = e
		}
	}
	// Emit in original order, one entry per key.
	emitted := make(map[key]bool, len(edges))
	out := make([]types.Edge, 0, len(best))
	for _, e := range edges {
		k := key{e.Source, e.Target, e.Kind}
		if emitted[k] {
			continue
		}
		emitted[k] = true
		out = append(out, best[k])
	}
	return out
}

func edgesOfKind(edges []types.Edge, kind types.EdgeKind) []types.Edge {
	var out []types.Edge
	for _, e := range edges {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}
