package serve_test

// provenance_test.go — CP10 TDD: provenance DAG walk + drift detection.
//
// Test-first contract (failing → impl → passing):
//  1. ReadProvenance parses reflects: (concern) and sources: (knowledge) correctly.
//  2. ReadProvenance tolerates pages with no frontmatter and no target keys.
//  3. BuildProvenanceDAG links concern → knowledge → bucket-file.
//  4. No drift when stamped fingerprint == live hash (happy path).
//  5. Drift detected when bucket file content changes after stamping.
//  6. /graph/data includes fingerprint-class edges for the provenance chain.
//  7. A drifted edge carries the "drift" marker in its classes string.
//  8. The hash function matches wiki's stamper exactly (known-content sha256).

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/serve"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// --- helpers ------------------------------------------------------------------

// writeFM writes a file with YAML frontmatter.
func writeFM(t *testing.T, path string, meta map[string]any, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	doc, err := frontmatter.Emit(meta, body)
	if err != nil {
		t.Fatalf("emit frontmatter: %v", err)
	}
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// sha256Hex returns the lowercase hex SHA-256 of data.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// buildProvenanceRealm creates a realm with:
//
//	<root>/wiki/concerns/c.md     — reflects: ["knowledge/topic.md@<hash>"]
//	<root>/wiki/knowledge/topic.md — sources: ["research/note.txt@<sha256>"]
//	<root>/research/note.txt      — bucket file (the original content)
//
// Returns (root, bucketFilePath, knowledgeSHA256, bucketFileSHA256).
func buildProvenanceRealm(t *testing.T) (root, bucketFile, knowledgeHash, bucketHash string) {
	t.Helper()
	root = t.TempDir()

	// 1. Bucket file.
	bucketDir := filepath.Join(root, "research")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatalf("mkdir research: %v", err)
	}
	bucketContent := []byte("# Research note v1\n\nSome content.\n")
	bucketFile = filepath.Join(bucketDir, "note.txt")
	if err := os.WriteFile(bucketFile, bucketContent, 0o644); err != nil {
		t.Fatalf("write note.txt: %v", err)
	}
	bucketHash = sha256Hex(bucketContent)

	// 2. Knowledge page — sources: ["research/note.txt@<bucketHash>"]
	knowledgeDir := filepath.Join(root, "wiki", "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0o755); err != nil {
		t.Fatalf("mkdir knowledge: %v", err)
	}
	knowledgeContent := "# Topic\n\nSynthesized from research.\n"
	knowledgePath := filepath.Join(knowledgeDir, "topic.md")
	sourcesEntry := fmt.Sprintf("research/note.txt@%s", bucketHash)
	writeFM(t, knowledgePath, map[string]any{
		"title":   "topic",
		"sources": []any{sourcesEntry},
	}, knowledgeContent)

	// Compute SHA-256 of the knowledge page file as written (the stamped content).
	knowledgeBytes, err := os.ReadFile(knowledgePath)
	if err != nil {
		t.Fatalf("read knowledge page: %v", err)
	}
	knowledgeHash = sha256Hex(knowledgeBytes)

	// 3. Concern — reflects: ["knowledge/topic.md@<knowledgeHash>"]
	concernDir := filepath.Join(root, "wiki", "concerns")
	if err := os.MkdirAll(concernDir, 0o755); err != nil {
		t.Fatalf("mkdir concerns: %v", err)
	}
	reflectsEntry := fmt.Sprintf("knowledge/topic.md@%s", knowledgeHash)
	writeFM(t, filepath.Join(concernDir, "c.md"), map[string]any{
		"title":    "c",
		"reflects": []any{reflectsEntry},
	}, "## Concern body\n")

	return root, bucketFile, knowledgeHash, bucketHash
}

// --- Unit tests (no HTTP server) ----------------------------------------------

// Test 1: ReadProvenance correctly parses reflects: from a concern page.
func TestReadProvenance_ParsesReflects(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")

	hash := "abc123def456"
	writeFM(t, filepath.Join(wikiDir, "concerns", "c.md"), map[string]any{
		"title":    "test",
		"reflects": []any{fmt.Sprintf("knowledge/vendor-x.md@%s", hash)},
	}, "body\n")

	prov, err := serve.ReadProvenance(filepath.Join(wikiDir, "concerns", "c.md"))
	if err != nil {
		t.Fatalf("ReadProvenance: %v", err)
	}
	if prov.Kind != serve.ProvConcern {
		t.Errorf("Kind = %v, want ProvConcern", prov.Kind)
	}
	if len(prov.Reflects) != 1 {
		t.Fatalf("len(Reflects) = %d, want 1", len(prov.Reflects))
	}
	if prov.Reflects[0].ID != "knowledge/vendor-x.md" {
		t.Errorf("Reflects[0].ID = %q, want %q", prov.Reflects[0].ID, "knowledge/vendor-x.md")
	}
	if prov.Reflects[0].StampedFP != hash {
		t.Errorf("Reflects[0].StampedFP = %q, want %q", prov.Reflects[0].StampedFP, hash)
	}
}

