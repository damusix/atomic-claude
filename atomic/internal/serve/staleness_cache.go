package serve

import (
	"strings"
	"sync"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// wiki.Stale walks the whole realm and hashes every bucket manifest —
// measured at ~3s on a five-member realm. Two endpoints wanted it on every
// request: /api/nav (member and bucket badges) and /api/status (the dashboard
// lists). /api/nav also carries the scope identity the header renders and the
// tree the library renders, so that walk was what made a page load open onto a
// nameless top bar and an empty library.
//
// One walk now serves both, and a request never waits on a repeat of it:
//
//   - nothing cached  → compute, and block (the first caller has no answer to
//     give); concurrent callers wait on that one walk rather than each start
//     their own.
//   - cached, fresh   → serve it.
//   - cached, stale   → serve it now and refresh in the background.
//
// Serving a stale answer is a deliberate trade and a small one: the value is a
// set of "this is out of date" badges, and the SSE reconcile path (?live=1)
// already skips the computation entirely, so these badges were never promised
// to be current to the request.
const stalenessTTL = 30 * time.Second

// stalenessCache memoizes one realm's parsed wiki.Stale output.
type stalenessCache struct {
	mu  sync.Mutex
	now func() time.Time
	// walk is the seam tests replace; nil means the real wiki.Stale.
	walk func(realmRoot string) staleSets

	root       string
	at         time.Time
	sets       *staleSets
	refreshing bool
}

func (c *stalenessCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *stalenessCache) walkFn() func(string) staleSets {
	if c.walk != nil {
		return c.walk
	}
	return walkStaleness
}

// get returns the realm's staleness sets, refreshing in the background when
// the cached copy has aged out. Callers get copies: consumers store what they
// are handed, and a shared map would let one request's bookkeeping surface in
// another's badges.
func (c *stalenessCache) get(realmRoot string) staleSets {
	c.mu.Lock()

	if c.sets == nil || c.root != realmRoot {
		// No answer to give — compute while holding the lock so concurrent
		// first callers wait on this walk instead of starting their own.
		sets := c.walkFn()(realmRoot)
		c.root, c.at, c.sets = realmRoot, c.clock(), &sets
		defer c.mu.Unlock()
		return copyStaleSets(sets)
	}

	cached := *c.sets
	stale := c.clock().Sub(c.at) >= stalenessTTL
	if stale && !c.refreshing {
		c.refreshing = true
		go c.refresh(realmRoot)
	}
	c.mu.Unlock()

	return copyStaleSets(cached)
}

func (c *stalenessCache) refresh(realmRoot string) {
	sets := c.walkFn()(realmRoot)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshing = false
	// A realm switch mid-refresh would make this result the wrong answer.
	if c.root != realmRoot && c.sets != nil {
		return
	}
	c.root, c.at, c.sets = realmRoot, c.clock(), &sets
}

// walkStaleness runs wiki.Stale once and parses it. Errors (exit code 2 —
// wiki/ absent, unreadable index) degrade to empty sets rather than an error:
// a staleness-check failure must not take down the nav tree.
func walkStaleness(realmRoot string) staleSets {
	var buf strings.Builder
	code, err := wiki.Stale(realmRoot, &buf)
	if err != nil || code == wiki.StaleCodeError {
		return staleSets{Members: map[string]bool{}, Buckets: map[string]bool{}, Concerns: map[string]bool{}}
	}
	return parseStaleLines(buf.String())
}

func copyStaleSets(src staleSets) staleSets {
	return staleSets{
		Members:  copyBoolSet(src.Members),
		Buckets:  copyBoolSet(src.Buckets),
		Concerns: copyBoolSet(src.Concerns),
	}
}

func copyBoolSet(src map[string]bool) map[string]bool {
	out := make(map[string]bool, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// The process-wide cache /api/nav and /api/status share. An injected seam on
// either handler bypasses it, so tests still see each result they stage.
var navStalenessCache = &stalenessCache{}
