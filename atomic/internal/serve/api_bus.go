// /api/bus/*: a web chat client over the atomic bus daemon, so the UI can
// watch rooms and speak into one as a human member.
//
// Read endpoints dial the daemon and degrade to "not running" when none is up
// — opening the chat page never spawns one. Only the paths expressing operator
// intent (join, send) go through bus.EnsureDaemon, so opening a channel works
// from cold exactly as `atomic bus join` does.
//
// These are serve's only write endpoints; see docs/spec/atomic-serve.md for
// how they narrow its read-only contract.
package serve

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/damusix/atomic-claude/atomic/internal/bus/remote"
)

// BusAPIOptions configures NewAPIBusHandler.
type BusAPIOptions struct {
	// Home holds the bus socket, state, and room logs under <home>/.atomic.
	Home string
	// TargetDir is the position the web member's name is stacked from, so the
	// chat member is named like a CLI join in that directory.
	TargetDir string
	// DialTimeout bounds each daemon round trip. Zero means 2s.
	DialTimeout time.Duration
	// EnsureDaemon is the seam for the spawn-capable paths; tests substitute a
	// Dial-only variant so a handler test can never fork a real daemon.
	EnsureDaemon func(home string) (*bus.Client, error)
}

type busAPIHandler struct {
	home         string
	targetDir    string
	dialTimeout  time.Duration
	ensureDaemon func(home string) (*bus.Client, error)

	// session is derived from TargetDir, not the pid, so a restarted serve
	// reclaims its roster entry instead of minting -2, -3, … each time. It is
	// per-instance, not per-tab: every tab speaks as the same operator.
	session string

	mu     sync.Mutex
	joined map[string]string // room → assigned member name
}

// NewAPIBusHandler returns the handler for every /api/bus/* route.
func NewAPIBusHandler(opts BusAPIOptions) http.Handler {
	h := &busAPIHandler{
		home:         opts.Home,
		targetDir:    opts.TargetDir,
		dialTimeout:  opts.DialTimeout,
		ensureDaemon: opts.EnsureDaemon,
		session:      webSessionID(opts.TargetDir),
		joined:       map[string]string{},
	}
	if h.dialTimeout == 0 {
		h.dialTimeout = 2 * time.Second
	}
	if h.ensureDaemon == nil {
		h.ensureDaemon = bus.EnsureDaemon
	}
	return h
}

// webSessionID derives the per-target-dir identity; see the session field for
// why it must survive a restart.
func webSessionID(targetDir string) string {
	sum := sha256.Sum256([]byte(targetDir))
	return "serve-web-" + hex.EncodeToString(sum[:4])
}

