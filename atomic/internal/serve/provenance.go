package serve

// provenance.go — CP10: provenance DAG walk for SC12.
//
// ReadProvenance reads the YAML frontmatter of a single markdown file and
// returns:
//   - For concern pages (reflects: key present): the cited ids + stamped fps.
//   - For knowledge pages (sources: key present): the bucket file paths + stamped sha256.
//   - For any other page: empty provenance (no error).
//
// BuildProvenanceDAG walks wiki/concerns/ and wiki/knowledge/ under wikiDir,
// reads provenance from each, resolves fingerprints against live content, and
// returns a ProvenanceDAG whose edges carry a Drift flag when the stamped
// fingerprint diverges from the live hash.
//
// Read-only: this package NEVER writes frontmatter or re-stamps anything.
// Hash helpers are imported from the wiki package to guarantee byte-identical
// results between stamper and reader (see wiki.FileSHA256 / wiki.ResolveFingerprint).

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// ProvKind identifies whether a provenance record is from a concern or a
// knowledge page (or is unknown/empty).
type ProvKind int

const (
	ProvUnknown   ProvKind = iota
	ProvConcern            // page carries reflects: key
	ProvKnowledge          // page carries sources: key
)

// ReflectsEntry is one entry from a concern's reflects: YAML list.
// Format in the file: "<id>@<fingerprint>".
type ReflectsEntry struct {
	// ID is the cited artifact id — either "knowledge/<topic>.md" for a knowledge
	// page, or a repo relative path for a summarized/indexed repo.
	ID string
	// StampedFP is the fingerprint recorded at stamp time.
	StampedFP string
}

// SourceEntry is one entry from a knowledge page's sources: YAML list.
// Format in the file: "<bucket>/<relpath>@<sha256hex>".
type SourceEntry struct {
	// BucketRelPath is the bucket-relative path of the source file (e.g. "research/note.txt").
	BucketRelPath string
	// StampedSHA256 is the SHA-256 hex recorded at stamp time.
	StampedSHA256 string
}

// PageProvenance is the provenance record for a single markdown file.
type PageProvenance struct {
	// Kind indicates whether this page is a concern or knowledge page.
	Kind ProvKind
	// Reflects holds entries from the reflects: key (concern pages).
	Reflects []ReflectsEntry
	// Sources holds entries from the sources: key (knowledge pages).
	Sources []SourceEntry
}

// ReadProvenance reads the YAML frontmatter from path and extracts provenance
// data. It does not error on missing frontmatter or missing keys — those are
// returned as empty slices. Only I/O and YAML parse errors propagate.
func ReadProvenance(path string) (PageProvenance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PageProvenance{}, err
	}

	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		// Malformed YAML frontmatter — treat as empty provenance (non-fatal).
		return PageProvenance{}, nil
	}
	if meta == nil {
		return PageProvenance{}, nil
	}

	prov := PageProvenance{}

	// Check for reflects: (concern page).
	if raw, ok := meta["reflects"]; ok {
		entries, ok := raw.([]any)
		if ok {
			for _, e := range entries {
				s, ok := e.(string)
				if !ok {
					continue
				}
				at := strings.LastIndex(s, "@")
				if at <= 0 || at == len(s)-1 {
					continue // malformed — skip
				}
				prov.Reflects = append(prov.Reflects, ReflectsEntry{
					ID:        s[:at],
					StampedFP: s[at+1:],
				})
			}
		}
		if len(prov.Reflects) > 0 {
			prov.Kind = ProvConcern
		}
	}

	// Check for sources: (knowledge page).
	if raw, ok := meta["sources"]; ok {
		entries, ok := raw.([]any)
		if ok {
			for _, e := range entries {
				s, ok := e.(string)
				if !ok {
					continue
				}
				at := strings.LastIndex(s, "@")
				if at <= 0 || at == len(s)-1 {
					continue // malformed — skip
				}
				prov.Sources = append(prov.Sources, SourceEntry{
					BucketRelPath: s[:at],
					StampedSHA256: s[at+1:],
				})
			}
		}
		if len(prov.Sources) > 0 && prov.Kind == ProvUnknown {
			prov.Kind = ProvKnowledge
		}
	}

	return prov, nil
}

// ProvenanceNode is a node in the provenance DAG.
type ProvenanceNode struct {
	// ID is the unique identifier for this node:
	//   - concerns: "wiki/concerns/<name>.md" (realm-root-relative)
	//   - knowledge: "wiki/knowledge/<topic>.md" (realm-root-relative)
	//   - bucket files: the BucketRelPath (e.g. "research/note.txt")
	ID string
	// Kind is "concern", "knowledge", or "bucket".
	Kind string
	// Drift is true when any edge pointing TO this node is drifted, or when
	// this node's sources are drifted (for knowledge nodes).
	Drift bool
}

