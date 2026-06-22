package extraction

// Internal test (package extraction) for lazy pool instantiation.
//
// WHY this exists: NewPool used to boot one wazero runtime per CPU eagerly —
// each runtime compiles a tree-sitter grammar, measured at ~0.5 s and totaling
// ~4.7 s of CPU / ~1.9 GB RSS for a default-size pool. A no-op `atomic code
// sync` (every file unchanged, zero parses) and a small incremental sync (a
// handful of changed files) both paid that full up-front cost even though they
// borrow few or zero instances. Instances must be created on first Borrow, so
// a pool that is never borrowed from boots no runtime at all.
//
// The instance count is observable only via the package-internal channel, so
// this lives in package extraction. It pins a resource-lifecycle guarantee.

import (
	"context"
	"testing"
)

func TestNewPoolDefersInstantiationUntilBorrow(t *testing.T) {
	ctx := context.Background()
	const size = 4

	p, err := NewPool(ctx, PoolOptions{Size: size})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer p.Close()

	if got := p.ChannelLen(); got != size {
		t.Fatalf("ChannelLen after NewPool = %d, want %d (one permit per slot)", got, size)
	}

	// Every slot must be an unbooted permit (nil) — no wazero runtime yet.
	booted := 0
	tokens := make([]*tsInstance, 0, size)
	for i := 0; i < size; i++ {
		tok := <-p.ch
		if tok != nil {
			booted++
		}
		tokens = append(tokens, tok)
	}
	for _, tok := range tokens {
		p.ch <- tok // restore the channel exactly as it was
	}
	if booted != 0 {
		t.Fatalf("NewPool eagerly booted %d instance(s); all instantiation must defer to Borrow", booted)
	}

	// First Borrow lazily creates exactly one live instance and it must parse.
	inst, err := p.Borrow(ctx)
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	if inst == nil {
		t.Fatal("Borrow returned a nil instance — the permit was not upgraded to a live runtime")
	}
	if _, err := inst.ParseString(ctx, "package p\n"); err != nil {
		t.Fatalf("ParseString on lazily-created instance: %v", err)
	}
	p.Return(inst)
}
