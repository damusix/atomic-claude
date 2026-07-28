package bus

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// chatTestTimeout bounds every wait in this file, per the checkpoint
// brief: no test here may be capable of hanging the suite.
const chatTestTimeout = 2 * time.Second

// syncBuffer is a mutex-guarded io.Writer with a String() reader — Chat.Run
// executes in its own goroutine in every test here while the test goroutine
// writes input and polls output, so a plain bytes.Buffer (unsafe for
// concurrent use) would race under `go test -race`.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// sendCall is one recorded invocation of a test's send spy.
type sendCall struct {
	text string
	to   []string
}

// chatHarness bundles a Chat wired to spies plus the pipe feeding its
// input, so each test only assembles what it actually exercises. in and
// out are connected through a real io.Pipe: a pipe write blocks until
// Chat.Run's own reader goroutine consumes it, so every test here starts
// Run first (via start) and only writes input afterward — writing before
// Run exists to read it would deadlock the pipe.
type chatHarness struct {
	chat *Chat
	out  *syncBuffer
	pr   *io.PipeReader
	pw   *io.PipeWriter

	mu        sync.Mutex
	sendCalls []sendCall
	haltCalls []string
	resumed   int
	leftCount int
}

func newChatHarness(envelopes <-chan Envelope) *chatHarness {
	pr, pw := io.Pipe()
	h := &chatHarness{out: &syncBuffer{}, pr: pr, pw: pw}
	h.chat = &Chat{
		home:      "/home/does-not-matter",
		room:      "potato",
		in:        pr,
		out:       h.out,
		envelopes: envelopes,
		send: func(text string, to []string) error {
			h.mu.Lock()
			h.sendCalls = append(h.sendCalls, sendCall{text: text, to: to})
			h.mu.Unlock()
			return nil
		},
		who: func() ([]Member, error) {
			return []Member{{Name: "backend", Kind: KindAgent, Mode: "participate"}}, nil
		},
		rooms: func() ([]RoomInfo, error) {
			return []RoomInfo{{Name: "potato", Members: 2}}, nil
		},
		halt: func(text string) error {
			h.mu.Lock()
			h.haltCalls = append(h.haltCalls, text)
			h.mu.Unlock()
			return nil
		},
		resume: func() error {
			h.mu.Lock()
			h.resumed++
			h.mu.Unlock()
			return nil
		},
		leave: func() error {
			h.mu.Lock()
			h.leftCount++
			h.mu.Unlock()
			return nil
		},
	}
	return h
}

func (h *chatHarness) getSendCalls() []sendCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]sendCall, len(h.sendCalls))
	copy(out, h.sendCalls)
	return out
}

func (h *chatHarness) getHaltCalls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.haltCalls))
	copy(out, h.haltCalls)
	return out
}

func (h *chatHarness) getResumed() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.resumed
}

func (h *chatHarness) getLeftCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.leftCount
}

// start runs the harness's Chat.Run in the background and returns a channel
// that receives its result exactly once.
func (h *chatHarness) start(t *testing.T) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- h.chat.Run() }()
	return done
}

// wait blocks for Run's result, bounded by chatTestTimeout.
func (h *chatHarness) wait(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(chatTestTimeout):
		t.Fatal("Chat.Run did not return within the bounded wait")
		return nil
	}
}

