package harness

import (
	"fmt"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// SharedConsumers names the targets an operation publishing one shared physical
// resource must rewrite, excluding mover — the target the caller writes
// directly. Enrolled targets of kind are consumers because the ledger records
// them; a row whose target key does not parse is a stale record an older writer
// left, which nothing enrolls, nothing in this operation will move, and nothing
// may block on, so it is rewritten alongside them. A row for a target that
// parses but the ledger does not enroll is a genuine pin: this operation will
// not move it, so it is not in the set and EnsureSharedGeneration refuses it.
//
// EnsureSharedGeneration returns this same set, which is what lets an enrollment
// assert it converged every consumer it did not refuse.
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

// EnsureSharedGeneration refuses when a shared physical resource carries a
// generation claim this operation cannot move, and returns the consumers the
// operation must rewrite in the same commit.
//
// One binary is the only writer, and the plan that publishes a shared resource
// converges every enrolled consumer of it in that same operation, so an
// enrolled consumer always requires the selected generation. The generation its
// row records is what Atomic last wrote, not a requirement: a new generation
// makes every such row stale by construction, and refusing on one would
// deadlock two consumers of the same resource forever.
//
// The one genuine pin is a row for a target the ledger does not enroll but
// whose key parses: nothing in this operation will move that consumer, so the
// shared resource must not move either. A row whose key does not parse is not a
// pin — it cannot name a consumer to move or to refuse — and is rewritten with
// the rest. The refusal happens before any mutation and names both sides, the
// pinned target by its raw ledger key.
//
// noun is the resource's kind in the adapter's own vocabulary ("package",
// "plugin tree"), so the refusal reads in the operator's terms without each
// adapter keeping a second copy of the rule.
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

// AssertSharedConsumers proves every consumer the generation check named was
// rewritten onto generation in the same commit. It is the assertion that keeps
// the refusal's premise true: EnsureSharedGeneration approves a shared-resource
// move because another file's loop rewrites those rows, so a loop that missed
// one would otherwise pin a consumer forever.
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
