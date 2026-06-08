// Package mcp implements the atomic code MCP server (master CP22).
//
// # Server construction
//
// NewServer creates a configured *mcp.Server bound to the given engine. The
// server is transport-agnostic: CP23 can run it over a unix-socket transport;
// RunStdio runs it over stdio.
//
// # Tool gating (tiny-repo)
//
// When fileCount < 500 only three tools are registered: atomic_code_explore,
// atomic_code_search, and atomic_code_node. Larger repos get all 8.
//
// # Explore budget constants
//
// The budget tiers in this package are copied exactly from appendix K and are
// asserted by tests. A table-driven test checks every literal and the
// monotonic-non-decreasing invariant on maxCharsPerFile.
//
// # Server instructions
//
// The initialize instructions text is the single source of truth for agent
// guidance. It is stored in serverInstructions (below) and returned as the
// instructions field of the initialize response. It is de-branded: no
// reference product name; only "atomic" and "atomic_code_*" tool names.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/codectx"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

// ---------------------------------------------------------------------------
// Explore budget constants (appendix K — COPY EXACTLY; asserted by tests)
// ---------------------------------------------------------------------------

// ExploreOutputBudget is the per-call output budget returned by GetExploreOutputBudget.
// Fields are exported so tests can assert exact appendix-K values.
type ExploreOutputBudget struct {
	MaxOutputChars       int
	DefaultMaxFiles      int
	MaxCharsPerFile      int
	GapThreshold         int
	ExcludeLowValueFiles bool
}

// GetExploreBudget returns the number of allowed explore calls based on
// project size. Copied exactly from appendix K.
//
//	<500  → 1
//	<5000 → 2
//	<15000 → 3
//	<25000 → 4
//	≥25000 → 5
func GetExploreBudget(fileCount int) int {
	switch {
	case fileCount < 500:
		return 1
	case fileCount < 5000:
		return 2
	case fileCount < 15000:
		return 3
	case fileCount < 25000:
		return 4
	default:
		return 5
	}
}

// GetExploreOutputBudget returns the output budget for the given file count.
// Tiers copied exactly from appendix K.
func GetExploreOutputBudget(fileCount int) ExploreOutputBudget {
	switch {
	case fileCount < 150:
		return ExploreOutputBudget{
			MaxOutputChars:       13000,
			DefaultMaxFiles:      4,
			MaxCharsPerFile:      3800,
			GapThreshold:         7,
			ExcludeLowValueFiles: true,
		}
	case fileCount < 500:
		return ExploreOutputBudget{
			MaxOutputChars:       18000,
			DefaultMaxFiles:      5,
			MaxCharsPerFile:      3800,
			GapThreshold:         8,
			ExcludeLowValueFiles: true,
		}
	case fileCount < 5000:
		return ExploreOutputBudget{
			MaxOutputChars:       24000,
			DefaultMaxFiles:      8,
			MaxCharsPerFile:      6500,
			GapThreshold:         12,
			ExcludeLowValueFiles: false,
		}
	default: // ≥5000
		return ExploreOutputBudget{
			MaxOutputChars:       24000,
			DefaultMaxFiles:      8,
			MaxCharsPerFile:      7000,
			GapThreshold:         15,
			ExcludeLowValueFiles: false,
		}
	}
}

// exploreHardCeiling is the absolute maximum output characters (25000).
// Any output longer than min(maxOutputChars*1.5, 25000) must be cut at the
// last \n#### section boundary in the back half.
const exploreHardCeiling = 25000

// exploreWholeCentralLines is the max line count for inlining a "central" file in full.
const exploreWholeCentralLines = 280

// exploreWholePeripheralLines is the max line count for inlining a "peripheral" file in full.
const exploreWholePeripheralLines = 220

// ---------------------------------------------------------------------------
// Input limits (appendix L)
// ---------------------------------------------------------------------------

const (
	maxQueryLen  = 10000
	maxSymbolLen = 10000
	maxPathLen   = 4096
)

// ---------------------------------------------------------------------------
// Tiny-repo threshold (appendix K)
// ---------------------------------------------------------------------------

const tinyRepoThreshold = 500

// ---------------------------------------------------------------------------
// Server instructions (de-branded — appendix L)
//
// This is the ONLY place agent guidance lives. Never duplicate it in tool
// descriptions or elsewhere.
// ---------------------------------------------------------------------------

