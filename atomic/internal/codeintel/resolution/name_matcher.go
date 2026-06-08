package resolution

// name_matcher.go — CP12 name matcher.
//
// Implements matchReference dispatch (appendix F sub-order) and findBestMatch
// scoring with CALIBRATED weights (appendix F — do not change without A/B).
//
// Sub-order (first match wins):
//  1. filePath    — ReferenceName looks like a file path (contains "/")
//  2. qualifiedName — ReferenceName contains "::" or is a dotted qualified name
//  3. methodCall  — ReferenceName contains "." (obj.method notation)
//  4. exactName   — exact case-insensitive name lookup
//  5. fuzzy       — Levenshtein-based fallback when all others miss
//
// Receiver inference (C++/Java, modest): for "receiver.method" notation in
// Java/C++, we score candidates whose QualifiedName contains a token matching
// the receiver name (case-insensitive prefix). This is a heuristic, not full
// type inference — it biases toward the right overload without requiring a
// type-resolution pass. Documented here so a reader can distinguish "we forgot
// to write code" from "we deliberately chose heuristic inference."

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Calibrated scoring weights — appendix F (COPY exactly, do NOT round or invent)
// ---------------------------------------------------------------------------

const (
	// ScoreSameFile is the bonus when the candidate is in the same file as the
	// reference site (appendix F: same-file +100).
	ScoreSameFile = 100

	// ScorePathProximityMax is the maximum score for path proximity (appendix F:
	// 0–80; closer dir = higher). Actual value is proportional to common path
	// segment depth.
	ScorePathProximityMax = 80

	// ScoreSameLanguage is the bonus when the candidate language equals the
	// reference language (appendix F: same-language +50).
	ScoreSameLanguage = 50

	// ScoreCrossLanguage is the penalty when the candidate language differs from
	// the reference language (appendix F: cross-language −80).
	ScoreCrossLanguage = -80

	// ScoreKindAffinity is the bonus for kind-affinity matches (appendix F: +25):
	//   call→function, instantiates→class, decorates→function.
	ScoreKindAffinity = 25

	// ScoreExported is the bonus for exported symbols (appendix F: exported +10).
	ScoreExported = 10
)

// MatchStrategy identifies which sub-strategy produced a result.
type MatchStrategy string

const (
	StrategyFilePath      MatchStrategy = "filePath"
	StrategyQualifiedName MatchStrategy = "qualifiedName"
	StrategyMethodCall    MatchStrategy = "methodCall"
	StrategyExactName     MatchStrategy = "exactName"
	StrategyFuzzy         MatchStrategy = "fuzzy"
)

// MatchResult is the result of a name-match attempt. Node is the best
// candidate; Score is the raw findBestMatch score; Confidence is normalised
// to [0, 1]; Strategy identifies which sub-order step produced this result.
type MatchResult struct {
	Node       types.Node
	Score      float64
	Confidence float64
	Strategy   MatchStrategy
}

// Candidate is one scored candidate node, used by GetAllCandidates.
type Candidate struct {
	Node  types.Node
	Score float64
}

// NameMatcher resolves non-import references (calls, type refs, etc.) to the
// best-matching target node using the appendix-F scoring model.
type NameMatcher struct {
	db         *db.DB
	knownNames []string // lowercased known symbol names — set once per batch via SetKnownNames
}

// NewNameMatcher constructs a NameMatcher backed by the given DB.
func NewNameMatcher(d *db.DB) *NameMatcher {
	return &NameMatcher{db: d}
}

// SetKnownNames sets the in-memory known-names slice used by byFuzzy. It must
// be called once after warmCaches and before the batch resolution loop.
// Each name must already be lowercased (warmCaches stores lower(name)).
func (nm *NameMatcher) SetKnownNames(names []string) {
	nm.knownNames = names
}

