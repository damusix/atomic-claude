package doctor

import (
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// ClaudeHomeMissing is the pre-flight gate: no <home>/.claude means no
// Claude-scoped category check can say anything useful.
func ClaudeHomeMissing(home string) bool {
	_, err := os.Stat(filepath.Join(home, ".claude"))
	return os.IsNotExist(err)
}

// HasEnrolledTargets reports whether the install ledger records any enrolled
// harness target, Claude or otherwise. An OMP-only home has no ~/.claude but
// still owns lifecycle state the harness categories must report on.
//
// A ledger that cannot be read — permissions, undecodable JSON, or a schema
// this binary refuses — counts as enrolled. That read failure is exactly the
// defect the harness categories exist to surface, so short-circuiting it to
// "atomic-claude not installed" would swallow it; running the categories lets
// them report the unreadable ledger instead.
func HasEnrolledTargets(home string) bool {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return true
	}
	return len(ledger.Targets) > 0
}

// ShortCircuit reports whether doctor should stop before running any category.
// A missing Claude home only short-circuits a machine with nothing enrolled; as
// soon as the ledger holds a target — an OMP-only home included — the harness
// categories run and the Claude-scoped ones skip on their own.
func ShortCircuit(home string) bool {
	return ClaudeHomeMissing(home) && !HasEnrolledTargets(home)
}

// claudeHomeMissing reports whether the Claude home a category reads is absent.
// opts.Home is the CLI-resolved home; a directly-invoked check falls back to
// the OS home.
func claudeHomeMissing(opts Opts) bool {
	home := opts.Home
	if home == "" {
		resolved, err := os.UserHomeDir()
		if err != nil {
			return true
		}
		home = resolved
	}
	return ClaudeHomeMissing(home)
}

const missingHomeMessage = "atomic-claude not installed; run `atomic claude install`."

// MissingHomeMessage returns the short-circuit message.
func MissingHomeMessage() string {
	return missingHomeMessage
}
