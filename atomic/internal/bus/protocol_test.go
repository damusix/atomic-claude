package bus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
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

// TestEnvelope_Ts_MarshalsAsUnixSeconds is finding 3's regression: the
// documented wire contract (docs/design/atomic-bus.md) is "ts": 1753900000
// — a Unix-seconds integer — not Go's default RFC3339Nano string
// ("2026-07-28T04:39:59.609364-04:00"), which is what the daemon actually
// emitted before this fix.
func TestEnvelope_Ts_MarshalsAsUnixSeconds(t *testing.T) {
	when := time.Date(2026, 7, 28, 4, 39, 59, 0, time.UTC)
	env := Envelope{ID: "m-1234abcd", Room: "potato", From: "frontend", FromKind: "agent", Ts: when, Text: "hi"}

	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	tsRaw, ok := m["ts"]
	if !ok {
		t.Fatalf("missing \"ts\" field in %s", b)
	}
	if strings.Contains(string(tsRaw), `"`) {
		t.Fatalf(`"ts" = %s, want a bare integer (Unix seconds), not a quoted string`, tsRaw)
	}
	var gotSeconds int64
	if err := json.Unmarshal(tsRaw, &gotSeconds); err != nil {
		t.Fatalf("\"ts\" = %s does not parse as an integer: %v", tsRaw, err)
	}
	if gotSeconds != when.Unix() {
		t.Fatalf("\"ts\" = %d, want %d (Unix seconds)", gotSeconds, when.Unix())
	}
}

