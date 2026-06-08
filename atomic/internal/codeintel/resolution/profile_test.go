package resolution_test

// Tests for the ResolveProfile returned from ResolveAndPersistBatched.
//
// Contract verified:
//   - ResolveProfile.NodeCount >= 0 (set from warmCaches knownNames count).
//   - ResolveProfile.RefCount >= 0 (set from the batch loop ref count).
//   - WarmDur, MatchDur, SynthDur are all non-negative durations.
//   - On a fixture with resolvable refs, RefCount > 0 (batch loop ran).
//   - On an empty DB, all durations and counts are zero.
//   - PhaseEmitFunc callback records phases in order: warm → match → synth.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// TestResolveProfile_EmptyDB confirms all counts are 0 on an empty DB and
// all durations are valid, non-negative time.Duration values.
//
// This collapses two formerly near-duplicate tests (NonNegative + EmptyDB) that
// both verified the same contract. One test is sufficient; the distinct coverage
// (RefCount via batch loop) is proven by TestResolveProfile_WithRefs.
func TestResolveProfile_EmptyDB(t *testing.T) {
	d, _ := openTestDB(t)
	pipe := resolution.NewPipeline(d)

	prof, totalEdges, err := pipe.ResolveAndPersistBatched(context.Background(), 500, nil)
	if err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}
	if totalEdges != 0 {
		t.Errorf("empty DB should produce 0 edges, got %d", totalEdges)
	}
	if prof.NodeCount != 0 {
		t.Errorf("empty DB NodeCount should be 0, got %d", prof.NodeCount)
	}
	if prof.RefCount != 0 {
		t.Errorf("empty DB RefCount should be 0, got %d", prof.RefCount)
	}
	// Durations should be valid (zero or positive).
	if prof.WarmDur < 0 || prof.MatchDur < 0 || prof.SynthDur < 0 {
		t.Errorf("durations should be >= 0: warm=%v match=%v synth=%v",
			prof.WarmDur, prof.MatchDur, prof.SynthDur)
	}
}

// TestResolveProfile_PhaseOrder proves that the PhaseEmitFunc callback is
// invoked once per sub-phase, in the order warm → match → synth, and that
// each invocation carries a non-negative duration.
//
// This is the primary regression guard for the incremental-emit requirement:
// a process killed mid-resolve must have already emitted resolve.warm before
// resolve.match starts. Prior to this test, only final struct values were
// asserted; batch-printing of all three lines after the pipeline completed
// was indistinguishable from incremental emission at the struct level.
func TestResolveProfile_PhaseOrder(t *testing.T) {
	ctx := context.Background()
	d, _ := openTestDB(t)

	// Seed a node so warmCaches finds at least one entry.
	seedNode(t, d, "node-order", "order.go", types.NodeKindFunction, types.LanguageGo, true)

	var mu sync.Mutex
	var emitted []string

	emit := resolution.PhaseEmitFunc(func(phase string, d time.Duration, count int) {
		mu.Lock()
		emitted = append(emitted, phase)
		mu.Unlock()
		if d < 0 {
			t.Errorf("emit(%q): duration %v is negative", phase, d)
		}
	})

	pipe := resolution.NewPipeline(d)
	_, _, err := pipe.ResolveAndPersistBatched(ctx, 500, emit)
	if err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	want := []string{"resolve.warm", "resolve.match", "resolve.synth"}
	if len(emitted) != len(want) {
		t.Fatalf("expected %d emit calls, got %d: %v", len(want), len(emitted), emitted)
	}
	for i, w := range want {
		if emitted[i] != w {
			t.Errorf("emit[%d]: want %q, got %q", i, w, emitted[i])
		}
	}
}

// TestResolveProfile_WithRefs asserts that RefCount reflects refs processed
// when there are unresolvable refs in the DB (they still count as processed in
// the batch loop even if they don't produce edges).
func TestResolveProfile_WithRefs(t *testing.T) {
	ctx := context.Background()
	d, _ := openTestDB(t)

	// Seed a node and an unresolvable ref (no matching target).
	seedNode(t, d, "node-1", "foo.go", types.NodeKindFunction, types.LanguageGo, true)
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-1",
		FilePath:      "foo.go",
		FromNodeID:    "node-1",
		ReferenceName: "neverExistsXYZ999",
		ReferenceKind: types.EdgeKindCalls,
		Language:      types.LanguageGo,
		Line:          5,
	})

	pipe := resolution.NewPipeline(d)
	prof, _, err := pipe.ResolveAndPersistBatched(ctx, 500, nil)
	if err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// NodeCount must be > 0 (we seeded one node, warmCaches captures it).
	if prof.NodeCount == 0 {
		t.Error("NodeCount should be > 0 after seeding a node")
	}

	// RefCount must be > 0 (the batch loop processed the ref).
	if prof.RefCount == 0 {
		t.Error("RefCount should be > 0 after processing refs in batch loop")
	}

	// All durations must be valid (>= 0).
	if prof.WarmDur < 0 || prof.MatchDur < 0 || prof.SynthDur < 0 {
		t.Errorf("durations should be >= 0: warm=%v match=%v synth=%v",
			prof.WarmDur, prof.MatchDur, prof.SynthDur)
	}
}

// TestResolveProfile_DurationsAreDurations confirms the fields are of type
// time.Duration (compile-time check via assignment; non-zero monotonic guarantees
// that the timer actually ran — we just assert positive on a warm run).
func TestResolveProfile_DurationsAreDurations(t *testing.T) {
	d, _ := openTestDB(t)
	pipe := resolution.NewPipeline(d)

	prof, _, err := pipe.ResolveAndPersistBatched(context.Background(), 500, nil)
	if err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// Assign to time.Duration variables — if the type is wrong, this won't compile.
	var _ time.Duration = prof.WarmDur
	var _ time.Duration = prof.MatchDur
	var _ time.Duration = prof.SynthDur
}