// isLoopbackPeer fails closed on an unparseable address.
func isLoopbackPeer(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (h *busAPIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// send and say publish as the human operator, and say bypasses room halts
	// — an escalation over the rest of serve's LAN-safe browsing. The gate is
	// the TCP peer alone, never a proxy-forwarded header, so --host 0.0.0.0
	// cannot carry this reach.
	if !isLoopbackPeer(r.RemoteAddr) {
		writeAPIError(w, http.StatusForbidden, "bus chat is loopback-only; connect from the serving machine")
		return
	}
	route := strings.TrimPrefix(r.URL.Path, "/api/bus/")
	switch {
	case route == "status" && r.Method == http.MethodGet:
		h.handleStatus(w)
	case route == "rooms" && r.Method == http.MethodGet:
		h.handleRooms(w)
	case route == "who" && r.Method == http.MethodGet:
		h.handleWho(w, r)
	case route == "sessions" && r.Method == http.MethodGet:
		h.handleSessions(w, r)
	case route == "transcript" && r.Method == http.MethodGet:
		h.handleTranscript(w, r)
	case route == "log" && r.Method == http.MethodGet:
		h.handleLog(w, r)
	case route == "tail" && r.Method == http.MethodGet:
		h.handleTail(w, r)
	case route == "join" && r.Method == http.MethodPost:
		if rejectCrossOrigin(w, r) {
			return
		}
		h.handleJoin(w, r)
	case route == "send" && r.Method == http.MethodPost:
		if rejectCrossOrigin(w, r) {
			return
		}
		h.handleSend(w, r)
	case route == "say" && r.Method == http.MethodPost:
		if rejectCrossOrigin(w, r) {
			return
		}
		h.handleSay(w, r)
	case route == "halt" && r.Method == http.MethodPost:
		if rejectCrossOrigin(w, r) {
			return
		}
		h.handleHalt(w, r)
	case route == "resume" && r.Method == http.MethodPost:
		if rejectCrossOrigin(w, r) {
			return
		}
		h.handleResume(w, r)
	case route == "leave" && r.Method == http.MethodPost:
		if rejectCrossOrigin(w, r) {
			return
		}
		h.handleLeave(w, r)
	case route == "close" && r.Method == http.MethodPost:
		if rejectCrossOrigin(w, r) {
			return
		}
		h.handleClose(w, r)
	case route == "end" && r.Method == http.MethodPost:
		if rejectCrossOrigin(w, r) {
			return
		}
		h.handleEnd(w, r)
	default:
		writeAPIError(w, http.StatusNotFound, "unknown bus route")
	}
}

// do runs one request against host, or Dial-only against the local socket
// when host is empty: a daemon that is not running is reported, never
// spawned. host comes from the caller's own request (query param or JSON
// body field) — serve persists no per-room membership the way the CLI's
// bus.json does, so every call names its own target.
func (h *busAPIHandler) do(host string, req bus.Request) (bus.Response, error) {
	if host != "" {
		return bus.DoRemote(h.home, host, req)
	}
	client, err := bus.Dial(h.home, h.dialTimeout)
	if err != nil {
		return bus.Response{}, err
	}
	defer client.Close()
	return client.Do(req)
}

// doEnsure spawns the daemon first if none is live; only join and send use
// it. A remote host skips EnsureDaemon entirely — a failed remote dial must
// never spawn a local daemon and quietly split the bus, matching
// bus/action.go's joinAction and sendAction.
func (h *busAPIHandler) doEnsure(host string, req bus.Request) (bus.Response, error) {
	if host != "" {
		return bus.DoRemote(h.home, host, req)
	}
	client, err := h.ensureDaemon(h.home)
	if err != nil {
		return bus.Response{}, err
	}
	defer client.Close()
	return client.Do(req)
}

// writeBusError maps a bus failure onto an HTTP status; a dial failure means
// the daemon is not running.
func writeBusError(w http.ResponseWriter, err error) {
	var busErr *bus.Error
	if !errors.As(err, &busErr) {
		writeAPIError(w, http.StatusServiceUnavailable, "bus daemon not running: "+err.Error())
		return
	}
	status := http.StatusInternalServerError
	switch busErr.Code {
	case bus.ExitUsage:
		status = http.StatusBadRequest
	case bus.ExitNoRoom, bus.ExitNotJoined:
		status = http.StatusNotFound
	case bus.ExitHalted:
		status = http.StatusConflict
	case bus.ExitUnreachable:
		status = http.StatusServiceUnavailable
	}
	writeAPIError(w, status, busErr.Msg)
}

type busStatusResponse struct {
	Running bool   `json:"running"`
	Name    string `json:"name"`
	Repo    string `json:"repo,omitempty"`
	Realm   string `json:"realm,omitempty"`
}

func (h *busAPIHandler) handleStatus(w http.ResponseWriter) {
	resp := busStatusResponse{}
	if name, repo, realm, err := bus.JoinIdentity(h.home, h.targetDir, "web"); err == nil {
		resp.Name, resp.Repo, resp.Realm = name, repo, realm
	}
	if _, err := h.do("", bus.Request{Op: bus.OpPing}); err == nil {
		resp.Running = true
	}
	writeAPIJSON(w, resp)
}

// busRoomEntry adds the host discriminator to bus.RoomInfo: local and
// remote rooms share one list, so two rooms named "potato" on different
// buses need Host to tell them apart. Host is empty for a local room.
type busRoomEntry struct {
	bus.RoomInfo
	Host string `json:"host,omitempty"`
}

type busRoomsResponse struct {
	Running bool           `json:"running"`
	Rooms   []busRoomEntry `json:"rooms"`
}

// handleRooms fans out across the local daemon and every configured remote,
// tagging each room with the host it came from. Running reflects only the
// local daemon, matching the "daemon up/down" badge it feeds; a machine with
// no [bus.remotes] table iterates zero remotes and returns exactly what it
// always has.
func (h *busAPIHandler) handleRooms(w http.ResponseWriter) {
	rooms := []busRoomEntry{}
	running := false

	if resp, err := h.do("", bus.Request{Op: bus.OpRooms}); err == nil {
		running = true
		var payload struct {
			Rooms []bus.RoomInfo `json:"rooms"`
		}
		if json.Unmarshal(resp.Payload, &payload) == nil {
			for _, r := range payload.Rooms {
				rooms = append(rooms, busRoomEntry{RoomInfo: r})
			}
		}
	}

	if remotes, err := remote.Remotes(h.home); err == nil {
		names := make([]string, 0, len(remotes))
		for name := range remotes {
			names = append(names, name)
		}
		sort.Strings(names)

		// Fanned out concurrently under h.dialTimeout each, rather than one
		// remote at a time under remote.Client's general default: the
		// frontend polls this route every 4 seconds, and a sequential fan-out
		// bounded only by that default made one unreachable remote stack
		// every later poll behind it.
		byName := make([][]busRoomEntry, len(names))
		var wg sync.WaitGroup
		for i, name := range names {
			wg.Add(1)
			go func(i int, name string) {
				defer wg.Done()
				resp, err := bus.DoRemoteTimeout(h.home, name, bus.Request{Op: bus.OpRooms}, h.dialTimeout)
				if err != nil {
					return
				}
				var payload struct {
					Rooms []bus.RoomInfo `json:"rooms"`
				}
				if json.Unmarshal(resp.Payload, &payload) != nil {
					return
				}
				entries := make([]busRoomEntry, 0, len(payload.Rooms))
				for _, r := range payload.Rooms {
					entries = append(entries, busRoomEntry{RoomInfo: r, Host: name})
				}
				byName[i] = entries
			}(i, name)
		}
		wg.Wait()
		for _, entries := range byName {
			rooms = append(rooms, entries...)
		}
	}

	writeAPIJSON(w, busRoomsResponse{Running: running, Rooms: rooms})
}

type busWhoResponse struct {
	Halted     bool         `json:"halted"`
	HaltReason string       `json:"halt_reason,omitempty"`
	Members    []bus.Member `json:"members"`
}

func (h *busAPIHandler) handleWho(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if !requireRoom(w, room) {
		return
	}
	resp, err := h.do(r.URL.Query().Get("host"), bus.Request{Op: bus.OpWho, Room: room})
	if err != nil {
		writeBusError(w, err)
		return
	}
	var payload busWhoResponse
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "parse who payload: "+err.Error())
		return
	}
	if payload.Members == nil {
		payload.Members = []bus.Member{}
	}
	writeAPIJSON(w, payload)
}