const serverInstructions = `You have access to a code-intelligence index via the atomic_code_* tools.
These tools let you navigate large codebases efficiently without reading files manually.

## How to use the tools

**Start with atomic_code_explore.** For any unfamiliar codebase topic or question, call
atomic_code_explore first. It gathers relevant context, traces call flows, and returns
source already read — treat the returned content as read; call another explore for more
context rather than using a file-read tool.

**Use atomic_code_search to find symbols** by name, kind (function, class, interface,
route, …), or language when you need to locate specific nodes before diving deeper.

**Use atomic_code_node to inspect a symbol** in detail. On an ambiguous bare name it
returns all overloads in one call. Container kinds (class, interface, module) return a
structural outline of member signatures rather than the full body. Code is line-numbered.
The trail section shows up to 12 callers and 12 callees; dynamic-dispatch edges are
annotated.

**Use atomic_code_callers / atomic_code_callees** to traverse the call graph from a
known node ID. Use these for targeted traversal after you have identified the relevant
symbols with search or node.

**Use atomic_code_impact** to understand which symbols are transitively affected by a
change to a given symbol — useful for change-impact analysis.

**Use atomic_code_files** to list indexed files, optionally filtered by path prefix or
glob pattern.

**Use atomic_code_status** to check whether the index is current and how many files are
pending re-index.

## Workflow guidance

1. Call atomic_code_explore for the broad question first.
2. Use atomic_code_search / atomic_code_node to narrow to specific symbols.
3. Use atomic_code_callers / atomic_code_callees / atomic_code_impact for graph traversal.
4. Call atomic_code_explore again with a refined query if you need more context.

The index is local and fast — prefer these tools over reading files directly.
Explore output is already-read source; do not read the same files again with a
file-read tool. For more context, call atomic_code_explore with a refined query.`

// ---------------------------------------------------------------------------
// Server construction
// ---------------------------------------------------------------------------

// NewServer creates a configured *sdk.Server bound to eng. The server
// registers the applicable tools based on fileCount (tiny-repo gating) and
// sets the de-branded server instructions. The returned server is
// transport-agnostic: call RunStdio to serve over stdio, or use
// srv.Connect(ctx, transport, nil) to serve over any other transport (CP23).
func NewServer(eng *engine.Engine, fileCount int) *sdk.Server {
	srv := sdk.NewServer(
		&sdk.Implementation{
			Name:    "atomic-code",
			Version: version.Version,
		},
		&sdk.ServerOptions{
			Instructions: serverInstructions,
		},
	)

	// Always-registered tools.
	addToolExplore(srv, eng, fileCount)
	addToolSearch(srv, eng)
	addToolNode(srv, eng)

	// Full-repo tools (≥500 files).
	if fileCount >= tinyRepoThreshold {
		addToolCallers(srv, eng)
		addToolCallees(srv, eng)
		addToolImpact(srv, eng)
		addToolStatus(srv, eng)
		addToolFiles(srv, eng)
	}

	return srv
}

// RunStdio builds the engine (Init or Open), creates the MCP server, and runs
// it over the stdio transport. This is the implementation for `atomic code mcp`.
func RunStdio(ctx context.Context, projectRoot string) error {
	eng, err := engine.New(projectRoot)
	if err != nil {
		return fmt.Errorf("atomic code mcp: create engine: %w", err)
	}
	defer eng.Close()

	// Open existing index (do not re-index on every mcp start).
	if eng.IsInitialized() {
		if err := eng.Open(ctx); err != nil {
			return fmt.Errorf("atomic code mcp: open engine: %w", err)
		}
	} else {
		// No index yet — still serve with a working (empty) engine.
		if err := eng.Init(ctx); err != nil {
			return fmt.Errorf("atomic code mcp: init engine: %w", err)
		}
	}

	stats, err := eng.GetStats(ctx)
	fileCount := 0
	if err == nil {
		fileCount = stats.FileCount
	}

	srv := NewServer(eng, fileCount)
	return srv.Run(ctx, &sdk.StdioTransport{})
}

// ---------------------------------------------------------------------------
// Tool: atomic_code_search
// ---------------------------------------------------------------------------

type searchInput struct {
	Query string `json:"query" jsonschema:"Search query (symbol name or keywords)"`
	Kind  string `json:"kind,omitempty"  jsonschema:"Optional node kind filter (function/class/interface/route/…)"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum results (default 20)"`
}

