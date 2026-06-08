package doctor

import (
	"fmt"
	"os"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
)

// checkCodeIndex implements category 11: code-index freshness.
//
// The code index is opt-in — its absence is normal and reports PASS
// (informational). Only a stale or missing-but-previously-present index
// warrants a WARN. This check never produces FAIL.
func checkCodeIndex(opts Opts) Result {
	cwd, err := os.Getwd()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not get cwd: %v", err)}
	}
	root := gitToplevel(cwd)
	return RunCheckCodeIndexWith(root, opts.StaleDays)
}

// RunCheckCodeIndexWith runs the code-index freshness check against an explicit
// project root and staleness threshold. Exported for testing; production callers
// use checkCodeIndex.
//
// Staleness signal: DB mtime age against staleDays, mirroring checks_signals.go.
// We stat the DB file rather than opening it — doctor checks are read-only and
// the DB open path (modernc.org/sqlite + engine.Open) spins up the WASM pool,
// which is too heavyweight for a health check. Mtime age is the honest proxy
// for "has the index been synced recently". If the project's source tree changes
// faster than staleDays, `atomic code sync` restores freshness — the same
// actionable advice regardless of the exact staleness measure chosen.
func RunCheckCodeIndexWith(root string, staleDays int) Result {
	dbPath := engine.IndexPath(root)

	fi, err := os.Stat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Absence is normal — the index is opt-in. Informational PASS only.
			// "atomic code index" (not "sync") is intentional: index = cold-start
			// creation of a missing index; sync = refresh an existing one.
			return Result{
				Severity: PASS,
				Detail:   "code index not initialized (optional; run 'atomic code index' to enable)",
			}
		}
		// Stat error other than not-exist: something odd but not a hard failure.
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not stat code index: %v", err)}
	}

	age := time.Since(fi.ModTime())
	days := int(age.Hours() / 24)

	if days >= staleDays {
		return Result{
			Severity: WARN,
			Detail:   fmt.Sprintf("code index last synced %dd ago (threshold %dd); run 'atomic code sync'", days, staleDays),
		}
	}

	return Result{
		Severity: PASS,
		Detail:   fmt.Sprintf("code index synced %dd ago (threshold %dd)", days, staleDays),
	}
}
