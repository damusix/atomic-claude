package standalone

// Regex-based SQL extractor — no grammar, so comments and single-quoted strings
// are blanked before matching or a CREATE inside one mints a node.
// Dialects: Postgres (ANSI "quotes"), MySQL (backticks), T-SQL ([brackets], GO
// terminators, CREATE OR ALTER).

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// --- Identifier helpers ---

// One SQL identifier in any quoting style: bare, "ANSI", `MySQL`, [T-SQL].
const sqlIdentPat = `(?:"([^"]+)"|` + "`([^`]+)`" + `|\[([^\]]+)\]|([A-Za-z_][A-Za-z0-9_$]*))`

// Optionally schema-qualified identifier, up to 3 parts.
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

// parseQName splits a dot-separated SQL name into (schemaOrEmpty, name).
func parseQName(raw string) (schema, name string) {
	parts := splitQName(raw)
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return "", normIdent(parts[0])
	default:
		name = normIdent(parts[len(parts)-1])
		schemaParts := make([]string, len(parts)-1)
		for i, p := range parts[:len(parts)-1] {
			schemaParts[i] = normIdent(p)
		}
		schema = strings.Join(schemaParts, ".")
		return schema, name
	}
}

// splitQName splits on dots, ignoring dots inside quote characters.
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

// --- Comment / string stripping ---

var stripLineCommentsRE = regexp.MustCompile(`--[^\n]*`)

var stripBlockCommentsRE = regexp.MustCompile(`(?s)/\*.*?\*/`)

// Handles the doubled single-quote escape, not backslash escapes.
var stripSingleQuotedRE = regexp.MustCompile(`'(?:[^']|'')*'`)

// stripComments blanks -- and /* */ comments in place: the result keeps source's
// byte length and newlines, so match offsets still map to line numbers.
func stripComments(source string) string {
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
	result = stripLineCommentsRE.ReplaceAllStringFunc(result, func(m string) string {
		return strings.Repeat(" ", len(m))
	})
	return result
}

// blankPreserveNewlines keeps newlines so line-number offsets stay stable.
func blankPreserveNewlines(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		if c == '\n' {
			b.WriteByte('\n')
		} else {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// padTo forces s to exactly n bytes so a substitution keeps source offsets intact.
// An over-long s blanks the span rather than truncating: the harvest already
// captured the edge, so blanking is lossless where a cut SQL token would not be.
func padTo(s string, n int) string {
	if len(s) == n {
		return s
	}
	if len(s) > n {
		var b strings.Builder
		b.Grow(n)
		for i := 0; i < n; i++ {
			b.WriteByte(' ')
		}
		return b.String()
	}
	var b strings.Builder
	b.Grow(n)
	b.WriteString(s)
	for b.Len() < n {
		b.WriteByte(' ')
	}
	return b.String()
}

// stripStrings blanks single-quoted literals so DDL text inside a default value
// cannot match as SQL.
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

// --- Regex patterns for CREATE statements ---

// Optional CREATE modifiers: OR REPLACE / OR ALTER / IF NOT EXISTS.
const modPat = `(?:(?:OR\s+(?:REPLACE|ALTER)|IF\s+NOT\s+EXISTS)\s+)*`

// Class modifiers between CREATE and TABLE. LOCAL/GLOBAL are legal only as a
// TEMP/TEMPORARY prefix — bare LOCAL TABLE is invalid and must not match.
const tableClassPat = `(?:(?:TRANSIENT|VOLATILE|(?:(?:LOCAL|GLOBAL)\s+)?(?:TEMPORARY|TEMP))\s+)?`

// Snowflake modifiers between CREATE and VIEW; multiple may stack.
const viewSecurityPat = `(?:(?:SECURE|RECURSIVE|TEMPORARY|TEMP)\s+)*`

// CREATE [FOREIGN|EXTERNAL] [class] TABLE [IF NOT EXISTS] <name>
var tableRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `(?:(FOREIGN|EXTERNAL)\s+)?` + tableClassPat + `TABLE\s+` + modPat + `(` + sqlQNameRaw + `)`)

// CREATE [OR REPLACE] [SECURE|RECURSIVE] [MATERIALIZED] VIEW <name>
var viewRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + viewSecurityPat + `(MATERIALIZED\s+)?VIEW\s+` + modPat + `(` + sqlQNameRaw + `)`)

// CREATE [OR REPLACE|OR ALTER] FUNCTION <name>
var functionRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `FUNCTION\s+` + modPat + `(` + sqlQNameRaw + `)`)

// CREATE [OR REPLACE|OR ALTER] PROC[EDURE] <name>
var procedureRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `PROC(?:EDURE)?\s+` + modPat + `(` + sqlQNameRaw + `)`)

// CREATE [OR REPLACE] TRIGGER <name>
var triggerRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `TRIGGER\s+` + modPat + `(` + sqlQNameRaw + `)`)

// CREATE [UNIQUE] INDEX <name> ON <table>
var indexRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+(?:UNIQUE\s+)?INDEX\s+` + modPat + `(` + sqlQNameRaw + `)\s+ON\s+(` + sqlQNameRaw + `)`)

// CREATE SEQUENCE <name>
var sequenceRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `SEQUENCE\s+` + modPat + `(` + sqlQNameRaw + `)`)

// CREATE [OR REPLACE] [TEMPORARY] STAGE [IF NOT EXISTS] <name> — Snowflake.
var stageRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `(?:(?:TEMPORARY|TEMP)\s+)?STAGE\s+` + modPat + `(` + sqlQNameRaw + `)`)

// CREATE [OR REPLACE] [TEMPORARY] FILE FORMAT [IF NOT EXISTS] <name> — Snowflake.
var fileFormatRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `(?:(?:TEMPORARY|TEMP)\s+)?FILE\s+FORMAT\s+` + modPat + `(` + sqlQNameRaw + `)`)

// CREATE [OR REPLACE] STREAM <name> ON <kind> <source> — Snowflake CDC.
// Multi-word kinds precede bare TABLE so the alternation takes the full phrase.
var streamRE = regexp.MustCompile(
	`(?im)^[ \t]*CREATE\s+` + modPat + `STREAM\s+` + modPat + `(` + sqlQNameRaw + `)` +
		`\s+ON\s+(?:EXTERNAL\s+TABLE|DYNAMIC\s+TABLE|EVENT\s+TABLE|TABLE|VIEW|STAGE)\s+(` + sqlQNameRaw + `)`,
)

// CREATE [OR REPLACE] TASK [IF NOT EXISTS] <name> — Snowflake.
var taskRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `TASK\s+` + modPat + `(` + sqlQNameRaw + `)`)

// AFTER <t1>[, <t2>…] task predecessors. AFTER is in sqlKeywordsForRef, so
// scanBodyEdges drops it — the list needs its own pass over the task statement.
var taskAfterRE = regexp.MustCompile(`(?i)\bAFTER\s+((?:` + sqlQNameRaw + `)(?:\s*,\s*(?:` + sqlQNameRaw + `))*)`)

var taskPredecessorSplitRE = regexp.MustCompile(`\s*,\s*`)

// CREATE SCHEMA <name>
var schemaRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `SCHEMA\s+` + modPat + `(` + sqlQNameRaw + `)`)

// CREATE DATABASE <name>
var databaseRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `DATABASE\s+` + modPat + `(` + sqlQNameRaw + `)`)

// CREATE DOMAIN <name>, optionally inside a DO $$ BEGIN wrapper: Postgres has
// no IF NOT EXISTS for DOMAIN, so every idempotent script wraps it.
var domainRE = regexp.MustCompile(`(?im)^[ \t]*(?:DO\s+\$[^$]*\$\s*BEGIN\s+)?CREATE\s+` + modPat + `DOMAIN\s+` + modPat + `(` + sqlQNameRaw + `)`)

// CREATE SYNONYM <name> FOR <target>
var synonymRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `SYNONYM\s+` + modPat + `(` + sqlQNameRaw + `)\s+FOR\s+(` + sqlQNameRaw + `)`)

// CREATE POLICY <name> ON <table>
var policyRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `POLICY\s+(` + sqlIdentOnlyRaw + `)\s+ON\s+(` + sqlQNameRaw + `)`)

// ON <table> in a CREATE TRIGGER, matched after the trigger node is found.
var triggerOnRE = regexp.MustCompile(`(?i)\bON\s+(` + sqlQNameRaw + `)`)

// EXECUTE [PROCEDURE|FUNCTION] <fn> in a trigger body.
var triggerExecFnRE = regexp.MustCompile(`(?i)\bEXECUTE\s+(?:PROCEDURE|FUNCTION)\s+(` + sqlQNameRaw + `)`)

// FROM/JOIN <table>; shared by the view-body and routine-body scans.
var viewBodyFROMRE = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+(` + sqlQNameRaw + `)`)

var bodyInsertIntoRE = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+(` + sqlQNameRaw + `)`)

var bodyUpdateRE = regexp.MustCompile(`(?i)\bUPDATE\s+(` + sqlQNameRaw + `)\s+SET\b`)

var bodyDeleteFromRE = regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+(` + sqlQNameRaw + `)`)

var bodyMergeIntoRE = regexp.MustCompile(`(?i)\bMERGE\s+INTO\s+(` + sqlQNameRaw + `)`)

// EXEC[UTE] <name> or CALL <name>( in a routine body.
var bodyExecCallRE = regexp.MustCompile(`(?i)\b(?:EXEC(?:UTE)?\s+|CALL\s+)(` + sqlQNameRaw + `)\s*[\s(]`)

// CROSS/OUTER APPLY <tvf>( — the trailing paren restricts this to table-valued
// function calls; APPLY (SELECT …) cannot match since sqlQNameRaw needs an ident.
var bodyApplyRE = regexp.MustCompile(`(?i)\b(?:CROSS|OUTER)\s+APPLY\s+(` + sqlQNameRaw + `)\s*\(`)

// FLATTEN([INPUT =>] <expr>). Unanchored so both LATERAL FLATTEN(…) and
// TABLE(FLATTEN(…)) match. A dotted <expr> is a column alias, indistinguishable
// from schema.table, so scanBodyEdges skips it rather than emit a false edge.
var bodyFlattenRE = regexp.MustCompile(`(?i)\bFLATTEN\s*\(\s*(?:INPUT\s*=>\s*)?(` + sqlQNameRaw + `)\s*\)`)

