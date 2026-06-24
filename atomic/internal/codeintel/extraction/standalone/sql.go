package standalone

// SQL standalone extractor (CP2 — definition nodes).
//
// Produces node kinds: table, view, column, function, procedure, trigger,
// index, sequence, namespace, enum, enum_member, type_alias, module.
// Contains edges: table→column, table→index, enum→enum_member.
//
// Comment stripping is applied before any matching to prevent false positives
// from CREATE statements inside -- or /* */ comments. String-literal stripping
// is best-effort (single-quoted contents replaced with blanks).
//
// Dialect coverage: Postgres (ANSI / "quotes"), MySQL (backticks),
// T-SQL ([brackets], GO terminators, CREATE OR ALTER, CREATE TYPE … FROM).

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Identifier helpers
// ---------------------------------------------------------------------------

// sqlIdentRE matches a single SQL identifier in any quoting style:
//   - bare:     word
//   - ANSI:     "word"
//   - MySQL:    `word`
//   - T-SQL:    [word]
//
// Group 1 captures the bare (unquoted) content.
const sqlIdentPat = `(?:"([^"]+)"|` + "`([^`]+)`" + `|\[([^\]]+)\]|([A-Za-z_][A-Za-z0-9_$]*))`

// sqlQNameRE matches an optionally schema-qualified (up to 3 parts) identifier.
// Each component uses sqlIdentPat. The full match is used to find the position;
// the caller uses extractQName to parse it.
var sqlQNameRE = regexp.MustCompile(
	sqlIdentPat + `(?:\.` + sqlIdentPat + `(?:\.` + sqlIdentPat + `)?)?`,
)

// normIdent strips surrounding quote characters from a captured SQL identifier.
func normIdent(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		switch {
		case s[0] == '"' && s[len(s)-1] == '"':
			return s[1 : len(s)-1]
		case s[0] == '`' && s[len(s)-1] == '`':
			return s[1 : len(s)-1]
		case s[0] == '[' && s[len(s)-1] == ']':
			return s[1 : len(s)-1]
		}
	}
	return s
}

// parseQName parses a possibly-schema-qualified SQL name (dot-separated) and
// returns (schemaOrEmpty, name). Handles all quoting styles.
func parseQName(raw string) (schema, name string) {
	parts := splitQName(raw)
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return "", normIdent(parts[0])
	default:
		// Take last component as name, join rest as schema.
		name = normIdent(parts[len(parts)-1])
		schemaParts := make([]string, len(parts)-1)
		for i, p := range parts[:len(parts)-1] {
			schemaParts[i] = normIdent(p)
		}
		schema = strings.Join(schemaParts, ".")
		return schema, name
	}
}

// splitQName splits a possibly-quoted, dot-delimited SQL name into its parts.
// Dots inside quote characters are not treated as separators.
func splitQName(raw string) []string {
	var parts []string
	var cur strings.Builder
	var quote byte
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if quote != 0 {
			cur.WriteByte(c)
			if (quote == '"' && c == '"') || (quote == '`' && c == '`') || (quote == '[' && c == ']') {
				quote = 0
			}
		} else {
			switch c {
			case '"', '`':
				quote = c
				cur.WriteByte(c)
			case '[':
				quote = '['
				cur.WriteByte(c)
			case '.':
				if cur.Len() > 0 {
					parts = append(parts, cur.String())
					cur.Reset()
				}
			default:
				cur.WriteByte(c)
			}
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// qualifiedName builds the qualified name string: "schema.name" or just "name".
func qualifiedName(schema, name string) string {
	if schema != "" {
		return schema + "." + name
	}
	return name
}

// ---------------------------------------------------------------------------
// Comment / string stripping
// ---------------------------------------------------------------------------

// stripLineCommentsRE matches -- to end of line.
// We replace the content (after --) with spaces, preserving the newline.
var stripLineCommentsRE = regexp.MustCompile(`--[^\n]*`)

// stripBlockCommentsRE matches /* ... */ block comments (non-greedy, dotall).
var stripBlockCommentsRE = regexp.MustCompile(`(?s)/\*.*?\*/`)

// stripSingleQuotedRE matches single-quoted string literals (best-effort;
// handles escaped single quotes as ” but not backslash-escape style).
var stripSingleQuotedRE = regexp.MustCompile(`'(?:[^']|'')*'`)

// stripComments removes -- line comments and /* */ block comments from source,
// preserving line counts by replacing stripped content with blank chars.
// The returned string has identical byte length to source (same number of
// newlines), so strings.Count(stripped[:offset], "\n")+1 still works.
func stripComments(source string) string {
	// Replace block comments: keep newlines, replace other chars with space.
	result := stripBlockCommentsRE.ReplaceAllStringFunc(source, func(m string) string {
		var sb strings.Builder
		sb.Grow(len(m))
		for _, c := range m {
			if c == '\n' {
				sb.WriteByte('\n')
			} else {
				sb.WriteByte(' ')
			}
		}
		return sb.String()
	})
	// Replace line comments: keep the newline at end of line.
	result = stripLineCommentsRE.ReplaceAllStringFunc(result, func(m string) string {
		return strings.Repeat(" ", len(m))
	})
	return result
}

// stripStrings replaces single-quoted string literals with same-length blank
// sequences (preserving newlines). Best-effort: protects against DDL comments
// embedded in default values.
func stripStrings(source string) string {
	return stripSingleQuotedRE.ReplaceAllStringFunc(source, func(m string) string {
		var sb strings.Builder
		sb.Grow(len(m))
		for _, c := range m {
			if c == '\n' {
				sb.WriteByte('\n')
			} else {
				sb.WriteByte(' ')
			}
		}
		return sb.String()
	})
}

// ---------------------------------------------------------------------------
// Regex patterns for CREATE statements
// ---------------------------------------------------------------------------

// sqlModifiersRE matches the optional modifier words between CREATE and the
// object kind keyword. Anchored to consume (but not capture) them.
// Pattern: optional IF NOT EXISTS / OR REPLACE / OR ALTER
const modPat = `(?:(?:OR\s+(?:REPLACE|ALTER)|IF\s+NOT\s+EXISTS)\s+)*`

// tableClassPat matches optional Snowflake/SQL-standard class modifiers that
// may appear between OR REPLACE and TABLE. OR REPLACE is consumed by modPat.
// Valid forms:
//   - TRANSIENT | VOLATILE | TEMPORARY | TEMP  — stand alone
//   - LOCAL TEMPORARY | LOCAL TEMP             — LOCAL only as prefix
//   - GLOBAL TEMPORARY | GLOBAL TEMP           — GLOBAL only as prefix
//
// Bare LOCAL TABLE / GLOBAL TABLE are invalid in all supported dialects and
// must NOT match — so LOCAL/GLOBAL are non-optional only when followed by
// TEMP/TEMPORARY. The alternation is: one optional modifier token which is
// either a standalone word or a LOCAL/GLOBAL-prefixed TEMP compound.
const tableClassPat = `(?:(?:TRANSIENT|VOLATILE|(?:(?:LOCAL|GLOBAL)\s+)?(?:TEMPORARY|TEMP))\s+)?`

// viewSecurityPat matches optional Snowflake security/recursive modifiers that
// may appear between OR REPLACE and VIEW (e.g. SECURE, RECURSIVE, and the
// optional TEMP/TEMPORARY qualifier on a view). Multiple modifiers are valid.
const viewSecurityPat = `(?:(?:SECURE|RECURSIVE|TEMPORARY|TEMP)\s+)*`

// tableRE matches CREATE [FOREIGN|EXTERNAL] [class-modifiers] TABLE [IF NOT EXISTS] <name>
// A1: tableClassPat inserted after the FOREIGN|EXTERNAL group to absorb
// Snowflake TRANSIENT/TEMPORARY/TEMP/VOLATILE/LOCAL/GLOBAL before TABLE.
var tableRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `(?:(FOREIGN|EXTERNAL)\s+)?` + tableClassPat + `TABLE\s+` + modPat + `(` + sqlQNameRaw + `)`)

// viewRE matches CREATE [OR REPLACE] [security-modifiers] [MATERIALIZED] VIEW <name>
// A1: viewSecurityPat inserted after modPat to absorb Snowflake SECURE/RECURSIVE
// before MATERIALIZED/VIEW.
var viewRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + viewSecurityPat + `(MATERIALIZED\s+)?VIEW\s+` + modPat + `(` + sqlQNameRaw + `)`)

