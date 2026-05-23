package doctor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// makeSignalsFile creates .claude/project/deterministic-signals.md in root
// and sets its mtime to the given time. It also writes a minimal signals.md
// router and a wired claude.local.md (both with the same mtime) so that router
// integrity checks PASS, allowing tests focused on freshness to remain
// unaffected by router WARNs.
func makeSignalsFile(t *testing.T, root string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}

	writeAtMtime := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}

	writeAtMtime(filepath.Join(dir, "deterministic-signals.md"), "# Deterministic signals\n")
	// Write a minimal router so freshness-focused tests aren't downgraded by router WARNs.
	writeAtMtime(filepath.Join(dir, "signals.md"), "# Project signals\n")
	// Wire the @-ref so the router check passes.
	writeAtMtime(filepath.Join(root, "claude.local.md"), "@.claude/project/signals.md\n")
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

// makeProjectDir ensures .claude/project/ exists under root.
func makeProjectDir(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "project"), 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
}

// makeRouterFile writes .claude/project/signals.md with the given content.
func makeRouterFile(t *testing.T, root, content string) {
	t.Helper()
	makeProjectDir(t, root)
	path := filepath.Join(root, ".claude", "project", "signals.md")
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

// makeDomainFile creates a domain file under .claude/project/ relative to root.
func makeDomainFile(t *testing.T, root, relPath string) {
	t.Helper()
	full := filepath.Join(root, ".claude", "project", relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(full, []byte("# domain\n"), 0o644); err != nil {
		t.Fatalf("write domain file: %v", err)
	}
}

// routerWithDomains builds a signals.md body with a Domains table referencing
// the supplied domain file paths in the Detail column.
func routerWithDomains(details ...string) string {
	header := "# Project signals\n\n## Domains\n\n| Domain | Repo paths | One-liner | Detail |\n|--------|------------|-----------|--------|\n"
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

// TestCheckSignalsRouterNoRef verifies WARN when signals.md exists but is not @-ref'd.
func TestCheckSignalsRouterNoRef(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithDomains())
	// No CLAUDE.md-family file references signals.md

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (router not @-ref'd)", r.Severity)
	}
	if !strings.Contains(r.Detail, "not @-ref") {
		t.Errorf("detail %q should mention @-ref", r.Detail)
	}
}

// TestCheckSignalsRouterRefWired verifies PASS when signals.md is @-ref'd and no domain files.
func TestCheckSignalsRouterRefWired(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithDomains())
	makeClaudeMd(t, root, "claude.local.md", "@.claude/project/signals.md\n")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (router ref wired, no domains)", r.Severity)
	}
}

// TestCheckSignalsRouterDomainFileMissing verifies WARN when a domain file
// referenced in the router table does not exist on disk.
func TestCheckSignalsRouterDomainFileMissing(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithDomains("signals/auth.md"))
	makeClaudeMd(t, root, "claude.local.md", "@.claude/project/signals.md\n")
	// signals/auth.md NOT created

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (domain file missing)", r.Severity)
	}
	if !strings.Contains(r.Detail, "signals/auth.md") {
		t.Errorf("detail %q should name the missing domain file", r.Detail)
	}
}

// TestCheckSignalsRouterDomainFilesPresent verifies PASS when all domain files
// referenced in the router table exist on disk.
func TestCheckSignalsRouterDomainFilesPresent(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithDomains("signals/auth.md", "signals/billing.md"))
	makeClaudeMd(t, root, "claude.local.md", "@.claude/project/signals.md\n")
	makeDomainFile(t, root, "signals/auth.md")
	makeDomainFile(t, root, "signals/billing.md")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (all domain files present)", r.Severity)
	}
}

// TestCheckSignalsRouterOrphanDomainFile verifies WARN when a domain file exists
// under signals/ but is not referenced in the router table.
func TestCheckSignalsRouterOrphanDomainFile(t *testing.T) {
	root := t.TempDir()
	// Router references auth.md only
	makeRouterFile(t, root, routerWithDomains("signals/auth.md"))
	makeClaudeMd(t, root, "claude.local.md", "@.claude/project/signals.md\n")
	makeDomainFile(t, root, "signals/auth.md")
	// orphan: signals/stale.md exists on disk but not in router table
	makeDomainFile(t, root, "signals/stale.md")

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
	content := "# Project signals\n\n## Domains\n\n| Domain | Repo paths | One-liner | Detail |\n|--------|------------|-----------|--------|\n| auth | src/auth/ | JWT | |\n"
	makeRouterFile(t, root, content)
	makeClaudeMd(t, root, "claude.local.md", "@.claude/project/signals.md\n")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (no domain files configured)", r.Severity)
	}
}

// TestCheckSignalsRouterSubdirDomainFile verifies domain files under signals/<domain>/index.md
// (sub-routed domains) are accepted.
func TestCheckSignalsRouterSubdirDomainFile(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithDomains("signals/auth/index.md"))
	makeClaudeMd(t, root, "claude.local.md", "@.claude/project/signals.md\n")
	makeDomainFile(t, root, "signals/auth/index.md")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (sub-routed domain file exists)", r.Severity)
	}
}
