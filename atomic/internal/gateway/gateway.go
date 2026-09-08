// Package gateway implements the key store and admission ladder that front
// the atomic bus daemon over the network, and the HTTP server built on top of
// them: one endpoint, sealed responses, and the daemon started beside it. See
// docs/design/atomic-bus-network.md, "Admission", "Identity", "One endpoint,
// and how the two directions work", and "Deployment".
package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/damusix/atomic-claude/atomic/internal/bus/remote"
)

// MaxFrameBytes bounds the request body the gateway reads before it ever
// looks at what is inside — the daemon decodes frames with no size limit of
// its own, so an unbounded read here would let an unauthenticated body
// exhaust memory before admission gets a chance to reject it.
//
// Headroom above bus.MaxTextBytes, not equal to it: a send at the local text
// cap grows past MaxTextBytes once it is JSON-escaped into a Request, wrapped
// in the sealed frame's binary header, and carries its AEAD tag, so a cap
// equal to the local limit silently failed exactly the sends that were
// legal locally — see internal/serve/api_bus.go's maxLogLineBytes for the
// same headroom against the same failure mode.
const MaxFrameBytes = bus.MaxTextBytes + 64*1024

// HeartbeatInterval is the default longest gap between sealed lines on a
// streaming response before the gateway seals and flushes a heartbeat that
// still advances the sequence. A quiet stream would otherwise look dead to a
// proxy that drops idle connections, and a gap left outside the seal would be
// an unauthenticated place to inject a fake "nothing happened" silence.
const HeartbeatInterval = 30 * time.Second

// readTimeout bounds reading the request line, headers and body — never the
// streaming response that follows, which has no write deadline once open.
const readTimeout = 10 * time.Second

// heartbeatPlaintext is what a heartbeat frame seals. It is not a bus.Envelope
// or bus.Response — a remote client tells it apart from real daemon output by
// shape and drops it rather than surfacing it.
var heartbeatPlaintext = []byte(`{"heartbeat":true}`)

// Server proxies sealed frames between remote clients and the bus daemon.
// dial opens a fresh connection to that daemon; Run wires it to the Unix
// listener the daemon is bound to. Now defaults to time.Now when nil.
type Server struct {
	Store  *Store
	Window *Window
	Now    func() time.Time

	// Heartbeat overrides HeartbeatInterval; zero means the default.
	Heartbeat time.Duration

	dial func() (net.Conn, error)
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) heartbeat() time.Duration {
	if s.Heartbeat > 0 {
		return s.Heartbeat
	}
	return HeartbeatInterval
}

// ServeHTTP routes only POST /v1/op; the op lives inside the sealed frame, so
// there is exactly one route regardless of what the op does.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/op" {
		http.NotFound(w, r)
		return
	}
	s.handleOp(w, r)
}

// handleOp reads and caps the request body, admits it, refuses shutdown with
// a sealed error, and otherwise dials the daemon and copies its output back
// sealed. Every rejection path closes the connection with no bytes written:
// a distinguishable HTTP status or body would turn admission into an oracle,
// which is the same reasoning ErrDrop already encodes for Admit itself.
func (s *Server) handleOp(w http.ResponseWriter, r *http.Request) {
	now := s.now()

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxFrameBytes))
	if err != nil {
		// Either malformed, or MaxBytesReader tripped the cap — the cap check
		// resolves here, before dial is ever reached.
		hijackClose(w)
		return
	}

	caller, err := Admit(s.Store, s.Window, body, now)
	switch err {
	case nil:
	case ErrShutdownRefused:
		s.writeSealedError(w, body, now)
		return
	default:
		hijackClose(w)
		return
	}

	header, _, err := remote.ParseHeader(body)
	if err != nil {
		hijackClose(w)
		return
	}

	conn, err := s.dial()
	if err != nil {
		hijackClose(w)
		return
	}
	defer conn.Close()

	if _, err := conn.Write(append(caller.Body, '\n')); err != nil {
		hijackClose(w)
		return
	}

	s.copyStream(w, r, conn, caller.KeyID, header.Nonce)
}