type busLogResponse struct {
	Envelopes []bus.Envelope `json:"envelopes"`
}

// maxLogLineBytes is a body plus headroom for the envelope's metadata.
const maxLogLineBytes = bus.MaxTextBytes + 64*1024

// handleLog backfills a room's transcript. A remote room has no log file on
// this machine, and the wire protocol carries no bulk-history op (OpTail is
// live-only, OpRead answers one id at a time) — see
// docs/design/atomic-bus-network.md, "Local versus remote". So a remote
// request returns an empty backlog by design and lets the SSE tail fill the
// transcript live. A local room is untouched: still the direct file read.
func (h *busAPIHandler) handleLog(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if !requireRoom(w, room) {
		return
	}
	host := r.URL.Query().Get("host")

	if host != "" {
		writeAPIJSON(w, busLogResponse{Envelopes: []bus.Envelope{}})
		return
	}

	n := 200
	if raw := r.URL.Query().Get("n"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 1000 {
			n = parsed
		}
	}
	envs, err := readRoomLogTail(h.home, room, n)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "read room log: "+err.Error())
		return
	}
	writeAPIJSON(w, busLogResponse{Envelopes: envs})
}

// readRoomLogTail returns a room's last n envelopes. A missing log is an empty
// history, and one malformed line does not fail the whole backfill.
func readRoomLogTail(home, room string, n int) ([]bus.Envelope, error) {
	f, err := os.Open(bus.RoomLogPath(home, room))
	if err != nil {
		if os.IsNotExist(err) {
			return []bus.Envelope{}, nil
		}
		return nil, err
	}
	defer f.Close()

	tail := make([]bus.Envelope, 0, n)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxLogLineBytes)
	for scanner.Scan() {
		var env bus.Envelope
		if json.Unmarshal(scanner.Bytes(), &env) != nil {
			continue
		}
		if len(tail) == n {
			copy(tail, tail[1:])
			tail = tail[:n-1]
		}
		tail = append(tail, env)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return tail, nil
}

