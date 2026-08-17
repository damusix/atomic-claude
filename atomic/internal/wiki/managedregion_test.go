package wiki

import (
	"errors"
	"testing"
)

// TestRenderManagedRegion_Shape verifies the wrapper shape: open tag, blank
// line, content, blank line, close tag. The blank lines are load-bearing
// (CommonMark HTML-block rule) and must never be omitted by a call site.
func TestRenderManagedRegion_Shape(t *testing.T) {
	got := renderManagedRegion(managedRegion{tag: "foo", content: "- a\n- b"})
	want := "<foo>\n\n- a\n- b\n\n</foo>"
	if got != want {
		t.Errorf("renderManagedRegion shape mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

// TestSpliceManagedRegion_AbsentAppendsAtEOF verifies that when the tag is
// absent from an empty document, the rendered region is appended with a
// trailing newline and nothing else.
func TestSpliceManagedRegion_AbsentAppendsAtEOF(t *testing.T) {
	got, err := spliceManagedRegion("", managedRegion{tag: "docs", content: "entry"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "<docs>\n\nentry\n\n</docs>\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSpliceManagedRegion_AbsentAppendsAfterPriorContent verifies that prior
// content is preserved byte-for-byte and a separating blank line is added
// before the appended region when the prior content doesn't already end in one.
func TestSpliceManagedRegion_AbsentAppendsAfterPriorContent(t *testing.T) {
	prior := "# Realm\n\nSome prose the user wrote."
	got, err := spliceManagedRegion(prior, managedRegion{tag: "docs", content: "entry"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := prior + "\n\n<docs>\n\nentry\n\n</docs>\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSpliceManagedRegion_AbsentNoDoubleBlankLine(t *testing.T) {
	prior := "# Realm\n\nSome prose.\n\n"
	got, err := spliceManagedRegion(prior, managedRegion{tag: "docs", content: "entry"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := prior + "<docs>\n\nentry\n\n</docs>\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSpliceManagedRegion_WellFormedReplacesBody verifies that an existing
// well-formed region has its body replaced wholesale, with content outside
// the tag pair preserved byte-for-byte.
func TestSpliceManagedRegion_WellFormedReplacesBody(t *testing.T) {
	original := "# Realm\n\nBefore prose.\n\n<docs>\n\nold entry\n\n</docs>\n\nAfter prose.\n"
	got, err := spliceManagedRegion(original, managedRegion{tag: "docs", content: "new entry"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "# Realm\n\nBefore prose.\n\n<docs>\n\nnew entry\n\n</docs>\n\nAfter prose.\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSpliceManagedRegion_PreservesOutsideContentByteForByte types prose
// before, between (a second, untouched region), and after the target region,
// asserting only the target region's body moved.
func TestSpliceManagedRegion_PreservesOutsideContentByteForByte(t *testing.T) {
	original := "" +
		"# Realm\n\n" +
		"Intro prose the user hand-wrote.\n\n" +
		"<wiki-bucket-list>\n\n" +
		"- [research](../research) - research notes\n\n" +
		"</wiki-bucket-list>\n\n" +
		"## Extra heading the user added\n\n" +
		"<wiki-member-list>\n\n" +
		"- old-member\n\n" +
		"</wiki-member-list>\n\n" +
		"Trailing prose after everything.\n"

	got, err := spliceManagedRegion(original, managedRegion{
		tag:     "wiki-member-list",
		content: "- new-member",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "" +
		"# Realm\n\n" +
		"Intro prose the user hand-wrote.\n\n" +
		"<wiki-bucket-list>\n\n" +
		"- [research](../research) - research notes\n\n" +
		"</wiki-bucket-list>\n\n" +
		"## Extra heading the user added\n\n" +
		"<wiki-member-list>\n\n" +
		"- new-member\n\n" +
		"</wiki-member-list>\n\n" +
		"Trailing prose after everything.\n"

	if got != want {
		t.Errorf("outside content not preserved byte-for-byte\ngot:  %q\nwant: %q", got, want)
	}
}

// TestSpliceManagedRegion_UnpairedOpenReturnsError verifies an open tag with
// no matching close returns errUnpairedRegion and leaves the document unchanged
// — never truncated to EOF.
func TestSpliceManagedRegion_UnpairedOpenReturnsError(t *testing.T) {
	original := "# Realm\n\n<docs>\n\nsome stray content the user typed\n"
	got, err := spliceManagedRegion(original, managedRegion{tag: "docs", content: "new"})
	if !errors.Is(err, errUnpairedRegion) {
		t.Fatalf("expected errUnpairedRegion, got %v", err)
	}
	if got != original {
		t.Errorf("document must be left unchanged on unpaired-open; got %q, want %q", got, original)
	}
}

func TestSpliceManagedRegion_UnpairedCloseReturnsError(t *testing.T) {
	original := "# Realm\n\nsome stray content\n\n</docs>\n\nafter text\n"
	got, err := spliceManagedRegion(original, managedRegion{tag: "docs", content: "new"})
	if !errors.Is(err, errUnpairedRegion) {
		t.Fatalf("expected errUnpairedRegion, got %v", err)
	}
	if got != original {
		t.Errorf("document must be left unchanged on unpaired-close; got %q, want %q", got, original)
	}
}

// TestSpliceManagedRegion_ReversedOrderReturnsError verifies that both tags
// present but in reversed order (close precedes open) is treated as unpaired
// — it cannot be spliced sensibly — and returns errUnpairedRegion with the
// document left unchanged.
func TestSpliceManagedRegion_ReversedOrderReturnsError(t *testing.T) {
	original := "</bucket-docs>\n\nstuff\n\n<bucket-docs>\n"
	got, err := spliceManagedRegion(original, managedRegion{tag: "bucket-docs", content: "new"})
	if !errors.Is(err, errUnpairedRegion) {
		t.Fatalf("expected errUnpairedRegion, got %v", err)
	}
	if got != original {
		t.Errorf("document must be left unchanged on reversed-order tags; got %q, want %q", got, original)
	}
}

func TestSpliceManagedRegion_Idempotent(t *testing.T) {
	original := "# Realm\n\n<docs>\n\nold entry\n\n</docs>\n\nAfter.\n"
	region := managedRegion{tag: "docs", content: "stable entry"}

	first, err := spliceManagedRegion(original, region)
	if err != nil {
		t.Fatalf("first splice: unexpected error: %v", err)
	}
	second, err := spliceManagedRegion(first, region)
	if err != nil {
		t.Fatalf("second splice: unexpected error: %v", err)
	}
	if first != second {
		t.Errorf("splice not idempotent\nfirst:  %q\nsecond: %q", first, second)
	}
}

func TestFindRegion_Absent(t *testing.T) {
	state, _ := findRegion("# Realm\n\nno tags here\n", "docs")
	if state != regionAbsent {
		t.Errorf("got state %v, want regionAbsent", state)
	}
}

func TestFindRegion_WellFormed(t *testing.T) {
	state, bounds := findRegion("<docs>\n\nbody\n\n</docs>\n", "docs")
	if state != regionWellFormed {
		t.Fatalf("got state %v, want regionWellFormed", state)
	}
	if bounds.openStart != 0 {
		t.Errorf("openStart = %d, want 0", bounds.openStart)
	}
}

func TestFindRegion_UnpairedOpenOnly(t *testing.T) {
	state, _ := findRegion("<docs>\n\nbody with no close\n", "docs")
	if state != regionUnpaired {
		t.Errorf("got state %v, want regionUnpaired", state)
	}
}

func TestFindRegion_UnpairedCloseOnly(t *testing.T) {
	state, _ := findRegion("body with no open\n\n</docs>\n", "docs")
	if state != regionUnpaired {
		t.Errorf("got state %v, want regionUnpaired", state)
	}
}

// TestFindRegion_ReversedOrderUnpaired verifies findRegion reports
// regionUnpaired when both tags are present but the close tag precedes the
// open tag — reversed order cannot be spliced sensibly.
func TestFindRegion_ReversedOrderUnpaired(t *testing.T) {
	state, _ := findRegion("</bucket-docs>\n\nstuff\n\n<bucket-docs>\n", "bucket-docs")
	if state != regionUnpaired {
		t.Errorf("got state %v, want regionUnpaired", state)
	}
}

//
// spliceRegionAt is the single tested home for interior-region boundary
// whitespace: LEAD (bytes before the open tag), TRAIL (bytes after the close
// tag), and the EOF/start-of-file terminal cases. Table-tested across every
// LEAD (L0-L3) x TRAIL (T0-T3) combination plus the L0+T0 "document IS just
// the span" case — the full matrix the RCA identified as reviewers'
// blind spot.

// leadFixtures maps a LEAD sub-case name to the bytes preceding the span.
var leadFixtures = map[string]string{
	"L0_startOfFile":   "",
	"L1_alreadyBlank":  "Prose before.\n\n",
	"L2_singleNewline": "Prose before.\n",
	"L3_extraBlanks":   "Prose before.\n\n\n",
}

// leadWant maps the same sub-case to the expected normalized LEAD bytes.
var leadWant = map[string]string{
	"L0_startOfFile":   "",
	"L1_alreadyBlank":  "Prose before.\n\n",
	"L2_singleNewline": "Prose before.\n\n",
	"L3_extraBlanks":   "Prose before.\n\n\n",
}

// trailFixtures maps a TRAIL sub-case name to the bytes following the span.
var trailFixtures = map[string]string{
	"T0_eof":           "",
	"T1_alreadyBlank":  "\n\nAfter prose.\n",
	"T2_singleNewline": "\nAfter prose.\n",
	"T3_extraBlanks":   "\n\n\nAfter prose.\n",
}

// trailWant maps the same sub-case to the expected normalized TRAIL bytes.
var trailWant = map[string]string{
	"T0_eof":           "\n",
	"T1_alreadyBlank":  "\n\nAfter prose.\n",
	"T2_singleNewline": "\n\nAfter prose.\n",
	"T3_extraBlanks":   "\n\n\nAfter prose.\n",
}

// TestSpliceRegionAt_BoundaryMatrix exercises all 16 LEAD x TRAIL
// combinations, asserting the exact resulting document each time.
func TestSpliceRegionAt_BoundaryMatrix(t *testing.T) {
	region := managedRegion{tag: "docs", content: "body"}
	rendered := renderManagedRegion(region)

	leadOrder := []string{"L0_startOfFile", "L1_alreadyBlank", "L2_singleNewline", "L3_extraBlanks"}
	trailOrder := []string{"T0_eof", "T1_alreadyBlank", "T2_singleNewline", "T3_extraBlanks"}

	for _, lk := range leadOrder {
		for _, tk := range trailOrder {
			t.Run(lk+"_x_"+tk, func(t *testing.T) {
				before := leadFixtures[lk]
				after := trailFixtures[tk]
				span := "<!-- legacy span -->"
				document := before + span + after
				start := len(before)
				end := start + len(span)

				got := spliceRegionAt(document, start, end, region)
				want := leadWant[lk] + rendered + trailWant[tk]
				if got != want {
					t.Errorf("case %s_x_%s\ngot:  %q\nwant: %q", lk, tk, got, want)
				}
			})
		}
	}
}

// TestSpliceRegionAt_DocumentIsExactlyTheSpan covers the combined L0+T0 case
// explicitly: the document contains nothing but the span being replaced —
// no prefix, no suffix. The result must be the rendered region plus exactly
// one trailing newline, with no leading blank-line artifact.
func TestSpliceRegionAt_DocumentIsExactlyTheSpan(t *testing.T) {
	region := managedRegion{tag: "docs", content: "body"}
	span := "<!-- legacy span -->"

	got := spliceRegionAt(span, 0, len(span), region)
	want := renderManagedRegion(region) + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
