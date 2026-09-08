package gateway

import (
	"encoding/json"
	"errors"
	"log"
	"time"
	"unicode/utf8"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/damusix/atomic-claude/atomic/internal/bus/remote"
)

// ErrDrop is the single outcome for every silent-failure step of the
// admission ladder — malformed header, timestamp outside the window,
// unknown key id, a frame that does not open, a replayed nonce. A caller
// must not be able to tell which check fired from the returned error, so
// every drop path returns this exact value rather than a wrapped cause; the
// specific reason goes only to the owner's log.
var ErrDrop = errors.New("gateway: admission failed")

// ErrShutdownRefused is returned for an otherwise-valid frame requesting
// shutdown. Unlike a drop this is not silent — refusing shutdown while
// forwarding every other op is the one authorization rule the design draws,
// and the caller answers with a sealed refusal rather than closing quietly.
var ErrShutdownRefused = errors.New("gateway: shutdown refused")

// Caller is an admitted frame's identity and the plaintext ready to forward
// to the daemon, Session already rewritten.
type Caller struct {
	KeyID string
	Name  string
	Body  []byte
}

// Admit runs the admission ladder in order, cheapest-first within what the
// protocol allows: header and version, timestamp window, key id, frame
// open, nonce, shutdown refusal, session rewrite. See
// docs/design/atomic-bus-network.md, "Admission".
func Admit(store *Store, window *Window, frame []byte, now time.Time) (*Caller, error) {
	header, _, err := remote.ParseHeader(frame)
	if err != nil {
		logDrop("malformed header or unknown version", "")
		return nil, ErrDrop
	}
	keyID := string(header.KeyID)

	ts := time.Unix(header.Timestamp, 0)
	if !window.Admissible(ts, now) {
		logDrop("timestamp outside window", keyID)
		return nil, ErrDrop
	}

	rec, ok, err := store.Lookup(keyID)
	if err != nil || !ok {
		logDrop("unknown or revoked key id", keyID)
		return nil, ErrDrop
	}

	plaintext, _, err := remote.OpenC2S(rec.Key, frame)
	if err != nil {
		logDrop("frame did not open", keyID)
		return nil, ErrDrop
	}

	if !window.Admit(keyID, header.Nonce, ts, now) {
		logDrop("nonce replayed", keyID)
		return nil, ErrDrop
	}

	var req bus.Request
	if err := json.Unmarshal(plaintext, &req); err != nil {
		logDrop("body did not decode", keyID)
		return nil, ErrDrop
	}

	if req.Op == bus.OpShutdown {
		log.Printf("gateway: shutdown refused (key_id=%s name=%s)", keyID, rec.Name)
		return nil, ErrShutdownRefused
	}

	req.Session = rewriteSession(keyID, req.Session)
	body, err := json.Marshal(req)
	if err != nil {
		logDrop("body did not re-encode", keyID)
		return nil, ErrDrop
	}

	return &Caller{KeyID: keyID, Name: rec.Name, Body: body}, nil
}

// rewriteSession rewrites unconditionally, including an empty client
// session: Hub records an empty session under "", so two different keys
// both sending Session: "" would otherwise resolve to the same member. The
// client half is attacker-supplied, so it is truncated to keep the combined
// id within bus.MaxIdentifierBytes regardless of what the daemon does with
// it downstream.
func rewriteSession(keyID, clientSession string) string {
	limit := bus.MaxIdentifierBytes - len(keyID) - 1
	if limit < 0 {
		limit = 0
	}
	if len(clientSession) > limit {
		clientSession = clientSession[:limit]
		// A raw byte cut can split a multi-byte rune, leaving invalid
		// UTF-8 that json.Marshal replaces with the 3-byte U+FFFD — that
		// can grow past limit once the daemon decodes the body. Back off
		// to the last valid boundary instead of cutting mid-rune.
		for len(clientSession) > 0 && !utf8.ValidString(clientSession) {
			clientSession = clientSession[:len(clientSession)-1]
		}
	}
	return keyID + "/" + clientSession
}

func logDrop(reason, keyID string) {
	if keyID == "" {
		log.Printf("gateway: drop: %s", reason)
		return
	}
	log.Printf("gateway: drop: %s (key_id=%s)", reason, keyID)
}