// MatchReference resolves ref to the best-matching node.
// Returns (nil, nil) when no candidate is found.
// Sub-order: filePath → qualifiedName → methodCall → exactName → fuzzy.
func (nm *NameMatcher) MatchReference(ctx context.Context, ref types.UnresolvedReference) (*MatchResult, error) {
	return nm.matchReference(ctx, ref, false)
}

// MatchReferenceNoFuzzy resolves ref using only non-fuzzy strategies
// (filePath → qualifiedName → methodCall → exactName). The fuzzy step is
// skipped entirely. Used by the pipeline for ref names longer than
// fuzzyNameLenCap to avoid generating a large variant set.
func (nm *NameMatcher) MatchReferenceNoFuzzy(ctx context.Context, ref types.UnresolvedReference) (*MatchResult, error) {
	return nm.matchReference(ctx, ref, true)
}

// matchReference is the shared implementation; skipFuzzy=true skips byFuzzy.
func (nm *NameMatcher) matchReference(ctx context.Context, ref types.UnresolvedReference, skipFuzzy bool) (*MatchResult, error) {
	name := ref.ReferenceName

	// 1. filePath strategy — name looks like a file path.
	if strings.Contains(name, "/") {
		candidates, err := nm.byFilePath(ctx, name, ref)
		if err != nil {
			return nil, err
		}
		if len(candidates) > 0 {
			best := findBestMatch(candidates, ref)
			return &MatchResult{
				Node:       best.Node,
				Score:      best.Score,
				Confidence: scoreToConfidence(best.Score),
				Strategy:   StrategyFilePath,
			}, nil
		}
	}

	// 2. qualifiedName strategy — name contains "::" or a dot-separated qualifier.
	if strings.Contains(name, "::") || isQualifiedDot(name) {
		candidates, err := nm.byQualifiedName(ctx, name, ref)
		if err != nil {
			return nil, err
		}
		if len(candidates) > 0 {
			best := findBestMatch(candidates, ref)
			return &MatchResult{
				Node:       best.Node,
				Score:      best.Score,
				Confidence: scoreToConfidence(best.Score),
				Strategy:   StrategyQualifiedName,
			}, nil
		}
	}

	// 3. methodCall strategy — name is "receiver.method" (single dot, not a
	// qualified import path).
	if receiver, method, ok := parseMethodCall(name); ok {
		candidates, err := nm.byMethodCall(ctx, receiver, method, ref)
		if err != nil {
			return nil, err
		}
		if len(candidates) > 0 {
			best := findBestMatch(candidates, ref)
			return &MatchResult{
				Node:       best.Node,
				Score:      best.Score,
				Confidence: scoreToConfidence(best.Score),
				Strategy:   StrategyMethodCall,
			}, nil
		}
	}

	// 4. exactName strategy.
	candidates, err := nm.byExactName(ctx, name, ref)
	if err != nil {
		return nil, err
	}
	if len(candidates) > 0 {
		best := findBestMatch(candidates, ref)
		return &MatchResult{
			Node:       best.Node,
			Score:      best.Score,
			Confidence: scoreToConfidence(best.Score),
			Strategy:   StrategyExactName,
		}, nil
	}

	// 5. fuzzy fallback — skipped when skipFuzzy is true (caller guards against
	// pathologically long names that would generate large variant sets).
	if !skipFuzzy {
		candidates, err = nm.byFuzzy(ctx, name, ref)
		if err != nil {
			return nil, err
		}
		if len(candidates) > 0 {
			best := findBestMatch(candidates, ref)
			return &MatchResult{
				Node:       best.Node,
				Score:      best.Score,
				Confidence: scoreToConfidence(best.Score) * 0.6, // fuzzy confidence discount
				Strategy:   StrategyFuzzy,
			}, nil
		}
	}

	return nil, nil
}

