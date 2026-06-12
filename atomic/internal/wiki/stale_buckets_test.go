package wiki_test

// stale_buckets_test.go — CP3: tests for the `atomic wiki stale` bucket extension.
//
// Success criteria from the spec (checkpoint 3):
//   - Fresh bucket (empty diff) → no STALE bucket line emitted, exit 0.
//   - Pending bucket (non-empty diff) → `STALE bucket <name>` line + exit 1.
//   - Hard error (missing wiki dir) → exit 2 (existing behaviour, verified here
//     as an explicit gate that the bucket path doesn't regress it).
//   - Realm with no <wiki-buckets> block → zero bucket lines, no error.
//   - Block with declined="true" (empty block) → zero bucket lines, no error.
//   - Stale does not write `current` for buckets (read-only contract).
//   - Existing DRIFT/STALE repo/concern behaviour unaffected (integration check).

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// --- helpers ------------------------------------------------------------------

// setupBucketRealm creates a minimal realm with a wiki/ dir (via Scan) and
// returns (root, wikiDir).  At least one git repo is required by Scan.
func setupBucketRealm(t *testing.T) (root, wikiDir string) {
	t.Helper()
	root = t.TempDir()
	// A single committed git repo so Scan can produce a valid <wiki-scan> block.
	makeCommittedRepo(t, root, "repoA")
	// Run Scan so wiki/index.md and the <wiki-scan> block are written.
	_, err := wiki.Scan(root, wiki.Options{Clock: func() time.Time {
		return time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	wikiDir = filepath.Join(root, "wiki")
	return root, wikiDir
}

// addBucket registers a bucket via the public WikiAction (bucket add) and
// returns the bucket directory path.
func addBucket(t *testing.T, root, wikiDir, name string) string {
	t.Helper()
	bucketDir := filepath.Join(root, name)
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Use WikiAction to register — it splices the <wiki-buckets> block in
	// wiki/index.md and creates the manifest dir, matching prod code paths.
	var out bytes.Buffer
	// claudeHome can be a temp dir (no real CLAUDE.md needed for the test).
	claudeHome := t.TempDir()
	args := []string{"bucket", "add", "--root=" + root, name}
	if code := wiki.WikiAction(args, claudeHome, root, &out); code != 0 {
		t.Fatalf("WikiAction bucket add %q: exit %d; output: %q", name, code, out.String())
	}
	return bucketDir
}

// writeBucketContent writes a content file (not index.md) into bucketDir at relname.
// Uses the existing writeBucketFile helper (path, content) from bucket_test.go.
func writeBucketContent(t *testing.T, bucketDir, relname, content string) {
	t.Helper()
	writeBucketFile(t, filepath.Join(bucketDir, relname), content)
}

// currentManifestMtime returns the mtime of wiki/.buckets/<name>/current, or
// the zero time if the file does not exist.
func currentManifestMtime(wikiDir, name string) time.Time {
	p := filepath.Join(wikiDir, ".buckets", name, "current")
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// --- tests --------------------------------------------------------------------

// TestStale_BucketFresh verifies that a bucket with no pending diff emits no
// STALE bucket line and exits 0 (assuming no other staleness).
func TestStale_BucketFresh(t *testing.T) {
	root, wikiDir := setupBucketRealm(t)

	bucketDir := addBucket(t, root, wikiDir, "research")
	writeBucketContent(t, bucketDir, "note.md", "some content\n")

	// Promote so the diff is empty (baseline == current).
	if err := wiki.PromoteBucket(wikiDir, "research", bucketDir); err != nil {
		t.Fatalf("PromoteBucket: %v", err)
	}

	code, out := runStale(t, root)

	if code != 0 {
		t.Errorf("expected exit 0 (fresh), got %d; stdout: %q", code, out)
	}
	if strings.Contains(out, "STALE bucket") {
		t.Errorf("fresh bucket must not emit STALE bucket line; got: %q", out)
	}
}

// TestStale_BucketPending verifies that a bucket with a non-empty diff emits
// `STALE bucket <name>` and exits 1.
func TestStale_BucketPending(t *testing.T) {
	root, wikiDir := setupBucketRealm(t)

	bucketDir := addBucket(t, root, wikiDir, "research")
	// Write a content file WITHOUT promoting — diff is non-empty (all new).
	writeBucketContent(t, bucketDir, "note.md", "some content\n")

	code, out := runStale(t, root)

	if code != 1 {
		t.Errorf("expected exit 1 (stale), got %d; stdout: %q", code, out)
	}
	wantLine := "STALE bucket research"
	if !strings.Contains(out, wantLine) {
		t.Errorf("expected %q in output; got: %q", wantLine, out)
	}
}

// TestStale_BucketPendingLiteralPrefix verifies the exact output prefix
// is `STALE bucket ` (not `STALE bucket:` or similar).
func TestStale_BucketPendingLiteralPrefix(t *testing.T) {
	root, wikiDir := setupBucketRealm(t)

	bucketDir := addBucket(t, root, wikiDir, "raw")
	writeBucketContent(t, bucketDir, "dump.txt", "data\n")

	_, out := runStale(t, root)

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(line, "bucket") {
			if !strings.HasPrefix(line, "STALE bucket ") {
				t.Errorf("bucket stale line has wrong prefix: %q", line)
			}
		}
	}
}

// TestStale_BucketHardError verifies that a missing wiki/ dir still causes
// exit 2 (bucket extension does not change the hard-error contract).
func TestStale_BucketHardError(t *testing.T) {
	root := t.TempDir()
	// Do NOT run Scan — wiki/ does not exist.

	var buf bytes.Buffer
	code, err := wiki.Stale(root, &buf)

	if code != 2 {
		t.Errorf("expected exit 2 (hard error), got %d", code)
	}
	if err == nil {
		t.Errorf("expected non-nil error for hard-error path, got nil")
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty data buffer on hard-error path, got: %q", buf.String())
	}
}

// TestStale_NoBucketsBlock verifies that a wiki with no <wiki-buckets> block
// emits zero bucket lines and does not error.
func TestStale_NoBucketsBlock(t *testing.T) {
	root, _ := setupBucketRealm(t)

	// No bucket registration — wiki/index.md has no <wiki-buckets> block.
	code, out := runStale(t, root)

	// Should be exit 0 (only repos present, all fresh by membership only).
	// Even if repos cause DRIFT, the key assertion is no STALE bucket line.
	if strings.Contains(out, "STALE bucket") {
		t.Errorf("no <wiki-buckets> block should produce zero bucket lines; got: %q", out)
	}
	// Exit code must not be 2.
	if code == 2 {
		t.Errorf("no <wiki-buckets> block must not cause exit 2, got %d", code)
	}
}

// TestStale_DeclinedBlock verifies that a <wiki-buckets declined="true"> block
// (empty, user declined offer) emits zero bucket lines and does not error.
func TestStale_DeclinedBlock(t *testing.T) {
	root, wikiDir := setupBucketRealm(t)

	// Manually write a declined block into wiki/index.md.
	indexPath := filepath.Join(wikiDir, "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	// Append the declined block.
	declined := "\n<wiki-buckets declined=\"true\">\n</wiki-buckets>\n"
	if err := os.WriteFile(indexPath, append(data, []byte(declined)...), 0o644); err != nil {
		t.Fatalf("write index.md: %v", err)
	}

	code, out := runStale(t, root)

	if strings.Contains(out, "STALE bucket") {
		t.Errorf("declined block should produce zero bucket lines; got: %q", out)
	}
	if code == 2 {
		t.Errorf("declined block must not cause exit 2, got %d", code)
	}
}

// TestStale_BucketReadOnly verifies that Stale does not write `current` for
// any bucket (the stale check is read-only).
func TestStale_BucketReadOnly(t *testing.T) {
	root, wikiDir := setupBucketRealm(t)

	bucketDir := addBucket(t, root, wikiDir, "research")
	writeBucketContent(t, bucketDir, "note.md", "content\n")

	// Stale has never been called — current should not exist.
	currentPath := filepath.Join(wikiDir, ".buckets", "research", "current")
	if _, err := os.Stat(currentPath); err == nil {
		t.Fatalf("current manifest must not exist before first Stale call")
	}

	var buf bytes.Buffer
	if _, err := wiki.Stale(root, &buf); err != nil {
		t.Fatalf("Stale: %v", err)
	}

	// current must still not exist.
	if _, err := os.Stat(currentPath); err == nil {
		t.Errorf("Stale wrote current manifest — must be read-only")
	}
}

// TestStale_BucketReadOnly_NoMutateAfterPromote verifies that Stale does not
// update the mtime of an existing current manifest written by a prior promote.
func TestStale_BucketReadOnly_NoMutateAfterPromote(t *testing.T) {
	root, wikiDir := setupBucketRealm(t)

	bucketDir := addBucket(t, root, wikiDir, "research")
	writeBucketContent(t, bucketDir, "note.md", "content\n")

	// Promote writes current.
	if err := wiki.PromoteBucket(wikiDir, "research", bucketDir); err != nil {
		t.Fatalf("PromoteBucket: %v", err)
	}
	before := currentManifestMtime(wikiDir, "research")

	// Add new file to make bucket pending.
	writeBucketContent(t, bucketDir, "note2.md", "new content\n")

	var buf bytes.Buffer
	if _, err := wiki.Stale(root, &buf); err != nil {
		t.Fatalf("Stale: %v", err)
	}

	after := currentManifestMtime(wikiDir, "research")
	if !after.Equal(before) {
		t.Errorf("Stale modified current manifest (before: %v, after: %v); must be read-only", before, after)
	}
}

// TestStale_BucketMultiple verifies that multiple pending buckets each emit
// their own STALE bucket line.
func TestStale_BucketMultiple(t *testing.T) {
	root, wikiDir := setupBucketRealm(t)

	bucketA := addBucket(t, root, wikiDir, "research")
	writeBucketContent(t, bucketA, "note.md", "a\n")

	bucketB := addBucket(t, root, wikiDir, "raw")
	writeBucketContent(t, bucketB, "dump.txt", "b\n")

	code, out := runStale(t, root)

	if code != 1 {
		t.Errorf("expected exit 1, got %d; stdout: %q", code, out)
	}
	if !strings.Contains(out, "STALE bucket research") {
		t.Errorf("expected STALE bucket research; got: %q", out)
	}
	if !strings.Contains(out, "STALE bucket raw") {
		t.Errorf("expected STALE bucket raw; got: %q", out)
	}
}

// TestStale_BucketMixedFreshAndPending verifies that only the pending bucket
// emits a line when one bucket is fresh and one is pending.
func TestStale_BucketMixedFreshAndPending(t *testing.T) {
	root, wikiDir := setupBucketRealm(t)

	// research: fresh (promoted).
	bucketA := addBucket(t, root, wikiDir, "research")
	writeBucketContent(t, bucketA, "note.md", "a\n")
	if err := wiki.PromoteBucket(wikiDir, "research", bucketA); err != nil {
		t.Fatalf("PromoteBucket research: %v", err)
	}

	// raw: pending (never promoted).
	bucketB := addBucket(t, root, wikiDir, "raw")
	writeBucketContent(t, bucketB, "dump.txt", "b\n")

	_, out := runStale(t, root)

	if strings.Contains(out, "STALE bucket research") {
		t.Errorf("fresh bucket must not emit STALE bucket line; got: %q", out)
	}
	if !strings.Contains(out, "STALE bucket raw") {
		t.Errorf("expected STALE bucket raw; got: %q", out)
	}
}

// TestStale_BucketAndRepoConcernUnaffected verifies that adding bucket staleness
// does not suppress DRIFT/STALE repo/concern lines — all appear in the same run.
func TestStale_BucketAndRepoConcernUnaffected(t *testing.T) {
	root, wikiDir := setupBucketRealm(t)

	// Add a second repo after scan to trigger DRIFT added.
	makeCommittedRepo(t, root, "repoB")

	// Add a pending bucket.
	bucketDir := addBucket(t, root, wikiDir, "research")
	writeBucketContent(t, bucketDir, "note.md", "content\n")

	code, out := runStale(t, root)

	if code != 1 {
		t.Errorf("expected exit 1, got %d; stdout: %q", code, out)
	}
	if !strings.Contains(out, "DRIFT added repoB") {
		t.Errorf("expected DRIFT added repoB; got: %q", out)
	}
	if !strings.Contains(out, "STALE bucket research") {
		t.Errorf("expected STALE bucket research; got: %q", out)
	}
}

// TestStale_BucketEmptyBucket verifies that an empty bucket (no content files)
// with no baseline emits no STALE bucket line (empty diff → no lines to report).
func TestStale_BucketEmptyBucket(t *testing.T) {
	root, wikiDir := setupBucketRealm(t)

	// Register bucket but write only index.md (excluded from manifest).
	addBucket(t, root, wikiDir, "research")
	// No content files added — walk returns empty, diff has no Added entries.

	code, out := runStale(t, root)

	if strings.Contains(out, "STALE bucket research") {
		t.Errorf("empty bucket should not emit STALE bucket line; got: %q", out)
	}
	// Must not be exit 2.
	if code == 2 {
		t.Errorf("empty bucket must not cause exit 2, got %d", code)
	}
}

// TestStale_BucketLinesAfterRepoConcernLines verifies that all "STALE bucket"
// lines appear AFTER all repo/concern/summary lines in the output.
// The spec requires: sections 1-2 (DRIFT/STALE summary/concern) sorted first,
// then section-3 bucket lines sorted among themselves at the end.
func TestStale_BucketLinesAfterRepoConcernLines(t *testing.T) {
	root, wikiDir := setupBucketRealm(t)

	// Force a stale concern by adding a concern file with no frontmatter.
	concernsDir := filepath.Join(wikiDir, "concerns")
	if err := os.MkdirAll(concernsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	concernPath := filepath.Join(concernsDir, "cross.md")
	if err := os.WriteFile(concernPath, []byte("## no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Add a pending bucket (never promoted).
	bucketDir := addBucket(t, root, wikiDir, "research")
	writeBucketContent(t, bucketDir, "note.md", "content\n")

	code, out := runStale(t, root)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d; stdout: %q", code, out)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")

	// Collect indices of bucket lines and non-bucket lines.
	var bucketIdx []int
	var nonBucketIdx []int
	for i, line := range lines {
		if strings.HasPrefix(line, "STALE bucket ") {
			bucketIdx = append(bucketIdx, i)
		} else if strings.HasPrefix(line, "DRIFT ") || strings.HasPrefix(line, "STALE ") {
			nonBucketIdx = append(nonBucketIdx, i)
		}
	}

	if len(bucketIdx) == 0 {
		t.Fatal("expected at least one STALE bucket line but found none")
	}
	if len(nonBucketIdx) == 0 {
		t.Fatal("expected at least one non-bucket DRIFT/STALE line but found none")
	}

	// Every bucket line index must be greater than every non-bucket line index.
	for _, bi := range bucketIdx {
		for _, ni := range nonBucketIdx {
			if bi <= ni {
				t.Errorf("STALE bucket line at index %d appears before or at non-bucket line at index %d; output:\n%s", bi, ni, out)
			}
		}
	}
}

// TestStale_BucketWalkError verifies that a walk/I/O error inside
// bucketDiffReadOnly (e.g. unreadable bucket dir) causes exit 2 (hard error)
// and does NOT emit a "STALE bucket" line.
// Rationale: a walk error means we cannot determine freshness; emitting a fake
// stale line would misrepresent the cause. Consistent with the stale exit-code
// contract: exit 2 = hard error, caller routes to stderr.
func TestStale_BucketWalkError(t *testing.T) {
	root, wikiDir := setupBucketRealm(t)

	// Register the bucket so the <wiki-buckets> block is present and parseable.
	bucketDir := addBucket(t, root, wikiDir, "research")

	// Make the bucket dir unreadable so WalkBucket returns an error.
	if err := os.Chmod(bucketDir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		// Restore permissions so t.TempDir cleanup can remove it.
		_ = os.Chmod(bucketDir, 0o755)
	})

	var buf bytes.Buffer
	code, err := wiki.Stale(root, &buf)

	if code != 2 {
		t.Errorf("expected exit 2 (hard error from walk error), got %d; stdout: %q", code, buf.String())
	}
	if err == nil {
		t.Errorf("expected non-nil error for walk-error path, got nil")
	}
	if strings.Contains(buf.String(), "STALE bucket") {
		t.Errorf("walk error must not emit STALE bucket line; got: %q", buf.String())
	}
}
