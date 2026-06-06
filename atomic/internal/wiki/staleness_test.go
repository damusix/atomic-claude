package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingRunner records every invocation so tests can assert it was never called.
type recordingRunner struct {
	mu    sync.Mutex
	calls [][]string
}

func (r *recordingRunner) Run(name string, args ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := append([]string{name}, args...)
	r.calls = append(r.calls, all)
	return nil
}

func (r *recordingRunner) CallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// makeWikiCLAUDEMD writes a CLAUDE.md with a <wikis> block listing indexPath.
func makeWikiCLAUDEMD(t *testing.T, dir, indexPath string) string {
	t.Helper()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	content := fmt.Sprintf("<wikis>\n- %s\n</wikis>\n", indexPath)
	if err := os.WriteFile(claudeMD, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return claudeMD
}

// makeIndexWithGenerated writes a wiki/index.md with a <wiki-scan> block whose
// generated attribute is set to the given date string.
func makeIndexWithGenerated(t *testing.T, wikiDir, generatedDate string) string {
	t.Helper()
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(wikiDir, "index.md")
	content := fmt.Sprintf("<wiki-scan root=%q generated=%q>\n</wiki-scan>\n", wikiDir, generatedDate)
	if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return indexPath
}

// TestCheckStaleness_NudgeOnStaleGenerated verifies that a wiki whose generated
// date is older than the threshold emits exactly one nudge line.
func TestCheckStaleness_NudgeOnStaleGenerated(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}

	wikiDir := filepath.Join(tmp, "mywiki", "wiki")
	// generated 40 days ago
	generatedDate := "2000-01-01"
	indexPath := makeIndexWithGenerated(t, wikiDir, generatedDate)
	makeWikiCLAUDEMD(t, claudeHome, indexPath)

	runner := &recordingRunner{}
	// clock returns a time well past the 30-day default threshold
	fixedNow := time.Date(2000, 2, 20, 0, 0, 0, 0, time.UTC) // ~50 days after generated
	nudges, err := CheckStaleness(claudeHome, 30, runner.Run, func() time.Time { return fixedNow })

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nudges) != 1 {
		t.Fatalf("expected 1 nudge, got %d: %v", len(nudges), nudges)
	}
	if runner.CallCount() != 0 {
		t.Fatalf("CheckStaleness must spawn zero git calls, got %d", runner.CallCount())
	}
}

// TestCheckStaleness_NudgeOnDirtyMarkerEvenIfFresh verifies that a wiki with a
// .dirty marker emits a nudge even when the generated date is fresh.
func TestCheckStaleness_NudgeOnDirtyMarkerEvenIfFresh(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}

	wikiDir := filepath.Join(tmp, "mywiki", "wiki")
	fixedNow := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	// generated yesterday — fresh within 30 days
	generatedDate := fixedNow.AddDate(0, 0, -1).Format("2006-01-02")
	indexPath := makeIndexWithGenerated(t, wikiDir, generatedDate)
	makeWikiCLAUDEMD(t, claudeHome, indexPath)

	// Write the .dirty marker
	dirtyPath := filepath.Join(wikiDir, ".dirty")
	if err := os.WriteFile(dirtyPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &recordingRunner{}
	nudges, err := CheckStaleness(claudeHome, 30, runner.Run, func() time.Time { return fixedNow })

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nudges) != 1 {
		t.Fatalf("expected 1 nudge (dirty marker), got %d: %v", len(nudges), nudges)
	}
	if runner.CallCount() != 0 {
		t.Fatalf("CheckStaleness must spawn zero git calls, got %d", runner.CallCount())
	}
}

// TestCheckStaleness_SilentWhenFreshAndNoMarker verifies that a fresh wiki with
// no .dirty marker emits no nudge.
func TestCheckStaleness_SilentWhenFreshAndNoMarker(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}

	wikiDir := filepath.Join(tmp, "mywiki", "wiki")
	fixedNow := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	// generated 5 days ago — fresh within 30 days
	generatedDate := fixedNow.AddDate(0, 0, -5).Format("2006-01-02")
	indexPath := makeIndexWithGenerated(t, wikiDir, generatedDate)
	makeWikiCLAUDEMD(t, claudeHome, indexPath)
	// no .dirty marker

	runner := &recordingRunner{}
	nudges, err := CheckStaleness(claudeHome, 30, runner.Run, func() time.Time { return fixedNow })

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nudges) != 0 {
		t.Fatalf("expected 0 nudges (fresh), got %d: %v", len(nudges), nudges)
	}
	if runner.CallCount() != 0 {
		t.Fatalf("CheckStaleness must spawn zero git calls, got %d", runner.CallCount())
	}
}

