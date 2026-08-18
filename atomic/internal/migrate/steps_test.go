package migrate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/migrate"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

// topRegistryVersion is the highest TargetVersion registered. Computed rather
// than hardcoded because these tests assert Run's contract — it returns the
// highest version it applied — not the version of any one step. A literal here
// breaks every time a migration is added, which says nothing about the step
// under test.
func topRegistryVersion(t *testing.T) string {
	t.Helper()
	top := "0.0.0"
	for _, m := range migrate.Registry {
		if selfupdate.CompareSemver(m.TargetVersion, top) > 0 {
			top = m.TargetVersion
		}
	}
	return top
}

// makeOldLayout writes the pre-migration tree: .claude/project/{signals.md,
// signals/domain.md, deterministic-signals.md} plus a CLAUDE.md carrying the
// old @-ref.
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

func TestRelocateSignalsNoOpWhenNewExists(t *testing.T) {
	root := t.TempDir()
	makeOldLayout(t, root)

	newIndexDir := filepath.Join(root, "docs", "wiki")
	if err := os.MkdirAll(newIndexDir, 0o755); err != nil {
		t.Fatalf("mkdir docs/wiki: %v", err)
	}
	existingContent := "# already migrated\n<wiki-type>repo</wiki-type>\n"
	if err := os.WriteFile(filepath.Join(newIndexDir, "index.md"), []byte(existingContent), 0o644); err != nil {
		t.Fatalf("write new index: %v", err)
	}

	ctx := &migrate.Context{Root: root}
	_, err := migrate.Run("", migrate.Registry, ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "docs", "wiki", "index.md"))
	if err != nil {
		t.Fatalf("read new index: %v", err)
	}
	if string(data) != existingContent {
		t.Errorf("new index was modified: got %q, want %q", string(data), existingContent)
	}

	if _, err := os.Stat(filepath.Join(root, ".claude", "project", "signals.md")); err != nil {
		t.Errorf("old signals.md should still exist: %v", err)
	}
}

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

	mkfile("docs/wiki/index.md", "# signals router\nsome signals content\n")
	mkfile(".claude/project/signals/domain.md", "# domain\nsome domain content\n")
	mkfile("CLAUDE.md", "some intro\n@.claude/project/signals.md\nmore content\n")
	// signals.md is deliberately absent: the prior run already moved it.

	ctx := &migrate.Context{Root: root}
	_, err := migrate.Run("", migrate.Registry, ctx)
	if err != nil {
		t.Fatalf("Run (resume from partial): %v", err)
	}

	indexData, err := os.ReadFile(filepath.Join(root, "docs", "wiki", "index.md"))
	if err != nil {
		t.Fatalf("read index.md after recovery: %v", err)
	}
	if !strings.Contains(string(indexData), "<wiki-type>") {
		t.Errorf("index.md missing <wiki-type> after recovery:\n%s", indexData)
	}

	if _, err := os.Stat(filepath.Join(root, "docs", "wiki", "domain.md")); err != nil {
		t.Errorf("docs/wiki/domain.md should exist after recovery: %v", err)
	}

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

	mkfile(".claude/project/signals.md", "# signals router\n")
	mkfile(".claude/project/signals/signals.md", "# domain signals\n")
	mkfile("CLAUDE.md", "@.claude/project/signals.md\n")

	const priorContent = "pre-existing unrelated content\n"
	mkfile("docs/wiki/signals.md", priorContent)

	ctx := &migrate.Context{Root: root}
	_, err := migrate.Run("", migrate.Registry, ctx)
	if err == nil {
		t.Fatal("Run: expected error due to clobber refusal, got nil")
	}

	got, readErr := os.ReadFile(filepath.Join(root, "docs", "wiki", "signals.md"))
	if readErr != nil {
		t.Fatalf("read docs/wiki/signals.md: %v", readErr)
	}
	if string(got) != priorContent {
		t.Errorf("docs/wiki/signals.md was modified:\ngot:  %q\nwant: %q", string(got), priorContent)
	}
}

func TestRelocateSignalsNoOpWhenNoOldLayout(t *testing.T) {
	root := t.TempDir()

	ctx := &migrate.Context{Root: root}
	_, err := migrate.Run("", migrate.Registry, ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "docs", "wiki", "index.md")); err == nil {
		t.Error("docs/wiki/index.md should not exist for a no-signals repo")
	}
}

func TestRelocateSignalsMigratesOldLayout(t *testing.T) {
	root := t.TempDir()
	makeOldLayout(t, root)

	ctx := &migrate.Context{Root: root}
	newVer, err := migrate.Run("", migrate.Registry, ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := topRegistryVersion(t); newVer != want {
		t.Errorf("returned version: got %q, want %q", newVer, want)
	}

	indexData, err := os.ReadFile(filepath.Join(root, "docs", "wiki", "index.md"))
	if err != nil {
		t.Fatalf("read new index.md: %v", err)
	}
	content := string(indexData)

	for _, block := range []string{"<wiki-type>repo</wiki-type>", "<scan-sha>", "<wiki-schema>1</wiki-schema>"} {
		if !strings.Contains(content, block) {
			t.Errorf("index.md missing %q\ncontent:\n%s", block, content)
		}
	}
	if !strings.Contains(content, "type: Index") {
		t.Errorf("index.md missing 'type: Index'\ncontent:\n%s", content)
	}
	if !strings.Contains(content, "signals router") {
		t.Errorf("index.md missing original content\ncontent:\n%s", content)
	}

	domainData, err := os.ReadFile(filepath.Join(root, "docs", "wiki", "domain.md"))
	if err != nil {
		t.Fatalf("read domain.md: %v", err)
	}
	domainStr := string(domainData)
	if !strings.Contains(domainStr, "type: Domain") {
		t.Errorf("domain.md missing 'type: Domain'\ncontent:\n%s", domainStr)
	}
	if !strings.Contains(domainStr, "domain content") {
		t.Errorf("domain.md missing original content\ncontent:\n%s", domainStr)
	}

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

	if _, err := os.Stat(filepath.Join(root, ".claude", "project", "signals.md")); err == nil {
		t.Error("old signals.md should have been moved")
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "project", "deterministic-signals.md")); err == nil {
		t.Error("old deterministic-signals.md should have been moved")
	}

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

func TestRelocateSignalsIdempotent(t *testing.T) {
	root := t.TempDir()
	makeOldLayout(t, root)

	ctx := &migrate.Context{Root: root}

	if _, err := migrate.Run("", migrate.Registry, ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	indexPath := filepath.Join(root, "docs", "wiki", "index.md")
	sentinel := "<wiki-schema>1</wiki-schema>\nsentinel content\n"
	if err := os.WriteFile(indexPath, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	newVer, err := migrate.Run("1.0.0", migrate.Registry, ctx)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if want := topRegistryVersion(t); newVer != want {
		t.Errorf("idempotent re-run changed version: got %q, want %q", newVer, want)
	}
	data, _ := os.ReadFile(indexPath)
	if string(data) != sentinel {
		t.Errorf("index.md was modified on idempotent re-run:\n%s", data)
	}
}

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
