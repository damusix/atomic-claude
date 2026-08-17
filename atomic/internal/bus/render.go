package bus

import (
	"fmt"
	"hash/fnv"
	"io"
	"strings"
	"text/tabwriter"
	"unicode/utf8"
)

// timeFormat is TailLine's clock format. No date: a room is not expected to run
// across a day boundary in one sitting, and HH:MM:SS leaves the most room for
// sender, addressee, and text on a normal-width terminal.
const timeFormat = "15:04:05"

// arrowSep separates sender from addressee in TailLine's output.
const arrowSep = " → "

// unaddressedMarker is TailLine's addressee for an FYI envelope (empty To) — a
// fixed literal, not a room-name substitution.
const unaddressedMarker = "(room)"

// defaultLineWidth is wrapHanging's wrap point when the caller has no terminal
// width to report.
const defaultLineWidth = 80

// minWrapColumns floors the width wrapHanging computes from width-indent: a long
// sender/addressee pair on a narrow terminal must not drive it to zero.
const minWrapColumns = 20

// TailLine formats one envelope as "<time>  <from> → <addressee>   <text>", with
// continuation lines indented to align under the text column and long payloads
// collapsed to a marker naming the room log. colour false drops every ANSI
// sequence, so piped output is clean text. roomPrefix prepends "[<room>] " for
// --all-rooms. width <= 0 falls back to defaultLineWidth.
func TailLine(env Envelope, home string, width int, colour, roomPrefix bool) string {
	if width <= 0 {
		width = defaultLineWidth
	}

	addressee := unaddressedMarker
	if len(env.To) > 0 {
		addressee = strings.Join(env.To, ",")
	}

	var roomTag string
	if roomPrefix {
		roomTag = "[" + env.Room + "] "
	}
	ts := env.Ts.Format(timeFormat)

	// indent comes from the plain prefix's visual width: an ANSI-wrapped prefix
	// has the same on-screen width but more bytes, and using its byte length here
	// would misalign every continuation line.
	plainPrefix := fmt.Sprintf("%s%s  %s%s%s   ", roomTag, ts, env.From, arrowSep, addressee)
	indent := utf8.RuneCountInString(plainPrefix)

	text := collapse(env.Text, env.Room, home)
	body := wrapHanging(text, indent, width)

	if !colour {
		return plainPrefix + body
	}

	colouredTs := ansiDim + ts + ansiReset
	open, close := colourFor(env.From)
	colouredFrom := open + env.From + close
	colouredPrefix := fmt.Sprintf("%s%s  %s%s%s   ", roomTag, colouredTs, colouredFrom, arrowSep, addressee)
	return colouredPrefix + body
}

// ansiReset closes any SGR sequence colourFor or the dim timestamp style
// opens.
const ansiReset = "\x1b[0m"

// ansiDim is TailLine's timestamp styling — one shared style, not
// per-sender.
const ansiDim = "\x1b[2m"

// ansiPalette is colourFor's sender palette, cycled by a stable hash of the
// sender's name so a sender keeps one colour for the life of a tail session
// without any per-session state. The daemon assigns no colour; this is purely
// client-side, recomputed identically by any number of concurrent tails.
var ansiPalette = [...]string{
	"\x1b[31m", "\x1b[32m", "\x1b[33m", "\x1b[34m",
	"\x1b[35m", "\x1b[36m", "\x1b[91m", "\x1b[92m",
}

// colourFor returns the open/close ANSI pair for name, stable across calls via a
// non-cryptographic hash rather than any per-process state.
func colourFor(name string) (open, close string) {
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(name))
	return ansiPalette[sum.Sum32()%uint32(len(ansiPalette))], ansiReset
}

// collapseLineThreshold is the payload line count above which TailLine
// collapses text to a marker.
const collapseLineThreshold = 15

// collapseShowLines is how many leading lines survive a collapse — enough to
// orient the reader without dumping a long payload into a scrolling transcript.
const collapseShowLines = 8

// collapse returns text unchanged at or below collapseLineThreshold lines,
// otherwise the first collapseShowLines plus a marker naming how many remain and
// the room log. Append persists the original Text before TailLine ever sees it,
// so collapsing is a display decision, never data loss.
func collapse(text, room, home string) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= collapseLineThreshold {
		return text
	}
	remaining := len(lines) - collapseShowLines
	marker := fmt.Sprintf("… %d more line(s) — full message in %s", remaining, RoomLogPath(home, room))
	return strings.Join(lines[:collapseShowLines], "\n") + "\n" + marker
}

// wrapHanging wraps text to width columns, splitting each of text's own lines
// independently so an embedded newline (a stack trace) survives, and indenting
// every line after the first so the block reads aligned under where it started
// rather than merging with the next envelope's timestamp column. The first line
// carries no indent — TailLine supplies its own prefix.
func wrapHanging(text string, indent, width int) string {
	avail := width - indent
	if avail < minWrapColumns {
		avail = minWrapColumns
	}
	indentStr := strings.Repeat(" ", indent)

	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, wrapOneLine(line, avail)...)
	}
	return strings.Join(out, "\n"+indentStr)
}

// wrapOneLine breaks line into avail-column chunks on word boundaries. A single
// word wider than avail (a URL, a path, a hash) is kept intact rather than
// hard-broken: a chopped path is unreadable and uncopyable, which costs more
// than one overflowing column.
func wrapOneLine(line string, avail int) []string {
	if utf8.RuneCountInString(line) <= avail {
		return []string{line}
	}
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{line}
	}

	var lines []string
	var cur strings.Builder
	curLen := 0
	flush := func() {
		if curLen > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			curLen = 0
		}
	}

	for _, word := range words {
		wl := utf8.RuneCountInString(word)
		if wl > avail {
			flush()
			lines = append(lines, word)
			continue
		}
		sep := 0
		if curLen > 0 {
			sep = 1
		}
		if curLen+sep+wl > avail {
			flush()
			cur.WriteString(word)
			curLen = wl
			continue
		}
		if sep == 1 {
			cur.WriteByte(' ')
		}
		cur.WriteString(word)
		curLen += sep + wl
	}
	flush()
	return lines
}

// MemberTable writes members as an aligned table in the order given (Hub.Who
// already sorts). No separate qualified-name column: a member's Name is already
// its stacked position. The liveness column comes from livenessLabel so `who`
// and chat's `/who` cannot drift apart.
func MemberTable(w io.Writer, members []Member) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, m := range members {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", m.Name, m.Kind, m.Mode, livenessLabel(m.Stale), m.Repo, m.Realm)
	}
	return tw.Flush()
}

// RoomTable writes rooms as an aligned table in the order given (Hub.Rooms
// already sorts).
func RoomTable(w io.Writer, rooms []RoomInfo) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, r := range rooms {
		fmt.Fprintf(tw, "%s\t%d\n", r.Name, r.Members)
	}
	return tw.Flush()
}
