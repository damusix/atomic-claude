package doctor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/signals"
)

// makeSignalsFile sets root up with a fresh, self-consistent signals state and
// ages it to the given mtime. It writes a minimal docs/wiki/index.md router and
// a wired claude.local.md (so router checks PASS), then runs a real signals.Scan
// so the docs/wiki/scan.md body matches what a re-scan would produce — the
// content-based staleness check therefore sees it as fresh, and freshness tests
// exercise the age logic rather than tripping a stub-vs-scan mismatch. Tests
// that want the stale (ErrStale) path add a source file after calling this.
func makeSignalsFile(t *testing.T, root string, mtime time.Time) {
	t.Helper()
	wikiDir := filepath.Join(root, "docs", "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}

	// Router + @-ref, written before the scan so the scanned tree is stable
	// (docs/wiki/ is excluded from the body; claude.local.md is counted).
	if err := os.WriteFile(filepath.Join(wikiDir, "index.md"), []byte("# Project wiki\n"), 0o644); err != nil {
		t.Fatalf("write index.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "claude.local.md"), []byte("@docs/wiki/index.md\n"), 0o644); err != nil {
		t.Fatalf("write claude.local.md: %v", err)
	}

	if err := signals.Scan(root); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// Age every file the check inspects.
	for _, p := range []string{
		filepath.Join(wikiDir, "scan.md"),
		filepath.Join(wikiDir, "index.md"),
		filepath.Join(root, "claude.local.md"),
	} {
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatalf("chtimes %s: %v", p, err)
		}
	}
}

// TestCheckSignalsMissingFile verifies WARN when signals file does not exist.
func TestCheckSignalsMissingFile(t *testing.T) {
	root := t.TempDir()
	r := doctor.RunCheckSignalsWith(root, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN", r.Severity)
	}
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

// TestCheckSignalsFreshFile verifies PASS when signals file is fresh (below threshold).
func TestCheckSignalsFreshFile(t *testing.T) {
	root := t.TempDir()
	// mtime = 3 days ago, threshold = 7
	mtime := time.Now().Add(-3 * 24 * time.Hour)
	makeSignalsFile(t, root, mtime)
	r := doctor.RunCheckSignalsWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (detail: %s)", r.Severity, r.Detail)
	}
}

// TestCheckSignalsStaleByAge verifies WARN when signals file is older than --stale-days.
func TestCheckSignalsStaleByAge(t *testing.T) {
	root := t.TempDir()
	// mtime = 10 days ago, threshold = 7
	mtime := time.Now().Add(-10 * 24 * time.Hour)
	makeSignalsFile(t, root, mtime)
	r := doctor.RunCheckSignalsWith(root, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN", r.Severity)
	}
	// Detail must mention the age in days and threshold.
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

// TestCheckSignalsStaleDaysOverride verifies --stale-days is respected.
// A file 3 days old is PASS at threshold=7 but WARN at threshold=2.
func TestCheckSignalsStaleDaysOverride(t *testing.T) {
	root := t.TempDir()
	mtime := time.Now().Add(-3 * 24 * time.Hour)
	makeSignalsFile(t, root, mtime)

	r7 := doctor.RunCheckSignalsWith(root, 7)
	if r7.Severity != doctor.PASS {
		t.Errorf("at threshold=7: severity = %v, want PASS", r7.Severity)
	}

	r2 := doctor.RunCheckSignalsWith(root, 2)
	if r2.Severity != doctor.WARN {
		t.Errorf("at threshold=2: severity = %v, want WARN", r2.Severity)
	}
}

// TestCheckSignalsSourceNewerThanFile verifies WARN when a source file was
// modified after the signals file (ErrStale path).
// We create the signals file first, then touch a new source file after.
func TestCheckSignalsSourceNewerThanFile(t *testing.T) {
	root := t.TempDir()
	// Signals file mtime = 2 days ago (fresh by age).
	signalsMtime := time.Now().Add(-2 * 24 * time.Hour)
	makeSignalsFile(t, root, signalsMtime)

	// Create a source file (e.g., CLAUDE.md) with current mtime (newer than signals).
	srcPath := filepath.Join(root, "CLAUDE.md")
	if err := os.WriteFile(srcPath, []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	// Mtime is already now, which is newer than signalsMtime.

	r := doctor.RunCheckSignalsWith(root, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (ErrStale path)", r.Severity)
	}
}

// --- router-specific checks ---

// makeWikiDir ensures docs/wiki/ exists under root.
func makeWikiDir(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "docs", "wiki"), 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
}

// makeRouterFile writes docs/wiki/index.md with the given content.
func makeRouterFile(t *testing.T, root, content string) {
	t.Helper()
	makeWikiDir(t, root)
	path := filepath.Join(root, "docs", "wiki", "index.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write router file: %v", err)
	}
}

// makeClaudeMd writes the named CLAUDE.md-family file with the given content.
func makeClaudeMd(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// makeDomainFile creates a domain file under docs/wiki/ relative to root.
func makeDomainFile(t *testing.T, root, relPath string) {
	t.Helper()
	full := filepath.Join(root, "docs", "wiki", relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(full, []byte("# domain\n"), 0o644); err != nil {
		t.Fatalf("write domain file: %v", err)
	}
}

// routerWithDomains builds a docs/wiki/index.md body with a Domains table
// referencing the supplied domain file paths (bare filenames) in the Detail column.
func routerWithDomains(details ...string) string {
	header := "# Project wiki\n\n## Domains\n\n| Domain | Repo paths | One-liner | Detail |\n|--------|------------|-----------|--------|\n"
	rows := ""
	for i, d := range details {
		rows += fmt.Sprintf("| domain%d | src/%d/ | desc | %s |\n", i, i, d)
	}
	return header + rows
}

// TestCheckSignalsRouterMissing verifies WARN (not FAIL) when signals.md absent
// (pre-migration state — old flat files still valid).
func TestCheckSignalsRouterMissing(t *testing.T) {
	root := t.TempDir()
	// No signals.md

	r := doctor.RunCheckSignalsWith(root, 7)
	// Pre-migration: WARN only, not FAIL
	if r.Severity == doctor.FAIL {
		t.Errorf("severity = FAIL, want PASS or WARN when router absent (pre-migration)")
	}
}

// TestCheckSignalsRouterNoRef verifies WARN when docs/wiki/index.md exists but is not @-ref'd.
func TestCheckSignalsRouterNoRef(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithDomains())
	// No CLAUDE.md-family file references docs/wiki/index.md

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (router not @-ref'd)", r.Severity)
	}
	if !strings.Contains(r.Detail, "not @-ref") {
		t.Errorf("detail %q should mention @-ref", r.Detail)
	}
}

// TestCheckSignalsRouterRefWired verifies PASS when docs/wiki/index.md is @-ref'd and no domain files.
func TestCheckSignalsRouterRefWired(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithDomains())
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (router ref wired, no domains)", r.Severity)
	}
}