func addToolSearch(srv *sdk.Server, eng *engine.Engine) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "atomic_code_search",
		Description: "Search indexed symbols by name, kind, or language. Use to find specific nodes before deeper inspection.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in searchInput) (*sdk.CallToolResult, any, error) {
		if len(in.Query) > maxQueryLen {
			return errorResult("query exceeds maximum length of %d characters", maxQueryLen), nil, nil
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		opts := types.SearchOptions{
			Query: in.Query,
			Limit: limit,
		}
		if in.Kind != "" {
			opts.Kind = types.NodeKind(in.Kind)
		}
		results, err := eng.SearchNodes(ctx, opts)
		if err != nil {
			return errorResult("search: %v", err), nil, nil
		}
		text := formatSearchResults(results)
		return textResult(text), nil, nil
	})
}

// ---------------------------------------------------------------------------
// Tool: atomic_code_callers
// ---------------------------------------------------------------------------

type callersInput struct {
	Symbol string `json:"symbol" jsonschema:"Symbol name or node ID to find callers of"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum callers to return (default 20)"`
}

func addToolCallers(srv *sdk.Server, eng *engine.Engine) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "atomic_code_callers",
		Description: "Find all nodes that call the given symbol. Resolves by name first, then traverses callers.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in callersInput) (*sdk.CallToolResult, any, error) {
		if len(in.Symbol) > maxSymbolLen {
			return errorResult("symbol exceeds maximum length of %d characters", maxSymbolLen), nil, nil
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		nodeID, err := resolveSymbolToID(ctx, eng, in.Symbol)
		if err != nil {
			return errorResult("callers: %v", err), nil, nil
		}
		sg, err := eng.GetCallers(ctx, nodeID, 2)
		if err != nil {
			return errorResult("callers: %v", err), nil, nil
		}
		nodes := types.SubgraphSortedNodes(sg)
		if len(nodes) > limit {
			nodes = nodes[:limit]
		}
		text := formatNodeList("Callers", nodes)
		return textResult(text), nil, nil
	})
}

// ---------------------------------------------------------------------------
// Tool: atomic_code_callees
// ---------------------------------------------------------------------------

type calleesInput struct {
	Symbol string `json:"symbol" jsonschema:"Symbol name or node ID to find callees of"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum callees to return (default 20)"`
}

func addToolCallees(srv *sdk.Server, eng *engine.Engine) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "atomic_code_callees",
		Description: "Find all nodes called by the given symbol. Resolves by name first, then traverses callees.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in calleesInput) (*sdk.CallToolResult, any, error) {
		if len(in.Symbol) > maxSymbolLen {
			return errorResult("symbol exceeds maximum length of %d characters", maxSymbolLen), nil, nil
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		nodeID, err := resolveSymbolToID(ctx, eng, in.Symbol)
		if err != nil {
			return errorResult("callees: %v", err), nil, nil
		}
		sg, err := eng.GetCallees(ctx, nodeID, 2)
		if err != nil {
			return errorResult("callees: %v", err), nil, nil
		}
		nodes := types.SubgraphSortedNodes(sg)
		if len(nodes) > limit {
			nodes = nodes[:limit]
		}
		text := formatNodeList("Callees", nodes)
		return textResult(text), nil, nil
	})
}

// ---------------------------------------------------------------------------
// Tool: atomic_code_impact
// ---------------------------------------------------------------------------

type impactInput struct {
	Symbol string `json:"symbol" jsonschema:"Symbol name or node ID to analyse impact of"`
	Depth  int    `json:"depth,omitempty" jsonschema:"Maximum traversal depth (default 3)"`
}

func addToolImpact(srv *sdk.Server, eng *engine.Engine) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "atomic_code_impact",
		Description: "Find all symbols transitively affected by a change to the given symbol.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in impactInput) (*sdk.CallToolResult, any, error) {
		if len(in.Symbol) > maxSymbolLen {
			return errorResult("symbol exceeds maximum length of %d characters", maxSymbolLen), nil, nil
		}
		depth := in.Depth
		if depth <= 0 {
			depth = 3
		}
		nodeID, err := resolveSymbolToID(ctx, eng, in.Symbol)
		if err != nil {
			return errorResult("impact: %v", err), nil, nil
		}
		sg, err := eng.GetImpactRadius(ctx, nodeID, depth)
		if err != nil {
			return errorResult("impact: %v", err), nil, nil
		}
		nodes := types.SubgraphSortedNodes(sg)
		text := formatNodeList("Impact radius", nodes)
		return textResult(text), nil, nil
	})
}

// ---------------------------------------------------------------------------
// Tool: atomic_code_node
// ---------------------------------------------------------------------------

type nodeInput struct {
	Symbol      string `json:"symbol"                 jsonschema:"Symbol name or node ID"`
	IncludeCode *bool  `json:"includeCode,omitempty"  jsonschema:"Include source code in output (default true); set false to omit code block"`
	File        string `json:"file,omitempty"         jsonschema:"Filter by file path (for disambiguation)"`
	Line        int    `json:"line,omitempty"         jsonschema:"Filter by line number (for disambiguation)"`
}

