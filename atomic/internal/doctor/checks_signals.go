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
	routerFile   = "docs/wiki/index.md"
	routerRef    = "@docs/wiki/index.md"
	domainSubdir = "docs/wiki"
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
	root := opts.RepoRoot
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Result{Severity: WARN, Detail: fmt.Sprintf("could not get cwd: %v", err)}
		}
		root = gitToplevelFn(cwd)
	}
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
	if _, err := signals.Stale(root); err == signals.ErrStale {
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

// RunCheckRouterWith validates the router (docs/wiki/index.md) integrity for the repo at root.
//
// Checks (all WARN, never FAIL — pre-migration state is valid):
//  1. docs/wiki/index.md absent → WARN
//  2. docs/wiki/index.md present but not @-ref'd in any CLAUDE.md-family file → WARN
//  3. domain files referenced in router table missing on disk → WARN
//  4. orphan domain files under docs/wiki/ not in router table → WARN
//
// Exported for testing; production callers use checkSignals.
func RunCheckRouterWith(root string) Result {
	routerPath := filepath.Join(root, routerFile)
	raw, err := os.ReadFile(routerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Severity: WARN, Detail: "docs/wiki/index.md not present; run 'atomic signals scan' to generate"}
		}
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not read docs/wiki/index.md: %v", err)}
	}

	// Check @-ref wired in a CLAUDE.md-family file.
	if !routerRefWired(root) {
		return Result{Severity: WARN, Detail: "docs/wiki/index.md not @-ref'd in CLAUDE.md, claude.local.md, CLAUDE.local.md, or claude.md"}
	}

	// Parse domain file paths from the router table (Detail column).
	referenced := parseRouterDomains(string(raw))

	// Check all referenced domain files exist.
	for _, rel := range referenced {
		full := filepath.Join(root, domainSubdir, rel)
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

// routerRefWired returns true if @docs/wiki/index.md appears in any
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
// column. Returns relative paths like "auth.md" (bare filenames in the flat
// docs/wiki/ layout).
//
// The Detail cell may be either a bare path (e.g. "auth.md") or a linkified
// markdown link (e.g. "[`docs/wiki/auth.md`](auth.md)"). In the latter case the
// link TARGET (the `](...)` part) is extracted, since that is the actual relative
// path on disk. The spec requires that linkify emits targets that join correctly
// as root/docs/wiki/<target>.
func parseRouterDomains(content string) []string {
	var result []string
	inSection := false
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Enter the Domains section on its heading.
		if line == "## Domains" || strings.HasPrefix(line, "## Domains ") {
			inSection = true
			continue
		}
		// Any subsequent heading exits the section.
		if inSection && strings.HasPrefix(line, "## ") {
			break
		}
		// Within the section, skip blank lines and intro prose; only process table rows.
		if !inSection || !strings.HasPrefix(line, "|") {
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
		// If the cell is a markdown link [`text`](target), extract the target.
		if path, ok := extractLinkTarget(detail); ok {
			detail = path
		}
		result = append(result, detail)
	}
	return result
}

// extractLinkTarget extracts the link target from a markdown link of the form
// `[text](target)` or “ [`text`](target) “. Returns (target, true) when the
// cell is a markdown link; (_, false) otherwise.
func extractLinkTarget(cell string) (string, bool) {
	// Find '](' which separates text from target.
	idx := strings.Index(cell, "](")
	if idx == -1 {
		return "", false
	}
	// Must start with '['.
	if !strings.HasPrefix(cell, "[") {
		return "", false
	}
	// Extract from after '](' to the closing ')'.
	after := cell[idx+2:]
	closeIdx := strings.LastIndex(after, ")")
	if closeIdx == -1 {
		return "", false
	}
	target := strings.TrimSpace(after[:closeIdx])
	if target == "" {
		return "", false
	}
	return target, true
}

// excludedWikiFiles lists filenames in docs/wiki/ that are never domain files.
// They must be excluded from orphan enumeration even when absent from the router
// table: index.md is the router itself, scan.md is the raw deterministic dump,
// CLAUDE.md is the steering / nested-memory file.
var excludedWikiFiles = map[string]bool{
	"index.md":  true,
	"scan.md":   true,
	"CLAUDE.md": true,
}

// findOrphanDomains lists files under docs/wiki/ that are not in the referenced
// set, excluding the router (index.md), raw dump (scan.md), and steering
// (CLAUDE.md). The new layout is flat: subdirectories are not domain files.
// Returns bare filenames like "auth.md".
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
			continue // flat layout: subdirectories are not domain files
		}
		name := e.Name()
		if excludedWikiFiles[name] {
			continue // router / scan / steering are never domain files
		}
		if !refSet[name] {
			orphans = append(orphans, name)
		}
	}
	return orphans
}
