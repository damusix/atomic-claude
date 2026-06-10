package standalone

// embedded_sql.go — CP1: SQL-side embedded entry point.
//
// IsSQLLiteral is the admission gate: decides whether a host-language string
// literal contains SQL worth extracting. Two implementers reading the gate
// contract table must build equivalent gates; this implementation is the
// canonical one.
//
// ExtractEmbeddedSQL is the entry point for CP2-CP4 host-language harvesters.
// Given a harvested literal span, it returns an ExtractionResult with:
//   - DDL literals: full node+edge set from SQLExtractor.Extract (tables,
//     columns, constraints, FK refs), with Provenance:"embedded" stamped on
//     every Edge and file-absolute line numbers encoded via newline padding.
//   - DML literals: UnresolvedReferences from ScanBodyEdges, owned by
//     ownerNodeID, with file-absolute line numbers encoded via newline padding.
//
// Substitution contract for harvesters: before calling ExtractEmbeddedSQL,
// the harvester must substitute language-specific interpolation segments:
//   - Interpolated TABLE TARGET (e.g. Python "{table}", TS "${table}"): replace
//     with an empty string or whitespace so no identifier appears after FROM/JOIN.
//     The gate may still pass (DML verb + confidence present elsewhere), but
//     ScanBodyEdges will emit no table ref for that FROM clause.
//   - Interpolated VALUE (e.g. "{id}", "${id}"): replace with a SQL placeholder
//     like "?" or "$1". The gate passes and the table name (literal identifier)
//     is extracted normally.
// Segment substitution is language-specific and owned by each harvester because
// the interpolation syntax differs per language (Python f-strings vs TS template
// literals vs Go fmt.Sprintf vs Ruby heredocs).

