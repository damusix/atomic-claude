package indexer

// sql_fragment_harvest.go — C8 fragment-tier harvest.
//
// Covers builder-arg SQL fragments (ActiveRecord where("title LIKE ?"), GORM
// Where("name = ?"), order("created_at DESC"), comma lists like
// select("isbn, out_of_print")) that fail both C1's identifier shape and the
// IsSQLLiteral gate. Checked only after both of those fail (see
// embeddedSQLPostPass / emitSpeculativeSQLStringRef caller ordering).
//
// Resolution-side handling (pass A/B consuming sql_fragment refs, one-notch
// confidence demotion) is — this file only harvests and tokenizes.

import (
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// fragmentMaxLen is the C8 fragment-gate length cap.
const fragmentMaxLen = 160

// fragmentTokenRE extracts bare identifiers and one-dot qualified pairs —
// the same per-token shape as sqlStringIdentifierRE (C1), applied to
// substrings found anywhere in the fragment rather than the whole literal.
var fragmentTokenRE = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{2,}(\.[A-Za-z_][A-Za-z0-9_]+)?`)

// fragmentComparisonRE matches SQL comparison operators.
var fragmentComparisonRE = regexp.MustCompile(`<>|!=|<=|>=|=|<|>`)

// fragmentPlaceholderRE matches bind-parameter placeholders: $N or :name.
// Bare `?` is checked separately (no regex needed for a literal rune).
var fragmentPlaceholderRE = regexp.MustCompile(`\$\d+|:[A-Za-z_][A-Za-z0-9_]*`)

// fragmentConnectiveRE matches the word-boundary case-insensitive SQL
// connective/order discriminator tokens.
var fragmentConnectiveRE = regexp.MustCompile(`(?i)\b(ASC|DESC|LIKE|IN|IS|AND|OR|NOT|NULL|BETWEEN)\b`)

// fragmentStoplist is the case-insensitive keyword stoplist tokenization
// drops. Includes the discriminator connectives plus the clause/keyword set
// from C8.
var fragmentStoplist = map[string]bool{
	"asc": true, "desc": true, "like": true, "in": true, "is": true,
	"and": true, "or": true, "not": true, "null": true, "between": true,
	"select": true, "from": true, "where": true, "order": true, "group": true,
	"by": true, "having": true, "limit": true, "offset": true, "join": true,
	"on": true, "as": true, "distinct": true, "case": true, "when": true,
	"then": true, "else": true, "end": true,
}

// matchesSQLFragmentGate applies the C8 fragment gate: length cap, at least
// one identifier token, and at least one discriminator (comparison operator,
// placeholder, comma, or connective keyword).
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

// tokenizeSQLFragment extracts the surviving identifier/qualified-pair
// tokens from a fragment that passed the gate, dropping anything on the
// keyword stoplist (case-insensitive, exact match on the full token).
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

// emitSpeculativeSQLFragmentRefs appends one UnresolvedReference with
// ReferenceKind sql_fragment per surviving token when span.Text fails both
// C1's identifier shape and IsSQLLiteral but passes the C8 fragment gate.
// seen mirrors C1's sql_string dedupe map shape (per ownerID+token) but must
// be a separate map instance from the caller's sql_string one — a fragment
// token and an unrelated same-text sql_string literal from the same owner
// would otherwise collide.
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
