package indexer

// Last-resort harvest tier for query-builder argument fragments —
// where("title LIKE ?"), order("created_at DESC"), select("isbn, out_of_print")
// — which are neither whole SQL statements nor bare identifiers, so both
// earlier gates reject them. This file only harvests and tokenizes; the
// resolution side demotes confidence for what it finds.

import (
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

const fragmentMaxLen = 160

// fragmentTokenRE is sqlStringIdentifierRE's per-token shape, matched anywhere
// inside the fragment rather than against the whole literal.
var fragmentTokenRE = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{2,}(\.[A-Za-z_][A-Za-z0-9_]+)?`)

var fragmentComparisonRE = regexp.MustCompile(`<>|!=|<=|>=|=|<|>`)

// fragmentPlaceholderRE covers $N and :name; bare `?` is checked directly.
var fragmentPlaceholderRE = regexp.MustCompile(`\$\d+|:[A-Za-z_][A-Za-z0-9_]*`)

var fragmentConnectiveRE = regexp.MustCompile(`(?i)\b(ASC|DESC|LIKE|IN|IS|AND|OR|NOT|NULL|BETWEEN)\b`)

// fragmentStoplist drops SQL keywords that would otherwise tokenize as if they
// named an object.
var fragmentStoplist = map[string]bool{
	"asc": true, "desc": true, "like": true, "in": true, "is": true,
	"and": true, "or": true, "not": true, "null": true, "between": true,
	"select": true, "from": true, "where": true, "order": true, "group": true,
	"by": true, "having": true, "limit": true, "offset": true, "join": true,
	"on": true, "as": true, "distinct": true, "case": true, "when": true,
	"then": true, "else": true, "end": true,
}

// matchesSQLFragmentGate demands a length cap, one identifier token, and one
// discriminator, so arbitrary prose cannot pass as a query fragment.
func matchesSQLFragmentGate(text string) bool {
	if len(text) > fragmentMaxLen {
		return false
	}
	if !fragmentTokenRE.MatchString(text) {
		return false
	}
	return strings.ContainsRune(text, ',') ||
		strings.ContainsRune(text, '?') ||
		fragmentComparisonRE.MatchString(text) ||
		fragmentPlaceholderRE.MatchString(text) ||
		fragmentConnectiveRE.MatchString(text)
}

func tokenizeSQLFragment(text string) []string {
	matches := fragmentTokenRE.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	var tokens []string
	for _, m := range matches {
		if fragmentStoplist[strings.ToLower(m)] {
			continue
		}
		tokens = append(tokens, m)
	}
	return tokens
}

// emitSpeculativeSQLFragmentRefs appends one ref per surviving token. seen
// must be a different map from the caller's sql_string one, or a fragment
// token would cancel an unrelated same-text literal from the same owner.
func emitSpeculativeSQLFragmentRefs(
	relPath, ownerID string,
	hostLang types.Language,
	span standalone.StringLiteralSpan,
	seen map[string]bool,
	result *types.ExtractionResult,
) {
	if hostLang == types.LanguageSQL {
		return
	}
	if !matchesSQLFragmentGate(span.Text) {
		return
	}
	tokens := tokenizeSQLFragment(span.Text)
	for _, token := range tokens {
		dedupeKey := ownerID + "\x00" + token
		if seen[dedupeKey] {
			continue
		}
		seen[dedupeKey] = true

		result.UnresolvedReferences = append(result.UnresolvedReferences, types.UnresolvedReference{
			ID:            extraction.GenerateRefID(ownerID, token, string(types.ReferenceKindSQLFragment), span.StartLine, 0),
			FromNodeID:    ownerID,
			ReferenceName: token,
			ReferenceKind: types.ReferenceKindSQLFragment,
			Line:          span.StartLine,
			FilePath:      relPath,
			Language:      hostLang,
			CalleeExpr:    span.CalleeExpr,
		})
	}
}