// Test 2: ReadProvenance correctly parses sources: from a knowledge page.
func TestReadProvenance_ParsesSources(t *testing.T) {
	root := t.TempDir()
	knowledgeDir := filepath.Join(root, "wiki", "knowledge")

	hash := "deadbeef1234"
	writeFM(t, filepath.Join(knowledgeDir, "topic.md"), map[string]any{
		"title":   "topic",
		"sources": []any{fmt.Sprintf("research/notes.md@%s", hash)},
	}, "body\n")

	prov, err := serve.ReadProvenance(filepath.Join(knowledgeDir, "topic.md"))
	if err != nil {
		t.Fatalf("ReadProvenance: %v", err)
	}
	if prov.Kind != serve.ProvKnowledge {
		t.Errorf("Kind = %v, want ProvKnowledge", prov.Kind)
	}
	if len(prov.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(prov.Sources))
	}
	if prov.Sources[0].BucketRelPath != "research/notes.md" {
		t.Errorf("Sources[0].BucketRelPath = %q, want %q", prov.Sources[0].BucketRelPath, "research/notes.md")
	}
	if prov.Sources[0].StampedSHA256 != hash {
		t.Errorf("Sources[0].StampedSHA256 = %q, want %q", prov.Sources[0].StampedSHA256, hash)
	}
}

