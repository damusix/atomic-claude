// api_bus.go — EXPERIMENT: /api/bus/* endpoints backing a web chat client
// over the atomic bus daemon (internal/bus), so the serve UI can watch
// rooms, open one, and speak into it as a human member.
//
// Read endpoints (status, rooms, who, log, tail) dial the daemon socket
// directly and degrade to "not running" when no daemon is up — opening the
// chat page never spawns one. The write paths that express operator intent
// (join, send) go through bus.EnsureDaemon, so "open a channel" works from
// cold exactly like `atomic bus join` does.
//
// Not yet covered by docs/spec/atomic-serve.md, which declares serve
// read-only; adopting this feature means amending that contract first.
package serve

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
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
	// Home is the user home dir; the bus socket, state, and room logs live
	// under <home>/.atomic.
	Home string
	// TargetDir is the directory being served — the position the web
	// member's name is stacked from (bus.JoinIdentity), so the chat member
	// is named like any CLI join running in that directory.
	TargetDir string
	// DialTimeout bounds each daemon round trip. Zero → 2s.
	DialTimeout time.Duration
	// EnsureDaemon is the seam for the two spawn-capable paths (join,
	// send). nil → bus.EnsureDaemon. Tests substitute a Dial-only variant
	// so a handler test can never fork a real daemon.
	EnsureDaemon func(home string) (*bus.Client, error)
}

type busAPIHandler struct {
	home         string
	targetDir    string
	dialTimeout  time.Duration
	ensureDaemon func(home string) (*bus.Client, error)

	// session is this serve process's one web-member identity. Per-process
	// (pid-scoped) rather than per-tab: the daemon treats one session value
	// as one member, and every tab of this serve instance speaks as the
	// same human operator.
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
		session:      fmt.Sprintf("serve-web-%d", os.Getpid()),
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

func (h *busAPIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	default:
		writeAPIError(w, http.StatusNotFound, "unknown bus route")
	}
}

// do runs one request against a live daemon, Dial-only — a daemon that is
// not running is reported, never spawned.
func (h *busAPIHandler) do(req bus.Request) (bus.Response, error) {
	client, err := bus.Dial(h.home, h.dialTimeout)
	if err != nil {
		return bus.Response{}, err
	}
	defer client.Close()
	return client.Do(req)
}

// doEnsure runs one request, spawning the daemon first if none is live —
// only the operator-intent paths (join, send) use it.
func (h *busAPIHandler) doEnsure(req bus.Request) (bus.Response, error) {
	client, err := h.ensureDaemon(h.home)
	if err != nil {
		return bus.Response{}, err
	}
	defer client.Close()
	return client.Do(req)
}

// writeBusError maps a bus failure onto an HTTP status. Dial failures are
// 503 (daemon not running); daemon-assigned exit codes map by kind.
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

// --- read endpoints ---

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
	if room == "" {
		writeAPIError(w, http.StatusBadRequest, "missing room")
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

// maxLogLineBytes bounds one room-log line: MaxTextBytes of body plus
// generous headroom for the envelope's metadata fields.
const maxLogLineBytes = bus.MaxTextBytes + 64*1024

func (h *busAPIHandler) handleLog(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if room == "" {
		writeAPIError(w, http.StatusBadRequest, "missing room")
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

// readRoomLogTail returns the last n parseable envelopes of a room's log.
// A missing log is an empty history, not an error; a malformed line is
// skipped rather than failing the whole backfill.
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
	if room == "" {
		writeAPIError(w, http.StatusBadRequest, "missing room")
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

// --- write endpoints ---

type busJoinBody struct {
	Room string `json:"room"`
	As   string `json:"as"`
}

func (h *busAPIHandler) handleJoin(w http.ResponseWriter, r *http.Request) {
	var body busJoinBody
	if !decodeBusBody(w, r, &body) || !requireRoom(w, body.Room) {
		return
	}
	name, err := h.join(body.Room, body.As)
	if err != nil {
		writeBusError(w, err)
		return
	}
	writeAPIJSON(w, map[string]string{"name": name})
}

// join joins (creating if absent) room as this serve process's human
// member and caches the assigned name.
func (h *busAPIHandler) join(room, as string) (string, error) {
	if as == "" {
		as = "web"
	}
	name, repo, realm, err := bus.JoinIdentity(h.home, h.targetDir, as)
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

	// Join-if-needed is the whole point of the send path: sending into a
	// room this serve process never joined creates the membership (and the
	// room) on the way. A cached membership can also be stale — the room
	// may have been closed and its roster dropped — so one not-joined
	// failure invalidates the cache and retries through a fresh join.
	h.mu.Lock()
	name, alreadyJoined := h.joined[body.Room]
	h.mu.Unlock()
	if !alreadyJoined {
		var err error
		if name, err = h.join(body.Room, ""); err != nil {
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
		if name, err = h.join(body.Room, ""); err == nil {
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

// decodeBusBody decodes a JSON request body, bounding it well under the
// bus's own MaxTextBytes so a request can never buffer unbounded input.
func decodeBusBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxLogLineBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeAPIError(w, http.StatusBadRequest, "parse request body: "+err.Error())
		return false
	}
	return true
}

func requireRoom(w http.ResponseWriter, room string) bool {
	if strings.TrimSpace(room) == "" {
		writeAPIError(w, http.StatusBadRequest, "missing room")
		return false
	}
	return true
}
