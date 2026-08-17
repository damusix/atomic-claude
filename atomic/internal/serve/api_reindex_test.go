package serve_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// blockingIndexEngine holds IndexAll open until released, so a test can
// observe the running state rather than racing a job that finishes instantly.
type blockingIndexEngine struct {
	*fakeCodeEngine
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (b *blockingIndexEngine) IndexAll(context.Context) error {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	<-b.release
	return nil
}

func (b *blockingIndexEngine) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func reindexHandlerFor(t *testing.T, eng serve.CodeEngine) http.Handler {
	t.Helper()
	return serve.NewAPIReindexHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(eng),
	})
}

func post(t *testing.T, h http.Handler, remote string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/code/index?member=", nil)
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func statusOf(t *testing.T, h http.Handler) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/code/index?member=", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var body struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode status: %v (%s)", err, rec.Body.String())
	}
	return body.State
}

// Reindexing writes to the serving machine's disk. It is refused off-loopback
// for the same reason bus chat is: --host widens the bind, and neither
// capability should travel with it.
func TestReindex_RefusedOffLoopback(t *testing.T) {
	h := reindexHandlerFor(t, &fakeCodeEngine{})

	rec := post(t, h, "10.0.0.7:5000")

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a non-loopback peer", rec.Code)
	}
}

func TestReindex_StartsAndReportsCompletion(t *testing.T) {
	eng := &blockingIndexEngine{fakeCodeEngine: &fakeCodeEngine{}, release: make(chan struct{})}
	h := reindexHandlerFor(t, eng)

	if got := statusOf(t, h); got != "idle" {
		t.Errorf("initial state = %q, want idle", got)
	}

	if rec := post(t, h, "127.0.0.1:5000"); rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", rec.Code)
	}

	waitFor(t, func() bool { return statusOf(t, h) == "running" }, "job to report running")

	close(eng.release)
	waitFor(t, func() bool { return statusOf(t, h) == "done" }, "job to report done")
}

// A second click while a rebuild is in flight must not start another: two
// concurrent indexers would contend for the same SQLite file to no purpose.
func TestReindex_SecondRequestDoesNotStartAConcurrentRun(t *testing.T) {
	eng := &blockingIndexEngine{fakeCodeEngine: &fakeCodeEngine{}, release: make(chan struct{})}
	h := reindexHandlerFor(t, eng)

	post(t, h, "127.0.0.1:5000")
	waitFor(t, func() bool { return statusOf(t, h) == "running" }, "first job to start")

	rec := post(t, h, "127.0.0.1:5000")
	if rec.Code != http.StatusOK {
		t.Errorf("second POST status = %d, want 200 reporting the live job", rec.Code)
	}

	close(eng.release)
	waitFor(t, func() bool { return statusOf(t, h) == "done" }, "job to finish")

	if got := eng.callCount(); got != 1 {
		t.Errorf("IndexAll ran %d times, want exactly 1", got)
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
