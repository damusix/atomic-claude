package wiki

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
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

func TestListEntry_LongDescriptionCutAtWordBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.md")
	desc := strings.Repeat("word ", 23) + "remove or replace a source and restart its crawl."
	writeTopicFile(t, path, "---\ndescription: "+desc+"\ntags: [ingest]\n---\n")

	topic := readTopicMeta(path)
	topic.Path = "sources.md"
	want := "- [sources](sources.md) - " + strings.Repeat("word ", 22) + "word… · tags: ingest"
	if got := listEntry(topic); got != want {
		t.Errorf("got entry\n%q\nwant\n%q", got, want)
	}
}

func TestTruncate(t *testing.T) {
	words := strings.Repeat("abcd ", 23)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"cut at word end keeps the word", words + "abcd more words", words + "abcd…"},
		{"double space at cut leaves no space before ellipsis", words + "abc  more", words + "abc…"},
		{"single long word cut mid-word", strings.Repeat("x", 130), strings.Repeat("x", 119) + "…"},
		{"leading space before long word cut mid-word", " " + strings.Repeat("x", 130), " " + strings.Repeat("x", 118) + "…"},
		{"exactly at limit in runes is unchanged", strings.Repeat("é ", 59) + "éé", strings.Repeat("é ", 59) + "éé"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := truncate(c.in, 120); got != c.want {
				t.Errorf("got\n%q\nwant\n%q", got, c.want)
			}
		})
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

func TestRebuildBucketTree_PreservesOutsideProseWithArbitraryEdits(t *testing.T) {
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

	if _, errs := RebuildBucketTree(dir); errs != nil {
		t.Fatalf("RebuildBucketTree: %v", errs)
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

func TestRebuildBucketTree_Idempotent(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "seo.md"), "---\ntitle: SEO\ndescription: Technical SEO checklist.\n---\n\nBody.\n")

	if _, errs := RebuildBucketTree(dir); errs != nil {
		t.Fatalf("first rebuild: %v", errs)
	}
	first, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("read after first rebuild: %v", err)
	}

	if _, errs := RebuildBucketTree(dir); errs != nil {
		t.Fatalf("second rebuild: %v", errs)
	}
	second, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("read after second rebuild: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("rebuild not idempotent\nfirst:  %q\nsecond: %q", first, second)
	}
}

func TestRebuildBucketTree_AbsentIndexAppendsRegion(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "seo.md"), "---\ntitle: SEO\ndescription: Technical SEO checklist.\n---\n\nBody.\n")

	if _, errs := RebuildBucketTree(dir); errs != nil {
		t.Fatalf("RebuildBucketTree: %v", errs)
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

func TestRebuildBucketTree_UnpairedRegionLeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "seo.md"), "---\ntitle: SEO\n---\n\nBody.\n")

	original := "# research\n\n<bucket-docs>\n\nstray content with no close tag\n"
	writeTopicFile(t, filepath.Join(dir, "index.md"), original)

	_, errs := RebuildBucketTree(dir)
	if err := errors.Join(errs...); !errors.Is(err, errUnpairedRegion) {
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

func readIndex(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("read %s/index.md: %v", dir, err)
	}
	return string(data)
}

