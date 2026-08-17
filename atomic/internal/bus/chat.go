package bus

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ansiClearLine returns the cursor to column 0 and erases the line — the one
// primitive every redraw here builds on. Chat only ever rewrites its own pinned
// input line; the transcript above is ordinary sequential output the terminal's
// scrollback handles, so there is no multi-line cursor tracking to get wrong.
const ansiClearLine = "\r\x1b[2K"

// promptPrefix opens the pinned input line when nothing is buffered.
const promptPrefix = "> "

// Chat is `atomic bus chat`'s interactive loop: a scrolling transcript above one
// pinned input line. Every collaborator is a field rather than a package-level
// call, so a test drives the loop with no real terminal and no real socket.
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

	// pending is what the operator has typed since the last Enter, decoded one
	// rune at a time so a backspace can never split a multi-byte character. Its
	// emptiness is the signal the rest of this file keys off — see backlog.
	pending []rune

	// backlog holds envelopes that arrived while pending was non-empty. There is
	// no portable raw-terminal-safe way to ask whether the operator has scrolled
	// away from the bottom, so "mid-typing" is the reachable proxy: interleaving
	// a transcript line while someone is composing yanks the viewport. Nothing is
	// dropped — the backlog flushes the instant pending goes empty.
	backlog []Envelope
}

// Run multiplexes keystrokes from in against envelopes arriving on the
// subscription channel until /quit, Ctrl+C, or in closes. All three leave the
// room first, so a chat session never leaves a ghost member behind. Only a read
// error other than EOF is returned; every clean exit returns nil.
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

// leaveQuiet leaves once Run's loop has ended, printing the same confirmation
// leaveAction prints, cleared onto the pinned line rather than appended.
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

// keyEvent is one decoded keystroke, or the error that ended readKeys.
type keyEvent struct {
	r   rune
	err error
}

// readKeys decodes one UTF-8 rune at a time and blocks on each send, so a slow
// consumer paces the read one keystroke at a time — which is what lets Run's
// select interleave keystrokes and envelopes without racing on c.pending. done
// unblocks a send once nothing is reading out; it does not unblock a read
// already parked in ReadRune, which returns only when the reader is closed.
func readKeys(r io.Reader, out chan<- keyEvent, done <-chan struct{}) {
	// A panic here kills the process, and the deferred terminal restore lives on
	// Run's stack, not this one — the operator would be left in a shell with echo
	// disabled. Convert it to an error event so Run unwinds and its defers run.
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

// handleKey applies one keystroke and reports whether it ends the session.
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

// backspace erases the last typed rune from pending and the screen. Erasing the
// final one crosses pending back to empty, which is flushBacklog's trigger.
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

// submitLine dispatches the submitted line (unless blank), then flushes anything
// buffered while the operator was composing. True only for "/quit".
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

// onInput dispatches one submitted non-blank line: a bare line sends to the
// room, "@name text" addresses one member, "/who" and "/rooms" print inline,
// "/halt" and "/resume" toggle the room, "/quit" ends the session.
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

// sendAddressed sends to exactly one addressee — the in-chat syntax takes a
// single name, not a comma-separated list like send --to.
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

// printMembers and printRooms reuse render.go's table formatting rather than a
// second chat-specific renderer.
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

// printRaw clears the pinned input line, writes s with a guaranteed trailing
// newline, and redraws the prompt below it. Every path that puts a full line
// into the transcript goes through this, so the redraw happens one way rather
// than being reimplemented per caller.
func (c *Chat) printRaw(s string) {
	fmt.Fprint(c.out, ansiClearLine)
	fmt.Fprint(c.out, s)
	if !strings.HasSuffix(s, "\n") {
		fmt.Fprintln(c.out)
	}
	c.redrawPrompt()
}

// deliver prints an arriving envelope immediately, or holds it in backlog and
// counts it in the prompt when the operator is mid-composition.
func (c *Chat) deliver(env Envelope) {
	if len(c.pending) > 0 {
		c.backlog = append(c.backlog, env)
		c.redrawPrompt()
		return
	}
	c.printRaw(TailLine(env, c.home, c.width, c.colour, false))
}

// flushBacklog prints every envelope withheld while the operator was composing,
// then redraws the prompt. Backpressure, not truncation: a count while
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

// promptLine is the pinned line's full content: a "[N new]" prefix while backlog
// is non-empty, then the prompt marker and whatever has been typed.
func (c *Chat) promptLine() string {
	if n := len(c.backlog); n > 0 {
		return fmt.Sprintf("[%d new] %s%s", n, promptPrefix, string(c.pending))
	}
	return promptPrefix + string(c.pending)
}
