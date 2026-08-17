package standalone

// Substitution contract for harvesters calling ExtractEmbeddedSQL: interpolation
// segments must already be substituted, since the syntax is language-specific.
// An interpolated table target becomes empty or whitespace — the gate may still
// pass, but no table ref is emitted for that clause. An interpolated value
// becomes a SQL placeholder such as ? or $1, so the gate passes and the literal
// table name still extracts normally.

import (
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// --- Admission gate ---

// sqlKeywordPrefilterRE is a cheap gate before the heavier structural regexes.
var sqlKeywordPrefilterRE = regexp.MustCompile(
	`(?i)\b(?:SELECT|INSERT|UPDATE|DELETE|MERGE|CREATE|TABLE|VIEW|INDEX|SEQUENCE|TRIGGER|FUNCTION|PROCEDURE|SCHEMA)\b`,
)

// ddlIdentAfterRE matches CREATE <object keyword> followed by an identifier.
var ddlIdentAfterRE = regexp.MustCompile(
	`(?im)^\s*CREATE\s+(?:OR\s+REPLACE\s+|TEMPORARY\s+|TEMP\s+|MATERIALIZED\s+|FOREIGN\s+|EXTERNAL\s+|IF\s+NOT\s+EXISTS\s+)*` +
		`(?:TABLE|VIEW|INDEX|SEQUENCE|TRIGGER|FUNCTION|PROCEDURE|SCHEMA)\s+(?:IF\s+NOT\s+EXISTS\s+)?` +
		`(?:"[^"]+"|` + "`[^`]+`" + `|\[[^\]]+\]|[A-Za-z_][A-Za-z0-9_$.]*)`,
)

// dmlStartRE matches a DML verb as the first non-whitespace content.
var dmlStartRE = regexp.MustCompile(
	`(?i)^\s*(?:SELECT\b|INSERT\s+INTO\b|UPDATE\b|DELETE\s+FROM\b|MERGE\s+INTO\b)`,
)

// dmlConfidenceRE matches one structural corroboration token: a comma, a
// comparison operator, a quoted literal, or a placeholder ($1, ?, :name, %s).
var dmlConfidenceRE = regexp.MustCompile(
	`,|[=<>!]=?|'[^']*'|\$\d+|\?|:[A-Za-z_][A-Za-z0-9_]*|%s`,
)

// updateStartRE pairs with updateSetRE to tighten the UPDATE admission gate.
var updateStartRE = regexp.MustCompile(`(?i)^\s*UPDATE\b`)

// updateSetRE guards against prose: "UPDATE available: version %s" carries the
// verb and a confidence token but no SET, which real UPDATE statements always do.
var updateSetRE = regexp.MustCompile(`(?i)\bSET\b`)

// IsSQLLiteral reports whether a host-language string literal is worth
// extracting as SQL: a CREATE of a named object, or a DML verb at the start plus
// at least one confidence discriminator. Prose rarely carries both, which is
// what keeps false positives down. Contract:
// docs/spec/embedded-sql-extraction.md.
func IsSQLLiteral(literal string) bool {
	if !sqlKeywordPrefilterRE.MatchString(literal) {
		return false
	}

	// Strip comments so keywords inside them cannot pass the structural check.
	stripped := stripComments(literal)

	if ddlIdentAfterRE.MatchString(stripped) {
		return true
	}

	if dmlStartRE.MatchString(stripped) && dmlConfidenceRE.MatchString(stripped) {
		if updateStartRE.MatchString(stripped) && !updateSetRE.MatchString(stripped) {
			return false
		}
		return true
	}

	return false
}

// --- Exported body-scan wrapper ---

// ScanBodyEdges returns the references a SQL body makes (FROM/JOIN, INSERT INTO,
// UPDATE … SET, DELETE FROM, MERGE INTO, EXEC/CALL), owned by fromNodeID. Line
// numbers are relative to body: the caller encodes file-absolute lines by
// padding body with leading newlines beforehand.
func ScanBodyEdges(filePath, fromNodeID, body string) []types.UnresolvedReference {
	stripped := stripComments(body)
	strippedNoStr := stripStrings(stripped)
	ctes := extractCTENames(strippedNoStr)
	// No T-SQL temp declarations exist in a host-language string literal.
	_, refs := scanBodyEdges(filePath, fromNodeID, strippedNoStr, 0, stripped, ctes, "", nil)
	return refs
}

// --- Embedded entry point ---

// ExtractEmbeddedSQL turns a harvested literal into an ExtractionResult, empty
// when the literal fails the admission gate. baseLine is the 1-based host-file
// line where the literal starts, and returned line numbers are file-absolute.
// Every Edge carries Provenance "embedded", distinct from "" (static) and
// "heuristic".
func (e *SQLExtractor) ExtractEmbeddedSQL(filePath, literalText string, baseLine int, ownerNodeID string) types.ExtractionResult {
	if !IsSQLLiteral(literalText) {
		return types.ExtractionResult{}
	}

	// Padding rather than a post-hoc line shift: GenerateNodeID hashes the line
	// into the node ID, so two same-named DDL literals at different host lines
	// would otherwise share an ID and INSERT OR REPLACE would collapse one.
	padding := strings.Repeat("\n", baseLine-1)
	paddedText := padding + literalText

	stripped := stripComments(paddedText)
	strippedNoStr := stripStrings(stripped)

	isDDL := ddlIdentAfterRE.MatchString(stripped)

	if isDDL {
		result, _ := e.Extract(filePath, paddedText)

		// Contains edges are created at extraction time and bypass the
		// resolution pipeline, so provenance has to be stamped here.
		for i := range result.Edges {
			result.Edges[i].Provenance = "embedded"
		}

		return result
	}

	// No T-SQL temp declarations exist in a host-language string literal.
	ctes := extractCTENames(strippedNoStr)
	_, refs := scanBodyEdges(filePath, ownerNodeID, strippedNoStr, 0, stripped, ctes, "", nil)

	return types.ExtractionResult{
		UnresolvedReferences: refs,
	}
}

// --- helpers ---

// isDMLVerb reports whether the trimmed literal starts with a DML verb.
func isDMLVerb(literal string) bool {
	return dmlStartRE.MatchString(strings.TrimSpace(literal))
}