// waitForOutput polls h.out until it contains want, bounded by
// chatTestTimeout — the same poll-until-condition pattern action_test.go's
// publishUntilDelivered/waitForDaemonGone use for the identical reason:
// there is no other signal to synchronize on across the goroutine boundary.
func (h *chatHarness) waitForOutput(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(chatTestTimeout)
	for {
		if strings.Contains(h.out.String(), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("output did not contain %q within %s; got:\n%s", want, chatTestTimeout, h.out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (h *chatHarness) writeInput(t *testing.T, s string) {
	t.Helper()
	if _, err := h.pw.Write([]byte(s)); err != nil {
		t.Fatalf("write input %q: %v", s, err)
	}
}

func (h *chatHarness) closeInput() {
	_ = h.pw.Close()
}

// --- bare line / addressed send ---

func TestChat_BareLine_SendsUnaddressed(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.writeInput(t, "hello room\n")
	h.closeInput() // EOF after the line ends Run

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}

	calls := h.getSendCalls()
	if len(calls) != 1 {
		t.Fatalf("send calls = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].text != "hello room" {
		t.Errorf("text = %q, want %q", calls[0].text, "hello room")
	}
	if calls[0].to != nil {
		t.Errorf("to = %v, want nil (unaddressed)", calls[0].to)
	}
	if h.getLeftCount() != 1 {
		t.Errorf("leave called %d times, want 1", h.getLeftCount())
	}
}

func TestChat_AtName_AddressesOneRecipient(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.writeInput(t, "@backend are you there\n")
	h.closeInput()

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}

	calls := h.getSendCalls()
	if len(calls) != 1 {
		t.Fatalf("send calls = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].text != "are you there" {
		t.Errorf("text = %q, want %q", calls[0].text, "are you there")
	}
	if len(calls[0].to) != 1 || calls[0].to[0] != "backend" {
		t.Errorf("to = %v, want [backend]", calls[0].to)
	}
}

func TestChat_AtNameWithNoText_PrintsUsageDoesNotSend(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.writeInput(t, "@backend\n")
	h.closeInput()

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(h.getSendCalls()) != 0 {
		t.Fatalf("send calls = %d, want 0 for a bare @name with no text", len(h.getSendCalls()))
	}
	if !strings.Contains(h.out.String(), "usage: @<name> <text>") {
		t.Errorf("output = %q, want a usage hint", h.out.String())
	}
}

func TestChat_BlankLine_DoesNotSend(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.writeInput(t, "\n")
	h.closeInput()

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.getSendCalls()) != 0 {
		t.Fatalf("send calls = %d, want 0 for a blank submitted line", len(h.getSendCalls()))
	}
}

// --- slash commands ---

func TestChat_SlashWho_PrintsRosterInline(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.writeInput(t, "/who\n")
	h.closeInput()

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(h.out.String(), "backend") {
		t.Errorf("output = %q, want it to contain the roster (MemberTable output)", h.out.String())
	}
}

func TestChat_SlashRooms_PrintsRoomListInline(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.writeInput(t, "/rooms\n")
	h.closeInput()

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(h.out.String(), "potato") {
		t.Errorf("output = %q, want it to contain the room list (RoomTable output)", h.out.String())
	}
}

func TestChat_SlashHalt_DispatchesWithReason(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.writeInput(t, "/halt wrong approach\n")
	h.closeInput()

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := h.getHaltCalls()
	if len(calls) != 1 || calls[0] != "wrong approach" {
		t.Fatalf("halt calls = %+v, want one call with reason %q", calls, "wrong approach")
	}
}

func TestChat_SlashHalt_NoReason_DispatchesWithEmptyText(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.writeInput(t, "/halt\n")
	h.closeInput()

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := h.getHaltCalls()
	if len(calls) != 1 || calls[0] != "" {
		t.Fatalf("halt calls = %+v, want one call with empty text", calls)
	}
}

func TestChat_SlashResume_Dispatches(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.writeInput(t, "/resume\n")
	h.closeInput()

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if h.getResumed() != 1 {
		t.Fatalf("resume called %d times, want 1", h.getResumed())
	}
}

func TestChat_UnknownSlashCommand_PrintsHintDoesNotSend(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.writeInput(t, "/frobnicate\n")
	h.closeInput()

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.getSendCalls()) != 0 {
		t.Fatalf("send calls = %d, want 0 for an unknown slash command", len(h.getSendCalls()))
	}
	if !strings.Contains(h.out.String(), "unknown command: /frobnicate") {
		t.Errorf("output = %q, want an unknown-command hint", h.out.String())
	}
}

// --- /quit and other exit paths ---

func TestChat_SlashQuit_ExitsCleanlyAndLeaves(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.writeInput(t, "/quit\n")
	// Deliberately not closing the pipe: /quit must end Run on its own,
	// without relying on EOF.

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if h.getLeftCount() != 1 {
		t.Fatalf("leave called %d times, want 1", h.getLeftCount())
	}
	if !strings.Contains(h.out.String(), "left potato") {
		t.Errorf("output = %q, want a \"left potato\" confirmation", h.out.String())
	}
	h.closeInput()
}

func TestChat_CtrlC_ExitsCleanlyAndLeaves(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.writeInput(t, "\x03")

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if h.getLeftCount() != 1 {
		t.Fatalf("leave called %d times, want 1", h.getLeftCount())
	}
	h.closeInput()
}

func TestChat_EOFOnInput_ExitsCleanlyAndLeaves(t *testing.T) {
	h := newChatHarness(nil)
	done := h.start(t)
	h.closeInput() // immediate EOF, nothing typed

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if h.getLeftCount() != 1 {
		t.Fatalf("leave called %d times, want 1", h.getLeftCount())
	}
}

// --- envelope delivery: must not corrupt a half-typed input line ---

// TestChat_EnvelopeMidInput_DoesNotCorruptPendingLine_BuffersAndCounts is the
// checkpoint's two hardest requirements proven together: an envelope
// arriving while the operator has typed a partial, unsubmitted line must
// not interleave into the transcript (it is held and counted instead), and
// the partial line itself must survive intact so the operator can keep
// typing and eventually submit exactly what they started.
func TestChat_EnvelopeMidInput_DoesNotCorruptPendingLine_BuffersAndCounts(t *testing.T) {
	envelopes := make(chan Envelope, 4)
	h := newChatHarness(envelopes)
	done := h.start(t)

	// Type a partial line — deliberately no trailing newline — and wait
	// for it to actually reach the (separate) Run goroutine and get
	// echoed, so the envelope sent next is unambiguously "mid-input".
	h.writeInput(t, "hello wor")
	h.waitForOutput(t, "> hello wor")

	env := Envelope{ID: "m-1", Room: "potato", From: "backend", Ts: time.Now(), Text: "interrupting message"}
	select {
	case envelopes <- env:
	case <-time.After(chatTestTimeout):
		t.Fatal("could not hand the envelope to Chat's subscription channel")
	}

	// The backlog counter must appear...
	h.waitForOutput(t, "[1 new]")
	// ...but the message text must not have been interleaved into the
	// transcript while pending was still non-empty.
	if strings.Contains(h.out.String(), "interrupting message") {
		t.Fatalf("envelope text appeared before the pending line was submitted — it must be buffered, not interleaved: %q", h.out.String())
	}
	// And the partially typed text must still be intact on screen.
	if !strings.Contains(h.out.String(), "hello wor") {
		t.Fatalf("pending input did not survive the arriving envelope: %q", h.out.String())
	}

	// Finish typing and submit; this must flush the backlog and send the
	// completed, uncorrupted line.
	h.writeInput(t, "ld\n")
	h.closeInput()

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}

	h.waitForOutput(t, "interrupting message")
	calls := h.getSendCalls()
	if len(calls) != 1 || calls[0].text != "hello world" {
		t.Fatalf("send calls = %+v, want one call with text %q", calls, "hello world")
	}
}

// TestChat_MultipleEnvelopesWhileComposing_CountAccumulatesThenFlushesInOrder
// is the "scrolled up" backpressure requirement in isolation: several
// envelopes arriving during one composition must accumulate a growing
// count rather than each yanking a line into the transcript, and must
// flush in arrival order once the operator stops composing.
func TestChat_MultipleEnvelopesWhileComposing_CountAccumulatesThenFlushesInOrder(t *testing.T) {
	envelopes := make(chan Envelope, 4)
	h := newChatHarness(envelopes)
	done := h.start(t)

	h.writeInput(t, "still typing")
	h.waitForOutput(t, "> still typing")

	first := Envelope{ID: "m-1", Room: "potato", From: "backend", Ts: time.Now(), Text: "first update"}
	envelopes <- first
	h.waitForOutput(t, "[1 new]")

	second := Envelope{ID: "m-2", Room: "potato", From: "backend", Ts: time.Now(), Text: "second update"}
	envelopes <- second
	h.waitForOutput(t, "[2 new]")

	if strings.Contains(h.out.String(), "first update") || strings.Contains(h.out.String(), "second update") {
		t.Fatalf("buffered envelopes leaked into the transcript before the line was submitted: %q", h.out.String())
	}

	// Backspace all the way to empty: per the buffering contract this must
	// flush on its own, without requiring Enter.
	for range "still typing" {
		h.writeInput(t, "\x7f")
	}
	h.waitForOutput(t, "second update")

	out := h.out.String()
	firstIdx := strings.Index(out, "first update")
	secondIdx := strings.Index(out, "second update")
	if firstIdx == -1 || secondIdx == -1 || firstIdx > secondIdx {
		t.Fatalf("backlog did not flush in arrival order: %q", out)
	}

	h.closeInput()
	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// TestChat_EnvelopeWhileIdle_PrintsImmediately proves the non-buffered
// path: with nothing pending, an arriving envelope must render right away,
// not wait for anything.
func TestChat_EnvelopeWhileIdle_PrintsImmediately(t *testing.T) {
	envelopes := make(chan Envelope, 1)
	h := newChatHarness(envelopes)
	done := h.start(t)

	env := Envelope{ID: "m-1", Room: "potato", From: "backend", Ts: time.Now(), Text: "quiet room update"}
	envelopes <- env
	h.waitForOutput(t, "quiet room update")

	h.closeInput()
	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// TestChat_ClosedEnvelopeChannel_EndsRunCleanly proves the subscription
// dying (channel closed, e.g. the daemon connection dropped) ends the chat
// session rather than leaving Run blocked forever.
func TestChat_ClosedEnvelopeChannel_EndsRunCleanly(t *testing.T) {
	envelopes := make(chan Envelope)
	h := newChatHarness(envelopes)
	close(envelopes)
	done := h.start(t)

	if err := h.wait(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	h.closeInput()
}
