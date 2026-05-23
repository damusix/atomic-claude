package doctor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/signals"
)

const (
	routerFile   = ".claude/project/signals.md"
	routerRef    = "@.claude/project/signals.md"
	domainSubdir = ".claude/project/signals"
)

// checkSignals implements category 3: signals freshness + router integrity.
//
// Resolution order:
//  1. signals file missing → WARN
//  2. file older than StaleDays (wall-clock) → WARN with age detail
//  3. signals.Stale() returns ErrStale (source tree newer) → WARN with ErrStale detail
//  4. router integrity checks (see RunCheckRouterWith)
//  5. all clear → PASS
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

	// Router integrity: WARN-level; never upgrades to FAIL.
	routerResult := RunCheckRouterWith(root)
	if routerResult.Severity == WARN {
		return routerResult
	}

	return Result{
		Severity: PASS,
		Detail:   fmt.Sprintf("last scan %dd ago (threshold %dd)", days, staleDays),
	}
}

// RunCheckRouterWith validates the router (signals.md) integrity for the repo at root.
//
// Checks (all WARN, never FAIL — pre-migration state is valid):
//  1. signals.md absent → WARN (pre-migration; old flat files still valid)
//  2. signals.md present but not @-ref'd in any CLAUDE.md-family file → WARN
//  3. domain files referenced in router table missing on disk → WARN
//  4. orphan domain files under signals/ not in router table → WARN
//
// Exported for testing; production callers use checkSignals.
func RunCheckRouterWith(root string) Result {
	routerPath := filepath.Join(root, routerFile)
	raw, err := os.ReadFile(routerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Severity: WARN, Detail: "signals.md not present; run 'atomic signals scan' to migrate"}
		}
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not read signals.md: %v", err)}
	}

	// Check @-ref wired in a CLAUDE.md-family file.
	if !routerRefWired(root) {
		return Result{Severity: WARN, Detail: "signals.md not @-ref'd in CLAUDE.md, claude.local.md, CLAUDE.local.md, or claude.md"}
	}

	// Parse domain file paths from the router table (Detail column).
	referenced := parseRouterDomains(string(raw))

	// Check all referenced domain files exist.
	for _, rel := range referenced {
		full := filepath.Join(root, ".claude", "project", rel)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			return Result{Severity: WARN, Detail: fmt.Sprintf("domain file referenced in router table missing: %s", rel)}
		}
	}

	// Check for orphan domain files under signals/.
	orphans := findOrphanDomains(root, referenced)
	if len(orphans) > 0 {
		return Result{Severity: WARN, Detail: fmt.Sprintf("orphan domain files not in router table: %s", strings.Join(orphans, ", "))}
	}

	return Result{Severity: PASS, Detail: "router present, @-ref'd, all domain files consistent"}
}

// routerRefWired returns true if @.claude/project/signals.md appears in any
// CLAUDE.md-family file at the repo root.
func routerRefWired(root string) bool {
	for _, name := range candidateFiles {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		if strings.Contains(string(raw), routerRef) {
			return true
		}
	}
	return false
}

// parseRouterDomains extracts non-empty Detail column values from the Domains
// table in the router file content. The Detail column is the 4th pipe-separated
// column. Returns relative paths like "signals/auth.md" or "signals/auth/index.md".
func parseRouterDomains(content string) []string {
	var result []string
	inTable := false
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Domains table header detection.
		if strings.HasPrefix(line, "## Domains") {
			inTable = true
			continue
		}
		// Any non-table line after the header ends the table.
		if inTable && !strings.HasPrefix(line, "|") && line != "" {
			inTable = false
		}
		if !inTable || !strings.HasPrefix(line, "|") {
			continue
		}
		// Skip separator rows (e.g. |---|---|)
		if strings.Contains(line, "---") {
			continue
		}
		cols := strings.Split(line, "|")
		// Expect at least 5 elements: ["", col1, col2, col3, col4, ...]
		if len(cols) < 5 {
			continue
		}
		detail := strings.TrimSpace(cols[4])
		if detail == "" || strings.EqualFold(detail, "Detail") {
			continue
		}
		result = append(result, detail)
	}
	return result
}

// findOrphanDomains lists files under .claude/project/signals/ that are not
// in the referenced set. Returns relative paths like "signals/stale.md".
func findOrphanDomains(root string, referenced []string) []string {
	// Build lookup set of referenced paths (O(1) check).
	refSet := make(map[string]bool, len(referenced))
	for _, r := range referenced {
		refSet[r] = true
	}

	dir := filepath.Join(root, domainSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory absent = no domain files = no orphans.
		return nil
	}

	var orphans []string
	for _, e := range entries {
		if e.IsDir() {
			// Check for index.md inside sub-dirs.
			subDir := filepath.Join(dir, e.Name())
			subEntries, err := os.ReadDir(subDir)
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if se.IsDir() {
					continue
				}
				rel := "signals/" + e.Name() + "/" + se.Name()
				if !refSet[rel] {
					orphans = append(orphans, rel)
				}
			}
			continue
		}
		rel := "signals/" + e.Name()
		if !refSet[rel] {
			orphans = append(orphans, rel)
		}
	}
	return orphans
}
