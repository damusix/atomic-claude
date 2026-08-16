package serve

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// ─── run identity ───────────────────────────────────────────────────────────

// runID identifies this server process. Browser tabs use it to tell "the
// layout was warmed by the server I am talking to now" from "warmed by a
// previous run" — a restart may be serving different content, so a warm from
// an older run cannot be trusted to still match.
//
// PID alone would collide after a reboot recycles it; the start timestamp
// makes it unique in practice without needing randomness.
var runID = fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())

// startedAt is when this process began serving. Reported as an elapsed
// duration rather than a wall-clock instant so the reader does not have to
// reconcile the server's clock and timezone with their own.
var startedAt = time.Now()

// RunID returns this process's run identity.
func RunID() string { return runID }

// Uptime reports how long this process has been serving.
func Uptime() time.Duration { return time.Since(startedAt) }

// ─── reindex jobs ───────────────────────────────────────────────────────────

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

// reindexRegistry tracks one job per member key. Indexing is expensive and
// writes to a SQLite file, so a second concurrent run over the same member
// would contend with the first for no benefit — the registry is what makes a
// repeated click a no-op rather than a pile-up.
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

// begin marks key as running. started is false when a run is already in
// flight, and the caller must not start another.
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

// ─── handler ────────────────────────────────────────────────────────────────

type reindexHandler struct {
	memberResolver
	provider EngineProvider
	registry *reindexRegistry
}

// NewAPIReindexHandler serves GET (status) and POST (start) on
// /api/code/index. Member resolution matches the code explorer's exactly —
// the same memberResolver — so the member a rebuild targets is the one the
// rest of the UI is showing.
//
// This is the second write surface on an otherwise read-only server, and like
// the first (bus chat) it is refused off-loopback: rebuilding an index is
// real work on the serving machine's disk and must never be reachable from
// the LAN when --host widens the bind.
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

	projectRoot, dbPath := h.realmRoot, h.localDBPath()
	if m, ok := memberByPrefix(h.members(), member); ok {
		projectRoot, dbPath = m.Path, m.DBPath
	}

	if !h.registry.begin(member) {
		// Already running: report the live job rather than erroring, so a
		// double-click reads as "still going", not as a failure.
		writeAPIJSON(w, h.registry.get(member))
		return
	}

	// Detached from the request: indexing outlives any sane HTTP timeout, so
	// the client polls GET for completion instead of holding the connection.
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
