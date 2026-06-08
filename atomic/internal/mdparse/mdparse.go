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

// parseAST parses src and returns the document AST root. The reader is kept
// alive inside this function for the duration of the parse; the returned node
// holds segment offsets into src (not into the reader), so src must remain
// valid for the lifetime of the returned node.
func parseAST(src []byte) ast.Node {
	md := newParser()
	reader := text.NewReader(src)
	return md.Parser().Parse(reader, parser.WithContext(parser.NewContext()))
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
	doc := parseAST(src)

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
		// Known limitation: an empty ATX heading (e.g. "## " with no content)
		// causes goldmark to produce a Heading node with Lines().Len() == 0.
		// In that case startLine falls back to 1. For non-first empty headings
		// this is a silent wrong line number. Empty ATX headings are not valid
		// CommonMark (a heading marker without content is not a heading), but
		// goldmark accepts them. Real spec files in this project never use empty
		// headings, so this limitation is documented but not fixed.
		var startLine int
		if h.Lines().Len() > 0 {
			startLine = lineOf(src, h.Lines().At(0).Start)
		} else {
			// Empty heading or unexpected goldmark behavior — fall back to 1.
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
// positives. Fence open: a line beginning with ``` or ~~~ (with an optional
// info string after). Fence close: a line beginning with the same marker
// characters (length ≥ opening fence — simplified CommonMark).
//
// Indented code blocks pass through isSetextUnderline harmlessly — a leading
// space character fails the '='/'−' only check before any Setext logic runs.
// No explicit skip is needed for them.
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
	doc := parseAST(src)

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
		// extension; line info typically lives on the TableCell grandchildren.
		//
		// Goldmark version assumption: goldmark v1.4+ populates TableCell.Lines()
		// for GFM tables. If a future or alternate goldmark version returns an
		// empty Lines() slice on the cell, we fall back progressively:
		//   1. hdr.FirstChild().Lines() — the first TableCell
		//   2. hdr.Lines()              — the TableHeader row
		//   3. tbl.Lines()              — the whole table node
		// If all three return empty, lineNumber stays 0 and found=true is still
		// returned — callers treat lineNumber=0 as "line unknown" and report
		// findings without a precise line reference.
		if first := hdr.FirstChild(); first != nil {
			if first.Lines().Len() > 0 {
				lineNumber = lineOf(src, first.Lines().At(0).Start)
			} else if hdr.Lines().Len() > 0 {
				lineNumber = lineOf(src, hdr.Lines().At(0).Start)
			} else if tbl.Lines().Len() > 0 {
				lineNumber = lineOf(src, tbl.Lines().At(0).Start)
			}
			// If all three are empty, lineNumber remains 0 (unknown).
		}
		found = true
		return ast.WalkStop, nil
	})

	return found, lineNumber, nil
}

// FindTableByRequiredColumns locates the first ast.Table in src whose header
// row contains all of the required column titles as an ordered subsequence.
// "Ordered subsequence" means the required columns appear among the actual
// header cells in the same left-to-right order, with zero or more extra
// columns allowed between or after them.
//
// Returns found=true and the 1-indexed line number of the table header row, or
// found=false and line=0 if no matching table is found.
//
// Unlike FindTableByHeader (exact match), this function accepts tables that
// have additional columns (e.g. the canonical 6-column Checkpoints header
// emitted by /atomic-plan passes when required = ["#","Checkpoint","Files/areas","Verifies"]).
func FindTableByRequiredColumns(src []byte, required []string) (found bool, lineNumber int, err error) {
	if len(src) == 0 {
		return false, 0, nil
	}
	doc := parseAST(src)

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || found {
			return ast.WalkContinue, nil
		}
		tbl, ok := n.(*extast.Table)
		if !ok {
			return ast.WalkContinue, nil
		}

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

		var cells []string
		for c := hdr.FirstChild(); c != nil; c = c.NextSibling() {
			cell, ok := c.(*extast.TableCell)
			if !ok {
				continue
			}
			cells = append(cells, cellText(cell, src))
		}

		// Check that required is an ordered subsequence of cells.
		if !isOrderedSubsequence(cells, required) {
			return ast.WalkContinue, nil
		}

		// Match. Determine line number using the same fallback chain as FindTableByHeader.
		if first := hdr.FirstChild(); first != nil {
			if first.Lines().Len() > 0 {
				lineNumber = lineOf(src, first.Lines().At(0).Start)
			} else if hdr.Lines().Len() > 0 {
				lineNumber = lineOf(src, hdr.Lines().At(0).Start)
			} else if tbl.Lines().Len() > 0 {
				lineNumber = lineOf(src, tbl.Lines().At(0).Start)
			}
		}
		found = true
		return ast.WalkStop, nil
	})

	return found, lineNumber, nil
}

