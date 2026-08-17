package bus

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ansiClearLine returns the cursor to column 0 of the current line and
// erases it — the one primitive every redraw in this file builds on. Chat
// only ever rewrites its own single pinned input line this way; the
// scrolling transcript above it is ordinary sequential output, which the
// terminal's own scrollback already handles, so there is no multi-line
// cursor tracking to get wrong (docs/spec/atomic-bus.md the risk
// row: line-oriented redraw, not a full-screen widget tree).
const ansiClearLine = "\r\x1b[2K"

// promptPrefix opens the pinned input line when nothing is buffered.
const promptPrefix = "> "

// Chat is atomic bus chat's interactive loop: a scrolling transcript above
// one pinned input line. Every collaborator is a field, not a package-level
// call, so the loop is fully driven by a test with no real terminal and no
// real socket — in is any io.Reader (a raw-mode stdin in production, an
// io.Pipe in tests), out is any io.Writer, and envelopes is any
// receive-only channel. send/who/rooms/halt/resume/leave are the wire
// operations chatAction (action.go) wires to the daemon; a test substitutes
// spies for all of them.
type Chat struct {
	home string
	room string

	in  io.Reader
	out io.Writer

	envelopes <-chan Envelope
	send      func(text string, to []string) error
	who       func() ([]Member, error)
	rooms     func() ([]RoomInfo, error)
	halt      func(text string) error
	resume    func() error
	leave     func() error

	colour bool
	width  int

	// pending is what the operator has typed since the last Enter, decoded
	// one rune at a time (never raw bytes) so a backspace can never split a
	// multi-byte character. Its emptiness is the one signal the rest of
	// this file keys off — see backlog below.
	pending []rune

	// backlog holds envelopes that arrived while pending was non-empty.
	// There is no portable, raw-terminal-safe way to ask the emulator
	// whether the operator has scrolled its native scrollback away from
	// the bottom, so this checkpoint uses "is the operator mid-typing" as
	// the one reachable proxy for the brief's backpressure requirement:
	// interleaving a new transcript line while someone is composing is
	// exactly the "yank the viewport" the brief warns against. Nothing
	// here is ever dropped — every envelope in backlog is flushed the
	// instant pending goes empty again (Enter, or backspacing to nothing).
	backlog []Envelope
}

// Run is Chat's core loop: multiplex keystrokes from in against envelopes
// arriving on the subscription channel until /quit, Ctrl+C, or in closes
// (io.EOF — e.g. Ctrl+D, or a closed pipe in a test). All three leave the
// room via leave (when set) before returning, so a chat session never
// leaves a ghost member behind. Only a genuine read error other than EOF is
// returned; every clean exit path returns nil.
func (c *Chat) Run() error {
	defer c.leaveQuiet()

	keys := make(chan keyEvent)
	done := make(chan struct{})
	defer close(done)
	go readKeys(c.in, keys, done)

	c.redrawPrompt()
	for {
		select {
		case env, ok := <-c.envelopes:
			if !ok {
				return nil
			}
			c.deliver(env)
		case k := <-keys:
			if k.err != nil {
				if errors.Is(k.err, io.EOF) {
					return nil
				}
				return k.err
			}
			if c.handleKey(k.r) {
				return nil
			}
		}
	}
}

// leaveQuiet runs c.leave (if set) once Run's loop has ended, printing the
// same "left <room>" confirmation leaveAction prints — or a warning on
// failure — cleared onto the pinned line rather than appended after
// whatever was there.
func (c *Chat) leaveQuiet() {
	if c.leave == nil {
		return
	}
	if err := c.leave(); err != nil {
		fmt.Fprintf(c.out, "%satomic bus chat: leave failed: %v\n", ansiClearLine, err)
		return
	}
	fmt.Fprintf(c.out, "%sleft %s\n", ansiClearLine, c.room)
}

// keyEvent is one decoded keystroke, or the terminal error that ended
// readKeys.
type keyEvent struct {
	r   rune
	err error
}

