package wiki

// managedregion.go — shared primitive for every code-generated region in a
// wiki markdown file (bucket doc listings, the bucket index, the member
// list).
//
// A region is delimited by an XML pseudo-tag pair (<region-name> …
// </region-name>) rather than bounded by a heading. Heading-bounded splicing
// can't tell "the next ## heading" apart from a heading the user typed inside
// the generated section, so a heading-bounded rewrite either swallows user
// prose into the generated body or clips generated content at a
// user-authored heading. A tag pair has no such ambiguity: the boundaries are
// exact strings that never occur in ordinary prose.
//
// The blank line on each side of the content is REQUIRED, not cosmetic.
// Under CommonMark, an open tag alone on its own line starts an HTML block
// that runs to the first blank line — without one, a markdown listing
// written flush against its tags renders as raw text instead of a list.
// renderManagedRegion is the only place these blank lines are emitted, so no
// call site can omit them by accident.

import (
	"errors"
	"fmt"
	"strings"
)

// errUnpairedRegion is the sentinel callers surface as "region unmanageable"
// when a tag pair is malformed (open without close, or close without open).
// Callers must report the region and move on — never truncate the document.
var errUnpairedRegion = errors.New("managed region: unpaired tag (open without close, or close without open)")

// managedRegion is one region's identity: the tag name and the content the
// caller wants inside it. content excludes the tags themselves and the
// wrapping blank lines — renderManagedRegion adds those.
type managedRegion struct {
	tag     string
	content string
}

// regionState classifies what findRegion located for a given tag.
type regionState int

const (
	regionAbsent regionState = iota
	regionWellFormed
	regionUnpaired
)

// regionBounds locates a well-formed region's tag lines within a document.
// openStart is the byte offset of the open tag line's first character;
// closeStart is the byte offset of the close tag line's first character;
// closeEnd is the byte offset immediately after the close tag text itself
// (before any trailing newline, which stays part of the preserved suffix).
type regionBounds struct {
	openStart  int
	closeStart int
	closeEnd   int
}

// renderManagedRegion wraps r's content as: open tag, blank line, content,
// blank line, close tag. No trailing newline — callers decide document-level
// newline conventions (see spliceManagedRegion's append path).
func renderManagedRegion(r managedRegion) string {
	return fmt.Sprintf("<%s>\n\n%s\n\n</%s>", r.tag, r.content, r.tag)
}

// findRegion locates the open/close tag pair for tag in document, matching
// only whole lines (a tag must be the entire line, mirroring the
// line-anchored discipline used elsewhere in this package for <wiki-buckets>
// and "## Capture surfaces"). It reports:
//
//   - regionAbsent — neither tag appears
//   - regionWellFormed — both appear, in order, first occurrence of each
//   - regionUnpaired — exactly one of the two appears (or close precedes
//     open, which cannot be spliced sensibly)
func findRegion(document, tag string) (regionState, regionBounds) {
	openLine := "<" + tag + ">"
	closeLine := "</" + tag + ">"

	openIdx := findLineAnchored(document, openLine)
	closeIdx := findLineAnchored(document, closeLine)

	if openIdx == -1 && closeIdx == -1 {
		return regionAbsent, regionBounds{}
	}
	if openIdx == -1 || closeIdx == -1 || closeIdx < openIdx {
		return regionUnpaired, regionBounds{}
	}

	return regionWellFormed, regionBounds{
		openStart:  openIdx,
		closeStart: closeIdx,
		closeEnd:   closeIdx + len(closeLine),
	}
}

// findLineAnchored returns the byte offset of the first line-anchored,
// whole-line occurrence of line in s, or -1 if absent. Line-anchored means
// line is preceded by start-of-string or '\n', and followed by '\n' or
// end-of-string — it cannot match a substring of a longer line.
func findLineAnchored(s, line string) int {
	if s == line || strings.HasPrefix(s, line+"\n") {
		return 0
	}
	if idx := strings.Index(s, "\n"+line+"\n"); idx != -1 {
		return idx + 1
	}
	if strings.HasSuffix(s, "\n"+line) {
		return len(s) - len(line)
	}
	return -1
}

