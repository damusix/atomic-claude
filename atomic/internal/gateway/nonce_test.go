package gateway

import (
	"testing"
	"time"
)

func TestWindow_Admissible(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)

	tests := []struct {
		name string
		ts   time.Time
		now  time.Time
		want bool
	}{
		{"exactly now", start, start, true},
		{"within window ahead", start.Add(90 * time.Second), start, true},
		{"within window behind", start.Add(90 * time.Second), start.Add(90 * time.Second).Add(90 * time.Second), true},
		{"just outside the window ahead", start.Add(AdmissionWindow + time.Second), start, false},
		{"just outside the window behind", start, start.Add(AdmissionWindow + time.Second), false},
		{
			// The frame's own timestamp sits inside a bare now-window of the
			// query time, but it predates gateway start, so the raised low
			// edge must reject it.
			"before gateway start, within a bare now-window",
			start.Add(-30 * time.Second),
			start.Add(60 * time.Second),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWindow(AdmissionWindow, start)
			if got := w.Admissible(tt.ts, tt.now); got != tt.want {
				t.Fatalf("Admissible(%v, %v) = %v, want %v", tt.ts, tt.now, got, tt.want)
			}
		})
	}
}

// TestNewWindow_TruncatesStartToSecond proves a frame timestamped at exactly
// the gateway's start second is admitted even when start itself carries
// sub-second precision — Header.Timestamp is always Unix-second-truncated,
// so an untruncated start would sit ahead of it and reject an admissible
// frame.
func TestNewWindow_TruncatesStartToSecond(t *testing.T) {
	start := time.Unix(1_700_000_000, 500_000_000)
	w := NewWindow(AdmissionWindow, start)

	ts := time.Unix(1_700_000_000, 0)
	if !w.Admissible(ts, start) {
		t.Fatalf("frame timestamped at the gateway's start second must be admissible")
	}
}

func TestWindow_Admit_FirstSightingAdmitted(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	w := NewWindow(AdmissionWindow, start)

	var nonce [12]byte
	nonce[0] = 1

	if !w.Admit("k1", nonce, start, start) {
		t.Fatalf("first sighting must be admitted")
	}
}

func TestWindow_Admit_ReplayInsideWindowRejected(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	w := NewWindow(AdmissionWindow, start)

	var nonce [12]byte
	nonce[0] = 2

	if !w.Admit("k1", nonce, start, start) {
		t.Fatalf("first sighting must be admitted")
	}
	replayAt := start.Add(10 * time.Second)
	if w.Admit("k1", nonce, start, replayAt) {
		t.Fatalf("replay of the same (key id, nonce) inside the window must be rejected")
	}
}

func TestWindow_Admit_DifferentKeyIDsDoNotCollide(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	w := NewWindow(AdmissionWindow, start)

	var nonce [12]byte
	nonce[0] = 3

	if !w.Admit("k1", nonce, start, start) {
		t.Fatalf("first sighting under k1 must be admitted")
	}
	if !w.Admit("k2", nonce, start, start) {
		t.Fatalf("the same nonce under a different key id must be admitted")
	}
}

func TestWindow_Admit_SweepsEntriesBelowLowEdge(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	w := NewWindow(AdmissionWindow, start)

	var nonce [12]byte
	nonce[0] = 5
	seenAt := start.Add(200 * time.Second)

	if !w.Admit("k1", nonce, seenAt, seenAt) {
		t.Fatalf("first sighting must be admitted")
	}

	// Once "now" advances far enough that seenAt falls below the low edge,
	// the entry sweeps out and the same nonce reads as unseen again.
	farLater := seenAt.Add(AdmissionWindow + time.Second)
	if !w.Admit("k1", nonce, seenAt, farLater) {
		t.Fatalf("a swept-out nonce must be treated as unseen")
	}
}