// An agent descends a bucket one index.md at a time, so every walkable
// directory needs its own listing, created when absent.
func TestRebuildBucketTree_IndexesEveryWalkableDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "seo-audit", "01_raw", "crawl.md"), "---\ntitle: Crawl\n---\n")
	writeTopicFile(t, filepath.Join(dir, "seo-audit", "01_raw", "deep", "page.md"), "---\ntitle: Page\n---\n")
	writeTopicFile(t, filepath.Join(dir, "node_modules", "pkg", "readme.md"), "x\n")
	writeTopicFile(t, filepath.Join(dir, ".obsidian", "notes.md"), "x\n")
	writeTopicFile(t, filepath.Join(dir, "seo-audit", ".cache", "c.md"), "x\n")

	if _, errs := RebuildBucketTree(dir); errs != nil {
		t.Fatalf("RebuildBucketTree: %v", errs)
	}

	for _, d := range []string{"", "seo-audit", "seo-audit/01_raw", "seo-audit/01_raw/deep"} {
		got := readIndex(t, filepath.Join(dir, d))
		if state, _ := findRegion(got, "bucket-docs"); state != regionWellFormed {
			t.Errorf("%q/index.md has no well-formed <bucket-docs> region:\n%s", d, got)
		}
	}
	if !strings.Contains(readIndex(t, filepath.Join(dir, "seo-audit", "01_raw")), "[Crawl](crawl.md)") {
		t.Errorf("01_raw listing missing its file")
	}
	if !strings.Contains(readIndex(t, filepath.Join(dir, "seo-audit", "01_raw", "deep")), "[Page](page.md)") {
		t.Errorf("deep listing missing its file")
	}

	for _, skipped := range []string{"node_modules", "node_modules/pkg", ".obsidian", "seo-audit/.cache"} {
		if _, err := os.Stat(filepath.Join(dir, skipped, "index.md")); !os.IsNotExist(err) {
			t.Errorf("skipped directory %q gained an index.md (err %v)", skipped, err)
		}
	}
	root := readIndex(t, dir)
	if strings.Contains(root, "node_modules") || strings.Contains(root, ".obsidian") {
		t.Errorf("skipped directories listed in root index:\n%s", root)
	}
}

func TestWalkDirChildren_FilesAndSubdirectories(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "index.md"), "# self\n")
	writeTopicFile(t, filepath.Join(dir, "notes.md"), "---\ntitle: Notes\n---\n")
	writeTopicFile(t, filepath.Join(dir, "report.pdf"), "binary")
	writeTopicFile(t, filepath.Join(dir, ".DS_Store"), "junk")
	writeTopicFile(t, filepath.Join(dir, "titled", "index.md"), "---\ntitle: Titled stage\ndescription: Collected sources.\ntags: [raw]\n---\n")
	writeTopicFile(t, filepath.Join(dir, "bare", "index.md"), "---\ncustom: x\n---\n")
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := walkDirChildren(dir)
	if err != nil {
		t.Fatalf("walkDirChildren: %v", err)
	}

	byPath := map[string]BucketTopic{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want notes.md, titled, bare, empty: %+v", len(entries), entries)
	}

	titled := byPath["titled"]
	if !titled.Dir || titled.Title != "Titled stage" || titled.Description != "Collected sources." || !titled.Indexed || len(titled.Tags) != 1 {
		t.Errorf("subdirectory entry must take its meta from its index.md: %+v", titled)
	}
	if got, want := listEntry(titled), "- [Titled stage](titled/index.md) - Collected sources. · tags: raw"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if empty := byPath["empty"]; !empty.Dir || empty.Title != "empty" || empty.Indexed {
		t.Errorf("subdirectory with no index.md falls back to its name and is unindexed: %+v", empty)
	}
	if bare := byPath["bare"]; bare.Title != "bare" || bare.Indexed {
		t.Errorf("subdirectory whose index.md has no recognized key is unindexed: %+v", bare)
	}

	rendered := renderBucketDocs(entries)
	unindexed := rendered[strings.Index(rendered, "### Unindexed"):]
	if !strings.Contains(unindexed, "[bare](bare/index.md)") {
		t.Errorf("unrecognized-frontmatter subdirectory must list under ### Unindexed:\n%s", rendered)
	}
}

// macOS writes AppleDouble "._<name>" companions onto non-HFS volumes.
func TestWalkDirChildren_SkipsDotPrefixedFiles(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "notes.md"), "---\ntitle: Notes\n---\n")
	writeTopicFile(t, filepath.Join(dir, "._notes.md"), "\x00\x05\x16\x07")
	writeTopicFile(t, filepath.Join(dir, ".draft.md"), "x\n")

	entries, err := walkDirChildren(dir)
	if err != nil {
		t.Fatalf("walkDirChildren: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != "notes.md" {
		t.Errorf("dot-prefixed files must be skipped: %+v", entries)
	}
}

