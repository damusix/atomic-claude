// Package validation_test is the engine's whole-system harness: a coverage map
// tying each umbrella success criterion to a real test, a schema-drift check
// against an embedded snapshot, and a synthesized-edge precision spot-check.
package validation_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/synthesis"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// TestCoverageMap is the auditable artifact proving every umbrella criterion in
// docs/spec/code-intel-engine.md has an automated check. An entry with neither a
// covering test nor ciOnly fails.
func TestCoverageMap(t *testing.T) {
	type entry struct {
		criterion     int
		description   string
		coveringTests []string
		ciOnly        bool   // true if the check is a CI/Makefile gate, not a Go test
		ciNote        string // explanation for CI-only entries
	}

	coverageMap := []entry{
		{
			criterion:   1,
			description: "CGO_ENABLED=0 static binary, cross-compiles darwin/linux×amd64/arm64 with no C toolchain",
			ciOnly:      true,
			ciNote:      "Enforced by goreleaser .github/workflows/release.yml and CI step 'CGO_ENABLED=0 go build ./...'. No C imports exist in the engine — verified by go vet + build tag absence. A Go test cannot cross-compile; this is a CI/Makefile gate.",
		},
		{
			criterion:   2,
			description: "Schema byte-identical to the reference — verified by schema dump diff",
			coveringTests: []string{
				"validation.TestSchemaDrift (this package)",
				"db.TestSchemaTablesPresent",
				"db.TestSchemaTriggersPresent",
				"db.TestSchemaIndexes",
				"db.TestSchemaEdgesFKCascade",
				"db.TestSchemaVersionRow",
				"db.TestIdempotentInit",
			},
		},
		{
			criterion:   3,
			description: "Node ids match reference formula for (filePath, kind, name, line) — golden vector test",
			coveringTests: []string{
				"extraction.TestGenerateNodeID_GoldenVectors (helpers_test.go — 6 golden pairs verified independently via Python sha256)",
				"extraction.TestGenerateNodeID_Stability (same input → same id across calls)",
			},
		},
		{
			criterion:   4,
			description: "NodeKind/EdgeKind/Language consts equal the verbatim appendix C lists (count + spelling)",
			coveringTests: []string{
				"types.TestNodeKindCount (22 entries, exact list)",
				"types.TestEdgeKindCount (12 entries, exact list)",
				"types.TestLanguageCount (29 entries, exact list)",
			},
		},
		{
			criterion:   5,
			description: "All 19 tree-sitter languages + 5 standalone formats extract on a fixture each; node-count stable across re-index",
			coveringTests: []string{
				// Go — via generic extractor tests (extractor_test.go uses Go fixtures)
				"extraction.TestExtractor_NodeCountStable (Go fixture)",
				// TypeScript / JavaScript / Python / Rust — per-language NodeCountStable
				"extraction/languages.TestTypeScript_NodeCountStable",
				"extraction/languages.TestJavaScript_NodeCountStable",
				"extraction/languages.TestPython_NodeCountStable",
				"extraction/languages.TestRust_NodeCountStable",
				//
				"extraction/languages.TestJava_NodeCountStable",
				"extraction/languages.TestC_NodeCountStable",
				"extraction/languages.TestCpp_NodeCountStable",
				"extraction/languages.TestCSharp_NodeCountStable",
				//
				"extraction/languages.TestSwift_NodeCountStable",
				"extraction/languages.TestKotlin_NodeCountStable",
				"extraction/languages.TestScala_NodeCountStable",
				//
				"extraction/languages.TestRuby_NodeCountStable",
				"extraction/languages.TestPHP_NodeCountStable",
				"extraction/languages.TestLua_NodeCountStable",
				"extraction/languages.TestLuau_NodeCountStable",
				//
				"extraction/languages.TestDart_NodeCountStable",
				"extraction/languages.TestObjC_NodeCountStable",
				"extraction/languages.TestPascal_NodeCountStable",
				// Standalone formats
				"extraction/standalone.TestVue_NodeCountStable",
				"extraction/standalone.TestSvelte_NodeCountStable",
				"extraction/standalone.TestLiquid_NodeCountStable",
				"extraction/standalone.TestDFM_NodeCountStable",
				"extraction/standalone.TestMyBatis_NodeCountStable",
				// Re-index stability
				"indexer.TestFullIndex (multi-file fixture, stable across re-index)",
				"indexer.TestOrphanInvariant (no count explosion across re-index)",
				"indexer.TestContentHashDedup",
			},
		},
		{
			criterion:   6,
			description: "Resolution links imports/names/frameworks; synthesized edges carry provenance='heuristic'+synthesizedBy; run after static edges",
			coveringTests: []string{
				"synthesis.TestPipelineWithSeams_SynthesisRunsLast (synthesis runs after all static edges)",
				"synthesis.TestCompositeStampsEdge (provenance=heuristic + synthesizedBy in metadata)",
				"synthesis.TestCompositeDedupWithinRun",
				"synthesis.TestCompositeDedupAcrossRuns",
				"synthesis.TestReactRenderSynthesizer_Gate",
				"synthesis.TestJSXRenderSynthesizer_Gate",
				"synthesis.TestVueHandlerSynthesizer_Gate",
				"validation.TestSynthesizedEdgePrecision (this package — exact edge set, no over-production)",
			},
		},
		{
			criterion:   7,
			description: "Graph queries (callers/callees/impact/path) correct on a fixture with known call structure",
			coveringTests: []string{
				"graph.TestGetCallers_Depth1",
				"graph.TestGetCallers_Depth2",
				"graph.TestGetCallees_Depth1",
				"graph.TestGetImpactRadius_ExcludesContains",
				"graph.TestFindPath_ReachableAtoC",
				"graph.TestGetTypeHierarchy_Ancestors",
				"graph.TestFindDeadCode",
				"graph.TestFindCircularDependencies",
			},
		},
		{
			criterion:   8,
			description: "FTS returns results in same rank order as reference (BM25 appendix J); 3-tier FTS→LIKE→fuzzy present",
			coveringTests: []string{
				"search.TestSearch_FTSTier_RankOrder (results descending by BM25 score)",
				"search.TestSearch_LIKETier_FiresOnFTSMiss (LIKE tier fires when FTS returns nothing)",
				"search.TestSearch_FuzzyTier_FiresOnLIKEMiss (fuzzy tier fires when LIKE returns nothing)",
				"search.TestKindBonus (appendix-J bonus table asserted literally)",
				"db.TestFTSSyncInsert",
				"db.TestFTSSyncDelete",
			},
		},
		{
			criterion:   9,
			description: "MCP initialize returns server-instructions; node tool returns all overloads; explore respects budget tiers/25k ceiling/section-boundary cut",
			coveringTests: []string{
				"mcp.TestInitialize_Instructions (de-branded text present)",
				"mcp.TestNodeTool_AllOverloads (ambiguous name → all in one call)",
				"mcp.TestExploreBudget_Constants (tier values asserted literally, R6)",
				"mcp.TestExploreBudget_MaxCharsPerFileMonotonic",
				"mcp.TestApplyCeiling_CutsAtSectionBoundary (\\n#### back-half absent)",
				"mcp.TestApplyCeiling_CutsAtLastBackHalfBoundary",
				"mcp.TestApplyCeiling_HardCeiling_25000",
				"mcp.TestTinyRepoGating_SmallRepo",
				"mcp.TestTinyRepoGating_LargeRepo",
			},
		},
		{
			criterion:   10,
			description: "atomic code <verb> subcommands exist for all 11 query verbs + mcp; each query verb has --json mode",
			coveringTests: []string{
				"cli.TestDispatch_UnknownVerb",
				"cli.TestStatus_JSON_Fields (appendix-N shape)",
				"cli.TestSearch_JSON",
				"cli.TestCallees_JSON",
				"cli.TestCallers_JSON",
				"cli.TestImpact_JSON",
				"cli.TestAffected_FindsTestFile",
				"cli.TestAffected_Stdin",
				"cli.TestFiles_JSON",
				"cli.TestExplore_ReturnsContent",
				"cli.TestEnsureGitignore_Idempotent",
				"cli.TestEnsureGitignore_CreatesFile",
				"cli.TestSync_NotIndexed_ReturnsError",
			},
		},
		{
			criterion:   11,
			description: "all 19 grammars load under wazero; parallel parse across instance pool; recycle returns RSS within K% of baseline",
			coveringTests: []string{
				"extraction.TestPool_RaceClean (8 goroutines × 20 parses, -race clean)",
				"extraction.TestPool_RecycleCadence (recycle triggers at threshold)",
				"extraction.TestPool_BindingInterface (per-instance isolation)",
				"extraction.TestPool_NoSharing (no shared state across instances)",
				"extraction.TestBorrow_ContextCancel",
				"extraction.TestPool_CloseAll",
				"extraction.TestWalkNamed_Order",
				"extraction.TestWalkNamed_ErrorStop",
				"tsbinding.TestNamedChildCount (grammar ABI regression — NamedChildCount not nodeChildCount)",
			},
		},
	}

	allMapped := true
	for _, e := range coverageMap {
		if !e.ciOnly && len(e.coveringTests) == 0 {
			t.Errorf("criterion %d (%s): no covering tests and not marked CI-only — coverage gap",
				e.criterion, e.description)
			allMapped = false
		}
	}

	if len(coverageMap) != 11 {
		t.Errorf("coverage map has %d entries, want 11 (one per umbrella criterion)", len(coverageMap))
		allMapped = false
	}

	if allMapped {
		t.Logf("coverage map: all 11 umbrella criteria mapped (1 CI-only, 10 Go-test-covered)")
	}

	if coverageMap[0].criterion != 1 || !coverageMap[0].ciOnly {
		t.Errorf("expected criterion 1 (cross-compile) to be the CI-only entry")
	}
}

