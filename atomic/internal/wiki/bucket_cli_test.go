package wiki

// bucket_cli_test.go — CP2 tests for `atomic wiki bucket` CLI verbs.
//
// Uses the internal (package wiki) test package to access wikiAction and
// the unexported bucket helpers directly.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- helpers ----

// setupBucketCLIRoot builds a minimal realm with a wiki/ directory and a
// bucket folder at <root>/testbucket/.
func setupBucketCLIRoot(t *testing.T) (root, bucketDir, wikiDir string) {
	t.Helper()
	root = t.TempDir()
	wikiDir = filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Minimal wiki/index.md with a <wiki-buckets> placeholder (will be created
	// by bucket add if absent; we test both cases).
	bucketDir = filepath.Join(root, "testbucket")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, bucketDir, wikiDir
}

// writeBucketCLIFile writes content to path, creating parent dirs.
func writeBucketCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ---- <wiki-buckets> block splice tests ----

// TestWriteWikiBucketsBlock_AppendsWhenAbsent verifies that when wiki/index.md
// has no <wiki-buckets> block, one is appended preserving existing content.
func TestWriteWikiBucketsBlock_AppendsWhenAbsent(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)

	indexPath := filepath.Join(wikiDir, "index.md")
	writeBucketCLIFile(t, indexPath, "# Wiki index\n\nSome content.\n")

	if err := spliceBucketEntry(indexPath, "research", filepath.Join(root, "research")); err != nil {
		t.Fatalf("spliceBucketEntry: %v", err)
	}

	data, _ := os.ReadFile(indexPath)
	content := string(data)

	if !strings.Contains(content, "<wiki-buckets>") {
		t.Error("expected <wiki-buckets> block in index.md")
	}
	if !strings.Contains(content, `name="research"`) {
		t.Error("expected bucket entry in block")
	}
	// Prior content preserved.
	if !strings.Contains(content, "# Wiki index") {
		t.Error("prior content was dropped")
	}
}

// TestWriteWikiBucketsBlock_IdempotentReSplice verifies that calling spliceBucketEntry
// twice with the same name does not duplicate the entry.
func TestWriteWikiBucketsBlock_IdempotentReSplice(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	indexPath := filepath.Join(wikiDir, "index.md")
	writeBucketCLIFile(t, indexPath, "# Wiki\n")

	bucketPath := filepath.Join(root, "research")
	if err := spliceBucketEntry(indexPath, "research", bucketPath); err != nil {
		t.Fatalf("first splice: %v", err)
	}
	if err := spliceBucketEntry(indexPath, "research", bucketPath); err != nil {
		t.Fatalf("second splice: %v", err)
	}

	data, _ := os.ReadFile(indexPath)
	count := strings.Count(string(data), `name="research"`)
	if count != 1 {
		t.Errorf("expected 1 entry, got %d", count)
	}
}

// TestWriteWikiBucketsBlock_MultipleEntries verifies two distinct buckets both
// appear after successive spliceBucketEntry calls.
func TestWriteWikiBucketsBlock_MultipleEntries(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	indexPath := filepath.Join(wikiDir, "index.md")
	writeBucketCLIFile(t, indexPath, "# Wiki\n")

	if err := spliceBucketEntry(indexPath, "research", filepath.Join(root, "research")); err != nil {
		t.Fatal(err)
	}
	if err := spliceBucketEntry(indexPath, "raw", filepath.Join(root, "raw")); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(indexPath)
	content := string(data)
	if !strings.Contains(content, `name="research"`) {
		t.Error("research entry missing")
	}
	if !strings.Contains(content, `name="raw"`) {
		t.Error("raw entry missing")
	}
}

