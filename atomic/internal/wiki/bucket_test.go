package wiki_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// ---- helpers ----

// setupBucketDir builds a temporary directory tree to use as a bucket folder.
// Returns the bucket dir path.
//
//	bucketDir/
//	  index.md           — must be excluded from walk
//	  a.md
//	  sub/
//	    b.md
//	  .DS_Store           — OS junk, must be excluded
//	  Thumbs.db           — OS junk, must be excluded
//	  node_modules/       — skip dir, must be excluded
//	  dist/sub/c.md       — inside skip dir, must be excluded
func setupBucketDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeBucketFile(t, filepath.Join(dir, "index.md"), "# index\n")
	writeBucketFile(t, filepath.Join(dir, "a.md"), "content of a\n")
	writeBucketFile(t, filepath.Join(dir, "sub", "b.md"), "content of b\n")
	writeBucketFile(t, filepath.Join(dir, ".DS_Store"), "mac junk")
	writeBucketFile(t, filepath.Join(dir, "Thumbs.db"), "windows junk")
	writeBucketFile(t, filepath.Join(dir, "node_modules", "pkg", "index.js"), "pkg")
	writeBucketFile(t, filepath.Join(dir, "dist", "sub", "c.md"), "built")

	return dir
}

func writeBucketFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// wikiRoot builds a temporary wiki root with a wiki/ subdirectory.
func wikiRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatalf("mkdir wiki: %v", err)
	}
	return root
}

// ---- WalkBucket tests ----

func TestWalkBucket_ContentFilesIncluded(t *testing.T) {
	dir := setupBucketDir(t)

	entries, err := wiki.WalkBucket(dir)
	if err != nil {
		t.Fatalf("WalkBucket: %v", err)
	}

	paths := entryPaths(entries)

	// a.md and sub/b.md must be present
	if !containsPath(paths, "a.md") {
		t.Errorf("a.md not found in entries: %v", paths)
	}
	if !containsPath(paths, "sub/b.md") {
		t.Errorf("sub/b.md not found in entries: %v", paths)
	}
}

func TestWalkBucket_IndexMDExcluded(t *testing.T) {
	dir := setupBucketDir(t)

	entries, err := wiki.WalkBucket(dir)
	if err != nil {
		t.Fatalf("WalkBucket: %v", err)
	}

	paths := entryPaths(entries)
	if containsPath(paths, "index.md") {
		t.Errorf("index.md must be excluded from bucket walk; got: %v", paths)
	}
}

func TestWalkBucket_OSJunkExcluded(t *testing.T) {
	dir := setupBucketDir(t)

	entries, err := wiki.WalkBucket(dir)
	if err != nil {
		t.Fatalf("WalkBucket: %v", err)
	}

	paths := entryPaths(entries)
	if containsPath(paths, ".DS_Store") {
		t.Errorf(".DS_Store must be excluded; got: %v", paths)
	}
	if containsPath(paths, "Thumbs.db") {
		t.Errorf("Thumbs.db must be excluded; got: %v", paths)
	}
}

func TestWalkBucket_SkipDirsExcluded(t *testing.T) {
	dir := setupBucketDir(t)

	entries, err := wiki.WalkBucket(dir)
	if err != nil {
		t.Fatalf("WalkBucket: %v", err)
	}

	paths := entryPaths(entries)
	for _, p := range paths {
		if strings.HasPrefix(p, "node_modules/") || strings.HasPrefix(p, "dist/") {
			t.Errorf("skip-dir content %q must be excluded from walk", p)
		}
	}
}

func TestWalkBucket_SortedOutput(t *testing.T) {
	dir := t.TempDir()
	writeBucketFile(t, filepath.Join(dir, "z.md"), "z")
	writeBucketFile(t, filepath.Join(dir, "a.md"), "a")
	writeBucketFile(t, filepath.Join(dir, "m.md"), "m")

	entries, err := wiki.WalkBucket(dir)
	if err != nil {
		t.Fatalf("WalkBucket: %v", err)
	}

	paths := entryPaths(entries)
	if len(paths) < 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(paths), paths)
	}
	for i := 1; i < len(paths); i++ {
		if paths[i] < paths[i-1] {
			t.Errorf("entries not sorted: %v[%d]=%q > %v[%d]=%q", paths, i-1, paths[i-1], paths, i, paths[i])
		}
	}
}

