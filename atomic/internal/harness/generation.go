package harness

import (
	"fmt"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// EnsureSharedGeneration refuses when another enrolled target's recorded
// generation of one shared physical resource is not the one a plan publishes.
// One physical resource cannot hold two generations, so a plan that moves it
// must move every consumer with it; converging one target while another records
// a different generation would leave that target's ledger row describing bytes
// that are no longer there. The refusal happens before any mutation and names
// both targets so the operator converges the shared resource as one operation.
//
// noun is the resource's kind in the adapter's own vocabulary ("package",
// "plugin tree"), so the refusal reads in the operator's terms without each
// adapter keeping a second copy of the rule.
func EnsureSharedGeneration(ledger *installstate.Ledger, adapter, target, resource, noun, generation string) error {
	var others []string
	for _, row := range ledger.Rows {
		if row.Resource != resource || row.Target == target {
			continue
		}
		if row.Generation == generation {
			continue
		}
		want := row.Generation
		if want == "" {
			want = "an unrecorded generation"
		}
		others = append(others, fmt.Sprintf("%s requires %s", row.Target, want))
	}
	if len(others) == 0 {
		return nil
	}
	sort.Strings(others)
	return fmt.Errorf("%s: shared %s %s: %s requires generation %s, but %s; convergence refuses before %s mutation",
		adapter, noun, resource, target, generation, strings.Join(others, ", "), noun)
}
