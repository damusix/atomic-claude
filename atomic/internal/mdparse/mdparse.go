// Package mdparse provides goldmark-based helpers for inspecting markdown
// structure, used by the atomic-validate spec and config validators. Every
// function takes source bytes, so the package needs no filesystem access.
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
	// Start is the heading's own 1-indexed line; End is the section's last
	// line, or 0 when the section runs to the end of the file.
	Start int
	End   int
}

// InlineRef is a code span or link extracted from markdown prose (outside code blocks).
type InlineRef struct {
	Kind string // "code" | "link"
	Text string // code-span inner text, or link destination
	Line int    // 1-indexed line number in source
}

// newParser enables the GFM table extension so FindTableByHeader sees
// ast.Table nodes, and leaves Setext headings on for IsATXOnly.
func newParser() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.Table),
	)
}

// parseAST returns the document root. Its nodes hold segment offsets into src,
// so src must outlive the returned node.
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

// Sections returns heading-bounded blocks with 1-indexed line numbers. H1 and
// H2 open sections; H3+ is absorbed into the enclosing one, matching this
// project's spec layout where H2 is the bracketing level.
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
		if level > 2 {
			return ast.WalkContinue, nil
		}

		// goldmark accepts an empty ATX heading ("## " with no content) and gives
		// it no line segments; such a heading reports line 1 rather than its real
		// position. Not valid CommonMark, and absent from this project's specs.
		var startLine int
		if h.Lines().Len() > 0 {
			startLine = lineOf(src, h.Lines().At(0).Start)
		} else {
			startLine = 1
		}

		text := headingText(h, src)

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

// IsATXOnly reports whether src contains no Setext headings.
//
// Line prescan rather than AST inspection: goldmark exposes no "is setext" flag
// on Heading, and deriving line spans from segment offsets invites off-by-ones.
// Fenced blocks are skipped so embedded examples don't false-positive; indented
// blocks need no skip, since their leading space already fails isSetextUnderline.
func IsATXOnly(src []byte) bool {
	lines := bytes.Split(src, []byte{'\n'})
	inFence := false
	var fenceMarker byte // '`' or '~'
	var fenceLen int

	for i := 1; i < len(lines); i++ {
		line := lines[i]

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

// fenceOpen returns the fence character and run length, or 0,0 when line does
// not open a fenced code block. Indented fence openers, which CommonMark allows
// up to 3 spaces, are not tracked — the prescan is best-effort.
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

// isFenceClose requires ≥ fenceLen of the same marker and nothing else on the
// line but trailing whitespace.
func isFenceClose(line []byte, marker byte, fenceLen int) bool {
	n := 0
	for n < len(line) && line[n] == marker {
		n++
	}
	if n < fenceLen {
		return false
	}
	rest := bytes.TrimRight(line[n:], " \t\r")
	return len(rest) == 0
}

// isSetextUnderline matches a line of all '=' or all '-', trailing space aside.
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

// FindTableByHeader locates the first table whose header cells exactly match
// the given titles in order, returning the header row's 1-indexed line.
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

		// goldmark's GFM TableHeader holds TableCell children directly, not rows.
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

		if len(cells) != len(header) {
			return ast.WalkContinue, nil
		}
		for i, h := range header {
			if cells[i] != h {
				return ast.WalkContinue, nil
			}
		}

		// Table and TableHeader carry no segments in goldmark's GFM extension, so
		// line info comes off the cell and falls back up the chain. All empty
		// leaves lineNumber 0, which callers read as "line unknown".
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

// FindTableByRequiredColumns is FindTableByHeader relaxed to an ordered
// subsequence: required titles must appear left to right, but extra columns
// between or after them are allowed.
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

		if !isOrderedSubsequence(cells, required) {
			return ast.WalkContinue, nil
		}

		// Same line-number fallback chain as FindTableByHeader.
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

func isOrderedSubsequence(cells, required []string) bool {
	ri := 0
	for _, c := range cells {
		if ri < len(required) && c == required[ri] {
			ri++
		}
	}
	return ri == len(required)
}

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

// InlineRefs collects code spans and links, skipping code-block subtrees.
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

		switch n.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			return ast.WalkSkipChildren, nil
		}

		switch typed := n.(type) {
		case *ast.CodeSpan:
			text := codeSpanText(typed, src)
			line := codeSpanLine(typed, src)
			refs = append(refs, InlineRef{Kind: "code", Text: text, Line: line})
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

// TextSegment is a run of prose outside any code block. Line is its 1-indexed
// start; Text may span multiple lines.
type TextSegment struct {
	Text string
	Line int
}

// TextSegments extracts prose from src, skipping fenced and indented code
// blocks so the config validator's regexes don't match embedded examples.
//
// Only block-level code is skipped: a pattern inside an inline `backtick span`
// still matches. Deliberate — such spans are rare for these patterns.
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
		lineNum := i + 1

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

		// CommonMark indented code block: 4 leading spaces, or a leading tab.
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

func codeSpanText(cs *ast.CodeSpan, src []byte) string {
	var buf bytes.Buffer
	for c := cs.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			buf.Write(t.Segment.Value(src))
		}
	}
	return buf.String()
}

// codeSpanLine falls back to the parent paragraph when the span carries no
// segments of its own.
func codeSpanLine(cs *ast.CodeSpan, src []byte) int {
	for c := cs.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			return lineOf(src, t.Segment.Start)
		}
	}
	if p := cs.Parent(); p != nil {
		if p.Lines().Len() > 0 {
			return lineOf(src, p.Lines().At(0).Start)
		}
	}
	return 1
}

// linkLine falls back to the parent paragraph, since a Link is inline.
func linkLine(lnk *ast.Link, src []byte) int {
	for c := lnk.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			return lineOf(src, t.Segment.Start)
		}
	}
	if p := lnk.Parent(); p != nil {
		if p.Lines().Len() > 0 {
			return lineOf(src, p.Lines().At(0).Start)
		}
	}
	return 1
}
