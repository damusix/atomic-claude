package serve

import (
	"sync"
	"testing"
	"time"
)

func sets(member string) staleSets {
	return staleSets{
		Members:  map[string]bool{member: true},
		Buckets:  map[string]bool{"raw": true},
		Concerns: map[string]bool{},
	}
}

func TestStalenessCache_ReusesWithinTTL(t *testing.T) {
	walks := 0
	now := time.Unix(1_700_000_000, 0)
	c := &stalenessCache{
		now: func() time.Time { return now },
		walk: func(string) staleSets {
			walks++
			return sets("alpha")
		},
	}

	got := c.get("/realm")
	if !got.Members["alpha"] || !got.Buckets["raw"] {
		t.Fatalf("first call lost the payload: %+v", got)
	}

	now = now.Add(stalenessTTL - time.Second)
	c.get("/realm")
	if walks != 1 {
		t.Fatalf("re-walked inside the TTL: %d walks", walks)
	}
}

// Past the TTL the request is served from cache and the walk happens behind
// it — the whole point is that no request ever waits on a repeat walk.
func TestStalenessCache_StaleEntryServesImmediatelyAndRefreshes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	release := make(chan struct{})
	var walks int
	var mu sync.Mutex

	c := &stalenessCache{
		now: func() time.Time { return now },
		walk: func(string) staleSets {
			mu.Lock()
			walks++
			n := walks
			mu.Unlock()
			if n > 1 {
				<-release // hold the background refresh open
				return sets("beta")
			}
			return sets("alpha")
		},
	}

	c.get("/realm")
	now = now.Add(stalenessTTL + time.Second)

	// Returns while the refresh is still blocked — proof it did not wait.
	got := c.get("/realm")
	if !got.Members["alpha"] {
		t.Fatalf("stale read did not serve the cached value: %+v", got)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.get("/realm").Members["beta"] {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("background refresh never landed")
}

// Serving a different realm must not answer from the previous realm's badges.
func TestStalenessCache_RecomputesForAnotherRealm(t *testing.T) {
	walks := 0
	c := &stalenessCache{
		walk: func(root string) staleSets {
			walks++
			return sets(root)
		},
	}

	c.get("/one")
	got := c.get("/two")
	if walks != 2 {
		t.Fatalf("reused another realm's result: %d walks", walks)
	}
	if !got.Members["/two"] {
		t.Fatalf("returned the wrong realm's members: %+v", got)
	}
}

// Consumers store the maps they are handed; sharing the cached instance would
// let one request's bookkeeping show up in the next one's badges.
func TestStalenessCache_CallerCannotMutateCachedResult(t *testing.T) {
	c := &stalenessCache{walk: func(string) staleSets { return sets("alpha") }}

	got := c.get("/realm")
	got.Members["injected"] = true
	got.Buckets["injected"] = true

	again := c.get("/realm")
	if again.Members["injected"] || again.Buckets["injected"] {
		t.Fatalf("mutation leaked into the cache: %+v", again)
	}
}

// Both endpoints read through this cache, and they fire together on every page
// load; the second must not pay for a walk the first already did.
func TestStalenessCache_ConcurrentFirstCallersShareOneWalk(t *testing.T) {
	var mu sync.Mutex
	walks := 0
	c := &stalenessCache{
		walk: func(string) staleSets {
			mu.Lock()
			walks++
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			return sets("alpha")
		},
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !c.get("/realm").Members["alpha"] {
				t.Error("a concurrent caller got an empty result")
			}
		}()
	}
	wg.Wait()

	if walks != 1 {
		t.Fatalf("concurrent first callers each walked: %d walks", walks)
	}
}
