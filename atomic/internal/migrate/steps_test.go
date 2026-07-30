package migrate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/migrate"
)

// makeOldLayout creates an old signals layout under root:
//
//	.claude/project/signals.md          (signals router)
//	.claude/project/signals/domain.md   (domain file)
//	.claude/project/deterministic-signals.md (scan output)
//	CLAUDE.md                           (with @-ref)
func makeOldLayout(t *testing.T, root string) {
	t.Helper()
	mkfile := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	mkfile(".claude/project/signals.md", "# signals router\nsome signals content\n")
	mkfile(".claude/project/signals/domain.md", "# domain\nsome domain content\n")
	mkfile(".claude/project/deterministic-signals.md", "deterministic scan output\n")
	mkfile("CLAUDE.md", "some intro\n@.claude/project/signals.md\nmore content\n")
}

// TestRelocateSignalsNoOpWhenNewExists verifies the step is a no-op when
// docs/wiki/index.md already exists and contains the <wiki-type> sentinel
// (fully migrated), leaving old files untouched.
func TestRelocateSignalsNoOpWhenNewExists(t *testing.T) {
	root := t.TempDir()
	makeOldLayout(t, root)

	// Pre-create the new index with the sentinel to simulate a fully-migrated repo.
	newIndexDir := filepath.Join(root, "docs", "wiki")
	if err := os.MkdirAll(newIndexDir, 0o755); err != nil {
		t.Fatalf("mkdir docs/wiki: %v", err)
	}
	// Sentinel must be present for Guard 1 to trigger.
	existingContent := "# already migrated\n<wiki-type>repo</wiki-type>\n"
	if err := os.WriteFile(filepath.Join(newIndexDir, "index.md"), []byte(existingContent), 0o644); err != nil {
		t.Fatalf("write new index: %v", err)
	}

	ctx := &migrate.Context{Root: root}
	_, err := migrate.Run("", migrate.Registry, ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// New index must be untouched (still the "already migrated" content).
	data, err := os.ReadFile(filepath.Join(root, "docs", "wiki", "index.md"))
	if err != nil {
		t.Fatalf("read new index: %v", err)
	}
	if string(data) != existingContent {
		t.Errorf("new index was modified: got %q, want %q", string(data), existingContent)
	}

	// Old signals.md must still be present (not moved, because Guard 1 fired first).
	if _, err := os.Stat(filepath.Join(root, ".claude", "project", "signals.md")); err != nil {
		t.Errorf("old signals.md should still exist: %v", err)
	}
}

// TestRelocateSignalsPartialFailureRecovery verifies that a partial migration
// (docs/wiki/index.md exists without <wiki-type>) is resumed and completed on
// retry rather than being treated as a no-op.
//
// Simulated partial state:
//   - .claude/project/signals.md has been moved to docs/wiki/index.md (no sentinel)
//   - .claude/project/signals/domain.md still present (not yet moved)
//   - CLAUDE.md still has old @-ref (not yet rewired)
func TestRelocateSignalsPartialFailureRecovery(t *testing.T) {
	root := t.TempDir()
	mkfile := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	// Simulate a partial run: index.md moved but sentinel not written,
	// domain file not yet moved, @-ref not yet rewired.
	mkfile("docs/wiki/index.md", "# signals router\nsome signals content\n")
	mkfile(".claude/project/signals/domain.md", "# domain\nsome domain content\n")
	mkfile("CLAUDE.md", "some intro\n@.claude/project/signals.md\nmore content\n")
	// NOTE: .claude/project/signals.md is intentionally absent (was moved on prior run).

	ctx := &migrate.Context{Root: root}
	_, err := migrate.Run("", migrate.Registry, ctx)
	if err != nil {
		t.Fatalf("Run (resume from partial): %v", err)
	}

	// index.md must now have the sentinel.
	indexData, err := os.ReadFile(filepath.Join(root, "docs", "wiki", "index.md"))
	if err != nil {
		t.Fatalf("read index.md after recovery: %v", err)
	}
	if !strings.Contains(string(indexData), "<wiki-type>") {
		t.Errorf("index.md missing <wiki-type> after recovery:\n%s", indexData)
	}

	// Domain file must have been moved.
	if _, err := os.Stat(filepath.Join(root, "docs", "wiki", "domain.md")); err != nil {
		t.Errorf("docs/wiki/domain.md should exist after recovery: %v", err)
	}

	// @-ref must be rewired.
	claudeData, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md after recovery: %v", err)
	}
	if strings.Contains(string(claudeData), "@.claude/project/signals.md") {
		t.Errorf("CLAUDE.md still has old @-ref after recovery:\n%s", claudeData)
	}
	if !strings.Contains(string(claudeData), "@docs/wiki/index.md") {
		t.Errorf("CLAUDE.md missing new @-ref after recovery:\n%s", claudeData)
	}
}