// functionRE matches CREATE [OR REPLACE|OR ALTER] FUNCTION <name>
var functionRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `FUNCTION\s+` + modPat + `(` + sqlQNameRaw + `)`)

// procedureRE matches CREATE [OR REPLACE|OR ALTER] PROCEDURE <name>
var procedureRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `PROC(?:EDURE)?\s+` + modPat + `(` + sqlQNameRaw + `)`)

// triggerRE matches CREATE [OR REPLACE] TRIGGER <name>
var triggerRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `TRIGGER\s+` + modPat + `(` + sqlQNameRaw + `)`)

// indexRE matches CREATE [UNIQUE] INDEX <name> ON <table>
var indexRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+(?:UNIQUE\s+)?INDEX\s+` + modPat + `(` + sqlQNameRaw + `)\s+ON\s+(` + sqlQNameRaw + `)`)

// sequenceRE matches CREATE SEQUENCE <name>
var sequenceRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `SEQUENCE\s+` + modPat + `(` + sqlQNameRaw + `)`)

// schemaRE matches CREATE SCHEMA <name>
var schemaRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `SCHEMA\s+` + modPat + `(` + sqlQNameRaw + `)`)

// databaseRE matches CREATE DATABASE <name>
var databaseRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `DATABASE\s+` + modPat + `(` + sqlQNameRaw + `)`)

// domainRE matches CREATE DOMAIN <name>
var domainRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `DOMAIN\s+` + modPat + `(` + sqlQNameRaw + `)`)

// synonymRE matches CREATE SYNONYM <name> FOR <target>
// Group 1 = synonym name, Group 2 = target name
var synonymRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `SYNONYM\s+` + modPat + `(` + sqlQNameRaw + `)\s+FOR\s+(` + sqlQNameRaw + `)`)

// policyRE matches CREATE POLICY <name> ON <table>
// Group 1 = policy name, Group 2 = table name
var policyRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `POLICY\s+(` + sqlIdentOnlyRaw + `)\s+ON\s+(` + sqlQNameRaw + `)`)

// triggerOnRE matches the ON <table> clause in a CREATE TRIGGER statement.
// Used to extract the trigger target table after the trigger node is found.
// Group 1 = table name.
var triggerOnRE = regexp.MustCompile(`(?i)\bON\s+(` + sqlQNameRaw + `)`)

// triggerExecFnRE matches EXECUTE [PROCEDURE|FUNCTION] <fn> in a trigger body.
// Group 1 = function name.
var triggerExecFnRE = regexp.MustCompile(`(?i)\bEXECUTE\s+(?:PROCEDURE|FUNCTION)\s+(` + sqlQNameRaw + `)`)

// viewBodyFROMRE matches FROM <table> or JOIN <table> in a view or routine body
// (after stripping comments/strings). Shared between view-body and routine-body scans.
// Group 1 = table name.
var viewBodyFROMRE = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+(` + sqlQNameRaw + `)`)

// bodyInsertIntoRE matches INSERT INTO <name> in a routine body.
var bodyInsertIntoRE = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+(` + sqlQNameRaw + `)`)

// bodyUpdateRE matches UPDATE <name> in a routine body (before SET).
var bodyUpdateRE = regexp.MustCompile(`(?i)\bUPDATE\s+(` + sqlQNameRaw + `)\s+SET\b`)

// bodyDeleteFromRE matches DELETE FROM <name> in a routine body.
var bodyDeleteFromRE = regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+(` + sqlQNameRaw + `)`)

// bodyMergeIntoRE matches MERGE INTO <name> in a routine body.
var bodyMergeIntoRE = regexp.MustCompile(`(?i)\bMERGE\s+INTO\s+(` + sqlQNameRaw + `)`)

// bodyExecCallRE matches EXEC[UTE] <name> or CALL <name>( in a routine body.
// Group 1 = routine name.
var bodyExecCallRE = regexp.MustCompile(`(?i)\b(?:EXEC(?:UTE)?\s+|CALL\s+)(` + sqlQNameRaw + `)\s*[\s(]`)

// cteNameRE matches the name of each CTE in WITH <name> AS (…) or , <name> AS (…).
// Used to collect CTE-local names so they are excluded from edge emission.
var cteNameRE = regexp.MustCompile(`(?i)(?:\bWITH\b|,)\s+(` + sqlIdentOnlyRaw + `)\s+AS\s*\(`)

// usingWithCheckRE matches the USING (...) and WITH CHECK (...) expressions
// in a policy statement. Group 1 = the expression content inside the parens.
// Used for F-7: scope fn-call capture to only these expression blocks.
var usingWithCheckRE = regexp.MustCompile(`(?i)\b(?:USING|WITH\s+CHECK)\s*\(([^)]*)\)`)

// inlineRefRE matches an inline REFERENCES <table> FK in a column definition.
// Group 1 = target table name.
var inlineRefRE = regexp.MustCompile(`(?i)\bREFERENCES\s+(` + sqlQNameRaw + `)`)

// alterTablePat is the common prefix for ALTER TABLE patterns.
// It handles the optional Postgres-specific ONLY keyword that appears between
// TABLE and the table name: ALTER TABLE ONLY orders ADD ...
//
// NOTE: the (?:ONLY\s+)? prefix consumes the keyword "ONLY" when present.
// A table literally named "only" (unquoted) would be mis-consumed, but Postgres
// forbids unquoted reserved words as identifiers in this position — valid DDL
// never uses bare "only" as a table name here. Quoted forms ("only", [only])
// are matched by sqlQNameRaw's quoted branches and are unaffected.
const alterTablePat = `(?:ONLY\s+)?` + modPat

// alterFKRefRE matches ALTER TABLE … FOREIGN KEY … REFERENCES <table> to extract the FK target.
// Group 1 = table name in ADD FOREIGN KEY clause, Group 2 = target table name.
var alterFKRefRE = regexp.MustCompile(`(?im)^[ \t]*ALTER\s+TABLE\s+` + alterTablePat + `(` + sqlQNameRaw + `)\s+ADD\s+(?:CONSTRAINT\s+` + sqlIdentOnlyRaw + `\s+)?FOREIGN\s+KEY\s*\([^)]*\)\s+REFERENCES\s+(` + sqlQNameRaw + `)`)

