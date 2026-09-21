package remote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/pelletier/go-toml/v2"
)

// RemoteConfig is one [bus.remotes.<name>] entry from ~/.atomic/config.toml.
// See docs/design/atomic-bus-network.md, "Enrollment".
type RemoteConfig struct {
	Name string
	Host string
	Key  []byte
	// CA is the path to a PEM file for a host whose certificate is private,
	// already expanded against home. Empty means the system trust store.
	CA string
}

// Remotes reads every [bus.remotes.<name>] table from home's config.toml. A
// machine with no such table returns an empty map and no error — the
// "unconfigured means local" contract in "Local versus remote": nothing about
// remotes existing changes what an unflagged verb does.
func Remotes(home string) (map[string]RemoteConfig, error) {
	path := config.TOMLPath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]RemoteConfig{}, nil
		}
		return nil, fmt.Errorf("remote: read %s: %w", path, err)
	}

	var raw struct {
		Bus struct {
			Remotes map[string]struct {
				Host string `toml:"host"`
				Key  string `toml:"key"`
				CA   string `toml:"ca,omitempty"`
			} `toml:"remotes"`
		} `toml:"bus"`
	}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("remote: parse %s: %w", path, err)
	}

	out := make(map[string]RemoteConfig, len(raw.Bus.Remotes))
	for name, r := range raw.Bus.Remotes {
		key, err := hex.DecodeString(r.Key)
		if err != nil {
			return nil, fmt.Errorf("remote: [bus.remotes.%s].key: %w", name, err)
		}
		out[name] = RemoteConfig{Name: name, Host: r.Host, Key: key, CA: expandHome(r.CA, home)}
	}
	return out, nil
}

// expandHome resolves a leading "~/" against home, the same convention the
// design's [bus.remotes] example uses for ca.
func expandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// keyIDBytes truncates sha256(key) to 16 bytes (128 bits) before hex
// encoding, so id collisions stay astronomically unlikely without carrying
// the full 32-byte hash on the wire and in keys.json.
const keyIDBytes = 16

// KeyID derives the id a key is looked up under: the client and the gateway
// (internal/gateway.Store) both compute it independently from the shared
// key, so this is the one implementation both sides import rather than two
// that must be kept in step by hand.
func KeyID(key []byte) []byte {
	sum := sha256.Sum256(key)
	return []byte(hex.EncodeToString(sum[:keyIDBytes]))
}

// ProbeHandshake reports whether frame is a Stream subscription's handshake
// confirmation ({"ok":...}), and its ok value when it is. That frame
// precedes the Envelope stream once per connection, including once per
// Stream reconnect, so every caller that reads a Stream must skip it rather
// than render it as a live envelope — recvRemoteStream, tailRemoteStream and
// handleTailRemote all probe for it the same way.
func ProbeHandshake(frame []byte) (isHandshake, ok bool) {
	var probe struct {
		OK *bool `json:"ok"`
	}
	if json.Unmarshal(frame, &probe) != nil || probe.OK == nil {
		return false, false
	}
	return true, *probe.OK
}

// maxResponseFrameBytes bounds a single length-prefixed response frame before
// it is ever allocated. The four-byte length sits outside the AEAD and is
// attacker-controlled — see writeFramed in internal/gateway/gateway.go — so a
// value approaching 4 GiB must be rejected before make() runs, never after.
const maxResponseFrameBytes = 2 << 20

// AdmissionWindow is the timestamp window a sealed frame's Header.Timestamp
// must fall within on either side of the reader's clock, mirrored on the
// gateway's c2s admission side as gateway.AdmissionWindow. The two must stay
// equal, but cannot share one constant: gateway already imports this
// package, so the dependency can only run this direction.
const AdmissionWindow = 120 * time.Second

var (
	// ErrFrameLength marks a length prefix past maxResponseFrameBytes: a
	// stream fault, never a cue to resynchronise by scanning for the next
	// plausible frame.
	ErrFrameLength = errors.New("remote: response frame length exceeds bound")

	// ErrStreamEnded marks a connection that produced no further usable
	// frame. A truncation and an orderly end are indistinguishable from
	// inside a stream, so both read as this fault rather than a clean finish.
	ErrStreamEnded = errors.New("remote: stream ended")

	// ErrStaleTimestamp marks an opened s2c frame whose Header.Timestamp
	// falls outside AdmissionWindow of the client's own clock — an in-order
	// frame a network-path attacker held and released late, since the AEAD
	// alone only proves the frame was sealed by the gateway, not when.
	ErrStaleTimestamp = errors.New("remote: response timestamp outside admission window")
)

