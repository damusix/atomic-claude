package wiki

// bucketindex.go — CP2 per-bucket topic index: the `<bucket-docs>` region
// inside `<bucket>/index.md`.
//
// This walk is a DIFFERENT granularity than bucket.go's WalkBucket manifest
// walk, deliberately. WalkBucket hashes every file (for staleness diffing);
// this walk groups files into TOPICS — a router `<slug>.md` collapses its
// `<slug>/` subtree into one entry instead of one entry per descendant file.
// Do not "fix" this into a single walk: the two answer different questions
// (content changed? vs. what should a human read next?) and the manifest
// walk's per-file granularity is required for accurate staleness diffing.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// recognizedFrontmatterKeys are the six keys the frontmatter contract defines.
// A topic with a frontmatter block containing none of these is unindexed.
var recognizedFrontmatterKeys = []string{"title", "type", "description", "tags", "status", "created"}

// BucketTopic is one entry in a bucket's `<bucket-docs>` listing: a single
// topic file, or an orphan subtree with no companion `<slug>.md`.
type BucketTopic struct {
	// Path is the topic's path relative to the bucket root: a file name
	// ("seo.md") for a simple or router topic, or a directory name
	// ("weird-notes") for an orphan subtree. Never nested — the walk only
	// promotes bucket-root entries to topics.
	Path string
	// Title is resolved via the title ladder (frontmatter -> H1 -> filename
	// stem) for a file topic, or the directory stem verbatim for an orphan.
	Title string
	// Description is resolved via DeriveMemberDescription; "" for orphans
	// (no single file to derive from) and for a ladder-exhausted file topic.
	Description string
	// Tags is the frontmatter tags list, or nil when absent or malformed.
	Tags []string
	// Indexed is false when the topic file has no frontmatter block, or a
	// block containing none of recognizedFrontmatterKeys. Always false for
	// an orphan subtree (there is no single file to carry frontmatter).
	Indexed bool
	// Router is true when a directory of the same slug sits beside this
	// topic's file; the subtree is collapsed into this one entry.
	Router bool
	// Orphan is true when this entry is a subtree directory with no sibling
	// "<slug>.md" file.
	Orphan bool
	// ChildCount is the recursive count of descendant .md files under the
	// subtree, valid only when Router or Orphan is true.
	ChildCount int
}

// walkBucketTopics walks bucketDir and returns its topics sorted by relative
// path (forward-slash).
//
// The walk covers the bucket root plus one directory level, for router/orphan
// detection only — .md files nested deeper than a router or orphan subtree
// root are never promoted to top-level topics. Excluded: the bucket's own
// index.md, osJunk basenames, skipDirs-named directories, and non-.md files
// at the bucket root (same exclusion sets WalkBucket uses).
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

	// A directory is a router subtree when a sibling "<slug>.md" file
	// exists at the bucket root; otherwise it's an orphan subtree.
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
			// Collapsed under its sibling .md topic — no separate entry.
			continue
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

// countDescendantMD recursively counts .md files under dir, skipping
// skipDirs-named subdirectories and osJunk basenames.
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

// readTopicMeta reads one topic file and fills a BucketTopic via the title,
// description, and tags ladders. Path, Router, Orphan, and ChildCount are
// left zero — the caller (walkBucketTopics) fills those in.
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

// hasRecognizedFrontmatterKey reports whether meta carries at least one of
// the six recognized frontmatter keys. A nil meta (no frontmatter block, or
// an unparseable one) is never indexed.
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

// deriveTitle resolves the title ladder: frontmatter title -> first H1 in the
// body -> filename stem (kebab-case preserved verbatim).
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

// deriveTags resolves the tags ladder: frontmatter tags as a YAML list of
// strings; a bare string is read as a single-element list; any other shape
// (a map, a number, a list containing a non-string element) is ignored
// entirely — no partial tag list, no fallback.
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
				return nil // malformed element -> ignore the whole shape
			}
			tags = append(tags, s)
		}
		return tags
	default:
		return nil
	}
}

// listEntry renders one BucketTopic as an OKF §6 line:
//
//   - [<title>](<relpath>) - <description> · tags: a, b · router (<N> docs)
//
// Link-only (no trailing " - ") when Description is empty, matching
// buildMembersSection's existing link-only form. Suffix order is
// description, then tags, then router/orphan.
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

// renderBucketDocs renders the content for inside the `<bucket-docs>` region:
// a "## Docs" heading, indexed topics as listEntry lines, then — when any
// unindexed topics exist — a "### Unindexed" sub-heading followed by theirs.
// Both groups preserve the caller's (relative-path-sorted) order.
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

// RebuildBucketIndex rebuilds the `<bucket-docs>` region in
// <bucketDir>/index.md: walk topics, render the listing, splice the region
// through the shared managed-region primitive, write atomically.
//
// index.md absent is treated as an empty document (region gets appended).
// An errUnpairedRegion from the splice is returned unchanged — the caller
// reports the bucket unmanageable — and index.md is left untouched.
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

// renderBucketList renders the content for inside the `<wiki-bucket-list>`
// region: a "## Buckets" heading plus one OKF §6 line per registered bucket,
// sorted by name. `<link>` is the bucket path relative to wikiDir (e.g.
// "../research" for a realm-root bucket), matching how `atomic wiki linkify`
// emits file-relative links — the absolute path stays in the
// `<wiki-buckets>` block, which `stale` and `serve` resolve from any cwd.
// A bucket whose directory is missing renders link-only with a "(missing)"
// marker instead of a derived description, and never errors.
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

// rebuildRealmBucketList splices the realm `<wiki-bucket-list>` region into
// <wikiDir>/index.md for the given bucket entries. Zero entries → no splice
// at all, no empty "## Buckets". An errUnpairedRegion from the splice leaves
// index.md untouched — spliceManagedRegion never writes on that path.
//
// Extracted from RebuildAllBucketIndexes so a caller that has already
// rebuilt one bucket's own `<bucket-docs>` region (e.g. wikiBucketIndexAction's
// single-bucket branch) can refresh just the realm list without re-walking
// every registered bucket.
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

// RebuildAllBucketIndexes rebuilds every registered bucket's `<bucket-docs>`
// region, then splices the realm `<wiki-bucket-list>` region into
// <wikiDir>/index.md (via rebuildRealmBucketList).
//
// A per-bucket rebuild failure (including errUnpairedRegion) is collected
// and joined into the returned error but never stops the loop — a broken
// bucket must not block its siblings or the realm splice. A bucket whose
// directory is missing is skipped for rebuild entirely (no error recorded;
// it still renders with a "(missing)" marker in the realm list).
//
// root is accepted for call-site symmetry with Scan; bucket paths in the
// <wiki-buckets> registry are already absolute, so it is not needed here.
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
