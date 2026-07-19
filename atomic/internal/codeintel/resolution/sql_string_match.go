package resolution

// sql_string_match.go — sql-string-match resolution pass A (C2), pass B
// (C3), the query-builder vocabulary check (C4), the C8 fragment tier, and
// speculative-ref cleanup (C5).
//
// Pass A runs as a batch step after ResolveAndPersistBatched's standard
// per-ref loop completes (sql_string/sql_fragment refs are skipped by that
// loop per C1/C8). It matches dotless refs against already-indexed SQL
// object names (table/view/procedure/function) using one bulk fetch per
// kind, loaded into an in-memory lowercase-name map for the whole pass —
// never a per-ref DB round trip. It returns the owner→anchor map
// (FromNodeID → matched table/view nodes) that pass B consumes for
// anchored bare-column matching. Only table/view matches count as anchors —
// procedure/function matches do not anchor column resolution.
//
// Pass B runs after pass A and handles columns, which never match on their
// own: a qualified ref ("x.y") resolves x against the same table/view
// candidates pass A uses, then matches y against that table's columns by
// QualifiedName; a bare ref left over from pass A is matched against the
// columns of its owner's pass-A anchors only — no anchor, no edge. A bare
// column match is upgraded low → medium when the ref's CalleeExpr is in the
// C4 vocabulary.
//
// C8: sql_fragment refs run through passes A and B exactly like sql_string
// refs — same object/column matching, same vocabulary upgrade, same anchor
// contribution — then have their computed tier demoted one notch
// (high → medium, medium → low, low stays low) via demoteConfidence.
//
// C5 cleanup: every ref (either kind) neither pass consumed (ambiguous
// matches, refs with zero candidates, unanchored bare columns) is deleted.
// Post-resolution, zero sql_string and zero sql_fragment rows remain.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// sqlObjectKinds are the node kinds pass A matches sql_string refs against.
var sqlObjectKinds = []types.NodeKind{
	types.NodeKindTable,
	types.NodeKindView,
	types.NodeKindProcedure,
	types.NodeKindFunction,
}

// anchorKinds are the subset of sqlObjectKinds that count as anchors for
// pass B's (C3) bare-column scoping.
var anchorKinds = map[types.NodeKind]bool{
	types.NodeKindTable: true,
	types.NodeKindView:  true,
}

// demoteConfidence applies the C8 one-notch demotion sql_fragment refs get
// after passes A/B compute their tier the same way a sql_string ref would:
// high → medium, medium → low, low stays low.
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

// resolveSQLStringRefs is the sql_string/sql_fragment batch step: pass A
// object-name matching (C2), pass B qualified/anchored column matching
// (C3), vocabulary-driven confidence tiering (C4), the C8 fragment-tier
// one-notch demotion, and cleanup of every ref left unconsumed by either
// pass (C5). Returns the owner→anchor map and the total edge count created.
func (p *Pipeline) resolveSQLStringRefs(ctx context.Context) (map[string][]types.Node, int, error) {
	stringRefs, err := p.db.GetUnresolvedRefsByKind(ctx, types.ReferenceKindSQLString)
	if err != nil {
		return nil, 0, err
	}

	// C8: sql_fragment refs run through the exact same passes A/B as
	// sql_string refs, then get their computed tier demoted one notch.
	fragmentRefs, err := p.db.GetUnresolvedRefsByKind(ctx, types.ReferenceKindSQLFragment)
	if err != nil {
		return nil, 0, err
	}

	if len(stringRefs) == 0 && len(fragmentRefs) == 0 {
		return nil, 0, nil
	}

	refs := append(append([]types.UnresolvedReference{}, stringRefs...), fragmentRefs...)

	// Bulk fetch, one call per candidate kind, filtered to Language == SQL,
	// loaded once into an in-memory lowercase-name map reused for the whole
	// pass.
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
		// Dotted refs are pass B's (C3) qualified-form job — deferred until
		// pass A finishes so the object-name map above is available to it.
		if strings.Contains(ref.ReferenceName, ".") {
			dottedRefs = append(dottedRefs, ref)
			continue
		}

		candidates := byName[strings.ToLower(ref.ReferenceName)]
		switch {
		case len(candidates) == 0:
			dotlessLeftovers = append(dotlessLeftovers, ref)
		case len(candidates) > 3:
			// Ambiguity cap: no edges, ref dropped.
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

	// Pass B (C3): qualified and anchored-bare column matching. Runs after
	// pass A so it can consume both the object-name map (for resolving the
	// "x" in "x.y") and the owner→anchor map pass A just built.
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