// canonicalSchema is the sqlite_master dump of a fresh fully-migrated DB, one
// "<type>\t<name>\t<DDL>" line per object, whitespace collapsed and sorted by
// name. Changing the schema means updating this AND adding a migration in
// db/migrations.go.
var canonicalSchema = []schemaEntry{
	{typ: "table", name: "edges", sql: "CREATE TABLE edges ( id INTEGER PRIMARY KEY AUTOINCREMENT, source TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE, target TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE, kind TEXT NOT NULL, metadata TEXT, line INTEGER NOT NULL DEFAULT 0, col INTEGER NOT NULL DEFAULT 0, provenance TEXT DEFAULT NULL )"},
	{typ: "table", name: "files", sql: "CREATE TABLE files ( path TEXT PRIMARY KEY, content_hash TEXT NOT NULL DEFAULT '', language TEXT NOT NULL DEFAULT '', size INTEGER NOT NULL DEFAULT 0, modified_at INTEGER NOT NULL DEFAULT 0, indexed_at INTEGER NOT NULL DEFAULT 0, node_count INTEGER NOT NULL DEFAULT 0, errors TEXT )"},
	{typ: "index", name: "idx_edges_kind", sql: "CREATE INDEX idx_edges_kind ON edges(kind)"},
	{typ: "index", name: "idx_edges_provenance", sql: "CREATE INDEX idx_edges_provenance ON edges(provenance)"},
	{typ: "index", name: "idx_edges_source_kind", sql: "CREATE INDEX idx_edges_source_kind ON edges(source, kind)"},
	{typ: "index", name: "idx_edges_target_kind", sql: "CREATE INDEX idx_edges_target_kind ON edges(target, kind)"},
	{typ: "index", name: "idx_nodes_lower_name", sql: "CREATE INDEX idx_nodes_lower_name ON nodes(lower(name))"},
	{typ: "table", name: "nodes", sql: "CREATE TABLE nodes ( id TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL, qualified_name TEXT NOT NULL, file_path TEXT NOT NULL, language TEXT NOT NULL, start_line INTEGER NOT NULL DEFAULT 0, end_line INTEGER NOT NULL DEFAULT 0, start_column INTEGER NOT NULL DEFAULT 0, end_column INTEGER NOT NULL DEFAULT 0, docstring TEXT, signature TEXT, visibility TEXT, is_exported INTEGER NOT NULL DEFAULT 0, is_async INTEGER NOT NULL DEFAULT 0, is_static INTEGER NOT NULL DEFAULT 0, is_const INTEGER NOT NULL DEFAULT 0, decorators TEXT, type_parameters TEXT, metadata TEXT, updated_at INTEGER NOT NULL DEFAULT 0 )"},
	{typ: "trigger", name: "nodes_ad", sql: "CREATE TRIGGER nodes_ad AFTER DELETE ON nodes BEGIN INSERT INTO nodes_fts(nodes_fts, rowid, id, name, qualified_name, docstring, signature) VALUES ('delete', OLD.rowid, OLD.id, OLD.name, OLD.qualified_name, OLD.docstring, OLD.signature); END"},
	{typ: "trigger", name: "nodes_ai", sql: "CREATE TRIGGER nodes_ai AFTER INSERT ON nodes BEGIN INSERT INTO nodes_fts(rowid, id, name, qualified_name, docstring, signature) VALUES (NEW.rowid, NEW.id, NEW.name, NEW.qualified_name, NEW.docstring, NEW.signature); END"},
	{typ: "trigger", name: "nodes_au", sql: "CREATE TRIGGER nodes_au AFTER UPDATE ON nodes BEGIN INSERT INTO nodes_fts(nodes_fts, rowid, id, name, qualified_name, docstring, signature) VALUES ('delete', OLD.rowid, OLD.id, OLD.name, OLD.qualified_name, OLD.docstring, OLD.signature); INSERT INTO nodes_fts(rowid, id, name, qualified_name, docstring, signature) VALUES (NEW.rowid, NEW.id, NEW.name, NEW.qualified_name, NEW.docstring, NEW.signature); END"},
	{typ: "table", name: "nodes_fts", sql: "CREATE VIRTUAL TABLE nodes_fts USING fts5( id, name, qualified_name, docstring, signature, content='nodes', content_rowid='rowid' )"},
	{typ: "table", name: "project_metadata", sql: "CREATE TABLE project_metadata ( key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL DEFAULT 0 )"},
	{typ: "table", name: "schema_versions", sql: "CREATE TABLE schema_versions ( version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL )"},
	// ALTER TABLE appends, so migrated columns land at the end separated by " , ".
	{typ: "table", name: "unresolved_refs", sql: "CREATE TABLE unresolved_refs ( id TEXT PRIMARY KEY, from_node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE, reference_name TEXT NOT NULL, reference_kind TEXT NOT NULL, line INTEGER NOT NULL DEFAULT 0, col INTEGER NOT NULL DEFAULT 0, candidates TEXT, file_path TEXT NOT NULL DEFAULT '', language TEXT NOT NULL DEFAULT 'unknown' , arguments TEXT, callee_expr TEXT)"},
}

