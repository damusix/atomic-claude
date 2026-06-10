package indexer

// embedded_sql_postpass.go — CP2: orchestrator post-pass for embedded SQL.
//
// embeddedSQLPostPass is called after host tree-sitter extraction for files
// whose language is in embeddedSQLHostExts. It:
//
//  1. Harvests string literal spans from the source using a language-specific
//     harvester (Go: hand-written scanner; Python: tree-sitter via pool; CP4
//     adds TypeScript; CP2 adds 16 more via the generic harvester).
//  2. For each literal, calls IsSQLLiteral to gate out non-SQL strings cheaply.
//  3. Finds the owner node: the narrowest host node whose StartLine..EndLine
//     spans the literal's StartLine. Falls back to the file: node.
//  4. Calls ExtractEmbeddedSQL and merges the returned nodes/edges/refs into
//     the file's ExtractionResult before storeExtractionResult.
//
// Language extensibility: embeddedSQLHostExts controls which extensions are
// post-processed. Harvesters are dispatched by extension via the harvester
// registry below. CP4 appended ".ts"/".tsx"; CP2 derives the remaining 16
// host languages from extToLanguage + embeddedLiteralConfigs (single source,
// no second ext list — see docs/spec/embedded-sql-language-expansion.md).
//
// Scope guard: standaloneExts routing for .sql/.ddl/.pgsql/.mysql is UNCHANGED.
// This pass runs only for tree-sitter-extracted files (host languages), never
// for standalone-ext files.

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// embeddedSQLHostExts is the set of file extensions that receive the embedded
// SQL post-pass. Only extensions wired with a harvester below will actually
// produce results; the ext check is a cheap first gate.
var embeddedSQLHostExts = map[string]bool{
	".go":  true,
	".py":  true,
	".ts":  true,
	".tsx": true,
}

// literalHarvester is the function signature for per-language string literal
// harvesters. Returns a slice of spans; each span carries the literal text and
// 1-based file-absolute start/end lines.
//
// ctx and pool are passed so harvesters that require tree-sitter (e.g. Python)
// can borrow a parse instance. Harvesters that do not need tree-sitter (Go)
// ignore both parameters. Errors from individual literals are silently skipped
// by callers (best-effort policy). Returning a nil error with nil spans is
// valid when the source contains no candidate literals.
type literalHarvester func(ctx context.Context, src string, pool *extraction.Pool) ([]standalone.StringLiteralSpan, error)

// harvesterRegistry maps lower-case file extension to its harvester function.
var harvesterRegistry = map[string]literalHarvester{
	".go":  goHarvesterAdapter,
	".py":  harvestPythonStringLiterals,
	".ts":  harvestTypeScriptStringLiterals,
	".tsx": harvestTSXStringLiterals,
}

// goHarvesterAdapter wraps the Go hand-written scanner to match the
// literalHarvester signature (ignores ctx and pool).
func goHarvesterAdapter(_ context.Context, src string, _ *extraction.Pool) ([]standalone.StringLiteralSpan, error) {
	return standalone.HarvestGoStringLiterals(src), nil
}

// makeGenericHarvester returns a literalHarvester closure for any language
// supported by extraction.HarvestEmbeddedLiterals. The closure borrows a pool
// instance, calls HarvestEmbeddedLiterals, and converts each EmbeddedSpan to
// standalone.StringLiteralSpan field-for-field.
//
// This is the CP2 adapter pattern; it mirrors python_harvester.go /
// typescript_harvester.go but is shared across all 16 new languages via the
// embeddedLiteralConfigs table instead of a bespoke file per language.
func makeGenericHarvester(entry embeddedLangEntry) literalHarvester {
	return func(ctx context.Context, src string, pool *extraction.Pool) ([]standalone.StringLiteralSpan, error) {
		inst, err := pool.Borrow(ctx)
		if err != nil {
			return nil, err
		}
		defer pool.Return(inst)

		spans, err := extraction.HarvestEmbeddedLiterals(ctx, inst, src, entry.binding, entry.cfg)
		if err != nil {
			return nil, err
		}

		if len(spans) == 0 {
			return nil, nil
		}

		out := make([]standalone.StringLiteralSpan, len(spans))
		for i, s := range spans {
			out[i] = standalone.StringLiteralSpan{
				Text:      s.Text,
				StartLine: s.StartLine,
				EndLine:   s.EndLine,
			}
		}
		return out, nil
	}
}

