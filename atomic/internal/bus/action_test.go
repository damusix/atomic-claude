package bus

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// mustStartTestDaemon wraps client_test.go's startTestDaemon with the
// EnsureDirs call every action_test.go test needs: unlike EnsureDaemon (which
// calls EnsureDirs itself before spawning), these tests bind a real daemon
// directly via startTestDaemon, so nothing else has created <home>/.atomic
// yet — net.Listen fails with "no such file or directory" without it.
func mustStartTestDaemon(t *testing.T, home string) {
	t.Helper()
	if err := EnsureDirs(home); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	if err := startTestDaemon(t, home); err != nil {
		t.Fatalf("startTestDaemon: %v", err)
	}
}

// --- BusAction dispatch ---

func TestBusAction_NoArgs_ExitUsage(t *testing.T) {
	var out bytes.Buffer
	code := BusAction(nil, t.TempDir(), t.TempDir(), &out)
	if code != int(ExitUsage) {
		t.Fatalf("exit code = %d, want %d (ExitUsage)", code, ExitUsage)
	}
}

func TestBusAction_UnknownVerb_ExitUsage(t *testing.T) {
	var out bytes.Buffer
	code := BusAction([]string{"potato"}, t.TempDir(), t.TempDir(), &out)
	if code != int(ExitUsage) {
		t.Fatalf("exit code = %d, want %d (ExitUsage)", code, ExitUsage)
	}
}

// TestBusAction_Chat_MissingRoom_ExitUsage proves chat is wired into
// BusAction's dispatch (checkpoint 6) rather than falling through to the
// unknown-verb case — exercised here via a usage error, since a full chat
// session needs a live daemon and a terminal, both covered by chat_test.go
// and action_test.go's other daemon-backed tests instead.
func TestBusAction_Chat_MissingRoom_ExitUsage(t *testing.T) {
	var out bytes.Buffer
	code := BusAction([]string{"chat"}, t.TempDir(), t.TempDir(), &out)
	if code != int(ExitUsage) {
		t.Errorf("BusAction(%q) exit code = %d, want %d (ExitUsage, missing <room>)", "chat", code, ExitUsage)
	}
}

// --- join ---

func TestJoinAction_Success_AssignsRequestedName(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)

	var out bytes.Buffer
	code := joinAction([]string{"potato", "--as", "backend", "--session", "sess-1"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d; output: %s", code, ExitOK, out.String())
	}
	if got := out.String(); got != "joined potato as backend\n" {
		t.Fatalf("output = %q, want %q", got, "joined potato as backend\n")
	}

	st, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	room, ok := st.LastRoom("sess-1")
	if !ok || room != "potato" {
		t.Fatalf("LastRoom = (%q, %v), want (\"potato\", true)", room, ok)
	}
}

// TestJoinAction_NameCollision_RetrySuffixReported proves the CLI reports
// the daemon-assigned name (room.go's Hub.Join owns the numeric-suffix
// retry itself) rather than the one requested.
func TestJoinAction_NameCollision_RetrySuffixReported(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)

	var out1 bytes.Buffer
	if code := joinAction([]string{"potato", "--as", "backend", "--session", "sess-1"}, home, &out1); code != int(ExitOK) {
		t.Fatalf("first join exit code = %d", code)
	}

	var out2 bytes.Buffer
	code := joinAction([]string{"potato", "--as", "backend", "--session", "sess-2"}, home, &out2)
	if code != int(ExitOK) {
		t.Fatalf("second join exit code = %d, want %d", code, ExitOK)
	}
	want := "joined potato as backend-2 (requested backend was taken)\n"
	if got := out2.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestJoinAction_NameTaken_ThirdAttemptExitsNameTaken(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)

	var discard bytes.Buffer
	if code := joinAction([]string{"potato", "--as", "backend", "--session", "sess-1"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("first join exit code = %d", code)
	}
	if code := joinAction([]string{"potato", "--as", "backend", "--session", "sess-2"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("second join exit code = %d", code)
	}

	code := joinAction([]string{"potato", "--as", "backend", "--session", "sess-3"}, home, &discard)
	if code != int(ExitNameTaken) {
		t.Fatalf("third join exit code = %d, want %d (ExitNameTaken)", code, ExitNameTaken)
	}
}

func TestJoinAction_MissingAsFlag_ExitUsage(t *testing.T) {
	home := testBusHome(t)
	var out bytes.Buffer
	code := joinAction([]string{"potato"}, home, &out)
	if code != int(ExitUsage) {
		t.Fatalf("exit code = %d, want %d (ExitUsage)", code, ExitUsage)
	}
}

func TestJoinAction_InvalidMode_ExitUsage(t *testing.T) {
	home := testBusHome(t)
	var out bytes.Buffer
	code := joinAction([]string{"potato", "--as", "backend", "--mode", "spectate"}, home, &out)
	if code != int(ExitUsage) {
		t.Fatalf("exit code = %d, want %d (ExitUsage)", code, ExitUsage)
	}
}

func TestJoinAction_NoSessionNoOverride_ExitHard(t *testing.T) {
	home := testBusHome(t)
	t.Setenv(sessionEnvVar, "") // absent, per SessionID's treatment of ""

	var out bytes.Buffer
	code := joinAction([]string{"potato", "--as", "backend"}, home, &out)
	if code != int(ExitHard) {
		t.Fatalf("exit code = %d, want %d (ExitHard)", code, ExitHard)
	}
}

// --- leave ---

func TestLeaveAction_NotJoined_ExitNotJoined(t *testing.T) {
	home := testBusHome(t)
	t.Setenv(sessionEnvVar, "sess-1")

	var out bytes.Buffer
	code := leaveAction(nil, home, &out)
	if code != int(ExitNotJoined) {
		t.Fatalf("exit code = %d, want %d (ExitNotJoined)", code, ExitNotJoined)
	}
}

func TestLeaveAction_DefaultsToLastJoinedRoom(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	t.Setenv(sessionEnvVar, "sess-1")

	var discard bytes.Buffer
	if code := joinAction([]string{"potato", "--as", "backend"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("join exit code = %d", code)
	}

	var out bytes.Buffer
	code := leaveAction(nil, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("leave exit code = %d, want %d; output: %s", code, ExitOK, out.String())
	}
	if got := out.String(); got != "left potato\n" {
		t.Fatalf("output = %q, want %q", got, "left potato\n")
	}

	st, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := st.LastRoom("sess-1"); ok {
		t.Fatal("expected LastRoom to be cleared after leaving the only joined room")
	}
}

func TestLeaveAction_ExplicitRoomNotExists_ExitNoRoom(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	t.Setenv(sessionEnvVar, "sess-1")

	var out bytes.Buffer
	code := leaveAction([]string{"nonexistent"}, home, &out)
	if code != int(ExitNoRoom) {
		t.Fatalf("exit code = %d, want %d (ExitNoRoom)", code, ExitNoRoom)
	}
}

// --- send ---

func TestSendAction_MissingArgs_ExitUsage(t *testing.T) {
	home := testBusHome(t)
	var out bytes.Buffer
	code := sendAction([]string{"potato"}, home, &out)
	if code != int(ExitUsage) {
		t.Fatalf("exit code = %d, want %d (ExitUsage)", code, ExitUsage)
	}
}

func TestSendAction_RoomDoesNotExist_ExitNoRoom(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	t.Setenv(sessionEnvVar, "sess-1")

	var out bytes.Buffer
	code := sendAction([]string{"nonexistent", "hello"}, home, &out)
	if code != int(ExitNoRoom) {
		t.Fatalf("exit code = %d, want %d (ExitNoRoom)", code, ExitNoRoom)
	}
}

// TestSendAction_NotJoined_ExitNotJoined proves exit 3 through send
// specifically: the room exists (someone else joined it) but the sending
// session never did.
func TestSendAction_NotJoined_ExitNotJoined(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-member"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}
	t.Setenv(sessionEnvVar, "sess-outsider")

	var out bytes.Buffer
	code := sendAction([]string{"potato", "hello"}, home, &out)
	if code != int(ExitNotJoined) {
		t.Fatalf("exit code = %d, want %d (ExitNotJoined)", code, ExitNotJoined)
	}
}

func TestSendAction_ToFlag_AddressesParsedCorrectly(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "sender", Kind: KindAgent, Session: "sess-sender"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}
	t.Setenv(sessionEnvVar, "sess-sender")

	var out bytes.Buffer
	code := sendAction([]string{"potato", "ping", "--to", "backend, frontend"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d; output: %s", code, ExitOK, out.String())
	}

	resp := dialAndDo(t, addr, Request{Op: OpRecv, Room: "potato"})
	if !resp.OK {
		t.Fatalf("recv: %s", resp.Error)
	}
	var payload struct {
		Envelopes []Envelope `json:"envelopes"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Envelopes) != 1 {
		t.Fatalf("got %d envelopes, want 1", len(payload.Envelopes))
	}
	got := payload.Envelopes[0].To
	want := []string{"backend", "frontend"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("To = %v, want %v", got, want)
	}
}

// TestSendAction_ToOmitted_FYIToWholeRoom proves the load-bearing
// distinction the brief calls out: an omitted --to is an FYI to nobody in
// particular, not an addressee list of one blank name.
func TestSendAction_ToOmitted_FYIToWholeRoom(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "sender", Kind: KindAgent, Session: "sess-sender"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}
	t.Setenv(sessionEnvVar, "sess-sender")

	var out bytes.Buffer
	if code := sendAction([]string{"potato", "fyi"}, home, &out); code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	resp := dialAndDo(t, addr, Request{Op: OpRecv, Room: "potato"})
	if !resp.OK {
		t.Fatalf("recv: %s", resp.Error)
	}
	var payload struct {
		Envelopes []Envelope `json:"envelopes"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Envelopes) != 1 {
		t.Fatalf("got %d envelopes, want 1", len(payload.Envelopes))
	}
	if len(payload.Envelopes[0].To) != 0 {
		t.Fatalf("To = %v, want empty (FYI)", payload.Envelopes[0].To)
	}
}

// TestSendAction_StdinDash_MultilinePayloadIntact proves "-" reads the full
// stdin content, unmangled, end to end through the real dispatch path — not
// merely readText in isolation.
func TestSendAction_StdinDash_MultilinePayloadIntact(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "sender", Kind: KindAgent, Session: "sess-sender"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	payload := "line one\nline two\n\ttabbed line three\n"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin })
	go func() {
		_, _ = w.WriteString(payload)
		w.Close()
	}()

	t.Setenv(sessionEnvVar, "sess-sender")
	var out bytes.Buffer
	code := sendAction([]string{"potato", "-"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d; output: %s", code, ExitOK, out.String())
	}

	resp := dialAndDo(t, addr, Request{Op: OpRecv, Room: "potato"})
	if !resp.OK {
		t.Fatalf("recv: %s", resp.Error)
	}
	var recvPayload struct {
		Envelopes []Envelope `json:"envelopes"`
	}
	if err := json.Unmarshal(resp.Payload, &recvPayload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(recvPayload.Envelopes) != 1 {
		t.Fatalf("got %d envelopes, want 1", len(recvPayload.Envelopes))
	}
	if got := recvPayload.Envelopes[0].Text; got != payload {
		t.Fatalf("Text = %q, want %q (multi-line payload must survive intact)", got, payload)
	}
}

// TestSendAction_DefaultOutput_IsShortConfirmation_NotBareID is the "also
// worth fixing" regression: send used to print only the bare message id
// ("1") to stdout — noise for a human, under-structured for an agent. The
// default output must name what happened, not just echo an opaque token.
func TestSendAction_DefaultOutput_IsShortConfirmation_NotBareID(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "sender", Kind: KindAgent, Session: "sess-sender"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}
	t.Setenv(sessionEnvVar, "sess-sender")

	var out bytes.Buffer
	if code := sendAction([]string{"potato", "hello"}, home, &out); code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	got := strings.TrimSpace(out.String())
	if got == "" {
		t.Fatal("send produced no output at all")
	}
	if _, err := strconv.Atoi(got); err == nil {
		t.Fatalf("output = %q, a bare integer — want a confirmation, not just the id", got)
	}
	if !strings.Contains(got, "potato") {
		t.Fatalf("output = %q, want it to name the room the message went to", got)
	}
}

// TestSendAction_JSONOutput_EmitsFullEnvelope proves --json's job: capture
// the assigned id (for --reply-to) without a second round trip, via the
// full published envelope rather than a bare id field.
func TestSendAction_JSONOutput_EmitsFullEnvelope(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "sender", Kind: KindAgent, Session: "sess-sender"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}
	t.Setenv(sessionEnvVar, "sess-sender")

	var out bytes.Buffer
	if code := sendAction([]string{"potato", "hello", "--json"}, home, &out); code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("output is not a parseable envelope: %v\n%s", err, out.String())
	}
	if env.ID == "" {
		t.Fatal("envelope id is empty")
	}
	if env.Room != "potato" || env.From != "sender" || env.Text != "hello" {
		t.Fatalf("envelope = %+v, want room=potato from=sender text=hello", env)
	}
}

