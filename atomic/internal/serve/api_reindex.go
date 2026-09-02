package serve

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// runID lets a browser tab tell a layout warmed by this server from one warmed
// by a previous run, which may have served different content. The timestamp is
// there because a reboot recycles PIDs.
var runID = fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())

// startedAt is reported as an elapsed duration, so the reader never has to
// reconcile the server's clock and timezone with their own.
var startedAt = time.Now()

// RunID returns this process's run identity.
func RunID() string { return runID }

// Uptime reports how long this process has been serving.
func Uptime() time.Duration { return time.Since(startedAt) }

// reindexState is the lifecycle of one member's index rebuild.
type reindexState string

const (
	reindexIdle    reindexState = "idle"
	reindexRunning reindexState = "running"
	reindexDone    reindexState = "done"
	reindexFailed  reindexState = "failed"
)

type reindexJob struct {
	State     reindexState `json:"state"`
	StartedAt time.Time    `json:"startedAt,omitempty"`
	EndedAt   time.Time    `json:"endedAt,omitempty"`
	Error     string       `json:"error,omitempty"`
}

// reindexRegistry tracks one job per member key. Indexing writes to SQLite, so
// a second concurrent run over the same member would only contend with the
// first; this is what makes a repeated click a no-op instead of a pile-up.
type reindexRegistry struct {
	mu   sync.Mutex
	jobs map[string]*reindexJob
}

func newReindexRegistry() *reindexRegistry {
	return &reindexRegistry{jobs: map[string]*reindexJob{}}
}

func (r *reindexRegistry) get(key string) reindexJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job, ok := r.jobs[key]; ok {
		return *job
	}
	return reindexJob{State: reindexIdle}
}

// begin returns false when a run is already in flight, and the caller must not
// start another.
func (r *reindexRegistry) begin(key string) (started bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job, ok := r.jobs[key]; ok && job.State == reindexRunning {
		return false
	}
	r.jobs[key] = &reindexJob{State: reindexRunning, StartedAt: time.Now()}
	return true
}

func (r *reindexRegistry) finish(key string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[key]
	if !ok {
		return
	}
	job.EndedAt = time.Now()
	if err != nil {
		job.State = reindexFailed
		job.Error = err.Error()
		return
	}
	job.State = reindexDone
}

type reindexHandler struct {
	memberResolver
	provider EngineProvider
	registry *reindexRegistry
}

// NewAPIReindexHandler serves GET for status and POST to start on
// /api/code/index. It shares memberResolver with the code explorer, so a
// rebuild targets the member the rest of the UI is showing.
//
// Refused off-loopback: rebuilding an index is real work on the serving
// machine's disk and must not be reachable from the LAN when --host widens
// the bind.
func NewAPIReindexHandler(opts CodeExplorerOptions) http.Handler {
	prov := opts.EngineProvider
	if prov == nil {
		prov = DefaultEngineProvider()
	}
	return &reindexHandler{
		memberResolver: memberResolver{
			realmRoot:     opts.RealmRoot,
			claudeMDPath:  opts.ClaudeMDPath,
			wikiIndexPath: opts.WikiIndexPath,
		},
		provider: prov,
		registry: newReindexRegistry(),
	}
}

func (h *reindexHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackPeer(r.RemoteAddr) {
		writeAPIError(w, http.StatusForbidden,
			"reindex is loopback-only; run it from the serving machine")
		return
	}

	member := r.URL.Query().Get("member")

	if r.Method == http.MethodGet {
		writeAPIJSON(w, h.registry.get(member))
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "use GET for status or POST to start")
		return
	}
	if rejectCrossOrigin(w, r) {
		return
	}

	projectRoot, dbPath := h.realmRoot, h.localDBPath()
	if m, ok := memberByPrefix(h.members(), member); ok {
		projectRoot, dbPath = m.Path, m.DBPath
	}

	if !h.registry.begin(member) {
		// Report the live job, so a double-click reads as "still going".
		writeAPIJSON(w, h.registry.get(member))
		return
	}

	// Detached: indexing outlives any sane HTTP timeout, so the client polls
	// GET rather than holding the connection.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		eng, openErr := h.provider(ctx, projectRoot, dbPath)
		if openErr != nil {
			h.registry.finish(member, openErr)
			return
		}
		defer eng.Close()
		h.registry.finish(member, eng.IndexAll(ctx))
	}()

	writeAPIJSON(w, h.registry.get(member))
}
