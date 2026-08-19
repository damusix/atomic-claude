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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
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
		h.handleJoin(w, r)
	case route == "send" && r.Method == http.MethodPost:
		h.handleSend(w, r)
	case route == "say" && r.Method == http.MethodPost:
		h.handleSay(w, r)
	case route == "halt" && r.Method == http.MethodPost:
		h.handleHalt(w, r)
	case route == "resume" && r.Method == http.MethodPost:
		h.handleResume(w, r)
	case route == "leave" && r.Method == http.MethodPost:
		h.handleLeave(w, r)
	case route == "close" && r.Method == http.MethodPost:
		h.handleClose(w, r)
	case route == "end" && r.Method == http.MethodPost:
		h.handleEnd(w, r)
	default:
		writeAPIError(w, http.StatusNotFound, "unknown bus route")
	}
}

// do runs one request Dial-only: a daemon that is not running is reported,
// never spawned.
func (h *busAPIHandler) do(req bus.Request) (bus.Response, error) {
	client, err := bus.Dial(h.home, h.dialTimeout)
	if err != nil {
		return bus.Response{}, err
	}
	defer client.Close()
	return client.Do(req)
}

// doEnsure spawns the daemon first if none is live; only join and send use it.
func (h *busAPIHandler) doEnsure(req bus.Request) (bus.Response, error) {
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
	if _, err := h.do(bus.Request{Op: bus.OpPing}); err == nil {
		resp.Running = true
	}
	writeAPIJSON(w, resp)
}

type busRoomsResponse struct {
	Running bool           `json:"running"`
	Rooms   []bus.RoomInfo `json:"rooms"`
}

func (h *busAPIHandler) handleRooms(w http.ResponseWriter) {
	resp, err := h.do(bus.Request{Op: bus.OpRooms})
	if err != nil {
		writeAPIJSON(w, busRoomsResponse{Running: false, Rooms: []bus.RoomInfo{}})
		return
	}
	var payload struct {
		Rooms []bus.RoomInfo `json:"rooms"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "parse rooms payload: "+err.Error())
		return
	}
	if payload.Rooms == nil {
		payload.Rooms = []bus.RoomInfo{}
	}
	writeAPIJSON(w, busRoomsResponse{Running: true, Rooms: payload.Rooms})
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
	resp, err := h.do(bus.Request{Op: bus.OpWho, Room: room})
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

func (h *busAPIHandler) handleLog(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if !requireRoom(w, room) {
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

type busJoinBody struct {
	Room string `json:"room"`
}

func (h *busAPIHandler) handleJoin(w http.ResponseWriter, r *http.Request) {
	var body busJoinBody
	if !decodeBusBody(w, r, &body) || !requireRoom(w, body.Room) {
		return
	}
	name, err := h.join(body.Room)
	if err != nil {
		writeBusError(w, err)
		return
	}
	writeAPIJSON(w, map[string]string{"name": name})
}

// join joins room, creating it if absent, and caches the assigned name.
func (h *busAPIHandler) join(room string) (string, error) {
	name, repo, realm, err := bus.JoinIdentity(h.home, h.targetDir, "web")
	if err != nil {
		return "", err
	}
	resp, err := h.doEnsure(bus.Request{
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
		if name, err = h.join(body.Room); err != nil {
			writeBusError(w, err)
			return
		}
	}

	req := bus.Request{Op: bus.OpSend, Room: body.Room, Session: h.session, To: body.To, ReplyTo: body.ReplyTo, Text: body.Text}
	resp, err := h.doEnsure(req)
	var busErr *bus.Error
	if err != nil && alreadyJoined && errors.As(err, &busErr) && busErr.Code == bus.ExitNotJoined {
		h.mu.Lock()
		delete(h.joined, body.Room)
		h.mu.Unlock()
		if name, err = h.join(body.Room); err == nil {
			resp, err = h.doEnsure(req)
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
	resp, err := h.do(bus.Request{Op: bus.OpSay, Room: body.Room, To: body.To, Text: body.Text})
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
}

func (h *busAPIHandler) handleHalt(w http.ResponseWriter, r *http.Request) {
	var body busRoomBody
	if !decodeBusBody(w, r, &body) || !requireRoom(w, body.Room) {
		return
	}
	if _, err := h.do(bus.Request{Op: bus.OpHalt, Room: body.Room, Text: body.Reason}); err != nil {
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
	if _, err := h.do(bus.Request{Op: bus.OpResume, Room: body.Room}); err != nil {
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
	if _, err := h.do(bus.Request{Op: bus.OpLeave, Room: body.Room, Session: h.session}); err != nil {
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
	if _, err := h.do(bus.Request{Op: bus.OpClose, Room: body.Room}); err != nil {
		writeBusError(w, err)
		return
	}
	// Rehydrate replays bus.json on the next daemon start, so a room left there
	// comes back. `atomic bus close` does this same second half.
	h.clearPersisted(body.Room, "")
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
	resp, err := h.do(bus.Request{Op: bus.OpEnd, Room: body.Room, Name: body.Name})
	if err != nil {
		writeBusError(w, err)
		return
	}
	// Same reason as close.
	var payload struct {
		Session string `json:"session"`
	}
	if jsonErr := json.Unmarshal(resp.Payload, &payload); jsonErr == nil && payload.Session != "" {
		h.clearPersisted(body.Room, payload.Session)
	}
	writeAPIJSON(w, map[string]bool{"ended": true})
}

// clearPersisted drops a room from ~/.atomic/bus.json, or just one session's
// membership of it. Failure is swallowed on purpose: the daemon has already
// acted, so the cost is a stale entry Prune reaps, not a wrong response.
func (h *busAPIHandler) clearPersisted(room, session string) {
	st, err := bus.Load(h.home)
	if err != nil {
		return
	}
	if session == "" {
		st.ClearRoom(room)
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
