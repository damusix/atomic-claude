package remote

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mustClient(t *testing.T, host string, key []byte) *Client {
	t.Helper()
	c, err := NewClient(RemoteConfig{Name: "test", Host: host, Key: key})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// writeFramedTest mirrors gateway.go's writeFramed: a four-byte big-endian
// length prefix ahead of the frame bytes, the framing that exists only on
// this HTTP body between gateway and client.
func writeFramedTest(w io.Writer, frame []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(frame)))
	_, _ = w.Write(length[:])
	_, _ = w.Write(frame)
}

// --- adversarial framing, against a hand-rolled server ---

func TestClient_StreamOnce_RejectsNonzeroFirstCounter(t *testing.T) {
	key := testKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		header, _, err := ParseHeader(body)
		if err != nil {
			t.Fatalf("parse header: %v", err)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		frame, err := SealS2C(key, []byte("kid"), header.Nonce, 1, []byte(`{}`), time.Now())
		if err != nil {
			t.Fatalf("SealS2C: %v", err)
		}
		writeFramedTest(w, frame)
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, key)
	out := make(chan []byte, 1)
	_, err := c.streamOnce(context.Background(), []byte(`{"op":"recv"}`), out)
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("err = %v, want ErrOpen", err)
	}
	select {
	case f := <-out:
		t.Fatalf("delivered a frame that should have been rejected: %s", f)
	default:
	}
}

func TestClient_StreamOnce_RejectsRepeat(t *testing.T) {
	key := testKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		header, _, err := ParseHeader(body)
		if err != nil {
			t.Fatalf("parse header: %v", err)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		f0, _ := SealS2C(key, []byte("kid"), header.Nonce, 0, []byte(`{"n":0}`), time.Now())
		writeFramedTest(w, f0)
		flusher.Flush()
		// Repeats seq 0 instead of advancing to 1.
		f1, _ := SealS2C(key, []byte("kid"), header.Nonce, 0, []byte(`{"n":"repeat"}`), time.Now())
		writeFramedTest(w, f1)
		flusher.Flush()
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, key)
	out := make(chan []byte, 2)
	_, err := c.streamOnce(context.Background(), []byte(`{"op":"recv"}`), out)
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("err = %v, want ErrOpen", err)
	}
	if len(out) != 1 {
		t.Fatalf("delivered %d frames, want exactly the one valid frame before the repeat", len(out))
	}
}

func TestClient_StreamOnce_RejectsGap(t *testing.T) {
	key := testKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		header, _, err := ParseHeader(body)
		if err != nil {
			t.Fatalf("parse header: %v", err)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		f0, _ := SealS2C(key, []byte("kid"), header.Nonce, 0, []byte(`{"n":0}`), time.Now())
		writeFramedTest(w, f0)
		flusher.Flush()
		// Skips straight to seq 2, leaving a gap at 1.
		f2, _ := SealS2C(key, []byte("kid"), header.Nonce, 2, []byte(`{"n":2}`), time.Now())
		writeFramedTest(w, f2)
		flusher.Flush()
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, key)
	out := make(chan []byte, 2)
	_, err := c.streamOnce(context.Background(), []byte(`{"op":"recv"}`), out)
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("err = %v, want ErrOpen", err)
	}
	if len(out) != 1 {
		t.Fatalf("delivered %d frames, want exactly the one valid frame before the gap", len(out))
	}
}

// TestClient_StreamOnce_HostileLengthPrefix_EndsWithoutAllocating proves the
// four-byte length prefix is bounded before make() runs: a value near 4 GiB
// must fail fast rather than attempt the allocation or hang waiting for
// bytes that will never arrive.
func TestClient_StreamOnce_HostileLengthPrefix_EndsWithoutAllocating(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], 0xFFFFFFF0) // ~4 GiB, past maxResponseFrameBytes
		_, _ = w.Write(length[:])
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, testKey(t))
	out := make(chan []byte, 1)
	done := make(chan error, 1)
	go func() {
		_, err := c.streamOnce(context.Background(), []byte(`{"op":"recv"}`), out)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrFrameLength) {
			t.Fatalf("err = %v, want ErrFrameLength", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("streamOnce did not end promptly on a hostile length prefix")
	}
}

