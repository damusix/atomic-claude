package bus

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func testEnvelope() Envelope {
	return Envelope{
		ID:       "m-1234",
		Room:     "potato",
		From:     "backend",
		FromKind: KindAgent,
		To:       []string{"frontend"},
		Ts:       time.Date(2026, 7, 28, 14, 2, 11, 0, time.UTC).Local(),
		Text:     "endpoint /v1/invoices is live, 12 tests pass",
	}
}

// --- TailLine ---

func TestTailLine_AddressedMessage_ShowsSenderArrowAddressee(t *testing.T) {
	line := TailLine(testEnvelope(), t.TempDir(), 80, false, false)
	if !strings.Contains(line, "backend") || !strings.Contains(line, "frontend") {
		t.Fatalf("line = %q, want it to name both sender and addressee", line)
	}
	if !strings.Contains(line, "endpoint /v1/invoices is live, 12 tests pass") {
		t.Fatalf("line = %q, want it to contain the message text", line)
	}
	if !strings.Contains(line, arrowSep) {
		t.Fatalf("line = %q, want an arrow separator between sender and addressee", line)
	}
}

// TestTailLine_UnaddressedMessage_ShowsRoomMarker proves an FYI message
// (empty To) renders the literal "(room)" placeholder as its addressee —
// docs/spec/atomic-bus.md, verbatim: "with `(room)` as the addressee
// for an unaddressed FYI message."
func TestTailLine_UnaddressedMessage_ShowsRoomMarker(t *testing.T) {
	env := testEnvelope()
	env.To = nil
	line := TailLine(env, t.TempDir(), 80, false, false)
	if !strings.Contains(line, unaddressedMarker) {
		t.Fatalf("line = %q, want it to contain the FYI marker %q", line, unaddressedMarker)
	}
}

// TestTailLine_NoColour_NoANSIEscapes is the success criterion,
// asserted at the byte level: "piping tail to a non-tty emits no ANSI
// escapes".
func TestTailLine_NoColour_NoANSIEscapes(t *testing.T) {
	line := TailLine(testEnvelope(), t.TempDir(), 80, false, false)
	if strings.ContainsRune(line, '\x1b') {
		t.Fatalf("line = %q, contains an ANSI escape byte with colour disabled", line)
	}
}

func TestTailLine_Colour_ContainsANSISequences(t *testing.T) {
	line := TailLine(testEnvelope(), t.TempDir(), 80, true, false)
	if !strings.ContainsRune(line, '\x1b') {
		t.Fatalf("line = %q, want at least one ANSI escape when colour is enabled", line)
	}
}

func TestTailLine_RoomPrefix_WhenEnabled(t *testing.T) {
	line := TailLine(testEnvelope(), t.TempDir(), 80, false, true)
	if !strings.HasPrefix(line, "[potato] ") {
		t.Fatalf("line = %q, want it to start with the room prefix", line)
	}
}

func TestTailLine_NoRoomPrefix_WhenDisabled(t *testing.T) {
	line := TailLine(testEnvelope(), t.TempDir(), 80, false, false)
	if strings.HasPrefix(line, "[potato]") {
		t.Fatalf("line = %q, want no room prefix when roomPrefix is false", line)
	}
}

// TestTailLine_LongPayload_CollapsesAndNamesLogPath proves TailLine wires
// collapse in: a payload over collapseLineThreshold lines is cut down and
// the rendered line names the room log path.
func TestTailLine_LongPayload_CollapsesAndNamesLogPath(t *testing.T) {
	home := t.TempDir()
	env := testEnvelope()
	lines := make([]string, collapseLineThreshold+5)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i)
	}
	env.Text = strings.Join(lines, "\n")

	line := TailLine(env, home, 80, false, false)
	if strings.Contains(line, "line "+strconv.Itoa(len(lines)-1)) {
		t.Fatalf("line = %q, want the tail of a long payload cut off, not present verbatim", line)
	}
	if !strings.Contains(line, RoomLogPath(home, env.Room)) {
		t.Fatalf("line = %q, want it to name the room log path %q", line, RoomLogPath(home, env.Room))
	}
}

// --- wrapHanging ---

func TestWrapHanging_ShortTextUnchanged(t *testing.T) {
	got := wrapHanging("short text", 10, 80)
	if got != "short text" {
		t.Fatalf("got %q, want unchanged", got)
	}
}

// TestWrapHanging_LongTextWrapsWithHangingIndent proves every continuation
// line is indented by exactly indent spaces, and no wrapped line exceeds
// width-indent columns.
func TestWrapHanging_LongTextWrapsWithHangingIndent(t *testing.T) {
	indent := 10
	width := 40
	words := make([]string, 30)
	for i := range words {
		words[i] = "word" + strconv.Itoa(i)
	}
	text := strings.Join(words, " ")

	got := wrapHanging(text, indent, width)
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("wrapHanging did not wrap at all: %q", got)
	}
	for i, line := range lines {
		if i == 0 {
			continue
		}
		if !strings.HasPrefix(line, strings.Repeat(" ", indent)) {
			t.Errorf("continuation line %d = %q, want it indented by %d spaces", i, line, indent)
		}
		if utf8.RuneCountInString(line) > width {
			t.Errorf("continuation line %d = %q (%d runes), want <= %d", i, line, utf8.RuneCountInString(line), width)
		}
	}
}