// TestWriteWikiBucketsBlock_RemovesDeclinedAttr verifies that when the block
// has a declined="true" attribute, spliceBucketEntry removes it.
func TestWriteWikiBucketsBlock_RemovesDeclinedAttr(t *testing.T) {
	_, _, wikiDir := setupBucketCLIRoot(t)
	indexPath := filepath.Join(wikiDir, "index.md")
	writeBucketCLIFile(t, indexPath, `# Wiki

<wiki-buckets declined="true">
</wiki-buckets>

## Members
`)

	if err := spliceBucketEntry(indexPath, "research", "/realm/research"); err != nil {
		t.Fatalf("spliceBucketEntry: %v", err)
	}

	data, _ := os.ReadFile(indexPath)
	content := string(data)

	if strings.Contains(content, `declined="true"`) {
		t.Error("declined attribute should have been removed")
	}
	if !strings.Contains(content, `name="research"`) {
		t.Error("new entry missing after removing declined attr")
	}
}

// TestWriteWikiBucketsBlock_CreateIndexWhenAbsent verifies that spliceBucketEntry
// creates wiki/index.md when absent (no prior file).
func TestWriteWikiBucketsBlock_CreateIndexWhenAbsent(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	indexPath := filepath.Join(wikiDir, "index.md")
	// Do NOT pre-create indexPath.

	if err := spliceBucketEntry(indexPath, "research", filepath.Join(root, "research")); err != nil {
		t.Fatalf("spliceBucketEntry on missing file: %v", err)
	}

	data, _ := os.ReadFile(indexPath)
	content := string(data)
	if !strings.Contains(content, "<wiki-buckets>") {
		t.Error("expected <wiki-buckets> block")
	}
	if !strings.Contains(content, `name="research"`) {
		t.Error("expected entry in new file")
	}
}

// ---- ## Capture surfaces tests ----

// TestCaptureSurfacesSection_CreatesFileWhenAbsent verifies that
// writeCaptureSurfacesSection creates realm CLAUDE.md when absent.
func TestCaptureSurfacesSection_CreatesFileWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")

	if err := writeCaptureSurfacesSection(claudeMD, "research", filepath.Join(dir, "research")); err != nil {
		t.Fatalf("writeCaptureSurfacesSection: %v", err)
	}

	data, _ := os.ReadFile(claudeMD)
	content := string(data)
	if !strings.Contains(content, "## Capture surfaces") {
		t.Error("expected ## Capture surfaces section")
	}
	if !strings.Contains(content, "research") {
		t.Error("expected bucket path in section")
	}
	if !strings.Contains(content, "<!-- describe what this bucket is for -->") {
		t.Error("expected purpose placeholder")
	}
}