func TestChildLess_NumericPrefixOrder(t *testing.T) {
	names := []string{"zeta", "10_publish", "alpha", "9_x", "02_findings", "01_raw", "1_a", "01_a"}
	sort.Slice(names, func(i, j int) bool { return childLess(names[i], names[j]) })
	want := []string{"01_a", "01_raw", "1_a", "02_findings", "9_x", "10_publish", "alpha", "zeta"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestChildLess_HugeNumericPrefix(t *testing.T) {
	big := "99999999999999999999999_x"
	if !childLess("2_x", big) || childLess(big, "2_x") {
		t.Errorf("an integer prefix beyond int64 must still order by value")
	}
}

// Buckets list a same-slug file and folder side by side; the docs-index router
// collapse does not apply.
func TestRebuildBucketTree_FlatSlugBesideDirectoryListsBoth(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "seo.md"), "---\ntitle: SEO\n---\n")
	writeTopicFile(t, filepath.Join(dir, "seo", "extra.md"), "---\ntitle: Extra\n---\n")

	if _, errs := RebuildBucketTree(dir); errs != nil {
		t.Fatalf("RebuildBucketTree: %v", errs)
	}
	root := readIndex(t, dir)
	if !strings.Contains(root, "[SEO](seo.md)") || !strings.Contains(root, "[seo](seo/index.md)") {
		t.Errorf("want both the file and the directory listed:\n%s", root)
	}
	if strings.Contains(root, "router") || strings.Contains(root, "orphan") {
		t.Errorf("router/orphan markers must not appear in a bucket listing:\n%s", root)
	}
}

func TestRebuildBucketTree_SecondRebuildWritesNothing(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "index.md"), "# research\n\nUser prose.\n")
	writeTopicFile(t, filepath.Join(dir, "item", "index.md"), "---\ntitle: Item\n---\n\n# Item\n\nItem prose kept as written.\n")
	writeTopicFile(t, filepath.Join(dir, "item", "01_raw", "a.md"), "---\ntitle: A\n---\n")

	if _, errs := RebuildBucketTree(dir); errs != nil {
		t.Fatalf("first rebuild: %v", errs)
	}

	paths := []string{
		filepath.Join(dir, "index.md"),
		filepath.Join(dir, "item", "index.md"),
		filepath.Join(dir, "item", "01_raw", "index.md"),
	}
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	before := map[string]string{}
	for _, p := range paths {
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(p)
		before[p] = string(data)
	}
	if !strings.HasPrefix(before[paths[1]], "---\ntitle: Item\n---\n\n# Item\n\nItem prose kept as written.\n") {
		t.Errorf("text outside the region changed:\n%s", before[paths[1]])
	}

	if _, errs := RebuildBucketTree(dir); errs != nil {
		t.Fatalf("second rebuild: %v", errs)
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(past) {
			t.Errorf("%s rewritten on an unchanged rebuild (mtime %v)", p, info.ModTime())
		}
		data, _ := os.ReadFile(p)
		if string(data) != before[p] {
			t.Errorf("%s content changed on an unchanged rebuild", p)
		}
	}
}

func TestRebuildBucketTree_UnpairedNestedIndexIsolated(t *testing.T) {
	dir := t.TempDir()
	brokenOriginal := "# broken\n\n<bucket-docs>\n\nno close tag\n"
	writeTopicFile(t, filepath.Join(dir, "broken", "index.md"), brokenOriginal)
	writeTopicFile(t, filepath.Join(dir, "broken", "child", "c.md"), "---\ntitle: C\n---\n")
	writeTopicFile(t, filepath.Join(dir, "sibling", "s.md"), "---\ntitle: S\n---\n")

	_, errs := RebuildBucketTree(dir)
	if len(errs) != 1 || !errors.Is(errs[0], errUnpairedRegion) {
		t.Fatalf("expected one errUnpairedRegion, got %v", errs)
	}
	if err := errs[0]; !strings.Contains(err.Error(), filepath.Join(dir, "broken", "index.md")) {
		t.Errorf("warning must name the broken file: %v", err)
	}
	if got := readIndex(t, filepath.Join(dir, "broken")); got != brokenOriginal {
		t.Errorf("unpaired index.md must be untouched\ngot:  %q\nwant: %q", got, brokenOriginal)
	}
	if !strings.Contains(readIndex(t, filepath.Join(dir, "broken", "child")), "[C](c.md)") {
		t.Errorf("child of the broken directory must still be indexed")
	}
	if !strings.Contains(readIndex(t, filepath.Join(dir, "sibling")), "[S](s.md)") {
		t.Errorf("sibling of the broken directory must still be indexed")
	}
	if !strings.Contains(readIndex(t, dir), "(broken/index.md)") {
		t.Errorf("root must still be indexed and list the broken directory")
	}
}