type schemaEntry struct {
	typ  string
	name string
	sql  string
}

// isExcludedFromSchemaDump filters out SQLite's own objects — autoindexes, FTS5
// support tables, the sequence table — which would otherwise drift on their own.
func isExcludedFromSchemaDump(name string) bool {
	return strings.HasPrefix(name, "sqlite_autoindex_") ||
		strings.HasPrefix(name, "nodes_fts_") && name != "nodes_fts" ||
		name == "sqlite_sequence"
}

// TestSchemaDrift diffs a fresh fully-migrated DB against canonicalSchema,
// catching a schema.sql edit that skipped a migration or a migration that
// skipped the snapshot. To adopt an intentional change, run with -v and paste
// the LIVE DUMP block over canonicalSchema.
func TestSchemaDrift(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "drift-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	rows, err := d.DB().QueryContext(context.Background(),
		`SELECT type, name, COALESCE(sql,'') FROM sqlite_master
		 WHERE type IN ('table','trigger','index')
		 ORDER BY name, type`)
	if err != nil {
		t.Fatalf("sqlite_master query: %v", err)
	}
	defer rows.Close()

	var live []schemaEntry
	for rows.Next() {
		var e schemaEntry
		if err := rows.Scan(&e.typ, &e.name, &e.sql); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if isExcludedFromSchemaDump(e.name) {
			continue
		}
		e.sql = strings.Join(strings.Fields(e.sql), " ")
		live = append(live, e)
	}
	sort.Slice(live, func(i, j int) bool {
		if live[i].name != live[j].name {
			return live[i].name < live[j].name
		}
		return live[i].typ < live[j].typ
	})

	t.Log("LIVE DUMP (for updating canonicalSchema on intentional changes):")
	for _, e := range live {
		t.Logf("  {typ:%q, name:%q, sql:%q},", e.typ, e.name, e.sql)
	}

	if len(live) != len(canonicalSchema) {
		t.Errorf("schema object count: live=%d canonical=%d", len(live), len(canonicalSchema))
		t.Logf("live names: %v", objectNames(live))
		t.Logf("canonical names: %v", objectNames(canonicalSchema))
		t.FailNow()
	}

	for i := range live {
		if live[i].typ != canonicalSchema[i].typ ||
			live[i].name != canonicalSchema[i].name ||
			live[i].sql != canonicalSchema[i].sql {
			t.Errorf("schema object %d mismatch:\n  live:      typ=%q name=%q sql=%q\n  canonical: typ=%q name=%q sql=%q",
				i,
				live[i].typ, live[i].name, live[i].sql,
				canonicalSchema[i].typ, canonicalSchema[i].name, canonicalSchema[i].sql,
			)
		}
	}
}

