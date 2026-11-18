package harness

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

const sharedResource = "/home/u/.atomic/packages/omp/atomic"

// sharedLedger shares one OMP package across two enrolled profiles, an
// unenrolled Claude target, and a stale row whose key does not parse.
func sharedLedger(resource string) *installstate.Ledger {
	return &installstate.Ledger{
		Header: installstate.NewHeader(),
		Targets: []installstate.TargetRecord{
			{Harness: "omp", Instance: "/a", NativeRoot: "/a"},
			{Harness: "omp", Instance: "/b", NativeRoot: "/b"},
		},
		Rows: []installstate.Row{
			{Target: "omp:/a", Resource: resource, Consumer: "omp:/a", Generation: "gen-1"},
			{Target: "omp:/b", Resource: resource, Consumer: "omp:/b", Generation: "gen-1"},
			{Target: "claude:/c", Resource: resource, Consumer: "claude:/c", Generation: "gen-1"},
			{Target: ":stale", Resource: resource, Consumer: ":stale", Generation: "gen-1"},
		},
	}
}

// A stale unparseable row must not become a permanent pin on the shared package.
func TestUnparseableRowIsRewrittenNotPinned(t *testing.T) {
	ledger := sharedLedger(sharedResource)
	_, err := EnsureSharedGeneration(ledger, KindOMP, "omp", "omp:/a", sharedResource, "package", "gen-2")
	if err == nil {
		t.Fatal("an unenrolled parseable consumer did not refuse the shared move")
	}
	if !strings.Contains(err.Error(), "claude:/c requires gen-1") {
		t.Fatalf("refusal = %q, want it to name the pinned claude consumer", err)
	}
	if strings.Contains(err.Error(), ":stale") {
		t.Errorf("refusal = %q, want no mention of the unparseable row", err)
	}

	// Without the genuine pin, the move is approved and the unparseable row is
	// named a consumer to rewrite.
	ledger.Rows = []installstate.Row{ledger.Rows[0], ledger.Rows[1], ledger.Rows[3]}
	consumers, err := EnsureSharedGeneration(ledger, KindOMP, "omp", "omp:/a", sharedResource, "package", "gen-2")
	if err != nil {
		t.Fatalf("unparseable row blocked the shared move: %v", err)
	}
	if !contains(consumers, ":stale") {
		t.Errorf("consumers = %v, want the unparseable row rewritten", consumers)
	}
	if !contains(consumers, "omp:/b") {
		t.Errorf("consumers = %v, want the enrolled consumer rewritten", consumers)
	}
	if contains(consumers, "omp:/a") {
		t.Errorf("consumers = %v, want the moving target excluded", consumers)
	}
}

// The operation that approves a shared move must be the one that rewrites
// every consumer row it approved.
func TestSharedConsumersMatchesGenerationRewriteSet(t *testing.T) {
	ledger := sharedLedger(sharedResource)
	ledger.Rows = []installstate.Row{ledger.Rows[0], ledger.Rows[1], ledger.Rows[3]}

	consumers := SharedConsumers(ledger, KindOMP, sharedResource, "omp:/a")
	rewrite, err := EnsureSharedGeneration(ledger, KindOMP, "omp", "omp:/a", sharedResource, "package", "gen-2")
	if err != nil {
		t.Fatalf("EnsureSharedGeneration: %v", err)
	}
	if len(consumers) != len(rewrite) {
		t.Fatalf("consumer sets differ: SharedConsumers = %v, rewrite set = %v", consumers, rewrite)
	}
	for i := range consumers {
		if consumers[i] != rewrite[i] {
			t.Fatalf("consumer sets differ at %d: SharedConsumers = %v, rewrite set = %v", i, consumers, rewrite)
		}
	}
}

// A missed consumer must fail loudly now rather than pin the resource later.
func TestAssertSharedConsumersRefusesAMissedConsumer(t *testing.T) {
	ledger := sharedLedger(sharedResource)
	ledger.Rows = []installstate.Row{
		{Target: "omp:/b", Resource: sharedResource, Consumer: "omp:/b", Generation: "gen-1"},
	}
	err := AssertSharedConsumers(ledger, "omp", sharedResource, "gen-2", []string{"omp:/b"})
	if err == nil {
		t.Fatal("a consumer still on the old generation was accepted")
	}
	if !strings.Contains(err.Error(), "omp:/b") {
		t.Errorf("assertion = %q, want it to name the missed consumer", err)
	}

	ledger.Rows[0].Generation = "gen-2"
	if err := AssertSharedConsumers(ledger, "omp", sharedResource, "gen-2", []string{"omp:/b"}); err != nil {
		t.Fatalf("converged consumer refused: %v", err)
	}
}
