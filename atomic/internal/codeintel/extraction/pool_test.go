package extraction_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// goSource is a minimal Go source file used as parse input across tests.
const goSource = `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`

// ---------------------------------------------------------------------------
// TestPool_RaceClean: M goroutines concurrently borrow+parse — must be race-
// clean and produce consistent parse trees. The test is also run with
// "-race" by the CI gate ("go test -race ./internal/codeintel/...").
//
// WHY: sharing one tsInstance across goroutines is a proven data race (spike
// A2). Each borrow must hand out an independent instance. A race detector hit
// here means the pool is sharing state it must not share.
// ---------------------------------------------------------------------------

func TestPool_RaceClean(t *testing.T) {
	ctx := context.Background()

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{
		// Small pool to force contention — goroutines must queue, not leak
		// instances.
		Size: 2,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	const goroutines = 8
	const parsesPerGoroutine = 20

	// We collect node counts; all parses of the same input must agree.
	type result struct {
		nodes int
		err   error
	}
	results := make([]result, goroutines*parsesPerGoroutine)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < parsesPerGoroutine; i++ {
				idx := base*parsesPerGoroutine + i
				inst, err := pool.Borrow(ctx)
				if err != nil {
					results[idx] = result{err: err}
					continue
				}
				var count int
				walkErr := extraction.WalkNamed(ctx, inst, goSource, func(_ extraction.NodeInfo) error {
					count++
					return nil
				})
				pool.Return(inst)
				results[idx] = result{nodes: count, err: walkErr}
			}
		}(g)
	}
	wg.Wait()

	// All successful parses must agree on node count.
	var reference int
	for i, r := range results {
		if r.err != nil {
			t.Errorf("result[%d]: unexpected error: %v", i, r.err)
			continue
		}
		if reference == 0 {
			reference = r.nodes
		} else if r.nodes != reference {
			t.Errorf("result[%d]: node count %d, want %d (non-deterministic parse or shared state)",
				i, r.nodes, reference)
		}
	}
}

// ---------------------------------------------------------------------------
// TestPool_RecycleCadence: drive >recycleInterval parses through a single
// pooled instance and assert the recycle counter increments at the right
// cadence.
//
// WHY: wazero's grow-only linear memory is unbounded without recycling (spike
// A3: unbounded growth vs ~1 GB flat with recycle@500). The counter is the
// observable seam for deterministic testing — without it, we'd need RSS
// sampling which is OS-dependent and slow.
// ---------------------------------------------------------------------------