// TestCaptureSurfacesSection_AppendsToExistingFile verifies that
// writeCaptureSurfacesSection appends the section to an existing file,
// preserving all prior content byte-for-byte.  The original bytes must occupy
// the exact prefix (index 0 through len(original)-1) and the appended section
// must be the only addition — no bytes inserted or removed from the prefix.
func TestCaptureSurfacesSection_AppendsToExistingFile(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	original := "# My Config\n\nsome existing content\n"
	if err := os.WriteFile(claudeMD, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	bucketPath := filepath.Join(dir, "research")
	if err := writeCaptureSurfacesSection(claudeMD, "research", bucketPath); err != nil {
		t.Fatalf("writeCaptureSurfacesSection: %v", err)
	}

	data, _ := os.ReadFile(claudeMD)
	content := string(data)

	// Byte-for-byte: original must be exact prefix at index 0.
	if len(content) < len(original) {
		t.Fatalf("result (%d bytes) is shorter than original (%d bytes)", len(content), len(original))
	}
	if content[:len(original)] != original {
		t.Errorf("original bytes mutated: got prefix %q, want %q", content[:len(original)], original)
	}

	// The appended portion must contain the section heading and the bucket bullet.
	appended := content[len(original):]
	if !strings.Contains(appended, "## Capture surfaces") {
		t.Error("section heading not in appended portion")
	}
	if !strings.Contains(appended, bucketPath) {
		t.Errorf("bucket path %q not in appended portion: %q", bucketPath, appended)
	}

	// No extra content beyond the section (no extra blank lines prepended to
	// original, no duplicate content).
	if strings.Contains(original, "## Capture surfaces") {
		t.Error("original already contained the heading — test setup is wrong")
	}
}

// TestCaptureSurfacesSection_AppendsNewBulletWhenSectionExists verifies that
// a second call appends a new bullet to an existing ## Capture surfaces section.
func TestCaptureSurfacesSection_AppendsNewBulletWhenSectionExists(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")

	if err := writeCaptureSurfacesSection(claudeMD, "research", filepath.Join(dir, "research")); err != nil {
		t.Fatal(err)
	}
	if err := writeCaptureSurfacesSection(claudeMD, "raw", filepath.Join(dir, "raw")); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(claudeMD)
	content := string(data)

	// One heading, two bullets.
	headingCount := strings.Count(content, "## Capture surfaces")
	if headingCount != 1 {
		t.Errorf("expected 1 heading, got %d", headingCount)
	}
	if !strings.Contains(content, "research") {
		t.Error("first bucket missing")
	}
	if !strings.Contains(content, "raw") {
		t.Error("second bucket missing")
	}
}

// TestCaptureSurfacesSection_WrittenOnceHeading verifies that the heading is
// written exactly once even when called multiple times.
func TestCaptureSurfacesSection_WrittenOnceHeading(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")

	for _, name := range []string{"a", "b", "c"} {
		if err := writeCaptureSurfacesSection(claudeMD, name, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}

	data, _ := os.ReadFile(claudeMD)
	count := strings.Count(string(data), "## Capture surfaces")
	if count != 1 {
		t.Errorf("expected heading once, got %d", count)
	}
}

// ---- bucket index.md stub tests ----

// TestBucketIndexStub_CreatesStub verifies that createBucketIndexStub writes
// an index.md with a purpose line and ## Conventions placeholder.
func TestBucketIndexStub_CreatesStub(t *testing.T) {
	dir := t.TempDir()
	bucketDir := filepath.Join(dir, "research")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := createBucketIndexStub(bucketDir, "research"); err != nil {
		t.Fatalf("createBucketIndexStub: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(bucketDir, "index.md"))
	content := string(data)
	if !strings.Contains(content, "## Conventions") {
		t.Error("expected ## Conventions placeholder")
	}
	if !strings.Contains(content, "research") {
		t.Error("expected bucket name in stub")
	}
}

// TestBucketIndexStub_PreservesExisting verifies that createBucketIndexStub
// does NOT overwrite an existing index.md.
func TestBucketIndexStub_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	bucketDir := filepath.Join(dir, "research")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "# My existing notes\n"
	if err := os.WriteFile(filepath.Join(bucketDir, "index.md"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := createBucketIndexStub(bucketDir, "research"); err != nil {
		t.Fatalf("createBucketIndexStub on existing: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(bucketDir, "index.md"))
	if string(data) != existing {
		t.Errorf("existing index.md was overwritten; got:\n%s", string(data))
	}
}

// ---- wikiAction integration: bucket add ----

// TestBucketAdd_RegistersAndSplices verifies that `atomic wiki bucket add <name>`
// via wikiAction: registers the manifest dir, splices the block, writes the
// CLAUDE.md section, creates the bucket index.md stub.
func TestBucketAdd_RegistersAndSplices(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	indexPath := filepath.Join(wikiDir, "index.md")
	writeBucketCLIFile(t, indexPath, "# Wiki\n")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "add", "--root=" + root, "mybucket"}, claudeHome, root, &out)
	if code != 0 {
		t.Fatalf("wikiAction bucket add returned %d; stderr likely has detail", code)
	}

	// Manifest dir created.
	mdir := filepath.Join(wikiDir, ".buckets", "mybucket")
	if _, err := os.Lstat(mdir); err != nil {
		t.Errorf("manifest dir not created: %v", err)
	}

	// Block spliced.
	data, _ := os.ReadFile(indexPath)
	if !strings.Contains(string(data), `name="mybucket"`) {
		t.Errorf("bucket entry missing from wiki/index.md:\n%s", string(data))
	}

	// Bucket dir created.
	bucketDir := filepath.Join(root, "mybucket")
	if _, err := os.Lstat(bucketDir); err != nil {
		t.Errorf("bucket dir not created: %v", err)
	}

	// index.md stub created.
	if _, err := os.Lstat(filepath.Join(bucketDir, "index.md")); err != nil {
		t.Errorf("bucket index.md stub not created: %v", err)
	}
}

// TestBucketAdd_RefusesReservedNameWiki verifies that `bucket add wiki` exits
// non-zero with a message.
func TestBucketAdd_RefusesReservedNameWiki(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "add", "--root=" + root, "wiki"}, claudeHome, root, &out)
	if code == 0 {
		t.Error("expected non-zero exit for reserved name wiki")
	}
}

// TestBucketAdd_RefusesDoubleRegister verifies that adding the same bucket
// twice exits non-zero on the second call.
func TestBucketAdd_RefusesDoubleRegister(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "mybucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("first add failed: %d", code)
	}
	out.Reset()
	code := wikiAction([]string{"bucket", "add", "--root=" + root, "mybucket"}, claudeHome, root, &out)
	if code == 0 {
		t.Error("expected non-zero exit for double-register")
	}
}

// TestBucketAdd_WritesCLAUDEMD verifies that bucket add writes the
// ## Capture surfaces section to the realm CLAUDE.md.
func TestBucketAdd_WritesCLAUDEMD(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	// Write a pre-existing realm CLAUDE.md.
	realmCLAUDE := filepath.Join(root, "CLAUDE.md")
	writeBucketCLIFile(t, realmCLAUDE, "# Realm config\n\nexisting content\n")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "add", "--root=" + root, "notes"}, claudeHome, root, &out)
	if code != 0 {
		t.Fatalf("bucket add failed: %d", code)
	}

	data, _ := os.ReadFile(realmCLAUDE)
	content := string(data)
	if !strings.Contains(content, "## Capture surfaces") {
		t.Error("## Capture surfaces section not written to realm CLAUDE.md")
	}
	if !strings.Contains(content, "# Realm config") || !strings.Contains(content, "existing content") {
		t.Error("prior content not preserved in realm CLAUDE.md")
	}
}

