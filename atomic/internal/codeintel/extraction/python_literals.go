package extraction

// python_literals.go — tree-sitter-based Python string literal harvester.
//
// HarvestPythonLiterals parses a Python source file and returns all string
// literal spans with:
//   - The literal text (post-substitution for f-strings: interpolation segments
//     replaced with "?" so they act as SQL parameter placeholders).
//   - 1-based file-absolute StartLine / EndLine.
//   - IsDocstring flag: true when the string is the first expression_statement
//     in a module, class_definition body, or function_definition body — the
//     three docstring positions Python defines (PEP 257).
//
// WHY tree-sitter instead of a flat scanner: docstring exclusion requires
// structural position (first statement in a body), which a byte scanner cannot
// determine. F-string segment composition also requires the child node list.
//
// Node types verified by probe (tmp/probe-py-strings):
//   - string        — all string literals (single/double/triple, f-strings)
//   - string_start  — opening delimiter (may have f/r/b prefix, e.g. f", """)
//   - string_content — raw text segments
//   - string_end    — closing delimiter
//   - interpolation — {expr} inside f-strings
//   - expression_statement — bare expression (wraps docstrings)
//   - block         — body of function_definition / class_definition
//   - module        — top-level module node

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"
)

// PythonLiteralSpan holds one Python string literal span returned by
// HarvestPythonLiterals.
type PythonLiteralSpan struct {
	// Text is the literal content after f-string interpolation substitution.
	// For plain strings: the raw content between the delimiters.
	// For f-strings: string_content segments joined with "?" replacing each
	// interpolation segment — so `f"SELECT a FROM {t} WHERE id = {id}"`
	// becomes "SELECT a FROM ? WHERE id = ?".
	Text string
	// StartLine is the 1-based line where the opening delimiter sits.
	StartLine int
	// EndLine is the 1-based line where the closing delimiter sits.
	EndLine int
	// IsDocstring is true when this string is the first expression_statement
	// in a module, class body, or function body — the three PEP 257 docstring
	// positions. IsDocstring strings must be excluded from SQL gating.
	IsDocstring bool
}

// HarvestPythonLiterals parses src as Python via inst (which must already have
// LangPython set, or the caller should set it before calling). It returns all
// string literal spans.
//
// The caller is responsible for borrowing inst from a pool and returning it
// after this call. HarvestPythonLiterals does not borrow or return instances.
//
// Returns (nil, nil) when the source has no string literals.
func HarvestPythonLiterals(ctx context.Context, inst Instance, src string) ([]PythonLiteralSpan, error) {
	// Set language to Python — the pool instance may be set to another language
	// from a prior call in the same goroutine.
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

	// lineOffsets[i] = byte offset of the first character of line i+1.
	// Used to convert byte positions back to 1-based line numbers.
	lineOffsets := buildLineOffsets(src)

	var spans []PythonLiteralSpan
	// Walk the module's direct named children. The recursive helper tracks the
	// docstring-position contract per scope.
	if err := pyWalkNode(ctx, root, src, lineOffsets, false /* isFirstInBody */, &spans); err != nil {
		return nil, err
	}

	return spans, nil
}

// pyWalkNode recursively walks the tree rooted at node.
//
// isFirstInBody is true when node is the first named child of a block that is
// a function/class body, or the first named child of module — meaning a string
// at this position is a docstring.
func pyWalkNode(ctx context.Context, node sitter.Node, src string, lineOffsets []int, isFirstInBody bool, out *[]PythonLiteralSpan) error {
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
		*out = append(*out, *span)
		return nil // do not recurse into string children

	case "expression_statement":
		// An expression_statement wrapping a single string is the docstring
		// form. Pass isFirstInBody down to the first child so the string node
		// picks up the flag.
		cnt, _ := node.NamedChildCount(ctx)
		for i := uint64(0); i < cnt; i++ {
			child, err := node.NamedChild(ctx, i)
			if err != nil {
				continue
			}
			childKind, _ := child.Kind(ctx)
			// Only the first child and only a string node gets the docstring flag.
			childIsDocstring := isFirstInBody && i == 0 && childKind == "string"
			if err := pyWalkNode(ctx, child, src, lineOffsets, childIsDocstring, out); err != nil {
				return err
			}
		}
		return nil

	case "module":
		// Module top-level: first named child at position 0 may be a docstring.
		return pyWalkChildren(ctx, node, src, lineOffsets, true, out)

	case "block":
		// block is the body of function_definition / class_definition.
		// First named child at position 0 may be a docstring.
		return pyWalkChildren(ctx, node, src, lineOffsets, true, out)

	case "function_definition", "class_definition":
		// Descend but don't mark children here; the block child handles it.
		return pyWalkChildren(ctx, node, src, lineOffsets, false, out)

	default:
		// General descent — no docstring context.
		return pyWalkChildren(ctx, node, src, lineOffsets, false, out)
	}
}

// pyWalkChildren visits all named children of node.
// bodyDocstringEnabled: when true, the FIRST child is considered the potential
// docstring position (pass isFirstInBody=true to child 0, false to the rest).
func pyWalkChildren(ctx context.Context, node sitter.Node, src string, lineOffsets []int, bodyDocstringEnabled bool, out *[]PythonLiteralSpan) error {
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
		if err := pyWalkNode(ctx, child, src, lineOffsets, firstInBody, out); err != nil {
			return err
		}
	}
	return nil
}

// pyHarvestString extracts text and line numbers from a "string" node.
// Returns nil when the node cannot be processed.
//
// For f-strings: collects string_content segments and replaces each
// interpolation child with "?" — the substitution contract for decision 8.
// For plain strings: collects all string_content children and joins them.
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
		case "string_start":
			// Opening delimiter — no content to collect.

		case "string_content":
			// Plain content segment.
			if int(ceb) <= len(src) && csb < ceb {
				textParts = append(textParts, src[csb:ceb])
			}

		case "interpolation":
			// Substitute interpolation segment with "?" per decision 8.
			// An interpolated value placeholder becomes a SQL parameter.
			// An interpolated table target becomes "SELECT FROM ?" (no
			// recognisable identifier after FROM), yielding zero refs.
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

// pyByteToLine converts a byte offset to a 1-based line number using the
// pre-built lineOffsets table. Mirrors visitor.byteToLine in extractor.go.
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