func TestRebuildBucketTree_SumsCountsAcrossTree(t *testing.T) {
	dir := t.TempDir()
	writeTopicFile(t, filepath.Join(dir, "a.md"), "---\ntitle: A\n---\n")
	writeTopicFile(t, filepath.Join(dir, "item", "index.md"), "---\ntitle: Item\n---\n")
	writeTopicFile(t, filepath.Join(dir, "item", "b.md"), "no frontmatter\n")
	writeTopicFile(t, filepath.Join(dir, "item", "01_raw", "c.md"), "---\ntitle: C\n---\n")

	res, errs := RebuildBucketTree(dir)
	if errs != nil {
		t.Fatalf("RebuildBucketTree: %v", errs)
	}
	// root: a.md, item (indexed). item: b.md (unindexed), 01_raw (no
	// frontmatter, unindexed). 01_raw: c.md (indexed).
	if res.Indexed != 3 || res.Unindexed != 2 {
		t.Errorf("got %d indexed, %d unindexed; want 3, 2", res.Indexed, res.Unindexed)
	}
}

func TestRebuildAllBucketIndexes_WalksNestedDirectories(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")
	bucketDir := filepath.Join(root, "research")
	writeTopicFile(t, filepath.Join(bucketDir, "item", "01_raw", "a.md"), "---\ntitle: A\n---\n")
	if err := spliceBucketEntry(filepath.Join(wikiDir, "index.md"), "research", bucketDir); err != nil {
		t.Fatal(err)
	}

	results, errs := RebuildAllBucketIndexes(root, wikiDir)
	if len(errs) > 0 {
		t.Fatalf("RebuildAllBucketIndexes: %v", errs)
	}
	if !strings.Contains(readIndex(t, filepath.Join(bucketDir, "item", "01_raw")), "[A](a.md)") {
		t.Errorf("nested stage directory not indexed by the all-bucket rebuild")
	}
	if results["research"].Indexed != 1 || results["research"].Unindexed != 2 {
		t.Errorf("got %+v, want 1 indexed, 2 unindexed", results["research"])
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
		"- [alpha](../alpha/index.md) - Alpha bucket description sentence here.\n" +
		"- [missing](../missing) · (missing)\n" +
		"- [zeta](../zeta/index.md) - Zeta bucket frontmatter description"

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

	if _, errs := RebuildAllBucketIndexes(root, wikiDir); len(errs) > 0 {
		t.Fatalf("RebuildAllBucketIndexes: %v", errs)
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
	if !strings.Contains(realm, "- [alpha](../alpha/index.md) - Alpha bucket for research notes") {
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

func TestRebuildAllBucketIndexes_UnchangedRebuildLeavesRealmIndexAlone(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")
	indexPath := filepath.Join(wikiDir, "index.md")
	bucketDir := filepath.Join(root, "research")
	writeTopicFile(t, filepath.Join(bucketDir, "a.md"), "---\ntitle: A\n---\n")
	if err := spliceBucketEntry(indexPath, "research", bucketDir); err != nil {
		t.Fatal(err)
	}
	if _, errs := RebuildAllBucketIndexes(root, wikiDir); len(errs) > 0 {
		t.Fatalf("first rebuild: %v", errs)
	}

	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(indexPath, past, past); err != nil {
		t.Fatal(err)
	}

	if _, errs := RebuildAllBucketIndexes(root, wikiDir); len(errs) > 0 {
		t.Fatalf("second rebuild: %v", errs)
	}
	info, err := os.Stat(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(past) {
		t.Errorf("realm index.md rewritten on an unchanged rebuild (mtime %v)", info.ModTime())
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

	_, errs := RebuildAllBucketIndexes(root, wikiDir)
	if len(errs) != 1 || !errors.Is(errs[0], errUnpairedRegion) {
		t.Fatalf("expected one errUnpairedRegion surfaced, got %v", errs)
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

func TestRebuildAllBucketIndexes_EveryErrorLineNamesItsBucket(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")
	bucketDir := filepath.Join(root, "research")
	writeTopicFile(t, filepath.Join(bucketDir, "a", "index.md"), "<bucket-docs>\n")
	writeTopicFile(t, filepath.Join(bucketDir, "b", "index.md"), "<bucket-docs>\n")
	if err := spliceBucketEntry(filepath.Join(wikiDir, "index.md"), "research", bucketDir); err != nil {
		t.Fatal(err)
	}

	_, errs := RebuildAllBucketIndexes(root, wikiDir)
	if len(errs) != 2 {
		t.Fatalf("want one error per broken directory, got %v", errs)
	}
	for _, err := range errs {
		if !strings.HasPrefix(err.Error(), `bucket "research": `) {
			t.Errorf("error lacks its bucket context: %q", err)
		}
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

	if _, errs := RebuildAllBucketIndexes(root, wikiDir); len(errs) > 0 {
		t.Fatalf("RebuildAllBucketIndexes: %v", errs)
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

func writeTwoConcepts(t *testing.T, dir string) {
	t.Helper()
	writeTopicFile(t, filepath.Join(dir, "sources.md"), "---\ntitle: Sources\ndescription: Register and crawl a source.\ntags: [ingest]\n---\n\nBody.\n")
	writeTopicFile(t, filepath.Join(dir, "jobs.md"), "---\ntitle: Jobs\ndescription: Read the worker queue.\n---\n\nBody.\n")
}

func TestIndexDir_WritesRegionAndCounts(t *testing.T) {
	dir := t.TempDir()
	writeTwoConcepts(t, dir)
	writeTopicFile(t, filepath.Join(dir, "draft.md"), "# Draft\n\nNo frontmatter.\n")
	writeTopicFile(t, filepath.Join(dir, "index.md"), "# Admin help\n\nIntro.\n\n<bucket-docs>\n\nold\n\n</bucket-docs>\n\nTrailer.\n")

	res, err := IndexDir(dir, false)
	if err != nil {
		t.Fatalf("IndexDir: %v", err)
	}
	if res.Indexed != 2 || res.Unindexed != 1 {
		t.Errorf("got %d indexed, %d unindexed; want 2, 1", res.Indexed, res.Unindexed)
	}
	if !res.Stale {
		t.Errorf("region differed before the write; want Stale")
	}

	got, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	want := "# Admin help\n\nIntro.\n\n<bucket-docs>\n\n## Docs\n\n" +
		"- [Jobs](jobs.md) - Read the worker queue.\n" +
		"- [Sources](sources.md) - Register and crawl a source. · tags: ingest\n\n" +
		"### Unindexed\n\n- [Draft](draft.md)\n\n" +
		"</bucket-docs>\n\nTrailer.\n"
	if string(got) != want {
		t.Errorf("index.md\ngot:  %q\nwant: %q", got, want)
	}
}

func TestIndexDir_CheckStaleLeavesFile(t *testing.T) {
	dir := t.TempDir()
	writeTwoConcepts(t, dir)
	original := "# Admin help\n\n<bucket-docs>\n\n## Docs\n\n- stale\n\n</bucket-docs>\n"
	writeTopicFile(t, filepath.Join(dir, "index.md"), original)

	res, err := IndexDir(dir, true)
	if err != nil {
		t.Fatalf("IndexDir: %v", err)
	}
	if !res.Stale {
		t.Errorf("region differs from a fresh render; want Stale")
	}

	got, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	if string(got) != original {
		t.Errorf("check must not write\ngot:  %q\nwant: %q", got, original)
	}
}

func TestIndexDir_CheckFreshAfterWrite(t *testing.T) {
	dir := t.TempDir()
	writeTwoConcepts(t, dir)

	if _, err := IndexDir(dir, false); err != nil {
		t.Fatalf("write run: %v", err)
	}
	res, err := IndexDir(dir, true)
	if err != nil {
		t.Fatalf("check run: %v", err)
	}
	if res.Stale {
		t.Errorf("check right after a write must report fresh")
	}
}

func TestIndexDir_MissingDirErrors(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")

	for _, check := range []bool{false, true} {
		_, err := IndexDir(dir, check)
		if err == nil {
			t.Fatalf("check=%v: want an error for a missing directory", check)
		}
		if !strings.Contains(err.Error(), dir) {
			t.Errorf("check=%v: error must name the directory: %v", check, err)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("a missing directory must not be created: %v", err)
	}
}
