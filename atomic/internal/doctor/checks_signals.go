package doctor

import (
	"fmt"
	"os"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/signals"
)

// checkSignals implements category 3: signals freshness.
//
// Resolution order:
//  1. signals file missing → WARN
//  2. file older than StaleDays (wall-clock) → WARN with age detail
//  3. signals.Stale() returns ErrStale (source tree newer) → WARN with ErrStale detail
//  4. all clear → PASS
func checkSignals(opts Opts) Result {
	cwd, err := os.Getwd()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not get cwd: %v", err)}
	}
	root := gitToplevel(cwd)
	return RunCheckSignalsWith(root, opts.StaleDays)
}

// RunCheckSignalsWith runs the signals freshness check against an explicit root.
// Exported for testing; production callers use checkSignals.
func RunCheckSignalsWith(root string, staleDays int) Result {
	path := signals.SignalsPath(root)

	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Severity: WARN, Detail: "signals not generated; run 'atomic signals scan'"}
		}
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not stat signals file: %v", err)}
	}

	age := time.Since(fi.ModTime())
	days := int(age.Hours() / 24)

	if days >= staleDays {
		return Result{
			Severity: WARN,
			Detail:   fmt.Sprintf("last scan %dd ago (threshold %dd)", days, staleDays),
		}
	}

	// Check if source tree changed since last scan (regardless of age).
	if err := signals.Stale(root); err == signals.ErrStale {
		return Result{
			Severity: WARN,
			Detail:   fmt.Sprintf("source tree changed since last scan %dd ago", days),
		}
	}

	return Result{
		Severity: PASS,
		Detail:   fmt.Sprintf("last scan %dd ago (threshold %dd)", days, staleDays),
	}
}