// --- daemon-gone recovery: respawn only, roster restored by rehydration ---
//
// These reproduce the original finding literally — join a room, stop the
// daemon exactly as idle shutdown does, then run a follow-up command — but
// now exercise the current fix: dialDaemonRecovered only respawns
// (recoveryEnsurer points the package-level seam at an in-process daemon so
// recovery never shells out to a real `atomic` binary), and the respawned
// daemon's own Hub.Rehydrate at Serve startup is what restores the roster,
// not a client-side rejoin.

// waitForDaemonGone polls until SocketPath(home) refuses connections,
// bounded by wireTimeout. serveAction --stop returns as soon as the daemon
// acknowledges the shutdown request over the wire — daemon.go's handleConn
// replies, then calls triggerShutdown, but the listener's actual Close()
// runs asynchronously in Serve's own loop goroutine. Proceeding straight
// into a respawn risks racing that still-in-flight teardown (the old,
// background test daemon started by mustStartTestDaemon may still be
// bound), so every test that stops a background test daemon and then
// exercises recovery calls this first to make the respawn genuinely start
// from "daemon gone", not "daemon mid-shutdown".
func waitForDaemonGone(t *testing.T, home string) {
	t.Helper()
	deadline := time.Now().Add(wireTimeout)
	for {
		conn, err := net.DialTimeout("unix", SocketPath(home), 50*time.Millisecond)
		if err != nil {
			return
		}
		conn.Close()
		if time.Now().After(deadline) {
			t.Fatalf("daemon at %s did not shut down within %s", SocketPath(home), wireTimeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// swapRecoveryEnsurer overrides recoveryEnsurer for the duration of the
// test, restoring the production default on cleanup.
func swapRecoveryEnsurer(t *testing.T, spawn func(home string) error) {
	t.Helper()
	orig := recoveryEnsurer
	recoveryEnsurer = func() Ensurer {
		return Ensurer{
			Spawn:        spawn,
			DialTimeout:  time.Second,
			SpawnWait:    2 * time.Second,
			PollInterval: 10 * time.Millisecond,
		}
	}
	t.Cleanup(func() { recoveryEnsurer = orig })
}

// TestWhoAction_DaemonGoneAfterJoin_RecoversAndSucceeds reproduces the
// finding's exact repro: join, then `atomic bus serve --stop` (what idle
// shutdown does after the default window), then `who` — must succeed, not
// exit 6, and the roster must be restored under the original name.
func TestWhoAction_DaemonGoneAfterJoin_RecoversAndSucceeds(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	t.Setenv(sessionEnvVar, "sess-1")

	var discard bytes.Buffer
	if code := joinAction([]string{"potato", "--as", "backend"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("join exit code = %d", code)
	}
	if code := serveAction([]string{"--stop"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("serve --stop exit code = %d", code)
	}
	waitForDaemonGone(t, home)

	var spawnCount int32
	swapRecoveryEnsurer(t, countingSpawn(t, &spawnCount))

	var out bytes.Buffer
	code := whoAction([]string{"potato"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("who exit code = %d, want %d (idle shutdown must be invisible); output: %s", code, ExitOK, out.String())
	}
	if !strings.Contains(out.String(), "backend") {
		t.Fatalf("who output = %q, want it to list backend (respawn's own Hub.Rehydrate must restore the roster)", out.String())
	}
	if got := atomic.LoadInt32(&spawnCount); got != 1 {
		t.Fatalf("spawn invoked %d times, want exactly 1", got)
	}
}

// TestSendAction_DaemonGoneAfterJoin_RecoversAndRetries proves the same
// invisibility for send specifically, since the original finding calls it
// out by name.
func TestSendAction_DaemonGoneAfterJoin_RecoversAndRetries(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	t.Setenv(sessionEnvVar, "sess-1")

	var discard bytes.Buffer
	if code := joinAction([]string{"potato", "--as", "backend"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("join exit code = %d", code)
	}
	if code := serveAction([]string{"--stop"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("serve --stop exit code = %d", code)
	}
	waitForDaemonGone(t, home)

	var spawnCount int32
	swapRecoveryEnsurer(t, countingSpawn(t, &spawnCount))

	var out bytes.Buffer
	code := sendAction([]string{"potato", "hello"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("send exit code = %d, want %d; output: %s", code, ExitOK, out.String())
	}
	if got := atomic.LoadInt32(&spawnCount); got != 1 {
		t.Fatalf("spawn invoked %d times, want exactly 1", got)
	}
}

// TestSendAction_SecondSessionAfterFirstSessionAlreadyRecovered_NoSecondSpawn
// is the two-session case a hands-on repro caught for the old per-session
// re-registration design: only the session whose command happened to
// notice the daemon was gone got rejoined, so a peer's very next send hit
// ExitNotJoined. Rehydration fixes this at the source — the very first
// respawn restores the *whole* persisted roster, so session B's own send
// must succeed without triggering a recovery of its own: the daemon
// already knows about B before B ever asks.
func TestSendAction_SecondSessionAfterFirstSessionAlreadyRecovered_NoSecondSpawn(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)

	var discard bytes.Buffer
	t.Setenv(sessionEnvVar, "sess-a")
	if code := joinAction([]string{"potato", "--as", "alice"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("join sess-a exit code = %d", code)
	}
	t.Setenv(sessionEnvVar, "sess-b")
	if code := joinAction([]string{"potato", "--as", "bob"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("join sess-b exit code = %d", code)
	}

	if code := serveAction([]string{"--stop"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("serve --stop exit code = %d", code)
	}
	waitForDaemonGone(t, home)

	var spawnCount int32
	swapRecoveryEnsurer(t, countingSpawn(t, &spawnCount))

	// Session A notices first and triggers the one respawn — the respawned
	// daemon's Hub.Rehydrate restores both alice and bob in that single pass.
	t.Setenv(sessionEnvVar, "sess-a")
	var whoOut bytes.Buffer
	if code := whoAction([]string{"potato", "--json"}, home, &whoOut); code != int(ExitOK) {
		t.Fatalf("who (sess-a) exit code = %d", code)
	}
	if got := atomic.LoadInt32(&spawnCount); got != 1 {
		t.Fatalf("spawn invoked %d times after sess-a's recovery, want exactly 1", got)
	}
	var members []Member
	if err := json.Unmarshal(whoOut.Bytes(), &members); err != nil {
		t.Fatalf("unmarshal who: %v", err)
	}
	names := map[string]bool{}
	for _, m := range members {
		names[m.Name] = true
	}
	if !names["alice"] || !names["bob"] {
		t.Fatalf("members after one respawn = %+v, want both alice and bob (whole-roster rehydration)", members)
	}

	// Session B's send must succeed with no additional spawn — the daemon
	// is already up and already knows bob, so dialDaemon succeeds outright.
	t.Setenv(sessionEnvVar, "sess-b")
	var out bytes.Buffer
	code := sendAction([]string{"potato", "hello from bob"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("send (sess-b) exit code = %d, want %d; output: %s", code, ExitOK, out.String())
	}
	if got := atomic.LoadInt32(&spawnCount); got != 1 {
		t.Fatalf("spawn invoked %d times after sess-b's send, want still exactly 1 (no per-session recovery left)", got)
	}
}

// TestDialDaemonRecovered_RecoveryFailsPersistently_NoLoop pins the "one
// recovery attempt, then exit 6" discipline: dialDaemonRecovered must call
// EnsureDaemon exactly once (not loop on its own), and a Spawn that never
// actually brings up a working listener must still hit EnsureDaemon's own
// maxSpawnAttempts bound, never more.
func TestDialDaemonRecovered_RecoveryFailsPersistently_NoLoop(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	t.Setenv(sessionEnvVar, "sess-1")

	var discard bytes.Buffer
	if code := joinAction([]string{"potato", "--as", "backend"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("join exit code = %d", code)
	}
	if code := serveAction([]string{"--stop"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("serve --stop exit code = %d", code)
	}
	waitForDaemonGone(t, home)

	var spawnCount int32
	orig := recoveryEnsurer
	recoveryEnsurer = func() Ensurer {
		return Ensurer{
			Spawn:        func(string) error { atomic.AddInt32(&spawnCount, 1); return nil }, // never opens the socket
			DialTimeout:  100 * time.Millisecond,
			SpawnWait:    80 * time.Millisecond,
			PollInterval: 10 * time.Millisecond,
		}
	}
	t.Cleanup(func() { recoveryEnsurer = orig })

	if _, err := dialDaemonRecovered(home); err == nil {
		t.Fatal("expected an error when recovery cannot bring the daemon back")
	}
	if got := atomic.LoadInt32(&spawnCount); got != maxSpawnAttempts {
		t.Fatalf("spawn invoked %d times, want exactly %d (EnsureDaemon's own bound — dialDaemonRecovered must not add a second loop on top)", got, maxSpawnAttempts)
	}
}

// TestServeAction_Restart_RehydratesNamesIncludingSuffixed is the
// action-layer companion to room_test.go's Hub.Rehydrate unit tests: two
// sessions that originally collided on "backend" (the second became
// "backend-2" via Join's numeric-suffix retry) must come back under those
// exact names after a real respawn through serveAction — proving
// rehydration, not a fresh Join, is what restores them (a fresh Join would
// have no way to know "backend-2" was ever taken by anyone other than
// itself).
func TestServeAction_Restart_RehydratesNamesIncludingSuffixed(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)

	var discard bytes.Buffer
	t.Setenv(sessionEnvVar, "sess-1")
	if code := joinAction([]string{"potato", "--as", "backend"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("join sess-1 exit code = %d", code)
	}
	t.Setenv(sessionEnvVar, "sess-2")
	if code := joinAction([]string{"potato", "--as", "backend"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("join sess-2 exit code = %d", code)
	}

	if code := serveAction([]string{"--stop"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("serve --stop exit code = %d", code)
	}
	waitForDaemonGone(t, home)

	// A real restart: a fresh Hub, loaded and rehydrated exactly as
	// startTestDaemon (mirroring serveAction) does for every test daemon.
	mustStartTestDaemon(t, home)

	var whoOut bytes.Buffer
	if code := whoAction([]string{"potato", "--json"}, home, &whoOut); code != int(ExitOK) {
		t.Fatalf("who exit code = %d", code)
	}
	var members []Member
	if err := json.Unmarshal(whoOut.Bytes(), &members); err != nil {
		t.Fatalf("unmarshal who: %v", err)
	}
	names := map[string]bool{}
	for _, m := range members {
		names[m.Name] = true
	}
	if !names["backend"] || !names["backend-2"] {
		t.Fatalf("members after restart = %+v, want backend and backend-2 preserved (no further rename)", members)
	}
}

// --- recv (one-shot) ---

func TestRecvAction_OneShot_JSONOutput(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "sender", Kind: KindAgent, Session: "sess-sender"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-sender", Text: "hello"}); !resp.OK {
		t.Fatalf("seed send: %s", resp.Error)
	}

	var out bytes.Buffer
	code := recvAction([]string{"potato", "--json"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	dec := json.NewDecoder(&out)
	var env Envelope
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("decode line: %v\n%s", err, out.String())
	}
	if env.Text != "hello" {
		t.Fatalf("Text = %q, want %q", env.Text, "hello")
	}
}

func TestRecvAction_OneShot_PlainOutput(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "sender", Kind: KindAgent, Session: "sess-sender"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-sender", Text: "hello"}); !resp.OK {
		t.Fatalf("seed send: %s", resp.Error)
	}

	var out bytes.Buffer
	code := recvAction([]string{"potato"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(out.String(), "hello") {
		t.Fatalf("output does not contain the message text: %q", out.String())
	}
}

// --- recv --follow ---

// publishUntilDelivered repeatedly publishes text to room until a decoded
// envelope arrives on delivered, bounded by deadline. A retry loop rather
// than a fixed sleep: the only race here is "has the subscriber's
// Hub.Subscribe call landed yet", which resolves in microseconds locally
// but has no other signal this test can observe directly.
func publishUntilDelivered(t *testing.T, addr, room, session, text string, delivered <-chan Envelope, deadline time.Duration) Envelope {
	t.Helper()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(deadline)
	for {
		select {
		case env := <-delivered:
			return env
		case <-ticker.C:
			dialAndDo(t, addr, Request{Op: OpSend, Room: room, Session: session, Text: text})
		case <-timeout:
			t.Fatalf("no envelope delivered within %s", deadline)
			return Envelope{}
		}
	}
}

// decodeEnvelopesInto runs a background JSON-line decoder over pr, pushing
// each decoded Envelope onto delivered (non-blocking: a full buffer just
// drops further envelopes, since every caller here only cares about the
// first one). Exits when pr is closed or a decode error occurs.
func decodeEnvelopesInto(pr io.Reader, delivered chan<- Envelope) {
	dec := json.NewDecoder(pr)
	for {
		var env Envelope
		if err := dec.Decode(&env); err != nil {
			return
		}
		select {
		case delivered <- env:
		default:
		}
	}
}

func TestRecvAction_Follow_DeliversPublishedMessageUnderOneSecond(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "sender", Kind: KindAgent, Session: "sess-sender"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	client, err := dialDaemon(home)
	if err != nil {
		t.Fatalf("dialDaemon: %v", err)
	}

	pr, pw := io.Pipe()
	t.Cleanup(func() { pr.Close(); pw.Close() })

	recvDone := make(chan int, 1)
	go func() { recvDone <- recvFollow(client, "potato", "", pw) }()

	delivered := make(chan Envelope, 1)
	go decodeEnvelopesInto(pr, delivered)

	env := publishUntilDelivered(t, addr, "potato", "sess-sender", "hello", delivered, time.Second)
	if env.Text != "hello" {
		t.Fatalf("Text = %q, want %q", env.Text, "hello")
	}
	if env.From != "sender" {
		t.Fatalf("From = %q, want %q", env.From, "sender")
	}

	// Closing the client unblocks recvFollow's subscription channel (it
	// closes on connection close — client.go's Subscribe doc), giving this
	// test a clean, bounded way to confirm recvFollow actually returns
	// rather than leaking the goroutine.
	client.Close()
	select {
	case code := <-recvDone:
		if code != int(ExitOK) {
			t.Fatalf("recvFollow exit code = %d, want %d", code, ExitOK)
		}
	case <-time.After(wireTimeout):
		t.Fatal("recvFollow did not exit after the client was closed")
	}
}

// TestRecvAction_Follow_ExitsZeroOnSIGTERM_NoPartialLine sends a real
// SIGTERM to this test process. This is safe only because
// publishUntilDelivered has already proven, before the signal is sent, that
// recvFollow reached its signal.NotifyContext registration (the first
// statement in the function, strictly before the Subscribe call that has to
// succeed for any envelope to arrive at all) — so the default
// process-terminating disposition for SIGTERM is already disabled for this
// process by the time the signal is sent.
func TestRecvAction_Follow_ExitsZeroOnSIGTERM_NoPartialLine(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "sender", Kind: KindAgent, Session: "sess-sender"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	client, err := dialDaemon(home)
	if err != nil {
		t.Fatalf("dialDaemon: %v", err)
	}

	pr, pw := io.Pipe()
	t.Cleanup(func() { pr.Close(); pw.Close() })

	recvDone := make(chan int, 1)
	go func() { recvDone <- recvFollow(client, "potato", "", pw) }()

	delivered := make(chan Envelope, 1)
	go decodeEnvelopesInto(pr, delivered)

	// A successfully decoded envelope proves the write that carried it was
	// never torn — a partial JSON line would fail to decode.
	env := publishUntilDelivered(t, addr, "potato", "sess-sender", "multi\nline\npayload", delivered, wireTimeout)
	if env.Text != "multi\nline\npayload" {
		t.Fatalf("Text = %q, want the full untruncated payload", env.Text)
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM to self: %v", err)
	}

	select {
	case code := <-recvDone:
		if code != int(ExitOK) {
			t.Fatalf("recvFollow exit code = %d, want %d (ExitOK) on SIGTERM", code, ExitOK)
		}
	case <-time.After(wireTimeout):
		t.Fatal("recvFollow did not exit within the bounded wait after SIGTERM")
	}
}

// --- who ---

func TestWhoAction_JSONOutput(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-1"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	var out bytes.Buffer
	code := whoAction([]string{"potato", "--json"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	var members []Member
	if err := json.Unmarshal(out.Bytes(), &members); err != nil {
		t.Fatalf("output is not parseable JSON: %v\n%s", err, out.String())
	}
	if len(members) != 1 || members[0].Name != "backend" {
		t.Fatalf("members = %+v, want one member named backend", members)
	}
}

func TestWhoAction_RoomNotFound_ExitNoRoom(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)

	var out bytes.Buffer
	code := whoAction([]string{"nonexistent"}, home, &out)
	if code != int(ExitNoRoom) {
		t.Fatalf("exit code = %d, want %d (ExitNoRoom)", code, ExitNoRoom)
	}
}

func TestWhoAction_NoRoomNoSession_ExitHard(t *testing.T) {
	home := testBusHome(t)
	t.Setenv(sessionEnvVar, "")

	var out bytes.Buffer
	code := whoAction(nil, home, &out)
	if code != int(ExitHard) {
		t.Fatalf("exit code = %d, want %d (ExitHard)", code, ExitHard)
	}
}

// --- rooms ---

func TestRoomsAction_JSONOutput_Empty(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)

	var out bytes.Buffer
	code := roomsAction([]string{"--json"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	var rooms []string
	if err := json.Unmarshal(out.Bytes(), &rooms); err != nil {
		t.Fatalf("output is not parseable JSON: %v\n%s", err, out.String())
	}
	if len(rooms) != 0 {
		t.Fatalf("rooms = %v, want empty", rooms)
	}
}

// TestRoomsAction_DaemonUnreachable_ExitUnreachable proves exit code 6 via
// the simplest possible verb: rooms needs no session and no prior join, so
// a fresh home with nothing bound at SocketPath is a clean, fast repro of
// "daemon unreachable". Also proves Finding 1's recovery path does not fire
// here: a session with nothing persisted in bus.json has nothing to
// recover, so this must return the plain unreachable error immediately —
// not pay for a daemon spawn attempt (see recoverAndRejoin's short circuit).
func TestRoomsAction_DaemonUnreachable_ExitUnreachable(t *testing.T) {
	home := testBusHome(t)

	var out bytes.Buffer
	code := roomsAction(nil, home, &out)
	if code != int(ExitUnreachable) {
		t.Fatalf("exit code = %d, want %d (ExitUnreachable)", code, ExitUnreachable)
	}
}

// TestRoomsAction_ReportsMemberCounts is finding 4's regression: rooms must
// report a member count per room, in both table and --json form — potato
// and carrot are given different counts so a fix that reports *a* count but
// always the same (wrong) one still fails this test.
func TestRoomsAction_ReportsMemberCounts(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-1"}); !resp.OK {
		t.Fatalf("seed join potato: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "carrot", Name: "backend", Kind: KindAgent, Session: "sess-2"}); !resp.OK {
		t.Fatalf("seed join carrot: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "carrot", Name: "frontend", Kind: KindAgent, Session: "sess-3"}); !resp.OK {
		t.Fatalf("seed join carrot (second member): %s", resp.Error)
	}

	var jsonOut bytes.Buffer
	if code := roomsAction([]string{"--json"}, home, &jsonOut); code != int(ExitOK) {
		t.Fatalf("--json exit code = %d, want %d", code, ExitOK)
	}
	var rooms []RoomInfo
	if err := json.Unmarshal(jsonOut.Bytes(), &rooms); err != nil {
		t.Fatalf("output is not parseable JSON: %v\n%s", err, jsonOut.String())
	}
	want := []RoomInfo{{Name: "carrot", Members: 2}, {Name: "potato", Members: 1}}
	if len(rooms) != len(want) || rooms[0] != want[0] || rooms[1] != want[1] {
		t.Fatalf("rooms --json = %+v, want %+v", rooms, want)
	}

	var tableOut bytes.Buffer
	if code := roomsAction(nil, home, &tableOut); code != int(ExitOK) {
		t.Fatalf("table exit code = %d, want %d", code, ExitOK)
	}
	table := tableOut.String()
	if !strings.Contains(table, "carrot\t2") || !strings.Contains(table, "potato\t1") {
		t.Fatalf("rooms table output = %q, want it to name each room's member count", table)
	}
}

// --- status ---

func TestStatusAction_NoDaemon_ReportsNotRunning(t *testing.T) {
	home := testBusHome(t)
	t.Setenv(sessionEnvVar, "sess-1")

	var out bytes.Buffer
	code := statusAction([]string{"--json"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	var status busStatus
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("output is not parseable JSON: %v\n%s", err, out.String())
	}
	if status.Daemon != "not running" {
		t.Fatalf("Daemon = %q, want %q", status.Daemon, "not running")
	}
}

func TestStatusAction_DaemonRunning_ReportsJoinedRoom(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	t.Setenv(sessionEnvVar, "sess-1")

	var discard bytes.Buffer
	if code := joinAction([]string{"potato", "--as", "backend"}, home, &discard); code != int(ExitOK) {
		t.Fatalf("join exit code = %d", code)
	}

	var out bytes.Buffer
	code := statusAction([]string{"--json"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	var status busStatus
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("output is not parseable JSON: %v\n%s", err, out.String())
	}
	if status.Daemon != "running" {
		t.Fatalf("Daemon = %q, want %q", status.Daemon, "running")
	}
	if status.Version != ProtocolVersion {
		t.Fatalf("Version = %d, want %d", status.Version, ProtocolVersion)
	}
	if len(status.Rooms) != 1 || status.Rooms[0].Room != "potato" || status.Rooms[0].Name != "backend" {
		t.Fatalf("Rooms = %+v, want one entry {potato backend}", status.Rooms)
	}
}

// --- serve ---

func TestServeAction_StopShutsDownRunningDaemon(t *testing.T) {
	home := testBusHome(t)

	serveDone := make(chan int, 1)
	go func() {
		var out bytes.Buffer
		serveDone <- serveAction([]string{"--idle-shutdown-minutes", "0"}, home, &out)
	}()
	// Safety net: if an assertion below fails before the --stop call runs,
	// this still retires the daemon rather than leaking it for the rest of
	// the test binary's life. serveStop is idempotent against an
	// already-stopped daemon (reports "no daemon running", exit 0).
	t.Cleanup(func() {
		var discard bytes.Buffer
		serveAction([]string{"--stop"}, home, &discard)
	})

	// Poll until the socket accepts connections, bounded — mirrors
	// EnsureDaemon's own spawnAndWaitForSocket pattern (client.go).
	deadline := time.Now().Add(wireTimeout)
	for {
		conn, err := net.DialTimeout("unix", SocketPath(home), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not start listening within %s: %v", wireTimeout, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	var stopOut bytes.Buffer
	code := serveAction([]string{"--stop"}, home, &stopOut)
	if code != int(ExitOK) {
		t.Fatalf("serve --stop exit code = %d, want %d", code, ExitOK)
	}
	if got := stopOut.String(); got != "daemon stopped\n" {
		t.Fatalf("serve --stop output = %q, want %q", got, "daemon stopped\n")
	}

	select {
	case code := <-serveDone:
		if code != int(ExitOK) {
			t.Fatalf("serveAction exit code = %d, want %d after --stop", code, ExitOK)
		}
	case <-time.After(wireTimeout):
		t.Fatal("serveAction did not exit after --stop")
	}

	// The socket file is unlinked on close (net.Listen's default
	// SetUnlinkOnClose behavior — see daemon.go's Serve doc): a dial must
	// now fail, proving the daemon actually stopped rather than merely
	// acknowledging the shutdown request.
	if _, err := net.DialTimeout("unix", SocketPath(home), 200*time.Millisecond); err == nil {
		t.Fatal("expected the socket to be gone after --stop, but a dial succeeded")
	}
}

func TestServeAction_StopNoDaemon_ExitOK(t *testing.T) {
	home := testBusHome(t)
	var out bytes.Buffer
	code := serveAction([]string{"--stop"}, home, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if got := out.String(); got != "no daemon running\n" {
		t.Fatalf("output = %q, want %q", got, "no daemon running\n")
	}
}

// --- serve: malformed persisted state must not block startup ---

// TestServeAction_MalformedBusJSON_DegradesToEmptyRosterAndStillServes pins
// the startup-degrade contract: a corrupt ~/.atomic/bus.json must not
// prevent the daemon from coming up — it starts with an empty roster
// instead of failing outright (docs/spec/atomic-bus.md's daemon-restart
// criteria).
func TestServeAction_MalformedBusJSON_DegradesToEmptyRosterAndStillServes(t *testing.T) {
	home := testBusHome(t)
	if err := EnsureDirs(home); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	if err := os.WriteFile(StatePath(home), []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write malformed bus.json: %v", err)
	}

	serveDone := make(chan int, 1)
	go func() {
		var out bytes.Buffer
		serveDone <- serveAction([]string{"--idle-shutdown-minutes", "0"}, home, &out)
	}()
	t.Cleanup(func() {
		var discard bytes.Buffer
		serveAction([]string{"--stop"}, home, &discard)
	})

	deadline := time.Now().Add(wireTimeout)
	for {
		conn, err := net.DialTimeout("unix", SocketPath(home), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not start listening within %s despite a malformed bus.json: %v", wireTimeout, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp := dialAndDo(t, SocketPath(home), Request{Op: OpRooms})
	if !resp.OK {
		t.Fatalf("rooms: %s", resp.Error)
	}
	var payload struct {
		Rooms []RoomInfo `json:"rooms"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Rooms) != 0 {
		t.Fatalf("rooms = %+v, want none — a malformed bus.json degrades to an empty roster, not an error", payload.Rooms)
	}

	var stopOut bytes.Buffer
	if code := serveAction([]string{"--stop"}, home, &stopOut); code != int(ExitOK) {
		t.Fatalf("serve --stop exit code = %d", code)
	}
	select {
	case code := <-serveDone:
		if code != int(ExitOK) {
			t.Fatalf("serveAction exit code = %d, want %d", code, ExitOK)
		}
	case <-time.After(wireTimeout):
		t.Fatal("serveAction did not exit after --stop")
	}
}

// --- send: unknown addressee warns but still delivers ---

// captureStderr redirects os.Stderr for the duration of fn and returns
// everything written to it. Bus verbs write warnings and errors directly to
// os.Stderr (this file's convention throughout), so asserting on that
// content needs a real fd swap, not merely passing a different io.Writer.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr pipe writer: %v", err)
	}
	os.Stderr = orig
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	return string(b)
}

// TestSendAction_UnknownAddressee_WarnsOnStderrStillExitsOK is the manual
// repro's exact shape: `send potato "ghost" --to nobody-here` must still
// deliver and exit 0 (a named member may legitimately be about to join),
// but the sender must not be told nothing.
func TestSendAction_UnknownAddressee_WarnsOnStderrStillExitsOK(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "sender", Kind: KindAgent, Session: "sess-sender"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}
	t.Setenv(sessionEnvVar, "sess-sender")

	var out bytes.Buffer
	var code int
	stderr := captureStderr(t, func() {
		code = sendAction([]string{"potato", "ghost", "--to", "nobody-here"}, home, &out)
	})
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d (still delivers, still exits 0); output: %s", code, ExitOK, out.String())
	}
	if !strings.Contains(stderr, "nobody-here") {
		t.Fatalf("stderr = %q, want it to name the unknown addressee %q", stderr, "nobody-here")
	}

	resp := dialAndDo(t, addr, Request{Op: OpRecv, Room: "potato"})
	if !resp.OK {
		t.Fatalf("recv: %s", resp.Error)
	}
	var payload struct {
		Envelopes []Envelope `json:"envelopes"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Envelopes) != 1 || payload.Envelopes[0].Text != "ghost" {
		t.Fatalf("envelopes = %+v, want one delivered envelope with text %q — the warning must never withhold delivery", payload.Envelopes, "ghost")
	}
}

func TestSendAction_KnownAddressee_NoStderrWarning(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "sender", Kind: KindAgent, Session: "sess-sender"}); !resp.OK {
		t.Fatalf("seed join sender: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-backend"}); !resp.OK {
		t.Fatalf("seed join backend: %s", resp.Error)
	}
	t.Setenv(sessionEnvVar, "sess-sender")

	var out bytes.Buffer
	var code int
	stderr := captureStderr(t, func() {
		code = sendAction([]string{"potato", "hi", "--to", "backend"}, home, &out)
	})
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty — every addressee is a real member", stderr)
	}
}

// --- small unit helpers ---

func TestParseTo(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"backend", []string{"backend"}},
		{"backend,frontend", []string{"backend", "frontend"}},
		{"backend, frontend , ", []string{"backend", "frontend"}},
	}
	for _, c := range cases {
		got := parseTo(c.in)
		if len(got) != len(c.want) {
			t.Errorf("parseTo(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parseTo(%q) = %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestReadText(t *testing.T) {
	got, err := readText("hello world", strings.NewReader("unused"))
	if err != nil {
		t.Fatalf("readText: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}

	got, err = readText("-", strings.NewReader("line one\nline two\n"))
	if err != nil {
		t.Fatalf("readText: %v", err)
	}
	if got != "line one\nline two\n" {
		t.Fatalf("got %q, want stdin content verbatim", got)
	}
}

func TestParseFlags_PositionalBeforeFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var as, mode string
	fs.StringVar(&as, "as", "", "")
	fs.StringVar(&mode, "mode", "participate", "")

	positional, err := parseFlags(fs, []string{"potato", "--as", "backend", "--mode", "observe"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if len(positional) != 1 || positional[0] != "potato" {
		t.Fatalf("positional = %v, want [potato]", positional)
	}
	if as != "backend" || mode != "observe" {
		t.Fatalf("as=%q mode=%q, want as=backend mode=observe", as, mode)
	}
}

// newTestFlagSet builds a FlagSet with one flag of each shape parseFlags
// has to distinguish: a string ("--text"/"--as"), a bool ("--follow"), and
// a second string ("--mode") so repeated/second-flag cases have something
// to target. Output is silenced so a deliberately-triggered usage error
// doesn't spam test output.
func newTestFlagSet() (*flag.FlagSet, *string, *string, *bool) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var text, mode string
	var follow bool
	fs.StringVar(&text, "text", "", "")
	fs.StringVar(&mode, "mode", "", "")
	fs.BoolVar(&follow, "follow", false, "")
	return fs, &text, &mode, &follow
}

// TestParseFlags_PositionalBeginningWithDash pins the reviewer's repro:
// parseFlags(fs, []string{"myroom", "-1"}) used to fail with "flag
// provided but not defined: -1" because the re-delegating loop re-fired
// stdlib's "starts with -, therefore an unknown flag" rejection on the
// second token. A single-dash positional — a negative number, a diff
// line, a stack-trace frame — must be accepted as text.
func TestParseFlags_PositionalBeginningWithDash(t *testing.T) {
	fs, _, _, _ := newTestFlagSet()

	positional, err := parseFlags(fs, []string{"myroom", "-1"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if len(positional) != 2 || positional[0] != "myroom" || positional[1] != "-1" {
		t.Fatalf("positional = %v, want [myroom -1]", positional)
	}
}

// TestParseFlags_DashAlone_StaysPositional proves "-" — the send verb's
// literal stdin sentinel (readText) — is never mistaken for a flag: it is
// too short to have the "--name" shape at all.
func TestParseFlags_DashAlone_StaysPositional(t *testing.T) {
	fs, _, _, _ := newTestFlagSet()

	positional, err := parseFlags(fs, []string{"potato", "-"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if len(positional) != 2 || positional[0] != "potato" || positional[1] != "-" {
		t.Fatalf("positional = %v, want [potato -]", positional)
	}
}

// TestParseFlags_LongFlagEqualsValue proves "--flag=value" is a
// self-contained token: the value must not be looked for in the next
// argv slot.
func TestParseFlags_LongFlagEqualsValue(t *testing.T) {
	fs, text, _, _ := newTestFlagSet()

	positional, err := parseFlags(fs, []string{"potato", "--text=hello"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if len(positional) != 1 || positional[0] != "potato" {
		t.Fatalf("positional = %v, want [potato]", positional)
	}
	if *text != "hello" {
		t.Fatalf("text = %q, want %q", *text, "hello")
	}
}

// TestParseFlags_LongFlagSpaceValue proves "--flag value" (two argv
// tokens) still works after the rewrite.
func TestParseFlags_LongFlagSpaceValue(t *testing.T) {
	fs, text, _, _ := newTestFlagSet()

	positional, err := parseFlags(fs, []string{"potato", "--text", "hello"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if len(positional) != 1 || positional[0] != "potato" {
		t.Fatalf("positional = %v, want [potato]", positional)
	}
	if *text != "hello" {
		t.Fatalf("text = %q, want %q", *text, "hello")
	}
}

// TestParseFlags_FlagValueBeginningWithDash proves a flag's value token
// is taken verbatim, even when it looks like a flag itself (e.g. a
// negative number passed as --text -1) — the scanner never re-classifies
// the token it already committed to consuming as a value.
func TestParseFlags_FlagValueBeginningWithDash(t *testing.T) {
	fs, text, _, _ := newTestFlagSet()

	positional, err := parseFlags(fs, []string{"potato", "--text", "-1"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if len(positional) != 1 || positional[0] != "potato" {
		t.Fatalf("positional = %v, want [potato]", positional)
	}
	if *text != "-1" {
		t.Fatalf("text = %q, want %q", *text, "-1")
	}
}

// TestParseFlags_DoubleDashTerminatesPositional proves a bare "--" ends
// flag scanning: every token after it is positional, even one shaped
// exactly like a registered flag.
func TestParseFlags_DoubleDashTerminatesPositional(t *testing.T) {
	fs, text, _, _ := newTestFlagSet()

	positional, err := parseFlags(fs, []string{"potato", "--", "--text", "hello"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	want := []string{"potato", "--text", "hello"}
	if len(positional) != len(want) {
		t.Fatalf("positional = %v, want %v", positional, want)
	}
	for i := range want {
		if positional[i] != want[i] {
			t.Fatalf("positional = %v, want %v", positional, want)
		}
	}
	if *text != "" {
		t.Fatalf("text = %q, want unset (the flag-shaped token after -- must not be parsed)", *text)
	}
}

// TestParseFlags_UnknownDoubleDashFlagRejected proves the fix does not
// overcorrect into silently swallowing every unrecognized "--" token as a
// positional — a token shaped like a flag that names nothing registered
// on fs is still a hard usage error.
func TestParseFlags_UnknownDoubleDashFlagRejected(t *testing.T) {
	fs, _, _, _ := newTestFlagSet()

	_, err := parseFlags(fs, []string{"potato", "--bogus", "value"})
	if err == nil {
		t.Fatal("parseFlags: want error for unknown --bogus flag, got nil")
	}
}

// TestParseFlags_RepeatedFlag_LastWins proves a flag passed twice does not
// error or desync the scanner — later wins, matching flag.FlagSet's own
// single-token Set semantics.
func TestParseFlags_RepeatedFlag_LastWins(t *testing.T) {
	fs, text, _, _ := newTestFlagSet()

	positional, err := parseFlags(fs, []string{"potato", "--text", "first", "--text", "second"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if len(positional) != 1 || positional[0] != "potato" {
		t.Fatalf("positional = %v, want [potato]", positional)
	}
	if *text != "second" {
		t.Fatalf("text = %q, want %q (last wins)", *text, "second")
	}
}

// TestParseFlags_MissingValueAtEnd proves a flag with no value token left
// in argv is a clean usage error, not a panic or a swallowed flag name.
func TestParseFlags_MissingValueAtEnd(t *testing.T) {
	fs, _, _, _ := newTestFlagSet()

	_, err := parseFlags(fs, []string{"potato", "--text"})
	if err == nil {
		t.Fatal("parseFlags: want error for --text with no value, got nil")
	}
}

// TestParseFlags_EmptyValue proves "--flag=" (an explicit empty value) is
// accepted, not rejected or confused with a missing value.
func TestParseFlags_EmptyValue(t *testing.T) {
	fs, text, _, _ := newTestFlagSet()

	positional, err := parseFlags(fs, []string{"potato", "--text="})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if len(positional) != 1 || positional[0] != "potato" {
		t.Fatalf("positional = %v, want [potato]", positional)
	}
	if *text != "" {
		t.Fatalf("text = %q, want empty string", *text)
	}
}

// TestParseFlags_BoolFlagDoesNotConsumeNextToken proves a bool flag like
// --follow/--json never swallows the following positional as its value —
// isBoolFlag has to correctly identify it as argument-free.
func TestParseFlags_BoolFlagDoesNotConsumeNextToken(t *testing.T) {
	fs, _, _, follow := newTestFlagSet()

	positional, err := parseFlags(fs, []string{"potato", "--follow", "extra"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !*follow {
		t.Fatal("follow = false, want true")
	}
	if len(positional) != 2 || positional[0] != "potato" || positional[1] != "extra" {
		t.Fatalf("positional = %v, want [potato extra]", positional)
	}
}

// --- halt / resume ---

// TestHaltAction_BlocksAgentSend_SayStillSucceeds_ResumeRestores is the
// action-layer marquee test for the whole checkpoint 5 halt/say asymmetry
// (docs/spec/atomic-bus.md: "`halt` blocks an agent `send` with exit 7
// while `say` still succeeds; `resume` restores").
func TestHaltAction_BlocksAgentSend_SayStillSucceeds_ResumeRestores(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-agent"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	var haltOut bytes.Buffer
	if code := haltAction([]string{"potato", "--text", "stop, wrong approach"}, home, &haltOut); code != int(ExitOK) {
		t.Fatalf("halt exit code = %d, want %d; output: %s", code, ExitOK, haltOut.String())
	}

	t.Setenv(sessionEnvVar, "sess-agent")
	var sendOut bytes.Buffer
	code := sendAction([]string{"potato", "still going"}, home, &sendOut)
	if code != int(ExitHalted) {
		t.Fatalf("agent send exit code = %d, want %d (ExitHalted)", code, ExitHalted)
	}

	var sayOut bytes.Buffer
	if code := sayAction([]string{"potato", "hold on"}, home, &sayOut); code != int(ExitOK) {
		t.Fatalf("say exit code = %d, want %d (say must bypass halt); output: %s", code, ExitOK, sayOut.String())
	}

	var resumeOut bytes.Buffer
	if code := resumeAction([]string{"potato"}, home, &resumeOut); code != int(ExitOK) {
		t.Fatalf("resume exit code = %d, want %d; output: %s", code, ExitOK, resumeOut.String())
	}

	var sendAfterResume bytes.Buffer
	if code := sendAction([]string{"potato", "resumed"}, home, &sendAfterResume); code != int(ExitOK) {
		t.Fatalf("agent send after resume exit code = %d, want %d", code, ExitOK)
	}
}

func TestHaltAction_UnknownRoom_ExitNoRoom(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)

	var out bytes.Buffer
	code := haltAction([]string{"nonexistent"}, home, &out)
	if code != int(ExitNoRoom) {
		t.Fatalf("exit code = %d, want %d (ExitNoRoom)", code, ExitNoRoom)
	}
}

func TestHaltAction_MissingRoom_ExitUsage(t *testing.T) {
	home := testBusHome(t)
	var out bytes.Buffer
	code := haltAction(nil, home, &out)
	if code != int(ExitUsage) {
		t.Fatalf("exit code = %d, want %d (ExitUsage)", code, ExitUsage)
	}
}

func TestResumeAction_UnknownRoom_ExitNoRoom(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)

	var out bytes.Buffer
	code := resumeAction([]string{"nonexistent"}, home, &out)
	if code != int(ExitNoRoom) {
		t.Fatalf("exit code = %d, want %d (ExitNoRoom)", code, ExitNoRoom)
	}
}

// --- say ---

// TestSayAction_PublishesAsHuman_NoRosterMemberAdded proves say's kind and
// its "never occupies a name" contract at the action layer.
func TestSayAction_PublishesAsHuman_NoRosterMemberAdded(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-agent"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	var out bytes.Buffer
	if code := sayAction([]string{"potato", "operator speaking"}, home, &out); code != int(ExitOK) {
		t.Fatalf("say exit code = %d, want %d; output: %s", code, ExitOK, out.String())
	}

	resp := dialAndDo(t, addr, Request{Op: OpRecv, Room: "potato"})
	if !resp.OK {
		t.Fatalf("recv: %s", resp.Error)
	}
	var payload struct {
		Envelopes []Envelope `json:"envelopes"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Envelopes) != 1 || payload.Envelopes[0].FromKind != KindHuman {
		t.Fatalf("envelopes = %+v, want one with FromKind %q", payload.Envelopes, KindHuman)
	}

	whoResp := dialAndDo(t, addr, Request{Op: OpWho, Room: "potato"})
	var whoPayload struct {
		Members []Member `json:"members"`
	}
	if err := json.Unmarshal(whoResp.Payload, &whoPayload); err != nil {
		t.Fatalf("unmarshal who: %v", err)
	}
	if len(whoPayload.Members) != 1 || whoPayload.Members[0].Name != "backend" {
		t.Fatalf("members = %+v, want only backend (say must not add a roster entry)", whoPayload.Members)
	}
}

func TestSayAction_StdinDash_ReadsFullPayload(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-agent"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	payload := "line one\nline two\n"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin })
	go func() {
		_, _ = w.WriteString(payload)
		w.Close()
	}()

	var out bytes.Buffer
	if code := sayAction([]string{"potato", "-"}, home, &out); code != int(ExitOK) {
		t.Fatalf("say exit code = %d, want %d; output: %s", code, ExitOK, out.String())
	}

	resp := dialAndDo(t, addr, Request{Op: OpRecv, Room: "potato"})
	var recvPayload struct {
		Envelopes []Envelope `json:"envelopes"`
	}
	if err := json.Unmarshal(resp.Payload, &recvPayload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(recvPayload.Envelopes) != 1 || recvPayload.Envelopes[0].Text != payload {
		t.Fatalf("Text = %q, want %q", recvPayload.Envelopes[0].Text, payload)
	}
}

func TestSayAction_UnknownAddressee_WarnsOnStderrStillExitsOK(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-agent"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	var out bytes.Buffer
	var code int
	stderr := captureStderr(t, func() {
		code = sayAction([]string{"potato", "ghost", "--to", "nobody-here"}, home, &out)
	})
	if code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d; output: %s", code, ExitOK, out.String())
	}
	if !strings.Contains(stderr, "nobody-here") {
		t.Fatalf("stderr = %q, want it to name the unknown addressee", stderr)
	}
}

func TestSayAction_MissingArgs_ExitUsage(t *testing.T) {
	home := testBusHome(t)
	var out bytes.Buffer
	code := sayAction([]string{"potato"}, home, &out)
	if code != int(ExitUsage) {
		t.Fatalf("exit code = %d, want %d (ExitUsage)", code, ExitUsage)
	}
}

func TestSayAction_RoomDoesNotExist_ExitNoRoom(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)

	var out bytes.Buffer
	code := sayAction([]string{"nonexistent", "hello"}, home, &out)
	if code != int(ExitNoRoom) {
		t.Fatalf("exit code = %d, want %d (ExitNoRoom)", code, ExitNoRoom)
	}
}

// --- tail: resolveTailRooms ---

func TestResolveTailRooms_ExplicitRoom_NoPrefix(t *testing.T) {
	home := testBusHome(t)
	rooms, roomPrefix, err := resolveTailRooms(home, "potato", false)
	if err != nil {
		t.Fatalf("resolveTailRooms: %v", err)
	}
	if len(rooms) != 1 || rooms[0] != "potato" {
		t.Fatalf("rooms = %v, want [potato]", rooms)
	}
	if roomPrefix {
		t.Fatal("roomPrefix = true, want false for an explicit single room")
	}
}

// TestResolveTailRooms_NoExplicit_ExactlyOneRoom_DefaultsToAllRoomsPrefix
// pins docs/spec/atomic-bus.md CP5, quoted verbatim: "[--all-rooms] is the
// default when no room argument is given and exactly one room exists."
func TestResolveTailRooms_NoExplicit_ExactlyOneRoom_DefaultsToAllRoomsPrefix(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-1"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	rooms, roomPrefix, err := resolveTailRooms(home, "", false)
	if err != nil {
		t.Fatalf("resolveTailRooms: %v", err)
	}
	if len(rooms) != 1 || rooms[0] != "potato" {
		t.Fatalf("rooms = %v, want [potato]", rooms)
	}
	if !roomPrefix {
		t.Fatal("roomPrefix = false, want true (spec default with exactly one room)")
	}
}

func TestResolveTailRooms_NoExplicit_MultipleRoomsNoFlag_ExitUsage(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-1"}); !resp.OK {
		t.Fatalf("seed join potato: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "carrot", Name: "backend", Kind: KindAgent, Session: "sess-2"}); !resp.OK {
		t.Fatalf("seed join carrot: %s", resp.Error)
	}

	_, _, err := resolveTailRooms(home, "", false)
	if err == nil {
		t.Fatal("expected an error when multiple rooms exist and neither an explicit room nor --all-rooms is given")
	}
	if exitFromErr(err) != int(ExitUsage) {
		t.Fatalf("exit code = %d, want %d (ExitUsage)", exitFromErr(err), ExitUsage)
	}
}

func TestResolveTailRooms_AllRoomsFlag_MultipleRooms(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-1"}); !resp.OK {
		t.Fatalf("seed join potato: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "carrot", Name: "backend", Kind: KindAgent, Session: "sess-2"}); !resp.OK {
		t.Fatalf("seed join carrot: %s", resp.Error)
	}

	rooms, roomPrefix, err := resolveTailRooms(home, "", true)
	if err != nil {
		t.Fatalf("resolveTailRooms: %v", err)
	}
	if len(rooms) != 2 {
		t.Fatalf("rooms = %v, want 2 entries", rooms)
	}
	if !roomPrefix {
		t.Fatal("roomPrefix = false, want true under --all-rooms")
	}
}

// --- tail: tailAction usage validation ---

func TestTailAction_ExplicitRoomWithAllRoomsFlag_ExitUsage(t *testing.T) {
	home := testBusHome(t)
	var out bytes.Buffer
	code := tailAction([]string{"potato", "--all-rooms"}, home, &out)
	if code != int(ExitUsage) {
		t.Fatalf("exit code = %d, want %d (ExitUsage)", code, ExitUsage)
	}
}

func TestTailAction_TooManyPositionals_ExitUsage(t *testing.T) {
	home := testBusHome(t)
	var out bytes.Buffer
	code := tailAction([]string{"potato", "carrot"}, home, &out)
	if code != int(ExitUsage) {
		t.Fatalf("exit code = %d, want %d (ExitUsage)", code, ExitUsage)
	}
}

// --- tail: tailStream (the subscription loop, factored like recvFollow) ---

// TestTailStream_SeesMessageAddressedToOtherMember_NotInWho is the
// checkpoint's headline success criterion: tail sees mail addressed to
// someone else, and never appears in who.
func TestTailStream_SeesMessageAddressedToOtherMember_NotInWho(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "frontend", Kind: KindAgent, Session: "sess-fe"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	client, err := dialDaemon(home)
	if err != nil {
		t.Fatalf("dialDaemon: %v", err)
	}

	pr, pw := io.Pipe()
	t.Cleanup(func() { pr.Close(); pw.Close() })

	streamDone := make(chan int, 1)
	go func() {
		streamDone <- tailStream(client, []string{"potato"}, "", false, "", true, false, false, home, 80, pw)
	}()

	delivered := make(chan Envelope, 1)
	go decodeEnvelopesInto(pr, delivered)

	env := publishUntilDelivered(t, addr, "potato", "sess-fe", "for backend", delivered, time.Second)
	if env.Text != "for backend" {
		t.Fatalf("Text = %q, want %q", env.Text, "for backend")
	}

	whoResp := dialAndDo(t, addr, Request{Op: OpWho, Room: "potato"})
	var whoPayload struct {
		Members []Member `json:"members"`
	}
	if err := json.Unmarshal(whoResp.Payload, &whoPayload); err != nil {
		t.Fatalf("unmarshal who: %v", err)
	}
	if len(whoPayload.Members) != 1 {
		t.Fatalf("who = %+v, want tail to not appear (only frontend)", whoPayload.Members)
	}

	client.Close()
	select {
	case code := <-streamDone:
		if code != int(ExitOK) {
			t.Fatalf("tailStream exit code = %d, want %d", code, ExitOK)
		}
	case <-time.After(wireTimeout):
		t.Fatal("tailStream did not exit after the client was closed")
	}
}

// TestTailStream_TwoConcurrentTails_BothReceiveEverything_NeitherOccupiesName
// proves two operators can watch the same room simultaneously.
func TestTailStream_TwoConcurrentTails_BothReceiveEverything_NeitherOccupiesName(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-be"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	client1, err := dialDaemon(home)
	if err != nil {
		t.Fatalf("dialDaemon 1: %v", err)
	}
	client2, err := dialDaemon(home)
	if err != nil {
		t.Fatalf("dialDaemon 2: %v", err)
	}

	pr1, pw1 := io.Pipe()
	pr2, pw2 := io.Pipe()
	t.Cleanup(func() { pr1.Close(); pw1.Close(); pr2.Close(); pw2.Close() })

	done1 := make(chan int, 1)
	done2 := make(chan int, 1)
	go func() {
		done1 <- tailStream(client1, []string{"potato"}, "", false, "", true, false, false, home, 80, pw1)
	}()
	go func() {
		done2 <- tailStream(client2, []string{"potato"}, "", false, "", true, false, false, home, 80, pw2)
	}()

	delivered1 := make(chan Envelope, 1)
	delivered2 := make(chan Envelope, 1)
	go decodeEnvelopesInto(pr1, delivered1)
	go decodeEnvelopesInto(pr2, delivered2)

	env1 := publishUntilDelivered(t, addr, "potato", "sess-be", "broadcast", delivered1, time.Second)
	if env1.Text != "broadcast" {
		t.Fatalf("tail 1 Text = %q, want %q", env1.Text, "broadcast")
	}
	select {
	case env2 := <-delivered2:
		if env2.Text != "broadcast" {
			t.Fatalf("tail 2 Text = %q, want %q", env2.Text, "broadcast")
		}
	case <-time.After(time.Second):
		t.Fatal("tail 2 did not receive the envelope")
	}

	whoResp := dialAndDo(t, addr, Request{Op: OpWho, Room: "potato"})
	var whoPayload struct {
		Members []Member `json:"members"`
	}
	if err := json.Unmarshal(whoResp.Payload, &whoPayload); err != nil {
		t.Fatalf("unmarshal who: %v", err)
	}
	if len(whoPayload.Members) != 1 {
		t.Fatalf("who = %+v, want neither tail to occupy a name (only backend)", whoPayload.Members)
	}

	client1.Close()
	client2.Close()
	for _, done := range []chan int{done1, done2} {
		select {
		case code := <-done:
			if code != int(ExitOK) {
				t.Fatalf("tailStream exit code = %d, want %d", code, ExitOK)
			}
		case <-time.After(wireTimeout):
			t.Fatal("tailStream did not exit after the client was closed")
		}
	}
}

// TestTailStream_OnlyAddressedFilter_DropsFYIMessages proves
// --only-addressed's filter.
func TestTailStream_OnlyAddressedFilter_DropsFYIMessages(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-be"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	client, err := dialDaemon(home)
	if err != nil {
		t.Fatalf("dialDaemon: %v", err)
	}
	pr, pw := io.Pipe()
	t.Cleanup(func() { pr.Close(); pw.Close() })

	streamDone := make(chan int, 1)
	go func() {
		streamDone <- tailStream(client, []string{"potato"}, "", true, "", true, false, false, home, 80, pw)
	}()

	delivered := make(chan Envelope, 1)
	go decodeEnvelopesInto(pr, delivered)

	// The FYI send below must never surface on delivered; a retried
	// addressed send is the only thing the filter should let through —
	// polled so a filtered FYI cannot be mistaken for "nothing published
	// yet".
	dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-be", Text: "fyi, ignore"})
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(time.Second)
	var env Envelope
	for {
		select {
		case env = <-delivered:
		case <-ticker.C:
			dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-be", To: []string{"frontend"}, Text: "addressed"})
			continue
		case <-timeout:
			t.Fatal("no addressed envelope delivered within the deadline")
		}
		break
	}
	if env.Text != "addressed" || len(env.To) == 0 {
		t.Fatalf("delivered envelope = %+v, want the addressed message, not the FYI one (--only-addressed must filter the FYI send)", env)
	}

	client.Close()
	<-streamDone
}

// TestTailStream_FromFilter_KeepsOnlyMatchingSender proves --from's filter.
func TestTailStream_FromFilter_KeepsOnlyMatchingSender(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-be"}); !resp.OK {
		t.Fatalf("seed join backend: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "frontend", Kind: KindAgent, Session: "sess-fe"}); !resp.OK {
		t.Fatalf("seed join frontend: %s", resp.Error)
	}

	client, err := dialDaemon(home)
	if err != nil {
		t.Fatalf("dialDaemon: %v", err)
	}
	pr, pw := io.Pipe()
	t.Cleanup(func() { pr.Close(); pw.Close() })

	streamDone := make(chan int, 1)
	go func() {
		streamDone <- tailStream(client, []string{"potato"}, "", false, "frontend", true, false, false, home, 80, pw)
	}()

	delivered := make(chan Envelope, 1)
	go decodeEnvelopesInto(pr, delivered)

	dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-be", Text: "from backend, must be filtered"})
	env := publishUntilDelivered(t, addr, "potato", "sess-fe", "from frontend", delivered, time.Second)
	if env.From != "frontend" || env.Text != "from frontend" {
		t.Fatalf("delivered envelope = %+v, want only frontend's message", env)
	}

	client.Close()
	<-streamDone
}

// TestTailStream_JSONOutput_EmitsJSONL proves --json's shape.
func TestTailStream_JSONOutput_EmitsJSONL(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-be"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	client, err := dialDaemon(home)
	if err != nil {
		t.Fatalf("dialDaemon: %v", err)
	}
	pr, pw := io.Pipe()
	t.Cleanup(func() { pr.Close(); pw.Close() })

	streamDone := make(chan int, 1)
	go func() {
		streamDone <- tailStream(client, []string{"potato"}, "", false, "", true, false, false, home, 80, pw)
	}()

	delivered := make(chan Envelope, 1)
	go decodeEnvelopesInto(pr, delivered)

	env := publishUntilDelivered(t, addr, "potato", "sess-be", "hello", delivered, time.Second)
	if env.Text != "hello" {
		t.Fatalf("Text = %q, want %q", env.Text, "hello")
	}

	client.Close()
	<-streamDone
}

// TestTailStream_PlainOutput_NoColour_NoANSIEscapes is the byte-level
// success criterion: "Piping tail to a non-tty emits no ANSI escapes —
// assert on the bytes."
func TestTailStream_PlainOutput_NoColour_NoANSIEscapes(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-be"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	client, err := dialDaemon(home)
	if err != nil {
		t.Fatalf("dialDaemon: %v", err)
	}

	// io.Pipe, not a bare bytes.Buffer shared across goroutines: a
	// *bytes.Buffer is not safe for the concurrent write (tailStream's
	// goroutine) + read (this goroutine) this test needs — io.Pipe
	// synchronizes the handoff instead of racing on shared memory.
	pr, pw := io.Pipe()
	t.Cleanup(func() { pr.Close(); pw.Close() })

	streamDone := make(chan int, 1)
	// colour=false here matches what tailAction computes for any
	// non-terminal destination (isTerminalWriter's own *os.File check).
	go func() {
		streamDone <- tailStream(client, []string{"potato"}, "", false, "", false, false, false, home, 80, pw)
	}()

	captured := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := pr.Read(buf)
		captured <- buf[:n]
	}()

	if resp := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-be", Text: "hello"}); !resp.OK {
		t.Fatalf("send: %s", resp.Error)
	}

	var got []byte
	select {
	case got = <-captured:
	case <-time.After(wireTimeout):
		t.Fatal("no output captured within the deadline")
	}

	client.Close()
	select {
	case <-streamDone:
	case <-time.After(wireTimeout):
		t.Fatal("tailStream did not exit after the client was closed")
	}

	if len(got) == 0 {
		t.Fatal("no output captured")
	}
	if bytes.ContainsRune(got, '\x1b') {
		t.Fatalf("output = %q, contains an ANSI escape byte with colour disabled", got)
	}
}

// --- who: kind visible in table and --json (docs/spec/atomic-bus.md CP5) ---

func TestWhoAction_TableOutput_ShowsKind(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-1"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	var out bytes.Buffer
	if code := whoAction([]string{"potato"}, home, &out); code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(out.String(), KindAgent) {
		t.Fatalf("table output = %q, want it to contain kind %q", out.String(), KindAgent)
	}
}

func TestWhoAction_JSONOutput_ShowsKind(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	addr := SocketPath(home)
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-1"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	var out bytes.Buffer
	if code := whoAction([]string{"potato", "--json"}, home, &out); code != int(ExitOK) {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	var members []Member
	if err := json.Unmarshal(out.Bytes(), &members); err != nil {
		t.Fatalf("output is not parseable JSON: %v\n%s", err, out.String())
	}
	if len(members) != 1 || members[0].Kind != KindAgent {
		t.Fatalf("members = %+v, want one member with Kind %q", members, KindAgent)
	}
}

// --- isTerminalWriter / terminalWidth ---

func TestIsTerminalWriter_NonFileWriter_ReturnsFalse(t *testing.T) {
	var buf bytes.Buffer
	if isTerminalWriter(&buf) {
		t.Fatal("expected a *bytes.Buffer to never be reported as a terminal")
	}
}

func TestTerminalWidth_NonFileWriter_ReturnsDefault(t *testing.T) {
	var buf bytes.Buffer
	if got := terminalWidth(&buf); got != defaultLineWidth {
		t.Fatalf("terminalWidth = %d, want %d (defaultLineWidth)", got, defaultLineWidth)
	}
}

// --- chat: end-to-end wiring (join, live subscription, /quit's leave) ---
//
// chat_test.go's own tests drive Chat directly against spies and no daemon
// at all — the right level for exercising the redraw/backlog logic without
// paying for a socket round trip on every assertion. This one test instead
// proves chatAction's wiring: join really happens (visible in a real who
// round trip, kind human), and /quit really leaves (bus.json cleared),
// exactly as joinAction/leaveAction's own tests prove for their verbs.

// waitForBufferContains polls buf until it contains want, bounded by
// wireTimeout — chatAction's join confirmation and Chat's own redraw output
// land on buf from a background goroutine, so this is the same
// poll-until-condition pattern publishUntilDelivered/waitForDaemonGone use
// above for the identical cross-goroutine reason.
func waitForBufferContains(t *testing.T, buf *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(wireTimeout)
	for {
		if strings.Contains(buf.String(), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("output did not contain %q within %s; got:\n%s", want, wireTimeout, buf.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestChatAction_JoinsRunsQuit_EndToEnd(t *testing.T) {
	home := testBusHome(t)
	mustStartTestDaemon(t, home)
	t.Setenv(sessionEnvVar, "sess-operator")

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = pr
	t.Cleanup(func() { os.Stdin = origStdin })

	out := &syncBuffer{}
	codeCh := make(chan int, 1)
	go func() { codeCh <- chatAction([]string{"potato", "--as", "operator"}, home, out) }()

	waitForBufferContains(t, out, "joined potato as operator")

	addr := SocketPath(home)
	resp := dialAndDo(t, addr, Request{Op: OpWho, Room: "potato"})
	if !resp.OK {
		t.Fatalf("who: %s", resp.Error)
	}
	var whoPayload struct {
		Members []Member `json:"members"`
	}
	if err := json.Unmarshal(resp.Payload, &whoPayload); err != nil {
		t.Fatalf("unmarshal who: %v", err)
	}
	found := false
	for _, m := range whoPayload.Members {
		if m.Name == "operator" && m.Kind == KindHuman {
			found = true
		}
	}
	if !found {
		t.Fatalf("who = %+v, want operator listed with kind %q", whoPayload.Members, KindHuman)
	}

	if _, err := pw.Write([]byte("/quit\n")); err != nil {
		t.Fatalf("write /quit: %v", err)
	}

	select {
	case code := <-codeCh:
		if code != int(ExitOK) {
			t.Fatalf("chatAction exit code = %d, want %d; output: %s", code, ExitOK, out.String())
		}
	case <-time.After(wireTimeout):
		t.Fatal("chatAction did not return after /quit")
	}
	_ = pw.Close()

	if !strings.Contains(out.String(), "left potato") {
		t.Fatalf("output = %q, want a \"left potato\" confirmation", out.String())
	}

	st, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := st.LastRoom("sess-operator"); ok {
		t.Fatal("expected LastRoom cleared after /quit's leave")
	}
}