func TestWalkBucket_HashDeterminism(t *testing.T) {
	dir := t.TempDir()
	writeBucketFile(t, filepath.Join(dir, "a.md"), "same content")

	e1, err := wiki.WalkBucket(dir)
	if err != nil {
		t.Fatalf("WalkBucket: %v", err)
	}
	e2, err := wiki.WalkBucket(dir)
	if err != nil {
		t.Fatalf("WalkBucket second call: %v", err)
	}

	if len(e1) != len(e2) {
		t.Fatalf("result lengths differ: %d vs %d", len(e1), len(e2))
	}
	for i := range e1 {
		if e1[i] != e2[i] {
			t.Errorf("entry %d differs: %q vs %q", i, e1[i], e2[i])
		}
	}
}

// ---- RegisterBucket / manifest directory tests ----

func TestRegisterBucket_CreatesManifestDir(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	if err := wiki.RegisterBucket(wikiDir, "repos"); err != nil {
		t.Fatalf("RegisterBucket: %v", err)
	}

	manifestDir := filepath.Join(wikiDir, ".buckets", "repos")
	if _, err := os.Lstat(manifestDir); err != nil {
		t.Errorf("manifest dir %s not created: %v", manifestDir, err)
	}
}

func TestRegisterBucket_WikiNameRefused(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	err := wiki.RegisterBucket(wikiDir, "wiki")
	if err == nil {
		t.Fatal("expected error for bucket name 'wiki', got nil")
	}
}

func TestRegisterBucket_DoubleRegisterRefused(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	if err := wiki.RegisterBucket(wikiDir, "repos"); err != nil {
		t.Fatalf("first RegisterBucket: %v", err)
	}
	err := wiki.RegisterBucket(wikiDir, "repos")
	if err == nil {
		t.Fatal("expected error for double-register, got nil")
	}
}

// ---- BucketDiff tests ----

func TestBucketDiff_EmptyBaselineAllNew(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	if err := wiki.RegisterBucket(wikiDir, "repos"); err != nil {
		t.Fatalf("RegisterBucket: %v", err)
	}

	bucketDir := t.TempDir()
	writeBucketFile(t, filepath.Join(bucketDir, "a.md"), "content a")
	writeBucketFile(t, filepath.Join(bucketDir, "b.md"), "content b")

	diff, err := wiki.BucketDiff(wikiDir, "repos", bucketDir)
	if err != nil {
		t.Fatalf("BucketDiff: %v", err)
	}

	if len(diff.Added) != 2 {
		t.Errorf("expected 2 added, got %d: %v", len(diff.Added), diff.Added)
	} else {
		if diff.Added[0] != "a.md" {
			t.Errorf("expected Added[0]==%q, got %q", "a.md", diff.Added[0])
		}
		if diff.Added[1] != "b.md" {
			t.Errorf("expected Added[1]==%q, got %q", "b.md", diff.Added[1])
		}
	}
	if len(diff.Changed) != 0 {
		t.Errorf("expected 0 changed, got %d", len(diff.Changed))
	}
	if len(diff.Removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(diff.Removed))
	}
}

func TestBucketDiff_CurrentFileWritten(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	if err := wiki.RegisterBucket(wikiDir, "repos"); err != nil {
		t.Fatalf("RegisterBucket: %v", err)
	}

	bucketDir := t.TempDir()
	writeBucketFile(t, filepath.Join(bucketDir, "a.md"), "content a")
	writeBucketFile(t, filepath.Join(bucketDir, "b.md"), "content b")

	if _, err := wiki.BucketDiff(wikiDir, "repos", bucketDir); err != nil {
		t.Fatalf("BucketDiff: %v", err)
	}

	// Every BucketDiff call must write wiki/.buckets/<name>/current.
	currentPath := filepath.Join(wikiDir, ".buckets", "repos", "current")
	data, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("current file not written: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines in current, got %d: %v", len(lines), lines)
	}

	// Lines are sorted; a.md < b.md.
	for i, wantPrefix := range []string{"a.md\t", "b.md\t"} {
		if !strings.HasPrefix(lines[i], wantPrefix) {
			t.Errorf("current line %d: want prefix %q, got %q", i, wantPrefix, lines[i])
		}
		// Verify the hash field looks like a 64-char hex string.
		parts := strings.SplitN(lines[i], "\t", 2)
		if len(parts) != 2 || len(parts[1]) != 64 {
			t.Errorf("current line %d: malformed entry %q (hash len=%d)", i, lines[i], len(parts[1]))
		}
	}
}

