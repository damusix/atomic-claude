// Live-reload's push side: /events streams {fp, changed} over EventSource,
// fed by one ticker goroutine bound to the server's context. The snapshot
// store validates lazily, which alone would leave an open tab stale.
//
// The ticker is subscriber-gated, so an idle server with no tab open does not
// even run the stat-only fingerprint walk.
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
	// changedCap is where enumerating the diff stops being useful. Past it the
	// field is omitted rather than truncated, which the client reads as
	// "refetch everything".
	changedCap = 100

	// sseWriteDeadline stops a dead peer that never disconnects from hanging
	// its subscriber goroutine. Shutdown responsiveness is the request
	// context's job, not this deadline's.
	sseWriteDeadline = 5 * time.Second
)

// changeEvent is the /events payload. An absent Changed means the diff
// exceeded changedCap.
type changeEvent struct {
	FP      string   `json:"fp"`
	Changed []string `json:"changed,omitempty"`
}

func newChangeEvent(fp string, changed []string) changeEvent {
	if len(changed) > changedCap {
		return changeEvent{FP: fp}
	}
	return changeEvent{FP: fp, Changed: changed}
}

// subscriber is one client's coalescing slot: a buffered-1 channel that push
// overwrites rather than blocks on.
type subscriber struct {
	ch chan changeEvent
}

// push never blocks. A full slot is drained and replaced, so the subscriber
// sees the latest state once it catches up and the ticker never stalls on a
// slow or dead client.
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

// subscriberRegistry tracks every connected /events client.
type subscriberRegistry struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

func newSubscriberRegistry() *subscriberRegistry {
	return &subscriberRegistry{subs: make(map[*subscriber]struct{})}
}

// subscribe returns a slot and an unsubscribe func the caller must invoke
// exactly once when the connection ends.
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

// broadcast pushes ev to every subscriber, coalescing to the latest.
func (r *subscriberRegistry) broadcast(ev changeEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for s := range r.subs {
		s.push(ev)
	}
}

// count is the ticker's gate.
func (r *subscriberRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subs)
}

// NewEventsHandler serves GET /events. It pushes a resync event reflecting
// current on-disk state immediately, off the tick cycle, then streams
// broadcasts until the request context ends.
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

// writeChangeEvent writes ev as one unnamed SSE message. It returns false
// when the caller should stop streaming.
func writeChangeEvent(w http.ResponseWriter, flusher http.Flusher, ev changeEvent) bool {
	payload, err := json.Marshal(ev)
	if err != nil {
		return false
	}
	// Best-effort: an httptest recorder supports no write deadline, and that
	// is no reason to fail the write.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(sseWriteDeadline))
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// startTicker runs the server's single live-reload ticker. A zero-subscriber
// tick does no walk; a nil changed means the fingerprint held or a rebuild was
// already in flight, so there is nothing to announce. Pass the context that
// drives graceful shutdown — the goroutine must not outlive the server.
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
