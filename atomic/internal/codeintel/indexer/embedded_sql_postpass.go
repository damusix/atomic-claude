package indexer

// Orchestrator post-pass that pulls SQL back out of host-language string
// literals: harvest literal spans, gate them, attribute each to an owner node,
// and merge the extracted SQL into the file's result before it is stored.
// See docs/spec/embedded-sql-language-expansion.md.
//
// Runs only for tree-sitter-extracted host files — .sql and friends still take
// the standalone-extractor path untouched.

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// sqlStringIdentifierRE gates speculative sql_string refs to identifier-shaped
// literals. Contract: docs/spec/sql-string-match.md.
var sqlStringIdentifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{2,}(\.[A-Za-z_][A-Za-z0-9_]+)?$`)

// embeddedSQLHostExts is the cheap first gate; init() adds the generic
// languages to it. An entry without a harvester yields nothing.
var embeddedSQLHostExts = map[string]bool{
	".go":  true,
	".py":  true,
	".ts":  true,
	".tsx": true,
}

// literalHarvester returns string-literal spans with 1-based, file-absolute
// lines. ctx and pool exist for harvesters that need tree-sitter; the
// hand-written ones ignore both. No spans and no error means no candidates.
type literalHarvester func(ctx context.Context, src string, pool *extraction.Pool) ([]standalone.StringLiteralSpan, error)

// harvesterRegistry is keyed by lower-case extension.
var harvesterRegistry = map[string]literalHarvester{
	".go":  goHarvesterAdapter,
	".py":  harvestPythonStringLiterals,
	".ts":  harvestTypeScriptStringLiterals,
	".tsx": harvestTSXStringLiterals,
}

func goHarvesterAdapter(_ context.Context, src string, _ *extraction.Pool) ([]standalone.StringLiteralSpan, error) {
	return standalone.HarvestGoStringLiterals(src), nil
}

// makeGenericHarvester is the table-driven counterpart to the bespoke
// per-language harvesters: one closure serves every language in
// embeddedLiteralConfigs.
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

// init wires generic harvesters by walking the intersection of extToLanguage
// and embeddedLiteralConfigs, so there is no second hand-maintained ext list.
//
// Never add a standalone-routed language (SQL, Svelte, Vue, XML …) to
// embeddedLiteralConfigs: it would be wired as an embedded host here AND
// routed as a standalone file by orchestrator.go, indexing the same content
// twice. Their absence from that map is also what makes this init's ordering
// against orchestrator.go's irrelevant.
func init() {
	for ext, lang := range extToLanguage {
		entry, ok := embeddedLiteralConfigs[lang]
		if !ok {
			continue
		}
		// Bespoke harvesters carry language-specific logic (docstring
		// exclusion, JSX awareness) and must win over the generic one.
		if _, exists := harvesterRegistry[ext]; exists {
			continue
		}
		harvesterRegistry[ext] = makeGenericHarvester(entry)
		embeddedSQLHostExts[ext] = true
	}
}

// embeddedSQLPostPass appends embedded nodes, edges, and refs to result in
// place. It cannot fail: a bad literal is skipped, matching the host
// extraction error policy.
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
		return
	}
	if len(spans) == 0 {
		return
	}

	fileNodeID := "file:" + relPath
	ownerNodes := result.Nodes // file node + every extracted symbol
	hostLang := extToLanguage[ext]

	// Both dedupe (owner, literal) pairs within this file. They stay separate
	// so a fragment token and a same-text sql_string literal from one owner
	// (say "name") cannot cancel each other across kinds.
	sqlStringSeen := make(map[string]bool)
	sqlFragmentSeen := make(map[string]bool)

	for _, span := range spans {
		if !standalone.IsSQLLiteral(span.Text) {
			// Not SQL, but an identifier-shaped literal may still name a SQL
			// object; emit it speculatively and let resolution decide.
			ownerID := findOwnerNode(ownerNodes, span.StartLine, fileNodeID)
			if sqlStringIdentifierRE.MatchString(span.Text) {
				emitSpeculativeSQLStringRef(relPath, ownerID, hostLang, span, sqlStringSeen, result)
			} else {
				emitSpeculativeSQLFragmentRefs(relPath, ownerID, hostLang, span, sqlFragmentSeen, result)
			}
			continue
		}

		ownerID := findOwnerNode(ownerNodes, span.StartLine, fileNodeID)

		embedded := sqlExt.ExtractEmbeddedSQL(relPath, span.Text, span.StartLine, ownerID)
		if len(embedded.Nodes) == 0 && len(embedded.UnresolvedReferences) == 0 {
			continue
		}

		result.Nodes = append(result.Nodes, embedded.Nodes...)
		result.Edges = append(result.Edges, embedded.Edges...)
		result.UnresolvedReferences = append(result.UnresolvedReferences, embedded.UnresolvedReferences...)
	}
}

// emitSpeculativeSQLStringRef appends one sql_string ref per unique
// (ownerID, text) pair. The LanguageSQL guard is defensive: the post-pass
// never runs on a SQL host file today.
func emitSpeculativeSQLStringRef(
	relPath, ownerID string,
	hostLang types.Language,
	span standalone.StringLiteralSpan,
	seen map[string]bool,
	result *types.ExtractionResult,
) {
	if hostLang == types.LanguageSQL {
		return
	}
	if !sqlStringIdentifierRE.MatchString(span.Text) {
		return
	}
	dedupeKey := ownerID + "\x00" + span.Text
	if seen[dedupeKey] {
		return
	}
	seen[dedupeKey] = true

	result.UnresolvedReferences = append(result.UnresolvedReferences, types.UnresolvedReference{
		ID:            extraction.GenerateRefID(ownerID, span.Text, string(types.ReferenceKindSQLString), span.StartLine, 0),
		FromNodeID:    ownerID,
		ReferenceName: span.Text,
		ReferenceKind: types.ReferenceKindSQLString,
		Line:          span.StartLine,
		FilePath:      relPath,
		Language:      hostLang,
		CalleeExpr:    span.CalleeExpr,
	})
}

// findOwnerNode picks the narrowest node spanning literalStartLine, so a
// literal in a method body belongs to the method rather than its class or
// file, falling back to fileNodeID. Ties go to the first candidate in the
// extraction walk's DFS order.
func findOwnerNode(nodes []types.Node, literalStartLine int, fileNodeID string) string {
	bestID := fileNodeID
	bestSpan := int(^uint(0) >> 1) // max int

	for _, n := range nodes {
		// The file node is the fallback, never a candidate.
		if n.Kind == types.NodeKindFile {
			continue
		}
		// Zero or inverted spans come from extraction errors.
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
