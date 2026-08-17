package resolution

// Name matching for non-import references: a first-match-wins strategy
// cascade, then a calibrated score to pick among the candidates it returns.
//
// The scoring weights below are calibrated. Do not change them without A/B
// evidence — every one of them shifts which overload wins repo-wide.

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

const (
	// ScoreSameFile — candidate declared in the referencing file.
	ScoreSameFile = 100

	// ScorePathProximityMax — awarded in full for the same directory, scaled
	// down by shared path depth from there.
	ScorePathProximityMax = 80

	ScoreSameLanguage = 50

	// ScoreCrossLanguage outweighs path proximity: a language mismatch is
	// stronger evidence against a candidate than proximity is for it.
	ScoreCrossLanguage = -80

	// ScoreKindAffinity — the candidate's kind fits the reference kind.
	ScoreKindAffinity = 25

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

// MatchResult is the winning candidate, its raw score, that score normalised
// to [0, 1], and which strategy produced it.
type MatchResult struct {
	Node       types.Node
	Score      float64
	Confidence float64
	Strategy   MatchStrategy
}

// Candidate is one scored candidate node.
type Candidate struct {
	Node  types.Node
	Score float64
}

// NameMatcher resolves non-import references to their best-matching node.
type NameMatcher struct {
	db *db.DB
	// knownNames is byFuzzy's corpus: lowercased, set once per batch.
	knownNames []string
}

func NewNameMatcher(d *db.DB) *NameMatcher {
	return &NameMatcher{db: d}
}

// SetKnownNames must run before the batch loop, with names already lowercased.
func (nm *NameMatcher) SetKnownNames(names []string) {
	nm.knownNames = names
}

// MatchReference returns (nil, nil) when nothing matches.
func (nm *NameMatcher) MatchReference(ctx context.Context, ref types.UnresolvedReference) (*MatchResult, error) {
	return nm.matchReference(ctx, ref, false)
}

// MatchReferenceNoFuzzy stops before the fuzzy fallback. The pipeline uses it
// for names past fuzzyNameLenCap, where the variant set stalls a batch.
func (nm *NameMatcher) MatchReferenceNoFuzzy(ctx context.Context, ref types.UnresolvedReference) (*MatchResult, error) {
	return nm.matchReference(ctx, ref, true)
}

func (nm *NameMatcher) matchReference(ctx context.Context, ref types.UnresolvedReference, skipFuzzy bool) (*MatchResult, error) {
	name := ref.ReferenceName

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
		// A SQL "table.col" is shaped like a method call but is a column, and
		// byMethodCall returns only method/function kinds — so it always
		// misses. SQL-gated so receiver.method resolution elsewhere is
		// untouched.
		if ref.Language == types.LanguageSQL {
			candidates, err = nm.byQualifiedName(ctx, name, ref)
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
	}

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

	if !skipFuzzy {
		candidates, err = nm.byFuzzy(ctx, name, ref)
		if err != nil {
			return nil, err
		}
		if len(candidates) > 0 {
			best := findBestMatch(candidates, ref)
			// Confidence is discounted: an edit-distance hit is a guess, and
			// must lose to any exact hit the pipeline finds elsewhere.
			return &MatchResult{
				Node:       best.Node,
				Score:      best.Score,
				Confidence: scoreToConfidence(best.Score) * 0.6,
				Strategy:   StrategyFuzzy,
			}, nil
		}
	}

	return nil, nil
}

// GetAllCandidates returns every scored candidate, highest first. Unlike
// MatchReference it runs all strategies, so callers can surface overloads
// rather than one winner.
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
		// The SQL column fall-through matchReference does, guarded so it does
		// not repeat a byQualifiedName call already made above.
		if ref.Language == types.LanguageSQL && !isQualifiedDot(name) && !strings.Contains(name, "::") {
			nodes, err = nm.byQualifiedName(ctx, name, ref)
			if err != nil {
				return nil, err
			}
			addCandidates(nodes)
		}
	}

	nodes, err := nm.byExactName(ctx, name, ref)
	if err != nil {
		return nil, err
	}
	addCandidates(nodes)

	// Fuzzy only as a last resort — guesses must not dilute a real hit list.
	if len(all) == 0 {
		nodes, err = nm.byFuzzy(ctx, name, ref)
		if err != nil {
			return nil, err
		}
		addCandidates(nodes)
	}

	sortCandidates(all)
	return all, nil
}

