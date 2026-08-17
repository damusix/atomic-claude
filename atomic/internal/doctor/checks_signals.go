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

// checkSignals is category 3: signals freshness and router integrity. WARN on
// a missing, aged, or out-of-date signals file, or a router inconsistency;
// never FAIL.
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

// RunCheckSignalsWith runs the freshness check against an explicit root.
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

	// A scan can be recent and still stale if the source tree moved under it.
	if _, err := signals.Stale(root); err == signals.ErrStale {
		return Result{
			Severity: WARN,
			Detail:   fmt.Sprintf("source tree changed since last scan %dd ago", days),
		}
	}

	routerResult := RunCheckRouterWith(root)
	if routerResult.Severity == WARN {
		return routerResult
	}

	return Result{
		Severity: PASS,
		Detail:   fmt.Sprintf("last scan %dd ago (threshold %dd)", days, staleDays),
	}
}

// RunCheckRouterWith validates docs/wiki/index.md: present, @-ref'd, and in
// agreement with the domain files on disk. Every failure is WARN, never FAIL —
// a repo that has not migrated yet is in a valid state.
func RunCheckRouterWith(root string) Result {
	routerPath := filepath.Join(root, routerFile)
	raw, err := os.ReadFile(routerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Severity: WARN, Detail: "docs/wiki/index.md not present; run 'atomic signals scan' to generate"}
		}
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not read docs/wiki/index.md: %v", err)}
	}

	if !routerRefWired(root) {
		return Result{Severity: WARN, Detail: "docs/wiki/index.md not @-ref'd in CLAUDE.md, claude.local.md, CLAUDE.local.md, or claude.md"}
	}

	referenced := parseRouterDomains(string(raw))

	for _, rel := range referenced {
		full := filepath.Join(root, domainSubdir, rel)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			return Result{Severity: WARN, Detail: fmt.Sprintf("domain file referenced in router table missing: %s", rel)}
		}
	}

	orphans := findOrphanDomains(root, referenced)
	if len(orphans) > 0 {
		return Result{Severity: WARN, Detail: fmt.Sprintf("orphan domain files not in router table: %s", strings.Join(orphans, ", "))}
	}

	return Result{Severity: PASS, Detail: "router present, @-ref'd, all domain files consistent"}
}

// routerRefWired reports whether any CLAUDE.md-family file @-refs the router.
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

// parseRouterDomains reads the Domains table's Detail column, returning bare
// filenames like "auth.md". A cell may be a bare path or a markdown link, in
// which case the link target is the real path on disk.
//
// Detail is located as the last content column rather than a fixed index:
// earlier cells carry unescaped pipes (e.g. "md|code search"), which would
// shift any fixed index off target.
func parseRouterDomains(content string) []string {
	var result []string
	inSection := false
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "## Domains" || strings.HasPrefix(line, "## Domains ") {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "## ") {
			break
		}
		if !inSection || !strings.HasPrefix(line, "|") {
			continue
		}
		if strings.Contains(line, "---") {
			continue
		}
		cols := strings.Split(line, "|")
		// A real 4-column row splits into 6, counting the empty strings the
		// leading and trailing pipes produce; 5 tolerates degenerate input.
		if len(cols) < 5 {
			continue
		}
		detail := strings.TrimSpace(cols[len(cols)-2])
		if detail == "" || strings.EqualFold(detail, "Detail") {
			continue
		}
		if path, ok := extractLinkTarget(detail); ok {
			detail = path
		}
		result = append(result, detail)
	}
	return result
}

// extractLinkTarget pulls the target out of a `[text](target)` cell.
func extractLinkTarget(cell string) (string, bool) {
	idx := strings.Index(cell, "](")
	if idx == -1 {
		return "", false
	}
	if !strings.HasPrefix(cell, "[") {
		return "", false
	}
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

// excludedWikiFiles are never domain files — the router itself, the raw scan
// dump, and the steering file — so they are not orphans when unreferenced.
var excludedWikiFiles = map[string]bool{
	"index.md":  true,
	"scan.md":   true,
	"CLAUDE.md": true,
}

// findOrphanDomains lists docs/wiki/ files absent from the router table. The
// layout is flat, so subdirectories are never domain files.
func findOrphanDomains(root string, referenced []string) []string {
	refSet := make(map[string]bool, len(referenced))
	for _, r := range referenced {
		refSet[r] = true
	}

	dir := filepath.Join(root, domainSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var orphans []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if excludedWikiFiles[name] {
			continue
		}
		if !refSet[name] {
			orphans = append(orphans, name)
		}
	}
	return orphans
}