// fnCallInExprRE matches function calls fn(...) in an expression (for policy USING / WITH CHECK).
// Group 1 = function name (bare identifier before the opening paren).
var fnCallInExprRE = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_$]*)\s*\(`)

// typeRE matches CREATE TYPE <name> (followed by AS ENUM / AS TABLE / FROM / or nothing for composite)
// Group 1 = name, Group 2 = "ENUM", Group 3 = "TABLE", Group 4 = "FROM <base>"
var typeRE = regexp.MustCompile(
	`(?im)^[ \t]*CREATE\s+` + modPat + `TYPE\s+` + modPat + `(` + sqlQNameRaw + `)` +
		`\s+(?:AS\s+(ENUM|TABLE)|FROM\s+(` + sqlQNameRaw + `))`,
)

// typeCompositeRE matches CREATE TYPE <name> with no trailing AS/FROM on same line
// (used as a fallback for composite types that don't match typeRE).
var typeCompositeRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `TYPE\s+` + modPat + `(` + sqlQNameRaw + `)\s*$`)

// alterAddColumnRE matches ALTER TABLE <table> ADD [COLUMN] <col>
var alterAddColumnRE = regexp.MustCompile(`(?im)^[ \t]*ALTER\s+TABLE\s+` + alterTablePat + `(` + sqlQNameRaw + `)\s+ADD\s+(?:COLUMN\s+)?(` + sqlIdentOnlyRaw + `)`)

// alterAddConstraintRE matches ALTER TABLE <table> ADD CONSTRAINT <name> <type> ...
// Group 1 = table name, Group 2 = constraint name, Group 3 = constraint type keyword
var alterAddConstraintRE = regexp.MustCompile(`(?im)^[ \t]*ALTER\s+TABLE\s+` + alterTablePat + `(` + sqlQNameRaw + `)\s+ADD\s+CONSTRAINT\s+(` + sqlIdentOnlyRaw + `)\s+(PRIMARY\s+KEY|FOREIGN\s+KEY|UNIQUE|CHECK)`)

// alterAddAnonConstraintRE matches ALTER TABLE <table> ADD PRIMARY KEY|FOREIGN KEY|UNIQUE|CHECK (no CONSTRAINT keyword)
// Group 1 = table name, Group 2 = constraint type keyword
var alterAddAnonConstraintRE = regexp.MustCompile(`(?im)^[ \t]*ALTER\s+TABLE\s+` + alterTablePat + `(` + sqlQNameRaw + `)\s+ADD\s+(PRIMARY\s+KEY|FOREIGN\s+KEY|UNIQUE|CHECK)\b`)

// sqlQNameRaw is the raw pattern for a qualified SQL name (no capturing groups
// within — used inside other regexes where subgroup numbering matters).
// We use a non-capturing variant that still handles all quoting styles.
//
// The bare-name alternative ([A-Za-z_][A-Za-z0-9_$.]*) includes '.' so that
// a bare schema.name like "public.orders" is matched as one token. The
// trailing quoted-component groups ((?:\.("..."))*) are therefore only
// reached for mixed forms such as schema."quoted_name" — they are dead for
// fully-bare or fully-quoted names. parseQName re-splits the matched token on
// dots, so callers always receive the correct (schema, name) pair regardless.
const sqlQNameRaw = `(?:"[^"]+"|` + "`[^`]+`" + `|\[[^\]]+\]|[A-Za-z_][A-Za-z0-9_$.]*)` +
	`(?:\.(?:"[^"]+"|` + "`[^`]+`" + `|\[[^\]]+\]|[A-Za-z_][A-Za-z0-9_$.]*))*`

// sqlIdentOnlyRaw is just a single unqualified identifier (no dots).
const sqlIdentOnlyRaw = `(?:"[^"]+"|` + "`[^`]+`" + `|\[[^\]]+\]|[A-Za-z_][A-Za-z0-9_$]*)`

// ---------------------------------------------------------------------------
// Enum label extraction
// ---------------------------------------------------------------------------

// enumValuesRE extracts the parenthesised label list from CREATE TYPE … AS ENUM (…).
var enumValuesRE = regexp.MustCompile(`(?si)AS\s+ENUM\s*\(([^)]*)\)`)

// singleQuotedLabelRE matches a single-quoted enum label.
var singleQuotedLabelRE = regexp.MustCompile(`'([^']*)'`)

// ---------------------------------------------------------------------------
// Column extraction inside CREATE TABLE body
// ---------------------------------------------------------------------------

// constraintKeywords is the set of column-level keywords that signal a
// table-level constraint line rather than a column definition.
// Lines starting with these words (after whitespace) are skipped.
var constraintKeywords = map[string]bool{
	"CONSTRAINT": true,
	"PRIMARY":    true,
	"FOREIGN":    true,
	"UNIQUE":     true,
	"CHECK":      true,
	"INDEX":      true,
	"KEY":        true,
}

// generatedMarkers are patterns that indicate a computed/generated column.
var generatedMarkerRE = regexp.MustCompile(`(?i)\bGENERATED\b|\bAS\s*\(`)

// ---------------------------------------------------------------------------
// SQLExtractor
// ---------------------------------------------------------------------------

// SQLExtractor extracts SQL definition nodes (CP2: definitions only).
// No body-level references, no constraints as nodes — those are later CPs.
type SQLExtractor struct{}

// NewSQLExtractor returns a SQLExtractor. No pool required.
func NewSQLExtractor() *SQLExtractor {
	return &SQLExtractor{}
}

