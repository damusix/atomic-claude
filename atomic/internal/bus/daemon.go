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

// daemon holds one Serve call's live state: the listener it accepts on and
// the room hub it dispatches into.
type daemon struct {
	hub       *Hub
	ln        net.Listener
	now       func() time.Time
	startedAt time.Time
	pid       int

	shutdown chan struct{}
	shutOnce sync.Once
}

// Serve accepts connections on ln and dispatches each one's op against hub
// until ctx is cancelled or a client sends "shutdown" — no timer ever
// retires the daemon on its own. now is injectable for ping's reported
// start time; pass nil for real time. ln is a parameter rather than
// something Serve calls net.Listen for itself, so tests can drive the
// daemon over a listener bound to a t.TempDir() socket path without
// spawning a process. The production `bus serve` CLI verb is the thin
// wrapper: it creates the real net.Listen("unix", sockPath) listener,
// passes it here, and is responsible for the socket file and the spawn-
// lock's lifecycle around this call — Serve only owns what happens once it
// has a live listener. Closing ln unlinks the socket automatically for a
// *net.UnixListener created via net.Listen (Go's default SetUnlinkOnClose
// behavior), so Serve does not need to unlink it separately.
func Serve(ctx context.Context, ln net.Listener, hub *Hub, now func() time.Time) error {
	if now == nil {
		now = time.Now
	}

	d := &daemon{
		hub:       hub,
		ln:        ln,
		now:       now,
		startedAt: now(),
		pid:       os.Getpid(),
		shutdown:  make(chan struct{}),
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
			// on either of the shutdown paths above racing this goroutine.
			return err

		case conn := <-connCh:
			go d.handleConn(ctx, conn)
		}
	}
}

func (d *daemon) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		// Malformed frame or the client hung up before sending one;
		// nothing to reply to.
		return
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
	case OpSay:
		respond(enc, d.handleSay(req))
	case OpWho:
		respond(enc, d.handleWho(req))
	case OpRooms:
		respond(enc, d.handleRooms())
	case OpHalt:
		respond(enc, d.handleHalt(req))
	case OpResume:
		respond(enc, d.handleResume(req))
	case OpPrune:
		respond(enc, d.handlePrune(req))
	case OpClose:
		respond(enc, d.handleClose(req))
	case OpShutdown:
		respond(enc, Response{OK: true})
		d.triggerShutdown()
	case OpRecv:
		// req.Session is client-claimed, wire input — nothing upstream of
		// this dispatch has verified the connection sending it actually is
		// that session (see Hub.SessionIsMember's doc). Honoring it verbatim
		// let a session that names nobody in this room — or nobody yet —
		// keep sitting in r.subs, ready to attach itself to whichever member
		// later joins under that name and permanently defeat that member's
		// staleness check. A claim that matches nobody currently in the room
		// is downgraded to "" — anonymous, exactly like tail's own
		// subscriptions.
		session := req.Session
		if session != "" && !d.hub.SessionIsMember(req.Room, session) {
			session = ""
		}
		d.subscribe(ctx, conn, enc, []string{req.Room}, session, req.SkipSelf)
	case OpTail:
		// Filter application (only_addressed / from, per protocol.go's
		// Request.Filters doc) is an action-layer concern (render/action);
		// this dispatch delivers every envelope on the subscribed rooms
		// unfiltered. tail never joins and holds no session of its own
		// (docs/design/atomic-bus.md's decision #5), so it always
		// subscribes with session "" and skipSelf false — it has nothing to
		// self-skip, and must keep seeing its own says/sends regardless.
		d.subscribe(ctx, conn, enc, req.Rooms, "", false)
	default:
		respond(enc, Response{OK: false, Code: ExitUsage, Error: fmt.Sprintf("bus: unknown op %q (want one of %v)", req.Op, AllOps)})
	}
}

// subscribe implements the shared shape of the subscription ops (recv,
// tail): reply {"ok":true}, then keep the connection open writing one
// Envelope frame per line — flushed immediately via a direct net.Conn.Write,
// never buffered — until the client disconnects or the daemon shuts down. A
// subscriber sees only what is published after this call registers with the
// hub; there is no backlog. This is also why the wire protocol is line-
// delimited rather than request-scoped: a subscription's response is
// unbounded.
func (d *daemon) subscribe(ctx context.Context, conn net.Conn, enc *json.Encoder, rooms []string, session string, skipSelf bool) {
	ch := make(chan Envelope, subscriberBuffer)
	unsubs := make([]func(), 0, len(rooms))
	for _, room := range rooms {
		unsubs = append(unsubs, d.hub.Subscribe(room, ch, session, skipSelf))
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	if err := enc.Encode(Response{OK: true}); err != nil {
		return
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
		case env, ok := <-ch:
			if !ok {
				// Hub.Close closes every live subscriber's channel after
				// delivering the closing envelope (room.go's Close doc) so
				// the listener learns why it stopped, not merely that it
				// did. A closed channel's receive always succeeds
				// immediately with the zero value; without this check the
				// loop above would spin forever writing empty frames
				// instead of ending the connection.
				return
			}
			if err := writeFrame(conn, env); err != nil {
				return
			}
		}
	}
}

// writeFrame writes one JSON envelope per line and flushes immediately —
// a buffered frame that never arrives is the whole recv feature failing
// silently.
func writeFrame(conn net.Conn, env Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = conn.Write(b)
	return err
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
	name, err := d.hub.Join(req.Room, req.Name, req.Mode, req.Kind, req.Session, req.Repo, req.Realm)
	if err != nil {
		return errorResponse(err)
	}
	payload, _ := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: name})
	return Response{OK: true, Payload: payload}
}

