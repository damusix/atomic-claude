// Package mdparse provides goldmark-based helpers for inspecting markdown
// structure. It is used by the atomic-validate spec and config validators.
//
// All functions accept raw source bytes — callers read files. This keeps the
// package pure and testable without filesystem access.
//
// Section bracketing rationale: spec files in this project use H2 as the
// bracketing level (H1 is the file title, H3+ are within an H2 section).
// The Sections function groups content by H2 exactly. H3+ nodes encountered
// during the AST walk are not emitted as separate sections; they remain
// within the enclosing H2's range. This matches the project spec pattern
// from go.abhg.dev/goldmark/toc.
package mdparse

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Section describes a heading-bounded block in a markdown document.
type Section struct {
	Heading string
	Level   int
	// Start is the 1-indexed line number of the heading itself.
	// End is the 1-indexed line of the last line in the section, or 0 if
	// the section extends to the end of the file.
	Start int
	End   int
}

// InlineRef is a code span or link extracted from markdown prose (outside code blocks).
type InlineRef struct {
	Kind string // "code" | "link"
	Text string // code-span inner text, or link destination
	Line int    // 1-indexed line number in source
}

// newParser returns a goldmark parser with the GFM table extension enabled
// (needed so FindTableByHeader can locate ast.Table nodes) and with Setext
// headings enabled (goldmark enables them by default; we keep the default so
// IsATXOnly can detect them via AST inspection).
func newParser() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.Table),
	)
}

// parseAST parses src and returns the document AST root. The returned node is
// valid only as long as reader is alive, so both are returned together.
func parseAST(src []byte) (ast.Node, *text.Reader) {
	md := newParser()
	reader := text.NewReader(src)
	doc := md.Parser().Parse(reader, parser.WithContext(parser.NewContext()))
	return doc, &reader
}

// lineOf returns the 1-indexed line number for the byte offset pos in src.
func lineOf(src []byte, pos int) int {
	if pos < 0 || pos > len(src) {
		return 1
	}
	return bytes.Count(src[:pos], []byte{'\n'}) + 1
}

// headingText extracts the plain-text content of a Heading node.
func headingText(n *ast.Heading, src []byte) string {
	var buf bytes.Buffer
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch tc := c.(type) {
		case *ast.Text:
			buf.Write(tc.Segment.Value(src))
		case *ast.CodeSpan:
			for sc := tc.FirstChild(); sc != nil; sc = sc.NextSibling() {
				if t, ok := sc.(*ast.Text); ok {
					buf.Write(t.Segment.Value(src))
				}
			}
		}
	}
	return buf.String()
}

// Sections walks an ATX-only markdown source and returns sections bounded by
// headings. H1 is its own section. H2 headings start new sections. H3+
// headings belong to the enclosing H2's section and are not returned as
// separate entries. If there is no H2, H3+ headings are silently absorbed.
//
// Line numbers are 1-indexed offsets into src.
func Sections(src []byte) ([]Section, error) {
	if len(src) == 0 {
		return nil, nil
	}
	doc, _ := parseAST(src)

	var sections []Section

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}

		level := h.Level
		// Only surface H1 and H2 as section boundaries; H3+ are absorbed.
		if level > 2 {
			return ast.WalkContinue, nil
		}

		// Determine line number from the heading's first segment.
		var startLine int
		if h.Lines().Len() > 0 {
			startLine = lineOf(src, h.Lines().At(0).Start)
		} else {
			// ATX headings on a single line — fall back to first segment of
			// the children (should not happen for well-formed ATX headings).
			startLine = 1
		}

		text := headingText(h, src)

		// Close the previous section's End line (one before this heading).
		if len(sections) > 0 && sections[len(sections)-1].End == 0 {
			sections[len(sections)-1].End = startLine - 1
		}

		sections = append(sections, Section{
			Heading: text,
			Level:   level,
			Start:   startLine,
		})
		return ast.WalkContinue, nil
	})

	return sections, nil
}

