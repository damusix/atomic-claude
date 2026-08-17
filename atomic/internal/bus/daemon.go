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

// daemon holds one Serve call's live state.
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
// until ctx is cancelled or a client sends "shutdown" — no timer retires the
// daemon on its own. now is injectable for ping's reported start time; pass nil
// for real time. ln is a parameter so tests can drive the daemon over a
// listener bound to a temp-dir socket without spawning a process; the `bus
// serve` verb owns the real listener, the socket file, and the spawn lock
// around this call. Closing a net.Listen unix listener unlinks the socket, so
// Serve never unlinks it separately.
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
			// Listener closed out from under the accept loop — expected on either
			// shutdown path above racing this goroutine.
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
		// Malformed frame, or the client hung up before sending one.
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
		// req.Session is client-claimed wire input that nothing upstream has
		// verified (Hub.SessionIsMember). Honoring it verbatim let a session
		// naming nobody in this room sit in r.subs, ready to attach to whoever
		// later joins under that name and permanently defeat their staleness
		// check. A claim matching no current member is downgraded to anonymous.
		session := req.Session
		if session != "" && !d.hub.SessionIsMember(req.Room, session) {
			session = ""
		}
		d.subscribe(ctx, conn, enc, []string{req.Room}, session, req.SkipSelf)
	case OpTail:
		// Filters are an action-layer concern; this dispatch delivers every
		// envelope on the subscribed rooms unfiltered. tail holds no session, so
		// it subscribes anonymously with skipSelf false — it has nothing to
		// self-skip and must keep seeing its own says.
		d.subscribe(ctx, conn, enc, req.Rooms, "", false)
	default:
		respond(enc, Response{OK: false, Code: ExitUsage, Error: fmt.Sprintf("bus: unknown op %q (want one of %v)", req.Op, AllOps)})
	}
}

// subscribe implements the shared shape of recv and tail: reply {"ok":true},
// then hold the connection open writing one Envelope frame per line, flushed
// immediately, until the client disconnects or the daemon shuts down. A
// subscriber sees only what is published after this registers; there is no
// backlog. An unbounded subscription response is also why the wire protocol is
// line-delimited rather than request-scoped.
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

	// A subscribing client sends nothing further, so the only way to notice it
	// hung up is a blocking Read that returns when the peer closes. Its own
	// goroutine plus a select means a dead client unblocks this loop instead of
	// leaving it parked on <-ch forever.
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
				// Hub.Close closes every subscriber channel after delivering the
				// closing envelope. A closed channel receives the zero value
				// immediately, so without this check the loop spins writing empty
				// frames instead of ending the connection.
				return
			}
			if err := writeFrame(conn, env); err != nil {
				return
			}
		}
	}
}

// writeFrame writes one JSON envelope per line and flushes immediately — a
// buffered frame that never arrives is the whole recv feature failing silently.
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

// errorResponse converts a bus error into a wire Response carrying the exit code
// the client should terminate with. This is the one place that mapping lives.
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

// handleLeave's payload reports whether Leave dropped the room entirely, so
// leaveAction can clear any orphaned persisted halt state for it.
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

// handleClose publishes the closing envelope, evicts the roster, and drops the
// room via Hub.Close.
func (d *daemon) handleClose(req Request) Response {
	if err := d.hub.Close(req.Room); err != nil {
		return errorResponse(err)
	}
	return Response{OK: true}
}

// handleSend's payload carries the full Envelope, not merely its id: --json
// needs the whole thing to capture the id for --reply-to without a second round
// trip. UnknownTo names any addressee not currently in the room — Publish still
// delivers unconditionally; this only drives the client's stderr warning. It is
// checked against env.To, not req.To, because Hub.Publish resolves a substring
// --to entry to its full member name, and checking the caller's shorter
// original would wrongly warn about an addressee that was in fact delivered.
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

// handleSay publishes as the human operator via Hub.PublishAsOperator, which
// needs no prior membership. req.Name and req.Kind are deliberately ignored:
// pinning the sender in the CLI wrapper is not enough, since the socket is the
// trust boundary and any local process can speak the protocol directly. An
// earlier version forwarded both, which let a raw OpSay claim an existing
// agent's name and publish into a halted room.
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

// whoJSON is the shape handleWho's payload and whoAction's --json output share,
// so a script reading `who --json` learns "halted and why" without a second
// round trip. Halted/HaltReason are room-level, carried once rather than
// denormalized onto every Member row — which would hide the flag entirely for a
// halted room with zero members, a state a live tail or recv can hold open.
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
	// Room already confirmed to exist by Who above, so IsHalted's error return is
	// unreachable here rather than a second failure mode.
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

// handlePrune's payload names the members actually removed, so the CLI reports
// what changed rather than a bare "ok"; empty on success means nothing stale.
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
