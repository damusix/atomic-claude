package resolution

// Matching SQL identifiers found in host-language string literals against
// already-indexed SQL objects. Contract: docs/spec/sql-string-match.md.
//
// Two passes, in order. Pass A matches bare names against object names and
// records which tables each owner touched. Pass B needs that map: a column
// name alone matches nothing useful, so a bare column resolves only within
// the tables its owner already referenced, and an unanchored one gets no edge.
//
// Every ref neither pass consumed is deleted, so no speculative rows survive.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// sqlObjectKinds are the node kinds pass A matches bare names against.
var sqlObjectKinds = []types.NodeKind{
	types.NodeKindTable,
	types.NodeKindView,
	types.NodeKindProcedure,
	types.NodeKindFunction,
}

// anchorKinds scope pass B's bare columns. Procedures and functions are
// excluded: they own no columns.
var anchorKinds = map[types.NodeKind]bool{
	types.NodeKindTable: true,
	types.NodeKindView:  true,
}

// demoteConfidence drops a fragment ref one tier. A fragment is a partial
// statement, so the same match is weaker evidence than from a whole one.
func demoteConfidence(tier string) string {
	switch tier {
	case "high":
		return "medium"
	case "medium":
		return "low"
	default:
		return "low"
	}
}

// resolveSQLStringRefs runs both passes and the cleanup, returning the
// owner→anchor map and the number of edges created.
func (p *Pipeline) resolveSQLStringRefs(ctx context.Context) (map[string][]types.Node, int, error) {
	stringRefs, err := p.db.GetUnresolvedRefsByKind(ctx, types.ReferenceKindSQLString)
	if err != nil {
		return nil, 0, err
	}

	// Fragments take the same two passes, differing only in the final tier.
	fragmentRefs, err := p.db.GetUnresolvedRefsByKind(ctx, types.ReferenceKindSQLFragment)
	if err != nil {
		return nil, 0, err
	}

	if len(stringRefs) == 0 && len(fragmentRefs) == 0 {
		return nil, 0, nil
	}

	refs := append(append([]types.UnresolvedReference{}, stringRefs...), fragmentRefs...)

	// One fetch per kind, held in memory for the whole pass — never a per-ref
	// DB round trip.
	byName := make(map[string][]types.Node)
	for _, kind := range sqlObjectKinds {
		nodes, err := p.db.GetNodesByKind(ctx, kind)
		if err != nil {
			return nil, 0, err
		}
		for _, n := range nodes {
			if n.Language != types.LanguageSQL {
				continue
			}
			key := strings.ToLower(n.Name)
			byName[key] = append(byName[key], n)
		}
	}

	ownerAnchors := make(map[string][]types.Node)
	var edges []types.Edge
	var consumedIDs []string
	var leftoverIDs []string
	var dottedRefs []types.UnresolvedReference
	var dotlessLeftovers []types.UnresolvedReference

	for _, ref := range refs {
		// Deferred to pass B, which needs the map pass A is still building.
		if strings.Contains(ref.ReferenceName, ".") {
			dottedRefs = append(dottedRefs, ref)
			continue
		}

		candidates := byName[strings.ToLower(ref.ReferenceName)]
		switch {
		case len(candidates) == 0:
			dotlessLeftovers = append(dotlessLeftovers, ref)
		case len(candidates) > 3:
			// Too ambiguous to be evidence of anything; drop it.
			leftoverIDs = append(leftoverIDs, ref.ID)
		default:
			confidence := "medium"
			if standalone.IsQueryBuilderCallee(ref.CalleeExpr) {
				confidence = "high"
			}
			if ref.ReferenceKind == types.ReferenceKindSQLFragment {
				confidence = demoteConfidence(confidence)
			}
			meta, merr := json.Marshal(map[string]string{"confidence": confidence})
			if merr != nil {
				return nil, 0, merr
			}
			for _, n := range candidates {
				edges = append(edges, types.Edge{
					Source:     ref.FromNodeID,
					Target:     n.ID,
					Kind:       types.EdgeKindReferences,
					Provenance: "string-match",
					Metadata:   meta,
					Line:       ref.Line,
					Column:     ref.Column,
				})
				if anchorKinds[n.Kind] {
					ownerAnchors[ref.FromNodeID] = append(ownerAnchors[ref.FromNodeID], n)
				}
			}
			consumedIDs = append(consumedIDs, ref.ID)
		}
	}

	// Pass B: columns, qualified and anchored-bare.
	columnByQName := make(map[string][]types.Node)
	if len(dottedRefs) > 0 || len(dotlessLeftovers) > 0 {
		columns, err := p.db.GetNodesByKind(ctx, types.NodeKindColumn)
		if err != nil {
			return nil, 0, err
		}
		for _, n := range columns {
			if n.Language != types.LanguageSQL {
				continue
			}
			key := strings.ToLower(n.QualifiedName)
			columnByQName[key] = append(columnByQName[key], n)
		}
	}

	for _, ref := range dottedRefs {
		dot := strings.Index(ref.ReferenceName, ".")
		x := ref.ReferenceName[:dot]
		y := ref.ReferenceName[dot+1:]

		var tableCandidates []types.Node
		for _, n := range byName[strings.ToLower(x)] {
			if anchorKinds[n.Kind] {
				tableCandidates = append(tableCandidates, n)
			}
		}

		if len(tableCandidates) == 0 || len(tableCandidates) > 3 {
			leftoverIDs = append(leftoverIDs, ref.ID)
			continue
		}

		var colEdges []types.Edge
		for _, tbl := range tableCandidates {
			colQName := strings.ToLower(tbl.QualifiedName) + "." + strings.ToLower(y)
			for _, col := range columnByQName[colQName] {
				confidence := "medium"
				if ref.ReferenceKind == types.ReferenceKindSQLFragment {
					confidence = demoteConfidence(confidence)
				}
				meta, merr := json.Marshal(map[string]string{"confidence": confidence})
				if merr != nil {
					return nil, 0, merr
				}
				colEdges = append(colEdges, types.Edge{
					Source:     ref.FromNodeID,
					Target:     col.ID,
					Kind:       types.EdgeKindReferences,
					Provenance: "string-match",
					Metadata:   meta,
					Line:       ref.Line,
					Column:     ref.Column,
				})
			}
		}

		if len(colEdges) == 0 {
			leftoverIDs = append(leftoverIDs, ref.ID)
			continue
		}
		edges = append(edges, colEdges...)
		consumedIDs = append(consumedIDs, ref.ID)
	}

	for _, ref := range dotlessLeftovers {
		anchors := ownerAnchors[ref.FromNodeID]
		if len(anchors) == 0 {
			leftoverIDs = append(leftoverIDs, ref.ID)
			continue
		}

		seen := make(map[string]bool)
		var colEdges []types.Edge
		for _, anchor := range anchors {
			colQName := strings.ToLower(anchor.QualifiedName) + "." + strings.ToLower(ref.ReferenceName)
			for _, col := range columnByQName[colQName] {
				if seen[col.ID] {
					continue
				}
				seen[col.ID] = true
				confidence := "low"
				if standalone.IsQueryBuilderCallee(ref.CalleeExpr) {
					confidence = "medium"
				}
				if ref.ReferenceKind == types.ReferenceKindSQLFragment {
					confidence = demoteConfidence(confidence)
				}
				meta, merr := json.Marshal(map[string]string{"confidence": confidence})
				if merr != nil {
					return nil, 0, merr
				}
				colEdges = append(colEdges, types.Edge{
					Source:     ref.FromNodeID,
					Target:     col.ID,
					Kind:       types.EdgeKindReferences,
					Provenance: "string-match",
					Metadata:   meta,
					Line:       ref.Line,
					Column:     ref.Column,
				})
			}
		}

		if len(colEdges) == 0 {
			leftoverIDs = append(leftoverIDs, ref.ID)
			continue
		}
		edges = append(edges, colEdges...)
		consumedIDs = append(consumedIDs, ref.ID)
	}

	toDelete := append(append([]string{}, consumedIDs...), leftoverIDs...)

	if err := p.db.WithTx(ctx, func(tx *db.Tx) error {
		for _, e := range edges {
			if _, err := tx.InsertEdge(ctx, e); err != nil {
				return err
			}
		}
		return tx.DeleteUnresolvedRefsByIDs(ctx, toDelete)
	}); err != nil {
		return nil, 0, err
	}

	return ownerAnchors, len(edges), nil
}
