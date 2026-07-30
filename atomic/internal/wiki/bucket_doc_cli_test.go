package wiki

// bucket_doc_cli_test.go — CP5 tests for the `atomic wiki bucket doc|skill|index`
// CLI verbs (action.go's wikiBucketDocAction / wikiBucketSkillAction /
// wikiBucketIndexAction). Exercises the CLI dispatch layer only — the
// underlying scaffold/index logic (ScaffoldBucketDoc, ScaffoldBucketSkill,
// RebuildBucketIndex, RebuildAllBucketIndexes) is covered by bucketdoc_test.go
// and bucketindex_test.go.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// registerBucketCLI registers a bucket named name under root via the real
// `bucket add` CLI path (mirrors production usage — a doc/skill/index verb
// should only ever see buckets registered this way).
func registerBucketCLI(t *testing.T, root, claudeHome, name string) {
	t.Helper()
	var out bytes.Buffer
	if code := wikiAction([]string{"bucket", "add", "--root=" + root, name}, claudeHome, root, &out); code != 0 {
		t.Fatalf("bucket add %q setup failed: %d; output: %q", name, code, out.String())
	}
}

// ---- wiki bucket doc ----

func TestBucketDoc_HappyPathWritesFile(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	registerBucketCLI(t, root, claudeHome, "research")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "doc", "--root=" + root, "research", "seo"}, claudeHome, root, &out)
	if code != 0 {
		t.Fatalf("bucket doc: expected exit 0, got %d; output: %q", code, out.String())
	}

	target := filepath.Join(root, "research", "seo.md")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("scaffolded doc not written: %v", err)
	}
	if !strings.Contains(string(data), "title: seo") {
		t.Errorf("expected title placeholder derived from slug, got:\n%s", string(data))
	}
	if !strings.Contains(out.String(), target) {
		t.Errorf("expected written path printed to stdout, got: %q", out.String())
	}
}

func TestBucketDoc_CollisionRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	registerBucketCLI(t, root, claudeHome, "research")

	target := filepath.Join(root, "research", "seo.md")
	original := "# pre-existing content\n"
	writeBucketCLIFile(t, target, original)

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "doc", "--root=" + root, "research", "seo"}, claudeHome, root, &out)
	if code == 0 {
		t.Fatal("expected non-zero exit on collision")
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Errorf("collision must never overwrite; got:\n%s", string(data))
	}
}

func TestBucketDoc_UnregisteredBucketRejectedListingNames(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	registerBucketCLI(t, root, claudeHome, "research")
	registerBucketCLI(t, root, claudeHome, "raw")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "doc", "--root=" + root, "ghost", "seo"}, claudeHome, root, &out)
	if code == 0 {
		t.Fatal("expected non-zero exit for unregistered bucket")
	}

	// Nothing should have been written.
	if _, err := os.Lstat(filepath.Join(root, "ghost")); err == nil {
		t.Error("unregistered bucket dir must not be created")
	}
}

func TestBucketDoc_RouterFlagScaffoldsSubtree(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	registerBucketCLI(t, root, claudeHome, "research")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "doc", "--root=" + root, "research", "seo", "--router"}, claudeHome, root, &out)
	if code != 0 {
		t.Fatalf("bucket doc --router: expected exit 0, got %d; output: %q", code, out.String())
	}

	subtreeClaude := filepath.Join(root, "research", "seo", "CLAUDE.md")
	if _, err := os.Lstat(subtreeClaude); err != nil {
		t.Errorf("router subtree CLAUDE.md not created: %v", err)
	}
}

func TestBucketDoc_MissingArgsUsageError(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	registerBucketCLI(t, root, claudeHome, "research")

	var out bytes.Buffer
	// Missing slug.
	code := wikiAction([]string{"bucket", "doc", "--root=" + root, "research"}, claudeHome, root, &out)
	if code == 0 {
		t.Error("expected non-zero exit for missing slug argument")
	}
}

// ---- wiki bucket skill ----

