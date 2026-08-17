// Package search ranks code-graph nodes through three tiers, each firing only
// when the one before it returned nothing: FTS5, case-insensitive LIKE, then
// bounded Damerau-Levenshtein fuzzy matching.
//
// Score ties break ascending by node ID, matching the db-level ORDER BY score,
// n.id so callers see one consistent ordering.
package search

import (
	"context"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

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

// dbQuerier is the subset of db.DB the search layer uses, kept narrow as a test seam.
type dbQuerier interface {
	SearchNodes(ctx context.Context, query string, limit int) ([]types.SearchResult, error)
	GetAllNodes(ctx context.Context) ([]types.Node, error)
	GetNodesByKind(ctx context.Context, kind types.NodeKind) ([]types.Node, error)
}

// ParsedQuery is a raw query split into field filters plus the FTS remainder.
type ParsedQuery struct {
	Kind     types.NodeKind
	Language types.Language
	FilePath string
	Name     string
	// FTSText is what is left after stripping all field: tokens.
	FTSText string
}

var validKinds = func() map[types.NodeKind]bool {
	m := make(map[types.NodeKind]bool, len(types.AllNodeKinds))
	for _, k := range types.AllNodeKinds {
		m[k] = true
	}
	return m
}()

var validLanguages = func() map[types.Language]bool {
	m := make(map[types.Language]bool, len(types.AllLanguages))
	for _, l := range types.AllLanguages {
		m[l] = true
	}
	return m
}()

// ParseQuery splits a raw query on the kind:/lang:/language:/path:/name:
// prefixes. Anything else — including a kind: or lang: value that does not
// validate — falls through to FTSText, so a typo still returns results.
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

// KindBonus ranks node kinds against each other; unlisted kinds score 0.
func KindBonus(k types.NodeKind) float64 {
	switch k {
	case types.NodeKindFunction, types.NodeKindMethod:
		return 10
	case types.NodeKindInterface, types.NodeKindTrait, types.NodeKindProtocol, types.NodeKindRoute:
		return 9
	case types.NodeKindClass, types.NodeKindComponent,
		types.NodeKindTable, types.NodeKindView, types.NodeKindProcedure, types.NodeKindPolicy:
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
		return 2
	case types.NodeKindVariable:
		return 2
	case types.NodeKindImport, types.NodeKindExport:
		return 1
	default:
		return 0
	}
}

// ScorePathRelevance favours shallower paths, on the assumption that a symbol
// near the repo root is more central than one buried in a subtree.
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

func isTestFile(filePath string) bool {
	lower := strings.ToLower(filePath)
	for _, seg := range strings.Split(lower, "/") {
		if seg == "test" || seg == "spec" || seg == "__tests__" {
			return true
		}
	}
	lastSlash := strings.LastIndex(lower, "/")
	filename := lower
	if lastSlash >= 0 {
		filename = lower[lastSlash+1:]
	}
	if idx := strings.LastIndex(filename, "_test."); idx >= 0 {
		return true
	}
	if strings.Contains(filename, ".test.") || strings.Contains(filename, ".spec.") {
		return true
	}
	return false
}

// applyFilters AND-combines every field filter set in pq.
func applyFilters(results []types.SearchResult, pq ParsedQuery) []types.SearchResult {
	if pq.Kind == "" && pq.Language == "" && pq.FilePath == "" && pq.Name == "" {
		return results
	}
	out := results[:0:0] // zero cap, so appends never alias the caller's array
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

// sortResults orders by descending score, breaking ties on node ID to match the
// db-level ORDER BY.
func sortResults(results []types.SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Node.ID < results[j].Node.ID
	})
}

// scoreMetadata scores a node for TierFilter, where there is no free-text term
// and therefore no bm25 base.
func scoreMetadata(n types.Node, nameFilter string) float64 {
	score := KindBonus(n.Kind) + ScorePathRelevance(n)
	if n.IsExported {
		score += 1.0
	}
	if nameFilter != "" {
		score += nameMatchBonus(n, nameFilter)
	}
	return score
}

// Searcher wraps a db handle and exposes the 3-tier search pipeline.
type Searcher struct {
	db dbQuerier
}

// New returns a Searcher backed by the given db.
func New(db dbQuerier) *Searcher {
	return &Searcher{db: db}
}