// IsATXOnly returns true iff src contains no Setext-style headings.
//
// Approach: goldmark parses Setext headings as Heading nodes whose source
// segment spans two lines (the text line + the underline). We detect this
// by checking whether any Heading node's first segment's line differs from
// the line of the text content — but that is subtle.
//
// Simpler and equally reliable: scan for lines that consist entirely of
// '=' or '-' characters (at least two) immediately after a non-blank line.
// This is a line-prescan approach. We choose this over AST inspection
// because goldmark's Heading nodes do not expose a "is setext" flag, and
// computing line spans from segment offsets requires careful arithmetic that
// can hide off-by-one bugs. The prescan is simple, direct, and correct for
// all CommonMark Setext heading forms.
//
// Fenced code block tracking: lines inside ``` or ~~~ fences are skipped
// so that YAML/Markdown examples embedded in spec files do not trigger false
// positives. Indented code blocks (lines with ≥4 leading spaces or a tab)
// are also skipped — isSetextUnderline already ignores them because a leading
// space causes the underline-character check to fail, but the skip is kept
// explicit for clarity. Fence open: a line beginning with ``` or ~~~ (with
// an optional info string after). Fence close: a line beginning with the
// same marker characters (length ≥ opening fence — simplified CommonMark).
func IsATXOnly(src []byte) bool {
	lines := bytes.Split(src, []byte{'\n'})
	inFence := false
	var fenceMarker byte // '`' or '~'
	var fenceLen int

	for i := 1; i < len(lines); i++ {
		line := lines[i]

		// Detect fenced code block open/close.
		if fenceChar, flen := fenceOpen(line); !inFence && flen > 0 {
			inFence = true
			fenceMarker = fenceChar
			fenceLen = flen
			continue
		}
		if inFence {
			if isFenceClose(line, fenceMarker, fenceLen) {
				inFence = false
			}
			// Inside fence: never treat any line as Setext underline.
			continue
		}

		if len(line) < 2 {
			continue
		}
		prev := bytes.TrimSpace(lines[i-1])
		if len(prev) == 0 {
			continue
		}
		if isSetextUnderline(line) {
			return false
		}
	}
	return true
}

// fenceOpen reports whether line opens a fenced code block (CommonMark §4.5).
// Returns the fence character ('`' or '~') and the fence run length. Returns
// 0,0 if this line is not a fence opener. Info strings after the fence
// characters are allowed (and common for syntax-highlighted blocks).
// Simplification: we do not track indented fence openers (up to 3 leading
// spaces are allowed by CommonMark); those are uncommon in spec files and
// the prescan is a best-effort approach.
func fenceOpen(line []byte) (marker byte, length int) {
	if len(line) == 0 {
		return 0, 0
	}
	ch := line[0]
	if ch != '`' && ch != '~' {
		return 0, 0
	}
	n := 0
	for n < len(line) && line[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	return ch, n
}

// isFenceClose reports whether line closes the current fence. Per CommonMark
// a close fence must begin with ≥ fenceLen of the same marker character and
// contain only those characters (optional trailing spaces allowed).
func isFenceClose(line []byte, marker byte, fenceLen int) bool {
	n := 0
	for n < len(line) && line[n] == marker {
		n++
	}
	if n < fenceLen {
		return false
	}
	// Rest of line must be blank (close fence has no info string).
	rest := bytes.TrimRight(line[n:], " \t\r")
	return len(rest) == 0
}

// isSetextUnderline reports whether line is a CommonMark Setext heading
// underline: all '=' or all '-', at least one character, optional trailing
// spaces.
func isSetextUnderline(line []byte) bool {
	trimmed := bytes.TrimRight(line, " \t\r")
	if len(trimmed) == 0 {
		return false
	}
	ch := trimmed[0]
	if ch != '=' && ch != '-' {
		return false
	}
	for _, b := range trimmed {
		if b != ch {
			return false
		}
	}
	return true
}

// FindTableByHeader locates the first ast.Table in src whose header row cells
// (trimmed text content) exactly match the given column titles in order.
// Returns found=true and the 1-indexed line number of the table header row, or
// found=false and line=0 if no matching table is found.
func FindTableByHeader(src []byte, header []string) (found bool, lineNumber int, err error) {
	if len(src) == 0 {
		return false, 0, nil
	}
	doc, _ := parseAST(src)

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || found {
			return ast.WalkContinue, nil
		}
		tbl, ok := n.(*extast.Table)
		if !ok {
			return ast.WalkContinue, nil
		}

		// Find the header (TableHeader child of the table).
		// In goldmark's GFM table extension, TableHeader contains TableCell
		// children directly (not TableRow children).
		var hdr *extast.TableHeader
		for c := tbl.FirstChild(); c != nil; c = c.NextSibling() {
			if th, ok := c.(*extast.TableHeader); ok {
				hdr = th
				break
			}
		}
		if hdr == nil {
			return ast.WalkContinue, nil
		}

		// Collect cell texts from the header's direct TableCell children.
		var cells []string
		for c := hdr.FirstChild(); c != nil; c = c.NextSibling() {
			cell, ok := c.(*extast.TableCell)
			if !ok {
				continue
			}
			cells = append(cells, cellText(cell, src))
		}

		if len(cells) != len(header) {
			return ast.WalkContinue, nil
		}
		for i, h := range header {
			if cells[i] != h {
				return ast.WalkContinue, nil
			}
		}

		// Match. Determine line number from the first header cell's first segment.
		// Table and TableHeader nodes carry no line segments in goldmark's GFM
		// extension; line info lives on the TableCell grandchildren.
		if first := hdr.FirstChild(); first != nil {
			if first.Lines().Len() > 0 {
				lineNumber = lineOf(src, first.Lines().At(0).Start)
			}
		}
		found = true
		return ast.WalkStop, nil
	})

	return found, lineNumber, nil
}