// TestCheckSignalsRouterDomainFileMissing verifies WARN when a domain file
// referenced in the router table does not exist on disk.
func TestCheckSignalsRouterDomainFileMissing(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithDomains("auth.md"))
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")
	// auth.md NOT created in docs/wiki/

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (domain file missing)", r.Severity)
	}
	if !strings.Contains(r.Detail, "auth.md") {
		t.Errorf("detail %q should name the missing domain file", r.Detail)
	}
}

// TestCheckSignalsRouterDomainFilesPresent verifies PASS when all domain files
// referenced in the router table exist on disk.
func TestCheckSignalsRouterDomainFilesPresent(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithDomains("auth.md", "billing.md"))
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")
	makeDomainFile(t, root, "auth.md")
	makeDomainFile(t, root, "billing.md")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (all domain files present)", r.Severity)
	}
}

// TestCheckSignalsRouterOrphanDomainFile verifies WARN when a domain file exists
// under docs/wiki/ but is not referenced in the router table.
func TestCheckSignalsRouterOrphanDomainFile(t *testing.T) {
	root := t.TempDir()
	// Router references auth.md only
	makeRouterFile(t, root, routerWithDomains("auth.md"))
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")
	makeDomainFile(t, root, "auth.md")
	// orphan: stale.md exists on disk but not in router table
	makeDomainFile(t, root, "stale.md")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (orphan domain file)", r.Severity)
	}
	if !strings.Contains(r.Detail, "stale.md") {
		t.Errorf("detail %q should name the orphan file", r.Detail)
	}
}

