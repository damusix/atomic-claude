package indexer_test

// Zero-phantom-edge integrity bar over the committed multilang fixture corpus.

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// fixturesDir walks up from this file to locate the committed corpus.
func fixturesDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed — cannot locate fixtures dir")
	}
	// thisFile = .../atomic/internal/codeintel/indexer/embedded_sql_integrity_test.go
	// Walk up 4 levels: indexer → codeintel → internal → atomic → repo root.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	dir := filepath.Join(repoRoot, "scripts", "code-eval", "fixtures", "embedded-sql-multilang")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("fixtures dir not found at %s: %v", dir, err)
	}
	return dir
}

// copyFixturesToTempDir stages the corpus in a real git repo, since discovery
// runs through git ls-files.
func copyFixturesToTempDir(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	initGitRepo(t, dst)

	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		writeFile(t, dst, rel, string(data))
		return nil
	})
	if err != nil {
		t.Fatalf("copy fixtures: %v", err)
	}

	gitAdd(t, dst, ".")
	gitCommit(t, dst, "multilang corpus")
	return dst
}

// TestEmbeddedSQLIntegrityMultilang asserts a non-empty index, at least one
// embedded edge, both endpoints of every embedded edge present in the nodes
// set, and table nodes from two or more fixture files — so a single language
// carrying the whole corpus cannot pass it.
func TestEmbeddedSQLIntegrityMultilang(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	src := fixturesDir(t)
	corpusDir := copyFixturesToTempDir(t, src)

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, corpusDir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// WHY: a vacuous pass (0 nodes, 0 edges) means the indexer silently failed;
	// the zero-dangling assertion would trivially pass on an empty index.
	allNodes, err := database.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}
	if len(allNodes) == 0 {
		t.Fatal("FAIL : index produced 0 nodes — indexer did not process the multilang corpus (vacuous pass guard)")
	}
	t.Logf("total nodes in index: %d", len(allNodes))

	// Build a node-ID set for O(1) membership check (per project convention:
	// map[T]bool over slice for set lookups).
	nodeIDs := make(map[string]bool, len(allNodes))
	for _, n := range allNodes {
		nodeIDs[n.ID] = true
	}

	// WHY: proves the embedded-SQL post-pass fired at least once across the
	// multilang corpus. If this is 0, the routing for new languages is broken.
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatal("FAIL : GetEdgesByProvenance(embedded) returned 0 — embedded-SQL post-pass did not fire on any new-language file in the multilang corpus")
	}
	t.Logf("embedded edges: %d", len(embeddedEdges))

	// Every embedded edge must have BOTH source AND target in the nodes set.
	// Collect ALL dangling edges before failing so the error message is complete.
	type danglingEdge struct {
		edge          types.Edge
		missingSource bool
		missingTarget bool
	}
	var dangling []danglingEdge
	for _, e := range embeddedEdges {
		ms := !nodeIDs[e.Source]
		mt := !nodeIDs[e.Target]
		if ms || mt {
			dangling = append(dangling, danglingEdge{edge: e, missingSource: ms, missingTarget: mt})
		}
	}
	// WHY: a dangling endpoint means a table node or owning node was emitted
	// with an ID that was never written to the nodes table — a pipeline bug
	// that would cause ghost references in call-graph queries.
	if len(dangling) != 0 {
		t.Errorf("FAIL : %d embedded edge(s) have phantom endpoints:", len(dangling))
		for _, d := range dangling {
			t.Errorf("  edge id=%d kind=%s source=%s (missing=%v) target=%s (missing=%v)",
				d.edge.ID, d.edge.Kind, d.edge.Source, d.missingSource, d.edge.Target, d.missingTarget)
		}
		t.FailNow()
	}
	t.Logf("zero-phantom-edge bar: PASS (%d embedded edges, all endpoints present)", len(embeddedEdges))

	// WHY: proves extraction fired for at least 2 different host languages, not
	// just one language dominating the results (e.g. only Java, only Ruby).
	filesWithTableNodes := make(map[string]bool)
	for _, n := range allNodes {
		if n.Kind == types.NodeKindTable {
			filesWithTableNodes[n.FilePath] = true
		}
	}
	if len(filesWithTableNodes) < 2 {
		t.Errorf("FAIL : table nodes found in %d fixture file(s), want ≥2 (at least 2 distinct host languages must contribute): files=%v",
			len(filesWithTableNodes), mapKeys(filesWithTableNodes))
	} else {
		t.Logf("table nodes from %d distinct files: %v", len(filesWithTableNodes), mapKeys(filesWithTableNodes))
	}
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