// TestRelocateSignalsOverwriteRefusal verifies that the step returns an error
// and does NOT clobber a pre-existing file in docs/wiki/ when both src and dst
// exist for the same move.
//
// Scenario: docs/wiki/signals.md exists (unrelated, user-created) but
// .claude/project/signals/signals.md also exists and would be moved there.
func TestRelocateSignalsOverwriteRefusal(t *testing.T) {
	root := t.TempDir()
	mkfile := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	// Old layout with a domain file named "signals.md".
	mkfile(".claude/project/signals.md", "# signals router\n")
	mkfile(".claude/project/signals/signals.md", "# domain signals\n")
	mkfile("CLAUDE.md", "@.claude/project/signals.md\n")

	// Pre-existing unrelated file that would be clobbered.
	const priorContent = "pre-existing unrelated content\n"
	mkfile("docs/wiki/signals.md", priorContent)

	ctx := &migrate.Context{Root: root}
	_, err := migrate.Run("", migrate.Registry, ctx)
	if err == nil {
		t.Fatal("Run: expected error due to clobber refusal, got nil")
	}

	// The pre-existing file must not have been overwritten.
	got, readErr := os.ReadFile(filepath.Join(root, "docs", "wiki", "signals.md"))
	if readErr != nil {
		t.Fatalf("read docs/wiki/signals.md: %v", readErr)
	}
	if string(got) != priorContent {
		t.Errorf("docs/wiki/signals.md was modified:\ngot:  %q\nwant: %q", string(got), priorContent)
	}
}