func (h *busAPIHandler) handleTail(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if !requireRoom(w, room) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	if host := r.URL.Query().Get("host"); host != "" {
		h.handleTailRemote(w, r, flusher, host, room)
		return
	}

	client, err := bus.Dial(h.home, h.dialTimeout)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "bus daemon not running")
		return
	}
	defer client.Close()
	ch, err := client.Subscribe(bus.Request{Op: bus.OpTail, Rooms: []string{room}})
	if err != nil {
		writeBusError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case env, open := <-ch:
			if !open {
				return
			}
			b, err := json.Marshal(env)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

// handleTailRemote is handleTail's counterpart to bus/action.go's
// recvRemoteStream: remote.Client.Stream already reconnects under backoff on
// any fault, so there is no local reconnect loop to drive, only ctx
// cancellation (the browser closing the EventSource) to watch for. Every
// sealed line is checked with remote.ProbeHandshake before being forwarded —
// the subscribe handshake's own confirmation frame precedes the Envelope
// stream once per connection, including once per Stream reconnect, so
// forwarding it verbatim would render a fake envelope.
func (h *busAPIHandler) handleTailRemote(w http.ResponseWriter, r *http.Request, flusher http.Flusher, host, room string) {
	remotes, err := remote.Remotes(h.home)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "read remotes config: "+err.Error())
		return
	}
	cfg, ok := remotes[host]
	if !ok {
		writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("unknown remote %q", host))
		return
	}
	client, err := remote.NewClient(cfg)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	body, err := json.Marshal(bus.Request{Op: bus.OpTail, Rooms: []string{room}})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	frames, _ := client.Stream(r.Context(), body)
	for frame := range frames {
		if isHandshake, _ := remote.ProbeHandshake(frame); isHandshake {
			continue
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", frame); err != nil {
			return
		}
		flusher.Flush()
	}
}

type busJoinBody struct {
	Room string `json:"room"`
	Host string `json:"host,omitempty"`
}

func (h *busAPIHandler) handleJoin(w http.ResponseWriter, r *http.Request) {
	var body busJoinBody
	if !decodeBusBody(w, r, &body) || !requireRoom(w, body.Room) {
		return
	}
	name, err := h.join(body.Room, body.Host)
	if err != nil {
		writeBusError(w, err)
		return
	}
	writeAPIJSON(w, map[string]string{"name": name})
}