// ---- wikiAction integration: bucket list ----

// TestBucketList_NoBuckets verifies that `bucket list` exits 0 with no output
// when no buckets are registered.
func TestBucketList_NoBuckets(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "list", "--root=" + root}, claudeHome, root, &out)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output, got: %q", out.String())
	}
}

// TestBucketList_PendingNoBaseline verifies that a registered bucket with no
// baseline and content files reports "(no baseline)  (pending)" — the count
// field is replaced by "(no baseline)" but the status field still prints.
// Every content file counts as pending when there is no baseline.
func TestBucketList_PendingNoBaseline(t *testing.T) {
	root, bucketDir, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	// Add a file to the bucket — makes it pending.
	writeBucketCLIFile(t, filepath.Join(bucketDir, "a.md"), "content\n")

	var out bytes.Buffer
	// Add the bucket via CLI.
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add failed: %d", code)
	}
	out.Reset()

	code := wikiAction([]string{"bucket", "list", "--root=" + root}, claudeHome, root, &out)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	line := strings.TrimSpace(out.String())
	if !strings.Contains(line, "testbucket") {
		t.Errorf("expected testbucket in output; got: %q", line)
	}
	if !strings.Contains(line, "(no baseline)") {
		t.Errorf("expected (no baseline) for unpromoted bucket; got: %q", line)
	}
	// Status field must still appear — every content file is pending when no baseline.
	if !strings.Contains(line, "(pending)") {
		t.Errorf("expected (pending) status when bucket has content and no baseline; got: %q", line)
	}
}

// TestBucketList_FreshNoBaselineAndNoContent verifies that a registered bucket
// with no baseline AND no content files reports "(no baseline)  (fresh)":
// diff is empty (nothing to process), so the status is fresh.
func TestBucketList_FreshNoBaselineAndNoContent(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	// testbucket dir has no content files (was created by setupBucketCLIRoot but empty).

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add failed: %d", code)
	}
	out.Reset()

	code := wikiAction([]string{"bucket", "list", "--root=" + root}, claudeHome, root, &out)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	line := strings.TrimSpace(out.String())
	if !strings.Contains(line, "(no baseline)") {
		t.Errorf("expected (no baseline) in output; got: %q", line)
	}
	// Empty bucket with no baseline → diff is empty → fresh.
	if !strings.Contains(line, "(fresh)") {
		t.Errorf("expected (fresh) status for empty bucket with no baseline; got: %q", line)
	}
}