// TestWrapHanging_PreservesEmbeddedNewlines proves a pre-existing newline
// in text (a multi-line payload, e.g. a stack trace) is preserved as a
// line break and indented like any other continuation line.
func TestWrapHanging_PreservesEmbeddedNewlines(t *testing.T) {
	got := wrapHanging("first line\nsecond line", 4, 80)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %v, want exactly 2", lines)
	}
	if lines[0] != "first line" {
		t.Fatalf("lines[0] = %q, want %q (first line carries no indent — the caller's prefix precedes it)", lines[0], "first line")
	}
	if lines[1] != "    second line" {
		t.Fatalf("lines[1] = %q, want %q", lines[1], "    second line")
	}
}

// TestWrapHanging_SingleOverlongWordStaysIntact proves one pathological
// token (a URL, a file path) longer than the available width is kept
// intact on its own line rather than hard-broken mid-token — a chopped
// path is unreadable and uncopyable, which defeats collapse's whole point
// of leaving a working pointer to the room log.
func TestWrapHanging_SingleOverlongWordStaysIntact(t *testing.T) {
	overlong := strings.Repeat("x", 100)
	got := wrapHanging(overlong, 0, 40)
	if got != overlong {
		t.Fatalf("got %q, want the overlong token kept intact, unbroken", got)
	}
}

// --- collapse ---

func TestCollapse_ShortTextUnchanged(t *testing.T) {
	text := "line 1\nline 2\nline 3"
	got := collapse(text, "potato", t.TempDir())
	if got != text {
		t.Fatalf("got %q, want unchanged", got)
	}
}

// TestCollapse_LongTextTruncatesAndNamesLogPath_FullTextStillInRoomLog
// proves both halves of the success criterion together: the rendered
// text is cut, and the full original text is still recoverable from the
// room log — collapse is a display decision, never a data-loss one.
func TestCollapse_LongTextTruncatesAndNamesLogPath_FullTextStillInRoomLog(t *testing.T) {
	home := t.TempDir()
	lines := make([]string, collapseLineThreshold+10)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i)
	}
	full := strings.Join(lines, "\n")

	got := collapse(full, "potato", home)
	if got == full {
		t.Fatal("collapse did not shorten a payload over the threshold")
	}
	if !strings.Contains(got, RoomLogPath(home, "potato")) {
		t.Fatalf("collapsed text = %q, want it to name the room log path", got)
	}
	lastLine := "line " + strconv.Itoa(len(lines)-1)
	if strings.Contains(got, lastLine) {
		t.Fatalf("collapsed text = %q, want the tail of the payload cut off", got)
	}

	// The full, uncollapsed text must still be recoverable from the room
	// log — Append writes the original envelope before TailLine ever sees
	// it.
	env := Envelope{ID: "m-1", Room: "potato", From: "human", FromKind: KindHuman, Text: full}
	if err := Append(home, "potato", env); err != nil {
		t.Fatalf("Append: %v", err)
	}
	envs := readRoomLog(t, home, "potato")
	if len(envs) != 1 || envs[0].Text != full {
		t.Fatalf("room log did not preserve the full text: got %d envelope(s)", len(envs))
	}
}

// --- colourFor ---

func TestColourFor_StableAcrossCalls(t *testing.T) {
	open1, close1 := colourFor("backend")
	open2, close2 := colourFor("backend")
	if open1 != open2 || close1 != close2 {
		t.Fatalf("colourFor(%q) is not stable across calls: (%q,%q) vs (%q,%q)", "backend", open1, close1, open2, close2)
	}
}

func TestColourFor_ReturnsANSISequences(t *testing.T) {
	open, closeSeq := colourFor("backend")
	if !strings.HasPrefix(open, "\x1b[") {
		t.Fatalf("open = %q, want an ANSI SGR sequence", open)
	}
	if closeSeq != ansiReset {
		t.Fatalf("close = %q, want %q", closeSeq, ansiReset)
	}
}

// --- MemberTable / RoomTable ---

func TestMemberTable_IncludesKindColumn(t *testing.T) {
	var buf bytes.Buffer
	members := []Member{
		{Name: "backend", Kind: KindAgent, Mode: "participate"},
		{Name: "operator", Kind: KindHuman, Mode: "participate"},
	}
	if err := MemberTable(&buf, members); err != nil {
		t.Fatalf("MemberTable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "backend") || !strings.Contains(out, KindAgent) {
		t.Errorf("table = %q, missing the agent row", out)
	}
	if !strings.Contains(out, "operator") || !strings.Contains(out, KindHuman) {
		t.Errorf("table = %q, missing the human row", out)
	}
}

func TestRoomTable_Basic(t *testing.T) {
	var buf bytes.Buffer
	rooms := []RoomInfo{{Name: "potato", Members: 2}, {Name: "carrot", Members: 1}}
	if err := RoomTable(&buf, rooms); err != nil {
		t.Fatalf("RoomTable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "potato") || !strings.Contains(out, "carrot") {
		t.Fatalf("table = %q, missing a room", out)
	}
}
