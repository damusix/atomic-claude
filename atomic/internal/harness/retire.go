package harness

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Card directories earlier versions projected, relative to the repository.
// Nothing writes them now; RetireTree clears them.
const (
	LegacyOMPCards    = ".omp/rules/atomic-wiki"
	LegacyClaudeCards = ".claude/rules/atomic-wiki"
)

// RetireTree removes the tree at path, and its rows, when a ledger row matches
// its bytes. A tree no row proves Atomic wrote is kept for the user.
func RetireTree(ledger *installstate.Ledger, path string) (removed, kept bool, err error) {
	obs, err := managedfile.Observe(path, managedfile.KindTree)
	if err != nil {
		return false, false, err
	}
	if !obs.Exists() {
		return false, false, nil
	}
	matched := false
	for _, row := range ledger.Rows {
		if row.Applied.Path == path && row.Applied.Kind == managedfile.KindTree && row.Applied.Digest == obs.Digest {
			matched = true
		}
	}
	if !matched {
		return false, true, nil
	}
	if err := os.RemoveAll(path); err != nil {
		return false, false, fmt.Errorf("harness: retire %s: %w", path, err)
	}
	rows := ledger.Rows[:0:0]
	for _, row := range ledger.Rows {
		if row.Applied.Path != path {
			rows = append(rows, row)
		}
	}
	ledger.Rows = rows
	return true, false, nil
}