// TestCheckSignalsRouterEmptyDetailColumn verifies PASS when Detail column is
// empty (small repo, all content in router — no domain files needed).
func TestCheckSignalsRouterEmptyDetailColumn(t *testing.T) {
	root := t.TempDir()
	// Detail column intentionally empty for all rows
	content := "# Project wiki\n\n## Domains\n\n| Domain | Repo paths | One-liner | Detail |\n|--------|------------|-----------|--------|\n| auth | src/auth/ | JWT | |\n"
	makeRouterFile(t, root, content)
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (no domain files configured)", r.Severity)
	}
}

// TestCheckSignalsRouterFlatDomainFile verifies that a flat domain file under
// docs/wiki/ (the new layout) is accepted when referenced in the router table.
func TestCheckSignalsRouterFlatDomainFile(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithDomains("auth.md"))
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")
	makeDomainFile(t, root, "auth.md")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (flat domain file exists)", r.Severity)
	}
}

// routerWithLinkifiedDomains builds a docs/wiki/index.md body where the Detail
// column contains linkified markdown links: [`docs/wiki/x.md`](x.md)
// This is the form emitted after `atomic signals linkify` runs.
func routerWithLinkifiedDomains(details ...string) string {
	header := "# Project wiki\n\n## Domains\n\n| Domain | Repo paths | One-liner | Detail |\n|--------|------------|-----------|--------|\n"
	rows := ""
	for i, d := range details {
		// Simulate a linkified Detail cell: [`docs/wiki/x.md`](x.md)
		linked := fmt.Sprintf("[`docs/wiki/%s`](%s)", d, d)
		rows += fmt.Sprintf("| domain%d | src/%d/ | desc | %s |\n", i, i, linked)
	}
	return header + rows
}

// TestCheckSignalsRouterLinkifiedDetailPresent verifies PASS when Detail column
// contains linkified markdown links ([`docs/wiki/x.md`](x.md)) and the domain
// files exist on disk. Exercises the link-extraction path in parseRouterDomains.
func TestCheckSignalsRouterLinkifiedDetailPresent(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithLinkifiedDomains("auth.md", "billing.md"))
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")
	makeDomainFile(t, root, "auth.md")
	makeDomainFile(t, root, "billing.md")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (linkified detail, all files present)", r.Severity)
	}
}

// TestCheckSignalsRouterLinkifiedDetailMissing verifies WARN when Detail column
// contains a linkified link but the domain file is missing.
func TestCheckSignalsRouterLinkifiedDetailMissing(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithLinkifiedDomains("auth.md"))
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")
	// auth.md NOT created in docs/wiki/

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (linkified detail, file missing)", r.Severity)
	}
	if !strings.Contains(r.Detail, "auth.md") {
		t.Errorf("detail %q should name the missing domain file", r.Detail)
	}
}

// TestCheckSignalsRouterLinkifiedDetailChain verifies the full resolution chain:
// linkified Detail cell [`docs/wiki/auth.md`](auth.md) → extract target
// "auth.md" → join root/docs/wiki/auth.md → exists check.
func TestCheckSignalsRouterLinkifiedDetailChain(t *testing.T) {
	root := t.TempDir()
	// The Detail column as emitted by the linkifier: [`docs/wiki/auth.md`](auth.md)
	content := "# Project wiki\n\n## Domains\n\n| Domain | Repo paths | One-liner | Detail |\n|--------|------------|-----------|--------|\n" +
		"| auth | src/auth/ | JWT | [`docs/wiki/auth.md`](auth.md) |\n"
	makeRouterFile(t, root, content)
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")
	makeDomainFile(t, root, "auth.md")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (linkified chain, file present): detail=%s", r.Severity, r.Detail)
	}
}

