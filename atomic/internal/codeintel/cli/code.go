// Package cli implements the `atomic code` subcommand handlers.
//
// Each verb is a standalone function taking an engine, args, and an io.Writer,
// so it is testable without os.Exit.
package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/codectx"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	codemcp "github.com/damusix/atomic-claude/atomic/internal/codeintel/mcp"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// RunCode dispatches `atomic code <verb>`. projectRoot must already be
// absolute; main.go resolves it. stdin serves the `affected --stdin` path.
func RunCode(args []string, projectRoot string, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printCodeUsage(stderr)
		return 0
	}

	verb := args[0]
	rest := args[1:]
	ctx := context.Background()

	// mcp proxies to a daemon and so needs no engine of its own.
	if verb == "mcp" {
		dbPath := engine.IndexPath(projectRoot)
		return runMCP(ctx, projectRoot, dbPath, rest, stderr)
	}

	eng, err := engine.New(projectRoot)
	if err != nil {
		fmt.Fprintf(stderr, "atomic code: create engine: %v\n", err)
		return 1
	}
	defer eng.Close()

	// index and sync may create the index; query verbs require an existing one.
	switch verb {
	case "index":
		return runIndex(ctx, eng, rest, projectRoot, stdout, stderr)
	case "sync":
		return runSync(ctx, eng, rest, stdout, stderr)
	case "status":
		return runStatus(ctx, eng, rest, projectRoot, stdout, stderr)
	case "search":
		return runSearch(ctx, eng, rest, stdout, stderr)
	case "callers":
		return runCallers(ctx, eng, rest, stdout, stderr)
	case "callees":
		return runCallees(ctx, eng, rest, stdout, stderr)
	case "impact":
		return runImpact(ctx, eng, rest, stdout, stderr)
	case "node":
		return runNode(ctx, eng, rest, stdout, stderr)
	case "files":
		return runFiles(ctx, eng, rest, stdout, stderr)
	case "affected":
		return runAffected(ctx, eng, rest, stdin, stdout, stderr)
	case "explore":
		return runExplore(ctx, eng, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "atomic code: unknown verb %q\n", verb)
		printCodeUsage(stderr)
		return 1
	}
}

func printCodeUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: atomic code <verb> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Verbs:")
	fmt.Fprintln(w, "  index     Index all source files in the project")
	fmt.Fprintln(w, "  sync      Incrementally re-index changed files")
	fmt.Fprintln(w, "  status    Show index status (--json for machine-readable)")
	fmt.Fprintln(w, "  search    Search indexed nodes by name/kind/language")
	fmt.Fprintln(w, "  callers   Find callers of a symbol (--depth)")
	fmt.Fprintln(w, "  callees   Find callees of a symbol (--depth)")
	fmt.Fprintln(w, "  impact    Find impact radius of a symbol (--depth)")
	fmt.Fprintln(w, "  node      Show node detail for a symbol")
	fmt.Fprintln(w, "  files     List indexed files (optional path/pattern filter)")
	fmt.Fprintln(w, "  affected  Find test files transitively affected by changed files")
	fmt.Fprintln(w, "  explore   Gather relevant context for a query (markdown output)")
	fmt.Fprintln(w, "  mcp       Run the MCP server over stdio (--daemon runs the auto-started server itself)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "All query verbs accept --json for machine-readable output.")
	fmt.Fprintln(w, "DB path: "+config.IndexDBPath("<project>"))
}