// Extract implements the Extractor interface for SQL files.
func (e *SQLExtractor) Extract(filePath, source string) (types.ExtractionResult, error) {
	var result types.ExtractionResult

	// Strip comments and string literals before matching to avoid false positives.
	stripped := stripComments(source)
	strippedNoStr := stripStrings(stripped)

	// nodeAt creates a Node at the given byte offset in the *original* stripped
	// source (line numbers derive from stripped which has the same newline
	// positions as source).
	nodeAt := func(kind types.NodeKind, schema, name, qname string, byteOffset int) types.Node {
		line := strings.Count(stripped[:byteOffset], "\n") + 1
		id := extraction.GenerateNodeID(filePath, string(kind), qname, line)
		return types.Node{
			ID:            id,
			Kind:          kind,
			Name:          name,
			QualifiedName: qname,
			FilePath:      filePath,
			Language:      types.LanguageSQL,
			StartLine:     line,
			EndLine:       line,
			IsExported:    true,
		}
	}

	// -- Tables --
	for _, m := range tableRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		// Group 1 = FOREIGN|EXTERNAL modifier (may be empty), Group 2 = table name
		isForeign := m[2] >= 0 && strings.TrimSpace(strippedNoStr[m[2]:m[3]]) != ""
		rawName := strippedNoStr[m[4]:m[5]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		node := nodeAt(types.NodeKindTable, schema, name, qname, m[0])
		if isForeign {
			node.Metadata = []byte(`{"foreign":true}`)
		}
		tableID := node.ID
		result.Nodes = append(result.Nodes, node)

		// Compute the table body paren-block once and pass it to all three consumers.
		// Using stripped (comment-only blanked) for structure; strippedNoStr for FK scan
		// so string-literal REFERENCES values cannot produce false FK edges.
		tableBody, tableBodyOff := findParenBlock(stripped, m[1])
		tableBodyNoStr, _ := findParenBlock(strippedNoStr, m[1])

		// Extract columns from the table body (the ( ... ) block following this match).
		colNodes, colEdges := extractColumns(filePath, source, stripped, tableID, name, qname, tableBody, tableBodyOff)
		result.Nodes = append(result.Nodes, colNodes...)
		result.Edges = append(result.Edges, colEdges...)

		// CP3: Extract constraint nodes from the same table body.
		anonCtrs := map[string]int{}
		conNodes, conEdges := extractConstraints(filePath, stripped, tableBody, tableBodyOff, tableID, name, anonCtrs)
		result.Nodes = append(result.Nodes, conNodes...)
		result.Edges = append(result.Edges, conEdges...)

		// CP4: FK → references. Scan the CREATE TABLE body for REFERENCES <target>.
		// Covers both inline column FKs (col TYPE REFERENCES t) and table-level
		// FOREIGN KEY (...) REFERENCES t.
		if tableBodyNoStr != "" {
			seenFKTargets := map[string]bool{}
			for _, rm := range inlineRefRE.FindAllStringSubmatchIndex(tableBodyNoStr, -1) {
				rawTgt := tableBodyNoStr[rm[2]:rm[3]]
				_, tgtName := parseQName(rawTgt)
				if tgtName == "" || isSQLRefKeyword(tgtName) || seenFKTargets[strings.ToLower(tgtName)] {
					continue
				}
				seenFKTargets[strings.ToLower(tgtName)] = true
				// Use approximate line from the table match start.
				line := strings.Count(stripped[:m[1]], "\n") + 1
				result.UnresolvedReferences = append(result.UnresolvedReferences,
					sqlRef(filePath, tableID, tgtName, types.EdgeKindReferences, line))
			}
		}
	}

	// Build a map of (kind, lower-name) → nodeID once so that the ALTER TABLE
	// and index lookup loops below run in O(1) per lookup instead of O(n).
	// All CREATE TABLE nodes have been appended before this point; no new table
	// nodes are added by the ALTER loops themselves.
	tableNodeIDMap := buildTableNodeIDMap(result.Nodes)

	// CP4: ALTER TABLE … FOREIGN KEY … REFERENCES <target> → references.
	for _, m := range alterFKRefRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawSrcTable := strippedNoStr[m[2]:m[3]]
		rawTgtTable := strippedNoStr[m[4]:m[5]]
		_, srcTableName := parseQName(rawSrcTable)
		_, tgtTableName := parseQName(rawTgtTable)
		if srcTableName == "" || tgtTableName == "" || isSQLRefKeyword(tgtTableName) {
			continue
		}
		srcNodeID := tableNodeIDMap[strings.ToLower(srcTableName)]
		if srcNodeID == "" {
			continue
		}
		line := strings.Count(stripped[:m[0]], "\n") + 1
		result.UnresolvedReferences = append(result.UnresolvedReferences,
			sqlRef(filePath, srcNodeID, tgtTableName, types.EdgeKindReferences, line))
	}

	// -- ALTER TABLE ADD COLUMN --
	for _, m := range alterAddColumnRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawTable := strippedNoStr[m[2]:m[3]]
		rawCol := strippedNoStr[m[4]:m[5]]
		_, tableName := parseQName(rawTable)
		colName := normIdent(rawCol)
		if tableName == "" || colName == "" {
			continue
		}
		line := strings.Count(stripped[:m[0]], "\n") + 1
		tableNodeID := tableNodeIDMap[strings.ToLower(tableName)]
		colQName := tableName + "." + colName
		colID := extraction.GenerateNodeID(filePath, string(types.NodeKindColumn), colQName, line)
		colNode := types.Node{
			ID:            colID,
			Kind:          types.NodeKindColumn,
			Name:          colName,
			QualifiedName: colQName,
			FilePath:      filePath,
			Language:      types.LanguageSQL,
			StartLine:     line,
			EndLine:       line,
			IsExported:    true,
		}
		result.Nodes = append(result.Nodes, colNode)
		if tableNodeID != "" {
			result.Edges = append(result.Edges, containsEdge(tableNodeID, colID))
		}
	}

	// -- ALTER TABLE ADD CONSTRAINT (named) --
	for _, m := range alterAddConstraintRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawTable := strippedNoStr[m[2]:m[3]]
		rawName := strippedNoStr[m[4]:m[5]]
		rawType := strippedNoStr[m[6]:m[7]]
		_, tableName := parseQName(rawTable)
		conName := normIdent(rawName)
		ctype := normalizeConstraintType(rawType)
		if tableName == "" || conName == "" {
			continue
		}
		line := strings.Count(stripped[:m[0]], "\n") + 1
		tableNodeID := tableNodeIDMap[strings.ToLower(tableName)]
		qname := tableName + "." + conName
		id := extraction.GenerateNodeID(filePath, string(types.NodeKindConstraint), qname, line)
		node := types.Node{
			ID:            id,
			Kind:          types.NodeKindConstraint,
			Name:          conName,
			QualifiedName: qname,
			FilePath:      filePath,
			Language:      types.LanguageSQL,
			StartLine:     line,
			EndLine:       line,
			IsExported:    true,
			Metadata:      buildConstraintMeta(ctype, ""),
		}
		result.Nodes = append(result.Nodes, node)
		if tableNodeID != "" {
			result.Edges = append(result.Edges, containsEdge(tableNodeID, id))
		}
	}

	// -- ALTER TABLE ADD <anonymous constraint> (no CONSTRAINT keyword) --
	// alterAddAnonConstraintRE requires ADD to be followed directly by the type
	// keyword (PRIMARY KEY / FOREIGN KEY / UNIQUE / CHECK) with no CONSTRAINT
	// token in between, so it structurally cannot match a named-constraint line
	// (ALTER TABLE t ADD CONSTRAINT foo …). No runtime guard is needed.
	// Count anonymous constraints per table to build stable synthesized names.
	anonAltCtrs := map[string]map[string]int{}
	for _, m := range alterAddAnonConstraintRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawTable := strippedNoStr[m[2]:m[3]]
		rawType := strippedNoStr[m[4]:m[5]]
		_, tableName := parseQName(rawTable)
		ctype := normalizeConstraintType(rawType)
		if tableName == "" {
			continue
		}
		if anonAltCtrs[tableName] == nil {
			anonAltCtrs[tableName] = map[string]int{}
		}
		anonAltCtrs[tableName][ctype]++
		n := anonAltCtrs[tableName][ctype]
		name := fmt.Sprintf("%s_%s_%d", tableName, anonSuffix(ctype), n)
		line := strings.Count(stripped[:m[0]], "\n") + 1
		tableNodeID := tableNodeIDMap[strings.ToLower(tableName)]
		qname := tableName + "." + name
		id := extraction.GenerateNodeID(filePath, string(types.NodeKindConstraint), qname, line)
		node := types.Node{
			ID:            id,
			Kind:          types.NodeKindConstraint,
			Name:          name,
			QualifiedName: qname,
			FilePath:      filePath,
			Language:      types.LanguageSQL,
			StartLine:     line,
			EndLine:       line,
			IsExported:    true,
			Metadata:      buildConstraintMeta(ctype, ""),
		}
		result.Nodes = append(result.Nodes, node)
		if tableNodeID != "" {
			result.Edges = append(result.Edges, containsEdge(tableNodeID, id))
		}
	}

	// -- Views --
	for _, m := range viewRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		// Group 1 = "MATERIALIZED " (may be empty), Group 2 = view name
		isMat := m[2] >= 0 && strings.TrimSpace(strippedNoStr[m[2]:m[3]]) != ""
		rawName := strippedNoStr[m[4]:m[5]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		node := nodeAt(types.NodeKindView, schema, name, qname, m[0])
		if isMat {
			node.Metadata = []byte(`{"materialized":true}`)
		}
		result.Nodes = append(result.Nodes, node)

		// CP4: view → source table references (FROM/JOIN in view body after AS).
		// Find the text after the AS keyword that terminates the CREATE VIEW header.
		// We scan from the end of the view RE match to a reasonable body window.
		viewBodyStart := m[1]
		viewBody := extractViewBody(strippedNoStr, viewBodyStart)
		seen := map[string]bool{}
		for _, bm := range viewBodyFROMRE.FindAllStringSubmatchIndex(viewBody, -1) {
			rawTgt := viewBody[bm[2]:bm[3]]
			_, tgtName := parseQName(rawTgt)
			if tgtName == "" || isSQLRefKeyword(tgtName) {
				continue
			}
			if seen[strings.ToLower(tgtName)] {
				continue
			}
			seen[strings.ToLower(tgtName)] = true
			byteOff := viewBodyStart + bm[2]
			line := strings.Count(stripped[:byteOff], "\n") + 1
			result.UnresolvedReferences = append(result.UnresolvedReferences,
				sqlRef(filePath, node.ID, tgtName, types.EdgeKindReferences, line))
		}
	}

	// -- Functions --
	for _, m := range functionRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		fnNode := nodeAt(types.NodeKindFunction, schema, name, qname, m[0])
		result.Nodes = append(result.Nodes, fnNode)

		// CP5: scan function body for reads (references), writes, and calls.
		body, bodyOff := extractRoutineBody(strippedNoStr, m[1])
		if body != "" {
			ctes := extractCTENames(body)
			bodyEdgeRefs := scanBodyEdges(filePath, fnNode.ID, body, bodyOff, stripped, ctes)
			result.UnresolvedReferences = append(result.UnresolvedReferences, bodyEdgeRefs...)
		}
	}

	// -- Procedures --
	for _, m := range procedureRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		procNode := nodeAt(types.NodeKindProcedure, schema, name, qname, m[0])
		result.Nodes = append(result.Nodes, procNode)

		// CP5: scan procedure body for reads (references), writes, and calls.
		body, bodyOff := extractRoutineBody(strippedNoStr, m[1])
		if body != "" {
			ctes := extractCTENames(body)
			bodyEdgeRefs := scanBodyEdges(filePath, procNode.ID, body, bodyOff, stripped, ctes)
			result.UnresolvedReferences = append(result.UnresolvedReferences, bodyEdgeRefs...)
		}
	}

	// -- Triggers --
	for _, m := range triggerRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		trgNode := nodeAt(types.NodeKindTrigger, schema, name, qname, m[0])
		result.Nodes = append(result.Nodes, trgNode)

		// CP4: trigger → ON table (references) + EXECUTE FUNCTION/PROCEDURE fn (calls).
		// Scan from the end of the trigger name match to end-of-statement.
		stmtText := extractStmtText(strippedNoStr, m[1])

		// ON <table>
		if om := triggerOnRE.FindStringSubmatchIndex(stmtText); om != nil {
			rawTgt := stmtText[om[2]:om[3]]
			_, tgtName := parseQName(rawTgt)
			if tgtName != "" && !isSQLRefKeyword(tgtName) {
				byteOff := m[1] + om[2]
				line := strings.Count(stripped[:byteOff], "\n") + 1
				result.UnresolvedReferences = append(result.UnresolvedReferences,
					sqlRef(filePath, trgNode.ID, tgtName, types.EdgeKindReferences, line))
			}
		}

		// EXECUTE [PROCEDURE|FUNCTION] <fn>
		if em := triggerExecFnRE.FindStringSubmatchIndex(stmtText); em != nil {
			rawFn := stmtText[em[2]:em[3]]
			_, fnName := parseQName(rawFn)
			if fnName != "" && !isSQLRefKeyword(fnName) {
				byteOff := m[1] + em[2]
				line := strings.Count(stripped[:byteOff], "\n") + 1
				result.UnresolvedReferences = append(result.UnresolvedReferences,
					sqlRef(filePath, trgNode.ID, fnName, types.EdgeKindCalls, line))
			}
		}
	}

	// -- Indexes --
	for _, m := range indexRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		rawTable := strippedNoStr[m[4]:m[5]]
		_, name := parseQName(rawName)
		_, tableName := parseQName(rawTable)
		qname := qualifiedName("", name)
		if name == "" {
			continue
		}
		line := strings.Count(stripped[:m[0]], "\n") + 1
		idxID := extraction.GenerateNodeID(filePath, string(types.NodeKindIndex), qname, line)
		idxNode := types.Node{
			ID:            idxID,
			Kind:          types.NodeKindIndex,
			Name:          name,
			QualifiedName: qname,
			FilePath:      filePath,
			Language:      types.LanguageSQL,
			StartLine:     line,
			EndLine:       line,
			IsExported:    true,
		}
		result.Nodes = append(result.Nodes, idxNode)
		// contains: table → index
		if tableName != "" {
			if tableNodeID := tableNodeIDMap[strings.ToLower(tableName)]; tableNodeID != "" {
				result.Edges = append(result.Edges, containsEdge(tableNodeID, idxID))
			}
		}
	}

	// -- Sequences --
	for _, m := range sequenceRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		result.Nodes = append(result.Nodes, nodeAt(types.NodeKindSequence, schema, name, qname, m[0]))
	}

	// -- Schemas --
	for _, m := range schemaRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		_, name := parseQName(rawName)
		if name == "" {
			continue
		}
		result.Nodes = append(result.Nodes, nodeAt(types.NodeKindNamespace, "", name, name, m[0]))
	}

	// -- Databases --
	for _, m := range databaseRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		_, name := parseQName(rawName)
		if name == "" {
			continue
		}
		result.Nodes = append(result.Nodes, nodeAt(types.NodeKindModule, "", name, name, m[0]))
	}

	// -- Types (ENUM, TABLE type, FROM alias, composite) --
	// Process typeRE first (AS ENUM / AS TABLE / FROM).
	seenTypeNames := map[string]bool{}
	for _, m := range typeRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		seenTypeNames[strings.ToLower(qname)] = true

		// m[4]:m[5] = group 2 = "ENUM" or "TABLE" (from AS clause)
		// m[6]:m[7] = group 3 = FROM base type name
		isEnum := m[4] >= 0 && strings.EqualFold(strippedNoStr[m[4]:m[5]], "ENUM")
		isTable := m[4] >= 0 && strings.EqualFold(strippedNoStr[m[4]:m[5]], "TABLE")
		isFrom := m[6] >= 0

		if isEnum {
			enumNode := nodeAt(types.NodeKindEnum, schema, name, qname, m[0])
			result.Nodes = append(result.Nodes, enumNode)
			// Extract enum member labels from the original (un-stripped) source.
			// We use the position of this match to find the ENUM(...) body nearby.
			afterMatch := source[m[0]:]
			if em := enumValuesRE.FindStringSubmatchIndex(afterMatch); em != nil {
				labelBlock := afterMatch[em[2]:em[3]]
				for _, lm := range singleQuotedLabelRE.FindAllStringSubmatchIndex(labelBlock, -1) {
					label := labelBlock[lm[2]:lm[3]]
					if label == "" {
						continue
					}
					byteOff := m[0] + em[2] + lm[2]
					memberLine := strings.Count(stripped[:byteOff], "\n") + 1
					memberQName := qname + "." + label
					memberID := extraction.GenerateNodeID(filePath, string(types.NodeKindEnumMember), memberQName, memberLine)
					memberNode := types.Node{
						ID:            memberID,
						Kind:          types.NodeKindEnumMember,
						Name:          label,
						QualifiedName: memberQName,
						FilePath:      filePath,
						Language:      types.LanguageSQL,
						StartLine:     memberLine,
						EndLine:       memberLine,
						IsExported:    true,
					}
					result.Nodes = append(result.Nodes, memberNode)
					result.Edges = append(result.Edges, containsEdge(enumNode.ID, memberID))
				}
			}
		} else if isTable {
			meta, _ := json.Marshal(map[string]bool{"table_type": true})
			node := nodeAt(types.NodeKindTypeAlias, schema, name, qname, m[0])
			node.Metadata = meta
			result.Nodes = append(result.Nodes, node)
		} else if isFrom {
			// T-SQL alias type: CREATE TYPE <name> FROM <base>
			result.Nodes = append(result.Nodes, nodeAt(types.NodeKindTypeAlias, schema, name, qname, m[0]))
		}
	}

	// Composite CREATE TYPE (no AS/FROM — matches only bare CREATE TYPE <name>)
	for _, m := range typeCompositeRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		if seenTypeNames[strings.ToLower(qname)] {
			continue // already handled by typeRE
		}
		result.Nodes = append(result.Nodes, nodeAt(types.NodeKindTypeAlias, schema, name, qname, m[0]))
	}

	// -- DOMAIN → type_alias --
	for _, m := range domainRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		result.Nodes = append(result.Nodes, nodeAt(types.NodeKindTypeAlias, schema, name, qname, m[0]))
	}

	// -- SYNONYM → type_alias + metadata --
	for _, m := range synonymRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		meta, _ := json.Marshal(map[string]bool{"synonym": true})
		node := nodeAt(types.NodeKindTypeAlias, schema, name, qname, m[0])
		node.Metadata = meta
		result.Nodes = append(result.Nodes, node)

		// CP4: synonym → target references.
		// Group 3 (index 4:5 after two-group name) = FOR <target>
		if m[4] >= 0 && m[5] >= 0 {
			rawTgt := strippedNoStr[m[4]:m[5]]
			_, tgtName := parseQName(rawTgt)
			if tgtName != "" && !isSQLRefKeyword(tgtName) {
				line := strings.Count(stripped[:m[4]], "\n") + 1
				result.UnresolvedReferences = append(result.UnresolvedReferences,
					sqlRef(filePath, node.ID, tgtName, types.EdgeKindReferences, line))
			}
		}
	}

	// -- POLICY (RLS) --
	for _, m := range policyRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawPolicyName := strippedNoStr[m[2]:m[3]]
		rawTableName := strippedNoStr[m[4]:m[5]]
		policyName := normIdent(rawPolicyName)
		_, tableName := parseQName(rawTableName)
		if policyName == "" {
			continue
		}
		line := strings.Count(stripped[:m[0]], "\n") + 1
		policyID := extraction.GenerateNodeID(filePath, string(types.NodeKindPolicy), policyName, line)
		policyNode := types.Node{
			ID:            policyID,
			Kind:          types.NodeKindPolicy,
			Name:          policyName,
			QualifiedName: policyName,
			FilePath:      filePath,
			Language:      types.LanguageSQL,
			StartLine:     line,
			EndLine:       line,
			IsExported:    true,
		}
		result.Nodes = append(result.Nodes, policyNode)

		// references → table
		if tableName != "" && !isSQLRefKeyword(tableName) {
			tblLine := strings.Count(stripped[:m[4]], "\n") + 1
			result.UnresolvedReferences = append(result.UnresolvedReferences,
				sqlRef(filePath, policyID, tableName, types.EdgeKindReferences, tblLine))
		}

		// calls → functions found ONLY in USING (...) and WITH CHECK (...) expressions.
		// F-7: scope to these expression blocks only, not the full statement text.
		// Scanning the whole statement grabs SQL builtins in non-expression positions
		// (e.g. AS PERMISSIVE, TO public) and produces noise that never resolves.
		stmtText := extractStmtText(strippedNoStr, m[1])
		seenFn := map[string]bool{}
		for _, um := range usingWithCheckRE.FindAllStringSubmatchIndex(stmtText, -1) {
			exprBlock := stmtText[um[2]:um[3]]
			exprBlockOff := m[1] + um[2]
			for _, fm := range fnCallInExprRE.FindAllStringSubmatchIndex(exprBlock, -1) {
				fnName := exprBlock[fm[2]:fm[3]]
				if isSQLRefKeyword(fnName) || seenFn[strings.ToLower(fnName)] {
					continue
				}
				seenFn[strings.ToLower(fnName)] = true
				byteOff := exprBlockOff + fm[2]
				fnLine := strings.Count(stripped[:byteOff], "\n") + 1
				result.UnresolvedReferences = append(result.UnresolvedReferences,
					sqlRef(filePath, policyID, fnName, types.EdgeKindCalls, fnLine))
			}
		}
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Column extraction helper
// ---------------------------------------------------------------------------