// TestCheckStaleness_SpawnsNoGit passes a recording runner and asserts zero
// invocations even when a stale wiki triggers the staleness path.
// This test specifically proves CheckStaleness performs file ops only.
func TestCheckStaleness_SpawnsNoGit(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}

	wikiDir := filepath.Join(tmp, "mywiki", "wiki")
	// stale: generated long ago
	indexPath := makeIndexWithGenerated(t, wikiDir, "2000-01-01")
	makeWikiCLAUDEMD(t, claudeHome, indexPath)

	runner := &recordingRunner{}
	fixedNow := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	_, err := CheckStaleness(claudeHome, 30, runner.Run, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.CallCount() != 0 {
		t.Errorf("expected zero runner calls, got %d calls: %v", runner.CallCount(), runner.calls)
	}
}

// TestCheckStaleness_MissingWikisBlock is non-fatal: garbled/absent <wikis>
// returns no error and no nudges.
func TestCheckStaleness_MissingWikisBlock(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	// CLAUDE.md present but has no <wikis> block
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte("# just some content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &recordingRunner{}
	nudges, err := CheckStaleness(claudeHome, 30, runner.Run, time.Now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nudges) != 0 {
		t.Fatalf("expected 0 nudges when block absent, got %d", len(nudges))
	}
}

// TestCheckStaleness_MissingCLAUDEMD is non-fatal: no CLAUDE.md → no error, no nudges.
func TestCheckStaleness_MissingCLAUDEMD(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude") // does not contain CLAUDE.md

	runner := &recordingRunner{}
	nudges, err := CheckStaleness(claudeHome, 30, runner.Run, time.Now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nudges) != 0 {
		t.Fatalf("expected 0 nudges when CLAUDE.md absent, got %d", len(nudges))
	}
}

// TestCheckStaleness_MissingWikiIndex is non-fatal: registered wiki whose index
// file is gone → skipped, no error.
func TestCheckStaleness_MissingWikiIndex(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	// Register a non-existent index
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
	fakeIndex := filepath.Join(tmp, "no-such-wiki", "wiki", "index.md")
	content := fmt.Sprintf("<wikis>\n- %s\n</wikis>\n", fakeIndex)
	if err := os.WriteFile(claudeMD, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &recordingRunner{}
	nudges, err := CheckStaleness(claudeHome, 30, runner.Run, time.Now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nudges) != 0 {
		t.Fatalf("expected 0 nudges when index missing (skip), got %d", len(nudges))
	}
}

// TestCheckStaleness_GarbledGeneratedDate is non-fatal: unreadable generated date
// is treated as stale (safe default → nudge emitted, no error).
func TestCheckStaleness_GarbledGeneratedDate(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}

	wikiDir := filepath.Join(tmp, "mywiki", "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(wikiDir, "index.md")
	// generated date is garbled
	content := "<wiki-scan root=\"/x\" generated=\"not-a-date\">\n</wiki-scan>\n"
	if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	makeWikiCLAUDEMD(t, claudeHome, indexPath)

	runner := &recordingRunner{}
	fixedNow := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	nudges, err := CheckStaleness(claudeHome, 30, runner.Run, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Garbled date → treated as stale → nudge
	if len(nudges) != 1 {
		t.Fatalf("expected 1 nudge (garbled date → stale), got %d: %v", len(nudges), nudges)
	}
}

// --------------------------------------------------------------------------
// mark-dirty tests
// --------------------------------------------------------------------------

// TestMarkDirty_TouchesDirtyWhenCwdUnderRoot verifies that mark-dirty touches
// <root>/wiki/.dirty when cwd is under a registered wiki root.
func TestMarkDirty_TouchesDirtyWhenCwdUnderRoot(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}

	// root is tmp/realm; wiki/ is tmp/realm/wiki/
	root := filepath.Join(tmp, "realm")
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(wikiDir, "index.md")
	if err := os.WriteFile(indexPath, []byte("<wiki-scan root=\"\" generated=\"\">\n</wiki-scan>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeWikiCLAUDEMD(t, claudeHome, indexPath)

	// cwd is a sub-dir of root
	cwd := filepath.Join(root, "some", "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := MarkDirty(claudeHome, cwd); err != nil {
		t.Fatalf("MarkDirty returned error: %v", err)
	}

	dirtyPath := filepath.Join(wikiDir, ".dirty")
	if _, err := os.Lstat(dirtyPath); err != nil {
		t.Fatalf(".dirty not created at %s: %v", dirtyPath, err)
	}
}

// TestMarkDirty_NoopWhenCwdNotUnderAnyRoot verifies that mark-dirty is a no-op
// when cwd is not under any registered wiki root.
func TestMarkDirty_NoopWhenCwdNotUnderAnyRoot(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}

	// Register a wiki under tmp/realm
	root := filepath.Join(tmp, "realm")
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(wikiDir, "index.md")
	if err := os.WriteFile(indexPath, []byte("<wiki-scan root=\"\" generated=\"\">\n</wiki-scan>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeWikiCLAUDEMD(t, claudeHome, indexPath)

	// cwd is somewhere else entirely
	elsewhere := filepath.Join(tmp, "unrelated", "project")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := MarkDirty(claudeHome, elsewhere); err != nil {
		t.Fatalf("MarkDirty returned error: %v", err)
	}

	dirtyPath := filepath.Join(wikiDir, ".dirty")
	if _, err := os.Lstat(dirtyPath); err == nil {
		t.Fatalf(".dirty should NOT be created when cwd is outside all roots")
	}
}

// TestMarkDirty_NormalizedPathMatching verifies that two spellings of the same
// root (trailing slash, double separator) both match correctly.
func TestMarkDirty_NormalizedPathMatching(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(tmp, "realm")
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(wikiDir, "index.md")
	if err := os.WriteFile(indexPath, []byte("<wiki-scan root=\"\" generated=\"\">\n</wiki-scan>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Register with a trailing slash in the path — simulating an oddly-spelled entry
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
	content := fmt.Sprintf("<wikis>\n- %s\n</wikis>\n", indexPath+string(filepath.Separator))
	if err := os.WriteFile(claudeMD, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// cwd is directly inside root
	cwd := filepath.Join(root, "subrepo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := MarkDirty(claudeHome, cwd); err != nil {
		t.Fatalf("MarkDirty returned error: %v", err)
	}

	dirtyPath := filepath.Join(wikiDir, ".dirty")
	if _, err := os.Lstat(dirtyPath); err != nil {
		t.Fatalf(".dirty not created with oddly-spelled index path: %v", err)
	}
}

// TestMarkDirty_SiblingPrefixNotUnder verifies that a cwd whose path shares a
// string prefix with the wiki root but is NOT a child of it does not trigger
// mark-dirty.  This catches a naive strings.HasPrefix regression where
// <tmp>/realm-other/project would incorrectly match root <tmp>/realm.
func TestMarkDirty_SiblingPrefixNotUnder(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}

	// Register a wiki whose root is <tmp>/realm.
	root := filepath.Join(tmp, "realm")
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(wikiDir, "index.md")
	if err := os.WriteFile(indexPath, []byte("<wiki-scan root=\"\" generated=\"\">\n</wiki-scan>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeWikiCLAUDEMD(t, claudeHome, indexPath)

	// cwd is <tmp>/realm-other/project — shares the "realm" string prefix but
	// is a sibling directory, NOT under <tmp>/realm.
	cwd := filepath.Join(tmp, "realm-other", "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := MarkDirty(claudeHome, cwd); err != nil {
		t.Fatalf("MarkDirty returned unexpected error: %v", err)
	}

	dirtyPath := filepath.Join(wikiDir, ".dirty")
	if _, err := os.Lstat(dirtyPath); err == nil {
		t.Fatalf(".dirty must NOT be created for a sibling-prefix cwd (%s is not under %s)", cwd, root)
	}
}

// TestMarkDirty_MissingWikisBlock is a no-op: no error when no <wikis> block.
func TestMarkDirty_MissingWikisBlock(t *testing.T) {
	tmp := t.TempDir()
	claudeHome := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte("# no wikis block\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MarkDirty(claudeHome, tmp); err != nil {
		t.Fatalf("MarkDirty should be no-op when no <wikis> block: %v", err)
	}
}

// TestReadWikiIndexPaths_ReadsAllEntries verifies the reader returns all
// registered paths.
func TestReadWikiIndexPaths_ReadsAllEntries(t *testing.T) {
	tmp := t.TempDir()
	claudeMD := filepath.Join(tmp, "CLAUDE.md")
	p1 := "/home/user/.claude/wiki1/index.md"
	p2 := "/home/user/.claude/wiki2/index.md"
	content := fmt.Sprintf("<wikis>\n- %s\n- %s\n</wikis>\n", p1, p2)
	if err := os.WriteFile(claudeMD, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := ReadWikiIndexPaths(claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d: %v", len(paths), paths)
	}
	if !containsAll(paths, p1, p2) {
		t.Fatalf("paths missing expected entries: %v", paths)
	}
}

// TestReadWikiIndexPaths_EmptyWhenNoBlock returns empty slice (no error) when
// block is absent.
func TestReadWikiIndexPaths_EmptyWhenNoBlock(t *testing.T) {
	tmp := t.TempDir()
	claudeMD := filepath.Join(tmp, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte("# no wikis\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := ReadWikiIndexPaths(claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected empty slice, got %v", paths)
	}
}

// TestReadWikiIndexPaths_FileAbsent returns empty slice (no error) when CLAUDE.md absent.
func TestReadWikiIndexPaths_FileAbsent(t *testing.T) {
	tmp := t.TempDir()
	paths, err := ReadWikiIndexPaths(filepath.Join(tmp, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected empty slice when file absent, got %v", paths)
	}
}

// containsAll returns true if all want values appear in got.
func containsAll(got []string, want ...string) bool {
	set := make(map[string]bool, len(got))
	for _, g := range got {
		set[strings.TrimSpace(g)] = true
	}
	for _, w := range want {
		if !set[strings.TrimSpace(w)] {
			return false
		}
	}
	return true
}