func runIndex(ctx context.Context, eng *engine.Engine, args []string, projectRoot string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("code index", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic code index [--profile]")
	var profileFlag bool
	fs.BoolVar(&profileFlag, "profile", false, "emit per-phase wall-time to stderr")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	profiling := profileFlag || os.Getenv("ATOMIC_CODE_PROFILE") == "1"

	if err := eng.Init(ctx); err != nil {
		fmt.Fprintf(stderr, "atomic code index: init: %v\n", err)
		return 1
	}

	if err := EnsureGitignore(projectRoot); err != nil {
		fmt.Fprintf(stderr, "atomic code index: gitignore: %v (non-fatal)\n", err)
	}

	fmt.Fprintf(stdout, "indexing %s…\n", projectRoot)

	extractStart := time.Now()
	if err := eng.IndexAll(ctx); err != nil {
		fmt.Fprintf(stderr, "atomic code index: %v\n", err)
		return 1
	}
	extractDur := time.Since(extractStart)

	// Unreadable files do not abort the index; report the count so the gap is
	// visible rather than silent. Same for a degraded .claude/atomic.toml.
	if skipped := eng.SkippedFiles(); skipped > 0 {
		fmt.Fprintf(stderr, "atomic code index: skipped %d unreadable file(s)\n", skipped)
	}

	if warns := eng.IgnoreWarnings(); len(warns) > 0 {
		fmt.Fprintf(stderr, "atomic code index: repo config: %s\n", strings.Join(warns, "; "))
	}

	// Emitted before resolve starts, so a killed process still reports extract.
	var profileStats types.GraphStats
	if profiling {
		s, err := eng.GetStats(ctx)
		if err == nil {
			profileStats = s
		}
		fmt.Fprintf(stderr, "[profile] extract: %s (%d files)\n", extractDur, profileStats.FileCount)
	}

	// Route nodes must land before resolution, or route→handler refs have
	// nothing to resolve against.
	fwStart := time.Now()
	routeCount, err := eng.ExtractFrameworkNodes(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "atomic code index: extract framework nodes: %v\n", err)
		return 1
	}
	if profiling {
		fmt.Fprintf(stderr, "[profile] frameworks: %s (%d routes)\n", time.Since(fwStart), routeCount)
	}

	if profiling {
		// Writes each sub-phase line the moment that phase ends, not at the end.
		emit := func(phase string, d time.Duration, count int) {
			switch phase {
			case "resolve.warm":
				fmt.Fprintf(stderr, "[profile] resolve.warm: %s (%d nodes)\n", d, count)
			case "resolve.match":
				fmt.Fprintf(stderr, "[profile] resolve.match: %s (%d refs)\n", d, count)
			default:
				fmt.Fprintf(stderr, "[profile] resolve.synth: %s\n", d)
			}
		}
		if _, err := eng.ResolveReferencesProfiled(ctx, emit); err != nil {
			fmt.Fprintf(stderr, "atomic code index: resolve references: %v\n", err)
			return 1
		}
	} else {
		if err := eng.ResolveReferences(ctx); err != nil {
			fmt.Fprintf(stderr, "atomic code index: resolve references: %v\n", err)
			return 1
		}
	}

	// Re-fetched after resolve, not reused from profileStats: those were taken
	// before framework extraction and resolution and undercount nodes/edges.
	summaryStats, err := eng.GetStats(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "atomic code index: get stats: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "indexed: %d files, %d nodes, %d edges\n", summaryStats.FileCount, summaryStats.NodeCount, summaryStats.EdgeCount)
	return 0
}

func runSync(ctx context.Context, eng *engine.Engine, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("code sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic code sync")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Sync must not silently create an index.
	if err := openOrError(ctx, eng, stderr, "sync"); err != nil {
		return 1
	}

	if err := eng.Sync(ctx); err != nil {
		fmt.Fprintf(stderr, "atomic code sync: %v\n", err)
		return 1
	}
	if skipped := eng.SkippedFiles(); skipped > 0 {
		fmt.Fprintf(stderr, "atomic code sync: skipped %d unreadable file(s)\n", skipped)
	}
	if warns := eng.IgnoreWarnings(); len(warns) > 0 {
		fmt.Fprintf(stderr, "atomic code sync: repo config: %s\n", strings.Join(warns, "; "))
	}
	if _, err := eng.ExtractFrameworkNodes(ctx); err != nil {
		fmt.Fprintf(stderr, "atomic code sync: extract framework nodes: %v\n", err)
		return 1
	}
	if err := eng.ResolveReferences(ctx); err != nil {
		fmt.Fprintf(stderr, "atomic code sync: resolve references: %v\n", err)
		return 1
	}

	stats, err := eng.GetStats(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "atomic code sync: get stats: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "synced: %d files, %d nodes, %d edges\n", stats.FileCount, stats.NodeCount, stats.EdgeCount)
	return 0
}

// StatusJSON is the shape emitted by `atomic code status --json`.
type StatusJSON struct {
	Initialized    bool           `json:"initialized"`
	Version        string         `json:"version"`
	IndexPath      string         `json:"indexPath"`
	LastIndexed    string         `json:"lastIndexed"`
	FileCount      int            `json:"fileCount"`
	NodeCount      int            `json:"nodeCount"`
	EdgeCount      int            `json:"edgeCount"`
	Backend        string         `json:"backend"`
	JournalMode    string         `json:"journalMode"`
	NodesByKind    map[string]int `json:"nodesByKind"`
	PendingChanges int            `json:"pendingChanges"`
	// Omitted entirely when no ignore patterns are configured.
	IgnorePatternCount int    `json:"ignorePatternCount,omitempty"`
	IgnoreConfigPath   string `json:"ignoreConfigPath,omitempty"`
}

func runStatus(ctx context.Context, eng *engine.Engine, args []string, projectRoot string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("code status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic code status [--json]")
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	initialized := eng.IsInitialized()

	if !initialized {
		if asJSON {
			s := StatusJSON{Initialized: false, Version: "1"}
			enc, err := json.MarshalIndent(s, "", "  ")
			if err != nil {
				fmt.Fprintf(stderr, "atomic code status: marshal: %v\n", err)
				return 1
			}
			fmt.Fprintln(stdout, string(enc))
		} else {
			fmt.Fprintln(stdout, "not initialized — run `atomic code index` first")
		}
		return 0
	}

	if err := eng.Open(ctx); err != nil {
		fmt.Fprintf(stderr, "atomic code status: open: %v\n", err)
		return 1
	}

	stats, err := eng.GetStats(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "atomic code status: stats: %v\n", err)
		return 1
	}

	pending, err := countPendingChanges(ctx, eng, projectRoot)
	if err != nil {
		// Non-fatal: report 0 rather than failing the whole status call.
		fmt.Fprintf(stderr, "atomic code status: pending changes: %v (non-fatal)\n", err)
	}

	// The engine's bound path, not one derived from projectRoot: in realm scope
	// the db lives under the realm, not the member repo.
	indexPath := eng.IndexPath()

	// A pool-free config read: status must not boot the indexer for one line.
	ignoreCount, ignorePath := eng.IgnorePatternInfo()

	if asJSON {
		byKind := make(map[string]int, len(stats.NodesByKind))
		for k, v := range stats.NodesByKind {
			byKind[string(k)] = v
		}
		s := StatusJSON{
			Initialized:    true,
			Version:        "1",
			IndexPath:      indexPath,
			LastIndexed:    stats.LastIndexedAt,
			FileCount:      stats.FileCount,
			NodeCount:      stats.NodeCount,
			EdgeCount:      stats.EdgeCount,
			Backend:        eng.GetBackend(),
			JournalMode:    eng.GetJournalMode(),
			NodesByKind:    byKind,
			PendingChanges: pending,
		}
		if ignoreCount > 0 {
			s.IgnorePatternCount = ignoreCount
			s.IgnoreConfigPath = ignorePath
		}
		enc, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "atomic code status: marshal: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(enc))
	} else {
		fmt.Fprintf(stdout, "initialized:     %v\n", initialized)
		fmt.Fprintf(stdout, "index path:      %s\n", indexPath)
		fmt.Fprintf(stdout, "last indexed:    %s\n", stats.LastIndexedAt)
		fmt.Fprintf(stdout, "files:           %d\n", stats.FileCount)
		fmt.Fprintf(stdout, "nodes:           %d\n", stats.NodeCount)
		fmt.Fprintf(stdout, "edges:           %d\n", stats.EdgeCount)
		fmt.Fprintf(stdout, "backend:         %s\n", eng.GetBackend())
		fmt.Fprintf(stdout, "journal mode:    %s\n", eng.GetJournalMode())
		fmt.Fprintf(stdout, "pending changes: %d\n", pending)
		if ignoreCount > 0 {
			fmt.Fprintf(stdout, "ignore patterns: %d (%s)\n", ignoreCount, ignorePath)
		}
	}
	return 0
}

// countPendingChanges compares stored content hashes against disk, so the user
// can see how stale the graph is.
func countPendingChanges(ctx context.Context, eng *engine.Engine, projectRoot string) (int, error) {
	files, err := eng.GetFiles(ctx)
	if err != nil {
		return 0, err
	}
	var pending int
	for _, f := range files {
		absPath := filepath.Join(projectRoot, f.Path)
		data, err := os.ReadFile(absPath)
		if err != nil {
			// Deleted since indexing — still a pending change.
			pending++
			continue
		}
		if hashContent(data) != f.ContentHash {
			pending++
		}
	}
	return pending, nil
}

// hashContent must stay identical to the orchestrator's, or stored and
// freshly-computed hashes stop comparing.
func hashContent(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])
}