// Test 3: ReadProvenance tolerates pages with no frontmatter.
func TestReadProvenance_NoFrontmatter(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.md")
	if err := os.WriteFile(path, []byte("# just a body\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	prov, err := serve.ReadProvenance(path)
	if err != nil {
		t.Fatalf("ReadProvenance (no frontmatter): %v", err)
	}
	if len(prov.Reflects) != 0 || len(prov.Sources) != 0 {
		t.Errorf("expected empty provenance for a page with no frontmatter; got %+v", prov)
	}
}

// Test 4: ReadProvenance tolerates pages with frontmatter but no reflects:/sources:.
func TestReadProvenance_NoTargetKeys(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.md")
	writeFM(t, path, map[string]any{"title": "just a title"}, "body\n")

	prov, err := serve.ReadProvenance(path)
	if err != nil {
		t.Fatalf("ReadProvenance (no keys): %v", err)
	}
	if len(prov.Reflects) != 0 || len(prov.Sources) != 0 {
		t.Errorf("expected empty provenance; got %+v", prov)
	}
}

// Test 5: BuildProvenanceDAG links concern → knowledge → bucket-file.
func TestBuildProvenanceDAG_Links(t *testing.T) {
	root, _, _, _ := buildProvenanceRealm(t)
	wikiDir := filepath.Join(root, "wiki")

	dag := serve.BuildProvenanceDAG(root, wikiDir)

	// Must have edges: concern→knowledge and knowledge→bucket-file.
	if len(dag.Edges) == 0 {
		t.Fatalf("BuildProvenanceDAG returned no edges; want at least 2")
	}

	// Expect at least one concern→knowledge edge and one knowledge→bucket edge.
	var hasConcernKnowledge, hasKnowledgeBucket bool
	for _, e := range dag.Edges {
		if strings.Contains(e.Source, "concerns/") && strings.Contains(e.Target, "knowledge/") {
			hasConcernKnowledge = true
		}
		if strings.Contains(e.Source, "knowledge/") && (strings.Contains(e.Target, "research/") || strings.HasSuffix(e.Target, ".txt")) {
			hasKnowledgeBucket = true
		}
	}
	if !hasConcernKnowledge {
		t.Errorf("no concern→knowledge edge found; edges: %+v", dag.Edges)
	}
	if !hasKnowledgeBucket {
		t.Errorf("no knowledge→bucket-file edge found; edges: %+v", dag.Edges)
	}
}

// Test 6: No drift when stamped fingerprint matches live content.
func TestBuildProvenanceDAG_NoDrift(t *testing.T) {
	root, _, _, _ := buildProvenanceRealm(t)
	wikiDir := filepath.Join(root, "wiki")

	dag := serve.BuildProvenanceDAG(root, wikiDir)

	for _, e := range dag.Edges {
		if e.Drift {
			t.Errorf("unexpected drift on edge %s→%s (fingerprints match)", e.Source, e.Target)
		}
	}
	for _, n := range dag.Nodes {
		if n.Drift {
			t.Errorf("unexpected drift flag on node %q", n.ID)
		}
	}
}

// Test 7: Drift detected when bucket file content changes after stamping.
func TestBuildProvenanceDAG_DriftOnBucketChange(t *testing.T) {
	root, bucketFile, _, _ := buildProvenanceRealm(t)
	wikiDir := filepath.Join(root, "wiki")

	// Mutate the bucket file — hash will no longer match the stamped value.
	if err := os.WriteFile(bucketFile, []byte("# Research note v2 — changed\n"), 0o644); err != nil {
		t.Fatalf("update bucket file: %v", err)
	}

	dag := serve.BuildProvenanceDAG(root, wikiDir)

	// The knowledge→bucket edge should be drifted.
	var foundDrift bool
	for _, e := range dag.Edges {
		if e.Drift {
			foundDrift = true
		}
	}
	if !foundDrift {
		t.Errorf("expected at least one drifted edge after bucket file mutation; got none\nedges: %+v", dag.Edges)
	}

	// The knowledge node should be flagged.
	var knowledgeDrifted bool
	for _, n := range dag.Nodes {
		if strings.Contains(n.ID, "knowledge/") && n.Drift {
			knowledgeDrifted = true
		}
	}
	if !knowledgeDrifted {
		t.Errorf("knowledge node should be drift-flagged when its source bucket file changes; nodes: %+v", dag.Nodes)
	}
}

// Test 8: The hash function matches wiki's stamper exactly.
func TestFileSHA256_MatchesWikiStamper(t *testing.T) {
	content := []byte("# known content\nsome data\n")
	wantHex := sha256Hex(content)

	// Write to a temp file and compute via wiki.FileSHA256.
	tmp, err := os.CreateTemp("", "sha256-test-*.txt")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	tmp.Close()

	got, err := wiki.FileSHA256(tmp.Name())
	if err != nil {
		t.Fatalf("FileSHA256: %v", err)
	}
	if got != wantHex {
		t.Errorf("FileSHA256 = %q, want %q (must match stamper)", got, wantHex)
	}
}

// --- HTTP integration tests ---------------------------------------------------

// provenanceRealm builds a realm with wiki/concerns/, wiki/knowledge/ and
// wires the serve server to use it.
func buildProvenanceServerRealm(t *testing.T) (root string) {
	t.Helper()
	root, _, _, _ = buildProvenanceRealm(t)
	// Add a plain markdown file so the server has something in the link graph.
	writeFile(t, filepath.Join(root, "index.md"), "# Realm index\n")
	return root
}

// graphDataResponse is a subset of Cytoscape elements JSON for provenance tests.
type graphDataResponse struct {
	Nodes []struct {
		Data struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Type  string `json:"type"`
			Drift bool   `json:"drift"`
		} `json:"data"`
	} `json:"nodes"`
	Edges []struct {
		Data struct {
			ID     string `json:"id"`
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"data"`
		Classes string `json:"classes"`
	} `json:"edges"`
}

// Test 9: /graph/data includes fingerprint-class edges for a fresh provenance chain.
func TestGraphData_IncludesFingerprintEdges(t *testing.T) {
	root := buildProvenanceServerRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/graph/data")
	if err != nil {
		t.Fatalf("GET /graph/data: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var elems graphDataResponse
	if err := json.Unmarshal(body, &elems); err != nil {
		t.Fatalf("JSON unmarshal: %v\nbody: %s", err, body)
	}

	// Expect at least one "fingerprint" edge.
	var foundFingerprint bool
	for _, e := range elems.Edges {
		if strings.Contains(e.Classes, "fingerprint") {
			foundFingerprint = true
			break
		}
	}
	if !foundFingerprint {
		t.Errorf("expected at least one 'fingerprint' edge in /graph/data; edges: %+v", elems.Edges)
	}
}

// Test 10: /graph/data marks a drifted edge with "fingerprint drift" classes.
func TestGraphData_DriftedEdgeHasDriftClass(t *testing.T) {
	root := buildProvenanceServerRealm(t)

	// Mutate the bucket file to create drift.
	bucketFile := filepath.Join(root, "research", "note.txt")
	if err := os.WriteFile(bucketFile, []byte("changed content\n"), 0o644); err != nil {
		t.Fatalf("mutate bucket file: %v", err)
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

	var elems graphDataResponse
	if err := json.Unmarshal(body, &elems); err != nil {
		t.Fatalf("JSON unmarshal: %v\nbody: %s", err, body)
	}

	// Expect at least one "fingerprint drift" edge (space-separated classes).
	var foundDrift bool
	for _, e := range elems.Edges {
		if strings.Contains(e.Classes, "fingerprint") && strings.Contains(e.Classes, "drift") {
			foundDrift = true
			break
		}
	}
	if !foundDrift {
		t.Errorf("expected a 'fingerprint drift' edge after bucket mutation; edges: %+v", elems.Edges)
	}
}
