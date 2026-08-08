package repl

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// TestExitCodes_PinnedValues is the spec's single place where all seven exit
// codes' literal values are locked. Agents route on these numbers, so a
// reordering that silently renumbers them is a breaking change to every caller
// — this table is what makes that fail a test instead of shipping.
func TestExitCodes_PinnedValues(t *testing.T) {
	tests := []struct {
		name string
		code ExitCode
		want int
	}{
		{"ok", ExitOK, 0},
		{"usage error", ExitUsage, 1},
		{"session not found", ExitNotFound, 2},
		{"eval exception", ExitEvalException, 3},
		{"timeout", ExitTimeout, 4},
		{"session dead", ExitDead, 5},
		{"interpreter unavailable", ExitInterpreterUnavailable, 6},
		{"protocol mismatch", ExitProtocolMismatch, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.code) != tt.want {
				t.Errorf("exit code %s = %d, want %d", tt.name, int(tt.code), tt.want)
			}
		})
	}

	seen := map[int]string{}
	for _, tt := range tests {
		if prior, dup := seen[tt.want]; dup {
			t.Fatalf("exit code %d used by both %q and %q; codes must be distinct", tt.want, prior, tt.name)
		}
		seen[tt.want] = tt.name
	}
}

func TestProtocolVersion_IsPinned(t *testing.T) {
	// Bumping this is a real decision (it retires every running harness), not
	// a drive-by edit — the assertion exists to force the decision to be made
	// deliberately, alongside the two harness scripts' own constants.
	if ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d, want 1", ProtocolVersion)
	}
}

func TestMaxStreamBytes_Is64KiB(t *testing.T) {
	if MaxStreamBytes != 64*1024 {
		t.Errorf("MaxStreamBytes = %d, want %d", MaxStreamBytes, 64*1024)
	}
}

func TestAllOps_MatchesGoldenList(t *testing.T) {
	want := []string{"eval", "ping", "reset", "shutdown"}
	got := append([]string(nil), AllOps...)
	sort.Strings(got)
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(got, sorted) {
		t.Errorf("AllOps = %v, want %v (add the op to both AllOps and each harness's dispatch)", AllOps, want)
	}

	for _, pair := range []struct{ constant, literal string }{
		{OpEval, "eval"},
		{OpPing, "ping"},
		{OpReset, "reset"},
		{OpShutdown, "shutdown"},
	} {
		if pair.constant != pair.literal {
			t.Errorf("op constant = %q, want %q", pair.constant, pair.literal)
		}
	}
}

// TestResponse_EveryFieldAlwaysMarshaled pins the wire contract from the Go
// side: a zero Response still emits all seven keys, with value and error as
// empty strings rather than null or absent. The harness side of the same
// contract is asserted against live JSON in harness_contract_test.go.
func TestResponse_EveryFieldAlwaysMarshaled(t *testing.T) {
	raw, err := json.Marshal(Response{})
	if err != nil {
		t.Fatalf("marshal zero Response: %v", err)
	}

	var keyed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keyed); err != nil {
		t.Fatalf("unmarshal into key map: %v", err)
	}
	assertExactKeys(t, keyed, responseWireKeys)

	if string(keyed["value"]) != `""` {
		t.Errorf("zero Response value = %s, want \"\"", keyed["value"])
	}
	if string(keyed["error"]) != `""` {
		t.Errorf("zero Response error = %s, want \"\"", keyed["error"])
	}
	if string(keyed["truncated"]) != "false" {
		t.Errorf("zero Response truncated = %s, want false", keyed["truncated"])
	}
}

func TestRequest_WireShape(t *testing.T) {
	raw, err := json.Marshal(Request{V: ProtocolVersion, Op: OpEval, Code: "1 + 1"})
	if err != nil {
		t.Fatalf("marshal Request: %v", err)
	}
	want := `{"v":1,"op":"eval","code":"1 + 1"}`
	if string(raw) != want {
		t.Errorf("Request wire shape = %s, want %s", raw, want)
	}
}

// responseWireKeys is the exact key set a Response frame carries, in the order
// the spec documents it.
var responseWireKeys = []string{"v", "ok", "stdout", "stderr", "value", "error", "truncated"}

func assertExactKeys(t *testing.T, keyed map[string]json.RawMessage, want []string) {
	t.Helper()
	for _, key := range want {
		if _, ok := keyed[key]; !ok {
			t.Errorf("response frame is missing key %q; every field is always present", key)
		}
	}
	if len(keyed) != len(want) {
		got := make([]string, 0, len(keyed))
		for key := range keyed {
			got = append(got, key)
		}
		sort.Strings(got)
		t.Errorf("response frame keys = %v, want exactly %v", got, want)
	}
}
