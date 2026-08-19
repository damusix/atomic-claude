package bus

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"
)

// Unlike dialSubscribe, a refusal is returned rather than failing the test.
func trySubscribe(t *testing.T, addr string, req Request) Response {
	t.Helper()

	conn, err := net.DialTimeout("unix", addr, wireTimeout)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	line := readLineBounded(t, bufio.NewReader(conn), wireTimeout)
	if !line.ok {
		t.Fatal("timed out waiting for the subscription's opening response")
	}
	var resp Response
	if err := json.Unmarshal(line.data, &resp); err != nil {
		t.Fatalf("unmarshal opening response: %v", err)
	}
	return resp
}

// The closing envelope can be dropped when the member's buffer is full, and
// recv then reconnects. Refusing the resubscribe is what holds the eviction.
func TestServe_End_EvictedSessionCannotResubscribeUntilItRejoins(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "victim", Kind: KindAgent, Session: "sess-victim"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpEnd, Room: "potato", Name: "victim"}); !resp.OK {
		t.Fatalf("end failed: %s", resp.Error)
	}

	refused := trySubscribe(t, addr, Request{Op: OpRecv, Room: "potato", Session: "sess-victim"})
	if refused.OK {
		t.Fatal("evicted session resubscribed; its Monitor would keep running after the operator ended it")
	}
	if refused.Code != ExitNotJoined {
		t.Errorf("refusal code = %v, want %v (not joined)", refused.Code, ExitNotJoined)
	}

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "victim", Kind: KindAgent, Session: "sess-victim"}); !resp.OK {
		t.Fatalf("rejoin: %s", resp.Error)
	}
	if again := trySubscribe(t, addr, Request{Op: OpRecv, Room: "potato", Session: "sess-victim"}); !again.OK {
		t.Errorf("rejoined session was still refused: %s", again.Error)
	}
}

// Without this payload the caller cannot clear bus.json, and Rehydrate restores
// the member on the next daemon start.
func TestServe_End_ReportsEvictedSession(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "victim", Kind: KindAgent, Session: "sess-victim"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}
	resp := dialAndDo(t, addr, Request{Op: OpEnd, Room: "potato", Name: "victim"})
	if !resp.OK {
		t.Fatalf("end failed: %s", resp.Error)
	}
	var payload struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal end payload: %v", err)
	}
	if payload.Session != "sess-victim" {
		t.Errorf("payload session = %q, want %q", payload.Session, "sess-victim")
	}
}

// Driven over the real socket: what happens to a live listener is the point of
// the verb, and only the wire shows it.
func TestServe_End_EvictedConnectionEndsButPeerKeepsStreaming(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	for _, seed := range []Request{
		{Op: OpJoin, Room: "potato", Name: "victim", Kind: KindAgent, Session: "sess-victim"},
		{Op: OpJoin, Room: "potato", Name: "bystander", Kind: KindAgent, Session: "sess-bystander"},
	} {
		if resp := dialAndDo(t, addr, seed); !resp.OK {
			t.Fatalf("seed join %s: %s", seed.Name, resp.Error)
		}
	}

	victimConn, victimR := dialSubscribe(t, addr, Request{Op: OpRecv, Room: "potato", Session: "sess-victim"})
	defer victimConn.Close()
	peerConn, peerR := dialSubscribe(t, addr, Request{Op: OpRecv, Room: "potato", Session: "sess-bystander"})
	defer peerConn.Close()

	if resp := dialAndDo(t, addr, Request{Op: OpEnd, Room: "potato", Name: "victim"}); !resp.OK {
		t.Fatalf("end failed: %s", resp.Error)
	}

	env, ok := readEnvelopeBounded(t, victimR)
	if !ok {
		t.Fatal("timed out waiting for the eviction envelope")
	}
	if !env.Closing {
		t.Errorf("eviction envelope Closing = false; recv reconnects unless the last envelope says otherwise")
	}

	type readResult struct {
		data []byte
		err  error
	}
	done := make(chan readResult, 1)
	go func() {
		data, err := victimR.ReadBytes('\n')
		done <- readResult{data: data, err: err}
	}()
	select {
	case res := <-done:
		if res.err == nil {
			t.Fatalf("evicted connection delivered another frame instead of ending: %s", res.data)
		}
	case <-time.After(wireTimeout):
		t.Fatal("evicted connection did not end; the agent's listener would keep running after the operator ended it")
	}

	if resp := dialAndDo(t, addr, Request{Op: OpSay, Room: "potato", Text: "still here"}); !resp.OK {
		t.Fatalf("say after eviction: %s", resp.Error)
	}
	peerEnv, ok := readEnvelopeBounded(t, peerR)
	if !ok {
		t.Fatal("peer stopped receiving after another member was evicted")
	}
	if peerEnv.Text != "still here" {
		t.Errorf("peer got %q, want %q", peerEnv.Text, "still here")
	}

	whoResp := dialAndDo(t, addr, Request{Op: OpWho, Room: "potato"})
	if !whoResp.OK {
		t.Fatalf("who after eviction: %s", whoResp.Error)
	}
}