// GetAllCandidates returns all scored candidates for ref, sorted descending by
// score. Used by the node tool (MCP CP22) to surface overloads.
func (nm *NameMatcher) GetAllCandidates(ctx context.Context, ref types.UnresolvedReference) ([]Candidate, error) {
	name := ref.ReferenceName
	seen := map[string]bool{}

	var all []Candidate

	addCandidates := func(nodes []types.Node) {
		for _, n := range nodes {
			if seen[n.ID] {
				continue
			}
			seen[n.ID] = true
			all = append(all, Candidate{
				Node:  n,
				Score: computeScore(n, ref),
			})
		}
	}

	// Collect via all strategies (deduplicated by node ID).
	if strings.Contains(name, "/") {
		nodes, err := nm.byFilePath(ctx, name, ref)
		if err != nil {
			return nil, err
		}
		addCandidates(nodes)
	}

	if strings.Contains(name, "::") || isQualifiedDot(name) {
		nodes, err := nm.byQualifiedName(ctx, name, ref)
		if err != nil {
			return nil, err
		}
		addCandidates(nodes)
	}

	if _, method, ok := parseMethodCall(name); ok {
		nodes, err := nm.byMethodCall(ctx, "", method, ref)
		if err != nil {
			return nil, err
		}
		addCandidates(nodes)
	}

	nodes, err := nm.byExactName(ctx, name, ref)
	if err != nil {
		return nil, err
	}
	addCandidates(nodes)

	// Include fuzzy candidates only if no exact/method match found yet.
	if len(all) == 0 {
		nodes, err = nm.byFuzzy(ctx, name, ref)
		if err != nil {
			return nil, err
		}
		addCandidates(nodes)
	}

	// Sort descending by score.
	sortCandidates(all)
	return all, nil
}

// ---------------------------------------------------------------------------
// Strategy implementations
// ---------------------------------------------------------------------------

// byFilePath looks up a file node whose path matches the name.
func (nm *NameMatcher) byFilePath(ctx context.Context, name string, ref types.UnresolvedReference) ([]types.Node, error) {
	// Try exact file node first.
	fileNodeID := "file:" + name
	n, err := nm.db.GetNode(ctx, fileNodeID)
	if err == nil {
		return []types.Node{n}, nil
	}
	// Try extension candidates (same logic as import resolver).
	candidates := extensionCandidates(name, ref.Language)
	var results []types.Node
	for _, path := range candidates {
		node, err2 := nm.db.GetNode(ctx, "file:"+path)
		if err2 == nil {
			results = append(results, node)
		}
	}
	return results, nil
}

// byQualifiedName looks up nodes by their qualified_name column.
func (nm *NameMatcher) byQualifiedName(ctx context.Context, name string, ref types.UnresolvedReference) ([]types.Node, error) {
	// Extract the simple name (right-hand side of "::" or last dot segment).
	simpleName := qualifiedSimpleName(name)
	if simpleName == "" {
		return nil, nil
	}

	nodes, err := nm.db.GetNodesByName(ctx, simpleName, "")
	if err != nil {
		return nil, err
	}

	// Filter to those whose QualifiedName (case-insensitive) matches or contains
	// the full qualified name.
	lowerFull := strings.ToLower(name)
	var matched []types.Node
	for _, n := range nodes {
		lq := strings.ToLower(n.QualifiedName)
		if lq == lowerFull || strings.HasSuffix(lq, strings.ToLower("::"+simpleName)) ||
			strings.HasSuffix(lq, strings.ToLower("."+simpleName)) {
			matched = append(matched, n)
		}
	}
	return matched, nil
}