func addToolNode(srv *sdk.Server, eng *engine.Engine) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "atomic_code_node",
		Description: "Show detail for a symbol. On an ambiguous bare name returns ALL overloads in one call. Container kinds return a structural outline (member signatures) not the full body.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in nodeInput) (*sdk.CallToolResult, any, error) {
		if len(in.Symbol) > maxSymbolLen {
			return errorResult("symbol exceeds maximum length of %d characters", maxSymbolLen), nil, nil
		}
		if len(in.File) > maxPathLen {
			return errorResult("file path exceeds maximum length of %d characters", maxPathLen), nil, nil
		}

		// Default includeCode to true; false only when caller explicitly passes false.
		includeCode := true
		if in.IncludeCode != nil && !*in.IncludeCode {
			includeCode = false
		}

		// Resolve: if it looks like a node ID (starts with "node:"), look it up
		// directly. Otherwise do a full name-scan returning ALL overloads.
		var nodes []types.Node
		if strings.HasPrefix(in.Symbol, "node:") {
			n, err := eng.GetNode(ctx, in.Symbol)
			if err != nil {
				return errorResult("node: %v", err), nil, nil
			}
			nodes = []types.Node{n}
		} else {
			// GetNodesByName with kind="" returns all kinds.
			all, err := eng.GetNodesByName(ctx, in.Symbol, "")
			if err != nil {
				return errorResult("node: %v", err), nil, nil
			}
			if len(all) == 0 {
				return textResult(fmt.Sprintf("No nodes found for %q", in.Symbol)), nil, nil
			}
			// Apply optional file/line filters.
			nodes = filterNodes(all, in.File, in.Line)
			if len(nodes) == 0 {
				nodes = all // fallback: no filter matched, return all
			}
		}

		var sb strings.Builder
		for i, n := range nodes {
			if i > 0 {
				sb.WriteString("\n---\n\n")
			}
			renderNodeDetail(ctx, eng, &sb, n, includeCode)
		}
		return textResult(sb.String()), nil, nil
	})
}

// renderNodeDetail renders one node as detailed markdown into sb.
// includeCode controls whether the source code block is emitted.
func renderNodeDetail(ctx context.Context, eng *engine.Engine, sb *strings.Builder, n types.Node, includeCode bool) {
	// Header
	fmt.Fprintf(sb, "#### %s `%s`\n\n", n.Kind, n.QualifiedName)
	fmt.Fprintf(sb, "- **id:** `%s`\n", n.ID)
	fmt.Fprintf(sb, "- **file:** `%s`\n", n.FilePath)
	fmt.Fprintf(sb, "- **lines:** %d–%d\n", n.StartLine, n.EndLine)
	if n.Language != "" {
		fmt.Fprintf(sb, "- **language:** %s\n", n.Language)
	}
	if n.IsExported {
		fmt.Fprintf(sb, "- **exported:** true\n")
	}
	sb.WriteString("\n")

	// Container kinds → structural outline (member signatures, not full body).
	isContainer := n.Kind == types.NodeKindClass ||
		n.Kind == types.NodeKindInterface ||
		n.Kind == types.NodeKindModule ||
		n.Kind == types.NodeKindNamespace
	if isContainer {
		renderContainerOutline(ctx, eng, sb, n)
	} else if includeCode {
		// Non-container: include line-numbered source code only when requested.
		cb, err := eng.GetCode(ctx, n.ID)
		if err == nil && cb.Content != "" {
			lang := strings.ToLower(string(n.Language))
			fmt.Fprintf(sb, "```%s\n", lang)
			renderLineNumbered(sb, cb.Content, n.StartLine)
			sb.WriteString("```\n\n")
		}
	}

	// Trail: up to 12 callers + 12 callees.
	renderTrail(ctx, eng, sb, n.ID)
}

// renderContainerOutline writes member signatures for a container node.
func renderContainerOutline(ctx context.Context, eng *engine.Engine, sb *strings.Builder, n types.Node) {
	members, err := eng.GetNodesInFile(ctx, n.FilePath)
	if err != nil {
		return
	}
	// Filter to members contained within this node's line range.
	var contained []types.Node
	for _, m := range members {
		if m.ID == n.ID {
			continue
		}
		if m.StartLine >= n.StartLine && m.EndLine <= n.EndLine {
			contained = append(contained, m)
		}
	}
	if len(contained) == 0 {
		return
	}
	sort.Slice(contained, func(i, j int) bool {
		return contained[i].StartLine < contained[j].StartLine
	})
	sb.WriteString("**Members:**\n\n")
	for _, m := range contained {
		fmt.Fprintf(sb, "- `%s` %s (line %d)\n", m.Name, m.Kind, m.StartLine)
	}
	sb.WriteString("\n")
}