// extractColumns scans the CREATE TABLE body and emits column nodes + contains
// edges. Constraint lines are skipped.
//
// body and bodyOff are the pre-computed paren-block content and start offset
// within stripped (callers compute these once via findParenBlock to avoid
// redundant walks over the same character range).
//
// Two-source-pass design: the column list structure (names, constraint skips)
// is parsed from the pre-computed body (derived from `stripped`, comments
// blanked out) to avoid false matches inside string-literal default values.
// The GENERATED/computed-column check then re-reads the corresponding line
// from the original `source` so that keywords which were stripped earlier are
// visible for metadata detection.
func extractColumns(
	filePath, source, stripped string,
	tableID, tableName, tableQName string,
	body string, bodyOff int,
) (nodes []types.Node, edges []types.Edge) {

	if body == "" {
		return
	}

	// Split body into lines; process each as a potential column definition.
	lines := strings.Split(body, "\n")
	lineOffset := strings.Count(stripped[:bodyOff], "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Remove trailing comma.
		trimmed = strings.TrimRight(trimmed, ", \t")
		if trimmed == "" {
			continue
		}

		// Check if this line starts with a constraint keyword — skip it.
		firstWord := strings.ToUpper(strings.Fields(trimmed)[0])
		if constraintKeywords[firstWord] {
			continue
		}

		// Extract the column name: the first identifier on the line.
		colName := extractFirstIdent(trimmed)
		if colName == "" {
			continue
		}
		// Skip if it looks like SQL keywords rather than a column name.
		if isSQLKeyword(colName) {
			continue
		}

		lineNum := lineOffset + i + 1
		colQName := fmt.Sprintf("%s.%s", tableQName, colName)
		colID := extraction.GenerateNodeID(filePath, string(types.NodeKindColumn), colQName, lineNum)

		colNode := types.Node{
			ID:            colID,
			Kind:          types.NodeKindColumn,
			Name:          colName,
			QualifiedName: colQName,
			FilePath:      filePath,
			Language:      types.LanguageSQL,
			StartLine:     lineNum,
			EndLine:       lineNum,
			IsExported:    true,
		}

		// Detect GENERATED / computed column.
		// Use the original source line (not stripped) for this check so we can
		// see keywords that might have been inside strings on other lines.
		origLine := getSourceLine(source, lineOffset+i)
		if generatedMarkerRE.MatchString(origLine) {
			colNode.Metadata = []byte(`{"generated":true}`)
		}

		nodes = append(nodes, colNode)
		edges = append(edges, containsEdge(tableID, colID))
	}
	return
}