// TestBucketList_FreshAfterPromote verifies that after a promote, list shows
// "(fresh)" (bucket is in sync with baseline).
func TestBucketList_FreshAfterPromote(t *testing.T) {
	root, bucketDir, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	writeBucketCLIFile(t, filepath.Join(bucketDir, "a.md"), "content\n")

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add: %d", code)
	}
	out.Reset()
	if code := wikiAction([]string{"bucket", "promote", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket promote: %d", code)
	}
	out.Reset()

	code := wikiAction([]string{"bucket", "list", "--root=" + root}, claudeHome, root, &out)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	line := strings.TrimSpace(out.String())
	if !strings.Contains(line, "(fresh)") {
		t.Errorf("expected (fresh) after promote; got: %q", line)
	}
}

// TestBucketList_PendingAfterChange verifies that after promoting and then
// changing a file, list shows "(pending)".
func TestBucketList_PendingAfterChange(t *testing.T) {
	root, bucketDir, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	writeBucketCLIFile(t, filepath.Join(bucketDir, "a.md"), "original\n")

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add: %d", code)
	}
	out.Reset()
	if code := wikiAction([]string{"bucket", "promote", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket promote: %d", code)
	}
	out.Reset()

	// Change the file.
	writeBucketCLIFile(t, filepath.Join(bucketDir, "a.md"), "modified\n")

	code := wikiAction([]string{"bucket", "list", "--root=" + root}, claudeHome, root, &out)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	line := strings.TrimSpace(out.String())
	if !strings.Contains(line, "(pending)") {
		t.Errorf("expected (pending) after file change; got: %q", line)
	}
}

// ---- wikiAction integration: bucket diff ----

// TestBucketDiff_ExitOneWhenPending verifies that `bucket diff` exits 1 and
// prints change lines when the bucket has pending changes.
func TestBucketDiff_ExitOneWhenPending(t *testing.T) {
	root, bucketDir, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	writeBucketCLIFile(t, filepath.Join(bucketDir, "a.md"), "content\n")

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add: %d", code)
	}
	out.Reset()

	code := wikiAction([]string{"bucket", "diff", "--root=" + root, "testbucket"}, claudeHome, root, &out)
	if code != 1 {
		t.Errorf("expected exit 1 (pending), got %d; output: %q", code, out.String())
	}
	if !strings.Contains(out.String(), "new ") {
		t.Errorf("expected 'new' lines in diff output; got: %q", out.String())
	}
}

// TestBucketDiff_ExitZeroWhenFresh verifies that `bucket diff` exits 0 and
// prints nothing when the bucket is in sync with baseline.
func TestBucketDiff_ExitZeroWhenFresh(t *testing.T) {
	root, bucketDir, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	writeBucketCLIFile(t, filepath.Join(bucketDir, "a.md"), "content\n")

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add: %d", code)
	}
	out.Reset()
	if code := wikiAction([]string{"bucket", "promote", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket promote: %d", code)
	}
	out.Reset()

	code := wikiAction([]string{"bucket", "diff", "--root=" + root, "testbucket"}, claudeHome, root, &out)
	if code != 0 {
		t.Errorf("expected exit 0 (fresh), got %d; output: %q", code, out.String())
	}
	if out.Len() != 0 {
		t.Errorf("expected no output when fresh; got: %q", out.String())
	}
}

// TestBucketDiff_RefusesUnregistered verifies that `bucket diff` exits non-zero
// for an unregistered bucket name.
func TestBucketDiff_RefusesUnregistered(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "diff", "--root=" + root, "ghost"}, claudeHome, root, &out)
	if code == 0 {
		t.Error("expected non-zero exit for unregistered bucket")
	}
}

// ---- wikiAction integration: bucket promote ----

