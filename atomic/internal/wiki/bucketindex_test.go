package wiki

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTopicFile writes content to path, creating parent directories as needed.
func writeTopicFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestWalkBucketTopics_SimpleTopicAndExclusions(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "index.md"), "# bucket\n")
	writeTopicFile(t, filepath.Join(dir, "seo.md"), "---\ntitle: SEO\n---\n\nBody.\n")
	writeTopicFile(t, filepath.Join(dir, ".DS_Store"), "junk")
	writeTopicFile(t, filepath.Join(dir, "notes.txt"), "not markdown")
	writeTopicFile(t, filepath.Join(dir, "node_modules", "pkg.md"), "should be excluded")

	topics, err := walkBucketTopics(dir)
	if err != nil {
		t.Fatalf("walkBucketTopics: %v", err)
	}
	if len(topics) != 1 {
		t.Fatalf("got %d topics, want 1 (index.md, junk, non-.md, and skipDirs content must be excluded): %+v", len(topics), topics)
	}
	if topics[0].Path != "seo.md" {
		t.Errorf("got path %q, want seo.md", topics[0].Path)
	}
	if topics[0].Router || topics[0].Orphan {
		t.Errorf("plain topic must not be flagged router/orphan: %+v", topics[0])
	}
}

func TestWalkBucketTopics_RouterCollapse(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "coding-agents.md"), "---\ntitle: Coding agents\n---\n\nBody.\n")
	writeTopicFile(t, filepath.Join(dir, "coding-agents", "claude-sdk.md"), "sdk one\n")
	writeTopicFile(t, filepath.Join(dir, "coding-agents", "langchain.md"), "sdk two\n")
	writeTopicFile(t, filepath.Join(dir, "coding-agents", "nested", "deep.md"), "nested doc\n")
	writeTopicFile(t, filepath.Join(dir, "seo.md"), "---\ntitle: SEO\n---\n\nBody.\n")

	topics, err := walkBucketTopics(dir)
	if err != nil {
		t.Fatalf("walkBucketTopics: %v", err)
	}
	if len(topics) != 2 {
		t.Fatalf("got %d topics, want 2 (router subtree must not contribute separate entries): %+v", len(topics), topics)
	}

	// Sorted by path: "coding-agents.md" < "seo.md".
	router := topics[0]
	if router.Path != "coding-agents.md" {
		t.Fatalf("got path %q, want coding-agents.md", router.Path)
	}
	if !router.Router {
		t.Errorf("coding-agents.md must be flagged as router")
	}
	if router.ChildCount != 3 {
		t.Errorf("got child count %d, want 3 (recursive under the subtree)", router.ChildCount)
	}

	for _, tp := range topics {
		if tp.Path == "coding-agents/claude-sdk.md" || tp.Path == "coding-agents/langchain.md" {
			t.Fatalf("subtree file promoted to a top-level topic: %+v", tp)
		}
	}
}

func TestWalkBucketTopics_OrphanSubtree(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "weird-notes", "a.md"), "a\n")
	writeTopicFile(t, filepath.Join(dir, "weird-notes", "b.md"), "b\n")

	topics, err := walkBucketTopics(dir)
	if err != nil {
		t.Fatalf("walkBucketTopics: %v", err)
	}
	if len(topics) != 1 {
		t.Fatalf("got %d topics, want 1 orphan entry: %+v", len(topics), topics)
	}

	orphan := topics[0]
	if !orphan.Orphan {
		t.Errorf("directory with no sibling .md must be flagged orphan")
	}
	if orphan.Router {
		t.Errorf("orphan must not also be flagged router")
	}
	if orphan.Path != "weird-notes" {
		t.Errorf("got path %q, want weird-notes", orphan.Path)
	}
	if orphan.Title != "weird-notes" {
		t.Errorf("got title %q, want dir stem weird-notes", orphan.Title)
	}
	if orphan.Description != "" {
		t.Errorf("orphan must have no description, got %q", orphan.Description)
	}
	if orphan.ChildCount != 2 {
		t.Errorf("got child count %d, want 2", orphan.ChildCount)
	}
	if orphan.Indexed {
		t.Errorf("orphan (no frontmatter possible) must be unindexed")
	}
}