// init derives embeddedSQLHostExts and harvesterRegistry entries for the 16
// CP2 host languages by walking the intersection of extToLanguage and
// embeddedLiteralConfigs.
//
// Init ordering note: Go executes intra-package init() functions in
// alphabetical source-file order. "embedded_sql_postpass.go" sorts before
// "orchestrator.go", so this init runs BEFORE orchestrator.go's init, which
// adds standalone-language extensions (SQL, Svelte, Vue, JSX, XML, …) to
// extToLanguage. That ordering is safe for two reasons:
//
//	(a) Only languages present in embeddedLiteralConfigs are wired here.
//	    Standalone/file-level languages (LanguageSQL, LanguageSvelte, etc.)
//	    are deliberately absent from that map — they are routed by the
//	    standalone-ext path, not the embedded post-pass.
//
//	(b) Therefore even if SQL/standalone extensions were already in
//	    extToLanguage when this init runs, the `entry, ok :=
//	    embeddedLiteralConfigs[lang]` guard would skip them.
//
// DANGER for future maintainers: if LanguageSQL (or any other
// standalone-routed language) is ever added to embeddedLiteralConfigs, this
// init will wire it as an embedded post-pass host AND orchestrator.go will
// also route it as a standalone file — double-routing the same content into
// the index. Do not add standalone languages to embeddedLiteralConfigs.
func init() {
	// Derive new host extensions from extToLanguage + embeddedLiteralConfigs.
	//
	// Single source: extToLanguage is the authoritative ext→language map.
	// embeddedLiteralConfigs is the authoritative language→config map.
	// Walking the intersection avoids a second hand-maintained ext list
	// (the embedded-sql-ext-list-dup lesson from the spec).
	//
	// Guard: never overwrite .go/.py/.ts/.tsx — those have bespoke harvesters
	// that carry language-specific logic (docstring exclusion, JSX awareness).
	for ext, lang := range extToLanguage {
		entry, ok := embeddedLiteralConfigs[lang]
		if !ok {
			continue
		}
		// Preserve pre-existing bespoke harvester entries.
		if _, exists := harvesterRegistry[ext]; exists {
			continue
		}
		harvesterRegistry[ext] = makeGenericHarvester(entry)
		embeddedSQLHostExts[ext] = true
	}
}

// embeddedSQLPostPass runs the embedded SQL post-pass for one file. It mutates
// result in-place by appending embedded nodes, edges, and unresolved refs.
//
// relPath is the file's canonical DB key (relative path). src is the full file
// source. sqlExt is the SQLExtractor used for DDL and DML extraction.
// ctx and pool are forwarded to harvesters that require tree-sitter (Python).
//
// Returns without error: extraction failures are best-effort; failed literals
// are silently skipped (consistent with the host extraction error policy).
func embeddedSQLPostPass(
	ctx context.Context,
	relPath, src string,
	result *types.ExtractionResult,
	sqlExt *standalone.SQLExtractor,
	pool *extraction.Pool,
) {
	ext := strings.ToLower(filepath.Ext(relPath))
	harvester, ok := harvesterRegistry[ext]
	if !ok {
		return
	}

	spans, err := harvester(ctx, src, pool)
	if err != nil {
		return // best-effort: harvester failure is silently skipped
	}
	if len(spans) == 0 {
		return // no candidate literals in this file
	}

	// Build a lookup of host nodes for ownership resolution.
	// We need nodes with real line spans. The file node is the fallback.
	fileNodeID := "file:" + relPath
	ownerNodes := result.Nodes // includes file node + all extracted symbols

	for _, span := range spans {
		if !standalone.IsSQLLiteral(span.Text) {
			continue
		}

		ownerID := findOwnerNode(ownerNodes, span.StartLine, fileNodeID)

		embedded := sqlExt.ExtractEmbeddedSQL(relPath, span.Text, span.StartLine, ownerID)
		if len(embedded.Nodes) == 0 && len(embedded.UnresolvedReferences) == 0 {
			continue
		}

		// Merge into the host result.
		result.Nodes = append(result.Nodes, embedded.Nodes...)
		result.Edges = append(result.Edges, embedded.Edges...)
		result.UnresolvedReferences = append(result.UnresolvedReferences, embedded.UnresolvedReferences...)
	}
}

// findOwnerNode returns the ID of the narrowest node in nodes whose
// StartLine..EndLine span contains literalStartLine. When multiple nodes
// contain the line, the narrowest (smallest line range) wins — this selects
// the innermost function/method rather than a containing class or file.
//
// Falls back to fileNodeID when no node spans the literal.
//
// WHY narrowest: a literal inside a method body should be owned by the method,
// not by the enclosing struct or file. Ties are broken by taking the first
// candidate (insertion order from the extraction walk, which is DFS top-down).
func findOwnerNode(nodes []types.Node, literalStartLine int, fileNodeID string) string {
	bestID := fileNodeID
	bestSpan := int(^uint(0) >> 1) // max int

	for _, n := range nodes {
		// Skip the file node itself; it is the fallback, not a candidate.
		if n.Kind == types.NodeKindFile {
			continue
		}
		// Skip nodes without meaningful spans (StartLine == EndLine == 0 or
		// StartLine > EndLine after extraction errors).
		if n.StartLine == 0 || n.EndLine < n.StartLine {
			continue
		}
		if n.StartLine <= literalStartLine && literalStartLine <= n.EndLine {
			span := n.EndLine - n.StartLine
			if span < bestSpan {
				bestSpan = span
				bestID = n.ID
			}
		}
	}

	return bestID
}