// readKeys decodes one UTF-8 rune at a time from r and pushes it to out,
// blocking on each send so a slow consumer paces the read exactly one
// keystroke at a time — this is what makes it safe for Run's select loop
// to interleave keystrokes against arriving envelopes without a data race
// on c.pending. done lets Run's return unblock a send that would otherwise
// park forever once nothing is reading out anymore; it does not unblock a
// read already parked in br.ReadRune() — that only returns once the
// underlying reader is closed, exactly like Client.Close does for a
// Subscribe goroutine (client.go).
func readKeys(r io.Reader, out chan<- keyEvent, done <-chan struct{}) {
	// A panic here would kill the process outright, and the deferred terminal
	// restore lives on Run's goroutine stack, not this one — so the operator
	// would be left in a shell with echo and line editing disabled. Convert it
	// into an error event instead: Run sees a read failure, unwinds normally,
	// and its defers put the terminal back.
	defer func() {
		if p := recover(); p != nil {
			select {
			case out <- keyEvent{err: fmt.Errorf("bus: chat input reader panicked: %v", p)}:
			case <-done:
			}
		}
	}()

	br := bufio.NewReader(r)
	for {
		ru, _, err := br.ReadRune()
		if err != nil {
			select {
			case out <- keyEvent{err: err}:
			case <-done:
			}
			return
		}
		select {
		case out <- keyEvent{r: ru}:
		case <-done:
			return
		}
	}
}

// handleKey applies one decoded keystroke and reports whether it ends the
// session (submitting "/quit", or Ctrl+C).
func (c *Chat) handleKey(r rune) bool {
	switch r {
	case '\r', '\n':
		return c.submitLine()
	case 0x7f, 0x08: // DEL, backspace
		c.backspace()
		return false
	case 0x03: // Ctrl+C
		return true
	default:
		if r < 0x20 { // other control bytes (tab, escape sequences, ...): ignore, don't echo garbage
			return false
		}
		c.echoRune(r)
		return false
	}
}

func (c *Chat) echoRune(r rune) {
	c.pending = append(c.pending, r)
	fmt.Fprint(c.out, string(r))
}

// backspace erases the last typed rune, both from pending and the screen.
// Erasing the final rune crosses pending back to empty, which is exactly
// the trigger flushBacklog waits for — see backlog's doc.
func (c *Chat) backspace() {
	if len(c.pending) == 0 {
		return
	}
	c.pending = c.pending[:len(c.pending)-1]
	if len(c.pending) == 0 {
		c.flushBacklog()
		return
	}
	fmt.Fprint(c.out, "\b \b")
}

// submitLine processes the line the operator just pressed Enter on:
// dispatches it (unless blank), then flushes anything buffered while they
// were composing. Returns true only for "/quit" (via onInput).
func (c *Chat) submitLine() bool {
	line := strings.TrimSpace(string(c.pending))
	c.pending = nil
	fmt.Fprintln(c.out)
	if line != "" && c.onInput(line) {
		return true
	}
	c.flushBacklog()
	return false
}

// onInput dispatches one submitted, non-blank line per
// docs/spec/atomic-bus.md the in-chat syntax table: a bare line
// sends to the room, "@name text" addresses one member, "/who" and
// "/rooms" print inline, "/halt" and "/resume" toggle the room, and
// "/quit" ends the session (signalled via the bool return).
func (c *Chat) onInput(line string) bool {
	switch {
	case line == "/quit":
		return true
	case line == "/who":
		c.printMembers()
	case line == "/rooms":
		c.printRooms()
	case line == "/halt" || strings.HasPrefix(line, "/halt "):
		c.doHalt(strings.TrimSpace(strings.TrimPrefix(line, "/halt")))
	case line == "/resume":
		c.doResume()
	case strings.HasPrefix(line, "/"):
		c.printSystem(fmt.Sprintf("unknown command: %s (try /who, /rooms, /halt, /resume, /quit)", line))
	case strings.HasPrefix(line, "@"):
		c.sendAddressed(line[1:])
	default:
		c.sendBare(line)
	}
	return false
}