func runSearch(ctx context.Context, eng *engine.Engine, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("code search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic code search <query> [--json] [--limit N]")
	var asJSON bool
	var limit int
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	fs.IntVar(&limit, "limit", 20, "max results")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	query := strings.Join(fs.Args(), " ")
	if query == "" {
		fmt.Fprintf(stderr, "atomic code search: query required\n")
		return 1
	}

	if err := openOrError(ctx, eng, stderr, "search"); err != nil {
		return 1
	}

	results, err := eng.SearchNodes(ctx, types.SearchOptions{Query: query, Limit: limit})
	if err != nil {
		fmt.Fprintf(stderr, "atomic code search: %v\n", err)
		return 1
	}

	if asJSON {
		enc, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "atomic code search: marshal: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(enc))
	} else {
		for _, r := range results {
			fmt.Fprintf(stdout, "%s  %s  %s:%d  score=%.3f\n",
				r.Node.Kind, r.Node.Name, r.Node.FilePath, r.Node.StartLine, r.Score)
		}
		if len(results) == 0 {
			fmt.Fprintln(stdout, "(no results)")
		}
	}
	return 0
}

func runCallers(ctx context.Context, eng *engine.Engine, args []string, stdout, stderr io.Writer) int {
	return runSymbolGraph(ctx, eng, args, stdout, stderr, "callers")
}

