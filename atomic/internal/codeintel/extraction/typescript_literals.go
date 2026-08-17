package extraction

// Tree-sitter TypeScript/TSX string-literal harvester. tree-sitter rather than a
// byte scanner because a `${…}` segment has to be detected structurally to be
// substituted, which no scanner does reliably. TS and TSX share the same
// node-type strings, so one harvester covers both.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"
)

// TSLiteralSpan is one string literal returned by HarvestTypeScriptLiterals.
type TSLiteralSpan struct {
	// Text is the content between the delimiters, escape sequences dropped; in a
	// template literal each ${…} is replaced by "?" so it reads as a SQL parameter.
	Text string
	// StartLine and EndLine are 1-based.
	StartLine int
	EndLine   int
	// CalleeExpr is the bare name of the nearest enclosing call ("selectFrom" for
	// db.selectFrom("x")), empty when the literal sits outside any call.
	CalleeExpr string
}

// HarvestTypeScriptLiterals returns every string literal span in src, with lang
// set on inst here. The caller owns inst: borrow it from a pool and return it
// afterwards. (nil, nil) means the source has no string literals.
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

	lineOffsets := buildLineOffsets(src)

	var spans []TSLiteralSpan
	if err := tsWalkNode(ctx, root, src, lineOffsets, "", &spans); err != nil {
		return nil, err
	}

	return spans, nil
}

// tsWalkNode walks the tree rooted at node. calleeCtx is the nearest enclosing
// call's bare callee, "" outside any call.
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
		// Scope the new callee to the "arguments" subtree only: a literal in callee
		// position ("tbl".toUpperCase()) must not inherit this call's own callee.
		// A deeper call overwrites it again for its own arguments.
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

// tsWalkCallChildren passes argsCallee to the "arguments" subtree and
// outerCallee to every other child.
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

// tsCalleeBareName returns a call's bare invoked name: the identifier for
// "foo(…)", the property for "db.selectFrom(…)". Any other callee shape — a
// chained result, a computed member — returns "", best-effort.
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

// tsHarvestLiteral extracts text and line numbers from a string or
// template_string node, returning nil when it cannot. Each ${…} becomes "?", so
// the text reads as parameterised SQL.
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
			if int(ceb) <= len(src) && csb < ceb {
				textParts = append(textParts, src[csb:ceb])
			}

		case "template_substitution":
			// A value interpolation becomes a SQL parameter; an interpolated table
			// target becomes "FROM ?", which yields no refs at all.
			textParts = append(textParts, "?")

		case "escape_sequence":
			// Not content for SQL matching.

		default:
			// Delimiters and other structural tokens.
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
