package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
)

// checkCodeIndex implements category 11: code-index freshness.
//
// The code index is opt-in — its absence is normal and reports PASS
// (informational). Only a stale or missing-but-previously-present index
// warrants a WARN. This check never produces FAIL.
//
// When the doctor runs at a wiki realm root (detected via realm.Resolve), it
// aggregates across all non-excluded member dbs instead of the single local db.
func checkCodeIndex(opts Opts) Result {
	cwd, err := os.Getwd()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not get cwd: %v", err)}
	}

	// Derive claudeMDPath: prefer the injected value in opts, fall back to $HOME.
	claudeMDPath := opts.ClaudeMDPath
	if claudeMDPath == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			// Can't locate CLAUDE.md — degrade to single-repo path.
			root := gitToplevel(cwd)
			return RunCheckCodeIndexWith(root, opts.StaleDays)
		}
		claudeMDPath = filepath.Join(home, ".claude", "CLAUDE.md")
	}

	// Attempt realm detection.
	res, rerr := realm.Resolve(cwd, claudeMDPath)
	if rerr != nil {
		// Registry read error: fall through to single-repo path.
		root := gitToplevel(cwd)
		return RunCheckCodeIndexWith(root, opts.StaleDays)
	}

	if res.Scope == realm.ScopeRealmAll || res.Scope == realm.ScopeRealmMember {
		// Both realm scopes report the aggregate across all non-excluded members.
		// ScopeRealmMember carries a valid RealmRoot even when cwd is inside a
		// member dir — the aggregate is the correct view (the member is indexed in
		// the realm db, not in a local .claude/.atomic-index).
		return RunCheckCodeIndexRealmWith(res.RealmRoot, opts.StaleDays)
	}

	// Single-repo path (ScopeRepo, ScopeNoIndex).
	root := gitToplevel(cwd)
	return RunCheckCodeIndexWith(root, opts.StaleDays)
}

// RunCheckCodeIndex is the exported entry point for the dispatcher. It delegates
// to checkCodeIndex so tests can exercise the full scope-detection branch without
// requiring package-internal access.
func RunCheckCodeIndex(opts Opts) Result {
	return checkCodeIndex(opts)
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

// RunCheckCodeIndexRealmWith runs the realm-aware code-index freshness check.
// It aggregates across all non-excluded members in the realm config:
//   - fresh (db exists, age < staleDays): counted
//   - stale (db exists, age ≥ staleDays): named, triggers WARN
//   - not indexed (db absent): named, triggers WARN
//
// All-fresh or no members → PASS. This check never produces FAIL.
// Exported for testing; production callers use checkCodeIndex.
func RunCheckCodeIndexRealmWith(realmRoot string, staleDays int) Result {
	cfg, err := realm.LoadConfig(realmRoot)
	if err != nil {
		// Config parse error: surface but don't FAIL.
		return Result{Severity: WARN, Detail: fmt.Sprintf("realm config error: %v", err)}
	}

	var members []realm.MemberEntry
	if cfg != nil {
		// Filter excluded members.
		for _, m := range cfg.Members {
			if !m.Exclude {
				members = append(members, m)
			}
		}
	}

	if len(members) == 0 {
		return Result{
			Severity: PASS,
			Detail:   "code index: no realm members configured",
		}
	}

	var fresh, stale, notIndexed []string
	for _, m := range members {
		dbPath := filepath.Join(realmRoot, ".atomic", m.Key+".db")
		fi, serr := os.Stat(dbPath)
		if serr != nil {
			if os.IsNotExist(serr) {
				notIndexed = append(notIndexed, m.Key)
			} else {
				// Unreadable db counts as stale for safety.
				stale = append(stale, m.Key)
			}
			continue
		}
		age := time.Since(fi.ModTime())
		days := int(age.Hours() / 24)
		if days >= staleDays {
			stale = append(stale, m.Key)
		} else {
			fresh = append(fresh, m.Key)
		}
	}

	// Build detail line following the spec example:
	// "code index: 6 fresh; stale: foo, bar (run atomic code sync); not indexed: baz"
	parts := []string{fmt.Sprintf("code index: %d fresh", len(fresh))}
	if len(stale) > 0 {
		parts = append(parts, fmt.Sprintf("stale: %s (run atomic code sync)", strings.Join(stale, ", ")))
	}
	if len(notIndexed) > 0 {
		parts = append(parts, fmt.Sprintf("not indexed: %s", strings.Join(notIndexed, ", ")))
	}
	detail := strings.Join(parts, "; ")

	if len(stale) > 0 || len(notIndexed) > 0 {
		return Result{Severity: WARN, Detail: detail}
	}
	return Result{Severity: PASS, Detail: detail}
}
