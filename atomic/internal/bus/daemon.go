package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// DefaultIdleWindow is the idle-shutdown window a production `atomic bus
// serve` invocation uses when the operator hasn't overridden it. Serve
// itself takes an explicit window (0 disables) rather than defaulting
// internally, so tests can shrink it without touching this constant.
const DefaultIdleWindow = 10 * time.Minute

// daemon holds one Serve call's live state: the listener it accepts on,
// the room hub it dispatches into, and the idle-shutdown bookkeeping.
type daemon struct {
	hub        *Hub
	ln         net.Listener
	now        func() time.Time
	idleWindow time.Duration
	startedAt  time.Time
	pid        int

	mu        sync.Mutex
	subs      int // open recv/tail subscriptions; idle shutdown arms at 0
	pending   int // accepted connections not yet classified; see pendingResolved
	idleTimer *time.Timer

	idleFired chan struct{}
	shutdown  chan struct{}
	shutOnce  sync.Once
}

// Serve accepts connections on ln and dispatches each one's op against hub
// until: ctx is cancelled, a client sends "shutdown", or idleWindow
// elapses with zero open subscriptions (0 disables idle shutdown). now is
// injectable for ping's reported start time; pass nil for real time.
//
// ln is a parameter rather than something Serve calls net.Listen for
// itself, so tests can drive the daemon over a listener bound to a
// t.TempDir() socket path without spawning a process. The production `bus
// serve` CLI verb (checkpoint 4) is the thin wrapper: it creates the real
// net.Listen("unix", sockPath) listener, passes it here, and is
// responsible for the socket file and the spawn-lock's lifecycle around
// this call — Serve only owns what happens once it has a live listener.
// Closing ln unlinks the socket automatically for a *net.UnixListener
// created via net.Listen (Go's default SetUnlinkOnClose behavior), so
// Serve does not need to unlink it separately.
func Serve(ctx context.Context, ln net.Listener, hub *Hub, idleWindow time.Duration, now func() time.Time) error {
	if now == nil {
		now = time.Now
	}

	d := &daemon{
		hub:        hub,
		ln:         ln,
		now:        now,
		idleWindow: idleWindow,
		startedAt:  now(),
		pid:        os.Getpid(),
		idleFired:  make(chan struct{}, 1),
		shutdown:   make(chan struct{}),
	}
	return d.run(ctx)
}

func (d *daemon) run(ctx context.Context) error {
	connCh := make(chan net.Conn)
	acceptErr := make(chan error, 1)
	go func() {
		for {
			c, err := d.ln.Accept()
			if err != nil {
				acceptErr <- err
				return
			}
			connCh <- c
		}
	}()

	// Zero subscriptions at startup: arm immediately.
	d.armIdleTimer()

	for {
		select {
		case <-ctx.Done():
			_ = d.ln.Close()
			return ctx.Err()

		case <-d.shutdown:
			_ = d.ln.Close()
			return nil

		case err := <-acceptErr:
			// Listener closed out from under the accept loop — expected
			// on any of the shutdown paths above racing this goroutine.
			return err

		case conn := <-connCh:
			// A plain request/reply connection does not touch the idle
			// timer — only a subscription (recv --follow, tail, chat)
			// counts as "activity" for idle-shutdown purposes, per
			// docs/design/atomic-bus.md's idle-shutdown flow. Arming and
			// disarming is handled entirely by subscriptionOpened/Closed.
			//
			// pending is bumped here, synchronously in the accept loop,
			// before dispatch — a connection is "busy" from the instant
			// it's accepted, not from whenever handleConn's goroutine
			// happens to get scheduled and finish decoding its request.
			// See pendingResolved's doc for why this exists.
			d.mu.Lock()
			d.pending++
			d.mu.Unlock()
			go d.handleConn(ctx, conn)

		case <-d.idleFired:
			// Re-check under lock: a subscription may have opened, or an
			// accepted connection may still be mid-classification (see
			// pending above), after this timer fired but before this case
			// ran. A busy re-check restarts the countdown rather than
			// shutting down out from under it, or leaving idle-shutdown
			// permanently disarmed for the rest of this daemon's life —
			// the one-shot timer that just fired is spent either way, so
			// "busy" must re-arm a fresh one itself rather than assume
			// some later event (e.g. subscriptionClosed) will.
			d.mu.Lock()
			empty := d.subs == 0 && d.pending == 0
			d.mu.Unlock()
			if !empty {
				d.armIdleTimer()
				continue
			}
			_ = d.ln.Close()
			return nil
		}
	}
}