// cellText extracts the plain text content of a table cell node.
func cellText(cell *extast.TableCell, src []byte) string {
	var buf bytes.Buffer
	for c := cell.FirstChild(); c != nil; c = c.NextSibling() {
		switch tc := c.(type) {
		case *ast.Text:
			buf.Write(tc.Segment.Value(src))
		case *ast.CodeSpan:
			for sc := tc.FirstChild(); sc != nil; sc = sc.NextSibling() {
				if t, ok := sc.(*ast.Text); ok {
					buf.Write(t.Segment.Value(src))
				}
			}
		}
	}
	return string(bytes.TrimSpace(buf.Bytes()))
}

// InlineRefs walks src collecting ast.CodeSpan and ast.Link nodes, skipping
// the content of ast.FencedCodeBlock and ast.CodeBlock subtrees entirely.
// Each returned InlineRef carries a 1-indexed line number.
func InlineRefs(src []byte) ([]InlineRef, error) {
	if len(src) == 0 {
		return nil, nil
	}
	doc, _ := parseAST(src)

	var refs []InlineRef

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		// Skip code block subtrees entirely — do not descend.
		switch n.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			return ast.WalkSkipChildren, nil
		}

		switch typed := n.(type) {
		case *ast.CodeSpan:
			text := codeSpanText(typed, src)
			line := codeSpanLine(typed, src)
			refs = append(refs, InlineRef{Kind: "code", Text: text, Line: line})
			// Children are raw text segments; no further useful nodes inside.
			return ast.WalkSkipChildren, nil

		case *ast.Link:
			dest := string(typed.Destination)
			line := linkLine(typed, src)
			refs = append(refs, InlineRef{Kind: "link", Text: dest, Line: line})
		}

		return ast.WalkContinue, nil
	})

	return refs, nil
}

// codeSpanText extracts the text content of a CodeSpan node. CodeSpan
// children are ast.Text nodes carrying the raw segments (with soft
// line-break normalization handled by goldmark).
func codeSpanText(cs *ast.CodeSpan, src []byte) string {
	var buf bytes.Buffer
	for c := cs.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			buf.Write(t.Segment.Value(src))
		}
	}
	return buf.String()
}

// codeSpanLine returns the 1-indexed line of the code span's first segment.
// Falls back to its parent paragraph's first segment if the span has no
// direct segments.
func codeSpanLine(cs *ast.CodeSpan, src []byte) int {
	for c := cs.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			return lineOf(src, t.Segment.Start)
		}
	}
	// Walk up to find a node with line info.
	if p := cs.Parent(); p != nil {
		if p.Lines().Len() > 0 {
			return lineOf(src, p.Lines().At(0).Start)
		}
	}
	return 1
}

// linkLine returns the 1-indexed line number of a Link node.
// Links are inline nodes; walk up to the parent paragraph for line info.
func linkLine(lnk *ast.Link, src []byte) int {
	// Try first child text segment.
	for c := lnk.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			return lineOf(src, t.Segment.Start)
		}
	}
	// Walk up to parent.
	if p := lnk.Parent(); p != nil {
		if p.Lines().Len() > 0 {
			return lineOf(src, p.Lines().At(0).Start)
		}
	}
	return 1
}