// sendAddressed parses "<name> <text>" (the part of an "@name text" line
// after the "@") and sends to exactly that one addressee — the in-chat
// syntax table specifies a single name, not a comma-separated list like
// send --to.
func (c *Chat) sendAddressed(rest string) {
	name, text, ok := strings.Cut(rest, " ")
	text = strings.TrimSpace(text)
	if !ok || name == "" || text == "" {
		c.printSystem("usage: @<name> <text>")
		return
	}
	if err := c.send(text, []string{name}); err != nil {
		c.printSystem(fmt.Sprintf("send failed: %v", err))
	}
}

func (c *Chat) sendBare(text string) {
	if err := c.send(text, nil); err != nil {
		c.printSystem(fmt.Sprintf("send failed: %v", err))
	}
}

func (c *Chat) doHalt(text string) {
	if err := c.halt(text); err != nil {
		c.printSystem(fmt.Sprintf("halt failed: %v", err))
	}
}

func (c *Chat) doResume() {
	if err := c.resume(); err != nil {
		c.printSystem(fmt.Sprintf("resume failed: %v", err))
	}
}

// printMembers and printRooms reuse render.go's own table formatting
// (MemberTable, RoomTable) rather than a second, chat-specific renderer.
func (c *Chat) printMembers() {
	members, err := c.who()
	if err != nil {
		c.printSystem(fmt.Sprintf("who failed: %v", err))
		return
	}
	var b strings.Builder
	_ = MemberTable(&b, members)
	c.printRaw(b.String())
}

func (c *Chat) printRooms() {
	rooms, err := c.rooms()
	if err != nil {
		c.printSystem(fmt.Sprintf("rooms failed: %v", err))
		return
	}
	var b strings.Builder
	_ = RoomTable(&b, rooms)
	c.printRaw(b.String())
}

func (c *Chat) printSystem(msg string) {
	c.printRaw("! " + msg)
}

// printRaw clears the pinned input line, writes s (a trailing newline is
// guaranteed), and redraws the prompt below it. Every path that puts a
// full line into the transcript — an arriving envelope, a /who or /rooms
// table, a system notice — goes through this, so the input line is
// cleared and rebuilt the same way every time rather than each caller
// reimplementing the redraw dance.
func (c *Chat) printRaw(s string) {
	fmt.Fprint(c.out, ansiClearLine)
	fmt.Fprint(c.out, s)
	if !strings.HasSuffix(s, "\n") {
		fmt.Fprintln(c.out)
	}
	c.redrawPrompt()
}

// deliver handles one arriving envelope: printed immediately when the
// operator isn't mid-composition, or held in backlog and counted in the
// prompt otherwise — see backlog's doc comment for why "mid-composition"
// is this checkpoint's stand-in for "scrolled up".
func (c *Chat) deliver(env Envelope) {
	if len(c.pending) > 0 {
		c.backlog = append(c.backlog, env)
		c.redrawPrompt()
		return
	}
	c.printRaw(TailLine(env, c.home, c.width, c.colour, false))
}

// flushBacklog prints every envelope withheld while the operator was
// composing, then redraws the prompt. Called the instant pending goes
// empty (Enter, or backspacing to nothing) so nothing buffered is ever
// held longer than the composition that caused it to be buffered — this
// is the "backpressure, not truncation" the brief asks for: a count while
// composing, never a dropped message.
func (c *Chat) flushBacklog() {
	if len(c.backlog) > 0 {
		var b strings.Builder
		for _, env := range c.backlog {
			b.WriteString(TailLine(env, c.home, c.width, c.colour, false))
			b.WriteByte('\n')
		}
		c.backlog = nil
		fmt.Fprint(c.out, ansiClearLine)
		fmt.Fprint(c.out, b.String())
	}
	c.redrawPrompt()
}

func (c *Chat) redrawPrompt() {
	fmt.Fprint(c.out, ansiClearLine+c.promptLine())
}

// promptLine is the pinned line's full content: a "[N new]" prefix while
// backlog is non-empty (see its doc), then the fixed prompt marker and
// whatever the operator has typed so far.
func (c *Chat) promptLine() string {
	if n := len(c.backlog); n > 0 {
		return fmt.Sprintf("[%d new] %s%s", n, promptPrefix, string(c.pending))
	}
	return promptPrefix + string(c.pending)
}
