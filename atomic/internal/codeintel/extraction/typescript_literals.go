package extraction

// typescript_literals.go — CP4: tree-sitter-based TypeScript/TSX string literal harvester.
//
// HarvestTypeScriptLiterals parses a TypeScript or TSX source file and returns
// all string literal spans with:
//   - The literal text (post-substitution for template literals: interpolation
//     segments replaced with "?" so they act as SQL parameter placeholders).
//   - 1-based file-absolute StartLine / EndLine.
//
// WHY tree-sitter instead of a flat scanner: template-literal `${...}` segments
// need structural detection so they can be substituted. A byte scanner cannot
// reliably distinguish `${expr}` from literal dollar-brace text inside strings.
//
// No docstring concept: TypeScript/TSX has no docstring positions (unlike Python),
// so no span is ever excluded on positional grounds.
//
// Node types verified against parser.c symbol tables for both TS and TSX grammars:
//   - string             — plain string literals (single or double-quoted)
//   - template_string    — template literals (backtick-quoted)
//   - template_substitution — ${expr} child inside template_string
//   - string_fragment    — raw text content (child of both string and template_string)
//   - escape_sequence    — escape child inside string (skipped — not content)
//
// The harvester is language-agnostic between TS and TSX: both grammars use the
// same node-type strings. The caller sets LangTypeScript or LangTSX on the pool
// instance before calling.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"
)

// TSLiteralSpan holds one TypeScript/TSX string literal span returned by
// HarvestTypeScriptLiterals.
type TSLiteralSpan struct {
	// Text is the literal content after template-literal interpolation substitution.
	// For plain strings: the raw content between the delimiters (string_fragment
	// segments joined; escape_sequence tokens skipped as non-content).
	// For template literals: string_fragment segments joined with "?" replacing
	// each template_substitution — so `SELECT a FROM ${t} WHERE id = ${id}`
	// becomes "SELECT a FROM ? WHERE id = ?".
	Text string
	// StartLine is the 1-based line where the opening delimiter sits.
	StartLine int
	// EndLine is the 1-based line where the closing delimiter sits.
	EndLine int
	// CalleeExpr is the bare name of the nearest enclosing call_expression's
	// callee (e.g. "selectFrom" for db.selectFrom("x"), "from" for a member
	// call knex.from("x")) when the literal sits in that call's argument list.
	// Empty when the literal is not inside a call expression. Used by
	// sql-string-match (C1) confidence tiering.
	CalleeExpr string
}

// HarvestTypeScriptLiterals parses src as TypeScript or TSX via inst. The
// caller must set LangTypeScript or LangTSX on inst before calling (or pass
// lang so this function sets it). This function sets lang on inst directly.
//
// The caller is responsible for borrowing inst from a pool and returning it
// after this call. HarvestTypeScriptLiterals does not borrow or return instances.
//
// Returns (nil, nil) when the source has no string literals.
func HarvestTypeScriptLiterals(ctx context.Context, inst Instance, src string, lang Lang) ([]TSLiteralSpan, error) {
	if err := inst.SetLanguage(ctx, lang); err != nil {
		return nil, err
	}

	tree, err := inst.ParseString(ctx, src)
	if err != nil {
		return nil, err
	}

	root, err := tree.(*tsTree).rootNode(ctx)
	if err != nil {
		return nil, err
	}

	// lineOffsets[i] = byte offset of the first character of line i+1.
	lineOffsets := buildLineOffsets(src)

	var spans []TSLiteralSpan
	if err := tsWalkNode(ctx, root, src, lineOffsets, "", &spans); err != nil {
		return nil, err
	}

	return spans, nil
}

// tsWalkNode recursively walks the tree rooted at node, collecting string and
// template_string literals. calleeCtx is the bare callee name of the nearest
// enclosing call_expression (sql-string-match C1 callee capture), or "" when
// node is not inside one.
func tsWalkNode(ctx context.Context, node sitter.Node, src string, lineOffsets []int, calleeCtx string, out *[]TSLiteralSpan) error {
	kind, err := node.Kind(ctx)
	if err != nil {
		return nil // best-effort: Kind() failed — skip this subtree entirely
	}

	switch kind {
	case "string", "template_string":
		span, err := tsHarvestLiteral(ctx, node, src, lineOffsets)
		if err != nil || span == nil {
			return nil // best-effort: harvest failed — skip this literal
		}
		span.CalleeExpr = calleeCtx
		*out = append(*out, *span)
		return nil // do not recurse into string children

	case "call_expression":
		// Nearest enclosing call: recompute calleeCtx for this subtree, but
		// scope it to the "arguments" field only — a literal in the callee/
		// receiver position (e.g. "tbl".toUpperCase()) must not inherit the
		// call's own callee. Children outside "arguments" keep the outer
		// calleeCtx; deeper nested calls will overwrite it again for their
		// own arguments subtree.
		newCallee := tsCalleeBareName(ctx, node, src)
		return tsWalkCallChildren(ctx, node, src, lineOffsets, calleeCtx, newCallee, out)

	default:
		return tsWalkChildren(ctx, node, src, lineOffsets, calleeCtx, out)
	}
}