// --- Column-level lineage: alias→table map + qualified column refs ---
//
// FROM/JOIN clauses build an alias→table map; single-dot "alias.col" refs then
// resolve to a specific column node. Bare identifiers stay ambiguous and are
// skipped; refs with two or more dots already resolve via byQualifiedName.

// FROM/JOIN <table> [AS] <alias>. A clause keyword (ON/WHERE/…) after the table
// also lands in the alias group; buildAliasMap drops it via aliasBoundaryKeywords.
var bodyFromAliasRE = regexp.MustCompile(
	`(?i)\b(?:FROM|JOIN)\s+(` + sqlQNameRaw + `)` +
		`(?:\s+(?:AS\s+)?(` + sqlIdentOnlyRaw + `))?`)

// Single-dot "prefix.column"; dotted schema prefixes carry 2+ dots and are handled
// elsewhere. Non-SQL "obj.method" is structurally identical, so only the SQL
// extraction paths may call this.
var bodyQualColRefRE = regexp.MustCompile(
	`\b([A-Za-z_][A-Za-z0-9_$]*)\.([A-Za-z_][A-Za-z0-9_$]*)`)

// Tokens that can follow a table name in FROM/JOIN but are not aliases. Kept
// separate from sqlKeywords so the shared column-name set stays unpolluted.
var aliasBoundaryKeywords = map[string]bool{
	"WHERE": true, "GROUP": true, "ORDER": true, "HAVING": true,
	"UNION": true, "INTERSECT": true, "EXCEPT": true,
	"JOIN": true, "INNER": true, "LEFT": true, "RIGHT": true,
	"OUTER": true, "CROSS": true, "FULL": true,
	"ON": true, "SET": true, "OPTION": true, "GO": true,
	"PIVOT": true, "UNPIVOT": true,
	// Duplicated from sqlKeywords so this set stands alone.
	"SELECT": true, "FROM": true, "INTO": true, "AS": true,
	"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true,
}

// buildAliasMap returns lowercase-alias → table-as-written. CTE names are excluded
// (computed relations, not base tables); an unaliased table maps to itself.
func buildAliasMap(body string, cteShadow map[string]bool) map[string]string {
	aliasMap := map[string]string{}
	for _, m := range bodyFromAliasRE.FindAllStringSubmatch(body, -1) {
		rawTable := m[1]
		rawAlias := ""
		if len(m) > 2 {
			rawAlias = strings.TrimSpace(m[2])
		}
		_, tableName := parseQName(rawTable)
		if tableName == "" || cteShadow[strings.ToLower(tableName)] {
			continue
		}
		if rawAlias != "" && !aliasBoundaryKeywords[strings.ToUpper(rawAlias)] {
			aliasMap[strings.ToLower(rawAlias)] = rawTable
		} else {
			// Unaliased: map the bare table name so "tbl.col" still resolves.
			aliasMap[strings.ToLower(tableName)] = rawTable
		}
	}
	return aliasMap
}

// emitQualifiedColumnRefs resolves single-dot "prefix.col" refs through aliasMap
// and calls addColRef per column. CTE aliases produce no column edges.
func emitQualifiedColumnRefs(
	body string,
	aliasMap map[string]string,
	cteShadow map[string]bool,
	addColRef func(name string, matchOff int),
) {
	// Local dedup: scanBodyEdges' seen map is not passed in, so this is not redundant.
	seen := map[string]bool{}
	for _, m := range bodyQualColRefRE.FindAllStringSubmatchIndex(body, -1) {
		prefix := body[m[2]:m[3]]
		col := body[m[4]:m[5]]
		lowerPrefix := strings.ToLower(prefix)
		if cteShadow[lowerPrefix] {
			continue
		}
		tableAsWritten, ok := aliasMap[lowerPrefix]
		if !ok {
			continue
		}
		// Must match the column node's QualifiedName: "<tableQName>.<col>".
		refName := tableAsWritten + "." + col
		if seen[strings.ToLower(refName)] {
			continue
		}
		seen[strings.ToLower(refName)] = true
		addColRef(refName, m[2])
	}
}

// --- T-SQL temp tables and table variables ---
//
// sqlQNameRaw starts with [A-Za-z_], so ##global, #local and @tablevar need their
// own pattern. It is deliberately broad; scanBodyEdges emits edges only for names
// it has seen declared.
const sqlTempTokenRaw = `##[A-Za-z0-9_$#]+|#[A-Za-z0-9_$#]+|@[A-Za-z0-9_$]+`

// CREATE TABLE #x / ##x in a routine body.
var bodyTempCreateRE = regexp.MustCompile(`(?i)\bCREATE\s+TABLE\s+(` + sqlTempTokenRaw + `)`)

// SELECT … INTO #x; the [^;]* bridge stops the match at a statement boundary.
var bodyTempSelectIntoRE = regexp.MustCompile(`(?i)\bSELECT\b[^;]*?\bINTO\s+(` + sqlTempTokenRaw + `)`)

// DECLARE @x TABLE( — a scalar DECLARE @x INT lacks TABLE and cannot match.
var bodyDeclareTableVarRE = regexp.MustCompile(`(?i)\bDECLARE\s+(@[A-Za-z0-9_$]+)\s+TABLE\s*\(`)

var bodyTempFROMRE = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+(` + sqlTempTokenRaw + `)`)

var bodyTempInsertRE = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+(` + sqlTempTokenRaw + `)`)

var bodyTempUpdateRE = regexp.MustCompile(`(?i)\bUPDATE\s+(` + sqlTempTokenRaw + `)\s+SET\b`)

var bodyTempDeleteRE = regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+(` + sqlTempTokenRaw + `)`)

var bodyTempMergeRE = regexp.MustCompile(`(?i)\bMERGE\s+INTO\s+(` + sqlTempTokenRaw + `)`)

// OUTPUT <gap> INTO <target>: T-SQL routes change-capture rows into a second
// target, a write owned by the enclosing routine. The gap is a lazy
// semicolon-barrier, and outputIntoBoundaryRE rejects a gap holding a DML keyword
// — that means two statements, not one clause, even where semicolons are absent.
var bodyOutputIntoRE = regexp.MustCompile(
	`(?i)\bOUTPUT\b([^;]*?)\bINTO\s+(` + sqlTempTokenRaw + `|` + sqlQNameRaw + `)`,
)

var outputIntoBoundaryRE = regexp.MustCompile(
	`(?i)\b(FROM|WHERE|VALUES|SELECT|INSERT|UPDATE|DELETE|MERGE|EXEC|EXECUTE)\b`,
)

// Snowflake stage tokens, including the internal-stage sigils @~/path (user) and
// @%name (table). The '@' stays in the match so callers can tell a stage from a
// table; parseStageToken strips it and returns empty for the @~ / @% forms.
const stageTokenPat = `@(?:[~%][A-Za-z0-9_$./]*|` + sqlQNameRaw + `(?:/[^\s]*)?)`

// COPY INTO <target> FROM <source>; a leading '@' decides which side is the stage.
var bodyCopyIntoRE = regexp.MustCompile(`(?i)\bCOPY\s+INTO\s+(` + stageTokenPat + `|` + sqlQNameRaw + `)\s+FROM\s+(` + stageTokenPat + `|` + sqlQNameRaw + `)`)

// CLONE <src> appended to CREATE TABLE/VIEW. Callers must scan only the preamble
// before the first '(' — a column named CLONE would otherwise mint a false edge.
var cloneRE = regexp.MustCompile(`(?i)\bCLONE\s+(` + sqlQNameRaw + `)`)

// CTE names in WITH <name> AS (…), collected so they are excluded from edges.
var cteNameRE = regexp.MustCompile(`(?i)(?:\bWITH\b|,)\s+(` + sqlIdentOnlyRaw + `)\s+AS\s*\(`)

// USING (…) / WITH CHECK (…) in a policy; scopes fn-call capture to those blocks.
var usingWithCheckRE = regexp.MustCompile(`(?i)\b(?:USING|WITH\s+CHECK)\s*\(([^)]*)\)`)

// Inline REFERENCES <table> FK in a column definition.
var inlineRefRE = regexp.MustCompile(`(?i)\bREFERENCES\s+(` + sqlQNameRaw + `)`)

// Table-level FOREIGN KEY (…) REFERENCES tgt (…): local columns, target qname,
// optional target columns. Drives column-level FK edges on top of the table→table
// edge inlineRefRE already emits.
var tableLevelFKColRE = regexp.MustCompile(`(?i)FOREIGN\s+KEY\s*\(([^)]*)\)\s+REFERENCES\s+(` + sqlQNameRaw + `)(?:\s*\(([^)]*)\))?`)

// Inline column FK: `col TYPE REFERENCES tgt (x)`. Applied per column-definition
// line, mirroring extractColumns, so the clause attributes to that column.
var inlineColFKColListRE = regexp.MustCompile(`(?i)\bREFERENCES\s+(` + sqlQNameRaw + `)(?:\s*\(([^)]*)\))?`)

// Common ALTER TABLE prefix, absorbing Postgres' optional ONLY keyword. A table
// bare-named "only" would be mis-consumed, but Postgres forbids the unquoted
// reserved word here; quoted forms go through sqlQNameRaw and are unaffected.
const alterTablePat = `(?:ONLY\s+)?` + modPat

// ALTER TABLE <t> ADD [CONSTRAINT c] FOREIGN KEY (…) REFERENCES <target>
var alterFKRefRE = regexp.MustCompile(`(?im)^[ \t]*ALTER\s+TABLE\s+` + alterTablePat + `(` + sqlQNameRaw + `)\s+ADD\s+(?:CONSTRAINT\s+` + sqlIdentOnlyRaw + `\s+)?FOREIGN\s+KEY\s*\([^)]*\)\s+REFERENCES\s+(` + sqlQNameRaw + `)`)

// Function calls fn(…) inside a policy USING / WITH CHECK expression.
var fnCallInExprRE = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_$]*)\s*\(`)

// CREATE TYPE <name> AS ENUM|TABLE, or CREATE TYPE <name> FROM <base>.
var typeRE = regexp.MustCompile(
	`(?im)^[ \t]*CREATE\s+` + modPat + `TYPE\s+` + modPat + `(` + sqlQNameRaw + `)` +
		`\s+(?:AS\s+(ENUM|TABLE)|FROM\s+(` + sqlQNameRaw + `))`,
)

// Composite CREATE TYPE <name> with no trailing AS/FROM — fallback for typeRE.
var typeCompositeRE = regexp.MustCompile(`(?im)^[ \t]*CREATE\s+` + modPat + `TYPE\s+` + modPat + `(` + sqlQNameRaw + `)\s*$`)

