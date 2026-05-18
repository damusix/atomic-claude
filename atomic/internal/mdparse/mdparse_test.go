// Package mdparse_test exercises the mdparse public API from a caller's
// perspective. Each test group verifies a contract the caller relies on:
// wrong detection = validator reports false positives/negatives downstream.
package mdparse_test

import (
	"os"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/mdparse"
)

// ---------------------------------------------------------------------------
// Sections
// ---------------------------------------------------------------------------

func TestSections_TypicalDoc(t *testing.T) {
	src := []byte(`# Title

## Overview

Some overview text.

## Details

Some detail text.

### Sub-detail

Sub-detail content.
`)
	sections, err := mdparse.Sections(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect: H1 "Title", H2 "Overview", H2 "Details" (H3 is inside Details).
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d: %+v", len(sections), sections)
	}
	assertSection(t, sections[0], "Title", 1)
	assertSection(t, sections[1], "Overview", 2)
	assertSection(t, sections[2], "Details", 2)
}

func TestSections_H3NotOwnSection(t *testing.T) {
	src := []byte(`## Parent

### Child one

text

### Child two

text
`)
	sections, err := mdparse.Sections(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// H3s belong to the enclosing H2 — not their own section.
	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d: %+v", len(sections), sections)
	}
	assertSection(t, sections[0], "Parent", 2)
}