// TestBucketPromote_RotatesViaCliPath verifies `bucket promote` via the CLI
// path: after promote, diff exits 0.
func TestBucketPromote_RotatesViaCliPath(t *testing.T) {
	root, bucketDir, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	writeBucketCLIFile(t, filepath.Join(bucketDir, "a.md"), "content\n")

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add: %d", code)
	}
	out.Reset()
	if code := wikiAction([]string{"bucket", "promote", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket promote: %d", code)
	}
	out.Reset()

	code := wikiAction([]string{"bucket", "diff", "--root=" + root, "testbucket"}, claudeHome, root, &out)
	if code != 0 {
		t.Errorf("expected exit 0 after promote, got %d; output: %q", code, out.String())
	}
}

// TestBucketPromote_RefusesUnregistered verifies that `bucket promote` exits
// non-zero for an unregistered bucket.
func TestBucketPromote_RefusesUnregistered(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "promote", "--root=" + root, "ghost"}, claudeHome, root, &out)
	if code == 0 {
		t.Error("expected non-zero exit for unregistered bucket")
	}
}

// ---- issue-2: bucket add fail-loud on CLAUDE.md write failure ----

// TestBucketAdd_FailsWhenCLAUDEMDUnwritable verifies that `bucket add` exits
// non-zero when writeCaptureSurfacesSection fails (CLAUDE.md directory is
// unwritable so the file cannot be created).
func TestBucketAdd_FailsWhenCLAUDEMDUnwritable(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	// Write a CLAUDE.md that is itself a directory so WriteFile returns an
	// error (can't write to a path that is a directory).
	realmCLAUDE := filepath.Join(root, "CLAUDE.md")
	if err := os.MkdirAll(realmCLAUDE, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "add", "--root=" + root, "notes"}, claudeHome, root, &out)
	if code == 0 {
		t.Error("expected non-zero exit when CLAUDE.md write fails")
	}
}

// ---- issue-3: bucket list must not write current ----

// TestBucketList_DoesNotWriteCurrent verifies that `bucket list` is a pure
// read-only status verb: it must NOT create or modify
// wiki/.buckets/<name>/current as a side effect of computing pending/fresh
// status.
func TestBucketList_DoesNotWriteCurrent(t *testing.T) {
	root, bucketDir, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	writeBucketCLIFile(t, filepath.Join(bucketDir, "a.md"), "content\n")

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add: %d", code)
	}
	if code := wikiAction([]string{"bucket", "promote", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket promote: %d", code)
	}

	currentPath := filepath.Join(wikiDir, ".buckets", "testbucket", "current")

	// Record mtime before list.
	info, err := os.Lstat(currentPath)
	if err != nil {
		t.Fatalf("current file should exist after promote: %v", err)
	}
	mtimeBefore := info.ModTime()

	out.Reset()
	code := wikiAction([]string{"bucket", "list", "--root=" + root}, claudeHome, root, &out)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}

	// current must not have been modified.
	infoAfter, err := os.Lstat(currentPath)
	if err != nil {
		t.Fatalf("current file absent after list: %v", err)
	}
	if !infoAfter.ModTime().Equal(mtimeBefore) {
		t.Error("bucket list wrote to wiki/.buckets/<name>/current — must be read-only")
	}
}

// TestBucketList_DoesNotCreateCurrentWhenAbsent verifies that `bucket list`
// on a bucket that has never been diff'd or promoted does NOT create the
// current manifest file.
func TestBucketList_DoesNotCreateCurrentWhenAbsent(t *testing.T) {
	root, bucketDir, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	writeBucketCLIFile(t, filepath.Join(bucketDir, "a.md"), "content\n")

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add: %d", code)
	}

	currentPath := filepath.Join(wikiDir, ".buckets", "testbucket", "current")

	out.Reset()
	code := wikiAction([]string{"bucket", "list", "--root=" + root}, claudeHome, root, &out)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}

	if _, err := os.Lstat(currentPath); err == nil {
		t.Error("bucket list created wiki/.buckets/<name>/current — must not do so")
	}
}

// ---- BUG 1: --root space-form must be honored by all bucket verbs ----