// ALTER TABLE <t> ADD [COLUMN] [IF NOT EXISTS] <col>. Without the IF NOT EXISTS
// branch, an idempotent migration mints a column literally named "IF".
var alterAddColumnRE = regexp.MustCompile(`(?im)^[ \t]*ALTER\s+TABLE\s+` + alterTablePat + `(` + sqlQNameRaw + `)\s+ADD\s+(?:COLUMN\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(` + sqlIdentOnlyRaw + `)`)

// Tokens that follow a bare ADD without naming a column. RE2 has no lookahead,
// so they are rejected after the match rather than excluded in the pattern —
// otherwise `ADD CONSTRAINT ck_foo CHECK (…)` mints a column named "CONSTRAINT".
var alterAddNonColumnKeywords = map[string]bool{
	"constraint": true,
	"primary":    true,
	"foreign":    true,
	"unique":     true,
	"check":      true,
	"exclude":    true,
	"if":         true,
}

// ALTER TABLE <t> ADD CONSTRAINT <name> <type> …
var alterAddConstraintRE = regexp.MustCompile(`(?im)^[ \t]*ALTER\s+TABLE\s+` + alterTablePat + `(` + sqlQNameRaw + `)\s+ADD\s+CONSTRAINT\s+(` + sqlIdentOnlyRaw + `)\s+(PRIMARY\s+KEY|FOREIGN\s+KEY|UNIQUE|CHECK)`)

// ALTER TABLE <t> ADD PRIMARY KEY|FOREIGN KEY|UNIQUE|CHECK, unnamed constraint.
var alterAddAnonConstraintRE = regexp.MustCompile(`(?im)^[ \t]*ALTER\s+TABLE\s+` + alterTablePat + `(` + sqlQNameRaw + `)\s+ADD\s+(PRIMARY\s+KEY|FOREIGN\s+KEY|UNIQUE|CHECK)\b`)

// Qualified SQL name with no capture groups, so it nests inside regexes where
// subgroup numbering matters. The bare alternative includes '.', so a bare
// schema.name matches as one token and the trailing quoted-component branches are
// reached only for mixed forms like schema."quoted"; parseQName re-splits anyway.
const sqlQNameRaw = `(?:"[^"]+"|` + "`[^`]+`" + `|\[[^\]]+\]|[A-Za-z_][A-Za-z0-9_$.]*)` +
	`(?:\.(?:"[^"]+"|` + "`[^`]+`" + `|\[[^\]]+\]|[A-Za-z_][A-Za-z0-9_$.]*))*`

// sqlIdentOnlyRaw is just a single unqualified identifier (no dots).
const sqlIdentOnlyRaw = `(?:"[^"]+"|` + "`[^`]+`" + `|\[[^\]]+\]|[A-Za-z_][A-Za-z0-9_$]*)`

// --- Enum label extraction ---

// The parenthesised label list of CREATE TYPE … AS ENUM (…).
var enumValuesRE = regexp.MustCompile(`(?si)AS\s+ENUM\s*\(([^)]*)\)`)

var singleQuotedLabelRE = regexp.MustCompile(`'([^']*)'`)

// --- Column extraction inside CREATE TABLE body ---

// Keywords starting a table-level constraint line rather than a column. A
// constraint routinely wraps across lines, so every token a continuation line can
// start with belongs here too — otherwise `REFERENCES sales_order (…)` parses as
// a column named REFERENCES of type sales_order.
var constraintKeywords = map[string]bool{
	"CONSTRAINT": true,
	"PRIMARY":    true,
	"FOREIGN":    true,
	"UNIQUE":     true,
	"CHECK":      true,
	"INDEX":      true,
	"KEY":        true,
	"REFERENCES": true,
	"ON":         true,
	"EXCLUDE":    true,
	"DEFERRABLE": true,
	"INITIALLY":  true,
	"MATCH":      true,
	"LIKE":       true,
	"INHERITS":   true,
	"PARTITION":  true,
	"WITH":       true,
	"USING":      true,
	"NOT":        true,
}

// Marks a computed/generated column.
var generatedMarkerRE = regexp.MustCompile(`(?i)\bGENERATED\b|\bAS\s*\(`)

// --- dbt Jinja pre-pass ---

// Any of {{ / {% / {# triggers the dbt pre-pass.
var dbtJinjaGateRE = regexp.MustCompile(`\{\{|\{%|\{#`)

// Jinja {# … #} comments, stripped before harvest so refs inside them are skipped.
var dbtJinjaCommentRE = regexp.MustCompile(`(?s)\{#.*?#\}`)

// Every dbt ref() form, including cross-project and versioned refs. The model is
// group 2 when two positional args are given, else group 1; a version in group 3
// targets "<model>_v<N>", dbt's compiled identifier for a versioned model.
var dbtRefRE = regexp.MustCompile(
	`\{\{[-\s]*\bref\s*\(\s*` +
		`'([^']+)'` + // group 1: first literal
		`(?:\s*,\s*'([^']+)')?` + // group 2: second literal (package, model) — optional
		`(?:\s*,\s*v(?:ersion)?\s*=\s*([0-9]+))?` + // group 3: version integer N — optional (E4)
		`\s*\)\s*-?\}\}`,
)

// {{ source('schema', 'table') }} → a references edge to schema.table.
var dbtSourceRE = regexp.MustCompile(
	`\{\{[-\s]*\bsource\s*\(\s*'([^']+)'\s*,\s*'([^']+)'\s*\)\s*-?\}\}`,
)

// {{ ref(…) }} for placeholder substitution; mirrors dbtRefRE's grammar exactly.
// The placeholder is __dbt_ref_<model>, or __dbt_ref_<model>_v<N> when versioned.
var dbtRefSubstRE = regexp.MustCompile(
	`\{\{[-\s]*\bref\s*\(\s*` +
		`'([^']+)'` + // group 1: first literal
		`(?:\s*,\s*'([^']+)')?` + // group 2: second literal — optional
		`(?:\s*,\s*v(?:ersion)?\s*=\s*([0-9]+))?` + // group 3: version integer N — optional (E4)
		`\s*\)\s*-?\}\}`,
)

// {{ source('a','b') }} for placeholder substitution.
var dbtSourceSubstRE = regexp.MustCompile(
	`\{\{[-\s]*\bsource\s*\(\s*'([^']+)'\s*,\s*'([^']+)'\s*\)\s*-?\}\}`,
)

// Jinja blocks left after ref/source substitution, blanked length-preservingly.
var dbtAnyExprRE = regexp.MustCompile(`(?s)\{\{.*?\}\}|\{%.*?%\}`)

// {{ config(… alias='name' …) }} — the alias annotates the model node's Metadata.
// [^)]* on both sides tolerates other kwargs in any order.
var dbtConfigAliasRE = regexp.MustCompile(
	`\{\{[-\s]*\bconfig\s*\([^)]*\balias\s*=\s*['"]([^'"]+)['"][^)]*\)\s*-?\}\}`,
)

// --- dbt macros and project awareness ---

// {% macro <name>(<args>) %} … {% endmacro %} blocks.
var dbtMacroDefRE = regexp.MustCompile(
	`(?s)\{%-?\s*macro\s+([A-Za-z_][A-Za-z0-9_]*)\s*\([^)]*\)\s*-?%\}.*?\{%-?\s*endmacro\s*-?%\}`,
)

// {{ <name>(…) }} invocations; the callee may be "pkg.fn" or bare "fn". The
// caller applies the denylist and the package-qualified skip.
var dbtMacroCallRE = regexp.MustCompile(
	`\{\{[-\s]*([A-Za-z_][A-Za-z0-9_.]*)\s*\(`,
)

// Bare names that are not user-defined macro calls: dbt built-ins and pseudo-
// variables, plus Jinja2 built-ins that are syntactically indistinguishable.
// Package-qualified calls (a.b) are rejected by a separate path check.
var dbtMacroCallDenylist = map[string]bool{
	"ref": true, "source": true, "config": true, "var": true,
	"env_var": true, "is_incremental": true, "should_full_refresh": true,
	"this": true, "target": true, "builtins": true, "adapter": true,
	"exceptions": true, "modules": true, "api": true, "log": true,
	"print": true, "run_query": true, "run_started_at": true, "statement": true,
	"return": true, "set": true, "dbt_version": true, "invocation_id": true,
	"flags": true, "model": true, "graph": true,
	"fromjson": true, "tojson": true, "fromyaml": true, "toyaml": true,
	"zip": true, "range": true, // Jinja2 built-ins, not dbt macros
}

// dbtFileRole classifies a dbt project file by path segment: "/macros/" yields
// macro defs and no model node, "/analyses|tests|seeds|snapshots/" neither, and
// anything else — including unrecognised locations — a model node.
func dbtFileRole(filePath string) string {
	// Slash-delimited so "macros_extra" cannot match "/macros/".
	p := strings.ToLower(filePath)
	for _, c := range []byte(p) {
		if c == '\\' {
			// Test fixtures carry Windows paths.
			p = strings.ReplaceAll(p, "\\", "/")
			break
		}
	}
	// seg arrives slash-wrapped; match mid-path or as the leading segment.
	hasSeg := func(seg string) bool {
		return strings.Contains(p, seg) || strings.HasPrefix(p, seg[1:])
	}
	if hasSeg("/macros/") {
		return "macro"
	}
	for _, seg := range []string{"/analyses/", "/tests/", "/seeds/", "/snapshots/"} {
		if hasSeg(seg) {
			return "other"
		}
	}
	return "model"
}

// macroSpan holds the byte [start, end) of a single {% macro %}…{% endmacro %} block.
type macroSpan struct {
	start int
	end   int
	id    string // node ID of the owning macro node
}

// inMacroSpan returns the span containing offset, or nil. dbt has no nested
// macros, so a linear scan is exact.
func inMacroSpan(spans []macroSpan, offset int) *macroSpan {
	for i := range spans {
		if offset >= spans[i].start && offset < spans[i].end {
			return &spans[i]
		}
	}
	return nil
}

// --- SQLExtractor ---

// SQLExtractor extracts SQL definitions and their lineage edges by regex.
type SQLExtractor struct{}

// NewSQLExtractor returns a SQLExtractor. No parser pool required.
func NewSQLExtractor() *SQLExtractor {
	return &SQLExtractor{}
}