func TestSections_Empty(t *testing.T) {
	sections, err := mdparse.Sections([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sections) != 0 {
		t.Fatalf("expected 0 sections, got %d", len(sections))
	}
}

func TestSections_OnlyH1(t *testing.T) {
	src := []byte("# Just a title\n\nSome content.\n")
	sections, err := mdparse.Sections(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	assertSection(t, sections[0], "Just a title", 1)
}

func TestSections_LineNumbersIncrease(t *testing.T) {
	src := []byte("# A\n\n## B\n\n## C\n")
	sections, err := mdparse.Sections(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d", len(sections))
	}
	for i := 1; i < len(sections); i++ {
		if sections[i].Start <= sections[i-1].Start {
			t.Errorf("sections[%d].Start (%d) <= sections[%d].Start (%d): want strictly increasing",
				i, sections[i].Start, i-1, sections[i-1].Start)
		}
	}
}

func TestSections_EndLineValues(t *testing.T) {
	// "# A\n\n## B\n\n## C\n"
	// Line 1: # A
	// Line 2: (blank)
	// Line 3: ## B
	// Line 4: (blank)
	// Line 5: ## C
	//
	// Section "A" (H1) starts at 1, must end at Start("B")-1 = 2.
	// Section "B" (H2) starts at 3, must end at Start("C")-1 = 4.
	// Section "C" (H2) starts at 5, End == 0 (extends to EOF).
	//
	// CP-5 callers use End to bound section-range searches; wrong End values
	// cause false positives or missed findings in adjacent sections.
	src := []byte("# A\n\n## B\n\n## C\n")
	sections, err := mdparse.Sections(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d: %+v", len(sections), sections)
	}

	// sections[0] ("A"): End must be sections[1].Start - 1
	wantEndA := sections[1].Start - 1
	if sections[0].End != wantEndA {
		t.Errorf("sections[0].End = %d, want %d (sections[1].Start-1)", sections[0].End, wantEndA)
	}

	// sections[1] ("B"): End must be sections[2].Start - 1
	wantEndB := sections[2].Start - 1
	if sections[1].End != wantEndB {
		t.Errorf("sections[1].End = %d, want %d (sections[2].Start-1)", sections[1].End, wantEndB)
	}

	// sections[2] ("C"): End == 0 (no following section, extends to EOF)
	if sections[2].End != 0 {
		t.Errorf("sections[2].End = %d, want 0 (last section extends to EOF)", sections[2].End)
	}
}

// ---------------------------------------------------------------------------
// IsATXOnly
// ---------------------------------------------------------------------------

func TestIsATXOnly_ATXReturnsTrue(t *testing.T) {
	src := []byte("# Heading\n\n## Sub\n\ntext\n")
	if !mdparse.IsATXOnly(src) {
		t.Error("expected true for ATX-only source")
	}
}

func TestIsATXOnly_SetextH1ReturnsFalse(t *testing.T) {
	// Setext-style H1: paragraph text followed by === underline.
	src := []byte("Title\n=====\n\nSome text.\n")
	if mdparse.IsATXOnly(src) {
		t.Error("expected false for source with Setext H1")
	}
}

func TestIsATXOnly_SetextH2ReturnsFalse(t *testing.T) {
	// Setext-style H2: paragraph text followed by --- underline.
	src := []byte("Subtitle\n--------\n\nSome text.\n")
	if mdparse.IsATXOnly(src) {
		t.Error("expected false for source with Setext H2")
	}
}

func TestIsATXOnly_Empty(t *testing.T) {
	if !mdparse.IsATXOnly([]byte("")) {
		t.Error("expected true for empty source (no setext headings)")
	}
}

func TestIsATXOnly_MixedReturnsFalse(t *testing.T) {
	src := []byte("# ATX heading\n\nSetext\n------\n\ntext\n")
	if mdparse.IsATXOnly(src) {
		t.Error("expected false for mixed ATX+Setext source")
	}
}

func TestIsATXOnly_SetextWithCRLF(t *testing.T) {
	// CRLF-encoded file: the underline line ends in \r\n. isSetextUnderline must
	// strip \r before checking characters, otherwise \r != '=' causes a false-
	// negative and IsATXOnly silently returns true for a Setext heading.
	src := []byte("Title\r\n=====\r\n\r\nSome text.\r\n")
	if mdparse.IsATXOnly(src) {
		t.Error("expected false for CRLF-encoded Setext H1 (IsATXOnly must not mis-detect as ATX-only)")
	}
}

func TestIsATXOnly_SetextInsideFencedCodeBlockReturnsTrue(t *testing.T) {
	// A fenced code block (backtick fence) containing a YAML frontmatter-style
	// `---` line must NOT trigger Setext detection. The `---` is inside the
	// block and therefore not a heading underline in the document.
	src := []byte("# Title\n\n## Section\n\nHere is an example:\n\n```yaml\nkey: value\n---\nother: val\n```\n\nMore text.\n")
	if !mdparse.IsATXOnly(src) {
		t.Error("expected true: --- inside backtick fenced block must not be treated as Setext underline")
	}
}

func TestIsATXOnly_SetextInsideTildeFencedCodeBlockReturnsTrue(t *testing.T) {
	// Same but with tilde fence (~~~). The `---` inside must not trigger Setext.
	src := []byte("# Title\n\n## Section\n\n~~~yaml\nkey: value\n---\nother: val\n~~~\n\nMore text.\n")
	if !mdparse.IsATXOnly(src) {
		t.Error("expected true: --- inside tilde fenced block must not be treated as Setext underline")
	}
}

// TestIsATXOnly_SetextInsideIndentedCodeBlockReturnsTrue was deleted: the test
// passed vacuously because "    ---" starts with a space and isSetextUnderline
// returns false at trimmed[0] == ' ' before any fence-tracking logic runs. The
// indented-code short-circuit is pre-existing behavior independent of the fence
// fix. TestIsATXOnly_SetextInsideFencedCodeBlockReturnsTrue (backtick fence) and
// TestIsATXOnly_SetextWithCRLF cover the meaningful detection paths.

func TestIsATXOnly_RealRepoFiles(t *testing.T) {
	// Dogfood test: real spec files in this repo must all be ATX-only.
	// If run outside the repo tree, skip gracefully.
	paths := []string{
		"../../../docs/spec/atomic-binary.md",
		"../../../docs/spec/install-workflow.md",
		"../../../docs/spec/signals-workflow.md",
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Skipf("cannot read %s (running outside repo tree): %v", p, err)
		}
		if !mdparse.IsATXOnly(data) {
			t.Errorf("%s: IsATXOnly returned false — file contains Setext headings or false positive", p)
		}
	}
}

// ---------------------------------------------------------------------------
// FindTableByHeader
// ---------------------------------------------------------------------------

const tableDoc = `# Doc

Some text.

## Section

| Name | Type | Required? | Notes |
|------|------|-----------|-------|
| foo  | string | yes | something |
| bar  | int    | no  | else      |
`

func TestFindTableByHeader_Found(t *testing.T) {
	found, line, err := mdparse.FindTableByHeader(
		[]byte(tableDoc),
		[]string{"Name", "Type", "Required?", "Notes"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if line <= 0 {
		t.Errorf("expected positive line number, got %d", line)
	}
}

func TestFindTableByHeader_NotFound(t *testing.T) {
	found, _, err := mdparse.FindTableByHeader(
		[]byte(tableDoc),
		[]string{"Alpha", "Beta"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected found=false for missing table header")
	}
}

func TestFindTableByHeader_SimilarButWrongHeader(t *testing.T) {
	found, _, err := mdparse.FindTableByHeader(
		[]byte(tableDoc),
		[]string{"Name", "Type", "Required?", "ExtraColumn"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected found=false for header that does not exactly match")
	}
}

func TestFindTableByHeader_EmptyInput(t *testing.T) {
	found, line, err := mdparse.FindTableByHeader([]byte(""), []string{"Foo"})
	if err != nil {
		t.Fatalf("unexpected error on empty input: %v", err)
	}
	if found {
		t.Error("expected found=false on empty input")
	}
	if line != 0 {
		t.Errorf("expected line=0 on empty input, got %d", line)
	}
}

// ---------------------------------------------------------------------------
// InlineRefs
// ---------------------------------------------------------------------------

func TestInlineRefs_ExtractsCodeSpansAndLinks(t *testing.T) {
	src := []byte("Use `atomic validate` and see [docs](https://example.com).\n")
	refs, err := mdparse.InlineRefs(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var codes, links []string
	for _, r := range refs {
		switch r.Kind {
		case "code":
			codes = append(codes, r.Text)
		case "link":
			links = append(links, r.Text)
		}
	}
	if len(codes) != 1 || codes[0] != "atomic validate" {
		t.Errorf("expected code span 'atomic validate', got %v", codes)
	}
	if len(links) != 1 || links[0] != "https://example.com" {
		t.Errorf("expected link 'https://example.com', got %v", links)
	}
}

func TestInlineRefs_FencedCodeBlockNotExtracted(t *testing.T) {
	// The string "atomic validate" appears both in a fenced code block and as
	// an inline code span. Only the inline span must be extracted.
	src := []byte("Use `atomic validate` in prose.\n\n```\natomic validate --json\n```\n")
	refs, err := mdparse.InlineRefs(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var codeTexts []string
	for _, r := range refs {
		if r.Kind == "code" {
			codeTexts = append(codeTexts, r.Text)
		}
	}
	// Should find exactly one: the inline span, not the fenced block content.
	if len(codeTexts) != 1 {
		t.Errorf("expected 1 code ref, got %d: %v", len(codeTexts), codeTexts)
	}
	// Content must be the inline span text, not the fenced block body.
	if len(codeTexts) == 1 && codeTexts[0] != "atomic validate" {
		t.Errorf("expected 'atomic validate', got %q", codeTexts[0])
	}
}

func TestInlineRefs_IndentedCodeBlockNotExtracted(t *testing.T) {
	// The string "my-command" appears in an indented code block (4 spaces) and
	// as an inline code span. Only the inline span must be extracted.
	src := []byte("Use `my-command` here.\n\n    my-command --flag\n")
	refs, err := mdparse.InlineRefs(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var codeTexts []string
	for _, r := range refs {
		if r.Kind == "code" {
			codeTexts = append(codeTexts, r.Text)
		}
	}
	if len(codeTexts) != 1 {
		t.Errorf("expected 1 code ref (inline only), got %d: %v", len(codeTexts), codeTexts)
	}
	if len(codeTexts) == 1 && codeTexts[0] != "my-command" {
		t.Errorf("expected 'my-command', got %q", codeTexts[0])
	}
}

func TestInlineRefs_LineNumbersPositive(t *testing.T) {
	src := []byte("Line one\nUse `foo` here\n")
	refs, err := mdparse.InlineRefs(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range refs {
		if r.Line <= 0 {
			t.Errorf("expected positive line, got %d for ref %+v", r.Line, r)
		}
	}
}

func TestInlineRefs_Empty(t *testing.T) {
	refs, err := mdparse.InlineRefs([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs, got %d", len(refs))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertSection(t *testing.T, s mdparse.Section, heading string, level int) {
	t.Helper()
	if s.Heading != heading {
		t.Errorf("expected heading %q, got %q", heading, s.Heading)
	}
	if s.Level != level {
		t.Errorf("expected level %d, got %d for section %q", level, s.Level, s.Heading)
	}
	if s.Start <= 0 {
		t.Errorf("expected Start > 0, got %d for section %q", s.Start, s.Heading)
	}
}
