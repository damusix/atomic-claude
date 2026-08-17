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

// checkCodeIndex implements category 11: code-index freshness. The index is
// opt-in, so its absence is an informational PASS; only a stale or unreadable
// index WARNs. Never FAILs. At a wiki realm root this aggregates across all
// non-excluded member dbs instead of the single local db.
func checkCodeIndex(opts Opts) Result {
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Result{Severity: WARN, Detail: fmt.Sprintf("could not get cwd: %v", err)}
		}
		repoRoot = gitToplevelFn(cwd)
	}

	claudeMDPath := opts.ClaudeMDPath
	if claudeMDPath == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return RunCheckCodeIndexWith(repoRoot, opts.StaleDays)
		}
		claudeMDPath = filepath.Join(home, ".claude", "CLAUDE.md")
	}

	// realm.Resolve needs the real cwd, not repoRoot, to tell ScopeRealmMember
	// from ScopeRealmAll.
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		return RunCheckCodeIndexWith(repoRoot, opts.StaleDays)
	}
	res, rerr := realm.Resolve(cwd, claudeMDPath)
	if rerr != nil {
		return RunCheckCodeIndexWith(repoRoot, opts.StaleDays)
	}

	if res.Scope == realm.ScopeRealmAll || res.Scope == realm.ScopeRealmMember {
		// ScopeRealmMember reports the aggregate too: a member is indexed in the
		// realm db, not in a local .claude/.atomic-index.
		return RunCheckCodeIndexRealmWith(res.RealmRoot, opts.StaleDays)
	}

	return RunCheckCodeIndexWith(repoRoot, opts.StaleDays)
}

// RunCheckCodeIndex is the dispatcher entry point for the code-index check.
func RunCheckCodeIndex(opts Opts) Result {
	return checkCodeIndex(opts)
}

// RunCheckCodeIndexWith runs the code-index freshness check against an explicit
// project root and staleness threshold. Exported for testing.
//
// Staleness is DB mtime age. We stat rather than open: engine.Open spins up the
// WASM pool, too heavyweight for a health check.
func RunCheckCodeIndexWith(root string, staleDays int) Result {
	dbPath := engine.IndexPath(root)

	fi, err := os.Stat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			// "index" not "sync": index creates a missing index, sync refreshes one.
			return Result{
				Severity: PASS,
				Detail:   "code index not initialized (optional; run 'atomic code index' to enable)",
			}
		}
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

// RunCheckCodeIndexRealmWith runs the realm-aware code-index freshness check,
// aggregating across all non-excluded members. Any stale or unindexed member
// WARNs; all-fresh or no members PASSes. Never FAILs. Exported for testing.
func RunCheckCodeIndexRealmWith(realmRoot string, staleDays int) Result {
	cfg, err := realm.LoadConfig(realmRoot)
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("realm config error: %v", err)}
	}

	var members []realm.MemberEntry
	if cfg != nil {
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
				// An unreadable db counts as stale, not fresh.
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