// byMethodCall resolves a "receiver.method" reference. It looks up all nodes
// named `method`, preferring method/function kinds. Receiver inference
// (C++/Java): if receiver is non-empty and the language is Java/C++, boost
// candidates whose QualifiedName contains a token similar to the receiver name
// (heuristic — see package comment).
func (nm *NameMatcher) byMethodCall(ctx context.Context, receiver, method string, ref types.UnresolvedReference) ([]types.Node, error) {
	nodes, err := nm.db.GetNodesByName(ctx, method, "")
	if err != nil {
		return nil, err
	}

	// Filter to method/function kinds.
	var results []types.Node
	for _, n := range nodes {
		if n.Kind == types.NodeKindMethod || n.Kind == types.NodeKindFunction {
			results = append(results, n)
		}
	}

	// Receiver inference for C++/Java: prefer candidates whose QualifiedName
	// contains a token that is a prefix-match of the receiver name.
	// This is intentionally modest — we do not traverse the type graph.
	if receiver != "" && isReceiverInferenceLanguage(ref.Language) && len(results) > 1 {
		results = receiverInferenceBias(results, receiver)
	}

	return results, nil
}

// byExactName looks up all nodes with the given name (case-insensitive).
func (nm *NameMatcher) byExactName(ctx context.Context, name string, ref types.UnresolvedReference) ([]types.Node, error) {
	return nm.db.GetNodesByName(ctx, name, "")
}

