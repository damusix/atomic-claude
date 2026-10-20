package wiki

// bucket_cli_test.go — tests for `atomic wiki bucket` CLI verbs.
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

// writeUnreadableAndUnpairedDirs puts an unreadable directory beside one whose
// index.md has an unpaired <bucket-docs>: a hard error next to a warning.
func writeUnreadableAndUnpairedDirs(t *testing.T, bucketDir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits don't block reads")
	}
	writeBucketCLIFile(t, filepath.Join(bucketDir, "broken", "index.md"), "<bucket-docs>\n\nno close tag\n")
	locked := filepath.Join(bucketDir, "locked")
	writeBucketCLIFile(t, filepath.Join(locked, "a.md"), "x\n")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
}

func TestBucketAdd_HardErrorBesideUnpairedRegionExitsNonzero(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	writeUnreadableAndUnpairedDirs(t, filepath.Join(root, "notes"))

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "notes"}, t.TempDir(), root, &out); code != 1 {
		t.Errorf("an unpaired region must not mask an unreadable directory: exit %d, want 1", code)
	}
}

// A malformed region in the user's own index.md is theirs to fix; it must not
// fail the registration.
func TestBucketAdd_UnpairedRegionAloneExitsZero(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	writeBucketCLIFile(t, filepath.Join(root, "notes", "broken", "index.md"), "<bucket-docs>\n\nno close tag\n")

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "notes"}, t.TempDir(), root, &out); code != 0 {
		t.Errorf("unpaired region alone: exit %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(root, "notes", "index.md")); err != nil {
		t.Errorf("bucket root index.md not written: %v", err)
	}
}

// `bucket add -h` once created a bucket named "-h"; a help probe must leave
// every site a real add writes untouched.
func TestBucketAdd_HelpProbeLeavesAllMutationSitesUntouched(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()

	indexPath := filepath.Join(wikiDir, "index.md")
	writeBucketCLIFile(t, indexPath, "# Wiki index\n\nSome content.\n")
	claudeMDPath := filepath.Join(root, "CLAUDE.md")
	writeBucketCLIFile(t, claudeMDPath, "# CLAUDE.md\n\nSome content.\n")

	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	claudeMDBefore, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "add", "--root=" + root, "-h"}, claudeHome, root, &out)
	if code != 0 {
		t.Fatalf("expected exit 0 for -h, got %d; output: %q", code, out.String())
	}

	// Site 1: no realm-root <name>/ directory.
	if _, statErr := os.Lstat(filepath.Join(root, "-h")); statErr == nil {
		t.Error("realm-root -h/ directory was created")
	}

	// Site 2: no <bucket> entry in wiki/index.md; file is byte-identical.
	indexAfter, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(indexBefore, indexAfter) {
		t.Errorf("wiki/index.md was modified by a help probe: before %q, after %q", indexBefore, indexAfter)
	}
	if strings.Contains(string(indexAfter), `name="-h"`) {
		t.Error("wiki/index.md gained a <bucket name=\"-h\"> entry")
	}

	// Site 3: no ## Capture surfaces bullet in the realm CLAUDE.md; file is
	// byte-identical.
	claudeMDAfter, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(claudeMDBefore, claudeMDAfter) {
		t.Errorf("realm CLAUDE.md was modified by a help probe: before %q, after %q", claudeMDBefore, claudeMDAfter)
	}
	if strings.Contains(string(claudeMDAfter), "## Capture surfaces") {
		t.Error("realm CLAUDE.md gained a ## Capture surfaces section")
	}
}

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
	// Sessions enter a bucket at its index.md (docs/spec/bucket-icm-structure.md).
	if want := "`" + filepath.Join(dir, "research", "index.md") + "`"; !strings.Contains(content, want) {
		t.Errorf("expected bullet %s in section, got %q", want, content)
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

func TestCaptureSurfacesSection_Dedupe(t *testing.T) {
	cases := []struct {
		name, seed, bucket string
		wantWrite          bool
	}{
		{"same bucket twice", "", "research", false},
		{"folder-form bullet already present", "## Capture surfaces\n\n- `{dir}/research` <!-- notes -->\n", "research", false},
		{"prefix sibling is a different bucket", "", "res", true},
		{"path mentioned outside the section", "Notes on `{dir}/research/index.md`.\n\n## Capture surfaces\n\n- `{dir}/other/index.md`\n", "research", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			claudeMD := filepath.Join(dir, "CLAUDE.md")
			if tc.seed != "" {
				writeBucketCLIFile(t, claudeMD, strings.ReplaceAll(tc.seed, "{dir}", dir))
			} else if err := writeCaptureSurfacesSection(claudeMD, "research", filepath.Join(dir, "research")); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(claudeMD)

			if err := writeCaptureSurfacesSection(claudeMD, tc.bucket, filepath.Join(dir, tc.bucket)); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(claudeMD)

			bullet := "`" + filepath.Join(dir, tc.bucket, "index.md") + "`"
			gotWrite := strings.Count(string(after), bullet) > strings.Count(string(before), bullet)
			if gotWrite != tc.wantWrite {
				t.Errorf("bullet %s added = %v, want %v\nbefore: %q\nafter:  %q", bullet, gotWrite, tc.wantWrite, before, after)
			}
		})
	}
}

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