func TestBucketSkill_WritesFileAndReportsNote(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	registerBucketCLI(t, root, claudeHome, "research")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "skill", "--root=" + root, "research"}, claudeHome, root, &out)
	if code != 0 {
		t.Fatalf("bucket skill: expected exit 0, got %d; output: %q", code, out.String())
	}

	target := filepath.Join(root, ".claude", "skills", "research-management", "SKILL.md")
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("skill file not written: %v", err)
	}
	if !strings.Contains(out.String(), target) {
		t.Errorf("expected written path printed to stdout, got: %q", out.String())
	}
	if !strings.Contains(out.String(), "realm root") {
		t.Errorf("expected the realm-root loading note, got: %q", out.String())
	}
}

func TestBucketSkill_NoOpWhenPresent(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	registerBucketCLI(t, root, claudeHome, "research")

	var first bytes.Buffer
	if code := wikiAction([]string{"bucket", "skill", "--root=" + root, "research"}, claudeHome, root, &first); code != 0 {
		t.Fatalf("first skill call failed: %d", code)
	}

	target := filepath.Join(root, ".claude", "skills", "research-management", "SKILL.md")
	info1, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}

	var second bytes.Buffer
	code := wikiAction([]string{"bucket", "skill", "--root=" + root, "research"}, claudeHome, root, &second)
	if code != 0 {
		t.Fatalf("second skill call: expected exit 0, got %d; output: %q", code, second.String())
	}
	if !strings.Contains(second.String(), "already exists") {
		t.Errorf("expected 'already exists' message on no-op, got: %q", second.String())
	}

	info2, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info1.ModTime() != info2.ModTime() {
		t.Error("skill file was rewritten on second call — must be a no-op")
	}
}

func TestBucketSkill_UnregisteredBucketRejected(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "skill", "--root=" + root, "ghost"}, claudeHome, root, &out)
	if code == 0 {
		t.Fatal("expected non-zero exit for unregistered bucket")
	}
}

// ---- wiki bucket index ----

func TestBucketIndex_AllRebuildsEveryBucketAndRealmList(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	registerBucketCLI(t, root, claudeHome, "research")
	registerBucketCLI(t, root, claudeHome, "raw")
	writeBucketCLIFile(t, filepath.Join(root, "research", "seo.md"), "---\ntitle: SEO\ndescription: Technical SEO checklist.\n---\n\nBody.\n")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "index", "--root=" + root}, claudeHome, root, &out)
	if code != 0 {
		t.Fatalf("bucket index (all): expected exit 0, got %d; output: %q", code, out.String())
	}

	// Per-bucket counts reported for both buckets.
	if !strings.Contains(out.String(), "research: 1 indexed, 0 unindexed") {
		t.Errorf("expected research counts in output, got: %q", out.String())
	}
	if !strings.Contains(out.String(), "raw: 0 indexed, 0 unindexed") {
		t.Errorf("expected raw counts in output, got: %q", out.String())
	}

	// Bucket index.md region actually rebuilt.
	data, err := os.ReadFile(filepath.Join(root, "research", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[SEO](seo.md) - Technical SEO checklist.") {
		t.Errorf("expected topic listing spliced into research/index.md, got:\n%s", string(data))
	}

	// Realm <wiki-bucket-list> region rebuilt too.
	realmData, err := os.ReadFile(filepath.Join(root, "wiki", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(realmData), "<wiki-bucket-list>") {
		t.Errorf("expected <wiki-bucket-list> region in wiki/index.md, got:\n%s", string(realmData))
	}
	if !strings.Contains(string(realmData), "[research]") || !strings.Contains(string(realmData), "[raw]") {
		t.Errorf("expected both buckets listed in realm list, got:\n%s", string(realmData))
	}
}

func TestBucketIndex_SingleBucketReportsCountsAndRebuildsOnlyThatRegion(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	registerBucketCLI(t, root, claudeHome, "research")
	writeBucketCLIFile(t, filepath.Join(root, "research", "seo.md"), "---\ntitle: SEO\ndescription: Technical SEO checklist.\n---\n\nBody.\n")
	writeBucketCLIFile(t, filepath.Join(root, "research", "untagged.md"), "No frontmatter here.\n")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "index", "--root=" + root, "research"}, claudeHome, root, &out)
	if code != 0 {
		t.Fatalf("bucket index research: expected exit 0, got %d; output: %q", code, out.String())
	}

	if !strings.Contains(out.String(), "research: 1 indexed, 1 unindexed") {
		t.Errorf("expected indexed/unindexed counts in output, got: %q", out.String())
	}

	data, err := os.ReadFile(filepath.Join(root, "research", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "### Unindexed") {
		t.Errorf("expected Unindexed sub-heading in rebuilt region, got:\n%s", string(data))
	}
}