func runCallees(ctx context.Context, eng *engine.Engine, args []string, stdout, stderr io.Writer) int {
	return runSymbolGraph(ctx, eng, args, stdout, stderr, "callees")
}

// runSymbolGraph handles both callers and callees.
func runSymbolGraph(ctx context.Context, eng *engine.Engine, args []string, stdout, stderr io.Writer, direction string) int {
	fs := flag.NewFlagSet("code "+direction, flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, fmt.Sprintf("atomic code %s <symbol> [--depth N] [--json]", direction))
	var asJSON bool
	var depth int
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	fs.IntVar(&depth, "depth", 3, "BFS depth")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	symbol := strings.Join(fs.Args(), " ")
	if symbol == "" {
		fmt.Fprintf(stderr, "atomic code %s: symbol required\n", direction)
		return 1
	}

	if err := openOrError(ctx, eng, stderr, direction); err != nil {
		return 1
	}

	nodes, err := eng.GetNodesByName(ctx, symbol, "")
	if err != nil {
		fmt.Fprintf(stderr, "atomic code %s: lookup %q: %v\n", direction, symbol, err)
		return 1
	}
	if len(nodes) == 0 {
		fmt.Fprintf(stderr, "atomic code %s: symbol %q not found\n", direction, symbol)
		return 1
	}

	var sg types.Subgraph
	if direction == "callers" {
		sg, err = aggregateSymbolGraph(nodes, func(id string) (types.Subgraph, error) {
			return eng.GetCallers(ctx, id, depth)
		})
	} else {
		sg, err = aggregateSymbolGraph(nodes, func(id string) (types.Subgraph, error) {
			return eng.GetCallees(ctx, id, depth)
		})
	}
	if err != nil {
		fmt.Fprintf(stderr, "atomic code %s: %v\n", direction, err)
		return 1
	}

	return printSubgraph(sg, asJSON, stdout, stderr)
}