// TestBucketAdd_SpaceFormRoot_FlagBeforeName verifies that
// `atomic wiki bucket add --root <path> <name>` (space-separated, flag before
// positional) uses <path> as the realm root and does NOT create anything in cwd.
func TestBucketAdd_SpaceFormRoot_FlagBeforeName(t *testing.T) {
	realm := t.TempDir()
	cwd := t.TempDir() // completely separate from realm
	claudeHome := t.TempDir()

	wikiDir := filepath.Join(realm, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	var out bytes.Buffer
	// Space form, flag before positional: ["--root", realm, "mybucket"]
	code := wikiAction([]string{"bucket", "add", "--root", realm, "mybucket"}, claudeHome, cwd, &out)
	if code != 0 {
		t.Fatalf("bucket add --root <space> returned %d; output: %q", code, out.String())
	}

	// Manifest dir must be under realm, not cwd.
	realmManifest := filepath.Join(wikiDir, ".buckets", "mybucket")
	if _, err := os.Lstat(realmManifest); err != nil {
		t.Errorf("manifest dir not created under realm: %v", err)
	}

	// bucket dir must be under realm.
	if _, err := os.Lstat(filepath.Join(realm, "mybucket")); err != nil {
		t.Errorf("bucket dir not created under realm: %v", err)
	}

	// Nothing should have been created under cwd.
	entries, _ := os.ReadDir(cwd)
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("cwd was polluted: %v", names)
	}
}

// TestBucketAdd_SpaceFormRoot_NameBeforeFlag verifies the failing scenario:
// `atomic wiki bucket add <name> --root <path>` (name before flag) must use
// <path> as realm root and must NOT fall back to cwd.
func TestBucketAdd_SpaceFormRoot_NameBeforeFlag(t *testing.T) {
	realm := t.TempDir()
	cwd := t.TempDir() // completely separate from realm
	claudeHome := t.TempDir()

	wikiDir := filepath.Join(realm, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	var out bytes.Buffer
	// Space form, name before flag (the reported bug): ["mybucket", "--root", realm]
	code := wikiAction([]string{"bucket", "add", "mybucket", "--root", realm}, claudeHome, cwd, &out)
	if code != 0 {
		t.Fatalf("bucket add <name> --root <path> returned %d; output: %q", code, out.String())
	}

	// Manifest dir must be under realm, not cwd.
	realmManifest := filepath.Join(wikiDir, ".buckets", "mybucket")
	if _, err := os.Lstat(realmManifest); err != nil {
		t.Errorf("manifest dir not created under realm (cwd=%s realm=%s): %v", cwd, realm, err)
	}

	// bucket dir must be under realm.
	if _, err := os.Lstat(filepath.Join(realm, "mybucket")); err != nil {
		t.Errorf("bucket dir not created under realm: %v", err)
	}

	// Nothing should have been created under cwd.
	entries, _ := os.ReadDir(cwd)
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("cwd was polluted: %v", names)
	}
}