func TestDeriveTitle_FrontmatterRung(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seo.md")
	writeTopicFile(t, path, "---\ntitle: Technical SEO\n---\n\n# Something else\n")

	topic := readTopicMeta(path)
	if topic.Title != "Technical SEO" {
		t.Errorf("got title %q, want frontmatter title", topic.Title)
	}
}

func TestDeriveTitle_H1Rung(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "directus-export.md")
	writeTopicFile(t, path, "# Directus export\n\nFull CMS collection dump pulled 2026-07-02, awaiting synthesis.\n")

	topic := readTopicMeta(path)
	if topic.Title != "Directus export" {
		t.Errorf("got title %q, want first H1", topic.Title)
	}
}

func TestDeriveTitle_H1RungSkipsFencedHeading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "example.md")
	writeTopicFile(t, path, "```\n# fake heading inside a fence\n```\n\n# Real Title\n\nBody prose.\n")

	topic := readTopicMeta(path)
	if topic.Title != "Real Title" {
		t.Errorf("got title %q, want %q (a # line inside a fenced code block must not be picked as the title)", topic.Title, "Real Title")
	}
}

func TestDeriveTitle_FilenameStemRung(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "call-notes-2026-07-11.md")
	writeTopicFile(t, path, "no heading, no frontmatter, just prose that is too short.\n")

	topic := readTopicMeta(path)
	if topic.Title != "call-notes-2026-07-11" {
		t.Errorf("got title %q, want filename stem verbatim (kebab-case preserved)", topic.Title)
	}
}

func TestReadTopicMeta_DescriptionFrontmatterRung(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seo.md")
	writeTopicFile(t, path, "---\ndescription: Technical SEO checklist for the marketing sites.\n---\n\nBody prose here.\n")

	topic := readTopicMeta(path)
	if topic.Description != "Technical SEO checklist for the marketing sites." {
		t.Errorf("got description %q, want frontmatter description", topic.Description)
	}
}

func TestReadTopicMeta_DescriptionProseLineRung(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "directus-export.md")
	writeTopicFile(t, path, "# Directus export\n\nFull CMS collection dump pulled 2026-07-02, awaiting synthesis.\n")

	topic := readTopicMeta(path)
	if topic.Description != "Full CMS collection dump pulled 2026-07-02, awaiting synthesis." {
		t.Errorf("got description %q, want first prose line", topic.Description)
	}
}

func TestReadTopicMeta_DescriptionExhaustedIsLinkOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "call-notes-2026-07-11.md")
	writeTopicFile(t, path, "")

	topic := readTopicMeta(path)
	if topic.Description != "" {
		t.Fatalf("got description %q, want empty (ladder exhausted)", topic.Description)
	}

	topic.Path = "call-notes-2026-07-11.md"
	entry := listEntry(topic)
	want := "- [call-notes-2026-07-11](call-notes-2026-07-11.md)"
	if entry != want {
		t.Errorf("got entry %q, want link-only form %q", entry, want)
	}
}