// ---------------------------------------------------------------------------
// Constraint extraction helpers
// ---------------------------------------------------------------------------

// normalizeConstraintType maps the SQL keyword(s) to a canonical constraint_type string.
func normalizeConstraintType(raw string) string {
	up := strings.ToUpper(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(up, "PRIMARY"):
		return "primary_key"
	case strings.HasPrefix(up, "FOREIGN"):
		return "foreign_key"
	case up == "UNIQUE":
		return "unique"
	case up == "CHECK":
		return "check"
	default:
		return strings.ToLower(up)
	}
}

// namedConstraintLineRE matches a CONSTRAINT <name> <type> line inside a CREATE TABLE body.
// Group 1 = constraint name, Group 2 = constraint type keyword(s).
var namedConstraintLineRE = regexp.MustCompile(`(?i)^\s*CONSTRAINT\s+(` + sqlIdentOnlyRaw + `)\s+(PRIMARY\s+KEY|FOREIGN\s+KEY|UNIQUE|CHECK)\b`)

// anonConstraintLineRE matches an anonymous table-level constraint line: PRIMARY KEY / UNIQUE / CHECK / FOREIGN KEY.
// Group 1 = constraint type keyword(s).
var anonConstraintLineRE = regexp.MustCompile(`(?i)^\s*(PRIMARY\s+KEY|FOREIGN\s+KEY|UNIQUE|CHECK)\b`)

// extractConstraints scans the CREATE TABLE body for named and anonymous
// table-level constraints and emits constraint nodes + contains edges.
// body and bodyOff are the pre-computed paren-block content and start offset
// within stripped (callers compute these once via findParenBlock shared with
// extractColumns). stripped is needed to compute the line-number base.
func extractConstraints(
	filePath, stripped string,
	body string, bodyOff int,
	tableID, tableName string,
	anonCounters map[string]int, // mutable, shared across tables — caller passes a fresh map
) (nodes []types.Node, edges []types.Edge) {

	if body == "" {
		return
	}

	lines := strings.Split(body, "\n")
	lineOffset := strings.Count(stripped[:bodyOff], "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimRight(trimmed, ", \t")

		// Named constraint: CONSTRAINT <name> <type> ...
		if nm := namedConstraintLineRE.FindStringSubmatch(trimmed); nm != nil {
			name := normIdent(nm[1])
			ctype := normalizeConstraintType(nm[2])
			lineNum := lineOffset + i + 1
			qname := tableName + "." + name
			id := extraction.GenerateNodeID(filePath, string(types.NodeKindConstraint), qname, lineNum)
			meta := buildConstraintMeta(ctype, "")
			node := types.Node{
				ID:            id,
				Kind:          types.NodeKindConstraint,
				Name:          name,
				QualifiedName: qname,
				FilePath:      filePath,
				Language:      types.LanguageSQL,
				StartLine:     lineNum,
				EndLine:       lineNum,
				IsExported:    true,
				Metadata:      meta,
			}
			nodes = append(nodes, node)
			edges = append(edges, containsEdge(tableID, id))
			continue
		}

		// Anonymous table-level constraint: PRIMARY KEY / UNIQUE / CHECK / FOREIGN KEY
		if am := anonConstraintLineRE.FindStringSubmatch(trimmed); am != nil {
			ctype := normalizeConstraintType(am[1])
			anonCounters[ctype]++
			name := fmt.Sprintf("%s_%s_%d", tableName, anonSuffix(ctype), anonCounters[ctype])
			lineNum := lineOffset + i + 1
			qname := tableName + "." + name
			id := extraction.GenerateNodeID(filePath, string(types.NodeKindConstraint), qname, lineNum)
			meta := buildConstraintMeta(ctype, "")
			node := types.Node{
				ID:            id,
				Kind:          types.NodeKindConstraint,
				Name:          name,
				QualifiedName: qname,
				FilePath:      filePath,
				Language:      types.LanguageSQL,
				StartLine:     lineNum,
				EndLine:       lineNum,
				IsExported:    true,
				Metadata:      meta,
			}
			nodes = append(nodes, node)
			edges = append(edges, containsEdge(tableID, id))
		}
	}
	return
}

