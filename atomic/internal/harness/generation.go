package harness

import (
	"fmt"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// SharedConsumers names the enrolled targets that consume one shared physical
// resource: every ledger target record of the resource's harness that already
// owns a row for it. The resource is the harness's one generated package tree,
// so each of those targets reads the same bytes and must agree on their
// generation.
func SharedConsumers(ledger *installstate.Ledger, kind Kind, resource string) []string {
	enrolled := make(map[string]bool, len(ledger.Targets))
	for _, record := range ledger.Targets {
		if record.Harness != string(kind) {
			continue
		}
		enrolled[installstate.TargetRecord{Harness: record.Harness, Instance: record.Instance}.Key()] = true
	}
	var consumers []string
	for _, row := range ledger.Rows {
		if row.Resource != resource || !enrolled[row.Target] {
			continue
		}
		if !contains(consumers, row.Target) {
			consumers = append(consumers, row.Target)
		}
	}
	sort.Strings(consumers)
	return consumers
}

// EnsureSharedGeneration refuses when a shared physical resource carries a
// generation claim this operation cannot move.
//
// One binary is the only writer, and the plan that publishes a shared resource
// converges every enrolled consumer of it in that same operation, so an
// enrolled consumer always requires the selected generation. The generation its
// row records is what Atomic last wrote, not a requirement: a new generation
// makes every such row stale by construction, and refusing on one would
// deadlock two consumers of the same resource forever. The one genuine pin is a
// row for a target the ledger does not enroll — nothing in this operation will
// move that consumer, so the shared resource must not move either. The refusal
// happens before any mutation and names both sides.
//
// noun is the resource's kind in the adapter's own vocabulary ("package",
// "plugin tree"), so the refusal reads in the operator's terms without each
// adapter keeping a second copy of the rule.
func EnsureSharedGeneration(ledger *installstate.Ledger, adapter, target, resource, noun, generation string) error {
	var pinned []string
	for _, row := range ledger.Rows {
		if row.Resource != resource || row.Target == target {
			continue
		}
		if row.Generation == generation {
			continue
		}
		if enrolledTarget(ledger, row.Target) {
			// An enrolled consumer: this operation converges it too, so the
			// generation its row records is stale, not a requirement.
			continue
		}
		want := row.Generation
		if want == "" {
			want = "an unrecorded generation"
		}
		pinned = append(pinned, fmt.Sprintf("%s requires %s", row.Target, want))
	}
	if len(pinned) == 0 {
		return nil
	}
	sort.Strings(pinned)
	return fmt.Errorf("%s: shared %s %s: %s requires generation %s, but %s; convergence refuses before %s mutation",
		adapter, noun, resource, target, generation, strings.Join(pinned, ", "), noun)
}

// enrolledTarget reports whether the ledger holds a target record for a row's
// target key. A key that does not parse cannot match a record, so its row stays
// a pin.
func enrolledTarget(ledger *installstate.Ledger, key string) bool {
	t, err := ParseKey(key)
	if err != nil {
		return false
	}
	_, ok := ledger.FindTarget(string(t.Kind), t.Instance)
	return ok
}