func TestDeriveTags(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
		want []string
	}{
		{"list of strings", map[string]any{"tags": []any{"agents", "sdk"}}, []string{"agents", "sdk"}},
		{"bare string", map[string]any{"tags": "solo"}, []string{"solo"}},
		{"absent", map[string]any{"title": "x"}, nil},
		{"malformed: map", map[string]any{"tags": map[string]any{"a": "b"}}, nil},
		{"malformed: mixed-type list", map[string]any{"tags": []any{"ok", 5}}, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deriveTags(c.meta)
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestReadTopicMeta_IndexedFlag(t *testing.T) {
	dir := t.TempDir()

	indexedPath := filepath.Join(dir, "indexed.md")
	writeTopicFile(t, indexedPath, "---\nstatus: active\n---\n\nBody.\n")
	if !readTopicMeta(indexedPath).Indexed {
		t.Error("a frontmatter block with a recognized key must be indexed")
	}

	noFrontmatterPath := filepath.Join(dir, "no-frontmatter.md")
	writeTopicFile(t, noFrontmatterPath, "# Heading\n\nBody.\n")
	if readTopicMeta(noFrontmatterPath).Indexed {
		t.Error("no frontmatter block at all must be unindexed")
	}

	unrecognizedPath := filepath.Join(dir, "unrecognized.md")
	writeTopicFile(t, unrecognizedPath, "---\ncustom_key: value\n---\n\nBody.\n")
	if readTopicMeta(unrecognizedPath).Indexed {
		t.Error("a frontmatter block with no recognized key must be unindexed")
	}
}

func TestRenderBucketDocs_IndexedThenUnindexedGrouping(t *testing.T) {
	topics := []BucketTopic{
		{Path: "coding-agents.md", Title: "Coding agents", Description: "Survey.", Tags: []string{"agents", "sdk"}, Indexed: true, Router: true, ChildCount: 4},
		{Path: "seo.md", Title: "SEO", Description: "Technical SEO checklist.", Tags: []string{"seo"}, Indexed: true},
		{Path: "directus-export.md", Title: "Directus export", Description: "CMS dump.", Indexed: false},
		{Path: "call-notes-2026-07-11.md", Title: "call-notes-2026-07-11", Indexed: false},
	}

	got := renderBucketDocs(topics)
	want := "## Docs\n" +
		"\n- [Coding agents](coding-agents.md) - Survey. · tags: agents, sdk · router (4 docs)" +
		"\n- [SEO](seo.md) - Technical SEO checklist. · tags: seo" +
		"\n\n### Unindexed\n" +
		"\n- [Directus export](directus-export.md) - CMS dump." +
		"\n- [call-notes-2026-07-11](call-notes-2026-07-11.md)"

	if got != want {
		t.Errorf("renderBucketDocs mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderBucketDocs_NoUnindexedOmitsHeading(t *testing.T) {
	topics := []BucketTopic{
		{Path: "seo.md", Title: "SEO", Description: "Technical SEO checklist.", Indexed: true},
	}
	got := renderBucketDocs(topics)
	if got != "## Docs\n\n- [SEO](seo.md) - Technical SEO checklist." {
		t.Errorf("unexpected render with no unindexed topics: %q", got)
	}
	if wantAbsent := "### Unindexed"; strings.Contains(got, wantAbsent) {
		t.Errorf("### Unindexed heading must be omitted when there are no unindexed topics: %q", got)
	}
}

func TestRebuildBucketIndex_PreservesOutsideProseWithArbitraryEdits(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "seo.md"), "---\ntitle: SEO\ndescription: Technical SEO checklist.\n---\n\nBody.\n")

	original := "# research\n\n" +
		"Authored research reports.\n\n" +
		"## Conventions\n\n" +
		"One topic per file.\n\n" +
		"<bucket-docs>\n\n" +
		"## Docs\n\n- stale entry\n\n" +
		"</bucket-docs>\n\n" +
		"A user-typed trailer paragraph.\n"
	writeTopicFile(t, filepath.Join(dir, "index.md"), original)

	if err := RebuildBucketIndex(dir); err != nil {
		t.Fatalf("RebuildBucketIndex: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}

	want := "# research\n\n" +
		"Authored research reports.\n\n" +
		"## Conventions\n\n" +
		"One topic per file.\n\n" +
		"<bucket-docs>\n\n" +
		"## Docs\n\n- [SEO](seo.md) - Technical SEO checklist.\n\n" +
		"</bucket-docs>\n\n" +
		"A user-typed trailer paragraph.\n"

	if string(got) != want {
		t.Errorf("outside-region prose not preserved\ngot:  %q\nwant: %q", string(got), want)
	}
}

func TestRebuildBucketIndex_Idempotent(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "seo.md"), "---\ntitle: SEO\ndescription: Technical SEO checklist.\n---\n\nBody.\n")

	if err := RebuildBucketIndex(dir); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("read after first rebuild: %v", err)
	}

	if err := RebuildBucketIndex(dir); err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("read after second rebuild: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("rebuild not idempotent\nfirst:  %q\nsecond: %q", first, second)
	}
}

func TestRebuildBucketIndex_AbsentIndexAppendsRegion(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "seo.md"), "---\ntitle: SEO\ndescription: Technical SEO checklist.\n---\n\nBody.\n")

	if err := RebuildBucketIndex(dir); err != nil {
		t.Fatalf("RebuildBucketIndex: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	want := "<bucket-docs>\n\n## Docs\n\n- [SEO](seo.md) - Technical SEO checklist.\n\n</bucket-docs>\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRebuildBucketIndex_UnpairedRegionLeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "seo.md"), "---\ntitle: SEO\n---\n\nBody.\n")

	original := "# research\n\n<bucket-docs>\n\nstray content with no close tag\n"
	writeTopicFile(t, filepath.Join(dir, "index.md"), original)

	err := RebuildBucketIndex(dir)
	if !errors.Is(err, errUnpairedRegion) {
		t.Fatalf("expected errUnpairedRegion, got %v", err)
	}

	got, readErr := os.ReadFile(filepath.Join(dir, "index.md"))
	if readErr != nil {
		t.Fatalf("read index.md: %v", readErr)
	}
	if string(got) != original {
		t.Errorf("index.md must be left untouched on unpaired region\ngot:  %q\nwant: %q", got, original)
	}
}

func TestRenderBucketList_SortedWithDescriptionsAndMissing(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")

	alphaDir := filepath.Join(root, "alpha")
	writeTopicFile(t, filepath.Join(alphaDir, "index.md"), "# Alpha bucket\n\nAlpha bucket description sentence here.\n")

	zetaDir := filepath.Join(root, "zeta")
	writeTopicFile(t, filepath.Join(zetaDir, "index.md"), "---\ndescription: Zeta bucket frontmatter description\n---\n\nBody.\n")

	missingDir := filepath.Join(root, "missing") // never created

	entries := []BucketEntry{
		{Name: "zeta", Path: zetaDir},
		{Name: "missing", Path: missingDir},
		{Name: "alpha", Path: alphaDir},
	}

	got := renderBucketList(wikiDir, entries)
	want := "## Buckets\n\n" +
		"- [alpha](../alpha) - Alpha bucket description sentence here.\n" +
		"- [missing](../missing) · (missing)\n" +
		"- [zeta](../zeta) - Zeta bucket frontmatter description"

	if got != want {
		t.Errorf("renderBucketList mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderBucketList_ZeroEntriesHeadingOnly(t *testing.T) {
	got := renderBucketList(t.TempDir(), nil)
	if got != "## Buckets" {
		t.Errorf("got %q, want heading-only %q", got, "## Buckets")
	}
}

func TestRebuildAllBucketIndexes_SplicesRealmListAndSkipsMissingDir(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(wikiDir, "index.md")

	alphaDir := filepath.Join(root, "alpha")
	writeTopicFile(t, filepath.Join(alphaDir, "index.md"), "---\ndescription: Alpha bucket for research notes\n---\n\n## Conventions\n")
	writeTopicFile(t, filepath.Join(alphaDir, "seo.md"), "---\ntitle: SEO\n---\n\nBody.\n")

	ghostDir := filepath.Join(root, "ghost") // registered but never created

	if err := spliceBucketEntry(indexPath, "alpha", alphaDir); err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	if err := spliceBucketEntry(indexPath, "ghost", ghostDir); err != nil {
		t.Fatalf("register ghost: %v", err)
	}

	if err := RebuildAllBucketIndexes(root, wikiDir); err != nil {
		t.Fatalf("RebuildAllBucketIndexes: %v", err)
	}

	alphaIndex, err := os.ReadFile(filepath.Join(alphaDir, "index.md"))
	if err != nil {
		t.Fatalf("read alpha index.md: %v", err)
	}
	if !strings.Contains(string(alphaIndex), "<bucket-docs>") || !strings.Contains(string(alphaIndex), "[SEO](seo.md)") {
		t.Errorf("alpha bucket-docs region not rebuilt:\n%s", alphaIndex)
	}

	realmIndex, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read realm index.md: %v", err)
	}
	realm := string(realmIndex)

	if !strings.Contains(realm, "<wiki-bucket-list>") {
		t.Errorf("realm index.md missing <wiki-bucket-list> region:\n%s", realm)
	}
	if !strings.Contains(realm, "- [alpha](../alpha) - Alpha bucket for research notes") {
		t.Errorf("realm bucket list missing alpha entry with description:\n%s", realm)
	}
	if !strings.Contains(realm, "- [ghost](../ghost) · (missing)") {
		t.Errorf("realm bucket list missing ghost (missing) entry:\n%s", realm)
	}
	// <wiki-buckets> registry itself must be untouched.
	if !strings.Contains(realm, fmt.Sprintf(`<bucket name="alpha" path=%q/>`, alphaDir)) {
		t.Errorf("<wiki-buckets> alpha entry mutated or missing:\n%s", realm)
	}
	if !strings.Contains(realm, fmt.Sprintf(`<bucket name="ghost" path=%q/>`, ghostDir)) {
		t.Errorf("<wiki-buckets> ghost entry mutated or missing:\n%s", realm)
	}
}

func TestRebuildAllBucketIndexes_BrokenBucketDoesNotBlockOthersOrRealmSplice(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(wikiDir, "index.md")

	brokenDir := filepath.Join(root, "broken")
	brokenOriginal := "# broken\n\n<bucket-docs>\n\nstray content, no close tag\n"
	writeTopicFile(t, filepath.Join(brokenDir, "index.md"), brokenOriginal)

	okDir := filepath.Join(root, "ok")
	writeTopicFile(t, filepath.Join(okDir, "index.md"), "---\ndescription: OK bucket\n---\n\n## Conventions\n")

	if err := spliceBucketEntry(indexPath, "broken", brokenDir); err != nil {
		t.Fatalf("register broken: %v", err)
	}
	if err := spliceBucketEntry(indexPath, "ok", okDir); err != nil {
		t.Fatalf("register ok: %v", err)
	}

	err := RebuildAllBucketIndexes(root, wikiDir)
	if !errors.Is(err, errUnpairedRegion) {
		t.Fatalf("expected errUnpairedRegion surfaced, got %v", err)
	}

	brokenAfter, readErr := os.ReadFile(filepath.Join(brokenDir, "index.md"))
	if readErr != nil {
		t.Fatalf("read broken index.md: %v", readErr)
	}
	if string(brokenAfter) != brokenOriginal {
		t.Errorf("broken bucket index.md must be left untouched\ngot:  %q\nwant: %q", brokenAfter, brokenOriginal)
	}

	okAfter, readErr := os.ReadFile(filepath.Join(okDir, "index.md"))
	if readErr != nil {
		t.Fatalf("read ok index.md: %v", readErr)
	}
	if !strings.Contains(string(okAfter), "<bucket-docs>") {
		t.Errorf("ok bucket-docs region must be rebuilt despite sibling failure:\n%s", okAfter)
	}

	realmAfter, readErr := os.ReadFile(indexPath)
	if readErr != nil {
		t.Fatalf("read realm index.md: %v", readErr)
	}
	if !strings.Contains(string(realmAfter), "<wiki-bucket-list>") {
		t.Errorf("realm bucket list must still be spliced despite per-bucket failure:\n%s", realmAfter)
	}
}

func TestRebuildAllBucketIndexes_ZeroBucketsNoSplice(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(wikiDir, "index.md")
	original := "# wiki index\n\nNo buckets registered.\n"
	if err := os.WriteFile(indexPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RebuildAllBucketIndexes(root, wikiDir); err != nil {
		t.Fatalf("RebuildAllBucketIndexes: %v", err)
	}

	got, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	if string(got) != original {
		t.Errorf("index.md must be untouched with zero registered buckets\ngot:  %q\nwant: %q", got, original)
	}
	if strings.Contains(string(got), "<wiki-bucket-list>") {
		t.Errorf("must not splice an empty ## Buckets region:\n%s", got)
	}
}
