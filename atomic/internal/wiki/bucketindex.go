package wiki

// Per-bucket topic index: the `<bucket-docs>` region inside `<bucket>/index.md`.
//
// This walk is deliberately coarser than bucket.go's WalkBucket, which hashes
// every file for staleness diffing. Here a router `<slug>.md` collapses its
// whole subtree into one entry. Do not merge the two walks: they answer
// different questions, and staleness needs the per-file granularity.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// recognizedFrontmatterKeys is the frontmatter contract. A topic whose block
// carries none of them counts as unindexed.
var recognizedFrontmatterKeys = []string{"title", "type", "description", "tags", "status", "created"}

// BucketTopic is one entry in a bucket's `<bucket-docs>` listing: a single
// topic file, or an orphan subtree with no companion `<slug>.md`.
type BucketTopic struct {
	// Path is relative to the bucket root and never nested: a file name for a
	// simple or router topic, a directory name for an orphan subtree.
	Path string
	// Title comes from the frontmatter -> H1 -> filename-stem ladder, or the
	// directory stem verbatim for an orphan.
	Title string
	// Description is "" for orphans, which have no single file to derive from.
	Description string
	// Tags is nil when absent or malformed.
	Tags []string
	// Indexed is false without a frontmatter block carrying a recognized key,
	// and always false for an orphan subtree.
	Indexed bool
	// Router marks a topic whose same-slug directory is collapsed into it.
	Router bool
	// Orphan marks a subtree directory with no sibling "<slug>.md".
	Orphan bool
	// ChildCount counts descendant .md files; valid only for Router or Orphan.
	ChildCount int
}

// walkBucketTopics returns bucketDir's topics sorted by path. It descends one
// directory level, purely to classify router versus orphan subtrees; nothing
// deeper is ever promoted to a top-level topic. Exclusions match WalkBucket's.
func walkBucketTopics(bucketDir string) ([]BucketTopic, error) {
	entries, err := os.ReadDir(bucketDir)
	if err != nil {
		return nil, fmt.Errorf("bucket index: read bucket dir %s: %w", bucketDir, err)
	}

	var fileNames, dirNames []string
	mdSlugs := map[string]bool{}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if skipDirs[name] {
				continue
			}
			dirNames = append(dirNames, name)
			continue
		}
		if osJunk[name] {
			continue
		}
		if name == "index.md" {
			continue
		}
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		fileNames = append(fileNames, name)
		mdSlugs[strings.TrimSuffix(name, ".md")] = true
	}

	// A sibling "<slug>.md" makes the directory a router; otherwise it orphans.
	routerDirs := map[string]bool{}
	for _, d := range dirNames {
		if mdSlugs[d] {
			routerDirs[d] = true
		}
	}

	var topics []BucketTopic

	for _, name := range fileNames {
		slug := strings.TrimSuffix(name, ".md")
		topic := readTopicMeta(filepath.Join(bucketDir, name))
		topic.Path = name

		if routerDirs[slug] {
			count, err := countDescendantMD(filepath.Join(bucketDir, slug))
			if err != nil {
				return nil, err
			}
			topic.Router = true
			topic.ChildCount = count
		}

		topics = append(topics, topic)
	}

	for _, d := range dirNames {
		if routerDirs[d] {
			continue // already collapsed under its sibling .md topic
		}
		count, err := countDescendantMD(filepath.Join(bucketDir, d))
		if err != nil {
			return nil, err
		}
		topics = append(topics, BucketTopic{
			Path:       d,
			Title:      d,
			Orphan:     true,
			ChildCount: count,
		})
	}

	sort.Slice(topics, func(i, j int) bool { return topics[i].Path < topics[j].Path })

	return topics, nil
}

func countDescendantMD(dir string) (int, error) {
	count := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if osJunk[d.Name()] {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".md") {
			count++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("bucket index: count descendants of %s: %w", dir, err)
	}
	return count, nil
}