// Extract implements the Extractor interface for SQL files.
func (e *SQLExtractor) Extract(filePath, source string) (types.ExtractionResult, error) {
	var result types.ExtractionResult

	// The dbt Jinja pre-pass must run on raw source: stripStrings blanks the
	// quoted arguments inside {{ ref('x') }}, destroying the harvest.
	var dbtModelID string // non-empty when the pre-pass fired AND a model node was created
	if dbtJinjaGateRE.MatchString(source) {
		role := dbtFileRole(filePath)

		// Strip {# … #} before harvest, and take every span and offset from this
		// same string: blankPreserveNewlines collapses multi-byte runes to one
		// space, so offsets taken from `source` would mis-attribute refs inside a
		// macro body to the model node.
		rawForHarvest := dbtJinjaCommentRE.ReplaceAllStringFunc(source, func(m string) string {
			return blankPreserveNewlines(m)
		})

		// Macro defs are harvested whatever the role; their spans drive ref/call
		// ownership.
		var spans []macroSpan // byte spans of all macro blocks (for E3)

		for _, m := range dbtMacroDefRE.FindAllStringSubmatchIndex(rawForHarvest, -1) {
			macroName := rawForHarvest[m[2]:m[3]]
			macroLine := strings.Count(rawForHarvest[:m[0]], "\n") + 1
			macroID := extraction.GenerateNodeID(filePath, string(types.NodeKindMacro), macroName, macroLine)
			macroNode := types.Node{
				ID:            macroID,
				Kind:          types.NodeKindMacro,
				Name:          macroName,
				QualifiedName: macroName,
				FilePath:      filePath,
				Language:      types.LanguageSQL,
				StartLine:     macroLine,
				EndLine:       macroLine,
				IsExported:    true,
			}
			result.Nodes = append(result.Nodes, macroNode)
			spans = append(spans, macroSpan{start: m[0], end: m[1], id: macroID})
		}

		// One model node per file, named for the basename. The .sql.jinja compound
		// suffix must come off in full or {{ ref('stg') }} resolves to a phantom
		// 'stg.sql'.
		if role == "model" {
			base := filepath.Base(filePath)
			var modelName string
			if strings.HasSuffix(strings.ToLower(base), ".sql.jinja") {
				modelName = base[:len(base)-len(".sql.jinja")]
			} else {
				modelName = strings.TrimSuffix(base, filepath.Ext(base))
			}

			modelLine := 1
			modelQName := modelName
			modelID := extraction.GenerateNodeID(filePath, string(types.NodeKindModel), modelQName, modelLine)
			modelNode := types.Node{
				ID:            modelID,
				Kind:          types.NodeKindModel,
				Name:          modelName,
				QualifiedName: modelQName,
				FilePath:      filePath,
				Language:      types.LanguageSQL,
				StartLine:     modelLine,
				EndLine:       modelLine,
				IsExported:    true,
			}
			// First config(alias=…) wins. json.Marshal escapes the value: the regex
			// excludes quotes but still admits backslashes and control chars.
			if am := dbtConfigAliasRE.FindStringSubmatch(rawForHarvest); am != nil {
				if b, err := json.Marshal(map[string]string{"alias": am[1]}); err == nil {
					modelNode.Metadata = b
				}
			}
			result.Nodes = append(result.Nodes, modelNode)
			dbtModelID = modelID
		}

		// A ref inside a macro span belongs to that macro; otherwise to the model
		// node, which is empty when the file's role produced none.
		ownerForOffset := func(offset int) string {
			if sp := inMacroSpan(spans, offset); sp != nil {
				return sp.id
			}
			return dbtModelID // empty string when role != model
		}

		// ref() edges; an ownerless ref has no node to attach to and is dropped.
		seenRef := map[string]bool{}
		for _, m := range dbtRefRE.FindAllStringSubmatchIndex(rawForHarvest, -1) {
			first := rawForHarvest[m[2]:m[3]]
			var modelRef string
			if m[4] >= 0 {
				// Two literals means (package, model).
				modelRef = rawForHarvest[m[4]:m[5]]
			} else {
				modelRef = first
			}
			if m[6] >= 0 {
				modelRef = modelRef + "_v" + rawForHarvest[m[6]:m[7]]
			}
			if modelRef == "" || seenRef[modelRef] {
				continue
			}
			owner := ownerForOffset(m[0])
			if owner == "" {
				continue // no owning node; drop
			}
			seenRef[modelRef] = true
			result.UnresolvedReferences = append(result.UnresolvedReferences,
				sqlRef(filePath, owner, modelRef, types.EdgeKindReferences, 1))
		}

		// source() edges.
		seenSrc := map[string]bool{}
		for _, m := range dbtSourceRE.FindAllStringSubmatchIndex(rawForHarvest, -1) {
			schema := rawForHarvest[m[2]:m[3]]
			table := rawForHarvest[m[4]:m[5]]
			synthetic := schema + "." + table
			if seenSrc[synthetic] {
				continue
			}
			owner := ownerForOffset(m[0])
			if owner == "" {
				continue // no owning node; drop
			}
			seenSrc[synthetic] = true
			result.UnresolvedReferences = append(result.UnresolvedReferences,
				sqlRef(filePath, owner, synthetic, types.EdgeKindReferences, 1))
		}

		// Macro call edges. A dotted callee names another package and a denylisted
		// bare name is a built-in; both are skipped. Dedup is per owner:callee, since
		// a model and a macro may each call the same helper.
		seenCall := map[string]bool{}
		for _, m := range dbtMacroCallRE.FindAllStringSubmatchIndex(rawForHarvest, -1) {
			callee := rawForHarvest[m[2]:m[3]]
			if strings.Contains(callee, ".") {
				continue
			}
			if dbtMacroCallDenylist[callee] {
				continue
			}
			owner := ownerForOffset(m[0])
			if owner == "" {
				continue // no owning node; drop
			}
			dedupKey := owner + ":" + callee
			if seenCall[dedupKey] {
				continue
			}
			seenCall[dedupKey] = true
			result.UnresolvedReferences = append(result.UnresolvedReferences,
				sqlRef(filePath, owner, callee, types.EdgeKindCalls, 1))
		}

		// Feed a placeholder-substituted residual to the normal pipeline. Skipped
		// without a model node: nothing would own the edges, and such files are
		// mostly Jinja anyway.
		if dbtModelID != "" {
			// {{ ref('x') }} → __dbt_ref_<model>. Starts from rawForHarvest, not
			// source: a {# … #} comment holding the words "from"/"join" would
			// otherwise leak into scanBodyEdges as false edges. ReplaceAllStringFunc
			// only fires on a match, so FindStringSubmatch below cannot be nil.
			residual := dbtRefSubstRE.ReplaceAllStringFunc(rawForHarvest, func(m string) string {
				sub := dbtRefSubstRE.FindStringSubmatch(m)
				modelRef := sub[1]
				if sub[2] != "" {
					modelRef = sub[2]
				}
				if sub[3] != "" {
					modelRef = modelRef + "_v" + sub[3]
				}
				return padTo("__dbt_ref_"+modelRef, len(m))
			})

			// {{ source('a','b') }} → __dbt_src_a__b
			residual = dbtSourceSubstRE.ReplaceAllStringFunc(residual, func(m string) string {
				sub := dbtSourceSubstRE.FindStringSubmatch(m)
				return padTo("__dbt_src_"+sub[1]+"__"+sub[2], len(m))
			})

			// Blank the remaining Jinja length-preservingly.
			residual = dbtAnyExprRE.ReplaceAllStringFunc(residual, func(m string) string {
				return blankPreserveNewlines(m)
			})

			// Scan the residual so real FROM/JOIN table names attach to the model node.
			residualStripped := stripStrings(stripComments(residual))
			ctes := extractCTENames(residualStripped)
			// dbt bodies hold no T-SQL temp tables, hence the empty routine name.
			_, bodyEdges := scanBodyEdges(filePath, dbtModelID, residualStripped, 0, residualStripped, ctes, "", nil)

			// Drop our own placeholders — the harvest already owns those edges.
			for _, ref := range bodyEdges {
				if strings.HasPrefix(ref.ReferenceName, "__dbt_ref_") || strings.HasPrefix(ref.ReferenceName, "__dbt_src_") {
					continue
				}
				result.UnresolvedReferences = append(result.UnresolvedReferences, ref)
			}

			// The definition scan below reads the residual, not the Jinja source.
			source = residual
		}
	}
	_ = dbtModelID // may be empty for non-Jinja files or macro/other roles

	// Strip comments and string literals before matching to avoid false positives.
	stripped := stripComments(source)
	strippedNoStr := stripStrings(stripped)

	// Offsets are into stripped, which shares source's newline positions.
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

		// stripped keeps structure; the FK scan uses strippedNoStr so a REFERENCES
		// inside a string literal cannot mint an edge.
		tableBody, tableBodyOff := findParenBlock(stripped, m[1])
		tableBodyNoStr, _ := findParenBlock(strippedNoStr, m[1])

		colNodes, colEdges := extractColumns(filePath, source, stripped, tableID, name, qname, tableBody, tableBodyOff)
		result.Nodes = append(result.Nodes, colNodes...)
		result.Edges = append(result.Edges, colEdges...)

		anonCtrs := map[string]int{}
		conNodes, conEdges := extractConstraints(filePath, stripped, tableBody, tableBodyOff, tableID, name, anonCtrs)
		result.Nodes = append(result.Nodes, conNodes...)
		result.Edges = append(result.Edges, conEdges...)

		// FK → references, covering both inline column FKs and table-level FOREIGN KEY.
		if tableBodyNoStr != "" {
			seenFKTargets := map[string]bool{}
			for _, rm := range inlineRefRE.FindAllStringSubmatchIndex(tableBodyNoStr, -1) {
				rawTgt := tableBodyNoStr[rm[2]:rm[3]]
				_, tgtName := parseQName(rawTgt)
				if tgtName == "" || isSQLRefKeyword(tgtName) || seenFKTargets[strings.ToLower(tgtName)] {
					continue
				}
				seenFKTargets[strings.ToLower(tgtName)] = true
				// Approximate: the line of the CREATE TABLE match.
				line := strings.Count(stripped[:m[1]], "\n") + 1
				result.UnresolvedReferences = append(result.UnresolvedReferences,
					sqlRef(filePath, tableID, tgtName, types.EdgeKindReferences, line))
			}

			// Column-level FK refs, from the local column node to the target column
			// (or to the bare table when no column list is given). Additive to the
			// table→table edge above — never deduped against it.
			if len(colNodes) > 0 {
				colIDByLowerName := make(map[string]string, len(colNodes))
				for _, cn := range colNodes {
					colIDByLowerName[strings.ToLower(cn.Name)] = cn.ID
				}
				result.UnresolvedReferences = append(result.UnresolvedReferences,
					extractColumnFKRefs(filePath, stripped, tableBodyNoStr, tableBodyOff, colIDByLowerName)...)
			}
		}

		// CLONE <src>. Scan only the preamble before the first '(': a real CLONE has
		// no column list, so a column named CLONE cannot be mistaken for one.
		stmtText := extractStmtText(strippedNoStr, m[1])
		preamble := stmtText
		if parenIdx := strings.IndexByte(stmtText, '('); parenIdx >= 0 {
			preamble = stmtText[:parenIdx]
		}
		if cm := cloneRE.FindStringSubmatchIndex(preamble); cm != nil {
			rawSrc := preamble[cm[2]:cm[3]]
			_, srcName := parseQName(rawSrc)
			if srcName != "" && !isSQLRefKeyword(srcName) {
				byteOff := m[1] + cm[2]
				line := strings.Count(stripped[:byteOff], "\n") + 1
				result.UnresolvedReferences = append(result.UnresolvedReferences,
					sqlRef(filePath, tableID, srcName, types.EdgeKindReferences, line))
			}
		}
	}

	// Every CREATE TABLE node exists by now and the ALTER loops add none, so the
	// lookup map can be built once.
	tableNodeIDMap := buildTableNodeIDMap(result.Nodes)

	// ALTER TABLE … FOREIGN KEY … REFERENCES <target> → references.
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
		if tableName == "" || colName == "" || alterAddNonColumnKeywords[strings.ToLower(colName)] {
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
			Metadata:      buildConstraintMeta(ctype, "", localConstraintColumns(statementAt(strippedNoStr, m[0]), ctype)),
		}
		result.Nodes = append(result.Nodes, node)
		if tableNodeID != "" {
			result.Edges = append(result.Edges, containsEdge(tableNodeID, id))
		}
	}

	// -- ALTER TABLE ADD <anonymous constraint> --
	// The regex puts the type keyword directly after ADD, so a named-constraint
	// line structurally cannot match and needs no runtime guard. Counting per table
	// gives the synthesized names a stable suffix.
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
			Metadata:      buildConstraintMeta(ctype, "", localConstraintColumns(statementAt(strippedNoStr, m[0]), ctype)),
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

		// view → source tables: FROM/JOIN in the body after AS.
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

		// FLATTEN(<expr>) in a view body; only unqualified identifiers become edges.
		for _, bm := range bodyFlattenRE.FindAllStringSubmatchIndex(viewBody, -1) {
			rawExpr := viewBody[bm[2]:bm[3]]
			if strings.ContainsRune(rawExpr, '.') {
				continue // dotted — column expr, skip
			}
			_, tgtName := parseQName(rawExpr)
			if tgtName == "" || isSQLRefKeyword(tgtName) || seen[strings.ToLower(tgtName)] {
				continue
			}
			seen[strings.ToLower(tgtName)] = true
			byteOff := viewBodyStart + bm[2]
			line := strings.Count(stripped[:byteOff], "\n") + 1
			result.UnresolvedReferences = append(result.UnresolvedReferences,
				sqlRef(filePath, node.ID, tgtName, types.EdgeKindReferences, line))
		}

		// Qualified "alias.col" refs from the view body; CTE names are excluded.
		viewCTEs := extractCTENames(viewBody)
		viewAliasMap := buildAliasMap(viewBody, viewCTEs)
		emitQualifiedColumnRefs(viewBody, viewAliasMap, viewCTEs, func(refName string, matchOff int) {
			byteOff := viewBodyStart + matchOff
			if byteOff > len(stripped) {
				byteOff = len(stripped)
			}
			line := strings.Count(stripped[:byteOff], "\n") + 1
			result.UnresolvedReferences = append(result.UnresolvedReferences,
				sqlRef(filePath, node.ID, refName, types.EdgeKindReferences, line))
		})

		// CLONE <src>, same preamble guard as the table path. A cloned view has no
		// AS SELECT body, so CLONE is its only lineage signal.
		viewStmtText := extractStmtText(strippedNoStr, m[1])
		viewPreamble := viewStmtText
		if parenIdx := strings.IndexByte(viewStmtText, '('); parenIdx >= 0 {
			viewPreamble = viewStmtText[:parenIdx]
		}
		if cm := cloneRE.FindStringSubmatchIndex(viewPreamble); cm != nil {
			rawSrc := viewPreamble[cm[2]:cm[3]]
			_, srcName := parseQName(rawSrc)
			if srcName != "" && !isSQLRefKeyword(srcName) {
				byteOff := m[1] + cm[2]
				line := strings.Count(stripped[:byteOff], "\n") + 1
				result.UnresolvedReferences = append(result.UnresolvedReferences,
					sqlRef(filePath, node.ID, srcName, types.EdgeKindReferences, line))
			}
		}
	}

	// Shared across every routine scan so a ##global temp declared in one routine
	// is visible to another and lands as a single node.
	globalTempNodes := map[string]*types.Node{}

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

		// The routine name scopes the temp tables found in its body.
		body, bodyOff := extractRoutineBody(strippedNoStr, m[1])
		if body != "" {
			ctes := extractCTENames(body)
			tempNodes, bodyEdgeRefs := scanBodyEdges(filePath, fnNode.ID, body, bodyOff, stripped, ctes, name, globalTempNodes)
			result.Nodes = append(result.Nodes, tempNodes...)
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

		// The routine name scopes the temp tables found in its body.
		body, bodyOff := extractRoutineBody(strippedNoStr, m[1])
		if body != "" {
			ctes := extractCTENames(body)
			tempNodes, bodyEdgeRefs := scanBodyEdges(filePath, procNode.ID, body, bodyOff, stripped, ctes, name, globalTempNodes)
			result.Nodes = append(result.Nodes, tempNodes...)
			result.UnresolvedReferences = append(result.UnresolvedReferences, bodyEdgeRefs...)
		}
	}

	// File-scoped ##global temp nodes, already deduped by the scans above.
	for _, n := range globalTempNodes {
		result.Nodes = append(result.Nodes, *n)
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

		// Scan from the end of the trigger name to the statement boundary.
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

	// -- Stages --
	for _, m := range stageRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		result.Nodes = append(result.Nodes, nodeAt(types.NodeKindStage, schema, name, qname, m[0]))
	}

	// -- File formats --
	for _, m := range fileFormatRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		result.Nodes = append(result.Nodes, nodeAt(types.NodeKindFileFormat, schema, name, qname, m[0]))
	}

	// -- Streams --
	for _, m := range streamRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		rawSrc := strippedNoStr[m[4]:m[5]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		streamNode := nodeAt(types.NodeKindStream, schema, name, qname, m[0])
		result.Nodes = append(result.Nodes, streamNode)

		_, srcName := parseQName(rawSrc)
		if srcName != "" && !isSQLRefKeyword(srcName) {
			line := strings.Count(stripped[:m[4]], "\n") + 1
			result.UnresolvedReferences = append(result.UnresolvedReferences,
				sqlRef(filePath, streamNode.ID, srcName, types.EdgeKindReferences, line))
		}
	}

	// -- Tasks --
	// Two edge sources: the AFTER predecessor list, which needs its own regex
	// because scanBodyEdges denylists the AFTER keyword, and the AS <sql> body.
	for _, m := range taskRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		taskNode := nodeAt(types.NodeKindTask, schema, name, qname, m[0])
		result.Nodes = append(result.Nodes, taskNode)

		stmtText := extractStmtText(strippedNoStr, m[1])

		// AFTER predecessor edges.
		if am := taskAfterRE.FindStringSubmatchIndex(stmtText); am != nil {
			rawList := stmtText[am[2]:am[3]]
			predecessors := taskPredecessorSplitRE.Split(rawList, -1)
			for _, pred := range predecessors {
				pred = strings.TrimSpace(pred)
				if pred == "" {
					continue
				}
				_, predName := parseQName(pred)
				if predName == "" {
					continue
				}
				byteOff := m[1] + am[2]
				line := strings.Count(stripped[:byteOff], "\n") + 1
				result.UnresolvedReferences = append(result.UnresolvedReferences,
					sqlRef(filePath, taskNode.ID, predName, types.EdgeKindReferences, line))
			}
		}

		// Task body (AS <sql>).
		body, bodyOff := extractRoutineBody(strippedNoStr, m[1])
		if body != "" {
			ctes := extractCTENames(body)
			// Tasks hold no T-SQL temp declarations, hence the empty routine name.
			_, bodyEdgeRefs := scanBodyEdges(filePath, taskNode.ID, body, bodyOff, stripped, ctes, "", nil)
			result.UnresolvedReferences = append(result.UnresolvedReferences, bodyEdgeRefs...)
		}
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
	// typeRE first; the composite fallback below dedups against seenTypeNames.
	seenTypeNames := map[string]bool{}
	for _, m := range typeRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
		rawName := strippedNoStr[m[2]:m[3]]
		schema, name := parseQName(rawName)
		qname := qualifiedName(schema, name)
		if name == "" {
			continue
		}
		seenTypeNames[strings.ToLower(qname)] = true

		isEnum := m[4] >= 0 && strings.EqualFold(strippedNoStr[m[4]:m[5]], "ENUM")
		isTable := m[4] >= 0 && strings.EqualFold(strippedNoStr[m[4]:m[5]], "TABLE")
		isFrom := m[6] >= 0

		if isEnum {
			enumNode := nodeAt(types.NodeKindEnum, schema, name, qname, m[0])
			result.Nodes = append(result.Nodes, enumNode)
			// Labels come from the un-stripped source — stripStrings blanks them.
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

		// synonym → target references.
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

		// Function calls only from USING / WITH CHECK blocks: scanning the whole
		// statement picks up AS PERMISSIVE, TO public and other never-resolving noise.
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

	// A COPY INTO outside every routine body needs an owner: a script node named
	// for the file, created lazily on the first such statement. dbt model files are
	// skipped — their model node already owns top-level statements.
	if dbtModelID == "" {
		// Body spans of every routine and task, to exclude COPYs already owned.
		type bodySpan struct{ start, end int }
		var bodySpans []bodySpan
		for _, re := range []*regexp.Regexp{functionRE, procedureRE, taskRE} {
			for _, m := range re.FindAllStringSubmatchIndex(strippedNoStr, -1) {
				body, bodyOff := extractRoutineBody(strippedNoStr, m[1])
				if body != "" {
					bodySpans = append(bodySpans, bodySpan{start: bodyOff, end: bodyOff + len(body)})
				}
			}
		}

		inBodySpan := func(off int) bool {
			for _, sp := range bodySpans {
				if off >= sp.start && off < sp.end {
					return true
				}
			}
			return false
		}

		// Hold the ID as a string, not a pointer into result.Nodes: a later append
		// reallocates the backing array and would leave the pointer stale.
		scriptID := ""
		for _, m := range bodyCopyIntoRE.FindAllStringSubmatchIndex(strippedNoStr, -1) {
			if inBodySpan(m[0]) {
				continue // already owned by the enclosing routine/task (v1)
			}

			if scriptID == "" {
				base := filepath.Base(filePath)
				// A .sql.jinja file never reaches here — the dbt pre-pass owns it.
				scriptName := strings.TrimSuffix(base, filepath.Ext(base))
				scriptLine := 1
				scriptID = extraction.GenerateNodeID(filePath, string(types.NodeKindScript), scriptName, scriptLine)
				result.Nodes = append(result.Nodes, types.Node{
					ID:            scriptID,
					Kind:          types.NodeKindScript,
					Name:          scriptName,
					QualifiedName: scriptName,
					FilePath:      filePath,
					Language:      types.LanguageSQL,
					StartLine:     scriptLine,
					EndLine:       scriptLine,
					IsExported:    true,
				})
			}

			// COPY lineage edges owned by the script node.
			rawTarget := strippedNoStr[m[2]:m[3]]
			rawSource := strippedNoStr[m[4]:m[5]]
			line := strings.Count(stripped[:m[0]], "\n") + 1

			if strings.HasPrefix(rawTarget, "@") {
				// COPY INTO @stage FROM <tbl>
				stageName := parseStageToken(rawTarget)
				_, tblName := parseQName(rawSource)
				if stageName != "" {
					result.UnresolvedReferences = append(result.UnresolvedReferences,
						sqlRef(filePath, scriptID, stageName, types.EdgeKindWrites, line))
				}
				if tblName != "" && !isSQLRefKeyword(tblName) {
					result.UnresolvedReferences = append(result.UnresolvedReferences,
						sqlRef(filePath, scriptID, tblName, types.EdgeKindReferences, line))
				}
			} else {
				// COPY INTO <tbl> FROM @stage[/path]
				_, tblName := parseQName(rawTarget)
				stageName := parseStageToken(rawSource)
				if tblName != "" && !isSQLRefKeyword(tblName) {
					result.UnresolvedReferences = append(result.UnresolvedReferences,
						sqlRef(filePath, scriptID, tblName, types.EdgeKindWrites, line))
				}
				if stageName != "" {
					result.UnresolvedReferences = append(result.UnresolvedReferences,
						sqlRef(filePath, scriptID, stageName, types.EdgeKindReferences, line))
				}
			}
		}
	}

	return result, nil
}