// TestClient_Stream_ReconnectsWithFreshNonceAfterFault proves the reconnect
// side of "an ended stream is a fault, not a clean finish": Stream backs off
// and reconnects under a brand-new request nonce rather than surfacing the
// first connection's fault as terminal.
func TestClient_Stream_ReconnectsWithFreshNonceAfterFault(t *testing.T) {
	key := testKey(t)
	var attempt int32
	var mu sync.Mutex
	nonces := map[int32][nonceSize]byte{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		header, _, err := ParseHeader(body)
		if err != nil {
			t.Fatalf("parse header: %v", err)
		}
		n := atomic.AddInt32(&attempt, 1)
		mu.Lock()
		nonces[n] = header.Nonce
		mu.Unlock()

		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		if n == 1 {
			// A nonzero first counter: this connection is a fault.
			frame, _ := SealS2C(key, []byte("kid"), header.Nonce, 5, []byte(`{"n":"bad"}`), time.Now())
			writeFramedTest(w, frame)
			flusher.Flush()
			return
		}
		frame, _ := SealS2C(key, []byte("kid"), header.Nonce, 0, []byte(`{"n":"good"}`), time.Now())
		writeFramedTest(w, frame)
		flusher.Flush()
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, key)
	c.backoff = func(int) time.Duration { return time.Millisecond }

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, errc := c.Stream(ctx, []byte(`{"op":"recv","room":"potato"}`))

	select {
	case frame := <-out:
		if string(frame) != `{"n":"good"}` {
			t.Fatalf("got %s, want the second attempt's frame", frame)
		}
	case err := <-errc:
		t.Fatalf("stream ended before reconnecting: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the reconnect to deliver")
	}

	mu.Lock()
	n1, n2 := nonces[1], nonces[2]
	mu.Unlock()
	if n1 == n2 {
		t.Fatal("reconnect reused the first attempt's request nonce")
	}
}

// TestClient_Stream_BackoffResetsAfterAnOpenedConnection proves finding 4 of
// the final review: attempt must not grow forever once a connection has
// proven the path live. Two faulting connections that never open a frame
// grow attempt normally; a third that opens one frame before faulting
// resets it; two more faulting connections grow it again from zero. Without
// the reset, backoff would see 1, 2, 3, 4, 5 instead of 1, 2, 1, 2, 3.
func TestClient_Stream_BackoffResetsAfterAnOpenedConnection(t *testing.T) {
	key := testKey(t)
	var connN int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		header, _, err := ParseHeader(body)
		if err != nil {
			t.Fatalf("parse header: %v", err)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		n := atomic.AddInt32(&connN, 1)
		if n == 3 {
			// The one connection that opens a frame before faulting.
			frame, _ := SealS2C(key, []byte("kid"), header.Nonce, 0, []byte(`{"n":0}`), time.Now())
			writeFramedTest(w, frame)
		}
		w.(http.Flusher).Flush()
		// Hijack and close immediately rather than letting the handler
		// return and the keep-alive connection linger: an explicit close is
		// what makes the client's next read fault promptly, on every
		// connection (1, 2, 4, 5 write nothing at all before it; 3 writes
		// one frame first).
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				conn.Close()
			}
		}
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, key)
	var mu sync.Mutex
	var attempts []int
	done := make(chan struct{})
	c.backoff = func(attempt int) time.Duration {
		mu.Lock()
		attempts = append(attempts, attempt)
		n := len(attempts)
		mu.Unlock()
		if n == 5 {
			close(done)
			// A long wait past the test's own deadline: ctx cancellation
			// (deferred below) ends Stream's goroutine from inside this
			// wait rather than letting it queue a 6th attempt.
			return time.Hour
		}
		return time.Millisecond
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, errc := c.Stream(ctx, []byte(`{"op":"recv","room":"potato"}`))
	go func() {
		for {
			select {
			case <-out:
			case <-errc:
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for 5 reconnect attempts")
	}

	mu.Lock()
	got := append([]int(nil), attempts...)
	mu.Unlock()
	want := []int{1, 2, 1, 2, 3}
	if len(got) < len(want) {
		t.Fatalf("attempts = %v, want at least %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("attempts = %v, want %v (backoff must reset after connection 3 opened a frame)", got, want)
		}
	}
}

// --- Do timeout ---

// TestClient_Do_GatewayNeverAnswers_ReturnsErrorInsteadOfHanging proves spec
// criterion 6: Do must not hardcode context.Background(), or a gateway that
// accepts the connection and never answers blocks the caller forever.
func TestClient_Do_GatewayNeverAnswers_ReturnsErrorInsteadOfHanging(t *testing.T) {
	unblock := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-unblock
	}))
	defer func() {
		close(unblock)
		srv.Close()
	}()

	c := mustClient(t, srv.URL, testKey(t))
	c.doTimeout = 50 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		_, err := c.Do([]byte(`{"op":"ping"}`))
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from a gateway that never answers")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Do hung past the deadline instead of returning an error")
	}
}