// join joins room, creating it if absent, and caches the assigned name.
func (h *busAPIHandler) join(room, host string) (string, error) {
	name, repo, realm, err := bus.JoinIdentity(h.home, h.targetDir, "web")
	if err != nil {
		return "", err
	}
	resp, err := h.doEnsure(host, bus.Request{
		Op: bus.OpJoin, Room: room, Name: name, Mode: "participate",
		Kind: bus.KindHuman, Session: h.session, Repo: repo, Realm: realm,
	})
	if err != nil {
		return "", err
	}
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		return "", fmt.Errorf("parse join payload: %w", err)
	}
	h.mu.Lock()
	h.joined[room] = payload.Name
	h.mu.Unlock()
	return payload.Name, nil
}

type busSendBody struct {
	Room    string   `json:"room"`
	Text    string   `json:"text"`
	To      []string `json:"to"`
	ReplyTo string   `json:"reply_to"`
	Host    string   `json:"host,omitempty"`
}

type busSendResponse struct {
	Envelope  bus.Envelope `json:"envelope"`
	UnknownTo []string     `json:"unknown_to,omitempty"`
	Name      string       `json:"name"`
}

func (h *busAPIHandler) handleSend(w http.ResponseWriter, r *http.Request) {
	var body busSendBody
	if !decodeBusBody(w, r, &body) || !requireRoom(w, body.Room) {
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeAPIError(w, http.StatusBadRequest, "missing text")
		return
	}

	// Sending into an unjoined room creates the membership, and the room, on
	// the way. A cached membership can also be stale — the room may have been
	// closed — so a not-joined failure invalidates it and rejoins once.
	h.mu.Lock()
	name, alreadyJoined := h.joined[body.Room]
	h.mu.Unlock()
	if !alreadyJoined {
		var err error
		if name, err = h.join(body.Room, body.Host); err != nil {
			writeBusError(w, err)
			return
		}
	}

	req := bus.Request{Op: bus.OpSend, Room: body.Room, Session: h.session, To: body.To, ReplyTo: body.ReplyTo, Text: body.Text}
	resp, err := h.doEnsure(body.Host, req)
	var busErr *bus.Error
	if err != nil && alreadyJoined && errors.As(err, &busErr) && busErr.Code == bus.ExitNotJoined {
		h.mu.Lock()
		delete(h.joined, body.Room)
		h.mu.Unlock()
		if name, err = h.join(body.Room, body.Host); err == nil {
			resp, err = h.doEnsure(body.Host, req)
		}
	}
	if err != nil {
		writeBusError(w, err)
		return
	}

	var payload busSendResponse
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "parse send payload: "+err.Error())
		return
	}
	payload.Name = name
	writeAPIJSON(w, payload)
}

func (h *busAPIHandler) handleSay(w http.ResponseWriter, r *http.Request) {
	var body busSendBody
	if !decodeBusBody(w, r, &body) || !requireRoom(w, body.Room) {
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeAPIError(w, http.StatusBadRequest, "missing text")
		return
	}
	resp, err := h.do(body.Host, bus.Request{Op: bus.OpSay, Room: body.Room, To: body.To, Text: body.Text})
	if err != nil {
		writeBusError(w, err)
		return
	}
	var payload busSendResponse
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "parse say payload: "+err.Error())
		return
	}
	writeAPIJSON(w, payload)
}

type busRoomBody struct {
	Room   string `json:"room"`
	Reason string `json:"reason"`
	Name   string `json:"name"`
	Host   string `json:"host,omitempty"`
}

func (h *busAPIHandler) handleHalt(w http.ResponseWriter, r *http.Request) {
	var body busRoomBody
	if !decodeBusBody(w, r, &body) || !requireRoom(w, body.Room) {
		return
	}
	if _, err := h.do(body.Host, bus.Request{Op: bus.OpHalt, Room: body.Room, Text: body.Reason}); err != nil {
		writeBusError(w, err)
		return
	}
	writeAPIJSON(w, map[string]bool{"halted": true})
}