// TestEnvelope_Ts_RoundTripsThroughUnixSeconds proves the client side of
// finding 3's fix: an envelope decoded off the wire (room log, subscription
// frame, response payload) recovers the same instant to the second — the
// precision the wire format actually carries — not a decode error from
// trying to unmarshal an integer into a time.Time.
func TestEnvelope_Ts_RoundTripsThroughUnixSeconds(t *testing.T) {
	when := time.Date(2026, 7, 28, 4, 39, 59, 0, time.UTC)
	sent := Envelope{ID: "m-1234abcd", Room: "potato", From: "frontend", FromKind: "agent", Ts: when, Text: "hi"}

	b, err := json.Marshal(sent)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var received Envelope
	if err := json.Unmarshal(b, &received); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !received.Ts.Equal(when) {
		t.Fatalf("round-tripped Ts = %v, want %v", received.Ts, when)
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

// TestProtocolWireShape_GoldenFieldsAndOps pins the exact JSON field names on
// Request, Response, Envelope, and Member, plus the exact Op set — the wire
// shape ProtocolVersion gates — and, separately, ties that shape to
// ProtocolVersion itself via protocolShapeHashes.
//
// The field/op assertions below derive the observed field set reflectively
// from each struct's json tags (wireShapeFields), never from a marshalled
// instance: marshalling silently drops a zero-value omitempty field from the
// output, so a version of this test that populated a struct literal and
// inspected the marshalled keys could add a new omitempty field, leave it
// unset in the literal, and pass unmodified — exactly the drift this test
// exists to catch. Reflection sees the tag regardless of whether any value
// was ever assigned.
//
// The hash check below is what correlates a shape change with a version
// bump: six prior wire-shape-changing commits landed against a version that
// never moved off 1, because nothing tied "the golden lists changed" to
// "ProtocolVersion must also change" (docs/spec/atomic-bus.md's 2026-07-30
// "ProtocolVersion must bump when the wire changes" entry). Updating the
// golden field/op lists above to match a new shape, while leaving
// ProtocolVersion untouched, still fails here: protocolShapeHashes[2] is
// frozen to the hash of the shape at version 2, so a real shape change makes
// wireShapeHash() diverge from it regardless of whether the golden lists
// above were kept in sync. The only way back to green is either revert the
// shape, or bump ProtocolVersion and add a new protocolShapeHashes entry for
// it — see protocol.go's ProtocolVersion doc.
func TestProtocolWireShape_GoldenFieldsAndOps(t *testing.T) {
	assertGoldenFields(t, "Request", reflect.TypeOf(Request{}), []string{
		"op", "room", "rooms", "name", "mode", "kind", "session", "to",
		"reply_to", "text", "repo", "realm", "skip_self", "filters",
	})
	assertGoldenFields(t, "Response", reflect.TypeOf(Response{}), []string{"ok", "error", "code", "payload"})
	assertGoldenFields(t, "Envelope", reflect.TypeOf(Envelope{}), []string{
		"id", "room", "from", "from_kind", "from_repo", "from_realm", "to",
		"reply_to", "ts", "text", "truncated", "log", "closing",
	})
	assertGoldenFields(t, "Member", reflect.TypeOf(Member{}), []string{
		"name", "kind", "mode", "session", "joined", "last_seen", "stale", "repo", "realm",
	})

	wantOps := []string{
		OpPing, OpJoin, OpLeave, OpSend, OpSay, OpRecv, OpTail, OpWho, OpRooms,
		OpHalt, OpResume, OpShutdown, OpPrune, OpClose,
	}
	gotOps := append([]string(nil), AllOps...)
	sort.Strings(gotOps)
	wantOpsSorted := append([]string(nil), wantOps...)
	sort.Strings(wantOpsSorted)
	if !reflect.DeepEqual(gotOps, wantOpsSorted) {
		t.Fatalf("AllOps = %v, want %v", gotOps, wantOpsSorted)
	}

	gotHash := wireShapeHash()
	wantHash, ok := protocolShapeHashes[ProtocolVersion]
	if !ok {
		t.Fatalf("no golden wire-shape hash recorded for ProtocolVersion %d (computed %s) — bumping ProtocolVersion requires adding a matching entry to protocolShapeHashes in this file, in the same change", ProtocolVersion, gotHash)
	}
	if gotHash != wantHash {
		t.Fatalf("wire shape hash = %s, want %s for ProtocolVersion %d — Request/Response/Envelope/Member's json tags or AllOps changed; a wire change requires bumping ProtocolVersion in protocol.go AND recording the new hash (%s) as its protocolShapeHashes entry here, in the same change", gotHash, wantHash, ProtocolVersion, gotHash)
	}
}

// protocolShapeHashes is the versioned golden record wireShapeHash's output
// is checked against — one entry per ProtocolVersion that has ever shipped a
// wire-shape-changing commit. This is what ties a shape change to a version
// bump: the entry for the CURRENT ProtocolVersion is frozen to the shape
// that version actually shipped, so changing the shape without bumping the
// version leaves the lookup pointed at a now-stale hash (mismatch), and
// bumping the version without adding an entry here leaves the lookup with
// nothing to find (missing-key failure) — either way,
// TestProtocolWireShape_GoldenFieldsAndOps only returns to green once both
// move together.
var protocolShapeHashes = map[int]string{
	2: "f4bf0980c3ca8d8177280563a956e7fd9383a1c529123e2b5d6608f703b08144",
}

// wireShapeFields returns the JSON field names declared via struct tag on
// typ's exported fields, in declaration order — derived from the type
// itself via reflection, never from a marshalled instance (see this file's
// TestProtocolWireShape_GoldenFieldsAndOps doc for why that distinction is
// load-bearing: a marshalled zero-value omitempty field is indistinguishable
// from a field that was never declared).
func wireShapeFields(typ reflect.Type) []string {
	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag, ok := f.Tag.Lookup("json")
		if !ok {
			fields = append(fields, f.Name)
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields = append(fields, name)
	}
	return fields
}

// wireShapeHash hashes the current wire shape — Request/Response/Envelope/
// Member's json field names (wireShapeFields) plus AllOps — into one value,
// each field/op set sorted first so declaration order is not part of what
// gets pinned. Compared against protocolShapeHashes[ProtocolVersion] to
// correlate a shape change with a version bump.
func wireShapeHash() string {
	var parts []string
	for _, pair := range []struct {
		label string
		typ   reflect.Type
	}{
		{"Request", reflect.TypeOf(Request{})},
		{"Response", reflect.TypeOf(Response{})},
		{"Envelope", reflect.TypeOf(Envelope{})},
		{"Member", reflect.TypeOf(Member{})},
	} {
		fields := wireShapeFields(pair.typ)
		sort.Strings(fields)
		parts = append(parts, pair.label+":"+strings.Join(fields, ","))
	}
	ops := append([]string(nil), AllOps...)
	sort.Strings(ops)
	parts = append(parts, "Ops:"+strings.Join(ops, ","))

	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

// assertGoldenFields asserts typ's json-tagged field set (wireShapeFields,
// reflected from struct tags — see that function's doc for why not a
// marshalled instance) is exactly want, order ignored.
func assertGoldenFields(t *testing.T, label string, typ reflect.Type, want []string) {
	t.Helper()
	got := wireShapeFields(typ)
	sort.Strings(got)
	wantSorted := append([]string(nil), want...)
	sort.Strings(wantSorted)
	if !reflect.DeepEqual(got, wantSorted) {
		t.Fatalf("%s wire fields = %v, want %v — a field was added, removed, or renamed; update this golden list and correlate it with a ProtocolVersion bump (see protocolShapeHashes)", label, got, wantSorted)
	}
}
