// Package search implements the 3-tier FTS→LIKE→fuzzy node search for the
// code-intelligence engine (master CP18, appendix J).
//
// # Entry point
//
// New(db) returns a Searcher. Call Search(ctx, SearchOptions) to execute the
// full pipeline: query parsing → FTS (tier 1) → LIKE fallback (tier 2) →
// Levenshtein fuzzy fallback (tier 3) → field filters → rescore → stable sort.
//
// # Tier ordering
//
// Tier 1 (FTS5): calls db.SearchNodes with the FTS text, over-fetches 5×, then
// rescores with kindBonus + scorePathRelevance + nameMatchBonus + test-file
// downrank. Applies field filters. If results remain → return TierFTS.
//
// Tier 2 (LIKE): fires when tier 1 returns 0 results. Case-insensitive LIKE on
// node name using the query term. Scores by CASE ladder: exact=1.0, prefix=0.9,
// contains=0.8, qualified=0.7. Applies field filters. If results remain →
// return TierLIKE.
//
// Tier 3 (Fuzzy): fires when tier 2 returns 0 results. Bounded
// Damerau-Levenshtein over all node names: maxDist = 1 if len(query)≤4 else 2.
// Early-exit per candidate when distance exceeds maxDist (avoids O(n·m) cliff).
//
// # Scoring
//
// Final score = bm25_base + kindBonus + scorePathRelevance + nameMatchBonus.
//   - bm25_base = −bm25RawScore (bm25 is negative; negate so higher=better).
//   - test-file downrank: −15 applied unless the raw query text contains "test".
//   - Stable sort: descending score; ties broken ascending by node ID.
//
// # Determinism
//
// Ties in final score are broken by node ID (ascending string comparison).
// This matches the db-level tiebreaker (ORDER BY score, n.id) so callers see
// consistent ordering.
package search