// TestBucketDiff_SpaceFormRoot verifies that `bucket diff --root <path> <name>`
// uses <path> as realm root (both orderings).
func TestBucketDiff_SpaceFormRoot(t *testing.T) {
	realm := t.TempDir()
	cwd := t.TempDir()
	claudeHome := t.TempDir()

	wikiDir := filepath.Join(realm, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	bucketDir := filepath.Join(realm, "testbucket")
	writeBucketCLIFile(t, filepath.Join(bucketDir, "a.md"), "content\n")

	var out bytes.Buffer
	// Register bucket first using = form.
	if code := wikiAction([]string{"bucket", "add", "--root=" + realm, "testbucket"}, claudeHome, cwd, &out); code != 0 {
		t.Fatalf("bucket add: %d", code)
	}
	out.Reset()

	// diff with name before --root (the reported bug form).
	code := wikiAction([]string{"bucket", "diff", "testbucket", "--root", realm}, claudeHome, cwd, &out)
	// exit 1 = pending (has new files) — that's the right answer, not a parse error
	if code != 1 {
		t.Errorf("expected exit 1 (pending changes), got %d; output: %q", code, out.String())
	}
	if !strings.Contains(out.String(), "new ") {
		t.Errorf("expected 'new' lines in diff output; got: %q", out.String())
	}
}

// TestBucketVerb_RefusesUnknownArg verifies that unrecognized extra arguments
// to bucket verbs are rejected with a non-zero exit. This is the specific
// failure mode that caused --root to be silently ignored: unknown args were
// treated as extra positionals.
func TestBucketVerb_RefusesUnknownArg(t *testing.T) {
	realm := t.TempDir()
	cwd := t.TempDir()
	claudeHome := t.TempDir()

	wikiDir := filepath.Join(realm, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	var out bytes.Buffer
	// Pass an unrecognized flag after the positional name — must be rejected.
	code := wikiAction([]string{"bucket", "add", "--root=" + realm, "mybucket", "--unknown"}, claudeHome, cwd, &out)
	if code == 0 {
		t.Error("expected non-zero exit for unrecognized extra arg --unknown")
	}
}

// TestBucketAdd_ExtraPositionalRefused verifies that extra positional args
// after the bucket name are refused (not silently dropped).
func TestBucketAdd_ExtraPositionalRefused(t *testing.T) {
	realm := t.TempDir()
	cwd := t.TempDir()
	claudeHome := t.TempDir()

	wikiDir := filepath.Join(realm, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	var out bytes.Buffer
	// Extra positional after name.
	code := wikiAction([]string{"bucket", "add", "--root=" + realm, "mybucket", "extra"}, claudeHome, cwd, &out)
	if code == 0 {
		t.Error("expected non-zero exit for extra positional argument")
	}
}

// ---- BUG 2: ## Capture surfaces heading detection must be line-anchored ----

// TestBucketVerb_EmptyRootValueRefused verifies that --root= (equals form with
// an empty value) is rejected with a usage error, matching the space-form
// behavior when --root has no following value.
func TestBucketVerb_EmptyRootValueRefused(t *testing.T) {
	realm := t.TempDir()
	cwd := t.TempDir()
	claudeHome := t.TempDir()

	wikiDir := filepath.Join(realm, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	verbs := [][]string{
		{"bucket", "add", "--root=", "mybucket"},
		{"bucket", "list", "--root="},
		{"bucket", "diff", "--root=", "mybucket"},
		{"bucket", "promote", "--root=", "mybucket"},
	}
	for _, args := range verbs {
		var out bytes.Buffer
		code := wikiAction(args, claudeHome, cwd, &out)
		if code == 0 {
			t.Errorf("args %v: expected non-zero exit for --root= (empty value), got 0", args)
		}
	}
}

// TestCaptureSurfacesSection_InlineBacktickMentionNotMatched verifies that
// when realm CLAUDE.md contains "## Capture surfaces" only inside a backtick
// code span (e.g. in prose describing a section), writeCaptureSurfacesSection
// must NOT treat that as a real heading. The real section must be appended at
// EOF and the prose paragraph must be preserved byte-for-byte.
func TestCaptureSurfacesSection_InlineBacktickMentionNotMatched(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")

	// Mimics the real CLAUDE.md: prose mentioning the heading inside backticks
	// but no actual heading line.
	original := "# Config\n\nSee `## Capture surfaces` for the bucket list.\n\nMore content.\n"
	if err := os.WriteFile(claudeMD, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	bucketPath := filepath.Join(dir, "research")
	if err := writeCaptureSurfacesSection(claudeMD, "research", bucketPath); err != nil {
		t.Fatalf("writeCaptureSurfacesSection: %v", err)
	}

	data, _ := os.ReadFile(claudeMD)
	content := string(data)

	// Original bytes must be preserved exactly as a prefix.
	if !strings.HasPrefix(content, original) {
		t.Errorf("original bytes were mutated.\ngot:  %q\nwant prefix: %q", content, original)
	}

	// The real heading must have been appended at EOF.
	appended := content[len(original):]
	if !strings.Contains(appended, "## Capture surfaces") {
		t.Errorf("expected real ## Capture surfaces heading in appended portion; got: %q", appended)
	}
	if !strings.Contains(appended, bucketPath) {
		t.Errorf("expected bucket path in appended portion; got: %q", appended)
	}

	// The inline prose paragraph must NOT have been modified.
	if !strings.Contains(content, "`## Capture surfaces`") {
		t.Errorf("inline backtick mention was removed or altered from prose")
	}
}