// byFuzzy finds candidates within edit distance 1 (len≤4) or 2 (len>4) per
// appendix J (same thresholds as the search tier).
//
// Algorithm: scan the warmed in-memory known-names set (set via SetKnownNames
// once per batch). Apply a length-window pre-filter (skip candidates whose
// rune-count differs from the ref name by more than maxDist — cheap pre-filter),
// then compute bounded Levenshtein distance (early-exit at maxDist+1). For each
// name within threshold, fetch its nodes from the DB exactly once. Total DB
// fetches = number of fuzzy-matched names (typically 0–2), not the number of
// edit-distance variants. This replaces the old variant×SQL approach that issued
// O(n·26^maxDist) DB probes per ref.
func (nm *NameMatcher) byFuzzy(ctx context.Context, name string, _ types.UnresolvedReference) ([]types.Node, error) {
	if len(name) == 0 || len(nm.knownNames) == 0 {
		return nil, nil
	}
	lowerName := strings.ToLower(name)
	nameRunes := utf8.RuneCountInString(lowerName)
	maxDist := 2
	if nameRunes <= 4 {
		maxDist = 1
	}

	seen := map[string]bool{}
	var results []types.Node

	for _, candidate := range nm.knownNames {
		if candidate == lowerName {
			continue // exact match — already handled by byExactName
		}
		// Length-window prune: cheap pre-filter before computing distance.
		candidateRunes := utf8.RuneCountInString(candidate)
		if abs(candidateRunes-nameRunes) > maxDist {
			continue
		}
		if LevenshteinDistance(lowerName, candidate, maxDist) > maxDist {
			continue
		}
		// Name is within threshold — fetch its nodes exactly once.
		nodes, err := nm.db.GetNodesByName(ctx, candidate, "")
		if err != nil {
			continue
		}
		for _, n := range nodes {
			if seen[n.ID] {
				continue
			}
			seen[n.ID] = true
			results = append(results, n)
		}
	}
	return results, nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ---------------------------------------------------------------------------
// findBestMatch — score all candidates and return the highest
// ---------------------------------------------------------------------------

func findBestMatch(nodes []types.Node, ref types.UnresolvedReference) Candidate {
	var best Candidate
	first := true
	for _, n := range nodes {
		s := computeScore(n, ref)
		if first || s > best.Score {
			best = Candidate{Node: n, Score: s}
			first = false
		}
	}
	return best
}

// computeScore applies the appendix-F scoring model to one candidate node.
func computeScore(n types.Node, ref types.UnresolvedReference) float64 {
	var score float64

	// Same-file bonus.
	if n.FilePath == ref.FilePath {
		score += ScoreSameFile
	} else {
		// Path proximity 0–80.
		score += PathProximityScore(ref.FilePath, n.FilePath)
	}

	// Language bonus/penalty.
	if ref.Language != types.LanguageUnknown && ref.Language != "" {
		if n.Language == ref.Language {
			score += ScoreSameLanguage
		} else {
			score += ScoreCrossLanguage
		}
	}

	// Kind affinity.
	if hasKindAffinity(ref.ReferenceKind, n.Kind) {
		score += ScoreKindAffinity
	}

	// Exported bonus.
	if n.IsExported {
		score += ScoreExported
	}

	// Line-distance adjustment: nearer line = slight boost.
	// Formula: max(0, 10 - |refLine - nodeLine| / 10)
	// The boost is at most 10 at distance 0 and decays to 0 at distance 100.
	if ref.Line > 0 && n.StartLine > 0 {
		dist := math.Abs(float64(ref.Line - n.StartLine))
		boost := math.Max(0, 10.0-dist/10.0)
		score += boost
	}

	return score
}

// ---------------------------------------------------------------------------
// PathProximityScore — exported so tests can assert the gradient
// ---------------------------------------------------------------------------

// PathProximityScore returns a score in [0, ScorePathProximityMax] based on
// how many directory segments refPath and nodePath share. Same-directory
// (including same-file) returns the maximum; no common prefix returns 0.
func PathProximityScore(refPath, nodePath string) float64 {
	if refPath == nodePath {
		return float64(ScorePathProximityMax)
	}

	refParts := strings.Split(filepath.ToSlash(filepath.Dir(refPath)), "/")
	nodeParts := strings.Split(filepath.ToSlash(filepath.Dir(nodePath)), "/")

	common := 0
	for i := 0; i < len(refParts) && i < len(nodeParts); i++ {
		if refParts[i] == nodeParts[i] {
			common++
		} else {
			break
		}
	}

	maxDepth := len(refParts)
	if len(nodeParts) > maxDepth {
		maxDepth = len(nodeParts)
	}
	if maxDepth == 0 {
		return float64(ScorePathProximityMax)
	}

	return float64(ScorePathProximityMax) * float64(common) / float64(maxDepth)
}

// ---------------------------------------------------------------------------
// Kind affinity helpers (appendix F: call→function, instantiates→class,
// decorates→function +25)
// ---------------------------------------------------------------------------

func hasKindAffinity(refKind types.EdgeKind, nodeKind types.NodeKind) bool {
	switch refKind {
	case types.EdgeKindCalls:
		return nodeKind == types.NodeKindFunction || nodeKind == types.NodeKindMethod
	case types.EdgeKindInstantiates:
		return nodeKind == types.NodeKindClass || nodeKind == types.NodeKindStruct
	case types.EdgeKindDecorates:
		return nodeKind == types.NodeKindFunction || nodeKind == types.NodeKindMethod
	}
	return false
}

// ---------------------------------------------------------------------------
// Receiver inference helpers (C++/Java — modest, heuristic-only)
// ---------------------------------------------------------------------------

// isReceiverInferenceLanguage returns true for languages where receiver
// inference is applied (Java and C++, per the spec heuristic comment).
func isReceiverInferenceLanguage(lang types.Language) bool {
	return lang == types.LanguageJava || lang == types.LanguageCpp ||
		lang == types.LanguageKotlin || lang == types.LanguageScala
}

// receiverInferenceBias reorders results so that candidates whose
// QualifiedName contains a token that is a case-insensitive prefix-match of
// the receiver name appear first. This is a stable bias — equal-bias
// candidates keep their original order.
//
// Heuristic: split QualifiedName on "::" and ".", check if any token starts
// with the first min(len(receiver), 4) characters of receiver (lowercased).
// "conn" prefix-matches "Connection", "svc" does not match "ServiceFactory"
// unless len("svc") == 3 and "servicefactory" starts with "svc" → it does.
// This is intentionally modest to avoid false positives.
func receiverInferenceBias(nodes []types.Node, receiver string) []types.Node {
	if receiver == "" {
		return nodes
	}
	prefixLen := len(receiver)
	if prefixLen > 4 {
		prefixLen = 4
	}
	recvPrefix := strings.ToLower(receiver[:prefixLen])

	// Stable partition: biased first, then rest.
	var biased, rest []types.Node
	for _, n := range nodes {
		if qualifiedNameContainsReceiverPrefix(n.QualifiedName, recvPrefix) {
			biased = append(biased, n)
		} else {
			rest = append(rest, n)
		}
	}
	return append(biased, rest...)
}

func qualifiedNameContainsReceiverPrefix(qualName, prefix string) bool {
	lq := strings.ToLower(qualName)
	// Split on "::" and ".".
	tokens := strings.FieldsFunc(lq, func(r rune) bool {
		return r == ':' || r == '.'
	})
	for _, tok := range tokens {
		if strings.HasPrefix(tok, prefix) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

// parseMethodCall parses "receiver.method" notation. Returns (receiver, method, true)
// when the name has exactly one "." and neither part is empty.
// Names with "/" are file paths (handled earlier) and not method calls.
func parseMethodCall(name string) (receiver, method string, ok bool) {
	if strings.Contains(name, "/") {
		return "", "", false
	}
	// Only single-dot names are method calls; "a.b.c" is a qualified import path.
	parts := strings.SplitN(name, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	// If the right-hand side also contains ".", it's a dotted path (qualified name).
	if strings.Contains(parts[1], ".") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// isQualifiedDot returns true if the name is a dotted qualified name
// (e.g. "pkg.Class.Method" — more than one dot).
func isQualifiedDot(name string) bool {
	if strings.Contains(name, "/") {
		return false
	}
	return strings.Count(name, ".") >= 2
}

// qualifiedSimpleName extracts the last segment from a qualified name.
func qualifiedSimpleName(name string) string {
	// "::" separator.
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		return name[idx+2:]
	}
	// "." separator.
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// ---------------------------------------------------------------------------
// Confidence normalisation
// ---------------------------------------------------------------------------

// scoreToConfidence normalises a raw score to a [0, 1] confidence value.
// Reference score for high confidence: ScoreSameFile + ScoreSameLanguage +
// ScoreKindAffinity + ScoreExported = 185. We use 200 as the practical ceiling.
func scoreToConfidence(score float64) float64 {
	const ceiling = 200.0
	c := score / ceiling
	if c < 0 {
		c = 0
	}
	if c > 1 {
		c = 1
	}
	return c
}

// ---------------------------------------------------------------------------
// Sort helpers
// ---------------------------------------------------------------------------

func sortCandidates(cs []Candidate) {
	// Insertion sort — candidate lists are typically small (< 50).
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].Score > cs[j-1].Score; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

// ---------------------------------------------------------------------------
// Bounded Levenshtein distance
// ---------------------------------------------------------------------------

// LevenshteinDistance computes the Levenshtein edit distance between a and b
// using the standard two-row DP. The max parameter enables early-exit: if the
// minimum possible distance for any row exceeds max, the function returns
// max+1 without finishing the matrix. This bounds work to O(len(a)*len(b))
// in the worst case but exits early on clearly-too-distant pairs.
//
// Exported so tests can verify known pairs directly.
func LevenshteinDistance(a, b string, max int) int {
	ra := []rune(a)
	rb := []rune(b)
	la, lb := len(ra), len(rb)

	if la == 0 {
		if lb > max {
			return max + 1
		}
		return lb
	}
	if lb == 0 {
		if la > max {
			return max + 1
		}
		return la
	}

	// Two-row DP.
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
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			v := del
			if ins < v {
				v = ins
			}
			if sub < v {
				v = sub
			}
			curr[j] = v
			if v < rowMin {
				rowMin = v
			}
		}
		// Early-exit: if the minimum value in this row already exceeds max,
		// the full distance will also exceed max — no point continuing.
		if rowMin > max {
			return max + 1
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
