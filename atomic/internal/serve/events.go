// events.go — CP3 (live-reload): the /events SSE endpoint and the
// subscriber-gated server ticker.
//
// Today, a change on disk is only reflected the next time a handler happens
// to be requested — the snapshot store (snapshot.go) validates lazily, but
// nothing pushes that fact to an open browser tab. This file adds the push
// side: /events streams {fp, changed} over a plain EventSource, fed by a
// single ticker goroutine started once at server startup and stopped by the
// server context.
//
// The ticker is subscriber-gated: with zero open /events connections it does
// not even perform the cheap stat-only fingerprint walk (SC12), so an idle
// `atomic serve` with no browser tab open costs nothing beyond the ticker's
// own timer wakeups.
package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	// changedCap bounds the manifest diff carried in a changeEvent. Above the
	// cap the field is omitted entirely rather than truncated, and clients
	// treat an omitted field as "everything may have changed" (SC16) — past
	// this size the enumeration itself stops being useful.
	changedCap = 100

	// sseWriteDeadline bounds each write to a subscriber's connection so a
	// stalled TCP peer cannot hang that subscriber's goroutine forever. It is
	// independent of shutdown responsiveness — that is the request context's
	// job (serve.go wires the server's BaseContext to the same context that
	// drives srv.Shutdown) — this deadline is purely about a dead peer that
	// never disconnects.
	sseWriteDeadline = 5 * time.Second
)

// changeEvent is the /events wire payload: the realm fingerprint and the
// manifest diff since the subscriber's last-seen state. Changed is nil (not
// present in the JSON, via omitempty) once it exceeds changedCap — the client
// treats a missing Changed the same as an oversized one: refetch everything.
type changeEvent struct {
	FP      string   `json:"fp"`
	Changed []string `json:"changed,omitempty"`
}

// newChangeEvent builds the wire payload for a fingerprint and its manifest
// diff, applying changedCap.
func newChangeEvent(fp string, changed []string) changeEvent {
	if len(changed) > changedCap {
		return changeEvent{FP: fp}
	}
	return changeEvent{FP: fp, Changed: changed}
}

// subscriber is one connected /events client's coalescing slot: a buffered-1
// channel that push overwrites rather than blocks on when the subscriber
// hasn't drained the previous event.
type subscriber struct {
	ch chan changeEvent
}

// push delivers ev to the subscriber's slot without ever blocking: a full
// slot (the subscriber hasn't read its previous event yet) is drained and
// replaced, so the subscriber always sees the latest state once it catches
// up, and this call — and therefore broadcast's caller, the ticker — never
// stalls on a slow or dead subscriber (SC14).
func (s *subscriber) push(ev changeEvent) {
	select {
	case s.ch <- ev:
		return
	default:
	}
	select {
	case <-s.ch:
	default:
	}
	select {
	case s.ch <- ev:
	default:
	}
}

// subscriberRegistry tracks every connected /events client. The ticker reads
// count() to decide whether to do any work at all (SC12); NewEventsHandler
// registers and unregisters through subscribe's returned unsubscribe func.
type subscriberRegistry struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

// newSubscriberRegistry constructs an empty registry.
func newSubscriberRegistry() *subscriberRegistry {
	return &subscriberRegistry{subs: make(map[*subscriber]struct{})}
}

// subscribe registers a new subscriber and returns its slot plus an
// unsubscribe func the caller must invoke exactly once when the connection
// ends.
func (r *subscriberRegistry) subscribe() (*subscriber, func()) {
	s := &subscriber{ch: make(chan changeEvent, 1)}
	r.mu.Lock()
	r.subs[s] = struct{}{}
	r.mu.Unlock()
	return s, func() {
		r.mu.Lock()
		delete(r.subs, s)
		r.mu.Unlock()
	}
}

// broadcast pushes ev to every subscriber's slot, coalescing to the latest.
func (r *subscriberRegistry) broadcast(ev changeEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for s := range r.subs {
		s.push(ev)
	}
}

// count returns the current subscriber count — the ticker's gate (SC12).
func (r *subscriberRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subs)
}

// NewEventsHandler returns the GET /events SSE handler (Subscribe flow):
// register a subscriber, immediately push a resync event reflecting current
// on-disk state regardless of the tick cycle, then stream subsequent
// broadcast events from the subscriber's slot until the request context ends
// (client disconnect or server shutdown).
func NewEventsHandler(store *snapshotStore, registry *subscriberRegistry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		sub, unsubscribe := registry.subscribe()
		defer unsubscribe()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // defeat any intermediary buffering
		w.WriteHeader(http.StatusOK)

		snap, changed := store.ensureFresh()
		if !writeChangeEvent(w, flusher, newChangeEvent(snap.fp, changed)) {
			return
		}

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-sub.ch:
				if !writeChangeEvent(w, flusher, ev) {
					return
				}
			}
		}
	})
}

// writeChangeEvent JSON-encodes ev as one SSE message event (the client's
// plain EventSource.onmessage — no custom event name), applying a bounded
// write deadline so a stalled peer cannot hang this goroutine indefinitely.
// Returns false when the caller should stop streaming.
func writeChangeEvent(w http.ResponseWriter, flusher http.Flusher, ev changeEvent) bool {
	payload, err := json.Marshal(ev)
	if err != nil {
		return false
	}
	// Best-effort: not every ResponseWriter supports a write deadline (e.g. a
	// unit test's httptest.ResponseRecorder); proceed without one rather than
	// fail the write over it.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(sseWriteDeadline))
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// startTicker starts the single ticker goroutine for the server's lifetime
// (SC11): gated on subscriber count so a zero-subscriber tick performs no
// walk at all (SC12), it otherwise calls store.ensureFresh() and — only when
// that call actually rebuilt (changed != nil; a nil result means either the
// fingerprint was unchanged or a rebuild was already in flight) — broadcasts
// {fp, changed} to every subscriber. The goroutine exits when ctx is
// cancelled; callers must pass the same context that drives the server's
// graceful shutdown so the ticker never outlives it.
func startTicker(ctx context.Context, store *snapshotStore, registry *subscriberRegistry, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if registry.count() == 0 {
					continue
				}
				snap, changed := store.ensureFresh()
				if changed == nil {
					continue
				}
				registry.broadcast(newChangeEvent(snap.fp, changed))
			}
		}
	}()
}