// writeSealedError answers a shutdown refusal in the open, not silently:
// refusing shutdown while forwarding every other op is the one authorization
// rule the design draws, so the caller must be able to see it happened.
func (s *Server) writeSealedError(w http.ResponseWriter, requestFrame []byte, now time.Time) {
	header, _, err := remote.ParseHeader(requestFrame)
	if err != nil {
		hijackClose(w)
		return
	}
	rec, ok, err := s.Store.Lookup(string(header.KeyID))
	if err != nil || !ok {
		hijackClose(w)
		return
	}

	resp := bus.Response{OK: false, Code: bus.ExitHard, Error: "gateway: shutdown refused"}
	plaintext, err := json.Marshal(resp)
	if err != nil {
		hijackClose(w)
		return
	}
	frame, err := remote.SealS2C(rec.Key, header.KeyID, header.Nonce, 0, plaintext, now)
	if err != nil {
		hijackClose(w)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	writeFramed(w, frame)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// copyStream copies conn's daemon output line by line into w, sealing each
// under the s2c subkey derived from requestNonce with the next sequence
// number, starting at zero. It re-checks the key store before every seal —
// not just the one that admitted the request — which is what lets a
// revocation end a live stream within one frame, with no registry of open
// streams to walk and cancel: the next line simply has nowhere to be sealed
// for. A one-shot op's daemon connection closes after one line, so this same
// loop ends it exactly like it ends a multi-hour recv.
//
// readDone and done are two different signals, not one: readDone is the
// reader goroutine telling the main loop it stopped reading (conn EOF or
// error); done is the main loop telling the reader to stop, closed on every
// return path. A reader blocked sending on lines when the main loop returns
// (client hang-up, a revoked key, a seal failure) has nothing else that can
// unblock it — conn.Close() does not unblock a channel send — so without its
// own exit signal it leaks for the life of the process.
func (s *Server) copyStream(w http.ResponseWriter, r *http.Request, conn net.Conn, keyID string, requestNonce [12]byte) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	lines := make(chan []byte)
	readDone := make(chan struct{})
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(readDone)
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				select {
				case lines <- line:
				case <-done:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	timer := time.NewTimer(s.heartbeat())
	defer timer.Stop()

	var seq uint64
	for {
		var plaintext []byte
		fromTimer := false
		select {
		case line := <-lines:
			plaintext = line
		case <-readDone:
			return
		case <-timer.C:
			plaintext = heartbeatPlaintext
			fromTimer = true
		case <-r.Context().Done():
			// The client disconnected. Nothing detects that on its own until
			// the next Write fails, which could be up to Heartbeat away —
			// watching the request context is what makes a client hang-up
			// end this loop immediately instead.
			return
		}

		rec, ok, err := s.Store.Lookup(keyID)
		if err != nil || !ok {
			return
		}
		frame, err := remote.SealS2C(rec.Key, []byte(keyID), requestNonce, seq, plaintext, s.now())
		if err != nil {
			return
		}
		writeFramed(w, frame)
		if flusher != nil {
			flusher.Flush()
		}
		seq++

		// timer.C was already drained by the select above when this
		// iteration came from the heartbeat case — stopping and re-draining
		// it in that case would block forever on a channel with nothing
		// left to deliver.
		if !fromTimer && !timer.Stop() {
			<-timer.C
		}
		timer.Reset(s.heartbeat())
	}
}

// writeFramed prefixes frame with its length. A sealed frame's ciphertext
// has no self-terminating length of its own — the header's "body remainder"
// layout assumes one frame per message — so a response streaming several
// frames back to back needs an explicit boundary between them. This framing
// exists only on the HTTP body between gateway and remote.Client; it has no
// bearing on the sealed frame format itself.
//
// The four-byte length sits outside the AEAD: a network-path attacker can
// rewrite it. That is not a forgery hole — a wrong length only ever
// misaligns the next Open, which fails closed — but it makes the length
// untrusted framing, not a trusted record length, and the reader owes it
// two things: bound the value against the gateway's own
// frame cap before allocating, so an attacker-supplied length near 4 GiB
// never becomes a matching-size make(); and treat a bad length as a stream
// fault that ends the connection, never as a cue to resynchronise by
// scanning for the next plausible frame.
func writeFramed(w io.Writer, frame []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(frame)))
	if _, err := w.Write(length[:]); err != nil {
		return
	}
	_, _ = w.Write(frame)
}

// hijackClose takes over the connection and closes it with no bytes written,
// the network-level counterpart of Admit's ErrDrop: any distinguishable
// status or body here would leak which check failed to an attacker probing
// the endpoint. h2 is disabled (newHTTPServer clears TLSNextProto), so every
// connection this handler runs on is HTTP/1.1, whose ResponseWriter always
// supports Hijacker. Falling through to a normal response on failure would
// silently turn the drop into net/http's implicit 200 — a distinguishable
// outcome from the silent close this exists to produce — so a failure here
// means that invariant broke, and panicking is what makes it unreachable by
// construction rather than by circumstance.
func hijackClose(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic("gateway: response writer does not support hijack; h2 must stay disabled")
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		panic("gateway: hijack failed: " + err.Error())
	}
	_ = conn.Close()
}

// newHTTPServer wires handler behind an *http.Server with h2 disabled. h2 is
// negotiated over TLS ALPN, and under it a recv stream and a one-shot op can
// share one connection as separate streams — closing on a failed admission
// would then RST_STREAM an unrelated live stream instead of just the
// connection that failed. Clearing TLSNextProto keeps every connection
// HTTP/1.1, whether or not TLS is in use.
func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:      handler,
		ReadTimeout:  readTimeout,
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){},
	}
}

// Run starts the bus daemon (state under home) on unixLn and serves /v1/op
// on httpLn until ctx is cancelled or either listener fails. TLS is served
// when both certFile and keyFile are set; otherwise Run serves plain HTTP,
// per design: the frame is sealed regardless, so TLS is defence in depth
// rather than the floor. now is injectable for tests; nil means time.Now.
func Run(ctx context.Context, home string, unixLn, httpLn net.Listener, certFile, keyFile string, store *Store, window *Window, now func() time.Time) error {
	hub := bus.NewHub(home)

	daemonDone := make(chan error, 1)
	go func() { daemonDone <- bus.Serve(ctx, unixLn, hub, now) }()

	addr := unixLn.Addr().String()
	srv := newHTTPServer(&Server{
		Store:  store,
		Window: window,
		Now:    now,
		dial:   func() (net.Conn, error) { return net.Dial("unix", addr) },
	})

	serveErr := make(chan error, 1)
	go func() {
		if certFile != "" && keyFile != "" {
			serveErr <- srv.ServeTLS(httpLn, certFile, keyFile)
		} else {
			serveErr <- srv.Serve(httpLn)
		}
	}()

	select {
	case <-ctx.Done():
		_ = srv.Close()
		<-serveErr
		return <-daemonDone
	case err := <-serveErr:
		log.Printf("gateway: http server stopped: %v", err)
		return err
	case err := <-daemonDone:
		_ = srv.Close()
		return err
	}
}
