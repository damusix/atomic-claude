package harness

import (
	"fmt"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// SharedConsumers names the other targets a shared-resource publish must rewrite:
// enrolled targets, plus stale rows whose key does not parse and so cannot pin.
func SharedConsumers(ledger *installstate.Ledger, kind Kind, resource, mover string) []string {
	enrolled := make(map[string]bool, len(ledger.Targets))
	for _, record := range ledger.Targets {
		if record.Harness != string(kind) {
			continue
		}
		enrolled[record.Key()] = true
	}
	var consumers []string
	for _, row := range ledger.Rows {
		if row.Resource != resource || row.Target == mover {
			continue
		}
		if !enrolled[row.Target] && parsesAsTarget(row.Target) {
			continue
		}
		consumers = appendUnique(consumers, row.Target)
	}
	sort.Strings(consumers)
	return consumers
}

// EnsureSharedGeneration refuses only an unenrolled parseable consumer; refusing
// on an enrolled consumer's stale generation would deadlock two consumers.
func EnsureSharedGeneration(ledger *installstate.Ledger, kind Kind, adapter, target, resource, noun, generation string) ([]string, error) {
	consumers := SharedConsumers(ledger, kind, resource, target)
	rewrite := make(map[string]bool, len(consumers))
	for _, consumer := range consumers {
		rewrite[consumer] = true
	}
	var pinned []string
	for _, row := range ledger.Rows {
		if row.Resource != resource || row.Target == target {
			continue
		}
		if row.Generation == generation || rewrite[row.Target] {
			continue
		}
		want := row.Generation
		if want == "" {
			want = "an unrecorded generation"
		}
		pinned = append(pinned, fmt.Sprintf("%s requires %s", row.Target, want))
	}
	if len(pinned) == 0 {
		return consumers, nil
	}
	sort.Strings(pinned)
	return nil, fmt.Errorf("%s: shared %s %s: %s requires generation %s, but %s; convergence refuses before %s mutation",
		adapter, noun, resource, target, generation, strings.Join(pinned, ", "), noun)
}

// AssertSharedConsumers proves every named consumer was rewritten onto
// generation; a missed one would pin the shared resource forever.
func AssertSharedConsumers(ledger *installstate.Ledger, adapter, resource, generation string, consumers []string) error {
	for _, consumer := range consumers {
		row, ok := ledger.Find(consumer, resource)
		if !ok {
			return fmt.Errorf("%s: shared resource %s: consumer %s has no row after convergence onto generation %s", adapter, resource, consumer, generation)
		}
		if row.Generation != generation {
			return fmt.Errorf("%s: shared resource %s: consumer %s still records generation %q after convergence onto %s", adapter, resource, consumer, row.Generation, generation)
		}
	}
	return nil
}

// parsesAsTarget reports whether a ledger target key parses into a target this
// binary could enroll. A key that does not is stale state, not a consumer.
func parsesAsTarget(key string) bool {
	_, err := ParseKey(key)
	return err == nil
}