// readTopicMeta fills only the fields derivable from the file itself;
// walkBucketTopics supplies Path, Router, Orphan, and ChildCount.
func readTopicMeta(topicPath string) BucketTopic {
	var meta map[string]any
	var body string

	if data, err := os.ReadFile(topicPath); err == nil {
		if m, b, ferr := frontmatter.Parse(string(data)); ferr == nil {
			meta, body = m, b
		}
	}

	return BucketTopic{
		Title:       deriveTitle(meta, body, topicPath),
		Description: deriveDescriptionFrom(meta, body),
		Tags:        deriveTags(meta),
		Indexed:     hasRecognizedFrontmatterKey(meta),
	}
}

// hasRecognizedFrontmatterKey treats a nil meta — no block, or an unparseable
// one — as unindexed.
func hasRecognizedFrontmatterKey(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	for _, k := range recognizedFrontmatterKeys {
		if _, ok := meta[k]; ok {
			return true
		}
	}
	return false
}

// deriveTitle walks frontmatter title -> first H1 outside a fence -> filename
// stem, kebab-case preserved verbatim.
func deriveTitle(meta map[string]any, body, topicPath string) string {
	if meta != nil {
		if v, ok := meta["title"]; ok {
			if s, ok := v.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					return s
				}
			}
		}
	}

	inFence := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}

	base := filepath.Base(topicPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// deriveTags accepts a YAML string list or a bare string. Any other shape is
// ignored whole — never a partial tag list.
func deriveTags(meta map[string]any) []string {
	if meta == nil {
		return nil
	}
	v, ok := meta["tags"]
	if !ok {
		return nil
	}

	switch t := v.(type) {
	case string:
		if s := strings.TrimSpace(t); s != "" {
			return []string{s}
		}
		return nil
	case []any:
		tags := make([]string, 0, len(t))
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil
			}
			tags = append(tags, s)
		}
		return tags
	default:
		return nil
	}
}

// listEntry renders one topic as an OKF §6 line, link-only when there is no
// description — the same form buildMembersSection uses.
func listEntry(t BucketTopic) string {
	link := t.Path
	if t.Orphan {
		link += "/"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "- [%s](%s)", t.Title, link)
	if t.Description != "" {
		fmt.Fprintf(&sb, " - %s", t.Description)
	}
	if len(t.Tags) > 0 {
		fmt.Fprintf(&sb, " · tags: %s", strings.Join(t.Tags, ", "))
	}
	switch {
	case t.Router:
		fmt.Fprintf(&sb, " · router (%d docs)", t.ChildCount)
	case t.Orphan:
		fmt.Fprintf(&sb, " · orphan subtree (%d docs)", t.ChildCount)
	}
	return sb.String()
}

// renderBucketDocs splits topics into a "## Docs" listing and, when any exist,
// an "### Unindexed" group, preserving the caller's sort order in both.
func renderBucketDocs(topics []BucketTopic) string {
	var indexed, unindexed []BucketTopic
	for _, t := range topics {
		if t.Indexed {
			indexed = append(indexed, t)
		} else {
			unindexed = append(unindexed, t)
		}
	}

	var sb strings.Builder
	sb.WriteString("## Docs")

	if len(indexed) > 0 {
		sb.WriteString("\n\n")
		for i, t := range indexed {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(listEntry(t))
		}
	}

	if len(unindexed) > 0 {
		sb.WriteString("\n\n### Unindexed\n\n")
		for i, t := range unindexed {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(listEntry(t))
		}
	}

	return sb.String()
}