// --- Column extraction helper ---

// extractColumns emits column nodes and contains edges from a CREATE TABLE body,
// skipping constraint lines. Structure is parsed from the comment-blanked body so
// a string-literal default cannot look like a column; the GENERATED check re-reads
// the same line from the original source, where those keywords survive.
func extractColumns(
	filePath, source, stripped string,
	tableID, tableName, tableQName string,
	body string, bodyOff int,
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
		if trimmed == "" {
			continue
		}

		firstWord := strings.ToUpper(strings.Fields(trimmed)[0])
		if constraintKeywords[firstWord] {
			continue
		}

		colName := extractFirstIdent(trimmed)
		if colName == "" {
			continue
		}
		if isSQLKeyword(colName) {
			continue
		}

		lineNum := lineOffset + i + 1
		colQName := fmt.Sprintf("%s.%s", tableQName, colName)
		colID := extraction.GenerateNodeID(filePath, string(types.NodeKindColumn), colQName, lineNum)

		// Declared type: the identifier after the column name. extractFirstIdent
		// stops at '(', so NUMBER(38,0) yields NUMBER. The two guards drop the
		// `col DEFAULT 0` and `col NOT NULL` shapes, where the second token is a
		// modifier rather than a type.
		colTypeToken := ""
		nameLen := extractFirstIdentLen(trimmed)
		if nameLen > 0 && nameLen < len(trimmed) {
			rest := strings.TrimSpace(trimmed[nameLen:])
			if typeIdent := extractFirstIdent(rest); typeIdent != "" &&
				!isSQLKeyword(typeIdent) &&
				!colAttributeKeywords[strings.ToUpper(typeIdent)] {
				colTypeToken = strings.ToUpper(typeIdent)
			}
		}

		// The original line, so a GENERATED marker inside a stripped span is seen.
		origLine := getSourceLine(source, lineOffset+i)
		isGenerated := generatedMarkerRE.MatchString(origLine)

		var colMeta json.RawMessage
		if colTypeToken != "" || isGenerated {
			meta := map[string]interface{}{}
			if colTypeToken != "" {
				meta["type"] = colTypeToken
			}
			if isGenerated {
				meta["generated"] = true
			}
			colMeta, _ = json.Marshal(meta)
		}

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
			Metadata:      colMeta,
		}

		nodes = append(nodes, colNode)
		edges = append(edges, containsEdge(tableID, colID))
	}
	return
}