func TestPool_RecycleCadence(t *testing.T) {
	ctx := context.Background()

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{
		// Pool of 1 so parses are serialized through the same slot; makes
		// recycle counting deterministic (no concurrency randomizing order).
		Size: 1,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	// Parse > recycleInterval times; each parse borrows and returns the same slot.
	target := extraction.RecycleInterval*2 + 50 // two full cycles + a bit
	for i := 0; i < target; i++ {
		inst, err := pool.Borrow(ctx)
		if err != nil {
			t.Fatalf("Borrow %d: %v", i, err)
		}
		_, err = inst.ParseString(ctx, goSource)
		pool.Return(inst)
		if err != nil {
			t.Fatalf("parse %d: %v", i, err)
		}
	}

	// Must have recycled at least twice (floor(target/recycleInterval) >= 2).
	got := pool.RecycleCount()
	want := target / extraction.RecycleInterval
	if got < want {
		t.Errorf("RecycleCount: got %d, want >= %d (cadence not firing at interval %d)",
			got, want, extraction.RecycleInterval)
	}
}

// ---------------------------------------------------------------------------
// TestPool_BindingInterface: Borrow returns an extraction.Instance (the
// interface), NOT *sitter.TreeSitter. Extractors must be able to drive a
// parse-and-walk cycle via the extraction package alone — no tsbinding import
// needed.
//
// WHY: the brief requires the binding to sit behind one Go interface so an
// A→C swap (wazero → cgo) is a build-tag flip. If Tree.RootNode leaks
// sitter.Node, callers are coupled to the binding. WalkNamed is the public
// traversal API; this test proves it works without importing sitter.
// ---------------------------------------------------------------------------

func TestPool_BindingInterface(t *testing.T) {
	ctx := context.Background()
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	inst, err := pool.Borrow(ctx)
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	defer pool.Return(inst)

	// Compile-time: inst must implement extraction.Instance.
	var _ extraction.Instance = inst // if this fails to compile the iface is broken

	// Runtime: full parse-and-walk via the extraction package alone — no
	// sitter import required by this test.
	var kinds []string
	walkErr := extraction.WalkNamed(ctx, inst, goSource, func(n extraction.NodeInfo) error {
		kinds = append(kinds, n.Kind)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("WalkNamed: %v", walkErr)
	}
	if len(kinds) == 0 {
		t.Error("WalkNamed: no named nodes visited")
	}
	if kinds[0] != "source_file" {
		t.Errorf("kinds[0]: got %q, want %q", kinds[0], "source_file")
	}
}

// ---------------------------------------------------------------------------
// TestWalkNamed_Order: walk a known snippet and assert named nodes are visited
// in the expected DFS pre-order.
//
// WHY: correctness test for the low-round-trip named traversal. If the
// NamedIterator is broken (wrong child-count function, wrong indexing), the
// visitor sees wrong nodes and the extractor produces garbage symbols.
// ---------------------------------------------------------------------------

func TestWalkNamed_Order(t *testing.T) {
	ctx := context.Background()

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	inst, err := pool.Borrow(ctx)
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	defer pool.Return(inst)

	// Set language to Go so we know the grammar.
	if err := inst.SetLanguage(ctx, extraction.LangGo); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}

	const src = "package main\n"
	var kinds []string
	err = extraction.WalkNamed(ctx, inst, src, func(n extraction.NodeInfo) error {
		kinds = append(kinds, n.Kind)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkNamed: %v", err)
	}

	// For "package main\n" the Go grammar (tree-sitter-go) produces:
	//   source_file
	//     package_clause
	//       package_identifier ("main")   ← Go grammar uses package_identifier,
	//                                       not the generic identifier node type
	// Named DFS pre-order: source_file, package_clause, package_identifier
	if len(kinds) < 3 {
		t.Fatalf("WalkNamed: got %d named nodes, want >= 3; kinds: %v", len(kinds), kinds)
	}
	if kinds[0] != "source_file" {
		t.Errorf("kinds[0]: got %q, want %q", kinds[0], "source_file")
	}
	if kinds[1] != "package_clause" {
		t.Errorf("kinds[1]: got %q, want %q", kinds[1], "package_clause")
	}
	// kinds[2] is the package name — tree-sitter-go uses "package_identifier"
	if kinds[2] != "package_identifier" {
		t.Errorf("kinds[2]: got %q, want %q", kinds[2], "package_identifier")
	}
}

// ---------------------------------------------------------------------------
// TestWalkNamed_ErrorStop: returning a non-nil error from the visit fn must
// halt the walk immediately and propagate that error.
//
// WHY: extractors use WalkNamed with early-exit patterns (e.g. "stop after
// finding the first symbol of kind X"). Without error propagation the walk
// continues needlessly, and callers have no reliable way to signal stop.
// ---------------------------------------------------------------------------

func TestWalkNamed_ErrorStop(t *testing.T) {
	ctx := context.Background()

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	inst, err := pool.Borrow(ctx)
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	defer pool.Return(inst)

	sentinel := errors.New("stop-after-first")
	var visited int

	// goSource has many named nodes; we stop after the first one.
	walkErr := extraction.WalkNamed(ctx, inst, goSource, func(_ extraction.NodeInfo) error {
		visited++
		if visited == 1 {
			return sentinel
		}
		return nil
	})

	// WalkNamed must return the sentinel error.
	if !errors.Is(walkErr, sentinel) {
		t.Errorf("WalkNamed: got %v, want sentinel error %v", walkErr, sentinel)
	}
	// Walk must have stopped at the first node — visited must be exactly 1.
	if visited != 1 {
		t.Errorf("WalkNamed: visited %d nodes after error, want exactly 1 (walk must stop immediately)", visited)
	}
}

// ---------------------------------------------------------------------------
// TestPool_CloseAll: Close must drain every available instance in the pool.
// With current logic a buffered channel that drains fully is fine, but with
// the for-len(ch) pattern we prove no early-exit occurs.
//
// WHY: the previous for-i-0-to-size loop with default:return would exit on
// the first empty receive even if the channel had items queued (e.g. after
// a borrowed instance returns between iterations). The for-len drain is
// deterministic: it closes exactly len(ch) items, no early-exit.
// ---------------------------------------------------------------------------

func TestPool_CloseAll(t *testing.T) {
	ctx := context.Background()

	const size = 3
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: size})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	// Borrow one instance (simulates a caller that borrowed it but returned
	// it before Close; we return it here to make all available again).
	inst, err := pool.Borrow(ctx)
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	pool.Return(inst) // back in pool, all size instances now available

	// Close must not panic and must drain the pool.
	// The main observable: pool.ChannelLen() == 0 after close.
	pool.Close()

	// After Close, the pool channel should be empty (all instances drained).
	if n := pool.ChannelLen(); n != 0 {
		t.Errorf("after Close, pool channel has %d items, want 0 (not all instances drained)", n)
	}
}