func (d *daemon) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		// Malformed frame or the client hung up before sending one;
		// nothing to reply to. This connection was never going to become
		// a subscription, so its accept-time busy marker clears here.
		d.pendingResolved()
		return
	}

	// A connection is "classified" the instant its op is known: a
	// subscription (recv --follow, tail) transfers the accept-time
	// pending marker into d.subs via subscriptionOpened below; every
	// other op clears it outright, since a plain request/reply was never
	// going to count as idle-shutdown activity either way.
	if !(req.Op == OpTail || (req.Op == OpRecv && req.Follow)) {
		d.pendingResolved()
	}

	enc := json.NewEncoder(conn)

	switch req.Op {
	case OpPing:
		respond(enc, d.handlePing())
	case OpJoin:
		respond(enc, d.handleJoin(req))
	case OpLeave:
		respond(enc, d.handleLeave(req))
	case OpSend:
		respond(enc, d.handleSend(req))
	case OpWho:
		respond(enc, d.handleWho(req))
	case OpRooms:
		respond(enc, d.handleRooms())
	case OpHalt:
		respond(enc, d.handleHalt(req))
	case OpResume:
		respond(enc, d.handleResume(req))
	case OpShutdown:
		respond(enc, Response{OK: true})
		d.triggerShutdown()
	case OpRecv:
		if req.Follow {
			d.subscribe(ctx, conn, enc, []string{req.Room}, req.Since)
			return
		}
		respond(enc, d.handleRecvOnce(req))
	case OpTail:
		// Filter application (only_addressed / from, per protocol.go's
		// Request.Filters doc) is a checkpoint 5 concern (render/action
		// layer); this checkpoint delivers every envelope on the
		// subscribed rooms unfiltered.
		d.subscribe(ctx, conn, enc, req.Rooms, req.Since)
	default:
		respond(enc, Response{OK: false, Code: ExitUsage, Error: fmt.Sprintf("bus: unknown op %q", req.Op)})
	}
}

// subscribe implements the shared shape of the subscription ops (recv
// --follow, tail): reply {"ok":true}, replay each room's backlog since the
// given cursor, then keep the connection open writing one Envelope frame
// per line — flushed immediately via a direct net.Conn.Write, never
// buffered — until the client disconnects or the daemon shuts down. This
// is why the wire protocol is line-delimited rather than request-scoped
// (docs/design/atomic-bus.md, "Wire protocol"): a subscription's response
// is unbounded.
func (d *daemon) subscribe(ctx context.Context, conn net.Conn, enc *json.Encoder, rooms []string, since string) {
	ch := make(chan Envelope, subscriberBuffer)
	unsubs := make([]func(), 0, len(rooms))
	for _, room := range rooms {
		unsubs = append(unsubs, d.hub.Subscribe(room, ch))
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	d.subscriptionOpened()
	defer d.subscriptionClosed()

	if err := enc.Encode(Response{OK: true}); err != nil {
		return
	}

	// Replay each room's backlog before switching to live delivery, so a
	// --since catch-up and the live stream can never interleave into
	// out-of-order or duplicate frames on the wire.
	for _, room := range rooms {
		backlog, err := d.hub.Since(room, since)
		if err != nil {
			continue
		}
		for _, env := range backlog {
			if err := writeFrame(conn, env); err != nil {
				return
			}
		}
	}

	// A subscribing client sends nothing further on this connection, so
	// the only way to notice it hung up is a blocking Read that returns
	// once the peer closes. Run it in its own goroutine and select on the
	// result so a dead client unblocks this loop instead of leaving it
	// parked on <-ch forever.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		var buf [1]byte
		_, _ = conn.Read(buf[:])
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.shutdown:
			return
		case <-closed:
			return
		case env := <-ch:
			if err := writeFrame(conn, env); err != nil {
				return
			}
		}
	}
}

// writeFrame writes one JSON envelope per line and flushes immediately —
// a buffered frame that never arrives is the whole recv --follow feature
// failing silently.
func writeFrame(conn net.Conn, env Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = conn.Write(b)
	return err
}

func (d *daemon) subscriptionOpened() {
	d.mu.Lock()
	d.pending-- // was bumped at accept time; now classified as a subscription
	d.subs++
	if d.idleTimer != nil {
		d.idleTimer.Stop()
		d.idleTimer = nil
	}
	d.mu.Unlock()
}