func (h *busAPIHandler) handleResume(w http.ResponseWriter, r *http.Request) {
	var body busRoomBody
	if !decodeBusBody(w, r, &body) || !requireRoom(w, body.Room) {
		return
	}
	if _, err := h.do(body.Host, bus.Request{Op: bus.OpResume, Room: body.Room}); err != nil {
		writeBusError(w, err)
		return
	}
	writeAPIJSON(w, map[string]bool{"halted": false})
}

func (h *busAPIHandler) handleLeave(w http.ResponseWriter, r *http.Request) {
	var body busRoomBody
	if !decodeBusBody(w, r, &body) || !requireRoom(w, body.Room) {
		return
	}
	h.mu.Lock()
	delete(h.joined, body.Room)
	h.mu.Unlock()
	if _, err := h.do(body.Host, bus.Request{Op: bus.OpLeave, Room: body.Room, Session: h.session}); err != nil {
		writeBusError(w, err)
		return
	}
	writeAPIJSON(w, map[string]bool{"left": true})
}

func (h *busAPIHandler) handleClose(w http.ResponseWriter, r *http.Request) {
	var body busRoomBody
	if !decodeBusBody(w, r, &body) || !requireRoom(w, body.Room) {
		return
	}
	h.mu.Lock()
	delete(h.joined, body.Room)
	h.mu.Unlock()
	if _, err := h.do(body.Host, bus.Request{Op: bus.OpClose, Room: body.Room}); err != nil {
		writeBusError(w, err)
		return
	}
	// Rehydrate replays bus.json on the next daemon start, so a room left there
	// comes back. `atomic bus close` does this same second half.
	h.clearPersisted(body.Room, "", body.Host)
	writeAPIJSON(w, map[string]bool{"closed": true})
}

func (h *busAPIHandler) handleEnd(w http.ResponseWriter, r *http.Request) {
	var body busRoomBody
	if !decodeBusBody(w, r, &body) || !requireRoom(w, body.Room) {
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeAPIError(w, http.StatusBadRequest, "name is required")
		return
	}
	resp, err := h.do(body.Host, bus.Request{Op: bus.OpEnd, Room: body.Room, Name: body.Name})
	if err != nil {
		writeBusError(w, err)
		return
	}
	// Same reason as close.
	var payload struct {
		Session string `json:"session"`
	}
	if jsonErr := json.Unmarshal(resp.Payload, &payload); jsonErr == nil && payload.Session != "" {
		h.clearPersisted(body.Room, payload.Session, body.Host)
	}
	writeAPIJSON(w, map[string]bool{"ended": true})
}

// clearPersisted drops a room from ~/.atomic/bus.json, or just one session's
// membership of it. host is the same value body.Host carried to h.do — the
// bus the close or end actually reached — so a same-named room on a
// different host is untouched; see identity.go, ClearRoom. Failure is
// swallowed on purpose: the daemon has already acted, so the cost is a stale
// entry Prune reaps, not a wrong response.
func (h *busAPIHandler) clearPersisted(room, session, host string) {
	st, err := bus.Load(h.home)
	if err != nil {
		return
	}
	if session == "" {
		st.ClearRoom(room, host)
	} else {
		st.Leave(session, room)
	}
	_ = st.Save(h.home)
}

// decodeBusBody bounds the body so a request can never buffer unbounded input.
func decodeBusBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxLogLineBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeAPIError(w, http.StatusBadRequest, "parse request body: "+err.Error())
		return false
	}
	return true
}

// requireRoom gates every room-taking route. Room names are free text on the
// wire but get spliced into a filesystem path, so anything path-shaped is
// rejected before it can escape the rooms directory. Mirrors bus/action.go.
func requireRoom(w http.ResponseWriter, room string) bool {
	if strings.TrimSpace(room) == "" {
		writeAPIError(w, http.StatusBadRequest, "missing room")
		return false
	}
	if strings.ContainsAny(room, `/\`) || strings.Contains(room, "..") {
		writeAPIError(w, http.StatusBadRequest, `invalid room name: must not contain "/", "\", or ".."`)
		return false
	}
	return true
}
