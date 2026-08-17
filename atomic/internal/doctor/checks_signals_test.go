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

// makeSignalsFile leaves root in a self-consistent signals state aged to
// mtime. It runs a real signals.Scan so the stored body matches what a re-scan
// would produce; otherwise the content check would fire and age tests would
// pass for the wrong reason. Callers wanting the ErrStale path add a source
// file afterwards.
func makeSignalsFile(t *testing.T, root string, mtime time.Time) {
	t.Helper()
	wikiDir := filepath.Join(root, "docs", "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}

	// Written before the scan so the scanned tree is stable: docs/wiki/ is
	// excluded from the body, claude.local.md is counted.
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

func TestCheckSignalsFreshFile(t *testing.T) {
	root := t.TempDir()
	mtime := time.Now().Add(-3 * 24 * time.Hour)
	makeSignalsFile(t, root, mtime)
	r := doctor.RunCheckSignalsWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (detail: %s)", r.Severity, r.Detail)
	}
}

func TestCheckSignalsStaleByAge(t *testing.T) {
	root := t.TempDir()
	mtime := time.Now().Add(-10 * 24 * time.Hour)
	makeSignalsFile(t, root, mtime)
	r := doctor.RunCheckSignalsWith(root, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN", r.Severity)
	}
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

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

// The signals file is fresh by age here, so only the source-newer rule can
// produce the WARN.
func TestCheckSignalsSourceNewerThanFile(t *testing.T) {
	root := t.TempDir()
	signalsMtime := time.Now().Add(-2 * 24 * time.Hour)
	makeSignalsFile(t, root, signalsMtime)

	// Written now, so its mtime is already newer than signalsMtime.
	srcPath := filepath.Join(root, "CLAUDE.md")
	if err := os.WriteFile(srcPath, []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	r := doctor.RunCheckSignalsWith(root, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (ErrStale path)", r.Severity)
	}
}

// --- router-specific checks ---

func makeWikiDir(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "docs", "wiki"), 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
}

func makeRouterFile(t *testing.T, root, content string) {
	t.Helper()
	makeWikiDir(t, root)
	path := filepath.Join(root, "docs", "wiki", "index.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write router file: %v", err)
	}
}

func makeClaudeMd(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

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

// routerWithDomains puts each bare filename in a Domains-table Detail cell.
func routerWithDomains(details ...string) string {
	header := "# Project wiki\n\n## Domains\n\n| Domain | Repo paths | One-liner | Detail |\n|--------|------------|-----------|--------|\n"
	rows := ""
	for i, d := range details {
		rows += fmt.Sprintf("| domain%d | src/%d/ | desc | %s |\n", i, i, d)
	}
	return header + rows
}

func TestCheckSignalsRouterMissing(t *testing.T) {
	root := t.TempDir()

	r := doctor.RunCheckSignalsWith(root, 7)
	if r.Severity == doctor.FAIL {
		t.Errorf("severity = FAIL, want PASS or WARN when router absent (pre-migration)")
	}
}

func TestCheckSignalsRouterNoRef(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithDomains())

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (router not @-ref'd)", r.Severity)
	}
	if !strings.Contains(r.Detail, "not @-ref") {
		t.Errorf("detail %q should mention @-ref", r.Detail)
	}
}

func TestCheckSignalsRouterRefWired(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithDomains())
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (router ref wired, no domains)", r.Severity)
	}
}

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

func TestCheckSignalsRouterOrphanDomainFile(t *testing.T) {
	root := t.TempDir()
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

func TestCheckSignalsRouterEmptyDetailColumn(t *testing.T) {
	root := t.TempDir()
	content := "# Project wiki\n\n## Domains\n\n| Domain | Repo paths | One-liner | Detail |\n|--------|------------|-----------|--------|\n| auth | src/auth/ | JWT | |\n"
	makeRouterFile(t, root, content)
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (no domain files configured)", r.Severity)
	}
}

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

// routerWithLinkifiedDomains writes Detail cells in the linkified form
// `atomic signals linkify` emits: [`docs/wiki/x.md`](x.md).
func routerWithLinkifiedDomains(details ...string) string {
	header := "# Project wiki\n\n## Domains\n\n| Domain | Repo paths | One-liner | Detail |\n|--------|------------|-----------|--------|\n"
	rows := ""
	for i, d := range details {
		linked := fmt.Sprintf("[`docs/wiki/%s`](%s)", d, d)
		rows += fmt.Sprintf("| domain%d | src/%d/ | desc | %s |\n", i, i, linked)
	}
	return header + rows
}

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

func TestCheckSignalsRouterLinkifiedDetailMissing(t *testing.T) {
	root := t.TempDir()
	makeRouterFile(t, root, routerWithLinkifiedDomains("auth.md"))
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")

	r := doctor.RunCheckRouterWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (linkified detail, file missing)", r.Severity)
	}
	if !strings.Contains(r.Detail, "auth.md") {
		t.Errorf("detail %q should name the missing domain file", r.Detail)
	}
}

func TestCheckSignalsRouterLinkifiedDetailChain(t *testing.T) {
	root := t.TempDir()
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

// routerWithIntroParagraph mirrors the real router shape: heading, intro
// paragraph, blank line, table, then the next heading.
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

// An intro paragraph between the heading and the table must not make
// parseRouterDomains return empty, which would orphan every domain file.
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

// An unescaped pipe inside the One-liner cell shifts the column count, so
// Detail has to be read as the last content column, not a fixed index.
func TestCheckSignalsRouterPipedOneLiner(t *testing.T) {
	root := t.TempDir()
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

// index.md, scan.md, and CLAUDE.md are wiki infrastructure, not domain files,
// so they never belong in the router table and never count as orphans.
func TestCheckSignalsOrphanExclusion(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "docs", "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}

	routerContent := "# Project wiki\n\n## Domains\n\n| Domain | Repo paths | One-liner | Detail |\n|--------|------------|-----------|--------|\n"
	if err := os.WriteFile(filepath.Join(wikiDir, "index.md"), []byte(routerContent), 0o644); err != nil {
		t.Fatalf("write index.md: %v", err)
	}
	makeClaudeMd(t, root, "claude.local.md", "@docs/wiki/index.md\n")

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