// pendingResolved clears the accept-time busy marker (see the accept loop
// in run) for a connection once it's known not to be a subscription — or
// the daemon never even learns its op, on a malformed frame or an early
// hangup. Without this, the idle-fire handler's d.subs==0 check can't see
// a connection that's been accepted but is still being decoded/dispatched,
// so the daemon could close the listener and return mid-handshake,
// orphaning that connection's handleConn goroutine.
func (d *daemon) pendingResolved() {
	d.mu.Lock()
	d.pending--
	d.mu.Unlock()
}

func (d *daemon) subscriptionClosed() {
	d.mu.Lock()
	d.subs--
	empty := d.subs == 0
	d.mu.Unlock()
	if empty {
		d.armIdleTimer()
	}
}

// armIdleTimer (re)starts the idle-shutdown countdown. A window <= 0
// disables idle shutdown entirely (docs/spec/atomic-bus.md: "0 disables").
func (d *daemon) armIdleTimer() {
	if d.idleWindow <= 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.idleTimer != nil {
		d.idleTimer.Stop()
	}
	d.idleTimer = time.AfterFunc(d.idleWindow, func() {
		select {
		case d.idleFired <- struct{}{}:
		default:
		}
	})
}

func (d *daemon) triggerShutdown() {
	d.shutOnce.Do(func() { close(d.shutdown) })
}

func respond(enc *json.Encoder, resp Response) {
	_ = enc.Encode(resp)
}

// errorResponse converts a bus error into a wire Response, carrying the
// exit code the client should terminate with. Error-to-exit-code mapping
// lives in exactly this one place — see protocol.go's Response.Code doc —
// so a caller never has to re-derive a code from Error's free text.
func errorResponse(err error) Response {
	var busErr *Error
	if errors.As(err, &busErr) {
		return Response{OK: false, Code: busErr.Code, Error: busErr.Msg}
	}
	return Response{OK: false, Code: ExitHard, Error: err.Error()}
}

func (d *daemon) handlePing() Response {
	payload, _ := json.Marshal(struct {
		Version int       `json:"version"`
		Pid     int       `json:"pid"`
		Started time.Time `json:"started"`
	}{Version: ProtocolVersion, Pid: d.pid, Started: d.startedAt})
	return Response{OK: true, Payload: payload}
}

func (d *daemon) handleJoin(req Request) Response {
	name, err := d.hub.Join(req.Room, req.Name, req.Mode, req.Kind, req.Session)
	if err != nil {
		return errorResponse(err)
	}
	payload, _ := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: name})
	return Response{OK: true, Payload: payload}
}

func (d *daemon) handleLeave(req Request) Response {
	if err := d.hub.Leave(req.Room, req.Session); err != nil {
		return errorResponse(err)
	}
	return Response{OK: true}
}

func (d *daemon) handleSend(req Request) Response {
	env, err := d.hub.Publish(req.Room, req.Session, req.To, req.ReplyTo, req.Text)
	if err != nil {
		return errorResponse(err)
	}
	payload, _ := json.Marshal(struct {
		ID string `json:"id"`
	}{ID: env.ID})
	return Response{OK: true, Payload: payload}
}

func (d *daemon) handleRecvOnce(req Request) Response {
	envs, err := d.hub.Since(req.Room, req.Since)
	if err != nil {
		return errorResponse(err)
	}
	payload, _ := json.Marshal(struct {
		Envelopes []Envelope `json:"envelopes"`
	}{Envelopes: envs})
	return Response{OK: true, Payload: payload}
}

func (d *daemon) handleWho(req Request) Response {
	members, err := d.hub.Who(req.Room)
	if err != nil {
		return errorResponse(err)
	}
	payload, _ := json.Marshal(struct {
		Members []Member `json:"members"`
	}{Members: members})
	return Response{OK: true, Payload: payload}
}

func (d *daemon) handleRooms() Response {
	payload, _ := json.Marshal(struct {
		Rooms []string `json:"rooms"`
	}{Rooms: d.hub.Rooms()})
	return Response{OK: true, Payload: payload}
}

func (d *daemon) handleHalt(req Request) Response {
	if err := d.hub.Halt(req.Room, req.Text); err != nil {
		return errorResponse(err)
	}
	return Response{OK: true}
}

func (d *daemon) handleResume(req Request) Response {
	if err := d.hub.Resume(req.Room, req.Text); err != nil {
		return errorResponse(err)
	}
	return Response{OK: true}
}