import (
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Admission gate
// ---------------------------------------------------------------------------

// sqlKeywordPrefilterRE is a cheap pre-filter: skip structural regex if the
// literal contains none of the SQL keywords we care about. Case-insensitive.
// This avoids running the heavier structural regexes on clearly non-SQL content.
var sqlKeywordPrefilterRE = regexp.MustCompile(
	`(?i)\b(?:SELECT|INSERT|UPDATE|DELETE|MERGE|CREATE|TABLE|VIEW|INDEX|SEQUENCE|TRIGGER|FUNCTION|PROCEDURE|SCHEMA)\b`,
)

// ddlIdentAfterRE matches the CREATE <keyword> pattern with an identifier
// following it (at least one word char).
var ddlIdentAfterRE = regexp.MustCompile(
	`(?im)^\s*CREATE\s+(?:OR\s+REPLACE\s+|TEMPORARY\s+|TEMP\s+|MATERIALIZED\s+|FOREIGN\s+|EXTERNAL\s+|IF\s+NOT\s+EXISTS\s+)*` +
		`(?:TABLE|VIEW|INDEX|SEQUENCE|TRIGGER|FUNCTION|PROCEDURE|SCHEMA)\s+(?:IF\s+NOT\s+EXISTS\s+)?` +
		`(?:"[^"]+"|` + "`[^`]+`" + `|\[[^\]]+\]|[A-Za-z_][A-Za-z0-9_$.]*)`,
)

// dmlStartRE matches a DML verb at the trimmed start of the literal.
// Must be the first non-whitespace content.
var dmlStartRE = regexp.MustCompile(
	`(?i)^\s*(?:SELECT\b|INSERT\s+INTO\b|UPDATE\b|DELETE\s+FROM\b|MERGE\s+INTO\b)`,
)

// dmlConfidenceRE matches at least one structural corroboration token:
//   - comma (column list or VALUES list)
//   - comparison operator (=, <, >, !=, <=, >=)
//   - single-quoted string literal inside the SQL
//   - positional/named placeholder: $1, ?, :name, %s
var dmlConfidenceRE = regexp.MustCompile(
	`,|[=<>!]=?|'[^']*'|\$\d+|\?|:[A-Za-z_][A-Za-z0-9_]*|%s`,
)

// updateStartRE matches an UPDATE verb at the trimmed start of the literal.
// Used together with updateSetRE to tighten the UPDATE admission gate.
var updateStartRE = regexp.MustCompile(`(?i)^\s*UPDATE\b`)

// updateSetRE matches the SET keyword that must follow an UPDATE verb in real
// SQL. WHY: prose strings like "UPDATE available: version %s" or "UPDATE plan
// len = %d" start with UPDATE and contain a confidence token (%s or =) but
// never include SET. Requiring SET eliminates these false positives while
// keeping all real UPDATE … SET … WHERE statements admitted.
var updateSetRE = regexp.MustCompile(`(?i)\bSET\b`)

// IsSQLLiteral reports whether a host-language string literal appears to
// contain SQL that is worth extracting. It implements the gate contract from
// docs/spec/embedded-sql-extraction.md §Gate contract.
//
// Pass conditions:
//   - DDL: literal contains CREATE <TABLE|VIEW|INDEX|SEQUENCE|TRIGGER|FUNCTION|
//     PROCEDURE|SCHEMA> followed by an identifier.
//   - DML: literal starts (after optional whitespace) with SELECT, INSERT INTO,
//     UPDATE, DELETE FROM, or MERGE INTO, AND contains at least one Confidence
//     discriminator (comma, comparison, quoted literal, or placeholder).
//
// The gate runs a cheap keyword pre-filter before the structural regexes.
// English prose ("choose an item from the dropdown") never has both a DML verb
// at the start AND a confidence discriminator, so false positives are rare.
func IsSQLLiteral(literal string) bool {
	// Cheap pre-filter: must contain at least one SQL keyword.
	if !sqlKeywordPrefilterRE.MatchString(literal) {
		return false
	}

	// Strip comments before structural check to prevent comment-embedded SQL
	// keywords from producing false positives.
	stripped := stripComments(literal)

	// DDL check: CREATE <object-keyword> <identifier>
	if ddlIdentAfterRE.MatchString(stripped) {
		return true
	}

	// DML check: verb at trimmed start + at least one confidence discriminator.
	// For UPDATE specifically, also require a SET token: prose like
	// "UPDATE available: version %s" has the verb and a confidence token but
	// never SET, so it would be a false positive without this extra guard.
	if dmlStartRE.MatchString(stripped) && dmlConfidenceRE.MatchString(stripped) {
		if updateStartRE.MatchString(stripped) && !updateSetRE.MatchString(stripped) {
			return false
		}
		return true
	}

	return false
}

// ---------------------------------------------------------------------------
// Exported body-scan wrapper
// ---------------------------------------------------------------------------

// ScanBodyEdges is the exported wrapper around the unexported scanBodyEdges.
// It strips comments and string literals from body, extracts CTE names for
// deduplication, and returns UnresolvedReferences for FROM/JOIN, INSERT INTO,
// UPDATE … SET, DELETE FROM, MERGE INTO, and EXEC/CALL targets.
//
// fromNodeID is the owning node (typically the enclosing host function or file
// node, passed in from the orchestrator in CP2).
//
// This is intentionally thin: no provenance stamping (UnresolvedReferences
// carry no Provenance field), no line-offset (the caller is responsible for
// encoding file-absolute lines, e.g. via newline padding before this call).
func ScanBodyEdges(filePath, fromNodeID, body string) []types.UnresolvedReference {
	stripped := stripComments(body)
	strippedNoStr := stripStrings(stripped)
	ctes := extractCTENames(strippedNoStr)
	// bodyBaseOffset=0: line numbers are relative to body start.
	// ExtractEmbeddedSQL passes a newline-padded body so that line 1 of the
	// literal is already at the correct file-absolute line number.
	return scanBodyEdges(filePath, fromNodeID, strippedNoStr, 0, stripped, ctes)
}

// ---------------------------------------------------------------------------
// Embedded entry point
// ---------------------------------------------------------------------------

// ExtractEmbeddedSQL turns a harvested string-literal span into an
// ExtractionResult. It is the CP1 seam consumed by host-language harvesters
// (Go, Python, TypeScript) in later checkpoints.
//
// Parameters:
//   - filePath: the host file path (used for node IDs and edge file attribution).
//   - literalText: the content of the string literal (the SQL itself, already
//     with interpolation segments substituted per the substitution contract above).
//   - baseLine: 1-based line number in filePath where the literal starts.
//     Line numbers in the returned result are file-absolute.
//   - ownerNodeID: the node ID of the enclosing host function or file node.
//     Used as the owner for DML UnresolvedReferences.
//
// Returns an empty ExtractionResult when the literal fails the admission gate.
//
// Provenance: every Edge in the returned result carries Provenance:"embedded".
// This is a new value, distinct from "" (static) and "heuristic" — do not
// touch existing heuristic-provenance dedup checks elsewhere.
//
// Node-ID correctness (CP1): rather than post-hoc offsetting line numbers after
// extraction, we prepend (baseLine-1) newline characters to the literal before
// passing it to Extract / scanBodyEdges. This makes the SQL extractor compute
// file-absolute line numbers from the start, so every GenerateNodeID call
// encodes the correct file line in the hash. No post-hoc line adjustment is
// applied — doing so would double the offset (once via padding, once via
// adjustment) and produce wrong node IDs.
func (e *SQLExtractor) ExtractEmbeddedSQL(filePath, literalText string, baseLine int, ownerNodeID string) types.ExtractionResult {
	if !IsSQLLiteral(literalText) {
		return types.ExtractionResult{}
	}

	// Pad the literal so line numbers computed inside the SQL extractor are
	// file-absolute. baseLine=1 → 0 newlines prepended (no-op). baseLine=10
	// → 9 newlines prepended so the first content line is counted as line 10.
	// WHY: GenerateNodeID(filePath, kind, name, line) hashes the line number
	// into the node ID. Two same-named DDL literals at different host-file
	// lines would otherwise receive the same ID (both at literal-relative
	// line 1), causing INSERT OR REPLACE to collapse one of them in the DB.
	padding := strings.Repeat("\n", baseLine-1)
	paddedText := padding + literalText

	stripped := stripComments(paddedText)
	strippedNoStr := stripStrings(stripped)

	isDDL := ddlIdentAfterRE.MatchString(stripped)

	if isDDL {
		// DDL path: pass paddedText so Extract computes file-absolute lines.
		// Padding already encodes the offset into every node ID and StartLine.
		result, _ := e.Extract(filePath, paddedText)

		// Stamp Provenance:"embedded" on every directly-created Edge.
		// WHY: contains edges (table→column, table→index, enum→member) are
		// created at extraction time and bypass the resolution pipeline, so
		// provenance must be applied here, not at createEdges time.
		for i := range result.Edges {
			result.Edges[i].Provenance = "embedded"
		}

		return result
	}

	// DML path: call scanBodyEdges with the padded text so ref line numbers
	// are file-absolute via the newline padding.
	ctes := extractCTENames(strippedNoStr)
	refs := scanBodyEdges(filePath, ownerNodeID, strippedNoStr, 0, stripped, ctes)

	return types.ExtractionResult{
		UnresolvedReferences: refs,
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// isDMLVerb reports whether the trimmed literal starts with a DML verb.
// Used internally; exported for tests that need to distinguish DDL vs DML
// without running the full gate.
func isDMLVerb(literal string) bool {
	return dmlStartRE.MatchString(strings.TrimSpace(literal))
}
