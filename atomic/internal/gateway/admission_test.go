package gateway

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/damusix/atomic-claude/atomic/internal/bus/remote"
)

func enrolledStore(t *testing.T, name string) (*Store, Record) {
	t.Helper()
	s := NewStore(filepath.Join(t.TempDir(), "keys.json"))
	rec, err := s.Enroll(name)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	return s, rec
}

func sealedFrame(t *testing.T, key []byte, keyID string, req bus.Request, now time.Time) []byte {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	frame, _, err := remote.SealC2S(key, []byte(keyID), body, now)
	if err != nil {
		t.Fatalf("SealC2S: %v", err)
	}
	return frame
}

// Table-driven: every silent drop path returns the same identity-comparable
// outcome, so a caller cannot branch on why a frame was refused.
func TestAdmit_DropPaths_AreIndistinguishable(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	req := bus.Request{Op: bus.OpPing}

	tests := []struct {
		name  string
		frame func(t *testing.T, s *Store, rec Record) []byte
		now   time.Time
	}{
		{
			name: "malformed header",
			frame: func(t *testing.T, s *Store, rec Record) []byte {
				return []byte{0xFF}
			},
			now: start,
		},
		{
			name: "unknown version",
			frame: func(t *testing.T, s *Store, rec Record) []byte {
				frame := sealedFrame(t, rec.Key, rec.ID, req, start)
				tampered := append([]byte(nil), frame...)
				tampered[0] = 99
				return tampered
			},
			now: start,
		},
		{
			name: "timestamp outside window",
			frame: func(t *testing.T, s *Store, rec Record) []byte {
				return sealedFrame(t, rec.Key, rec.ID, req, start.Add(-500*time.Second))
			},
			now: start,
		},
		{
			name: "unknown key id",
			frame: func(t *testing.T, s *Store, rec Record) []byte {
				return sealedFrame(t, rec.Key, "not-a-real-key-id", req, start)
			},
			now: start,
		},
		{
			name: "revoked key id",
			frame: func(t *testing.T, s *Store, rec Record) []byte {
				frame := sealedFrame(t, rec.Key, rec.ID, req, start)
				if err := s.Revoke(rec.Name); err != nil {
					t.Fatalf("Revoke: %v", err)
				}
				return frame
			},
			now: start,
		},
		{
			name: "frame did not open (altered byte)",
			frame: func(t *testing.T, s *Store, rec Record) []byte {
				frame := sealedFrame(t, rec.Key, rec.ID, req, start)
				tampered := append([]byte(nil), frame...)
				tampered[len(tampered)-1] ^= 0xFF
				return tampered
			},
			now: start,
		},
		{
			name: "plaintext not JSON",
			frame: func(t *testing.T, s *Store, rec Record) []byte {
				frame, _, err := remote.SealC2S(rec.Key, []byte(rec.ID), []byte("not json"), start)
				if err != nil {
					t.Fatalf("SealC2S: %v", err)
				}
				return frame
			},
			now: start,
		},
	}

	var outcomes []error
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, rec := enrolledStore(t, "web-api")
			window := NewWindow(AdmissionWindow, start)
			frame := tt.frame(t, s, rec)

			caller, err := Admit(s, window, frame, tt.now)
			if caller != nil {
				t.Fatalf("expected no caller on a drop, got %+v", caller)
			}
			if err != ErrDrop {
				t.Fatalf("expected ErrDrop, got %v", err)
			}
			outcomes = append(outcomes, err)
		})
	}

	for _, o := range outcomes {
		if o != ErrDrop {
			t.Fatalf("every drop path must return the exact same error value, got %v", o)
		}
	}
}

func TestAdmit_ReplayedNonce_RejectedInsideWindow(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	s, rec := enrolledStore(t, "web-api")
	window := NewWindow(AdmissionWindow, start)

	frame := sealedFrame(t, rec.Key, rec.ID, bus.Request{Op: bus.OpPing}, start)

	if _, err := Admit(s, window, frame, start); err != nil {
		t.Fatalf("first admission: %v", err)
	}

	replayFrame := append([]byte(nil), frame...)
	if _, err := Admit(s, window, replayFrame, start.Add(10*time.Second)); err != ErrDrop {
		t.Fatalf("replay inside the window must return ErrDrop, got %v", err)
	}
}

