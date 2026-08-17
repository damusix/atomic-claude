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

const goSource = `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`

// Sharing one tsInstance across goroutines is a data race; every borrow must
// hand out an independent instance. CI also runs this under -race.
func TestPool_RaceClean(t *testing.T) {
	ctx := context.Background()

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{
		// Undersized on purpose: goroutines must queue, not leak instances.
		Size: 2,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	const goroutines = 8
	const parsesPerGoroutine = 20

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

// wazero's linear memory only grows, so instances must be recycled on a cadence.
// The counter is the observable seam; the alternative is RSS sampling, which is
// OS-dependent and slow.
func TestPool_RecycleCadence(t *testing.T) {
	ctx := context.Background()

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{
		// Size 1 serializes parses through one slot, so the count is deterministic.
		Size: 1,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

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

	got := pool.RecycleCount()
	want := target / extraction.RecycleInterval
	if got < want {
		t.Errorf("RecycleCount: got %d, want >= %d (cadence not firing at interval %d)",
			got, want, extraction.RecycleInterval)
	}
}

// The binding sits behind one Go interface so swapping wazero for cgo stays a
// build-tag flip. A full parse-and-walk must therefore be drivable from the
// extraction package alone — this file must never need to import sitter.
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

	var _ extraction.Instance = inst // compile-time: a failure here means the iface broke

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

// A NamedIterator with the wrong child-count function or indexing hands the
// visitor the wrong nodes, and the extractor emits garbage symbols.
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

	// tree-sitter-go names the package name node package_identifier, not the
	// generic identifier — the expectations below are grammar-specific.
	if len(kinds) < 3 {
		t.Fatalf("WalkNamed: got %d named nodes, want >= 3; kinds: %v", len(kinds), kinds)
	}
	if kinds[0] != "source_file" {
		t.Errorf("kinds[0]: got %q, want %q", kinds[0], "source_file")
	}
	if kinds[1] != "package_clause" {
		t.Errorf("kinds[1]: got %q, want %q", kinds[1], "package_clause")
	}
	if kinds[2] != "package_identifier" {
		t.Errorf("kinds[2]: got %q, want %q", kinds[2], "package_identifier")
	}
}

// A visitor error is the only way an extractor can signal early exit ("stop at
// the first symbol of kind X"), so it must halt the walk and propagate.
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

	walkErr := extraction.WalkNamed(ctx, inst, goSource, func(_ extraction.NodeInfo) error {
		visited++
		if visited == 1 {
			return sentinel
		}
		return nil
	})

	if !errors.Is(walkErr, sentinel) {
		t.Errorf("WalkNamed: got %v, want sentinel error %v", walkErr, sentinel)
	}
	if visited != 1 {
		t.Errorf("WalkNamed: visited %d nodes after error, want exactly 1 (walk must stop immediately)", visited)
	}
}

// Close must drain every slot. A size-bounded loop with a default:return arm
// exits on the first empty receive even while items are still queued, leaking
// instances; the for-len(ch) drain closes exactly what is there.
func TestPool_CloseAll(t *testing.T) {
	ctx := context.Background()

	const size = 3
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: size})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	inst, err := pool.Borrow(ctx)
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	pool.Return(inst) // back in pool, all size instances now available

	pool.Close()

	if n := pool.ChannelLen(); n != 0 {
		t.Errorf("after Close, pool channel has %d items, want 0 (not all instances drained)", n)
	}
}

// A blocked Borrow must surface ctx cancellation as an error, not a panic —
// otherwise the pool cannot be used under a deadline or graceful shutdown.
func TestBorrow_ContextCancel(t *testing.T) {
	ctx := context.Background()

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

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	inst, borrowErr := pool.Borrow(cancelled)
	if borrowErr == nil {
		pool.Return(inst)
		t.Fatal("Borrow with cancelled ctx: got nil error, want ctx error")
	}
	if !errors.Is(borrowErr, context.Canceled) {
		t.Errorf("Borrow with cancelled ctx: got %v, want to wrap context.Canceled", borrowErr)
	}
}

// Structural proof via Instance.ID(): if two goroutines can hold one instance
// at the same moment the pool is broken, whatever -race reports about the
// current access pattern.
func TestPool_NoSharing(t *testing.T) {
	ctx := context.Background()
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	const goroutines = 10
	const iters = 50

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

// Soft memory gate: RSS is OS-dependent, but MemStats heap is deterministic
// enough to show recycling bounds growth.
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

	// Without recycling wazero's linear memory passed 1 GB here.
	if growthMB > 500 {
		b.Errorf("heap grew %.1f MB over %d parses — recycle may not be working", growthMB, total)
	}
}