// ProvenanceEdge is a directed edge in the provenance DAG.
type ProvenanceEdge struct {
	// Source is the node ID of the edge's source (e.g. a concern or knowledge page).
	Source string
	// Target is the node ID of the edge's target (a knowledge page or bucket file).
	Target string
	// Drift is true when the stamped fingerprint does not match the live hash.
	Drift bool
}

// ProvenanceDAG is the full provenance graph for a realm.
type ProvenanceDAG struct {
	Nodes []ProvenanceNode
	Edges []ProvenanceEdge
}

// BuildProvenanceDAG walks wiki/concerns/ and wiki/knowledge/ under wikiDir,
// reads provenance from each page, resolves live fingerprints, and returns the
// populated DAG with drift flags set where the stamped value diverges.
//
// root is the realm root (parent of wikiDir). wikiDir is the wiki/ directory.
//
// This function is read-only — it never writes any file.
func BuildProvenanceDAG(root, wikiDir string) ProvenanceDAG {
	// Use heap-allocated nodes in a map so pointer mutation is safe across appends.
	nodeByID := make(map[string]*ProvenanceNode)
	var edges []ProvenanceEdge

	ensureNode := func(id, kind string) *ProvenanceNode {
		if n, ok := nodeByID[id]; ok {
			return n
		}
		n := &ProvenanceNode{ID: id, Kind: kind}
		nodeByID[id] = n
		return n
	}

	// ── Walk wiki/concerns/ ──────────────────────────────────────────────────
	concernsDir := filepath.Join(wikiDir, "concerns")
	concernFiles := globMDFiles(concernsDir)

	for _, fp := range concernFiles {
		prov, err := ReadProvenance(fp)
		if err != nil || len(prov.Reflects) == 0 {
			continue
		}

		// Realm-relative id for the concern node (e.g. "wiki/concerns/c.md").
		concernID := realmRelative(root, fp)
		concernNode := ensureNode(concernID, "concern")

		for _, entry := range prov.Reflects {
			// Determine the live fingerprint.
			// knowledge/ ids: resolveRoot = wikiDir.
			// repo ids: resolveRoot = root.
			resolveRoot := root
			if strings.HasPrefix(entry.ID, "knowledge/") && strings.HasSuffix(entry.ID, ".md") {
				resolveRoot = wikiDir
			}
			liveFP, ok := wiki.ResolveFingerprint(resolveRoot, entry.ID)
			drifted := !ok || liveFP != entry.StampedFP

			// Target node id: knowledge page → realm-relative; otherwise use entry.ID.
			targetID := entry.ID
			if strings.HasPrefix(entry.ID, "knowledge/") {
				// Make it realm-relative: "wiki/knowledge/<topic>.md"
				targetID = realmRelative(root, filepath.Join(wikiDir, entry.ID))
			}
			targetKind := "knowledge"
			if !strings.HasPrefix(entry.ID, "knowledge/") {
				targetKind = "repo"
			}
			targetNode := ensureNode(targetID, targetKind)
			if drifted {
				targetNode.Drift = true
				concernNode.Drift = true
			}

			edges = append(edges, ProvenanceEdge{
				Source: concernID,
				Target: targetID,
				Drift:  drifted,
			})
		}
	}

	// ── Walk wiki/knowledge/ ─────────────────────────────────────────────────
	knowledgeDir := filepath.Join(wikiDir, "knowledge")
	knowledgeFiles := globMDFiles(knowledgeDir)

	for _, fp := range knowledgeFiles {
		prov, err := ReadProvenance(fp)
		if err != nil || len(prov.Sources) == 0 {
			continue
		}

		knowledgeID := realmRelative(root, fp)
		kn := ensureNode(knowledgeID, "knowledge")

		for _, src := range prov.Sources {
			// The sources: entry is "<bucket>/<relpath>@<sha256>".
			// The actual file lives at <root>/<BucketRelPath>.
			bucketAbs := filepath.Join(root, filepath.FromSlash(src.BucketRelPath))
			liveHash, hashErr := wiki.FileSHA256(bucketAbs)
			drifted := hashErr != nil || liveHash != src.StampedSHA256

			bucketNode := ensureNode(src.BucketRelPath, "bucket")
			if drifted {
				bucketNode.Drift = true
				kn.Drift = true
			}

			edges = append(edges, ProvenanceEdge{
				Source: knowledgeID,
				Target: src.BucketRelPath,
				Drift:  drifted,
			})
		}
	}

	// Flatten the node map into a slice (order is non-deterministic but stable
	// per build for test purposes; sort if needed by callers).
	dag := ProvenanceDAG{Edges: edges}
	for _, n := range nodeByID {
		dag.Nodes = append(dag.Nodes, *n)
	}
	return dag
}

// globMDFiles returns all *.md files under dir (non-recursive, single level).
// Returns nil when dir does not exist.
func globMDFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// realmRelative converts an absolute path to a realm-root-relative path using
// forward slashes. Returns the absolute path unchanged on error.
func realmRelative(root, absPath string) string {
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return absPath
	}
	return filepath.ToSlash(rel)
}