// heartbeatPlaintext must match gateway.go's literal exactly — the shape a
// remote client tells a heartbeat apart from real daemon output by, not a
// decoded field.
var heartbeatPlaintext = []byte(`{"heartbeat":true}`)

func isHeartbeat(plaintext []byte) bool {
	return bytes.Equal(plaintext, heartbeatPlaintext)
}

// Client is one caller's connection to a gateway, sealed under one enrolled
// key. Do performs a single round trip; Stream performs the same handshake
// and then yields the daemon's output until ctx is cancelled.
type Client struct {
	baseURL string
	key     []byte
	keyID   []byte
	http    *http.Client
	now     func() time.Time
	backoff func(attempt int) time.Duration

	// doTimeout bounds a single Do round trip so a gateway that accepts the
	// connection and never answers cannot hang the caller forever — the
	// local client sets a deadline on every request (bus/client.go's Do) and
	// this is the one place that convention was not matched. It is never
	// applied to Stream, whose live connection is meant to wait indefinitely
	// for the next line; only ctx cancellation ends that one.
	doTimeout time.Duration
}

// defaultDoTimeout is Do's request bound. A one-shot op normally answers in
// well under a second even over a WAN; this is generous headroom, not a
// tuned figure.
const defaultDoTimeout = 15 * time.Second

// NewClient builds a Client for cfg. host is treated as a bare hostname
// (https, matching the gateway's optional-TLS default) unless it already
// names a scheme, which lets tests point at a plain httptest.Server.
func NewClient(cfg RemoteConfig) (*Client, error) {
	if len(cfg.Key) == 0 {
		return nil, fmt.Errorf("remote: %s: no key configured", cfg.Name)
	}

	base := cfg.Host
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}

	tlsConfig, err := tlsConfigFor(cfg.CA)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
		// h2 is disabled explicitly on this side too, matching the gateway's
		// own TLSNextProto clearing — see docs/design/atomic-bus-network.md,
		// "One endpoint, and how the two directions work".
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}

	return &Client{
		baseURL:   base,
		key:       cfg.Key,
		keyID:     KeyID(cfg.Key),
		http:      &http.Client{Transport: transport},
		now:       time.Now,
		backoff:   defaultBackoff,
		doTimeout: defaultDoTimeout,
	}, nil
}

// SetDoTimeout overrides Do's per-round-trip bound (defaultDoTimeout unless
// called). A caller fanning out across several remotes at once — serve's
// handleRooms is the one today — wants a bound tighter than the general
// 15-second default, so one unreachable remote does not make the whole
// fan-out wait 15 seconds before the others even start.
func (c *Client) SetDoTimeout(d time.Duration) {
	c.doTimeout = d
}

func tlsConfigFor(ca string) (*tls.Config, error) {
	if ca == "" {
		return nil, nil
	}
	pemBytes, err := os.ReadFile(ca)
	if err != nil {
		return nil, fmt.Errorf("remote: read ca %s: %w", ca, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("remote: no certificates found in %s", ca)
	}
	return &tls.Config{RootCAs: pool}, nil
}

// Do seals plaintext under a fresh client nonce, POSTs it to /v1/op, and
// returns the first non-heartbeat frame the gateway answers with — opened
// under the subkey derived from the nonce this call sent, never from
// anything the response claims about itself.
func (c *Client) Do(plaintext []byte) ([]byte, error) {
	frame, nonce, err := SealC2S(c.key, c.keyID, plaintext, c.now())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.doTimeout)
	defer cancel()
	resp, err := c.post(ctx, frame)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var seq uint64
	for {
		opened, err := c.openNext(resp.Body, nonce, seq)
		if err != nil {
			return nil, err
		}
		seq++
		if isHeartbeat(opened) {
			continue
		}
		return opened, nil
	}
}

