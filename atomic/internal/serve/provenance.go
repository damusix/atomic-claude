package serve

// The provenance DAG: which concern cites which knowledge page, and which
// bucket file each knowledge page was synthesized from, with a Drift flag
// wherever a stamped fingerprint no longer matches live content.
//
// Strictly read-only — nothing here re-stamps. Hashing goes through the wiki
// package so stamper and reader cannot disagree byte-for-byte.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// ProvKind identifies which provenance key a page carries.
type ProvKind int

const (
	ProvUnknown   ProvKind = iota
	ProvConcern            // page carries reflects:
	ProvKnowledge          // page carries sources:
)

// ReflectsEntry is one "<id>@<fingerprint>" entry from a concern's reflects:
// list.
type ReflectsEntry struct {
	// ID is either "knowledge/<topic>.md" or a repo-relative path.
	ID string
	// StampedFP is the fingerprint recorded at stamp time.
	StampedFP string
}

// SourceEntry is one "<bucket>/<relpath>@<sha256hex>" entry from a knowledge
// page's sources: list.
type SourceEntry struct {
	BucketRelPath string
	StampedSHA256 string
}

// PageProvenance is one markdown file's provenance record.
type PageProvenance struct {
	// Kind names which key the page carried.
	Kind ProvKind
	// Reflects is populated for a concern page.
	Reflects []ReflectsEntry
	// Sources is populated for a knowledge page.
	Sources []SourceEntry
}

// ReadProvenance extracts provenance from path's frontmatter. Missing
// frontmatter or keys yield an empty record; only I/O errors propagate.
func ReadProvenance(path string) (PageProvenance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PageProvenance{}, err
	}

	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		// Malformed frontmatter is not fatal — the page simply has none.
		return PageProvenance{}, nil
	}
	if meta == nil {
		return PageProvenance{}, nil
	}

	prov := PageProvenance{}

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

// ProvenanceNode is a page or bucket file in the DAG.
type ProvenanceNode struct {
	// ID is realm-relative for pages, bucket-relative for source files.
	ID string
	// Kind is "concern", "knowledge", "repo", or "bucket".
	Kind string
	// Drift is set when any edge into this node drifted.
	Drift bool
}

// ProvenanceEdge is a citation from a page to what it was built from.
type ProvenanceEdge struct {
	Source string
	Target string
	// Drift is set when the stamped fingerprint no longer matches the live hash.
	Drift bool
}

// ProvenanceDAG is the full provenance graph for a realm.
type ProvenanceDAG struct {
	Nodes []ProvenanceNode
	Edges []ProvenanceEdge
}

// BuildProvenanceDAG walks the realm's concerns and knowledge pages, resolving
// each stamped fingerprint against live content. root is the parent of wikiDir.
func BuildProvenanceDAG(root, wikiDir string) ProvenanceDAG {
	// Pointers, so a later drift flag reaches a node already recorded.
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

	concernsDir := filepath.Join(wikiDir, "concerns")
	concernFiles := globMDFiles(concernsDir)

	for _, fp := range concernFiles {
		prov, err := ReadProvenance(fp)
		if err != nil || len(prov.Reflects) == 0 {
			continue
		}

		concernID := realmRelative(root, fp)
		concernNode := ensureNode(concernID, "concern")

		for _, entry := range prov.Reflects {
			// A knowledge id resolves inside wiki/; a repo id against the realm.
			resolveRoot := root
			if strings.HasPrefix(entry.ID, "knowledge/") && strings.HasSuffix(entry.ID, ".md") {
				resolveRoot = wikiDir
			}
			liveFP, ok := wiki.ResolveFingerprint(resolveRoot, entry.ID)
			drifted := !ok || liveFP != entry.StampedFP

			targetID := entry.ID
			if strings.HasPrefix(entry.ID, "knowledge/") {
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

	// Node order is map-iteration order; callers needing determinism sort.
	dag := ProvenanceDAG{Edges: edges}
	for _, n := range nodeByID {
		dag.Nodes = append(dag.Nodes, *n)
	}
	return dag
}

// globMDFiles returns the *.md files directly in dir.
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

// realmRelative returns absPath relative to root, unchanged on error.
func realmRelative(root, absPath string) string {
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return absPath
	}
	return filepath.ToSlash(rel)
}