func (nm *NameMatcher) byFilePath(ctx context.Context, name string, ref types.UnresolvedReference) ([]types.Node, error) {
	fileNodeID := "file:" + name
	n, err := nm.db.GetNode(ctx, fileNodeID)
	if err == nil {
		return []types.Node{n}, nil
	}
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

// byQualifiedName narrows by simple name first, because the DB is indexed by
// name and not by qualified name.
func (nm *NameMatcher) byQualifiedName(ctx context.Context, name string, ref types.UnresolvedReference) ([]types.Node, error) {
	simpleName := qualifiedSimpleName(name)
	if simpleName == "" {
		return nil, nil
	}

	nodes, err := nm.db.GetNodesByName(ctx, simpleName, "")
	if err != nil {
		return nil, err
	}

	lowerFull := strings.ToLower(name)
	lowerSimple := strings.ToLower(simpleName)
	var exact []types.Node
	var suffix []types.Node
	for _, n := range nodes {
		lq := strings.ToLower(n.QualifiedName)
		if lq == lowerFull {
			exact = append(exact, n)
		} else if strings.HasSuffix(lq, "::"+lowerSimple) ||
			strings.HasSuffix(lq, "."+lowerSimple) {
			suffix = append(suffix, n)
		}
	}

	// Exact wins outright: a suffix match on ".id" hits every table's id
	// column, so returning both sets would drown the unambiguous answer.
	if len(exact) > 0 {
		return exact, nil
	}
	return suffix, nil
}

// byMethodCall resolves "receiver.method" by method name, keeping only
// callable kinds.
func (nm *NameMatcher) byMethodCall(ctx context.Context, receiver, method string, ref types.UnresolvedReference) ([]types.Node, error) {
	nodes, err := nm.db.GetNodesByName(ctx, method, "")
	if err != nil {
		return nil, err
	}

	var results []types.Node
	for _, n := range nodes {
		if n.Kind == types.NodeKindMethod || n.Kind == types.NodeKindFunction {
			results = append(results, n)
		}
	}

	if receiver != "" && isReceiverInferenceLanguage(ref.Language) && len(results) > 1 {
		results = receiverInferenceBias(results, receiver)
	}

	return results, nil
}

func (nm *NameMatcher) byExactName(ctx context.Context, name string, ref types.UnresolvedReference) ([]types.Node, error) {
	return nm.db.GetNodesByName(ctx, name, "")
}

// byFuzzy scans the warmed name set rather than generating edit-distance
// variants and probing each: variants cost O(n·26^maxDist) DB round-trips per
// ref, while scanning costs one DB fetch per name actually within threshold —
// typically none. Distance thresholds match the search tier's.
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
			continue
		}
		// Length window: cheaper than the distance computation it skips.
		candidateRunes := utf8.RuneCountInString(candidate)
		if abs(candidateRunes-nameRunes) > maxDist {
			continue
		}
		if LevenshteinDistance(lowerName, candidate, maxDist) > maxDist {
			continue
		}
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

func computeScore(n types.Node, ref types.UnresolvedReference) float64 {
	var score float64

	if n.FilePath == ref.FilePath {
		score += ScoreSameFile
	} else {
		score += PathProximityScore(ref.FilePath, n.FilePath)
	}

	if ref.Language != types.LanguageUnknown && ref.Language != "" {
		if n.Language == ref.Language {
			score += ScoreSameLanguage
		} else {
			score += ScoreCrossLanguage
		}
	}

	if hasKindAffinity(ref.ReferenceKind, n.Kind) {
		score += ScoreKindAffinity
	}

	if n.IsExported {
		score += ScoreExported
	}

	// Nearby lines break ties within a file: up to 10, decaying to 0 at a
	// distance of 100 lines.
	if ref.Line > 0 && n.StartLine > 0 {
		dist := math.Abs(float64(ref.Line - n.StartLine))
		boost := math.Max(0, 10.0-dist/10.0)
		score += boost
	}

	return score
}

// PathProximityScore scales ScorePathProximityMax by how many leading
// directory segments the two paths share. Exported so tests can assert the
// gradient rather than only its endpoints.
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

func isReceiverInferenceLanguage(lang types.Language) bool {
	return lang == types.LanguageJava || lang == types.LanguageCpp ||
		lang == types.LanguageKotlin || lang == types.LanguageScala
}

// receiverInferenceBias floats candidates whose qualified name contains a
// token starting with the receiver's first few characters — "conn" reaches
// "Connection". A stable partition, not a filter: this is a name heuristic
// standing in for type inference, so it must never discard the right answer.
func receiverInferenceBias(nodes []types.Node, receiver string) []types.Node {
	if receiver == "" {
		return nodes
	}
	prefixLen := len(receiver)
	if prefixLen > 4 {
		prefixLen = 4
	}
	recvPrefix := strings.ToLower(receiver[:prefixLen])

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

// parseMethodCall accepts exactly one dot: "a.b.c" is a qualified path and a
// name containing "/" is a file path, both handled by other strategies.
func parseMethodCall(name string) (receiver, method string, ok bool) {
	if strings.Contains(name, "/") {
		return "", "", false
	}
	parts := strings.SplitN(name, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if strings.Contains(parts[1], ".") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func isQualifiedDot(name string) bool {
	if strings.Contains(name, "/") {
		return false
	}
	return strings.Count(name, ".") >= 2
}

// qualifiedSimpleName returns the last segment of a "::"- or "."-qualified name.
func qualifiedSimpleName(name string) string {
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		return name[idx+2:]
	}
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// scoreToConfidence clamps to [0, 1] against a ceiling just above the
// realistic best case (same file + same language + kind affinity + exported).
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

// sortCandidates uses insertion sort: candidate lists run well under 50.
func sortCandidates(cs []Candidate) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].Score > cs[j-1].Score; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

// LevenshteinDistance returns max+1, not the true distance, once the answer
// is known to exceed max — callers only ever compare against a threshold.
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
		// Row minimum only ever grows, so exceeding max here is final.
		if rowMin > max {
			return max + 1
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