func TestAdmit_ReplayedNonce_RejectedOnTimestampOutsideWindow(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	s, rec := enrolledStore(t, "web-api")
	window := NewWindow(AdmissionWindow, start)

	frame := sealedFrame(t, rec.Key, rec.ID, bus.Request{Op: bus.OpPing}, start)

	if _, err := Admit(s, window, frame, start); err != nil {
		t.Fatalf("first admission: %v", err)
	}

	// Replayed after the window has closed: dropped on the timestamp check,
	// never reaching the nonce check at all.
	replayFrame := append([]byte(nil), frame...)
	if _, err := Admit(s, window, replayFrame, start.Add(500*time.Second)); err != ErrDrop {
		t.Fatalf("replay outside the window must return ErrDrop, got %v", err)
	}
}

// A frame captured before a gateway restart, replayed after it and still
// inside the 120s window, must be dropped even though the restart cleared
// the in-memory seen-nonce set. The low edge (max(now-window, start)) is
// what catches it: the frame's own timestamp predates the new gateway
// start, so it fails the timestamp check before the nonce set is ever
// consulted.
func TestAdmit_FrameFromBeforeRestart_DroppedAfterRestart(t *testing.T) {
	s, rec := enrolledStore(t, "web-api")

	preRestart := time.Unix(1_700_000_000, 0)
	frame := sealedFrame(t, rec.Key, rec.ID, bus.Request{Op: bus.OpPing}, preRestart)

	restart := preRestart.Add(30 * time.Second)
	window := NewWindow(AdmissionWindow, restart)

	replayAfterRestart := restart.Add(10 * time.Second)
	if _, err := Admit(s, window, frame, replayAfterRestart); err != ErrDrop {
		t.Fatalf("frame captured before restart must be dropped after it, got %v", err)
	}
}

func TestAdmit_ShutdownRefused(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	s, rec := enrolledStore(t, "web-api")
	window := NewWindow(AdmissionWindow, start)

	frame := sealedFrame(t, rec.Key, rec.ID, bus.Request{Op: bus.OpShutdown}, start)

	caller, err := Admit(s, window, frame, start)
	if caller != nil {
		t.Fatalf("expected no caller on a shutdown refusal, got %+v", caller)
	}
	if err != ErrShutdownRefused {
		t.Fatalf("expected ErrShutdownRefused, got %v", err)
	}
}

func TestAdmit_EveryOpExceptShutdownForwarded(t *testing.T) {
	ops := []string{
		bus.OpPing, bus.OpJoin, bus.OpLeave, bus.OpSend, bus.OpSay, bus.OpRecv,
		bus.OpTail, bus.OpWho, bus.OpRooms, bus.OpHalt, bus.OpResume,
		bus.OpPrune, bus.OpClose, bus.OpEnd, bus.OpRead,
	}
	start := time.Unix(1_700_000_000, 0)

	for _, op := range ops {
		t.Run(op, func(t *testing.T) {
			s, rec := enrolledStore(t, "web-api")
			window := NewWindow(AdmissionWindow, start)
			frame := sealedFrame(t, rec.Key, rec.ID, bus.Request{Op: op, Room: "potato"}, start)

			caller, err := Admit(s, window, frame, start)
			if err != nil {
				t.Fatalf("op %q: unexpected error: %v", op, err)
			}
			if caller == nil {
				t.Fatalf("op %q: expected a caller", op)
			}

			var forwarded bus.Request
			if err := json.Unmarshal(caller.Body, &forwarded); err != nil {
				t.Fatalf("op %q: forwarded body did not decode: %v", op, err)
			}
			if forwarded.Op != op {
				t.Fatalf("op %q: forwarded op = %q", op, forwarded.Op)
			}
		})
	}
}