// renderTrail appends up to 12 callers + 12 callees with heuristic annotations.
// DB errors are surfaced inline rather than silently yielding an empty trail.
func renderTrail(ctx context.Context, eng *engine.Engine, sb *strings.Builder, nodeID string) {
	callers, callersErr := eng.GetCallers(ctx, nodeID, 1)
	callees, calleesErr := eng.GetCallees(ctx, nodeID, 1)

	if callersErr != nil {
		fmt.Fprintf(sb, "*trail unavailable (callers): %v*\n\n", callersErr)
		return
	}
	if calleesErr != nil {
		fmt.Fprintf(sb, "*trail unavailable (callees): %v*\n\n", calleesErr)
		return
	}

	callerNodes := types.SubgraphSortedNodes(callers)
	calleeNodes := types.SubgraphSortedNodes(callees)

	const trailLimit = 12

	if len(callerNodes) > 0 {
		if len(callerNodes) > trailLimit {
			callerNodes = callerNodes[:trailLimit]
		}
		sb.WriteString("**Callers:**\n\n")
		for _, c := range callerNodes {
			heur := heuristicAnnotation(callers, nodeID, c.ID)
			fmt.Fprintf(sb, "- `%s` (`%s`%s)\n", c.QualifiedName, c.ID, heur)
		}
		sb.WriteString("\n")
	}

	if len(calleeNodes) > 0 {
		if len(calleeNodes) > trailLimit {
			calleeNodes = calleeNodes[:trailLimit]
		}
		sb.WriteString("**Callees:**\n\n")
		for _, c := range calleeNodes {
			heur := heuristicAnnotation(callees, nodeID, c.ID)
			fmt.Fprintf(sb, "- `%s` (`%s`%s)\n", c.QualifiedName, c.ID, heur)
		}
		sb.WriteString("\n")
	}
}

// heuristicAnnotation returns " [dynamic]" if any edge between fromID and toID
// in sg has heuristic provenance, else "".
func heuristicAnnotation(sg types.Subgraph, fromID, toID string) string {
	for _, e := range sg.Edges {
		if (e.Source == fromID && e.Target == toID) ||
			(e.Source == toID && e.Target == fromID) {
			if e.Provenance == "heuristic" {
				return " [dynamic]"
			}
		}
	}
	return ""
}

// renderLineNumbered writes source with line numbers starting at startLine.
func renderLineNumbered(sb *strings.Builder, content string, startLine int) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		fmt.Fprintf(sb, "%4d  %s\n", startLine+i, line)
	}
}

// filterNodes applies optional file and line filters.
func filterNodes(nodes []types.Node, file string, line int) []types.Node {
	if file == "" && line == 0 {
		return nodes
	}
	var out []types.Node
	for _, n := range nodes {
		if file != "" && !strings.HasSuffix(n.FilePath, file) {
			continue
		}
		if line != 0 && (n.StartLine > line || n.EndLine < line) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// ---------------------------------------------------------------------------
// Tool: atomic_code_explore
// ---------------------------------------------------------------------------

type exploreInput struct {
	Query    string `json:"query"              jsonschema:"Natural-language question or topic to explore"`
	MaxFiles int    `json:"maxFiles,omitempty" jsonschema:"Override maximum files to include"`
}

func addToolExplore(srv *sdk.Server, eng *engine.Engine, fileCount int) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "atomic_code_explore",
		Description: "Gather relevant context for a query. Returns source already read — treat it as read. Call again with a refined query for more context rather than using a file-read tool.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in exploreInput) (*sdk.CallToolResult, any, error) {
		if len(in.Query) > maxQueryLen {
			return errorResult("query exceeds maximum length of %d characters", maxQueryLen), nil, nil
		}

		budget := GetExploreOutputBudget(fileCount)
		maxFiles := budget.DefaultMaxFiles
		if in.MaxFiles > 0 {
			maxFiles = in.MaxFiles
		}

		// Build context via FindRelevantContext.
		opts := codectx.Options{
			Limit: maxFiles * 5, // generous result cap
		}
		sg, tier, truncated, err := eng.FindRelevantContext(ctx, in.Query, opts)
		if err != nil {
			return errorResult("explore: %v", err), nil, nil
		}

		buildOpts := codectx.BuildOptions{
			Format:    codectx.FormatMarkdown,
			Query:     in.Query,
			Source:    tier,
			Truncated: truncated,
		}
		ctxResult, err := eng.BuildContext(ctx, sg, buildOpts)
		if err != nil {
			return errorResult("explore: build context: %v", err), nil, nil
		}

		// Build the Flow section from named symbols in the query.
		flowSection := buildFlowFromNamedSymbols(ctx, eng, in.Query)

		// Assemble output.
		var sb strings.Builder
		if flowSection != "" {
			sb.WriteString(flowSection)
			sb.WriteString("\n")
		}
		sb.WriteString(ctxResult.Content)

		// Append tier note (for transparency).
		if tier != "" {
			fmt.Fprintf(&sb, "\n\n*Search tier: %s*", tier)
		}

		output := sb.String()

		// Apply hard ceiling: min(maxOutputChars*1.5, 25000).
		ceiling := budget.MaxOutputChars * 3 / 2
		if ceiling > exploreHardCeiling {
			ceiling = exploreHardCeiling
		}
		output = ApplyCeiling(output, ceiling)

		// Ensure output never tells the agent to "use Read".
		output = sanitizeExploreOutput(output)

		return textResult(output), nil, nil
	})
}