// --- FK column-level reference extraction ---

// splitIdentList normalizes a comma-separated column list from a FOREIGN KEY or
// REFERENCES clause.
func splitIdentList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		id := normIdent(strings.TrimSpace(p))
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

// columnFKRefs pairs local and target columns positionally, emitting an edge from
// each local column node to "tgt.col". Mismatched lists pair to the shorter one —
// malformed DDL must not error the file. With no target list the reference is to
// the bare table, the implicit-PK form.
func columnFKRefs(
	filePath, tgtName string,
	localCols, tgtCols []string,
	colIDByLowerName map[string]string,
	line int,
) (refs []types.UnresolvedReference) {

	if len(tgtCols) == 0 {
		for _, lc := range localCols {
			colID := colIDByLowerName[strings.ToLower(lc)]
			if colID == "" {
				continue
			}
			refs = append(refs, sqlRef(filePath, colID, tgtName, types.EdgeKindReferences, line))
		}
		return
	}

	n := len(localCols)
	if len(tgtCols) < n {
		n = len(tgtCols)
	}
	for i := 0; i < n; i++ {
		colID := colIDByLowerName[strings.ToLower(localCols[i])]
		if colID == "" {
			continue
		}
		refs = append(refs, sqlRef(filePath, colID, tgtName+"."+tgtCols[i], types.EdgeKindReferences, line))
	}
	return
}

// extractColumnFKRefs emits references from each local column node — not the
// table node — for both the table-level FOREIGN KEY (…) REFERENCES form and the
// inline `col TYPE REFERENCES tgt (x)` form. Additive to the caller's table→table
// edge. bodyNoStr must be the string-blanked body so a REFERENCES inside a literal
// cannot produce an edge.
func extractColumnFKRefs(
	filePath, stripped, bodyNoStr string, bodyOff int,
	colIDByLowerName map[string]string,
) (refs []types.UnresolvedReference) {

	if bodyNoStr == "" {
		return
	}

	// Table-level: FOREIGN KEY (a[, b]) REFERENCES tgt (x[, y])
	for _, m := range tableLevelFKColRE.FindAllStringSubmatchIndex(bodyNoStr, -1) {
		localCols := splitIdentList(bodyNoStr[m[2]:m[3]])
		rawTgt := bodyNoStr[m[4]:m[5]]
		_, tgtName := parseQName(rawTgt)
		if tgtName == "" || isSQLRefKeyword(tgtName) || len(localCols) == 0 {
			continue
		}
		var tgtCols []string
		if m[6] >= 0 {
			tgtCols = splitIdentList(bodyNoStr[m[6]:m[7]])
		}
		line := strings.Count(stripped[:bodyOff+m[0]], "\n") + 1
		refs = append(refs, columnFKRefs(filePath, tgtName, localCols, tgtCols, colIDByLowerName, line)...)
	}

	// Inline: walk lines as extractColumns does, so a REFERENCES attributes to the
	// column defined on that line and not a neighbour.
	lines := strings.Split(bodyNoStr, "\n")
	lineOffset := strings.Count(stripped[:bodyOff], "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimRight(trimmed, ", \t")
		if trimmed == "" {
			continue
		}
		firstWord := strings.ToUpper(strings.Fields(trimmed)[0])
		if constraintKeywords[firstWord] {
			continue // table-level constraint line — handled above
		}
		colName := extractFirstIdent(trimmed)
		if colName == "" || isSQLKeyword(colName) {
			continue
		}
		colID := colIDByLowerName[strings.ToLower(colName)]
		if colID == "" {
			continue
		}
		im := inlineColFKColListRE.FindStringSubmatchIndex(trimmed)
		if im == nil {
			continue
		}
		rawTgt := trimmed[im[2]:im[3]]
		_, tgtName := parseQName(rawTgt)
		if tgtName == "" || isSQLRefKeyword(tgtName) {
			continue
		}
		lineNum := lineOffset + i + 1
		if im[4] >= 0 {
			if tgtCols := splitIdentList(trimmed[im[4]:im[5]]); len(tgtCols) > 0 {
				refs = append(refs, sqlRef(filePath, colID, tgtName+"."+tgtCols[0], types.EdgeKindReferences, lineNum))
				continue
			}
		}
		refs = append(refs, sqlRef(filePath, colID, tgtName, types.EdgeKindReferences, lineNum))
	}

	return
}

// --- Constraint extraction helpers ---

// normalizeConstraintType maps SQL keywords to a canonical constraint_type.
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

// A CONSTRAINT <name> <type> line inside a CREATE TABLE body.
var namedConstraintLineRE = regexp.MustCompile(`(?i)^\s*CONSTRAINT\s+(` + sqlIdentOnlyRaw + `)\s+(PRIMARY\s+KEY|FOREIGN\s+KEY|UNIQUE|CHECK)\b`)

// An anonymous table-level constraint line, with no CONSTRAINT <name> prefix.
var anonConstraintLineRE = regexp.MustCompile(`(?i)^\s*(PRIMARY\s+KEY|FOREIGN\s+KEY|UNIQUE|CHECK)\b`)

