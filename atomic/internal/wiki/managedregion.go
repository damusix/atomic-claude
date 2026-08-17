package wiki

// Shared primitive for every code-generated region in a wiki markdown file.
//
// Regions are delimited by an XML pseudo-tag pair rather than bounded by a
// heading: heading-bounded splicing cannot tell "the next ## heading" from one
// the user typed inside the generated section, so it either swallows user
// prose or clips generated content. Tag lines never occur in ordinary prose.
//
// The blank line on each side of the content is required, not cosmetic. Under
// CommonMark a lone open tag starts an HTML block running to the first blank
// line, so a listing flush against its tags renders as raw text.
// renderManagedRegion is the only emitter, so no call site can forget them.

import (
	"errors"
	"fmt"
	"strings"
)

// errUnpairedRegion means the tag pair is malformed. Callers report it and
// move on; they must never truncate the document to "repair" it.
var errUnpairedRegion = errors.New("managed region: unpaired tag (open without close, or close without open)")

// managedRegion is a tag name plus the content to go inside it, excluding the
// tags and their wrapping blank lines.
type managedRegion struct {
	tag     string
	content string
}

type regionState int

const (
	regionAbsent regionState = iota
	regionWellFormed
	regionUnpaired
)

// regionBounds are byte offsets of a well-formed region's tag lines. closeEnd
// sits just past the close tag text, so its trailing newline stays part of the
// preserved suffix.
type regionBounds struct {
	openStart  int
	closeStart int
	closeEnd   int
}

// renderManagedRegion emits no trailing newline; document-level newline
// conventions are the caller's.
func renderManagedRegion(r managedRegion) string {
	return fmt.Sprintf("<%s>\n\n%s\n\n</%s>", r.tag, r.content, r.tag)
}

// findRegion matches the first whole-line occurrence of each tag. A lone tag,
// or a close before its open, is regionUnpaired — there is no sensible splice.
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

// findLineAnchored returns the offset of the first whole-line occurrence of
// line in s, or -1. It cannot match a substring of a longer line.
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

// spliceManagedRegion idempotently replaces r.tag's region, preserving
// everything outside the tag pair byte-for-byte. An absent region is appended
// at EOF; an unpaired one returns errUnpairedRegion with the document
// unchanged, never truncated.
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

// ensureTrailingBlankLine pads up to one trailing blank line; an existing run
// of 2+ newlines is left alone.
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

// ensureLeadingBlankLine mirrors ensureTrailingBlankLine at the other boundary.
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

// spliceRegionAt places a region at an interior position, the counterpart to
// spliceManagedRegion's EOF-only append path. It is the single home for
// boundary whitespace: callers delegate all of it here rather than hand-rolling
// one sub-case at a time.
//
// Both boundaries collapse to nothing at start-of-file and to a single "\n" at
// EOF; otherwise they are padded to a blank line. Padding never collapses an
// existing run of 2+ newlines.
//
// Precondition: start and end must sit at line starts. A mid-line offset
// yields undefined boundary whitespace.
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