// Search runs the tiers in order and returns the ranked results alongside the
// tier that produced them. A blank query with no field filters yields nil.
func (s *Searcher) Search(ctx context.Context, opts types.SearchOptions) ([]types.SearchResult, Tier, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	pq := ParseQuery(opts.Query)

	// Filters parsed out of the raw query win; the SearchOptions fields exist
	// for programmatic callers that pass no raw query at all.
	if opts.Kind != "" && pq.Kind == "" {
		pq.Kind = opts.Kind
	}
	if opts.Language != "" && pq.Language == "" {
		pq.Language = opts.Language
	}
	if opts.FilePath != "" && pq.FilePath == "" {
		pq.FilePath = opts.FilePath
	}

	// Suppresses the test-file downrank: someone searching for "test" wants them.
	isTestQuery := strings.Contains(strings.ToLower(opts.Query), "test")

	// Metadata-only path: field filters but no free-text term to rank against.
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
		// Empty, not nil: nil is reserved for a blank query, so callers can tell
		// "found nothing" apart from "nothing was asked".
		return []types.SearchResult{}, TierFilter, nil
	}

	if pq.FTSText != "" {
		// Over-fetch, because rescoring reorders and filters can drop rows.
		ftsLimit := limit * 5
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

	likeQuery := pq.FTSText
	if likeQuery == "" {
		likeQuery = opts.Query
	}
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

func rescore(results []types.SearchResult, queryText string, isTestQuery bool) []types.SearchResult {
	out := make([]types.SearchResult, len(results))
	for i, r := range results {
		base := -r.Score // bm25 is negative and best is least-negative
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

// searchLIKE scans every node for a case-insensitive substring match on name.
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

// likeScore returns the match score before bonuses, or 0 for no match.
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

// searchFuzzy compares the query against every node name by bounded
// Damerau-Levenshtein distance. Short queries get a tighter bound, since one
// edit on a 4-character name is already a different word.
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
		// Normalised into (0,1] so closer matches rank higher.
		base := float64(maxDist+1-dist) / float64(maxDist+1)
		score := base + KindBonus(n.Kind) + ScorePathRelevance(n)
		if !isTestQuery && isTestFile(n.FilePath) {
			score -= 15.0
		}
		results = append(results, types.SearchResult{Node: n, Score: score})
	}

	return applyFilters(results, pq), nil
}

// boundedDL is Damerau-Levenshtein distance that returns -1 as soon as the
// distance is known to exceed maxDist, rather than filling the whole DP table.
// Bailing early is what keeps a full-corpus scan off the O(n·m) cliff.
func boundedDL(a, b string, maxDist int) int {
	la, lb := len(a), len(b)

	// A length gap alone already puts the distance out of bounds.
	diff := la - lb
	if diff < 0 {
		diff = -diff
	}
	if diff > maxDist {
		return -1
	}

	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	aLow := strings.ToLower(a)
	bLow := strings.ToLower(b)

	// Three rolling rows instead of a full table; prev2 is only needed because
	// the transposition check reaches back two rows.
	prev2 := make([]int, lb+1)
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		rowMin := curr[0]
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
			// A transposition costs 1 flat, never +cost: cost==0 would make
			// the swap free.
			if i > 1 && j > 1 && aLow[i-1] == bLow[j-2] && aLow[i-2] == bLow[j-1] {
				if t := prev2[j-2] + 1; t < curr[j] {
					curr[j] = t
				}
			}
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		// Row minima never decrease, so this row already settles the bound.
		if rowMin > maxDist {
			return -1
		}
		prev2, prev, curr = prev, curr, prev2
	}

	return prev[lb]
}

// searchFilter narrows by kind at the db when it can — the only filter with a
// targeted query — and falls back to a full scan filtered in memory.
func (s *Searcher) searchFilter(ctx context.Context, pq ParsedQuery, isTestQuery bool) ([]types.SearchResult, error) {
	var nodes []types.Node
	var err error

	switch {
	case pq.Kind != "":
		nodes, err = s.db.GetNodesByKind(ctx, pq.Kind)
	default:
		nodes, err = s.db.GetAllNodes(ctx)
	}
	if err != nil {
		return nil, err
	}

	raw := make([]types.SearchResult, len(nodes))
	for i, n := range nodes {
		raw[i] = types.SearchResult{Node: n}
	}
	filtered := applyFilters(raw, pq)

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
