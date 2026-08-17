package serve

// The streaming tests drive a real httptest.NewServer with a bounded-context
// client: a ResponseRecorder only works against a stream that ends on its own.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// readOneEvent decodes one SSE data frame, skipping the blank separator lines.
func readOneEvent(br *bufio.Reader) (changeEvent, error) {
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return changeEvent{}, err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		var ev changeEvent
		if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
			return changeEvent{}, err
		}
		return ev, nil
	}
}

// Over the cap the field is dropped whole, never truncated to a partial list.
func TestNewChangeEvent_OmitsChangedOverCap(t *testing.T) {
	atCap := make([]string, changedCap)
	for i := range atCap {
		atCap[i] = fmt.Sprintf("f%d.md", i)
	}

	ev := newChangeEvent("fp1", atCap)
	if ev.Changed == nil {
		t.Fatal("at-cap changed list must still be present")
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(payload), `"changed"`) {
		t.Errorf("expected changed field present at cap, got %s", payload)
	}

	overCap := append(atCap, "one-more.md")
	ev2 := newChangeEvent("fp1", overCap)
	if ev2.Changed != nil {
		t.Errorf("over-cap changed list must be omitted, got %v", ev2.Changed)
	}
	payload2, err := json.Marshal(ev2)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(payload2), `"changed"`) {
		t.Errorf("expected changed field omitted over cap, got %s", payload2)
	}
}

// count() is the ticker's zero-subscriber gate, so both edges must be exact.
func TestSubscriberRegistry_CountTracksSubscribeUnsubscribe(t *testing.T) {
	registry := newSubscriberRegistry()
	if got := registry.count(); got != 0 {
		t.Fatalf("count = %d, want 0 before any subscribe", got)
	}

	_, unsubA := registry.subscribe()
	_, unsubB := registry.subscribe()
	if got := registry.count(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}

	unsubA()
	if got := registry.count(); got != 1 {
		t.Fatalf("count = %d, want 1 after one unsubscribe", got)
	}

	unsubB()
	if got := registry.count(); got != 0 {
		t.Fatalf("count = %d, want 0 after both unsubscribed", got)
	}
}

// A subscriber that never drains must not block broadcast, and its slot keeps
// only the latest event rather than queueing every one.
func TestSubscriberRegistry_BroadcastCoalescesForSlowSubscriber(t *testing.T) {
	registry := newSubscriberRegistry()

	subA, unsubA := registry.subscribe()
	defer unsubA()
	subB, unsubB := registry.subscribe()
	defer unsubB()

	ev1 := changeEvent{FP: "fp1"}
	ev2 := changeEvent{FP: "fp2"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		registry.broadcast(ev1)
		registry.broadcast(ev2)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("broadcast blocked on an undrained subscriber slot")
	}

	select {
	case got := <-subA.ch:
		if got.FP != ev2.FP {
			t.Errorf("subA: got fp %q, want coalesced latest %q", got.FP, ev2.FP)
		}
	default:
		t.Error("subA slot must hold the coalesced latest event")
	}
	select {
	case got := <-subB.ch:
		if got.FP != ev2.FP {
			t.Errorf("subB: got fp %q, want coalesced latest %q", got.FP, ev2.FP)
		}
	default:
		t.Error("subB slot must hold the coalesced latest event")
	}
}

// A new subscription is pushed the current fp immediately; no ticker is started
// here, so a tick cannot be what delivers it.
func TestNewEventsHandler_ResyncPushOnSubscribe(t *testing.T) {
	root := t.TempDir()
	writeSnapFile(t, root, "a.md", "# A\n")

	store := newSnapshotStore(root, defaultTickInterval, defaultQuietWindow)
	registry := newSubscriberRegistry()

	srv := httptest.NewServer(NewEventsHandler(store, registry))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	ev, err := readOneEvent(bufio.NewReader(resp.Body))
	if err != nil {
		t.Fatalf("read resync event: %v", err)
	}
	if want := store.current().fp; ev.FP != want {
		t.Errorf("resync fp = %q, want current snapshot fp %q", ev.FP, want)
	}
}

// With no subscribers the ticker does no work at all, not even the cheap
// fingerprint walk.
func TestStartTicker_ZeroSubscribers_NoRebuild(t *testing.T) {
	root := t.TempDir()
	writeSnapFile(t, root, "a.md", "# A\n")

	interval := 20 * time.Millisecond
	store := newSnapshotStore(root, interval, defaultQuietWindow)
	store.ensureFresh() // warm, as NewSnapshotStore does

	registry := newSubscriberRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startTicker(ctx, store, registry, interval)

	// Planted while nobody is subscribed.
	writeSnapFile(t, root, "b.md", "# B\n")

	baseline := store.rebuildCalls.Load()
	time.Sleep(10 * interval)
	if got := store.rebuildCalls.Load(); got != baseline {
		t.Errorf("ticker rebuilt with 0 subscribers: rebuildCalls %d -> %d", baseline, got)
	}

	// The same change lands once someone subscribes, proving the gate — not some
	// other stall — explains the silence above.
	_, unsubscribe := registry.subscribe()
	defer unsubscribe()

	time.Sleep(10 * interval)
	if got := store.rebuildCalls.Load(); got == baseline {
		t.Error("ticker never rebuilt once a subscriber was attached")
	}
}

// Subscribed but unchanged on disk: repeated ensureFresh must stay a no-op.
func TestStartTicker_NoRebuildWhenFingerprintUnchanged(t *testing.T) {
	root := t.TempDir()
	writeSnapFile(t, root, "a.md", "# A\n")

	interval := 20 * time.Millisecond
	store := newSnapshotStore(root, interval, defaultQuietWindow)
	store.ensureFresh() // warm

	registry := newSubscriberRegistry()
	_, unsubscribe := registry.subscribe()
	defer unsubscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startTicker(ctx, store, registry, interval)

	baseline := store.rebuildCalls.Load()
	time.Sleep(10 * interval)
	if got := store.rebuildCalls.Load(); got != baseline {
		t.Errorf("ticker rebuilt an unchanged realm: rebuildCalls %d -> %d", baseline, got)
	}
}

// The second event a client reads is tick-triggered, not the resync push, and
// carries the changed relpath.
func TestStartTicker_BroadcastsChangeToSubscriberAfterTick(t *testing.T) {
	root := t.TempDir()
	writeSnapFile(t, root, "a.md", "# A\n")

	interval := 20 * time.Millisecond
	store := newSnapshotStore(root, interval, defaultQuietWindow)
	store.ensureFresh() // warm, as NewSnapshotStore does

	registry := newSubscriberRegistry()
	srv := httptest.NewServer(NewEventsHandler(store, registry))
	defer srv.Close()

	tickerCtx, cancelTicker := context.WithCancel(context.Background())
	defer cancelTicker()
	startTicker(tickerCtx, store, registry, interval)

	reqCtx, cancelReq := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelReq()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	if _, err := readOneEvent(br); err != nil {
		t.Fatalf("read resync event: %v", err)
	}

	writeSnapFile(t, root, "b.md", "# B\n")

	ev, err := readOneEvent(br)
	if err != nil {
		t.Fatalf("read tick-triggered event: %v", err)
	}
	found := false
	for _, c := range ev.Changed {
		if c == "b.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected b.md in tick-triggered changed set, got %v", ev.Changed)
	}
}