// ---------------------------------------------------------------------------
// TestBorrow_ContextCancel: Borrow must return an error (not panic) when the
// context is already cancelled and no instance is available.
//
// WHY: panicking on ctx cancel makes the pool impossible to use safely in
// contexts with deadlines/cancellation. Returning an error lets callers
// handle graceful shutdown.
// ---------------------------------------------------------------------------

func TestBorrow_ContextCancel(t *testing.T) {
	ctx := context.Background()

	// Pool of 1 with the one instance already borrowed — Borrow will block.
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	// Hold the only instance so the next Borrow must wait.
	held, err := pool.Borrow(ctx)
	if err != nil {
		t.Fatalf("initial Borrow: %v", err)
	}
	defer pool.Return(held)

	// Cancelled context: Borrow must return a wrapped error, not panic.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	inst, borrowErr := pool.Borrow(cancelled)
	if borrowErr == nil {
		pool.Return(inst) // clean up if it somehow succeeded
		t.Fatal("Borrow with cancelled ctx: got nil error, want ctx error")
	}
	if !errors.Is(borrowErr, context.Canceled) {
		t.Errorf("Borrow with cancelled ctx: got %v, want to wrap context.Canceled", borrowErr)
	}
}

// ---------------------------------------------------------------------------
// TestPool_NoSharing: prove that two goroutines never hold the same instance
// simultaneously. This is a structural proof via Instance.ID().
//
// WHY: if two goroutines can hold the same instance ID at the same moment,
// the pool is broken regardless of what the race detector says about the
// current access pattern.
// ---------------------------------------------------------------------------

func TestPool_NoSharing(t *testing.T) {
	ctx := context.Background()
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	const goroutines = 10
	const iters = 50

	// Track which IDs are currently borrowed. A concurrent collision means
	// the same instance was lent twice.
	var mu sync.Mutex
	inFlight := map[int]int{} // id → goroutine that holds it
	var collisions int64

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				inst, err := pool.Borrow(ctx)
				if err != nil {
					t.Errorf("Borrow: %v", err)
					return
				}
				id := inst.ID()

				mu.Lock()
				if holder, ok := inFlight[id]; ok {
					t.Errorf("instance %d held by goroutine %d AND goroutine %d simultaneously",
						id, holder, gid)
					atomic.AddInt64(&collisions, 1)
				}
				inFlight[id] = gid
				mu.Unlock()

				// Simulate some work
				_, _ = inst.ParseString(ctx, goSource)

				mu.Lock()
				delete(inFlight, id)
				mu.Unlock()

				pool.Return(inst)
			}
		}(g)
	}
	wg.Wait()

	if atomic.LoadInt64(&collisions) > 0 {
		t.Errorf("pool lent the same instance to two goroutines; collisions: %d", collisions)
	}
}

// ---------------------------------------------------------------------------
// BenchmarkPool_HeapBounded: sample Go-heap before and after many parses with
// recycle enabled; assert heap does not grow unboundedly. Skipped under -short.
//
// WHY: soft RSS gate from the brief — shows recycle bounds heap vs unbounded.
// Not a hard gate because RSS is OS-dependent, but heap via MemStats is
// deterministic enough for a benchmark assertion.
// ---------------------------------------------------------------------------

func BenchmarkPool_HeapBounded(b *testing.B) {
	if testing.Short() {
		b.Skip("skipped under -short (heap sampling test)")
	}

	ctx := context.Background()
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 1})
	if err != nil {
		b.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	const total = 2000

	// Baseline heap before any parses.
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := 0; i < total; i++ {
		inst, err := pool.Borrow(ctx)
		if err != nil {
			b.Fatalf("Borrow %d: %v", i, err)
		}
		_, _ = inst.ParseString(ctx, goSource)
		pool.Return(inst)
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	growthMB := float64(int64(after.HeapInuse)-int64(before.HeapInuse)) / 1e6
	b.Logf("heap growth over %d parses with recycle@%d: %.1f MB", total, extraction.RecycleInterval, growthMB)

	// With recycle, heap growth should stay well under 500 MB.
	// Without recycle, wazero linear memory grows unboundedly (spike A3: >1 GB).
	if growthMB > 500 {
		b.Errorf("heap grew %.1f MB over %d parses — recycle may not be working", growthMB, total)
	}
}