func TestBucketIndex_SingleBucketDoesNotReWalkOrReReportOtherBuckets(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	registerBucketCLI(t, root, claudeHome, "research")
	registerBucketCLI(t, root, claudeHome, "broken")
	writeBucketCLIFile(t, filepath.Join(root, "research", "seo.md"), "---\ntitle: SEO\ndescription: Technical SEO checklist.\n---\n\nBody.\n")

	// Corrupt the broken bucket's index.md with an unpaired <bucket-docs>
	// region AFTER registration (bucket add's own stub is well-formed).
	brokenIndex := filepath.Join(root, "broken", "index.md")
	brokenOriginal := "# broken\n\n<bucket-docs>\n\nstray content, no close tag\n"
	writeBucketCLIFile(t, brokenIndex, brokenOriginal)

	// Capture stderr to prove the broken bucket is never re-reported.
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "index", "--root=" + root, "research"}, claudeHome, root, &out)

	w.Close()
	os.Stderr = oldStderr
	stderrBytes := make([]byte, 4096)
	n, _ := r.Read(stderrBytes)
	stderrOutput := string(stderrBytes[:n])

	if code != 0 {
		t.Fatalf("bucket index research: expected exit 0, got %d; stdout: %q stderr: %q", code, out.String(), stderrOutput)
	}
	if strings.Contains(stderrOutput, "broken") {
		t.Errorf("single-bucket rebuild must not re-walk or re-report the unrelated broken bucket; stderr: %q", stderrOutput)
	}

	// The broken bucket's index.md must be left completely untouched — proof
	// that no re-walk of the other bucket happened.
	brokenAfter, err := os.ReadFile(brokenIndex)
	if err != nil {
		t.Fatalf("read broken index.md: %v", err)
	}
	if string(brokenAfter) != brokenOriginal {
		t.Errorf("broken bucket index.md must be untouched by a single-bucket rebuild\ngot:  %q\nwant: %q", brokenAfter, brokenOriginal)
	}

	// The target bucket's own <bucket-docs> region must still be rebuilt.
	researchData, err := os.ReadFile(filepath.Join(root, "research", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(researchData), "[SEO](seo.md) - Technical SEO checklist.") {
		t.Errorf("expected topic listing spliced into research/index.md, got:\n%s", string(researchData))
	}

	// The realm <wiki-bucket-list> region must still be refreshed, listing
	// both buckets.
	realmData, err := os.ReadFile(filepath.Join(root, "wiki", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(realmData), "<wiki-bucket-list>") {
		t.Errorf("expected <wiki-bucket-list> region in wiki/index.md, got:\n%s", string(realmData))
	}
	if !strings.Contains(string(realmData), "[research]") || !strings.Contains(string(realmData), "[broken]") {
		t.Errorf("expected both buckets listed in realm list, got:\n%s", string(realmData))
	}
}

func TestBucketIndex_UnregisteredBucketRejected(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "index", "--root=" + root, "ghost"}, claudeHome, root, &out)
	if code == 0 {
		t.Fatal("expected non-zero exit for unregistered bucket")
	}
}

func TestBucketIndex_NoBucketsIsNoop(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBucketCLIFile(t, filepath.Join(root, "wiki", "index.md"), "# Wiki\n")

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "index", "--root=" + root}, claudeHome, root, &out)
	if code != 0 {
		t.Errorf("expected exit 0 with no registered buckets, got %d; output: %q", code, out.String())
	}
}