// defaultBackoff grows 250ms, 500ms, 1s, ... capped at 30s. The daemon's own
// retry is two attempts at 50ms, tuned for a local restart; a remote fault
// needs real backoff, since no sleeping laptop survives that cadence against
// a network peer.
func defaultBackoff(attempt int) time.Duration {
	d := 250 * time.Millisecond
	for i := 0; i < attempt && d < 30*time.Second; i++ {
		d *= 2
	}
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// Stream opens plaintext as a subscription and forwards every opened,
// non-heartbeat frame on the returned channel until ctx is cancelled. A
// truncation and an orderly end look identical from inside one connection
// (see streamOnce), so any fault there reconnects under a fresh request
// nonce after backoff rather than surfacing as terminal — only ctx
// cancellation ends Stream for good, reported on the error channel.
func (c *Client) Stream(ctx context.Context, plaintext []byte) (<-chan []byte, <-chan error) {
	out := make(chan []byte)
	errc := make(chan error, 1)
	go func() {
		defer close(out)
		attempt := 0
		for {
			if err := ctx.Err(); err != nil {
				errc <- err
				return
			}
			if opened, _ := c.streamOnce(ctx, plaintext, out); opened {
				// A stream that delivered at least one frame proved the path
				// live; the fault that ended it does not carry forward
				// against a peer that already answered once. Without this,
				// attempt only grows, and a multi-hour recv that reconnected
				// a handful of times waits the full backoff.Max on every
				// later fault for the rest of its life.
				attempt = 0
			}
			attempt++
			select {
			case <-time.After(c.backoff(attempt)):
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			}
		}
	}()
	return out, errc
}

// streamOnce performs one connection attempt: seals plaintext under a fresh
// nonce, POSTs it, and forwards each opened, non-heartbeat frame to out in
// strict sequence order. It returns whatever ended the connection — a wrong
// sequence, an oversized length prefix, a transport failure, or an ordinary
// EOF — never nil, since a truncation and a clean finish cannot be told
// apart from inside the stream — and whether it opened at least one frame
// before the fault, which is what Stream resets its backoff on.
func (c *Client) streamOnce(ctx context.Context, plaintext []byte, out chan<- []byte) (opened bool, err error) {
	frame, nonce, err := SealC2S(c.key, c.keyID, plaintext, c.now())
	if err != nil {
		return false, err
	}
	resp, err := c.post(ctx, frame)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	var seq uint64
	for {
		frame, err := c.openNext(resp.Body, nonce, seq)
		if err != nil {
			return opened, err
		}
		opened = true
		seq++
		if isHeartbeat(frame) {
			continue
		}
		select {
		case out <- frame:
		case <-ctx.Done():
			return opened, ctx.Err()
		}
	}
}

// openNext reads one length-prefixed frame off r and opens it under nonce at
// wantSeq exactly — a gap, a repeat, a reorder, or a first frame whose
// counter is not zero all fail this same check, since each produces a
// sequence other than the one wantSeq demands. A frame that opens but whose
// Header.Timestamp falls outside AdmissionWindow is rejected too: the AEAD
// alone proves the gateway sealed it, not when, and a network-path attacker
// holding an in-order frame and releasing it later is otherwise undetected.
func (c *Client) openNext(r io.Reader, nonce [nonceSize]byte, wantSeq uint64) ([]byte, error) {
	raw, err := readFramed(r, maxResponseFrameBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStreamEnded, err)
	}
	opened, header, err := OpenS2C(c.key, nonce, raw, wantSeq)
	if err != nil {
		return nil, ErrOpen
	}
	ts := time.Unix(header.Timestamp, 0)
	now := c.now()
	if ts.Before(now.Add(-AdmissionWindow)) || ts.After(now.Add(AdmissionWindow)) {
		return nil, ErrStaleTimestamp
	}
	return opened, nil
}

func (c *Client) post(ctx context.Context, frame []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/op", bytes.NewReader(frame))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("remote: unexpected status %d", resp.StatusCode)
	}
	return resp, nil
}

// readFramed reads one length-prefixed frame from r. The four-byte length
// sits outside the AEAD and is attacker-controlled, so it is bounded against
// max before make() ever runs — an oversized value ends the stream rather
// than becoming a multi-gigabyte allocation.
func readFramed(r io.Reader, max int) ([]byte, error) {
	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(length[:])
	if n > uint32(max) {
		return nil, ErrFrameLength
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