// tsWalkChildren visits all named children of node.
func tsWalkChildren(ctx context.Context, node sitter.Node, src string, lineOffsets []int, calleeCtx string, out *[]TSLiteralSpan) error {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return nil
	}
	for i := uint64(0); i < cnt; i++ {
		child, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		if err := tsWalkNode(ctx, child, src, lineOffsets, calleeCtx, out); err != nil {
			return err
		}
	}
	return nil
}

// tsWalkCallChildren visits the named children of a call_expression node,
// passing argsCallee to the subtree rooted at the "arguments" field and
// outerCallee to every other child (the "function" field and any
// type_arguments).
func tsWalkCallChildren(ctx context.Context, node sitter.Node, src string, lineOffsets []int, outerCallee, argsCallee string, out *[]TSLiteralSpan) error {
	argsNode, argsErr := node.ChildByFieldName(ctx, "arguments")
	var argsStart, argsEnd uint64
	haveArgs := argsErr == nil
	if haveArgs {
		argsStart, _ = argsNode.StartByte(ctx)
		argsEnd, _ = argsNode.EndByte(ctx)
	}

	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return nil
	}
	for i := uint64(0); i < cnt; i++ {
		child, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		calleeCtx := outerCallee
		if haveArgs {
			sb, _ := child.StartByte(ctx)
			eb, _ := child.EndByte(ctx)
			if sb == argsStart && eb == argsEnd {
				calleeCtx = argsCallee
			}
		}
		if err := tsWalkNode(ctx, child, src, lineOffsets, calleeCtx, out); err != nil {
			return err
		}
	}
	return nil
}

// tsCalleeBareName returns the bare invoked name of a call_expression node's
// "function" field: the identifier itself for a plain call ("foo(...)"), or
// the "property" field of a member_expression for a method call
// ("db.selectFrom(...)" → "selectFrom"). Returns "" for any other callee
// shape (e.g. a chained call result, a computed member) — best-effort, per
// the harvester's existing failure policy.
func tsCalleeBareName(ctx context.Context, callExpr sitter.Node, src string) string {
	fn, err := callExpr.ChildByFieldName(ctx, "function")
	if err != nil {
		return ""
	}
	kind, err := fn.Kind(ctx)
	if err != nil {
		return ""
	}
	switch kind {
	case "identifier":
		return tsNodeText(ctx, fn, src)
	case "member_expression":
		prop, err := fn.ChildByFieldName(ctx, "property")
		if err != nil {
			return ""
		}
		return tsNodeText(ctx, prop, src)
	default:
		return ""
	}
}

// tsNodeText returns the raw source text spanned by node, or "" on failure.
func tsNodeText(ctx context.Context, node sitter.Node, src string) string {
	sb, err := node.StartByte(ctx)
	if err != nil {
		return ""
	}
	eb, err := node.EndByte(ctx)
	if err != nil || int(eb) > len(src) || sb >= eb {
		return ""
	}
	return src[sb:eb]
}

// tsHarvestLiteral extracts text and line numbers from a "string" or
// "template_string" node. Returns nil when the node cannot be processed.
//
// For plain strings: collects string_fragment children and joins them.
// For template literals: collects string_fragment segments and replaces each
// template_substitution child with "?" — the substitution contract for decision 8.
func tsHarvestLiteral(ctx context.Context, node sitter.Node, src string, lineOffsets []int) (*TSLiteralSpan, error) {
	startByte, err := node.StartByte(ctx)
	if err != nil {
		return nil, nil
	}
	endByte, err := node.EndByte(ctx)
	if err != nil {
		return nil, nil
	}

	startLine := pyByteToLine(lineOffsets, startByte)
	endLine := pyByteToLine(lineOffsets, endByte)

	// Walk children to collect content segments and substitute interpolations.
	cnt, _ := node.NamedChildCount(ctx)
	var textParts []string

	for i := uint64(0); i < cnt; i++ {
		child, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		childKind, _ := child.Kind(ctx)
		csb, _ := child.StartByte(ctx)
		ceb, _ := child.EndByte(ctx)

		switch childKind {
		case "string_fragment":
			// Raw content segment — present in both plain strings and template literals.
			if int(ceb) <= len(src) && csb < ceb {
				textParts = append(textParts, src[csb:ceb])
			}

		case "template_substitution":
			// Substitute ${expr} with "?" per decision 8.
			// An interpolated value placeholder becomes a SQL parameter.
			// An interpolated table target becomes "SELECT FROM ?" (no recognisable
			// identifier after FROM), yielding zero refs.
			textParts = append(textParts, "?")

		case "escape_sequence":
			// Escape sequences inside plain strings — skip, not meaningful content
			// for SQL matching.

		default:
			// Opening/closing delimiter tokens and any other structural nodes
			// (backtick, quote chars) — skip.
		}
	}

	text := strings.Join(textParts, "")
	if text == "" {
		return nil, nil
	}

	return &TSLiteralSpan{
		Text:      text,
		StartLine: startLine,
		EndLine:   endLine,
	}, nil
}