// isOrderedSubsequence reports whether required is an ordered subsequence of
// cells: every element of required appears in cells in the same left-to-right
// order (extra elements between or around them are allowed).
func isOrderedSubsequence(cells, required []string) bool {
	ri := 0
	for _, c := range cells {
		if ri < len(required) && c == required[ri] {
			ri++
		}
	}
	return ri == len(required)
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
	doc := parseAST(src)

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

// TextSegment is a contiguous run of plain text extracted from a markdown
// document, outside of fenced code blocks and indented code blocks. Line is
// the 1-indexed start line of the segment in the original source. Text is the
// raw content of the segment (may span multiple lines).
type TextSegment struct {
	Text string
	Line int
}

// TextSegments extracts prose text from src, skipping fenced code blocks
// (``` ... ``` and ~~~ ... ~~~) and indented code blocks. Used by the config
// validator to regex for @-refs and subagent_type: "..." literals without
// matching documentation examples embedded in code blocks.
//
// Limitation: inline backtick code spans are NOT excluded. A pattern like
// @.claude/foo.md inside a `backtick span` will still match reAtRef (C5) or
// reSubagentType (C3). Only block-level code is skipped. This matches the
// spec's stated scope and is intentional — inline spans in prose are
// uncommon for these patterns. Callers at C3/C5 are aware of this scope.
//
// Implementation: line-by-line prescan (same strategy as IsATXOnly). Prose
// lines are accumulated into segments; a new segment starts after each
// code-block skip. Each segment carries the 1-indexed line number of its first
// line. This is simpler and sufficient for the regex patterns C3/C5 require;
// the goldmark AST approach would also work but requires more plumbing for
// paragraph-level line recovery.
func TextSegments(src []byte) []TextSegment {
	lines := bytes.Split(src, []byte{'\n'})
	var segments []TextSegment

	inFence := false
	var fenceMarker byte
	var fenceLen int

	var segLines [][]byte
	segStartLine := 1

	flush := func(nextLine int) {
		if len(segLines) > 0 {
			segments = append(segments, TextSegment{
				Text: string(bytes.Join(segLines, []byte{'\n'})),
				Line: segStartLine,
			})
			segLines = nil
		}
		segStartLine = nextLine
	}

	for i, raw := range lines {
		lineNum := i + 1 // 1-indexed

		if fenceChar, flen := fenceOpen(raw); !inFence && flen > 0 {
			flush(lineNum)
			inFence = true
			fenceMarker = fenceChar
			fenceLen = flen
			segStartLine = lineNum + 1
			continue
		}
		if inFence {
			if isFenceClose(raw, fenceMarker, fenceLen) {
				inFence = false
				segStartLine = lineNum + 1
			}
			continue
		}

		// Skip indented code blocks (4+ leading spaces or a leading tab).
		// CommonMark: indented code blocks require 4 spaces of indentation.
		if len(raw) >= 4 && raw[0] == ' ' && raw[1] == ' ' && raw[2] == ' ' && raw[3] == ' ' {
			flush(lineNum)
			segStartLine = lineNum + 1
			continue
		}
		if len(raw) > 0 && raw[0] == '\t' {
			flush(lineNum)
			segStartLine = lineNum + 1
			continue
		}

		segLines = append(segLines, raw)
	}
	flush(len(lines) + 1)
	return segments
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