// TestRelocateSignalsNoOpWhenNoOldLayout verifies the step is a no-op when
// neither the old layout nor the new index exists.
func TestRelocateSignalsNoOpWhenNoOldLayout(t *testing.T) {
	root := t.TempDir()
	// No signals layout at all.

	ctx := &migrate.Context{Root: root}
	_, err := migrate.Run("", migrate.Registry, ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// docs/wiki/index.md must NOT have been created.
	if _, err := os.Stat(filepath.Join(root, "docs", "wiki", "index.md")); err == nil {
		t.Error("docs/wiki/index.md should not exist for a no-signals repo")
	}
}

// TestRelocateSignalsMigratesOldLayout is the happy-path: old signals layout
// → docs/wiki/ with control blocks and rewired @-ref.
func TestRelocateSignalsMigratesOldLayout(t *testing.T) {
	root := t.TempDir()
	makeOldLayout(t, root)

	ctx := &migrate.Context{Root: root}
	newVer, err := migrate.Run("", migrate.Registry, ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if newVer != "1.0.0" {
		t.Errorf("returned version: got %q, want %q", newVer, "1.0.0")
	}

	// docs/wiki/index.md must exist.
	indexData, err := os.ReadFile(filepath.Join(root, "docs", "wiki", "index.md"))
	if err != nil {
		t.Fatalf("read new index.md: %v", err)
	}
	content := string(indexData)

	// Control blocks present.
	for _, block := range []string{"<wiki-type>repo</wiki-type>", "<scan-sha>", "<wiki-schema>1</wiki-schema>"} {
		if !strings.Contains(content, block) {
			t.Errorf("index.md missing %q\ncontent:\n%s", block, content)
		}
	}
	// OKF type frontmatter.
	if !strings.Contains(content, "type: Index") {
		t.Errorf("index.md missing 'type: Index'\ncontent:\n%s", content)
	}
	// Original content preserved.
	if !strings.Contains(content, "signals router") {
		t.Errorf("index.md missing original content\ncontent:\n%s", content)
	}

	// Domain file moved to docs/wiki/domain.md.
	domainData, err := os.ReadFile(filepath.Join(root, "docs", "wiki", "domain.md"))
	if err != nil {
		t.Fatalf("read domain.md: %v", err)
	}
	domainStr := string(domainData)
	// OKF Domain frontmatter prepended.
	if !strings.Contains(domainStr, "type: Domain") {
		t.Errorf("domain.md missing 'type: Domain'\ncontent:\n%s", domainStr)
	}
	if !strings.Contains(domainStr, "domain content") {
		t.Errorf("domain.md missing original content\ncontent:\n%s", domainStr)
	}

	// scan.md present (moved from deterministic-signals.md).
	scanData, err := os.ReadFile(filepath.Join(root, "docs", "wiki", "scan.md"))
	if err != nil {
		t.Fatalf("read scan.md: %v", err)
	}
	if !strings.Contains(string(scanData), "deterministic scan output") {
		t.Errorf("scan.md missing original content: %s", scanData)
	}
	// scan.md must NOT have frontmatter (raw machine output).
	if strings.Contains(string(scanData), "type:") {
		t.Errorf("scan.md should have no frontmatter, got: %s", scanData)
	}

	// Old files removed.
	if _, err := os.Stat(filepath.Join(root, ".claude", "project", "signals.md")); err == nil {
		t.Error("old signals.md should have been moved")
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "project", "deterministic-signals.md")); err == nil {
		t.Error("old deterministic-signals.md should have been moved")
	}

	// @-ref rewired in CLAUDE.md.
	claudeData, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	claudeStr := string(claudeData)
	if strings.Contains(claudeStr, "@.claude/project/signals.md") {
		t.Errorf("CLAUDE.md still has old @-ref:\n%s", claudeStr)
	}
	if !strings.Contains(claudeStr, "@docs/wiki/index.md") {
		t.Errorf("CLAUDE.md missing new @-ref:\n%s", claudeStr)
	}
}

// TestRelocateSignalsIdempotent: re-running via migrate.Run after a successful
// migration applies the step only once (schema == target → skip).
func TestRelocateSignalsIdempotent(t *testing.T) {
	root := t.TempDir()
	makeOldLayout(t, root)

	ctx := &migrate.Context{Root: root}

	// First run: migrates.
	if _, err := migrate.Run("", migrate.Registry, ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Mutate index.md to a sentinel so we can detect re-writes.
	indexPath := filepath.Join(root, "docs", "wiki", "index.md")
	sentinel := "<wiki-schema>1</wiki-schema>\nsentinel content\n"
	if err := os.WriteFile(indexPath, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// Re-run with schema "1.0.0" already recorded → step must be skipped.
	newVer, err := migrate.Run("1.0.0", migrate.Registry, ctx)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if newVer != "1.0.0" {
		t.Errorf("idempotent re-run changed version: got %q, want %q", newVer, "1.0.0")
	}
	// Sentinel must be intact (no re-write).
	data, _ := os.ReadFile(indexPath)
	if string(data) != sentinel {
		t.Errorf("index.md was modified on idempotent re-run:\n%s", data)
	}
}

// TestRelocateSignalsRunAppliesOnlyWhenBelowTarget: schema < target runs;
// schema == target skips; schema > target skips.
func TestRelocateSignalsRunAppliesOnlyWhenBelowTarget(t *testing.T) {
	cases := []struct {
		recorded string
		wantRun  bool
	}{
		{"", true},       // floor → step runs
		{"0.0.0", true},  // explicit floor → step runs (but no-op without old layout)
		{"1.0.0", false}, // already at target → skip
		{"2.0.0", false}, // beyond target → skip
	}
	for _, tc := range cases {
		t.Run("recorded="+tc.recorded, func(t *testing.T) {
			root := t.TempDir()
			if tc.wantRun {
				// Give it something to no-op on (no old layout, just check step runs).
				// The step is a no-op but should be called.
			}

			called := 0
			probeRegistry := []migrate.Migration{
				{TargetVersion: "1.0.0", Scope: "repo", Up: func(*migrate.Context) error {
					called++
					return nil
				}},
			}
			ctx := &migrate.Context{Root: root}
			migrate.Run(tc.recorded, probeRegistry, ctx) //nolint:errcheck

			if tc.wantRun && called == 0 {
				t.Errorf("recorded=%q: expected step to run, but it did not", tc.recorded)
			}
			if !tc.wantRun && called > 0 {
				t.Errorf("recorded=%q: expected step to skip, but it ran %d times", tc.recorded, called)
			}
		})
	}
}