// spliceManagedRegion idempotently replaces the tag-delimited region for
// r.tag in document, returning the new document. Content outside the tag
// pair is preserved byte-for-byte.
//
//   - Absent → the rendered region is appended at EOF, with a separating
//     blank line unless document already ends in one; prior content is
//     preserved.
//   - Well-formed → the body between the tags is replaced wholesale;
//     everything outside the tags is preserved.
//   - Unpaired → returns errUnpairedRegion and the document UNCHANGED. Never
//     truncated to EOF.
func spliceManagedRegion(document string, r managedRegion) (string, error) {
	state, bounds := findRegion(document, r.tag)

	switch state {
	case regionAbsent:
		return ensureTrailingBlankLine(document) + renderManagedRegion(r) + "\n", nil

	case regionWellFormed:
		before := document[:bounds.openStart]
		after := document[bounds.closeEnd:]
		return before + renderManagedRegion(r) + after, nil

	default: // regionUnpaired
		return document, fmt.Errorf("region %q: %w", r.tag, errUnpairedRegion)
	}
}

// ensureTrailingBlankLine returns s with a single trailing blank line, unless
// s is empty (nothing to separate from) or already ends in one.
func ensureTrailingBlankLine(s string) string {
	if s == "" {
		return s
	}
	if !strings.HasSuffix(s, "\n") {
		return s + "\n\n"
	}
	if !strings.HasSuffix(s, "\n\n") {
		return s + "\n"
	}
	return s
}

// ensureLeadingBlankLine returns s with a single leading blank line, unless s
// is empty or already begins with one. Mirrors ensureTrailingBlankLine for
// the opposite boundary: pad a single leading "\n" up to "\n\n"; never
// collapse an existing run of 2+ leading newlines.
func ensureLeadingBlankLine(s string) string {
	if s == "" {
		return s
	}
	if !strings.HasPrefix(s, "\n") {
		return "\n\n" + s
	}
	if !strings.HasPrefix(s, "\n\n") {
		return "\n" + s
	}
	return s
}

// spliceRegionAt replaces document[start:end) with r rendered as a managed
// region, normalizing every boundary: LEAD (before the open tag), TRAIL
// (after the close tag), and EOF. This is the single tested primitive for
// placing a region at an INTERIOR document position — the counterpart to
// spliceManagedRegion's absent-append path, which is always at EOF and only
// ever normalizes the leading side. migrateLegacyMemberMarkers (wiki.go) is
// the first caller to need an interior placement; it delegates ALL boundary
// whitespace here rather than hand-rolling it (the root cause of three
// prior rounds of one-sub-case-at-a-time patching).
//
// LEAD: bytes before <r.tag> become empty when nothing but whitespace
// precedes the span (start-of-file), or end in exactly "\n\n" otherwise — a
// single existing "\n" is padded up; 2+ existing blank lines are left alone
// (pad-if-missing, never collapse).
//
// TRAIL: bytes after </r.tag> become exactly one "\n" when nothing but
// whitespace follows the span (EOF), or begin with "\n\n" otherwise — same
// pad-if-missing, never-collapse rule.
//
// Precondition: start and end must be line-anchored (the span begins and
// ends at line starts) — the LEAD/TRAIL normalization above assumes before
// never ends mid-line and after never begins mid-line; a caller passing a
// mid-line offset gets undefined boundary whitespace.
func spliceRegionAt(document string, start, end int, r managedRegion) string {
	before := document[:start]
	after := document[end:]
	rendered := renderManagedRegion(r)

	if strings.TrimSpace(before) == "" {
		before = ""
	} else {
		before = ensureTrailingBlankLine(before)
	}

	if strings.TrimSpace(after) == "" {
		return before + rendered + "\n"
	}
	return before + rendered + ensureLeadingBlankLine(after)
}