// anonSuffix returns the short suffix used in synthesized anonymous constraint names.
func anonSuffix(ctype string) string {
	switch ctype {
	case "primary_key":
		return "pk"
	case "foreign_key":
		return "fk"
	case "unique":
		return "unique"
	case "check":
		return "check"
	default:
		return ctype
	}
}

// buildConstraintMeta constructs the Metadata JSON for a constraint node.
// references is the FK target table name (empty if not FK or CP4 not yet active).
func buildConstraintMeta(ctype, references string) json.RawMessage {
	m := map[string]string{"constraint_type": ctype}
	if references != "" {
		m["references"] = references
	}
	b, _ := json.Marshal(m)
	return b
}

// findParenBlock finds the matching parenthesised block starting at or after
// startOffset in source. Returns the content between ( and ) (exclusive) and
// the byte offset of the opening '('.
func findParenBlock(source string, startOffset int) (string, int) {
	// Find first '(' at or after startOffset.
	idx := strings.IndexByte(source[startOffset:], '(')
	if idx < 0 {
		return "", 0
	}
	open := startOffset + idx
	depth := 0
	for i := open; i < len(source); i++ {
		switch source[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return source[open+1 : i], open + 1
			}
		}
	}
	return "", 0
}

// extractFirstIdent extracts the first SQL identifier from a line.
// Handles quoted, backtick, and bracket identifiers.
func extractFirstIdent(line string) string {
	line = strings.TrimSpace(line)
	if len(line) == 0 {
		return ""
	}
	switch line[0] {
	case '"':
		end := strings.Index(line[1:], `"`)
		if end < 0 {
			return ""
		}
		return line[1 : end+1]
	case '`':
		end := strings.Index(line[1:], "`")
		if end < 0 {
			return ""
		}
		return line[1 : end+1]
	case '[':
		end := strings.Index(line[1:], "]")
		if end < 0 {
			return ""
		}
		return line[1 : end+1]
	default:
		// Bare identifier: stop at whitespace or non-ident characters.
		end := 0
		for end < len(line) {
			c := line[end]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$' {
				end++
			} else {
				break
			}
		}
		return line[:end]
	}
}

// getSourceLine returns the (i+1)'th line (0-indexed) of source. Used to
// check for GENERATED markers in the original (non-stripped) source.
func getSourceLine(source string, lineIdx int) string {
	lines := strings.SplitN(source, "\n", lineIdx+2)
	if lineIdx < len(lines) {
		return lines[lineIdx]
	}
	return ""
}

// sqlKeywords is a set of SQL reserved words that cannot be column names.
var sqlKeywords = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "INSERT": true, "UPDATE": true,
	"DELETE": true, "CREATE": true, "ALTER": true, "DROP": true, "TABLE": true,
	"INDEX": true, "VIEW": true, "TRIGGER": true, "PROCEDURE": true, "FUNCTION": true,
	"BEGIN": true, "END": true, "AS": true, "ON": true, "SET": true, "INTO": true,
	"VALUES": true, "AND": true, "OR": true, "NOT": true, "NULL": true,
	"RETURNS": true, "RETURN": true, "DECLARE": true, "EXEC": true, "EXECUTE": true,
}

func isSQLKeyword(s string) bool {
	return sqlKeywords[strings.ToUpper(s)]
}

// buildTableNodeIDMap builds a lower-name → nodeID map for all table nodes in
// the given slice. Used by ALTER TABLE and index loops for O(1) lookups instead
// of O(n) linear scans per lookup.
func buildTableNodeIDMap(nodes []types.Node) map[string]string {
	m := make(map[string]string, len(nodes))
	for _, n := range nodes {
		if n.Kind == types.NodeKindTable {
			m[strings.ToLower(n.Name)] = n.ID
		}
	}
	return m
}

// ---------------------------------------------------------------------------
// Unresolved reference helper
// ---------------------------------------------------------------------------

// sqlRef builds a types.UnresolvedReference for a SQL source node referencing
// a named target. kind is EdgeKindReferences or EdgeKindCalls.
func sqlRef(filePath, fromNodeID, targetName string, kind types.EdgeKind, line int) types.UnresolvedReference {
	return types.UnresolvedReference{
		ID:            extraction.GenerateRefID(fromNodeID, targetName, string(kind), line, 0),
		FromNodeID:    fromNodeID,
		ReferenceName: targetName,
		ReferenceKind: kind,
		Line:          line,
		FilePath:      filePath,
		Language:      types.LanguageSQL,
	}
}

// sqlKeywordsForRef is the set of SQL keywords that must not be emitted as
// reference targets (prevents false edges to imaginary nodes).
// F-6: LATERAL, UNNEST, ROWS added so JOIN LATERAL / UNNEST() are not captured
// as table names.
var sqlKeywordsForRef = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "ON": true, "SET": true,
	"BEGIN": true, "END": true, "NOT": true, "NULL": true, "AND": true, "OR": true,
	"AS": true, "BY": true, "INTO": true, "VALUES": true, "WITH": true,
	"TABLE": true, "INDEX": true, "VIEW": true, "TRIGGER": true, "FUNCTION": true,
	"PROCEDURE": true, "CREATE": true, "ALTER": true, "DROP": true, "INSERT": true,
	"UPDATE": true, "DELETE": true, "MERGE": true, "EXEC": true, "EXECUTE": true,
	"USING": true, "CHECK": true, "EACH": true, "ROW": true, "FOR": true,
	"AFTER": true, "BEFORE": true, "INSTEAD": true, "OF": true, "WHEN": true,
	"RETURNS": true, "RETURN": true, "DECLARE": true, "IF": true, "ELSE": true,
	"THEN": true, "LOOP": true, "WHILE": true, "DO": true, "CASE": true,
	"LANGUAGE": true, "NEW": true, "OLD": true, "FOUND": true,
	// F-6: table-function and clause modifiers that appear after FROM/JOIN.
	"LATERAL": true, "UNNEST": true, "ROWS": true,
}

func isSQLRefKeyword(s string) bool {
	return sqlKeywordsForRef[strings.ToUpper(s)]
}

// ---------------------------------------------------------------------------
// Statement-body extraction helpers
// ---------------------------------------------------------------------------

// extractViewBody returns the text of a view's SELECT body.
// startOffset is the byte offset just past the end of the view RE match (m[1]).
// We scan forward to find "AS" (or "AS SELECT") and return from there to the
// next statement boundary (semicolon or next top-level CREATE keyword).
// Returns a substring of source (zero-copy where possible).
func extractViewBody(source string, startOffset int) string {
	tail := source[startOffset:]
	// Find AS keyword that precedes the view body.
	loc := bodyAsRE.FindStringIndex(tail)
	if loc == nil {
		return ""
	}
	body := tail[loc[1]:]
	// Trim to end-of-statement: next semicolon OR next top-level CREATE.
	return trimToStatementEnd(body)
}