// TestCheckSignalsRouterNewLayout_Pass verifies PASS when docs/wiki/index.md
// exists and @docs/wiki/index.md is wired (new layout, CP2).
func TestCheckSignalsRouterNewLayout_Pass(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "docs", "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wikiDir, "index.md"), []byte("# Project wiki\n"), 0o644); err != nil {
		t.Fatalf("write index.md: %v", err)
	}
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (new layout docs/wiki/index.md); detail: %s", r.Severity, r.Detail)
	}
}

// routerWithIntroParagraph builds a docs/wiki/index.md body that mirrors the
// real router structure: ## Domains heading, an intro paragraph, a blank line,
// the table, then a ## Cross-cutting heading that follows.
// The Detail column contains linkified markdown links: [`docs/wiki/x.md`](x.md).
func routerWithIntroParagraph(details ...string) string {
	var b strings.Builder
	b.WriteString("# Project wiki\n\n")
	b.WriteString("## Domains\n\n")
	b.WriteString("Each domain groups ALL files across ALL layers (artifacts + CLI code + docs) for one feature concern. Read a domain file when you're working on that feature end-to-end.\n\n")
	b.WriteString("| Domain | Repo paths | One-liner | Detail |\n")
	b.WriteString("|--------|------------|-----------|--------|\n")
	for i, d := range details {
		linked := fmt.Sprintf("[`docs/wiki/%s`](%s)", d, d)
		b.WriteString(fmt.Sprintf("| domain%d | src/%d/ | desc | %s |\n", i, i, linked))
	}
	b.WriteString("\n## Cross-cutting\n\n")
	b.WriteString("Cross-cutting content here.\n")
	return b.String()
}

// TestCheckSignalsRouterIntroParagraphTolerant verifies that an intro paragraph
// between ## Domains and the table does NOT trip parseRouterDomains into returning
// empty — which would cause every real domain file to be reported as an orphan.
// This is the regression test for the false-positive WARN bug.
func TestCheckSignalsRouterIntroParagraphTolerant(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithIntroParagraph("auth.md", "billing.md"))
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")
	makeDomainFile(t, root, "auth.md")
	makeDomainFile(t, root, "billing.md")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (intro paragraph must not break domain parsing); detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckSignalsRouterPipedOneLiner verifies that domain rows whose One-liner
// column contains unescaped pipes (e.g. "md|code search and [page|system] toggle")
// are correctly parsed: the Detail column (last content column before the trailing
// pipe) must be extracted, NOT a one-liner fragment at the fixed cols[4] position.
// This is the regression test for the false-positive WARN:
//
//	"domain file referenced in router table missing: code search, left nav, middle content [page"
func TestCheckSignalsRouterPipedOneLiner(t *testing.T) {
	root := t.TempDir()
	// Build a router row that mirrors the real serve domain:
	// the One-liner cell contains two raw unescaped pipes ("|").
	content := "# Project wiki\n\n## Domains\n\n" +
		"| Domain | Repo paths | One-liner | Detail |\n" +
		"|--------|------------|-----------|--------|\n" +
		"| serve | docs/wiki/serve.md | shell with md|code search and [page|system] toggle | [`docs/wiki/serve.md`](serve.md) |\n"
	makeRouterFile(t, root, content)
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")
	makeDomainFile(t, root, "serve.md")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (piped one-liner must not confuse Detail extraction); detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckSignalsOrphanExclusion verifies that index.md, scan.md, and
// CLAUDE.md inside docs/wiki/ are never reported as orphan domain files,
// even when they are not listed in the router table (new layout, CP2).
func TestCheckSignalsOrphanExclusion(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "docs", "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}

	// Router with no domain rows.
	routerContent := "# Project wiki\n\n## Domains\n\n| Domain | Repo paths | One-liner | Detail |\n|--------|------------|-----------|--------|\n"
	if err := os.WriteFile(filepath.Join(wikiDir, "index.md"), []byte(routerContent), 0o644); err != nil {
		t.Fatalf("write index.md: %v", err)
	}
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")

	// Write the excluded files — must NOT be flagged as orphans.
	for _, name := range []string{"scan.md", "CLAUDE.md"} {
		if err := os.WriteFile(filepath.Join(wikiDir, name), []byte("# excluded\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (scan.md and CLAUDE.md must not be orphans); detail: %s", r.Severity, r.Detail)
	}
}