import (
	"context"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Tier constants
// ---------------------------------------------------------------------------

// Tier identifies which search tier produced the results.
type Tier int

const (
	TierFTS    Tier = iota // FTS5 full-text search
	TierLIKE               // case-insensitive LIKE fallback
	TierFuzzy              // Levenshtein fuzzy fallback
	TierFilter             // metadata-only field-filter listing (no free-text term)
)

func (t Tier) String() string {
	switch t {
	case TierFTS:
		return "fts"
	case TierLIKE:
		return "like"
	case TierFuzzy:
		return "fuzzy"
	case TierFilter:
		return "filter"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// DB interface (seam for testing)
// ---------------------------------------------------------------------------

// dbQuerier is the subset of db.DB methods the search layer uses.
type dbQuerier interface {
	SearchNodes(ctx context.Context, query string, limit int) ([]types.SearchResult, error)
	GetAllNodes(ctx context.Context) ([]types.Node, error)
	GetNodesByKind(ctx context.Context, kind types.NodeKind) ([]types.Node, error)
}

// ---------------------------------------------------------------------------
// ParsedQuery
// ---------------------------------------------------------------------------

// ParsedQuery holds the result of parsing a raw query string into structured
// fields plus an FTS text remainder.
type ParsedQuery struct {
	// Kind is set when the raw query contained "kind:<value>".
	Kind types.NodeKind
	// Language is set when the raw query contained "lang:<value>" or "language:<value>".
	Language types.Language
	// FilePath is set when the raw query contained "path:<value>".
	FilePath string
	// Name is set when the raw query contained "name:<value>".
	Name string
	// FTSText is the remainder after stripping all field: tokens.
	FTSText string
}

// validKinds is a set for O(1) validation of NodeKind values from the query.
var validKinds = func() map[types.NodeKind]bool {
	m := make(map[types.NodeKind]bool, len(types.AllNodeKinds))
	for _, k := range types.AllNodeKinds {
		m[k] = true
	}
	return m
}()

// validLanguages is a set for O(1) validation of Language values from the query.
var validLanguages = func() map[types.Language]bool {
	m := make(map[types.Language]bool, len(types.AllLanguages))
	for _, l := range types.AllLanguages {
		m[l] = true
	}
	return m
}()

// ParseQuery parses a raw query string into structured fields plus FTS text.
//
// Field prefixes (case-sensitive):
//   - "kind:<NodeKind>"    → Kind (validated against AllNodeKinds; invalid → ignored, token → FTS)
//   - "lang:<Language>"   → Language
//   - "language:<Language>" → Language
//   - "path:<substr>"     → FilePath
//   - "name:<substr>"     → Name
//
// All other tokens go to FTSText (joined with spaces).
func ParseQuery(raw string) ParsedQuery {
	var pq ParsedQuery
	var ftsParts []string

	tokens := strings.Fields(raw)
	for _, tok := range tokens {
		lower := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(lower, "kind:"):
			val := tok[len("kind:"):]
			k := types.NodeKind(strings.ToLower(val))
			if validKinds[k] {
				pq.Kind = k
			} else {
				// Invalid kind — treat as FTS text so the user still gets results.
				ftsParts = append(ftsParts, tok)
			}
		case strings.HasPrefix(lower, "lang:"):
			val := tok[len("lang:"):]
			l := types.Language(strings.ToLower(val))
			if validLanguages[l] {
				pq.Language = l
			} else {
				ftsParts = append(ftsParts, tok)
			}
		case strings.HasPrefix(lower, "language:"):
			val := tok[len("language:"):]
			l := types.Language(strings.ToLower(val))
			if validLanguages[l] {
				pq.Language = l
			} else {
				ftsParts = append(ftsParts, tok)
			}
		case strings.HasPrefix(lower, "path:"):
			pq.FilePath = tok[len("path:"):]
		case strings.HasPrefix(lower, "name:"):
			pq.Name = tok[len("name:"):]
		default:
			ftsParts = append(ftsParts, tok)
		}
	}

	pq.FTSText = strings.Join(ftsParts, " ")
	return pq
}

// ---------------------------------------------------------------------------
// Scoring helpers
// ---------------------------------------------------------------------------

// KindBonus returns the appendix-J kind bonus for a NodeKind.
// Not-listed kinds return 0.
func KindBonus(k types.NodeKind) float64 {
	switch k {
	case types.NodeKindFunction, types.NodeKindMethod:
		return 10
	case types.NodeKindInterface, types.NodeKindTrait, types.NodeKindProtocol, types.NodeKindRoute:
		return 9
	case types.NodeKindClass, types.NodeKindComponent,
		types.NodeKindTable, types.NodeKindView, types.NodeKindProcedure, types.NodeKindPolicy:
		// SQL top-level objects tier-equivalent to class/component.
		return 8
	case types.NodeKindTypeAlias, types.NodeKindStruct:
		return 6
	case types.NodeKindEnum:
		return 5
	case types.NodeKindModule, types.NodeKindNamespace:
		return 4
	case types.NodeKindProperty, types.NodeKindField, types.NodeKindConstant:
		return 3
	case types.NodeKindColumn, types.NodeKindConstraint, types.NodeKindIndex, types.NodeKindSequence, types.NodeKindTrigger:
		// SQL member/metadata objects: tier-equivalent to field/property.
		return 2
	case types.NodeKindVariable:
		return 2
	case types.NodeKindImport, types.NodeKindExport:
		return 1
	default: // file, parameter, enum_member, and anything unlisted → 0
		return 0
	}
}

// ScorePathRelevance returns a path-centrality bonus for a node. Shorter paths
// and paths with fewer directory components score higher. The result is
// deterministic: same path always returns the same value.
//
// Implementation: base 5.0 minus a penalty proportional to the number of path
// separators (directory depth). Clamped to ≥0.
func ScorePathRelevance(n types.Node) float64 {
	if n.FilePath == "" {
		return 0
	}
	depth := strings.Count(n.FilePath, "/")
	score := 5.0 - float64(depth)*0.5
	if score < 0 {
		score = 0
	}
	return score
}

// nameMatchBonus returns a bonus when the node name matches the query text.
// Exact match → 5.0; prefix → 3.0; contains → 1.0.
func nameMatchBonus(n types.Node, queryText string) float64 {
	if queryText == "" {
		return 0
	}
	nodeName := strings.ToLower(n.Name)
	q := strings.ToLower(queryText)
	switch {
	case nodeName == q:
		return 5.0
	case strings.HasPrefix(nodeName, q):
		return 3.0
	case strings.Contains(nodeName, q):
		return 1.0
	default:
		return 0
	}
}

// isTestFile returns true when the node's file path looks like a test file:
// the path contains a "test" or "spec" or "__tests__" directory segment, or
// the filename ends with "_test." (Go convention).
func isTestFile(filePath string) bool {
	lower := strings.ToLower(filePath)
	// Check path segments
	for _, seg := range strings.Split(lower, "/") {
		if seg == "test" || seg == "spec" || seg == "__tests__" {
			return true
		}
	}
	// Check filename suffix: *_test.go, *_test.ts, etc.
	// Find the last component.
	lastSlash := strings.LastIndex(lower, "/")
	filename := lower
	if lastSlash >= 0 {
		filename = lower[lastSlash+1:]
	}
	// Go: file ends with _test.<ext>
	if idx := strings.LastIndex(filename, "_test."); idx >= 0 {
		return true
	}
	// Also handle test/ files that end in .test.ts, .spec.ts etc.
	if strings.Contains(filename, ".test.") || strings.Contains(filename, ".spec.") {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Field filter
// ---------------------------------------------------------------------------

// applyFilters returns results that pass the field filters in pq.
// All filters are AND-combined.
func applyFilters(results []types.SearchResult, pq ParsedQuery) []types.SearchResult {
	if pq.Kind == "" && pq.Language == "" && pq.FilePath == "" && pq.Name == "" {
		return results
	}
	out := results[:0:0] // zero-length, shares no backing array
	for _, r := range results {
		if pq.Kind != "" && r.Node.Kind != pq.Kind {
			continue
		}
		if pq.Language != "" && r.Node.Language != pq.Language {
			continue
		}
		if pq.FilePath != "" && !strings.Contains(strings.ToLower(r.Node.FilePath), strings.ToLower(pq.FilePath)) {
			continue
		}
		if pq.Name != "" && !strings.Contains(strings.ToLower(r.Node.Name), strings.ToLower(pq.Name)) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// ---------------------------------------------------------------------------
// Stable sort
// ---------------------------------------------------------------------------

// sortResults sorts results descending by Score, with ties broken ascending by
// Node.ID (deterministic tiebreaker matching the db-level ORDER BY score, n.id).
func sortResults(results []types.SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Node.ID < results[j].Node.ID
	})
}

// ---------------------------------------------------------------------------
// Metadata scoring (no bm25 base — used by TierFilter)
// ---------------------------------------------------------------------------

// scoreMetadata computes a score for a node when there is no free-text term:
// kindBonus + pathRelevance + exportedBonus.
// If a name filter is provided the name-match bonus is included.
func scoreMetadata(n types.Node, nameFilter string) float64 {
	score := KindBonus(n.Kind) + ScorePathRelevance(n)
	// Small bonus for exported symbols (same weight as nameMatchBonus partial).
	if n.IsExported {
		score += 1.0
	}
	if nameFilter != "" {
		score += nameMatchBonus(n, nameFilter)
	}
	return score
}

// ---------------------------------------------------------------------------
// Searcher
// ---------------------------------------------------------------------------

// Searcher wraps a db handle and exposes the 3-tier search pipeline.
type Searcher struct {
	db dbQuerier
}

// New returns a Searcher backed by the given db.
func New(db dbQuerier) *Searcher {
	return &Searcher{db: db}
}

// Search executes the full 3-tier search pipeline per appendix J.
//
// Returns the ranked results, the tier that produced them (TierFTS, TierLIKE,
// or TierFuzzy), and any error.
//
// If opts.Query is empty and no field filters are set, returns nil, TierFTS, nil.
func (s *Searcher) Search(ctx context.Context, opts types.SearchOptions) ([]types.SearchResult, Tier, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20 // sensible default
	}

	pq := ParseQuery(opts.Query)

	// Merge opts-level filters with parsed-query filters. ParseQuery has priority
	// for Kind/Language/FilePath/Name; explicit SearchOptions fields may not
	// duplicate (they're the public API for programmatic callers without raw query).
	if opts.Kind != "" && pq.Kind == "" {
		pq.Kind = opts.Kind
	}
	if opts.Language != "" && pq.Language == "" {
		pq.Language = opts.Language
	}
	if opts.FilePath != "" && pq.FilePath == "" {
		pq.FilePath = opts.FilePath
	}

	// Determine if this is a test query (affects downrank decision).
	isTestQuery := strings.Contains(strings.ToLower(opts.Query), "test")

	// -----------------------------------------------------------------------
	// Metadata-only path (TierFilter): fires when there is no free-text term
	// but at least one field filter is set (kind/lang/path/name). Fetches
	// candidates by the most selective available filter, applies the remaining
	// filters in memory, and scores without bm25.
	// -----------------------------------------------------------------------
	hasFilter := pq.Kind != "" || pq.Language != "" || pq.FilePath != "" || pq.Name != ""
	if pq.FTSText == "" && hasFilter {
		filterResults, err := s.searchFilter(ctx, pq, isTestQuery)
		if err != nil {
			return nil, TierFilter, err
		}
		if len(filterResults) > 0 {
			sortResults(filterResults)
			if len(filterResults) > limit {
				filterResults = filterResults[:limit]
			}
			return filterResults, TierFilter, nil
		}
		// No matching nodes — return empty (not nil) so callers can distinguish
		// "found nothing" from "invalid query"; nil is reserved for blank queries.
		return []types.SearchResult{}, TierFilter, nil
	}

	// -----------------------------------------------------------------------
	// Tier 1: FTS5
	// -----------------------------------------------------------------------
	if pq.FTSText != "" {
		ftsLimit := limit * 5 // over-fetch 5×
		raw, err := s.db.SearchNodes(ctx, pq.FTSText, ftsLimit)
		if err != nil {
			return nil, TierFTS, err
		}

		rescored := rescore(raw, pq.FTSText, isTestQuery)
		filtered := applyFilters(rescored, pq)
		if len(filtered) > 0 {
			sortResults(filtered)
			if len(filtered) > limit {
				filtered = filtered[:limit]
			}
			return filtered, TierFTS, nil
		}
	}

	// -----------------------------------------------------------------------
	// Tier 2: LIKE fallback
	// -----------------------------------------------------------------------
	likeQuery := pq.FTSText
	if likeQuery == "" {
		likeQuery = opts.Query
	}
	// Strip any remaining field tokens to get a bare term for LIKE.
	likeQuery = strings.TrimSpace(likeQuery)

	if likeQuery != "" {
		likeResults, err := s.searchLIKE(ctx, likeQuery, pq, isTestQuery)
		if err != nil {
			return nil, TierLIKE, err
		}
		if len(likeResults) > 0 {
			sortResults(likeResults)
			if len(likeResults) > limit {
				likeResults = likeResults[:limit]
			}
			return likeResults, TierLIKE, nil
		}
	}

	// -----------------------------------------------------------------------
	// Tier 3: Fuzzy (bounded Damerau-Levenshtein)
	// -----------------------------------------------------------------------
	fuzzyQuery := likeQuery
	if fuzzyQuery != "" {
		fuzzyResults, err := s.searchFuzzy(ctx, fuzzyQuery, pq, isTestQuery)
		if err != nil {
			return nil, TierFuzzy, err
		}
		if len(fuzzyResults) > 0 {
			sortResults(fuzzyResults)
			if len(fuzzyResults) > limit {
				fuzzyResults = fuzzyResults[:limit]
			}
			return fuzzyResults, TierFuzzy, nil
		}
	}

	return nil, TierFuzzy, nil
}

// rescore takes raw FTS results and computes the final score per appendix J:
// finalScore = bm25_base + kindBonus + scorePathRelevance + nameMatchBonus
// where bm25_base = -bm25RawScore (bm25 is negative; negate so higher=better).
// Test-file downrank of −15 is applied unless isTestQuery.
func rescore(results []types.SearchResult, queryText string, isTestQuery bool) []types.SearchResult {
	out := make([]types.SearchResult, len(results))
	for i, r := range results {
		base := -r.Score // negate: bm25 is negative, best is least-negative
		score := base +
			KindBonus(r.Node.Kind) +
			ScorePathRelevance(r.Node) +
			nameMatchBonus(r.Node, queryText)

		if !isTestQuery && isTestFile(r.Node.FilePath) {
			score -= 15.0
		}

		out[i] = types.SearchResult{Node: r.Node, Score: score}
	}
	return out
}

// ---------------------------------------------------------------------------
// Tier 2: LIKE
// ---------------------------------------------------------------------------

// searchLIKE fetches all nodes via GetAllNodes and applies case-insensitive
// substring matching on the name, scoring by the CASE ladder:
// exact=1.0 / prefix=0.9 / contains=0.8 / qualified-name match=0.7.
func (s *Searcher) searchLIKE(ctx context.Context, query string, pq ParsedQuery, isTestQuery bool) ([]types.SearchResult, error) {
	all, err := s.db.GetAllNodes(ctx)
	if err != nil {
		return nil, err
	}

	lower := strings.ToLower(query)
	var results []types.SearchResult
	for _, n := range all {
		score := likeScore(n, lower)
		if score <= 0 {
			continue
		}
		if !isTestQuery && isTestFile(n.FilePath) {
			score -= 15.0
		}
		score += KindBonus(n.Kind) + ScorePathRelevance(n)
		results = append(results, types.SearchResult{Node: n, Score: score})
	}

	return applyFilters(results, pq), nil
}

// likeScore returns the raw LIKE score for a node (before bonuses), or 0 if
// no match. Score ladder per appendix J:
// exact match 1.0 / prefix 0.9 / contains 0.8 / qualified-name match 0.7.
func likeScore(n types.Node, lowerQuery string) float64 {
	nodeLower := strings.ToLower(n.Name)
	switch {
	case nodeLower == lowerQuery:
		return 1.0
	case strings.HasPrefix(nodeLower, lowerQuery):
		return 0.9
	case strings.Contains(nodeLower, lowerQuery):
		return 0.8
	case strings.Contains(strings.ToLower(n.QualifiedName), lowerQuery):
		return 0.7
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// Tier 3: Fuzzy (bounded Damerau-Levenshtein)
// ---------------------------------------------------------------------------

// searchFuzzy compares query against all node names using bounded
// Damerau-Levenshtein distance.
//
// maxDist: 1 if len(query)≤4, else 2 (appendix J).
// Candidates with distance > maxDist are excluded.
// The score for a fuzzy hit is: (maxDist+1-dist)/float64(maxDist+1) to give
// a score in (0, 1] — closer matches score higher.
func (s *Searcher) searchFuzzy(ctx context.Context, query string, pq ParsedQuery, isTestQuery bool) ([]types.SearchResult, error) {
	all, err := s.db.GetAllNodes(ctx)
	if err != nil {
		return nil, err
	}

	maxDist := 2
	if len(query) <= 4 {
		maxDist = 1
	}

	var results []types.SearchResult
	for _, n := range all {
		dist := boundedDL(query, n.Name, maxDist)
		if dist < 0 || dist > maxDist {
			continue
		}
		// Score in (0,1]: dist=0 → 1.0, dist=1 → 0.5 (maxDist=1) or 0.67 (maxDist=2)
		base := float64(maxDist+1-dist) / float64(maxDist+1)
		score := base + KindBonus(n.Kind) + ScorePathRelevance(n)
		if !isTestQuery && isTestFile(n.FilePath) {
			score -= 15.0
		}
		results = append(results, types.SearchResult{Node: n, Score: score})
	}

	return applyFilters(results, pq), nil
}

// boundedDL computes the Damerau-Levenshtein distance between a and b, with
// an early-exit bound: returns -1 if the distance exceeds maxDist without
// finishing the full DP table. This prevents the O(n·m) cost cliff (F-20).
//
// Implements the standard DP recurrence with transpositions. The "bounded"
// variant skips candidates where even the length difference exceeds maxDist.
func boundedDL(a, b string, maxDist int) int {
	la, lb := len(a), len(b)

	// Quick length check: if lengths differ by more than maxDist, distance
	// is at least |la-lb| > maxDist.
	diff := la - lb
	if diff < 0 {
		diff = -diff
	}
	if diff > maxDist {
		return -1
	}

	// Trivial cases.
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// Use lower-case comparison for robustness.
	aLow := strings.ToLower(a)
	bLow := strings.ToLower(b)

	// Standard Levenshtein DP with transpositions (Damerau).
	// Keep two rows to reduce allocations.
	// prev2: row i-2 (for transposition check)
	// prev:  row i-1
	// curr:  row i
	prev2 := make([]int, lb+1)
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		rowMin := curr[0] // track minimum value in this row for early exit
		for j := 1; j <= lb; j++ {
			cost := 1
			if aLow[i-1] == bLow[j-1] {
				cost = 0
			}
			curr[j] = minInt3(
				curr[j-1]+1,    // insert
				prev[j]+1,      // delete
				prev[j-1]+cost, // replace
			)
			// Transposition check (Damerau extension).
			// A transposition always costs 1 — independent of whether the
			// chars at position (i,j) match (which is what `cost` captures).
			// Using +cost was wrong: when cost==0 the swap would be free.
			if i > 1 && j > 1 && aLow[i-1] == bLow[j-2] && aLow[i-2] == bLow[j-1] {
				if t := prev2[j-2] + 1; t < curr[j] {
					curr[j] = t
				}
			}
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		// Early exit: if the minimum value in this row already exceeds maxDist,
		// no subsequent row can produce a smaller total distance.
		if rowMin > maxDist {
			return -1
		}
		// Rotate rows.
		prev2, prev, curr = prev, curr, prev2
	}

	return prev[lb]
}

// ---------------------------------------------------------------------------
// Tier TierFilter: metadata-only listing
// ---------------------------------------------------------------------------

// searchFilter fetches candidate nodes by the most selective available filter,
// then applies the remaining filters in memory and scores without bm25.
//
// Selectivity order: kind → (name/lang/path fallback to GetAllNodes).
// GetNodesByKind pre-filters the most common selective case; GetAllNodes covers
// lang:/path:/name: queries (applyFilters does the in-memory filtering for those).
// applyFilters enforces all remaining constraints after the initial fetch.
func (s *Searcher) searchFilter(ctx context.Context, pq ParsedQuery, isTestQuery bool) ([]types.SearchResult, error) {
	var nodes []types.Node
	var err error

	switch {
	case pq.Kind != "":
		// kind: is the most selective filter supported by a targeted DB query.
		nodes, err = s.db.GetNodesByKind(ctx, pq.Kind)
	default:
		// name:/lang:/path: only — full table scan, filter in memory via applyFilters.
		nodes, err = s.db.GetAllNodes(ctx)
	}
	if err != nil {
		return nil, err
	}

	// Wrap as SearchResult (score 0) then apply all remaining filters in memory.
	raw := make([]types.SearchResult, len(nodes))
	for i, n := range nodes {
		raw[i] = types.SearchResult{Node: n}
	}
	filtered := applyFilters(raw, pq)

	// Score without bm25.
	out := make([]types.SearchResult, 0, len(filtered))
	for _, r := range filtered {
		score := scoreMetadata(r.Node, pq.Name)
		if !isTestQuery && isTestFile(r.Node.FilePath) {
			score -= 15.0
		}
		out = append(out, types.SearchResult{Node: r.Node, Score: score})
	}
	return out, nil
}

func minInt3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