// Hub records an empty session under "", so two keys both sending an empty
// session must resolve to two different rewritten sessions, not one.
func TestAdmit_TwoKeysWithEmptySession_ResolveToDifferentMembers(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)

	sA, recA := enrolledStore(t, "machine-a")
	sB := NewStore(filepath.Join(t.TempDir(), "keys.json"))
	recB, err := sB.Enroll("machine-b")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	frameA := sealedFrame(t, recA.Key, recA.ID, bus.Request{Op: bus.OpJoin, Room: "potato", Session: ""}, start)
	frameB := sealedFrame(t, recB.Key, recB.ID, bus.Request{Op: bus.OpJoin, Room: "potato", Session: ""}, start)

	callerA, err := Admit(sA, NewWindow(AdmissionWindow, start), frameA, start)
	if err != nil {
		t.Fatalf("Admit A: %v", err)
	}
	callerB, err := Admit(sB, NewWindow(AdmissionWindow, start), frameB, start)
	if err != nil {
		t.Fatalf("Admit B: %v", err)
	}

	var reqA, reqB bus.Request
	if err := json.Unmarshal(callerA.Body, &reqA); err != nil {
		t.Fatalf("decode A: %v", err)
	}
	if err := json.Unmarshal(callerB.Body, &reqB); err != nil {
		t.Fatalf("decode B: %v", err)
	}

	if reqA.Session == "" || reqB.Session == "" {
		t.Fatalf("rewritten session must never be empty: A=%q B=%q", reqA.Session, reqB.Session)
	}
	if reqA.Session == reqB.Session {
		t.Fatalf("two keys sending an empty session resolved to the same rewritten session %q", reqA.Session)
	}
}

func TestAdmit_SessionRewrite_PrependsKeyIDAlways(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	s, rec := enrolledStore(t, "web-api")
	window := NewWindow(AdmissionWindow, start)

	frame := sealedFrame(t, rec.Key, rec.ID, bus.Request{Op: bus.OpJoin, Room: "potato", Session: "sess-1"}, start)

	caller, err := Admit(s, window, frame, start)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	var forwarded bus.Request
	if err := json.Unmarshal(caller.Body, &forwarded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := rec.ID + "/sess-1"
	if forwarded.Session != want {
		t.Fatalf("Session = %q, want %q", forwarded.Session, want)
	}
}

func TestAdmit_SessionRewrite_CapsAttackerSuppliedHalf(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	s, rec := enrolledStore(t, "web-api")
	window := NewWindow(AdmissionWindow, start)

	huge := make([]byte, bus.MaxIdentifierBytes*4)
	for i := range huge {
		huge[i] = 'a'
	}
	frame := sealedFrame(t, rec.Key, rec.ID, bus.Request{Op: bus.OpJoin, Room: "potato", Session: string(huge)}, start)

	caller, err := Admit(s, window, frame, start)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	var forwarded bus.Request
	if err := json.Unmarshal(caller.Body, &forwarded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(forwarded.Session) > bus.MaxIdentifierBytes {
		t.Fatalf("rewritten session is %d bytes, over MaxIdentifierBytes (%d)", len(forwarded.Session), bus.MaxIdentifierBytes)
	}
}

// A raw byte truncation landing inside a multi-byte rune leaves invalid
// UTF-8 behind; encoding/json then replaces each invalid byte with the
// 3-byte U+FFFD, growing what the daemon decodes back past the cap the
// gateway computed. The truncation point below sits one byte inside a
// 3-byte '€' rune, which is what forces the split.
func TestAdmit_SessionRewrite_TruncatesOnRuneBoundary(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	s, rec := enrolledStore(t, "web-api")
	window := NewWindow(AdmissionWindow, start)

	limit := bus.MaxIdentifierBytes - len(rec.ID) - 1
	clientSession := strings.Repeat("a", limit-1) + "€€€€"

	frame := sealedFrame(t, rec.Key, rec.ID, bus.Request{Op: bus.OpJoin, Room: "potato", Session: clientSession}, start)

	caller, err := Admit(s, window, frame, start)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}

	var forwarded bus.Request
	if err := json.Unmarshal(caller.Body, &forwarded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(forwarded.Session) > bus.MaxIdentifierBytes {
		t.Fatalf("decoded session is %d bytes, over MaxIdentifierBytes (%d)", len(forwarded.Session), bus.MaxIdentifierBytes)
	}
}
