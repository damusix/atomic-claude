package extraction

// NewPool used to boot one wazero runtime per CPU eagerly; each compiles a
// tree-sitter grammar (~0.5 s, ~4.7 s CPU and ~1.9 GB RSS for a default pool).
// A no-op or small incremental `atomic code sync` paid that in full while
// borrowing few or zero instances, so instantiation must defer to first Borrow.
// This is an internal test because the instance count is observable only
// through the package-private channel.

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

	// A nil slot is an unbooted permit; anything else is a live runtime.
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