// TestBucketIndexStub_ConventionsPromptsForStages verifies that Conventions asks for the
// bucket's stages and names no bucket, since code has no bucket semantics.
func TestBucketIndexStub_ConventionsPromptsForStages(t *testing.T) {
	dir := t.TempDir()
	bucketDir := filepath.Join(dir, "alpha")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := createBucketIndexStub(bucketDir, "alpha"); err != nil {
		t.Fatalf("createBucketIndexStub: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(bucketDir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	_, conventions, ok := strings.Cut(content, "## Conventions\n")
	if !ok {
		t.Fatalf("expected ## Conventions section, got:\n%s", content)
	}
	conventions, _, _ = strings.Cut(conventions, "<bucket-docs")

	for _, want := range []string{"Stages:", "01_raw/", "output"} {
		if !strings.Contains(conventions, want) {
			t.Errorf("Conventions placeholder missing %q:\n%s", want, conventions)
		}
	}
	for _, gone := range []string{"ticket", "triage", "research", "alpha"} {
		if strings.Contains(strings.ToLower(conventions), gone) {
			t.Errorf("Conventions placeholder names a bucket (%q):\n%s", gone, conventions)
		}
	}
}

// TestBucketIndexStub_CarriesOKFFrontmatterAndEmptyRegion verifies the
// reshaped stub carries OKF frontmatter (title, type: Bucket, description
// placeholder) plus a well-formed, empty <bucket-docs> region.
func TestBucketIndexStub_CarriesOKFFrontmatterAndEmptyRegion(t *testing.T) {
	dir := t.TempDir()
	bucketDir := filepath.Join(dir, "research")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := createBucketIndexStub(bucketDir, "research"); err != nil {
		t.Fatalf("createBucketIndexStub: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(bucketDir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.HasPrefix(content, "---\n") {
		t.Fatalf("expected frontmatter block, got:\n%s", content)
	}
	if !strings.Contains(content, "title: research") {
		t.Errorf("expected title: research in frontmatter, got:\n%s", content)
	}
	if !strings.Contains(content, "type: Bucket") {
		t.Errorf("expected type: Bucket in frontmatter, got:\n%s", content)
	}
	if !strings.Contains(content, "description:") {
		t.Errorf("expected description placeholder in frontmatter, got:\n%s", content)
	}

	state, _ := findRegion(content, "bucket-docs")
	if state != regionWellFormed {
		t.Fatalf("expected a well-formed <bucket-docs> region, got state %v; content:\n%s", state, content)
	}
}

// TestBucketIndexStub_ThenRebuildFillsRegion verifies that a subsequent
// RebuildBucketTree call fills the stub's empty <bucket-docs> region in
// place (well-formed replace) rather than appending a second region.
func TestBucketIndexStub_ThenRebuildFillsRegion(t *testing.T) {
	dir := t.TempDir()
	bucketDir := filepath.Join(dir, "research")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := createBucketIndexStub(bucketDir, "research"); err != nil {
		t.Fatalf("createBucketIndexStub: %v", err)
	}
	writeTopicFile(t, filepath.Join(bucketDir, "seo.md"), "---\ntitle: SEO\ndescription: Technical SEO checklist.\n---\n\nBody.\n")

	if _, errs := RebuildBucketTree(bucketDir); errs != nil {
		t.Fatalf("RebuildBucketTree: %v", errs)
	}

	data, err := os.ReadFile(filepath.Join(bucketDir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if strings.Count(content, "<bucket-docs>") != 1 {
		t.Fatalf("expected exactly one <bucket-docs> open tag, got:\n%s", content)
	}
	if !strings.Contains(content, "[SEO](seo.md) - Technical SEO checklist.") {
		t.Errorf("expected topic listing spliced into the region, got:\n%s", content)
	}
	// Prose outside the region (## Conventions, the H1) must survive.
	if !strings.Contains(content, "## Conventions") {
		t.Errorf("expected ## Conventions to survive the rebuild, got:\n%s", content)
	}
}

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

// TestBucketAdd_RegistersAndSplices verifies that `atomic wiki bucket add <name>`
// via wikiAction: splices the block, writes the CLAUDE.md section, creates
// the bucket index.md stub.
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

func TestBucketAdd_ExistingMaterialIndexedAtEveryLevel(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	notes := filepath.Join(root, "notes")
	userFile := filepath.Join(notes, "meetings", "kickoff.md")
	userContent := "---\ntitle: Kickoff\n---\n\nUser notes.\n"
	writeBucketCLIFile(t, userFile, userContent)

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "notes"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add returned %d", code)
	}

	rootIndex, _ := os.ReadFile(filepath.Join(notes, "index.md"))
	if !strings.Contains(string(rootIndex), "(meetings/index.md)") {
		t.Errorf("bucket root index must list the existing subfolder:\n%s", rootIndex)
	}
	sub, err := os.ReadFile(filepath.Join(notes, "meetings", "index.md"))
	if err != nil {
		t.Fatalf("existing subfolder not indexed on registration: %v", err)
	}
	if !strings.Contains(string(sub), "[Kickoff](kickoff.md)") {
		t.Errorf("subfolder index missing its file:\n%s", sub)
	}
	if got, _ := os.ReadFile(userFile); string(got) != userContent {
		t.Errorf("user file changed by registration:\n%s", got)
	}
}

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

// The registry in wiki/index.md is the only record of a bucket, so a name
// already listed there is a double register even with nothing else on disk.
func TestBucketAdd_RefusesNameAlreadyInRegistry(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	indexPath := filepath.Join(wikiDir, "index.md")
	writeBucketCLIFile(t, indexPath, "# Wiki\n\n<wiki-buckets>\n<bucket name=\"mybucket\" path=\""+filepath.Join(root, "mybucket")+"\"/>\n</wiki-buckets>\n")
	realmCLAUDE := filepath.Join(root, "CLAUDE.md")
	writeBucketCLIFile(t, realmCLAUDE, "# Realm\n")

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "mybucket"}, claudeHome, root, &out); code != 1 {
		t.Fatalf("expected exit 1 for a name already in the registry, got %d", code)
	}

	data, _ := os.ReadFile(realmCLAUDE)
	if string(data) != "# Realm\n" {
		t.Errorf("refused add still wrote realm CLAUDE.md:\n%s", data)
	}
	if _, err := os.Lstat(filepath.Join(root, "mybucket")); err == nil {
		t.Error("refused add still created the bucket dir")
	}
}

// Buckets carry no derived state under wiki/: add touches index.md and nothing
// else there.
func TestBucketAdd_WritesNothingUnderWikiButIndex(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "mybucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add returned %d", code)
	}

	entries, err := os.ReadDir(wikiDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "index.md" {
			t.Errorf("bucket add created wiki/%s", e.Name())
		}
	}
}

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

func TestBucketList_PrintsNameAndPath(t *testing.T) {
	root, bucketDir, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")
	writeBucketCLIFile(t, filepath.Join(bucketDir, "a.md"), "content\n")

	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, "testbucket"}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add failed: %d", code)
	}
	out.Reset()

	if code := wikiAction([]string{"bucket", "list", "--root=" + root}, claudeHome, root, &out); code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if want := "testbucket\t" + bucketDir + "\n"; out.String() != want {
		t.Errorf("bucket list output = %q, want %q", out.String(), want)
	}
}

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

	if data, _ := os.ReadFile(filepath.Join(wikiDir, "index.md")); !strings.Contains(string(data), `name="mybucket"`) {
		t.Errorf("bucket not registered in the realm's wiki/index.md:\n%s", data)
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

	if data, _ := os.ReadFile(filepath.Join(wikiDir, "index.md")); !strings.Contains(string(data), `name="mybucket"`) {
		t.Errorf("bucket not registered in the realm's wiki/index.md (cwd=%s realm=%s):\n%s", cwd, realm, data)
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