// buildFlowFromNamedSymbols tokenizes the query, resolves named symbols to
// node IDs, performs a BFS along calls edges (≤1 unnamed bridge hop), finds
// the longest chain, and returns a "## Flow" markdown section. Returns "" if
// no symbols are found.
func buildFlowFromNamedSymbols(ctx context.Context, eng *engine.Engine, query string) string {
	// Tokenize: split on whitespace and common punctuation, keep CamelCase and
	// snake_case tokens that look like identifiers (≥3 chars, at least one letter).
	tokens := tokenizeQuery(query)
	if len(tokens) == 0 {
		return ""
	}

	// Resolve each token to nodes.
	type namedNode struct {
		token string
		node  types.Node
	}
	var named []namedNode
	seen := make(map[string]bool)
	for _, tok := range tokens {
		if len(tok) < 3 {
			continue
		}
		nodes, err := eng.GetNodesByName(ctx, tok, "")
		if err != nil || len(nodes) == 0 {
			continue
		}
		for _, n := range nodes {
			if !seen[n.ID] {
				seen[n.ID] = true
				named = append(named, namedNode{token: tok, node: n})
			}
		}
		if len(named) >= 8 {
			break // limit resolution effort
		}
	}
	if len(named) == 0 {
		return ""
	}

	// BFS along calls edges from each named node to find chains.
	// Allow ≤1 unnamed bridge hop (a node not named in the query).
	type chainLink struct {
		nodeID string
		bridge bool // true if this is an unnamed bridge hop
	}
	type chainState struct {
		links   []chainLink
		bridges int
	}

	// Build a set of named node IDs for quick lookup.
	namedIDs := make(map[string]bool, len(named))
	for _, nn := range named {
		namedIDs[nn.node.ID] = true
	}

	var longestChain []string
	for _, start := range named {
		// BFS to find chains connecting named nodes.
		type bfsState struct {
			nodeID  string
			chain   []string
			bridges int
		}
		queue := []bfsState{{nodeID: start.node.ID, chain: []string{start.node.ID}}}
		visited := map[string]bool{start.node.ID: true}

		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]

			if len(cur.chain) > 1 && len(cur.chain) > len(longestChain) {
				longestChain = append([]string(nil), cur.chain...)
			}

			if len(cur.chain) > 6 { // max chain length
				continue
			}

			sg, err := eng.GetCallees(ctx, cur.nodeID, 1)
			if err != nil {
				continue
			}
			for _, next := range types.SubgraphSortedNodes(sg) {
				if visited[next.ID] {
					continue
				}
				isNamed := namedIDs[next.ID]
				newBridges := cur.bridges
				if !isNamed {
					newBridges++
				}
				if newBridges > 1 {
					continue // exceed bridge budget
				}
				visited[next.ID] = true
				newChain := append(append([]string(nil), cur.chain...), next.ID)
				queue = append(queue, bfsState{
					nodeID:  next.ID,
					chain:   newChain,
					bridges: newBridges,
				})
			}
		}
	}

	if len(longestChain) < 2 {
		return ""
	}

	// Supplement with heuristic (synthesized) edges incident to named symbols.
	type heuristicEdge struct {
		from, to string
	}
	var hEdges []heuristicEdge
	for _, nn := range named {
		callees, _ := eng.GetCallees(ctx, nn.node.ID, 1)
		for _, e := range callees.Edges {
			if e.Provenance == "heuristic" {
				hEdges = append(hEdges, heuristicEdge{from: e.Source, to: e.Target})
			}
		}
		callers, _ := eng.GetCallers(ctx, nn.node.ID, 1)
		for _, e := range callers.Edges {
			if e.Provenance == "heuristic" {
				hEdges = append(hEdges, heuristicEdge{from: e.Source, to: e.Target})
			}
		}
	}

	// Build the markdown Flow section.
	var sb strings.Builder
	sb.WriteString("## Flow\n\n")

	// Render the longest chain.
	for i, id := range longestChain {
		n, err := eng.GetNode(ctx, id)
		name := id
		if err == nil {
			name = n.QualifiedName
			if name == "" {
				name = n.Name
			}
		}
		if i < len(longestChain)-1 {
			fmt.Fprintf(&sb, "`%s` →\n", name)
		} else {
			fmt.Fprintf(&sb, "`%s`\n", name)
		}
	}

	// Append heuristic edges.
	if len(hEdges) > 0 {
		sb.WriteString("\n**Synthesized (heuristic) edges:**\n\n")
		seen2 := make(map[string]bool)
		for _, he := range hEdges {
			key := he.from + "→" + he.to
			if seen2[key] {
				continue
			}
			seen2[key] = true
			fromN, _ := eng.GetNode(ctx, he.from)
			toN, _ := eng.GetNode(ctx, he.to)
			fromName := he.from
			toName := he.to
			if fromN.Name != "" {
				fromName = fromN.QualifiedName
			}
			if toN.Name != "" {
				toName = toN.QualifiedName
			}
			fmt.Fprintf(&sb, "- `%s` → `%s` *(heuristic)*\n", fromName, toName)
		}
	}
	sb.WriteString("\n")

	return sb.String()
}