// --- scheme-less host ---

// TestClient_SchemeLessHost_AgainstPlainHTTPGateway_NamesTheScheme proves
// NewClient's documented default: a host with no "://" is treated as https,
// matching the gateway's TLS-optional-but-not-default posture. Against a
// plain-HTTP gateway (httptest.NewServer, not NewTLSServer) that produces a
// clear net/http diagnostic naming both schemes, never an opaque TLS
// handshake failure a reader cannot act on — see docs/guides/bus-hosting.md,
// "Enroll a machine".
func TestClient_SchemeLessHost_AgainstPlainHTTPGateway_NamesTheScheme(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached: the TLS handshake itself must fail first")
	}))
	defer srv.Close()

	bareHost := strings.TrimPrefix(srv.URL, "http://")
	c := mustClient(t, bareHost, testKey(t))
	c.doTimeout = 2 * time.Second

	_, err := c.Do([]byte(`{"op":"ping"}`))
	if err == nil {
		t.Fatal("expected an error dialing a plain-HTTP gateway as https")
	}
	if !strings.Contains(err.Error(), "HTTPS") && !strings.Contains(err.Error(), "HTTP response") {
		t.Fatalf("err = %q, want a message naming the scheme mismatch, not an opaque TLS error", err)
	}
}

// --- Remotes config ---

func TestRemotes_NoConfigFile_ReturnsEmptyMap(t *testing.T) {
	remotes, err := Remotes(t.TempDir())
	if err != nil {
		t.Fatalf("Remotes: %v", err)
	}
	if len(remotes) != 0 {
		t.Fatalf("remotes = %+v, want empty (no [bus.remotes] table configured)", remotes)
	}
}

func TestRemotes_ParsesHostKeyAndExpandsCA(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".atomic")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	toml := "[bus.remotes.prod]\n" +
		"host = \"bus.example.com\"\n" +
		"key  = \"aabbcc\"\n" +
		"ca   = \"~/.atomic/bus/prod-ca.pem\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	remotes, err := Remotes(home)
	if err != nil {
		t.Fatalf("Remotes: %v", err)
	}
	cfg, ok := remotes["prod"]
	if !ok {
		t.Fatalf("remotes = %+v, missing prod", remotes)
	}
	if cfg.Host != "bus.example.com" {
		t.Errorf("Host = %q", cfg.Host)
	}
	if string(cfg.Key) != "\xaa\xbb\xcc" {
		t.Errorf("Key = %x, want aabbcc decoded", cfg.Key)
	}
	want := filepath.Join(home, ".atomic", "bus", "prod-ca.pem")
	if cfg.CA != want {
		t.Errorf("CA = %q, want %q", cfg.CA, want)
	}
}