// handleLeave's payload reports whether Leave dropped the room entirely
// (its last member, and no live subscriber — see Hub.Leave/dropIfEmpty) so
// leaveAction can also clear any orphaned persisted halt state for a room
// that no longer exists.
func (d *daemon) handleLeave(req Request) Response {
	dropped, err := d.hub.Leave(req.Room, req.Session)
	if err != nil {
		return errorResponse(err)
	}
	payload, _ := json.Marshal(struct {
		RoomDropped bool `json:"room_dropped,omitempty"`
	}{RoomDropped: dropped})
	return Response{OK: true, Payload: payload}
}

// handleClose is `close`'s daemon-side handler: publishes the closing
// envelope, evicts the roster, and drops the room via Hub.Close.
func (d *daemon) handleClose(req Request) Response {
	if err := d.hub.Close(req.Room); err != nil {
		return errorResponse(err)
	}
	return Response{OK: true}
}

// handleSend's payload carries the full published Envelope, not merely its
// id — see docs/spec/atomic-bus.md's "send prints a bare message id, under-
// structured for an agent" fix: --json needs the whole envelope (to capture
// the id for --reply-to without a second round trip), and the plain-text
// path derives its short confirmation from the same payload. UnknownTo
// names any env.To entry that is not currently a room member (Hub.
// UnknownAddressees) — Publish above still delivers unconditionally; this
// is only the signal the client uses to warn the sender on stderr. Checked
// against env.To, not req.To: Hub.Publish resolves a suffix/substring --to
// entry to its full member name before returning env (room.go's
// resolveAddressees), so checking the caller's original, shorter req.To
// here would wrongly warn about an addressee that was in fact resolved and
// delivered.
func (d *daemon) handleSend(req Request) Response {
	env, err := d.hub.Publish(req.Room, req.Session, req.To, req.ReplyTo, req.Text)
	if err != nil {
		return errorResponse(err)
	}
	payload, _ := json.Marshal(struct {
		Envelope  Envelope `json:"envelope"`
		UnknownTo []string `json:"unknown_to,omitempty"`
	}{Envelope: env, UnknownTo: d.hub.UnknownAddressees(req.Room, env.To)})
	return Response{OK: true, Payload: payload}
}

// handleSay is `say`'s daemon-side handler: publishes as the human operator via
// Hub.PublishAsOperator, which — unlike handleSend's Hub.Publish — needs no
// prior roster membership. UnknownTo mirrors handleSend's own warning-not-
// withholding contract, checked against the same resolved env.To for the same
// reason handleSend's own comment gives. req.Name and req.Kind are deliberately
// ignored. Pinning the sender in the CLI wrapper is not enough: the socket is
// the trust boundary, and any local process can speak the protocol directly. An
// earlier version forwarded both fields, which let a raw OpSay claim an
// existing agent's name with kind "agent" and publish into a halted room.
func (d *daemon) handleSay(req Request) Response {
	env, err := d.hub.PublishAsOperator(req.Room, req.To, req.ReplyTo, req.Text)
	if err != nil {
		return errorResponse(err)
	}
	payload, _ := json.Marshal(struct {
		Envelope  Envelope `json:"envelope"`
		UnknownTo []string `json:"unknown_to,omitempty"`
	}{Envelope: env, UnknownTo: d.hub.UnknownAddressees(req.Room, env.To)})
	return Response{OK: true, Payload: payload}
}

// whoJSON is the shape both handleWho's wire payload and whoAction's --json
// CLI output use: the room's own halt state alongside its member list, so
// an agent or script reading `who --json` learns "halted and why" the same
// way `rooms`/`status` do, without a second round trip. Halted/HaltReason
// are room-level, not per-member — carried once here rather than
// denormalized onto every Member row, which would silently hide the flag
// for a halted room with zero current members (a `tail` or `recv` can hold
// that room open with no member rows to read it from — see
// Hub.dropIfEmpty's own subscriber guard).
type whoJSON struct {
	Halted     bool     `json:"halted"`
	HaltReason string   `json:"halt_reason,omitempty"`
	Members    []Member `json:"members"`
}

func (d *daemon) handleWho(req Request) Response {
	members, err := d.hub.Who(req.Room)
	if err != nil {
		return errorResponse(err)
	}
	// Room already confirmed to exist by Who above; IsHalted's own error
	// return is unreachable here in practice, ignored rather than
	// re-derived into a second failure mode.
	halted, reason, _ := d.hub.IsHalted(req.Room)
	payload, _ := json.Marshal(whoJSON{Halted: halted, HaltReason: reason, Members: members})
	return Response{OK: true, Payload: payload}
}

func (d *daemon) handleRooms() Response {
	payload, _ := json.Marshal(struct {
		Rooms []RoomInfo `json:"rooms"`
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

// handlePrune's payload names the members Hub.Prune actually removed, so
// the CLI can report exactly what changed rather than a bare "ok" — an
// empty Removed list on success means the room had nothing stale to reap.
func (d *daemon) handlePrune(req Request) Response {
	removed, err := d.hub.Prune(req.Room)
	if err != nil {
		return errorResponse(err)
	}
	payload, _ := json.Marshal(struct {
		Removed []string `json:"removed"`
	}{Removed: removed})
	return Response{OK: true, Payload: payload}
}