// tokenizeQuery splits a query string into identifier-like tokens.
func tokenizeQuery(query string) []string {
	var tokens []string
	// Split on whitespace and common punctuation.
	cur := strings.Builder{}
	for _, r := range query {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			cur.WriteRune(r)
		} else {
			if cur.Len() >= 3 {
				tok := cur.String()
				// Must contain at least one letter.
				if hasLetter(tok) {
					tokens = append(tokens, tok)
				}
			}
			cur.Reset()
		}
	}
	if cur.Len() >= 3 {
		tok := cur.String()
		if hasLetter(tok) {
			tokens = append(tokens, tok)
		}
	}
	return tokens
}

func hasLetter(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

// ApplyCeiling truncates output to ceiling chars, cutting at the last
// \n#### section boundary in the back half of the string.
// Exported so tests can assert the exact cut behaviour.
func ApplyCeiling(output string, ceiling int) string {
	if len(output) <= ceiling {
		return output
	}

	// The "back half" begins at ceiling/2.
	backHalfStart := ceiling / 2

	// Find the last \n#### in the range [backHalfStart, ceiling].
	searchRegion := output[backHalfStart:ceiling]
	lastBoundary := strings.LastIndex(searchRegion, "\n####")
	if lastBoundary >= 0 {
		cutAt := backHalfStart + lastBoundary
		return output[:cutAt]
	}

	// No \n#### found in the back half — cut at ceiling.
	return output[:ceiling]
}

// sanitizeExploreOutput replaces any instruction to "use Read" with a
// steering message directing the agent to call explore again.
func sanitizeExploreOutput(output string) string {
	replacements := []string{
		"use Read", "use the Read tool", "use file read", "call Read",
		"use the read tool", "call the Read tool",
	}
	for _, old := range replacements {
		output = strings.ReplaceAll(output, old, "call atomic_code_explore with a refined query")
	}
	return output
}

// ---------------------------------------------------------------------------
// Tool: atomic_code_status
// ---------------------------------------------------------------------------

func addToolStatus(srv *sdk.Server, eng *engine.Engine) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "atomic_code_status",
		Description: "Return the current index status: whether the index is initialized, file/node/edge counts, and pending changes.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		initialized := eng.IsInitialized()
		if !initialized {
			data, err := json.Marshal(map[string]any{
				"initialized": false,
				"version":     "1",
			})
			if err != nil {
				return errorResult("status: marshal: %v", err), nil, nil
			}
			return textResult(string(data)), nil, nil
		}

		stats, err := eng.GetStats(ctx)
		if err != nil {
			return errorResult("status: %v", err), nil, nil
		}

		byKind := make(map[string]int, len(stats.NodesByKind))
		for k, v := range stats.NodesByKind {
			byKind[string(k)] = v
		}

		s := map[string]any{
			"initialized": true,
			"version":     "1",
			"fileCount":   stats.FileCount,
			"nodeCount":   stats.NodeCount,
			"edgeCount":   stats.EdgeCount,
			"backend":     eng.GetBackend(),
			"journalMode": eng.GetJournalMode(),
			"lastIndexed": stats.LastIndexedAt,
			"nodesByKind": byKind,
		}

		data, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			return errorResult("status: marshal: %v", err), nil, nil
		}
		return textResult(string(data)), nil, nil
	})
}

