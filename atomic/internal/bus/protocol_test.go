package bus

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestEnvelope_ToMarshalsEmptyAsArrayNotNull pins the wire invariant: an
// envelope's To must always serialize as "to":[], never "to":null or an
// omitted key. This is not cosmetic — a receiver reads len(To) == 0 as "this
// was an FYI to the whole room"; if To could also come across as null or
// absent, that would be indistinguishable from a malformed frame that
// forgot to set it at all.
func TestEnvelope_ToMarshalsEmptyAsArrayNotNull(t *testing.T) {
	cases := []struct {
		name string
		to   []string
	}{
		{"nil To (never set)", nil},
		{"explicit empty To", []string{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := Envelope{ID: "k1", Room: "potato", From: "frontend", FromKind: "agent", To: tc.to, Text: "status update"}

			b, err := json.Marshal(env)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			// Assert on the raw bytes, not a re-decoded struct: decoding
			// first would collapse "null" and "[]" back to the same nil
			// slice, hiding exactly the wire-format bug this test exists
			// to catch.
			if !strings.Contains(string(b), `"to":[]`) {
				t.Fatalf("expected %q in marshaled bytes, got: %s", `"to":[]`, b)
			}
			if strings.Contains(string(b), `"to":null`) {
				t.Fatalf("to must never marshal as null (ambiguous with a missing field): %s", b)
			}
		})
	}
}

func TestEnvelope_ToWithAddresseesMarshalsPopulated(t *testing.T) {
	env := Envelope{ID: "k2", Room: "potato", From: "frontend", FromKind: "agent", To: []string{"backend"}, Text: "please pick this up"}

	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(b), `"to":["backend"]`) {
		t.Fatalf("expected an addressed To to survive marshal, got: %s", b)
	}
}

// TestEnvelope_ToRoundTripPreservesFYISemantics is the round-trip half of
// the invariant above: after Marshal -> Unmarshal, an FYI envelope's To
// decodes as a non-nil empty slice, so len(env.To) == 0 stays a reliable
// "addressed to everyone" check on the receiving side — the reaction-policy
// distinction (act vs. note) skills/atomic-bus depends on — rather than
// something that only holds pre-marshal.
func TestEnvelope_ToRoundTripPreservesFYISemantics(t *testing.T) {
	sent := Envelope{ID: "k3", Room: "potato", From: "referee", FromKind: "human", To: nil, Text: "fyi: build is green"}

	b, err := json.Marshal(sent)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var received Envelope
	if err := json.Unmarshal(b, &received); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if received.To == nil {
		t.Fatal("To decoded as nil; a receiver checking To == nil vs len(To) == 0 would now disagree with the sender's FYI intent")
	}
	if len(received.To) != 0 {
		t.Fatalf("expected no addressees, got %v", received.To)
	}
}

func TestEnvelope_JSONFieldNamesMatchWireContract(t *testing.T) {
	env := Envelope{
		ID: "k4", Room: "potato", From: "frontend", FromKind: "agent",
		To: []string{"backend"}, ReplyTo: "k1", Ts: time.Unix(0, 0).UTC(),
		Text: "hi", Truncated: 12, Log: "rooms/potato.log",
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// docs/design/atomic-bus.md: "Envelope — id, room, from, from_kind, to,
	// reply_to, ts, text, truncated, log".
	for _, key := range []string{"id", "room", "from", "from_kind", "to", "reply_to", "ts", "text", "truncated", "log"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing wire field %q in %s", key, b)
		}
	}
}

func TestMember_JSONFieldNamesMatchWireContract(t *testing.T) {
	member := Member{Name: "backend", Kind: "agent", Mode: "normal", Session: "sess-1", Joined: time.Unix(0, 0).UTC()}
	b, err := json.Marshal(member)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// docs/design/atomic-bus.md: "Member — name, kind, mode, session, joined".
	for _, key := range []string{"name", "kind", "mode", "session", "joined"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing wire field %q in %s", key, b)
		}
	}
}

// TestResponse_CodeSurvivesRoundTrip locks in the contract that
// Response.Code carries the exit code a client should terminate with: the
// daemon decides it once, and the client must recover the exact value
// without re-deriving it from Error's free-text message.
func TestResponse_CodeSurvivesRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		code ExitCode
	}{
		{"not joined", ExitNotJoined},
		{"name taken", ExitNameTaken},
		{"unreachable", ExitUnreachable},
		{"halted", ExitHalted},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sent := Response{OK: false, Error: "boom", Code: tc.code}

			b, err := json.Marshal(sent)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var received Response
			if err := json.Unmarshal(b, &received); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if received.Code != tc.code {
				t.Fatalf("Code = %d, want %d — the client would exit with the wrong status", received.Code, tc.code)
			}
			if received.OK {
				t.Fatal("OK should be false on an error response")
			}
		})
	}
}

// TestExitCodes_StableValues pins the numeric exit codes named across
// docs/spec/atomic-bus.md's success criteria and docs/design/atomic-bus.md's
// flows. protocol.go is not revisited by later checkpoints (see the spec's
// Checkpoints table), so client.go and daemon.go will hardcode expectations
// against these numbers — a silent reorder of the iota block would be a
// wire-compatibility break the compiler cannot catch.
func TestExitCodes_StableValues(t *testing.T) {
	cases := []struct {
		name string
		code ExitCode
		want ExitCode
	}{
		{"ExitOK", ExitOK, 0},
		{"ExitUsage", ExitUsage, 1},
		{"ExitHard", ExitHard, 2},
		{"ExitNotJoined", ExitNotJoined, 3},
		{"ExitNameTaken", ExitNameTaken, 4},
		{"ExitNoRoom", ExitNoRoom, 5},
		{"ExitUnreachable", ExitUnreachable, 6},
		{"ExitHalted", ExitHalted, 7},
	}
	for _, tc := range cases {
		if tc.code != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.code, tc.want)
		}
	}
}

func TestBusError_ImplementsError(t *testing.T) {
	var err error = &Error{Code: ExitNotJoined, Msg: "not joined"}
	if err.Error() != "not joined" {
		t.Fatalf("Error() = %q, want %q", err.Error(), "not joined")
	}
}