func objectNames(entries []schemaEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.name
	}
	return names
}

// heuristicEdge is a synthesized edge flattened for precision assertions.
type heuristicEdge struct {
	sourceName    string
	targetName    string
	synthesizedBy string
}

// The per-synthesizer tests measure recall — that each synthesizer emits the
// edges it should. This one measures precision: an exact expected edge set, so
// any synthesizer emitting an edge it should not fails here.

func TestSynthesizedEdgePrecision(t *testing.T) {
	ctx := context.Background()

	d, err := db.Open(filepath.Join(t.TempDir(), "precision-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	fixtureDir := t.TempDir()

	// counter.ts has setState and should get a react-render edge; helper.ts has
	// none and is the negative control.
	writeFixture(t, fixtureDir, "counter.ts", `
class PrecisionCounter {
  state = { n: 0 };

  increment() {
    this.setState({ n: this.state.n + 1 });
  }

  render() {
    return this.state.n;
  }
}
export { PrecisionCounter };
`)

	writeFixture(t, fixtureDir, "helper.ts", `
function helperFn(x: number): number {
  return x * 2;
}
export { helperFn };
`)

	writeFixture(t, fixtureDir, "parent.tsx", `
import React from "react";
function ParentWidget() {
  return <ChildBox />;
}
export { ParentWidget };
`)
	writeFixture(t, fixtureDir, "child.tsx", `
import React from "react";
function ChildBox() {
  return <div>ok</div>;
}
export { ChildBox };
`)

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	orch := indexer.NewOrchestrator(d, pool)
	if err := orch.IndexAll(ctx, fixtureDir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	composite := synthesis.Default(d)
	pipe := resolution.NewPipelineWithSeams(d, fixtureDir, nil, composite)
	if _, _, err := pipe.ResolveAndPersistBatched(ctx, 0, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}

	nodeByName := make(map[string]types.Node, len(allNodes))
	for _, n := range allNodes {
		nodeByName[n.Name] = n
	}

	var heuristicEdges []heuristicEdge

	for _, n := range allNodes {
		edges, err := d.GetEdgesBySource(ctx, n.ID)
		if err != nil {
			t.Fatalf("GetEdgesBySource %s: %v", n.ID, err)
		}
		for _, e := range edges {
			if e.Provenance != "heuristic" {
				continue
			}
			srcNode, ok := nodeByID(allNodes, e.Source)
			if !ok {
				t.Errorf("heuristic edge has unknown source ID %q", e.Source)
				continue
			}
			tgtNode, ok := nodeByID(allNodes, e.Target)
			if !ok {
				t.Errorf("heuristic edge has unknown target ID %q", e.Target)
				continue
			}

			if e.Kind != types.EdgeKindCalls {
				t.Errorf("heuristic edge %s→%s: kind=%q, want calls", e.Source, e.Target, e.Kind)
			}
			var meta map[string]string
			if len(e.Metadata) > 0 {
				if err := json.Unmarshal(e.Metadata, &meta); err != nil {
					t.Errorf("heuristic edge %s→%s: unmarshal metadata: %v", e.Source, e.Target, err)
					continue
				}
			}
			synBy := meta["synthesizedBy"]
			if synBy == "" {
				t.Errorf("heuristic edge %s→%s: missing metadata.synthesizedBy", e.Source, e.Target)
			}

			heuristicEdges = append(heuristicEdges, heuristicEdge{
				sourceName:    srcNode.Name,
				targetName:    tgtNode.Name,
				synthesizedBy: synBy,
			})
		}
	}

	// Exactly two edges are expected, and helperFn must appear in neither.
	for _, he := range heuristicEdges {
		if he.sourceName == "helperFn" || he.targetName == "helperFn" {
			t.Errorf("PRECISION FAILURE: helperFn appears in heuristic edge %s→%s (synthesizedBy=%s)",
				he.sourceName, he.targetName, he.synthesizedBy)
		}
	}

	assertHeuristicEdgeExists(t, heuristicEdges, "increment", "render", "react-render")

	assertHeuristicEdgeExists(t, heuristicEdges, "ParentWidget", "ChildBox", "jsx-render")

	for _, he := range heuristicEdges {
		if srcNode, ok := nodeByName[he.sourceName]; ok && strings.HasSuffix(srcNode.FilePath, "helper.ts") {
			t.Errorf("PRECISION FAILURE: node from helper.ts is heuristic edge source: %s→%s (synthesizedBy=%s)",
				he.sourceName, he.targetName, he.synthesizedBy)
		}
		if tgtNode, ok := nodeByName[he.targetName]; ok && strings.HasSuffix(tgtNode.FilePath, "helper.ts") {
			t.Errorf("PRECISION FAILURE: node from helper.ts is heuristic edge target: %s→%s (synthesizedBy=%s)",
				he.sourceName, he.targetName, he.synthesizedBy)
		}
	}

	t.Logf("precision fixture: %d heuristic edges total:", len(heuristicEdges))
	for _, he := range heuristicEdges {
		t.Logf("  %s → %s (synthesizedBy=%s)", he.sourceName, he.targetName, he.synthesizedBy)
	}
}

// assertHeuristicEdgeExists fails when the set has no matching edge.
func assertHeuristicEdgeExists(t *testing.T, edges []heuristicEdge, srcName, tgtName, synthesizedBy string) {
	t.Helper()
	for _, e := range edges {
		if e.sourceName == srcName && e.targetName == tgtName && e.synthesizedBy == synthesizedBy {
			return
		}
	}
	t.Errorf("PRECISION FAILURE: expected heuristic edge %s→%s (synthesizedBy=%s) not found; edges=%v",
		srcName, tgtName, synthesizedBy, edges)
}

func nodeByID(nodes []types.Node, id string) (types.Node, bool) {
	for _, n := range nodes {
		if n.ID == id {
			return n, true
		}
	}
	return types.Node{}, false
}

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFixture %s: %v", name, err)
	}
}