// ---------------------------------------------------------------------------
// Tool: atomic_code_files
// ---------------------------------------------------------------------------

type filesInput struct {
	Path    string `json:"path,omitempty"    jsonschema:"Filter by path prefix"`
	Pattern string `json:"pattern,omitempty" jsonschema:"Filter by glob pattern (e.g. '*.go')"`
	Format  string `json:"format,omitempty"  jsonschema:"Output format: 'list' (default) or 'json'"`
}

func addToolFiles(srv *sdk.Server, eng *engine.Engine) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "atomic_code_files",
		Description: "List indexed files. Optionally filter by path prefix or glob pattern.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in filesInput) (*sdk.CallToolResult, any, error) {
		if len(in.Path) > maxPathLen {
			return errorResult("path exceeds maximum length of %d characters", maxPathLen), nil, nil
		}

		files, err := eng.GetFiles(ctx)
		if err != nil {
			return errorResult("files: %v", err), nil, nil
		}

		// Apply filters.
		var filtered []types.FileRecord
		for _, f := range files {
			if in.Path != "" && !strings.HasPrefix(f.Path, in.Path) {
				continue
			}
			if in.Pattern != "" {
				matched, err := filepath.Match(in.Pattern, filepath.Base(f.Path))
				if err != nil || !matched {
					continue
				}
			}
			filtered = append(filtered, f)
		}

		if in.Format == "json" {
			data, err := json.MarshalIndent(filtered, "", "  ")
			if err != nil {
				return errorResult("files: marshal: %v", err), nil, nil
			}
			return textResult(string(data)), nil, nil
		}

		// Default: list format.
		var sb strings.Builder
		fmt.Fprintf(&sb, "%d files\n\n", len(filtered))
		for _, f := range filtered {
			fmt.Fprintf(&sb, "- `%s`\n", f.Path)
		}
		return textResult(sb.String()), nil, nil
	})
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// resolveSymbolToID resolves a symbol string to a node ID. If the input
// already looks like a node ID (starts with "node:"), it is returned as-is.
// Otherwise GetNodesByName is called and the first result is used.
func resolveSymbolToID(ctx context.Context, eng *engine.Engine, symbol string) (string, error) {
	if strings.HasPrefix(symbol, "node:") {
		return symbol, nil
	}
	nodes, err := eng.GetNodesByName(ctx, symbol, "")
	if err != nil {
		return "", err
	}
	if len(nodes) == 0 {
		return "", fmt.Errorf("no node found for symbol %q", symbol)
	}
	return nodes[0].ID, nil
}

// formatSearchResults formats a slice of SearchResult as markdown.
func formatSearchResults(results []types.SearchResult) string {
	if len(results) == 0 {
		return "No results found."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d result(s)\n\n", len(results))
	for _, r := range results {
		n := r.Node
		fmt.Fprintf(&sb, "- **`%s`** (%s) — `%s` line %d  `%s`\n",
			n.QualifiedName, n.Kind, n.FilePath, n.StartLine, n.ID)
	}
	return sb.String()
}

// formatNodeList formats a slice of nodes as a markdown list.
func formatNodeList(heading string, nodes []types.Node) string {
	if len(nodes) == 0 {
		return heading + ": none found."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "**%s** (%d)\n\n", heading, len(nodes))
	for _, n := range nodes {
		fmt.Fprintf(&sb, "- `%s` (%s) — `%s`:%d  `%s`\n",
			n.QualifiedName, n.Kind, n.FilePath, n.StartLine, n.ID)
	}
	return sb.String()
}

// textResult creates a successful *sdk.CallToolResult with a single TextContent.
func textResult(text string) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		Content: []sdk.Content{
			&sdk.TextContent{Text: text},
		},
	}
}

// errorResult creates an error *sdk.CallToolResult.
func errorResult(format string, args ...any) *sdk.CallToolResult {
	msg := fmt.Sprintf(format, args...)
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{
			&sdk.TextContent{Text: msg},
		},
	}
}
