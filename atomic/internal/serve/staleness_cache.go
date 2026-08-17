package serve

import (
	"strings"
	"sync"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// wiki.Stale walks the realm and hashes every bucket manifest — seconds on a
// five-member realm — and both /api/nav and /api/status want it per request.
// One walk serves both, and only the very first caller ever blocks on it: a
// cached-but-aged answer is served now and refreshed in the background.
//
// Serving a stale answer is deliberate. The value is a set of "out of date"
// badges, and the live-reload path skips the computation entirely, so these
// were never promised current to the request.
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

// get hands back a copy: consumers keep what they are given, and a shared map
// would let one request's bookkeeping surface in another's badges.
func (c *stalenessCache) get(realmRoot string) staleSets {
	c.mu.Lock()

	if c.sets == nil || c.root != realmRoot {
		// Computed under the lock, so concurrent first callers wait on this
		// walk rather than each starting their own.
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

// walkStaleness degrades to empty sets on error: a staleness-check failure
// must not take down the nav tree.
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

// Shared by /api/nav and /api/status. An injected seam on either handler
// bypasses it, so tests still see each result they stage.
var navStalenessCache = &stalenessCache{}