// extractStmtText returns the remaining text of the current statement starting
// at startOffset (typically m[1] — just past the object name match).
// Extends to the next statement boundary: semicolon or top-level CREATE.
func extractStmtText(source string, startOffset int) string {
	if startOffset >= len(source) {
		return ""
	}
	return trimToStatementEnd(source[startOffset:])
}

// nextStmtRE matches a semicolon or a top-level CREATE statement start.
var nextStmtRE = regexp.MustCompile(`(?im)(?:;|^[ \t]*CREATE\b)`)

// trimToStatementEnd trims text to end at the first statement boundary
// (semicolon or next top-level CREATE keyword at line start).
// If no boundary is found, returns the full text.
func trimToStatementEnd(text string) string {
	loc := nextStmtRE.FindStringIndex(text)
	if loc == nil {
		return text
	}
	return text[:loc[0]]
}

// ---------------------------------------------------------------------------
// CP5: Routine body extraction helpers
// ---------------------------------------------------------------------------

// routineDollarRE matches the opening dollar-quote tag ($$  or  $tag$) in a
// PG function/procedure body. Group 1 = the full tag including dollar signs.
var routineDollarRE = regexp.MustCompile(`(\$[A-Za-z0-9_]*\$)`)

// routineBeginRE matches the BEGIN keyword that starts a T-SQL or PG block.
var routineBeginRE = regexp.MustCompile(`(?i)\bBEGIN\b`)

// routineTokenRE matches BEGIN or END keywords for depth-tracking inside blocks.
var routineTokenRE = regexp.MustCompile(`(?i)\bBEGIN\b|\bEND\b`)

// bodyAsRE matches the AS keyword used in view and routine body-extraction paths.
var bodyAsRE = regexp.MustCompile(`(?i)\bAS\b`)

// extractRoutineBody returns the body text of a function or procedure and the
// byte offset of the body's start within source.
// startOffset is the byte offset just past the end of the CREATE FUNCTION/PROCEDURE
// name match (m[1]).
//
// Returning the offset directly avoids the fragile strings.Index re-search that
// callers would otherwise need to compute the body's position in source.
//
// Strategy:
//  1. Dollar-quoted body (PG): $tag$...$tag$ — extract the content between
//     the outermost dollar-quote delimiters.
//  2. BEGIN...END block (T-SQL / PG without dollar quotes): find BEGIN, track
//     depth until matching END, return that range.
//  3. Fallback: single AS clause up to statement end (simple inline body).
func extractRoutineBody(source string, startOffset int) (string, int) {
	if startOffset >= len(source) {
		return "", 0
	}
	tail := source[startOffset:]

	// Try dollar-quoted body first: $$...$$  or  $tag$...$tag$
	if dm := routineDollarRE.FindStringIndex(tail); dm != nil {
		tag := tail[dm[0]:dm[1]]
		// Find the closing tag.
		closeIdx := strings.Index(tail[dm[1]:], tag)
		if closeIdx >= 0 {
			bodyOff := startOffset + dm[1]
			return tail[dm[1] : dm[1]+closeIdx], bodyOff
		}
	}

	// Try BEGIN...END depth tracking.
	if loc := routineBeginRE.FindStringIndex(tail); loc != nil {
		body := tail[loc[0]:]
		bodyOff := startOffset + loc[0]
		depth := 0
		for _, m := range routineTokenRE.FindAllStringIndex(body, -1) {
			word := strings.ToUpper(body[m[0]:m[1]])
			if word == "BEGIN" {
				depth++
			} else if word == "END" {
				depth--
				if depth == 0 {
					return body[:m[1]], bodyOff
				}
			}
		}
		return body, bodyOff // unclosed — return what we have
	}

	// Fallback: after AS keyword, to statement end.
	if loc := bodyAsRE.FindStringIndex(tail); loc != nil {
		bodyOff := startOffset + loc[1]
		return trimToStatementEnd(tail[loc[1]:]), bodyOff
	}
	return "", 0
}

// extractCTENames returns the set of CTE names bound by WITH <name> AS (...)
// in a body text. Names are lower-cased for case-insensitive comparison.
// WHY: CTE names are statement-local — a FROM/INSERT ref to a CTE name must
// not produce an edge to a non-existent table.
func extractCTENames(body string) map[string]bool {
	ctes := map[string]bool{}
	for _, m := range cteNameRE.FindAllStringSubmatch(body, -1) {
		if len(m) > 1 {
			ctes[strings.ToLower(normIdent(m[1]))] = true
		}
	}
	return ctes
}

// scanBodyEdges scans a stripped routine/view body for FROM/JOIN (references),
// INSERT INTO / UPDATE / DELETE FROM / MERGE INTO (writes), and EXEC/CALL
// (calls) targets. Emits UnresolvedReferences for each non-keyword, non-CTE
// target. fromNodeID is the routine/view node that owns the body.
// bodyBaseOffset is the byte offset in the original stripped source where
// body begins (used for accurate line-number calculation).
func scanBodyEdges(
	filePath string,
	fromNodeID string,
	body string,
	bodyBaseOffset int,
	strippedFull string,
	cteShadow map[string]bool,
) []types.UnresolvedReference {
	var refs []types.UnresolvedReference
	seen := map[string]map[types.EdgeKind]bool{}

	addRef := func(name string, kind types.EdgeKind, matchOff int) {
		lower := strings.ToLower(name)
		if isSQLRefKeyword(name) || cteShadow[lower] {
			return
		}
		if seen[lower] == nil {
			seen[lower] = map[types.EdgeKind]bool{}
		}
		if seen[lower][kind] {
			return // deduplicate same name+kind
		}
		seen[lower][kind] = true
		byteOff := bodyBaseOffset + matchOff
		if byteOff > len(strippedFull) {
			byteOff = len(strippedFull)
		}
		line := strings.Count(strippedFull[:byteOff], "\n") + 1
		refs = append(refs, sqlRef(filePath, fromNodeID, name, kind, line))
	}

	// FROM / JOIN → references
	for _, m := range viewBodyFROMRE.FindAllStringSubmatchIndex(body, -1) {
		rawName := body[m[2]:m[3]]
		_, name := parseQName(rawName)
		if name != "" {
			addRef(name, types.EdgeKindReferences, m[2])
		}
	}

	// INSERT INTO → writes
	for _, m := range bodyInsertIntoRE.FindAllStringSubmatchIndex(body, -1) {
		rawName := body[m[2]:m[3]]
		_, name := parseQName(rawName)
		if name != "" {
			addRef(name, types.EdgeKindWrites, m[2])
		}
	}

	// UPDATE <name> SET → writes
	for _, m := range bodyUpdateRE.FindAllStringSubmatchIndex(body, -1) {
		rawName := body[m[2]:m[3]]
		_, name := parseQName(rawName)
		if name != "" {
			addRef(name, types.EdgeKindWrites, m[2])
		}
	}

	// DELETE FROM → writes
	for _, m := range bodyDeleteFromRE.FindAllStringSubmatchIndex(body, -1) {
		rawName := body[m[2]:m[3]]
		_, name := parseQName(rawName)
		if name != "" {
			addRef(name, types.EdgeKindWrites, m[2])
		}
	}

	// MERGE INTO → writes
	for _, m := range bodyMergeIntoRE.FindAllStringSubmatchIndex(body, -1) {
		rawName := body[m[2]:m[3]]
		_, name := parseQName(rawName)
		if name != "" {
			addRef(name, types.EdgeKindWrites, m[2])
		}
	}

	// EXEC[UTE] / CALL → calls
	for _, m := range bodyExecCallRE.FindAllStringSubmatchIndex(body, -1) {
		rawName := body[m[2]:m[3]]
		_, name := parseQName(rawName)
		if name != "" {
			addRef(name, types.EdgeKindCalls, m[2])
		}
	}

	return refs
}