// RebuildBucketIndex rewrites the `<bucket-docs>` region in
// <bucketDir>/index.md, treating an absent index.md as an empty document. An
// errUnpairedRegion propagates and leaves index.md untouched.
func RebuildBucketIndex(bucketDir string) error {
	indexPath := filepath.Join(bucketDir, "index.md")

	var document string
	if data, err := os.ReadFile(indexPath); err == nil {
		document = string(data)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("bucket index: read %s: %w", indexPath, err)
	}

	topics, err := walkBucketTopics(bucketDir)
	if err != nil {
		return err
	}

	content := renderBucketDocs(topics)
	newDocument, err := spliceManagedRegion(document, managedRegion{tag: "bucket-docs", content: content})
	if err != nil {
		return fmt.Errorf("bucket index: splice %s: %w", indexPath, err)
	}

	return writeFileAtomic(indexPath, []byte(newDocument))
}

// renderBucketList renders one OKF §6 line per registered bucket, name-sorted.
// Links are relative to wikiDir, matching `atomic wiki linkify`; the absolute
// path stays in the `<wiki-buckets>` block that stale and serve resolve from
// any cwd. A missing bucket directory renders "(missing)" rather than erroring.
func renderBucketList(wikiDir string, entries []BucketEntry) string {
	sorted := make([]BucketEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var sb strings.Builder
	sb.WriteString("## Buckets")

	if len(sorted) > 0 {
		sb.WriteString("\n\n")
		for i, e := range sorted {
			if i > 0 {
				sb.WriteString("\n")
			}

			link, err := filepath.Rel(wikiDir, e.Path)
			if err != nil {
				link = e.Path
			}
			link = filepath.ToSlash(link)

			if !fileExists(e.Path) {
				fmt.Fprintf(&sb, "- [%s](%s) · (missing)", e.Name, link)
				continue
			}

			desc := DeriveMemberDescription(filepath.Join(e.Path, "index.md"))
			if desc != "" {
				fmt.Fprintf(&sb, "- [%s](%s) - %s", e.Name, link, desc)
			} else {
				fmt.Fprintf(&sb, "- [%s](%s)", e.Name, link)
			}
		}
	}

	return sb.String()
}

// rebuildRealmBucketList splices the realm `<wiki-bucket-list>` region. Zero
// entries splices nothing, rather than an empty "## Buckets". It is separate
// from RebuildAllBucketIndexes so a single-bucket rebuild can refresh the realm
// list without re-walking every registered bucket.
func rebuildRealmBucketList(wikiDir string, entries []BucketEntry) error {
	if len(entries) == 0 {
		return nil
	}

	indexPath := filepath.Join(wikiDir, "index.md")

	var document string
	if data, readErr := os.ReadFile(indexPath); readErr == nil {
		document = string(data)
	} else if !os.IsNotExist(readErr) {
		return fmt.Errorf("bucket index: read %s: %w", indexPath, readErr)
	}

	content := renderBucketList(wikiDir, entries)
	newDocument, spliceErr := spliceManagedRegion(document, managedRegion{tag: "wiki-bucket-list", content: content})
	if spliceErr != nil {
		return fmt.Errorf("realm bucket list: %w", spliceErr)
	}

	return writeFileAtomic(indexPath, []byte(newDocument))
}

// RebuildAllBucketIndexes rebuilds every bucket's `<bucket-docs>` region, then
// the realm `<wiki-bucket-list>`. Per-bucket failures are joined into the
// returned error but never stop the loop: one broken bucket must not block its
// siblings or the realm splice. A missing bucket directory is skipped silently.
//
// root is unused; it exists for call-site symmetry with Scan, since registry
// bucket paths are already absolute.
func RebuildAllBucketIndexes(root, wikiDir string) error {
	indexPath := filepath.Join(wikiDir, "index.md")

	entries, err := readBucketEntries(indexPath)
	if err != nil {
		return fmt.Errorf("bucket index: read bucket entries: %w", err)
	}

	var errs []error
	for _, e := range entries {
		if !fileExists(e.Path) {
			continue
		}
		if rebErr := RebuildBucketIndex(e.Path); rebErr != nil {
			errs = append(errs, fmt.Errorf("bucket %q: %w", e.Name, rebErr))
		}
	}

	if err := rebuildRealmBucketList(wikiDir, entries); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}