func TestBucketDiff_ChangedFileDetected(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	if err := wiki.RegisterBucket(wikiDir, "repos"); err != nil {
		t.Fatalf("RegisterBucket: %v", err)
	}

	bucketDir := t.TempDir()
	writeBucketFile(t, filepath.Join(bucketDir, "a.md"), "original content")

	// First promote to establish baseline (no prior BucketDiff required).
	if err := wiki.PromoteBucket(wikiDir, "repos", bucketDir); err != nil {
		t.Fatalf("PromoteBucket: %v", err)
	}

	// Modify the file.
	writeBucketFile(t, filepath.Join(bucketDir, "a.md"), "changed content")

	diff, err := wiki.BucketDiff(wikiDir, "repos", bucketDir)
	if err != nil {
		t.Fatalf("second BucketDiff: %v", err)
	}

	if len(diff.Changed) != 1 || diff.Changed[0] != "a.md" {
		t.Errorf("expected a.md in Changed, got: %v", diff.Changed)
	}
	if len(diff.Added) != 0 {
		t.Errorf("expected 0 added, got %d: %v", len(diff.Added), diff.Added)
	}
}

func TestBucketDiff_RemovedFileDetected(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	if err := wiki.RegisterBucket(wikiDir, "repos"); err != nil {
		t.Fatalf("RegisterBucket: %v", err)
	}

	bucketDir := t.TempDir()
	writeBucketFile(t, filepath.Join(bucketDir, "a.md"), "content a")
	writeBucketFile(t, filepath.Join(bucketDir, "b.md"), "content b")

	if _, err := wiki.BucketDiff(wikiDir, "repos", bucketDir); err != nil {
		t.Fatalf("first BucketDiff: %v", err)
	}
	if err := wiki.PromoteBucket(wikiDir, "repos", bucketDir); err != nil {
		t.Fatalf("PromoteBucket: %v", err)
	}

	// Remove b.md.
	if err := os.Remove(filepath.Join(bucketDir, "b.md")); err != nil {
		t.Fatalf("remove b.md: %v", err)
	}

	diff, err := wiki.BucketDiff(wikiDir, "repos", bucketDir)
	if err != nil {
		t.Fatalf("second BucketDiff: %v", err)
	}

	if len(diff.Removed) != 1 || diff.Removed[0] != "b.md" {
		t.Errorf("expected b.md in Removed, got: %v", diff.Removed)
	}
}

func TestBucketDiff_PromoteThenDiffEmpty(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	if err := wiki.RegisterBucket(wikiDir, "repos"); err != nil {
		t.Fatalf("RegisterBucket: %v", err)
	}

	bucketDir := t.TempDir()
	writeBucketFile(t, filepath.Join(bucketDir, "a.md"), "content a")

	// Diff → promote → diff same tree → empty.
	if _, err := wiki.BucketDiff(wikiDir, "repos", bucketDir); err != nil {
		t.Fatalf("first BucketDiff: %v", err)
	}
	if err := wiki.PromoteBucket(wikiDir, "repos", bucketDir); err != nil {
		t.Fatalf("PromoteBucket: %v", err)
	}

	diff, err := wiki.BucketDiff(wikiDir, "repos", bucketDir)
	if err != nil {
		t.Fatalf("second BucketDiff: %v", err)
	}

	if len(diff.Added) != 0 || len(diff.Changed) != 0 || len(diff.Removed) != 0 {
		t.Errorf("expected empty diff after promote, got: added=%v changed=%v removed=%v",
			diff.Added, diff.Changed, diff.Removed)
	}
}

// ---- PromoteBucket tests ----

func TestPromoteBucket_UnregisteredRefused(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	err := wiki.PromoteBucket(wikiDir, "nonexistent", t.TempDir())
	if err == nil {
		t.Fatal("expected error for promote on unregistered bucket, got nil")
	}
}

