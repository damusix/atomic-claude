package extraction

// Tree-sitter Python string-literal harvester. tree-sitter rather than a byte
// scanner because docstring exclusion needs structural position — first statement
// in a body — and f-string composition needs the child node list.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"
)

// PythonLiteralSpan is one string literal returned by HarvestPythonLiterals.
type PythonLiteralSpan struct {
	// Text is the content between the delimiters; in an f-string each
	// interpolation is replaced by "?" so it reads as a SQL parameter.
	Text string
	// StartLine and EndLine are 1-based.
	StartLine int
	EndLine   int
	// IsDocstring marks the three PEP 257 positions — first statement of a
	// module, class body, or function body. These are excluded from SQL gating.
	IsDocstring bool
	// CalleeExpr is the bare name of the nearest enclosing call ("select" for
	// db.select("x")), empty when the literal sits outside any call.
	CalleeExpr string
}

// HarvestPythonLiterals returns every string literal span in src. The caller owns
// inst: borrow it from a pool and return it afterwards. (nil, nil) means the
// source has no string literals.
func HarvestPythonLiterals(ctx context.Context, inst Instance, src string) ([]PythonLiteralSpan, error) {
	// The pooled instance may still be set to another language.
	if err := inst.SetLanguage(ctx, LangPython); err != nil {
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

	var spans []PythonLiteralSpan
	if err := pyWalkNode(ctx, root, src, lineOffsets, false /* isFirstInBody */, "", &spans); err != nil {
		return nil, err
	}

	return spans, nil
}

// pyWalkNode walks the tree rooted at node. isFirstInBody marks the docstring
// position: first named child of a module or of a function/class body. calleeCtx
// is the nearest enclosing call's bare callee, "" outside any call.
func pyWalkNode(ctx context.Context, node sitter.Node, src string, lineOffsets []int, isFirstInBody bool, calleeCtx string, out *[]PythonLiteralSpan) error {
	kind, err := node.Kind(ctx)
	if err != nil {
		return nil // best-effort: Kind() failed — skip this subtree entirely
	}

	switch kind {
	case "string":
		span, err := pyHarvestString(ctx, node, src, lineOffsets)
		if err != nil || span == nil {
			return nil // best-effort: harvest failed or empty literal — skip this string node
		}
		span.IsDocstring = isFirstInBody
		span.CalleeExpr = calleeCtx
		*out = append(*out, *span)
		return nil // do not recurse into string children

	case "expression_statement":
		// A string wrapped in an expression_statement is the docstring form, so
		// isFirstInBody has to reach the string child itself.
		cnt, _ := node.NamedChildCount(ctx)
		for i := uint64(0); i < cnt; i++ {
			child, err := node.NamedChild(ctx, i)
			if err != nil {
				continue
			}
			childKind, _ := child.Kind(ctx)
			childIsDocstring := isFirstInBody && i == 0 && childKind == "string"
			if err := pyWalkNode(ctx, child, src, lineOffsets, childIsDocstring, calleeCtx, out); err != nil {
				return err
			}
		}
		return nil

	case "call":
		// Scope the new callee to the "arguments" subtree only: a literal in
		// callee position ("tbl".upper()) must not inherit this call's own callee.
		// A deeper call overwrites it again for its own arguments.
		newCallee := pyCalleeBareName(ctx, node, src)
		return pyWalkCallChildren(ctx, node, src, lineOffsets, calleeCtx, newCallee, out)

	case "module":
		return pyWalkChildren(ctx, node, src, lineOffsets, true, calleeCtx, out)

	case "block":
		// A block is a function or class body.
		return pyWalkChildren(ctx, node, src, lineOffsets, true, calleeCtx, out)

	case "function_definition", "class_definition":
		// The block child owns the docstring position, not this node.
		return pyWalkChildren(ctx, node, src, lineOffsets, false, calleeCtx, out)

	default:
		return pyWalkChildren(ctx, node, src, lineOffsets, false, calleeCtx, out)
	}
}

// pyWalkChildren visits all named children. With bodyDocstringEnabled the first
// child is the potential docstring position.
func pyWalkChildren(ctx context.Context, node sitter.Node, src string, lineOffsets []int, bodyDocstringEnabled bool, calleeCtx string, out *[]PythonLiteralSpan) error {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return nil
	}
	for i := uint64(0); i < cnt; i++ {
		child, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		firstInBody := bodyDocstringEnabled && i == 0
		if err := pyWalkNode(ctx, child, src, lineOffsets, firstInBody, calleeCtx, out); err != nil {
			return err
		}
	}
	return nil
}

// pyWalkCallChildren passes argsCallee to the "arguments" subtree and
// outerCallee to every other child.
func pyWalkCallChildren(ctx context.Context, node sitter.Node, src string, lineOffsets []int, outerCallee, argsCallee string, out *[]PythonLiteralSpan) error {
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
		if err := pyWalkNode(ctx, child, src, lineOffsets, false, calleeCtx, out); err != nil {
			return err
		}
	}
	return nil
}

// pyCalleeBareName returns a call's bare invoked name: the identifier for
// "select(…)", the attribute for "db.select(…)". Any other callee shape returns
// "", matching this harvester's best-effort failure policy.
func pyCalleeBareName(ctx context.Context, callNode sitter.Node, src string) string {
	fn, err := callNode.ChildByFieldName(ctx, "function")
	if err != nil {
		return ""
	}
	kind, err := fn.Kind(ctx)
	if err != nil {
		return ""
	}
	switch kind {
	case "identifier":
		return pyNodeText(ctx, fn, src)
	case "attribute":
		attr, err := fn.ChildByFieldName(ctx, "attribute")
		if err != nil {
			return ""
		}
		return pyNodeText(ctx, attr, src)
	default:
		return ""
	}
}

// pyNodeText returns the raw source text spanned by node, or "" on failure.
func pyNodeText(ctx context.Context, node sitter.Node, src string) string {
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

// pyHarvestString extracts text and line numbers from a "string" node, returning
// nil when it cannot. Each f-string interpolation becomes "?", so the text reads
// as parameterised SQL.
func pyHarvestString(ctx context.Context, node sitter.Node, src string, lineOffsets []int) (*PythonLiteralSpan, error) {
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
		case "string_start":
			// Delimiter, no content.

		case "string_content":
			if int(ceb) <= len(src) && csb < ceb {
				textParts = append(textParts, src[csb:ceb])
			}

		case "interpolation":
			// A value interpolation becomes a SQL parameter; an interpolated table
			// target becomes "FROM ?", which yields no refs at all.
			textParts = append(textParts, "?")

		case "string_end":
			// Delimiter, no content.
		}
	}

	text := strings.Join(textParts, "")
	if text == "" {
		return nil, nil
	}

	return &PythonLiteralSpan{
		Text:      text,
		StartLine: startLine,
		EndLine:   endLine,
	}, nil
}

// pyByteToLine mirrors visitor.byteToLine in extractor.go.
func pyByteToLine(lineOffsets []int, byteOffset uint64) int {
	off := int(byteOffset)
	lo, hi := 0, len(lineOffsets)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if lineOffsets[mid] <= off {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return hi + 1 // hi is the last index where lineOffsets[hi] <= off; +1 → 1-based
}