// extractConstraints emits constraint nodes and contains edges for the named and
// anonymous table-level constraints in a CREATE TABLE body.
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

	// Marks continuation lines already folded into an earlier constraint: a
	// FOREIGN KEY line under its own CONSTRAINT name would otherwise be read again
	// as anonymous, renaming a descriptive key to `<table>_fk_1`.
	consumed := make([]bool, len(lines))

	for i, line := range lines {
		if consumed[i] {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimRight(trimmed, ", \t")

		// A constraint may wrap, with the name on one line and the key clause on
		// the next, so match the whole constraint rather than the CONSTRAINT line.
		full, last := constraintSpan(lines, i)
		if last > i {
			if nm := namedConstraintLineRE.FindStringSubmatch(full); nm != nil {
				for k := i; k <= last; k++ {
					consumed[k] = true
				}
				trimmed = strings.TrimSpace(full)
			}
		}

		// Named constraint: CONSTRAINT <name> <type> ...
		if nm := namedConstraintLineRE.FindStringSubmatch(trimmed); nm != nil {
			name := normIdent(nm[1])
			ctype := normalizeConstraintType(nm[2])
			lineNum := lineOffset + i + 1
			qname := tableName + "." + name
			id := extraction.GenerateNodeID(filePath, string(types.NodeKindConstraint), qname, lineNum)
			meta := buildConstraintMeta(ctype, "", localConstraintColumns(constraintText(lines, i), ctype))
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
			meta := buildConstraintMeta(ctype, "", localConstraintColumns(constraintText(lines, i), ctype))
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

// anonSuffix is the short tag used in a synthesized anonymous constraint name.
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

// buildConstraintMeta builds a constraint node's Metadata. references is the FK
// target table, empty for a non-FK; columns are in declaration order.
func buildConstraintMeta(ctype, references string, columns []string) json.RawMessage {
	m := map[string]any{"constraint_type": ctype}
	if references != "" {
		m["references"] = references
	}
	if len(columns) > 0 {
		m["columns"] = columns
	}
	b, _ := json.Marshal(m)
	return b
}

// constraintText follows continuation lines from lines[i] until the parentheses
// balance. A constraint routinely splits across name / FOREIGN KEY / REFERENCES
// lines, so the keyword's own line often holds no column list at all.
func constraintText(lines []string, i int) string {
	text, _ := constraintSpan(lines, i)
	return text
}

// constraintSpan is constraintText plus the index of the last line it folded
// in, so the caller can skip those lines rather than re-reading them.
func constraintSpan(lines []string, i int) (string, int) {
	var b strings.Builder
	depth := 0
	opened := false
	last := i
	for ; i < len(lines); i++ {
		b.WriteByte(' ')
		b.WriteString(lines[i])
		last = i
		for _, ch := range lines[i] {
			switch ch {
			case '(':
				depth++
				opened = true
			case ')':
				depth--
			}
		}
		// Stop at the first balanced group: for a foreign key that is the local
		// column list, while any REFERENCES that follows names the target's.
		if opened && depth <= 0 {
			break
		}
	}
	return b.String(), last
}

// statementAt returns the statement beginning at offset — up to the next
// semicolon, so a column list split across lines is still in scope while the
// statement after it is not.
func statementAt(source string, at int) string {
	if at < 0 || at >= len(source) {
		return ""
	}
	rest := source[at:]
	if end := strings.IndexByte(rest, ';'); end >= 0 {
		return rest[:end]
	}
	return rest
}

// constraintTypeKeyword is the word whose following parenthesis holds the
// constrained column list.
var constraintTypeKeyword = map[string]string{
	"primary_key": "PRIMARY",
	"unique":      "UNIQUE",
	"foreign_key": "FOREIGN",
}

// localConstraintColumns reads a constraint's columns from its declaration rather
// than inferring them from its name. CHECK and EXCLUDE are excluded: their
// parentheses hold an expression, not a column list.
func localConstraintColumns(text, ctype string) []string {
	keyword, ok := constraintTypeKeyword[ctype]
	if !ok {
		return nil
	}
	at := strings.Index(strings.ToUpper(text), keyword)
	if at < 0 {
		return nil
	}
	inner, _ := findParenBlock(text, at)
	if inner == "" {
		return nil
	}

	var cols []string
	for _, part := range strings.Split(inner, ",") {
		// Index specifications carry a direction or opclass after the name.
		field := strings.TrimSpace(part)
		if space := strings.IndexAny(field, " \t"); space > 0 {
			field = field[:space]
		}
		if c := normIdent(field); c != "" {
			cols = append(cols, c)
		}
	}
	return cols
}

// findParenBlock returns the content of the first balanced paren block at or
// after startOffset, plus the offset just past its opening '('.
func findParenBlock(source string, startOffset int) (string, int) {
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

// extractFirstIdentLen is the byte length of the leading identifier, quotes
// included, so a caller can step past the column name to the type token.
func extractFirstIdentLen(line string) int {
	line = strings.TrimSpace(line)
	if len(line) == 0 {
		return 0
	}
	switch line[0] {
	case '"':
		end := strings.Index(line[1:], `"`)
		if end < 0 {
			return 0
		}
		return end + 2 // include both quote chars
	case '`':
		end := strings.Index(line[1:], "`")
		if end < 0 {
			return 0
		}
		return end + 2
	case '[':
		end := strings.Index(line[1:], "]")
		if end < 0 {
			return 0
		}
		return end + 2
	default:
		end := 0
		for end < len(line) {
			c := line[end]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$' {
				end++
			} else {
				break
			}
		}
		return end
	}
}

// extractFirstIdent returns the leading identifier, unwrapping any quoting style.
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

// getSourceLine returns the 0-indexed line lineIdx of source.
func getSourceLine(source string, lineIdx int) string {
	lines := strings.SplitN(source, "\n", lineIdx+2)
	if lineIdx < len(lines) {
		return lines[lineIdx]
	}
	return ""
}

// Tokens in the type position that are attributes, not types (`col DEFAULT 0`).
// A denylist rather than a type allowlist, so user-defined types pass through.
var colAttributeKeywords = map[string]bool{
	"DEFAULT": true, "REFERENCES": true, "COLLATE": true, "COMMENT": true,
	"ENCODING": true, "CONSTRAINT": true, "PRIMARY": true, "UNIQUE": true,
	"CHECK": true, "GENERATED": true, "NOT": true, "NULL": true, "AS": true,
}

// Reserved words that cannot be column names.
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

// buildTableNodeIDMap indexes table nodes by lower-cased name.
func buildTableNodeIDMap(nodes []types.Node) map[string]string {
	m := make(map[string]string, len(nodes))
	for _, n := range nodes {
		if n.Kind == types.NodeKindTable {
			m[strings.ToLower(n.Name)] = n.ID
		}
	}
	return m
}

// --- Unresolved reference helper ---

// sqlRef builds an UnresolvedReference from a SQL node to a named target.
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

// Keywords that must never be emitted as reference targets, or they mint edges
// to nodes that cannot exist.
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
	// Table-function and clause modifiers that appear right after FROM/JOIN.
	"LATERAL": true, "UNNEST": true, "ROWS": true,
}

func isSQLRefKeyword(s string) bool {
	return sqlKeywordsForRef[strings.ToUpper(s)]
}

// --- Statement-body extraction helpers ---

// extractViewBody returns a view's SELECT body: from the AS that follows
// startOffset to the next statement boundary.
func extractViewBody(source string, startOffset int) string {
	tail := source[startOffset:]
	loc := bodyAsRE.FindStringIndex(tail)
	if loc == nil {
		return ""
	}
	body := tail[loc[1]:]
	return trimToStatementEnd(body)
}

// extractStmtText returns the rest of the statement at startOffset, ending at a
// semicolon or the next top-level CREATE.
func extractStmtText(source string, startOffset int) string {
	if startOffset >= len(source) {
		return ""
	}
	return trimToStatementEnd(source[startOffset:])
}

// A statement boundary: semicolon or a top-level CREATE.
var nextStmtRE = regexp.MustCompile(`(?im)(?:;|^[ \t]*CREATE\b)`)

// trimToStatementEnd cuts text at the first statement boundary, if there is one.
func trimToStatementEnd(text string) string {
	loc := nextStmtRE.FindStringIndex(text)
	if loc == nil {
		return text
	}
	return text[:loc[0]]
}

// --- Routine body extraction helpers ---

// The opening dollar-quote tag ($$ or $tag$) of a PG routine body.
var routineDollarRE = regexp.MustCompile(`(\$[A-Za-z0-9_]*\$)`)

// The BEGIN that starts a T-SQL or PG block.
var routineBeginRE = regexp.MustCompile(`(?i)\bBEGIN\b`)

// BEGIN/END pairs, for depth tracking inside a block.
var routineTokenRE = regexp.MustCompile(`(?i)\bBEGIN\b|\bEND\b`)

// The AS keyword, shared by the view and routine body-extraction paths.
var bodyAsRE = regexp.MustCompile(`(?i)\bAS\b`)

// extractRoutineBody returns a routine's body and its byte offset in source;
// returning the offset spares callers a fragile strings.Index re-search. Three
// shapes are tried in order: a PG dollar-quoted $tag$…$tag$ body, a depth-tracked
// BEGIN…END block, then a bare AS clause up to the statement end.
func extractRoutineBody(source string, startOffset int) (string, int) {
	if startOffset >= len(source) {
		return "", 0
	}
	tail := source[startOffset:]

	if dm := routineDollarRE.FindStringIndex(tail); dm != nil {
		tag := tail[dm[0]:dm[1]]
		closeIdx := strings.Index(tail[dm[1]:], tag)
		if closeIdx >= 0 {
			bodyOff := startOffset + dm[1]
			return tail[dm[1] : dm[1]+closeIdx], bodyOff
		}
	}

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

	if loc := bodyAsRE.FindStringIndex(tail); loc != nil {
		bodyOff := startOffset + loc[1]
		return trimToStatementEnd(tail[loc[1]:]), bodyOff
	}
	return "", 0
}

// extractCTENames collects lower-cased WITH <name> bindings. CTE names are
// statement-local, so a ref to one must not become an edge to a missing table.
func extractCTENames(body string) map[string]bool {
	ctes := map[string]bool{}
	for _, m := range cteNameRE.FindAllStringSubmatch(body, -1) {
		if len(m) > 1 {
			ctes[strings.ToLower(normIdent(m[1]))] = true
		}
	}
	return ctes
}

// scanBodyEdges emits references, writes and calls edges from a stripped routine
// or view body, skipping keywords and CTE names. bodyBaseOffset locates body
// within strippedFull so line numbers stay accurate.
//
// routineName scopes T-SQL #tmp / @tvar names and is "" where no temp can be
// declared; globalTempNodes is the file-wide ##temp map, nil when unused.
func scanBodyEdges(
	filePath string,
	fromNodeID string,
	body string,
	bodyBaseOffset int,
	strippedFull string,
	cteShadow map[string]bool,
	routineName string,
	globalTempNodes map[string]*types.Node,
) ([]types.Node, []types.UnresolvedReference) {
	var tempNodes []types.Node
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

	// Maps a temp token to its synthetic name, or false when undeclared here. Set
	// by the pre-scan below; the no-op default makes OUTPUT INTO skip temps.
	resolveTempFn := func(_ string) (string, bool) { return "", false }

	// -- T-SQL temp / table-variable declaration pre-scan --
	//
	// Only declared tokens emit edges, so a stray #tmp from another proc is
	// skipped. #x and @x are routine-local, named routineName+token; ##x is
	// file-local and deduped through globalTempNodes.
	type tempDecl struct {
		token     string // as written, e.g. "#staging", "##g", "@results"
		synthetic string // resolved Name for UnresolvedReference
		isGlobal  bool
	}
	var localDecls []tempDecl // routine-scoped (#x / @x)
	seenLocal := map[string]bool{}

	if routineName != "" {
		// 1. CREATE TABLE #x / ##x
		for _, m := range bodyTempCreateRE.FindAllStringSubmatchIndex(body, -1) {
			tok := body[m[2]:m[3]]
			lower := strings.ToLower(tok)
			isGlobal := strings.HasPrefix(tok, "##")
			if isGlobal {
				if globalTempNodes != nil && globalTempNodes[lower] == nil {
					node := makeTempNode(filePath, tok, tok, "global", bodyBaseOffset+m[2], strippedFull)
					globalTempNodes[lower] = &node
				}
			} else if !seenLocal[lower] {
				seenLocal[lower] = true
				synthetic := routineName + tok
				localDecls = append(localDecls, tempDecl{token: tok, synthetic: synthetic, isGlobal: false})
			}
		}

		// 2. SELECT … INTO #x / ##x — declares and writes in one statement.
		for _, m := range bodyTempSelectIntoRE.FindAllStringSubmatchIndex(body, -1) {
			tok := body[m[2]:m[3]]
			lower := strings.ToLower(tok)
			isGlobal := strings.HasPrefix(tok, "##")
			if isGlobal {
				if globalTempNodes != nil && globalTempNodes[lower] == nil {
					node := makeTempNode(filePath, tok, tok, "global", bodyBaseOffset+m[2], strippedFull)
					globalTempNodes[lower] = &node
				}
			} else if !seenLocal[lower] {
				seenLocal[lower] = true
				synthetic := routineName + tok
				localDecls = append(localDecls, tempDecl{token: tok, synthetic: synthetic, isGlobal: false})
			}
		}

		// 3. DECLARE @x TABLE(…) — a scalar DECLARE cannot match.
		for _, m := range bodyDeclareTableVarRE.FindAllStringSubmatchIndex(body, -1) {
			tok := body[m[2]:m[3]] // @x
			lower := strings.ToLower(tok)
			if !seenLocal[lower] {
				seenLocal[lower] = true
				synthetic := routineName + tok
				localDecls = append(localDecls, tempDecl{token: tok, synthetic: synthetic, isGlobal: false})
			}
		}

		localMap := make(map[string]tempDecl, len(localDecls))
		for _, d := range localDecls {
			localMap[strings.ToLower(d.token)] = d
			node := makeTempNode(filePath, d.token, d.synthetic, "local", 0, strippedFull)
			tempNodes = append(tempNodes, node)
		}

		// -- temp-token scans --

		// Assigned to resolveTempFn so the OUTPUT INTO scan can reuse it below.
		resolveTemp := func(tok string) (string, bool) {
			lower := strings.ToLower(tok)
			if strings.HasPrefix(tok, "##") {
				if globalTempNodes != nil && globalTempNodes[lower] != nil {
					return tok, true
				}
				return "", false
			}
			if d, ok := localMap[lower]; ok {
				return d.synthetic, true
			}
			return "", false
		}
		resolveTempFn = resolveTemp

		// INSERT INTO <temp> → writes.
		for _, m := range bodyTempInsertRE.FindAllStringSubmatchIndex(body, -1) {
			tok := body[m[2]:m[3]]
			if syn, ok := resolveTemp(tok); ok {
				addRef(syn, types.EdgeKindWrites, m[2])
			}
		}

		// SELECT … INTO <temp> → writes.
		for _, m := range bodyTempSelectIntoRE.FindAllStringSubmatchIndex(body, -1) {
			tok := body[m[2]:m[3]]
			if syn, ok := resolveTemp(tok); ok {
				addRef(syn, types.EdgeKindWrites, m[2])
			}
		}

		// FROM / JOIN <temp> → references
		for _, m := range bodyTempFROMRE.FindAllStringSubmatchIndex(body, -1) {
			tok := body[m[2]:m[3]]
			if syn, ok := resolveTemp(tok); ok {
				addRef(syn, types.EdgeKindReferences, m[2])
			}
		}

		// UPDATE <temp> SET → writes
		for _, m := range bodyTempUpdateRE.FindAllStringSubmatchIndex(body, -1) {
			tok := body[m[2]:m[3]]
			if syn, ok := resolveTemp(tok); ok {
				addRef(syn, types.EdgeKindWrites, m[2])
			}
		}

		// DELETE FROM <temp> → writes
		for _, m := range bodyTempDeleteRE.FindAllStringSubmatchIndex(body, -1) {
			tok := body[m[2]:m[3]]
			if syn, ok := resolveTemp(tok); ok {
				addRef(syn, types.EdgeKindWrites, m[2])
			}
		}

		// MERGE INTO <temp> → writes
		for _, m := range bodyTempMergeRE.FindAllStringSubmatchIndex(body, -1) {
			tok := body[m[2]:m[3]]
			if syn, ok := resolveTemp(tok); ok {
				addRef(syn, types.EdgeKindWrites, m[2])
			}
		}

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

	// FLATTEN(<expr>): a dotted expr is a column reference, not a relation.
	for _, m := range bodyFlattenRE.FindAllStringSubmatchIndex(body, -1) {
		rawExpr := body[m[2]:m[3]]
		if strings.ContainsRune(rawExpr, '.') {
			continue
		}
		_, name := parseQName(rawExpr)
		if name != "" {
			addRef(name, types.EdgeKindReferences, m[2])
		}
	}

	// COPY INTO <target> FROM <source>: a leading '@' makes the stage the target.
	for _, m := range bodyCopyIntoRE.FindAllStringSubmatchIndex(body, -1) {
		rawTarget := body[m[2]:m[3]]
		rawSource := body[m[4]:m[5]]

		if strings.HasPrefix(rawTarget, "@") {
			// COPY INTO @stage FROM <tbl>
			stageName := parseStageToken(rawTarget)
			_, tblName := parseQName(rawSource)
			if stageName != "" {
				addRef(stageName, types.EdgeKindWrites, m[2])
			}
			if tblName != "" {
				addRef(tblName, types.EdgeKindReferences, m[4])
			}
		} else {
			// COPY INTO <tbl> FROM @stage[/path]
			_, tblName := parseQName(rawTarget)
			stageName := parseStageToken(rawSource)
			if tblName != "" {
				addRef(tblName, types.EdgeKindWrites, m[2])
			}
			if stageName != "" {
				addRef(stageName, types.EdgeKindReferences, m[4])
			}
		}
	}

	// CROSS APPLY / OUTER APPLY <tvf>( → calls
	for _, m := range bodyApplyRE.FindAllStringSubmatchIndex(body, -1) {
		rawName := body[m[2]:m[3]]
		_, name := parseQName(rawName)
		if name != "" {
			addRef(name, types.EdgeKindCalls, m[2])
		}
	}

	// OUTPUT … INTO <target> → writes. outputIntoBoundaryRE rejects a gap holding a
	// DML keyword, where the lazy match has run across two statements. A temp target
	// resolves through resolveTempFn and is dropped when undeclared.
	for _, m := range bodyOutputIntoRE.FindAllStringSubmatchIndex(body, -1) {
		gap := body[m[2]:m[3]]       // group 1: gap text between OUTPUT and INTO
		rawTarget := body[m[4]:m[5]] // group 2: target name (temp or real table)

		if outputIntoBoundaryRE.MatchString(gap) {
			continue
		}

		isTempTarget := strings.HasPrefix(rawTarget, "#") || strings.HasPrefix(rawTarget, "@")
		if isTempTarget {
			if syn, ok := resolveTempFn(rawTarget); ok {
				addRef(syn, types.EdgeKindWrites, m[4])
			}
		} else {
			_, name := parseQName(rawTarget)
			if name != "" {
				addRef(name, types.EdgeKindWrites, m[4])
			}
		}
	}

	// Qualified "alias.col" refs; these coexist with the table-level edges above.
	bodyAliasMap := buildAliasMap(body, cteShadow)
	emitQualifiedColumnRefs(body, bodyAliasMap, cteShadow, func(refName string, matchOff int) {
		addRef(refName, types.EdgeKindReferences, matchOff)
	})

	return tempNodes, refs
}

// makeTempNode builds a synthetic table node for a T-SQL temp or table variable.
// syntheticName drives resolution; token keeps the written form for display.
func makeTempNode(filePath, token, syntheticName, tempScope string, byteOff int, strippedFull string) types.Node {
	line := 1
	if byteOff > 0 && byteOff <= len(strippedFull) {
		line = strings.Count(strippedFull[:byteOff], "\n") + 1
	}
	id := extraction.GenerateNodeID(filePath, string(types.NodeKindTable), syntheticName, line)
	meta, _ := json.Marshal(map[string]string{"temp": tempScope, "token": token})
	return types.Node{
		ID:            id,
		Kind:          types.NodeKindTable,
		Name:          syntheticName,
		QualifiedName: syntheticName,
		FilePath:      filePath,
		Language:      types.LanguageSQL,
		StartLine:     line,
		EndLine:       line,
		IsExported:    false,
		Metadata:      meta,
	}
}

// parseStageToken strips '@' and any '/path' from a Snowflake stage token. The
// @~ user stage and @%tbl table stage name no object, so they return "".
func parseStageToken(raw string) string {
	s := strings.TrimPrefix(raw, "@")
	if strings.HasPrefix(s, "~") || strings.HasPrefix(s, "%") {
		return ""
	}
	if idx := strings.IndexByte(s, '/'); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}