// aggregateSymbolGraph unions the query across every node matching the name.
// One name routinely covers several definitions — overloads, an interface and
// its implementation — and querying only the first silently drops the callers
// and callees that live on the siblings.
func aggregateSymbolGraph(nodes []types.Node, query func(id string) (types.Subgraph, error)) (types.Subgraph, error) {
	sgs := make([]types.Subgraph, 0, len(nodes))
	for _, n := range nodes {
		sg, err := query(n.ID)
		if err != nil {
			return types.Subgraph{}, err
		}
		sgs = append(sgs, sg)
	}
	return types.MergeSubgraphs(sgs), nil
}

func runImpact(ctx context.Context, eng *engine.Engine, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("code impact", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic code impact <symbol> [--depth N] [--json]")
	var asJSON bool
	var depth int
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	fs.IntVar(&depth, "depth", 3, "BFS depth")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	symbol := strings.Join(fs.Args(), " ")
	if symbol == "" {
		fmt.Fprintf(stderr, "atomic code impact: symbol required\n")
		return 1
	}

	if err := openOrError(ctx, eng, stderr, "impact"); err != nil {
		return 1
	}

	nodes, err := eng.GetNodesByName(ctx, symbol, "")
	if err != nil {
		fmt.Fprintf(stderr, "atomic code impact: lookup %q: %v\n", symbol, err)
		return 1
	}
	if len(nodes) == 0 {
		fmt.Fprintf(stderr, "atomic code impact: symbol %q not found\n", symbol)
		return 1
	}

	sg, err := aggregateSymbolGraph(nodes, func(id string) (types.Subgraph, error) {
		return eng.GetImpactRadius(ctx, id, depth)
	})
	if err != nil {
		fmt.Fprintf(stderr, "atomic code impact: %v\n", err)
		return 1
	}

	return printSubgraph(sg, asJSON, stdout, stderr)
}

func runNode(ctx context.Context, eng *engine.Engine, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("code node", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic code node <symbol> [--file path] [--line N] [--json]")
	var asJSON bool
	var filterFile string
	var filterLine int
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	fs.StringVar(&filterFile, "file", "", "filter by file path")
	fs.IntVar(&filterLine, "line", 0, "filter by line number (requires --file)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	symbol := strings.Join(fs.Args(), " ")
	if symbol == "" {
		fmt.Fprintf(stderr, "atomic code node: symbol required\n")
		return 1
	}

	if err := openOrError(ctx, eng, stderr, "node"); err != nil {
		return 1
	}

	nodes, err := eng.GetNodesByName(ctx, symbol, "")
	if err != nil {
		fmt.Fprintf(stderr, "atomic code node: %v\n", err)
		return 1
	}
	if len(nodes) == 0 {
		fmt.Fprintf(stderr, "atomic code node: symbol %q not found\n", symbol)
		return 1
	}

	// Disambiguate when --file or --line provided.
	filtered := nodes
	if filterFile != "" {
		var match []types.Node
		for _, n := range nodes {
			if strings.Contains(n.FilePath, filterFile) {
				if filterLine == 0 || (n.StartLine <= filterLine && filterLine <= n.EndLine) {
					match = append(match, n)
				}
			}
		}
		if len(match) > 0 {
			filtered = match
		}
	}

	if asJSON {
		enc, err := json.MarshalIndent(filtered, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "atomic code node: marshal: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(enc))
	} else {
		for _, n := range filtered {
			fmt.Fprintf(stdout, "%s  %s  %s  %s:%d-%d\n",
				n.Kind, n.Name, n.QualifiedName, n.FilePath, n.StartLine, n.EndLine)
		}
	}
	return 0
}

func runFiles(ctx context.Context, eng *engine.Engine, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("code files", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic code files [pattern] [--json]")
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	pattern := strings.Join(fs.Args(), " ")

	if err := openOrError(ctx, eng, stderr, "files"); err != nil {
		return 1
	}

	files, err := eng.GetFiles(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "atomic code files: %v\n", err)
		return 1
	}

	if pattern != "" {
		var matched []types.FileRecord
		for _, f := range files {
			if strings.Contains(f.Path, pattern) {
				matched = append(matched, f)
			}
		}
		files = matched
	}

	if asJSON {
		enc, err := json.MarshalIndent(files, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "atomic code files: marshal: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(enc))
	} else {
		for _, f := range files {
			fmt.Fprintf(stdout, "%s\t%s\t%d nodes\n", f.Path, f.Language, f.NodeCount)
		}
		if len(files) == 0 {
			fmt.Fprintln(stdout, "(no indexed files)")
		}
	}
	return 0
}

func runAffected(ctx context.Context, eng *engine.Engine, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("code affected", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic code affected [--depth N] [--test-glob pattern] [--stdin] [--json] [paths...]")
	var asJSON bool
	var depth int
	var testGlob string
	var fromStdin bool
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	fs.IntVar(&depth, "depth", 5, "BFS depth for dependency traversal")
	fs.StringVar(&testGlob, "test-glob", "", "glob pattern to identify test files (e.g. '_test.go')")
	fs.BoolVar(&fromStdin, "stdin", false, "read changed file paths from stdin (one per line)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var changedFiles []string
	if fromStdin {
		scanner := bufio.NewScanner(stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				changedFiles = append(changedFiles, line)
			}
		}
	} else {
		changedFiles = fs.Args()
	}

	if len(changedFiles) == 0 {
		fmt.Fprintf(stderr, "atomic code affected: no changed files specified (use --stdin or pass paths)\n")
		return 1
	}

	if err := openOrError(ctx, eng, stderr, "affected"); err != nil {
		return 1
	}

	// BFS over GetFileDependents from each changed file up to depth hops.
	visited := make(map[string]bool)
	frontier := make([]string, 0, len(changedFiles))
	for _, f := range changedFiles {
		if !visited[f] {
			visited[f] = true
			frontier = append(frontier, f)
		}
	}

	var affected []string
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var nextFrontier []string
		for _, f := range frontier {
			dependents, err := eng.GetFileDependents(ctx, f)
			if err != nil {
				continue // best-effort
			}
			for _, dep := range dependents {
				if !visited[dep.Path] {
					visited[dep.Path] = true
					nextFrontier = append(nextFrontier, dep.Path)
					// Identify test files by glob or heuristic.
					if IsTestFile(dep.Path, testGlob) {
						affected = append(affected, dep.Path)
					}
				}
			}
		}
		frontier = nextFrontier
	}

	if asJSON {
		enc, err := json.MarshalIndent(affected, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "atomic code affected: marshal: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(enc))
	} else {
		for _, f := range affected {
			fmt.Fprintln(stdout, f)
		}
		if len(affected) == 0 {
			fmt.Fprintln(stdout, "(no affected test files found)")
		}
	}
	return 0
}

// IsTestFile matches the caller's glob when one is set, otherwise per-language
// naming conventions.
func IsTestFile(path, glob string) bool {
	if glob != "" {
		matched, err := filepath.Match(glob, filepath.Base(path))
		if err == nil && matched {
			return true
		}
		if matched, err := filepath.Match(glob, path); err == nil && matched {
			return true
		}
	}
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return true
	}
	if strings.HasPrefix(base, "test_") || strings.HasSuffix(strings.TrimSuffix(base, filepath.Ext(base)), "_test") {
		return true
	}
	return false
}

func runExplore(ctx context.Context, eng *engine.Engine, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("code explore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic code explore <query> [--json]")
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "emit JSON (structured context)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	query := strings.Join(fs.Args(), " ")
	if query == "" {
		fmt.Fprintf(stderr, "atomic code explore: query required\n")
		return 1
	}

	if err := openOrError(ctx, eng, stderr, "explore"); err != nil {
		return 1
	}

	sg, _, _, err := eng.FindRelevantContext(ctx, query, engine.ContextOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "atomic code explore: %v\n", err)
		return 1
	}

	ctx2, err := eng.BuildContext(ctx, sg, codectx.BuildOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "atomic code explore: build context: %v\n", err)
		return 1
	}

	if asJSON {
		enc, err := json.MarshalIndent(ctx2, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "atomic code explore: marshal: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(enc))
	} else {
		fmt.Fprintln(stdout, ctx2.Content)
	}
	return 0
}

// openOrError opens an existing index, printing an `atomic code index` hint
// when there is none.
func openOrError(ctx context.Context, eng *engine.Engine, stderr io.Writer, verb string) error {
	if !eng.IsInitialized() {
		fmt.Fprintf(stderr, "atomic code %s: index not initialized — run `atomic code index` first\n", verb)
		return errors.New("not initialized")
	}
	if err := eng.Open(ctx); err != nil {
		fmt.Fprintf(stderr, "atomic code %s: open: %v\n", verb, err)
		return err
	}
	return nil
}

func printSubgraph(sg types.Subgraph, asJSON bool, stdout, stderr io.Writer) int {
	if asJSON {
		enc, err := json.MarshalIndent(sg, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "atomic code: marshal: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(enc))
	} else {
		sorted := types.SubgraphSortedNodes(sg)
		for _, n := range sorted {
			fmt.Fprintf(stdout, "%s  %s  %s:%d\n", n.Kind, n.Name, n.FilePath, n.StartLine)
		}
		if len(sorted) == 0 {
			fmt.Fprintln(stdout, "(no results)")
		}
	}
	return 0
}

// EnsureGitignore is idempotent: repeated runs leave exactly one entry.
func EnsureGitignore(projectRoot string) error {
	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	entry := filepath.ToSlash(config.IndexDir("")) + "/"

	var existing string
	data, err := os.ReadFile(gitignorePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("EnsureGitignore: read: %w", err)
	}
	existing = string(data)

	for _, line := range strings.Split(existing, "\n") {
		if strings.TrimSpace(line) == entry {
			return nil // already present
		}
	}

	// A file not ending in a newline needs one, or the entry joins the last line.
	toAppend := entry + "\n"
	if len(existing) > 0 && !strings.HasSuffix(existing, "\n") {
		toAppend = "\n" + toAppend
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("EnsureGitignore: open: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(toAppend); err != nil {
		return fmt.Errorf("EnsureGitignore: write: %w", err)
	}
	return nil
}

// runMCP is the proxy by default, and the daemon itself under --daemon, which
// DefaultSpawn re-execs. Daemon mode lives on this registered verb rather than
// a separate internal one so the spawned argv always names a real Cobra
// command; an unregistered verb made Cobra reject the spawn's own flags before
// the handler ran. projectRoot and dbPath apply to proxy mode only — under
// --daemon the explicit --source/--db flags win, keeping the daemon from
// re-resolving scope from cwd.
func runMCP(ctx context.Context, projectRoot, dbPath string, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("code mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cliutil.SetUsage(fs, "atomic code mcp [--daemon --source <path> --db <path>] [--watch-interval <dur>] [--no-watch]")
	var daemon bool
	var sourceRoot, explicitDB string
	var watchInterval time.Duration
	var noWatch bool
	fs.BoolVar(&daemon, "daemon", false, "run as the daemon itself, bound to --source/--db (spawned by the proxy auto-start path)")
	fs.StringVar(&sourceRoot, "source", "", "source root to serve (requires --daemon)")
	fs.StringVar(&explicitDB, "db", "", "absolute path to the SQLite index db (requires --daemon)")
	fs.DurationVar(&watchInterval, "watch-interval", 0, "override the daemon's sync interval (default 10s)")
	fs.BoolVar(&noWatch, "no-watch", false, "disable background sync poller in the daemon")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if !daemon {
		if sourceRoot != "" || explicitDB != "" {
			fmt.Fprintln(stderr, "atomic code mcp: --source/--db require --daemon")
			return 1
		}
		opts := codemcp.WatchOptions{
			Disable:  noWatch,
			Interval: watchInterval,
		}
		if err := codemcp.RunProxy(ctx, projectRoot, dbPath, opts, nil, os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(stderr, "atomic code mcp: %v\n", err)
			return 1
		}
		return 0
	}

	if sourceRoot == "" || explicitDB == "" {
		fmt.Fprintln(stderr, "atomic code mcp: --daemon requires --source and --db")
		return 1
	}

	interval := codemcp.SyncInterval
	if noWatch {
		interval = 0
	} else if watchInterval > 0 {
		interval = watchInterval
	}

	if err := codemcp.RunDaemon(ctx, sourceRoot, explicitDB, nil, interval); err != nil {
		fmt.Fprintf(stderr, "atomic code mcp: %v\n", err)
		return 1
	}
	return 0
}