func TestPromoteBucket_NoPriorDiff(t *testing.T) {
	// PromoteBucket must succeed even when BucketDiff has never been called.
	// It recomputes the walk live, so it does not depend on an on-disk current.
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	if err := wiki.RegisterBucket(wikiDir, "repos"); err != nil {
		t.Fatalf("RegisterBucket: %v", err)
	}

	bucketDir := t.TempDir()
	writeBucketFile(t, filepath.Join(bucketDir, "a.md"), "content a")

	if err := wiki.PromoteBucket(wikiDir, "repos", bucketDir); err != nil {
		t.Fatalf("PromoteBucket without prior BucketDiff: %v", err)
	}

	// baseline must be written.
	baselinePath := filepath.Join(wikiDir, ".buckets", "repos", "baseline")
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("baseline not written: %v", err)
	}
	if !strings.Contains(string(data), "a.md\t") {
		t.Errorf("baseline does not contain a.md entry: %q", string(data))
	}

	// After promote, diff must report empty (in-sync).
	diff, err := wiki.BucketDiff(wikiDir, "repos", bucketDir)
	if err != nil {
		t.Fatalf("BucketDiff after promote: %v", err)
	}
	if len(diff.Added) != 0 || len(diff.Changed) != 0 || len(diff.Removed) != 0 {
		t.Errorf("expected empty diff after promote-only flow, got added=%v changed=%v removed=%v",
			diff.Added, diff.Changed, diff.Removed)
	}
}

func TestPromoteBucket_FirstPromoteSkipsPrevious(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	if err := wiki.RegisterBucket(wikiDir, "repos"); err != nil {
		t.Fatalf("RegisterBucket: %v", err)
	}

	bucketDir := t.TempDir()
	writeBucketFile(t, filepath.Join(bucketDir, "a.md"), "v1")

	// Promote without a prior BucketDiff call — must succeed (spec: recomputes live).
	if err := wiki.PromoteBucket(wikiDir, "repos", bucketDir); err != nil {
		t.Fatalf("PromoteBucket: %v", err)
	}

	manifestDir := filepath.Join(wikiDir, ".buckets", "repos")

	// baseline must exist (fresh walk → baseline)
	if _, err := os.Lstat(filepath.Join(manifestDir, "baseline")); err != nil {
		t.Errorf("baseline missing after first promote: %v", err)
	}
	// previous must NOT exist on first promote
	if _, err := os.Lstat(filepath.Join(manifestDir, "previous")); err == nil {
		t.Error("previous must not exist after first promote")
	}
}

func TestPromoteBucket_SecondPromoteWritesPrevious(t *testing.T) {
	wikiDir := filepath.Join(wikiRoot(t), "wiki")
	if err := wiki.RegisterBucket(wikiDir, "repos"); err != nil {
		t.Fatalf("RegisterBucket: %v", err)
	}

	bucketDir := t.TempDir()
	writeBucketFile(t, filepath.Join(bucketDir, "a.md"), "v1")

	// First promote — no prior BucketDiff needed.
	if err := wiki.PromoteBucket(wikiDir, "repos", bucketDir); err != nil {
		t.Fatalf("first PromoteBucket: %v", err)
	}

	// Update content and second promote.
	writeBucketFile(t, filepath.Join(bucketDir, "a.md"), "v2")
	if err := wiki.PromoteBucket(wikiDir, "repos", bucketDir); err != nil {
		t.Fatalf("second PromoteBucket: %v", err)
	}

	manifestDir := filepath.Join(wikiDir, ".buckets", "repos")

	// previous must exist now
	if _, err := os.Lstat(filepath.Join(manifestDir, "previous")); err != nil {
		t.Errorf("previous missing after second promote: %v", err)
	}
	// baseline must still exist
	if _, err := os.Lstat(filepath.Join(manifestDir, "baseline")); err != nil {
		t.Errorf("baseline missing after second promote: %v", err)
	}
}

// ---- helpers ----

// entryPaths extracts the path portion of WalkBucket entries (tab-separated "<path>\t<sha256hex>").
func entryPaths(entries []string) []string {
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		parts := strings.SplitN(e, "\t", 2)
		if len(parts) > 0 {
			paths = append(paths, parts[0])
		}
	}
	return paths
}

// containsPath reports whether path is in paths.
func containsPath(paths []string, path string) bool {
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	return false
}
