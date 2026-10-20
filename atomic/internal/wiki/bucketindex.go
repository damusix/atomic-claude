package wiki

// The `<bucket-docs>` region of a directory's index.md: one level deep for
// `atomic docs index`, every folder of a bucket for the bucket tree walk.

import (
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

// osJunk base names are excluded from the `atomic docs index` walk and its
// descendant counts at any depth.
var osJunk = map[string]bool{
	".DS_Store": true,
	"Thumbs.db": true,
}

// BucketTopic is one `<bucket-docs>` entry: a file, a bucket subdirectory, or
// an `atomic docs index` orphan subtree with no companion `<slug>.md`.
type BucketTopic struct {
	// Path is relative to the listing's directory and never nested: a file
	// name, or a directory name for a Dir entry or an orphan subtree.
	Path string
	// Title comes from the frontmatter -> H1 -> filename-stem ladder, or the
	// directory name verbatim for an orphan or a Dir entry with no title.
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
	// Dir marks a bucket subdirectory, whose metadata comes from its index.md.
	Dir bool
}

// walkBucketTopics is the `atomic docs index` walker, not the bucket tree walk:
// it descends one level only to classify router versus orphan subtrees.
func walkBucketTopics(bucketDir string) ([]BucketTopic, error) {
	entries, err := os.ReadDir(bucketDir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", bucketDir, err)
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
		return 0, fmt.Errorf("count descendants of %s: %w", dir, err)
	}
	return count, nil
}

// walkDirChildren lists dir's .md files except index.md, and its walkable
// subdirectories, in childLess order, skipping dot-prefixed names of either kind.
func walkDirChildren(dir string) ([]BucketTopic, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var topics []BucketTopic
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		switch {
		case e.IsDir():
			if skipDirs[name] {
				continue
			}
			topic := readEntryMeta(filepath.Join(dir, name, "index.md"), name)
			topic.Path = name
			topic.Dir = true
			topics = append(topics, topic)
		case name != "index.md" && strings.HasSuffix(name, ".md"):
			topic := readTopicMeta(filepath.Join(dir, name))
			topic.Path = name
			topics = append(topics, topic)
		}
	}

	sort.Slice(topics, func(i, j int) bool { return childLess(topics[i].Path, topics[j].Path) })
	return topics, nil
}

// childLess orders numeric-prefixed names first, by prefix value (9_x before
// 10_x); digit-string compare keeps prefixes past int64 in order.
func childLess(a, b string) bool {
	na, nb := numericPrefix(a), numericPrefix(b)
	switch {
	case na == "" && nb == "":
		return a < b
	case na == "":
		return false
	case nb == "":
		return true
	}
	na, nb = strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
	if len(na) != len(nb) {
		return len(na) < len(nb)
	}
	if na != nb {
		return na < nb
	}
	return a < b
}

func numericPrefix(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i]
}

func readTopicMeta(topicPath string) BucketTopic {
	base := filepath.Base(topicPath)
	return readEntryMeta(topicPath, strings.TrimSuffix(base, filepath.Ext(base)))
}

// readEntryMeta fills only the fields derivable from the file itself, treating
// a missing file as empty; fallbackTitle is the title ladder's last rung.
func readEntryMeta(path, fallbackTitle string) BucketTopic {
	var meta map[string]any
	var body string

	if data, err := os.ReadFile(path); err == nil {
		if m, b, ferr := frontmatter.Parse(string(data)); ferr == nil {
			meta, body = m, b
		}
	}

	return BucketTopic{
		Title:       deriveTitle(meta, body, fallbackTitle),
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

// deriveTitle walks frontmatter title -> first H1 outside a fence -> fallback,
// kebab-case preserved verbatim.
func deriveTitle(meta map[string]any, body, fallback string) string {
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

	return fallback
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
	switch {
	case t.Orphan:
		link += "/"
	case t.Dir:
		link += "/index.md"
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

// TopicCounts counts rendered topics by indexed state.
type TopicCounts struct {
	Indexed   int
	Unindexed int
}

// IndexResult is one directory's counts. Stale means the `<bucket-docs>`
// region differed from a fresh render before the call, in write mode too.
type IndexResult struct {
	TopicCounts
	Stale bool
}

// indexOneDir splices topics into <dir>/index.md, creating it when absent and
// writing only on change; counts survive a failed splice, which writes nothing.
func indexOneDir(dir string, topics []BucketTopic, check bool) (IndexResult, error) {
	var res IndexResult
	for _, t := range topics {
		if t.Indexed {
			res.Indexed++
		} else {
			res.Unindexed++
		}
	}

	indexPath := filepath.Join(dir, "index.md")

	var document string
	if data, err := os.ReadFile(indexPath); err == nil {
		document = string(data)
	} else if !os.IsNotExist(err) {
		return res, fmt.Errorf("read %s: %w", indexPath, err)
	}

	content := renderBucketDocs(topics)
	newDocument, err := spliceManagedRegion(document, managedRegion{tag: "bucket-docs", content: content})
	if err != nil {
		return res, fmt.Errorf("splice %s: %w", indexPath, err)
	}
	res.Stale = newDocument != document

	if check || !res.Stale {
		return res, nil
	}
	return res, writeFileAtomic(indexPath, []byte(newDocument))
}

// IndexDir rebuilds the one-level `<bucket-docs>` listing of dir for
// `atomic docs index`, with its router and orphan rules.
func IndexDir(dir string, check bool) (IndexResult, error) {
	topics, err := walkBucketTopics(dir)
	if err != nil {
		return IndexResult{}, err
	}
	return indexOneDir(dir, topics, check)
}

// RebuildBucketTree indexes bucketDir and every walkable directory under it,
// summing counts; each failing directory adds one error and the walk goes on.
func RebuildBucketTree(bucketDir string) (TopicCounts, []error) {
	var total TopicCounts
	var errs []error

	var walk func(dir string)
	walk = func(dir string) {
		topics, err := walkDirChildren(dir)
		if err != nil {
			errs = append(errs, err)
			return
		}

		res, err := indexOneDir(dir, topics, false)
		if err != nil {
			errs = append(errs, err)
		}
		total.Indexed += res.Indexed
		total.Unindexed += res.Unindexed

		for _, t := range topics {
			if t.Dir {
				walk(filepath.Join(dir, t.Path))
			}
		}
	}
	walk(bucketDir)

	return total, errs
}

// renderBucketList renders one OKF §6 line per bucket, name-sorted, linked relative to
// wikiDir as `atomic wiki linkify` does; a missing bucket directory renders "(missing)".
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

			link += "/index.md"
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
		return fmt.Errorf("read %s: %w", indexPath, readErr)
	}

	content := renderBucketList(wikiDir, entries)
	newDocument, spliceErr := spliceManagedRegion(document, managedRegion{tag: "wiki-bucket-list", content: content})
	if spliceErr != nil {
		return fmt.Errorf("realm bucket list: %w", spliceErr)
	}
	if newDocument == document {
		return nil
	}

	return writeFileAtomic(indexPath, []byte(newDocument))
}

// RebuildAllBucketIndexes rebuilds each bucket tree, then the realm list, and
// returns counts by bucket name. Each failure adds one error without stopping the loop.
//
// root is unused; it exists for call-site symmetry with Scan, since registry
// bucket paths are already absolute.
func RebuildAllBucketIndexes(root, wikiDir string) (map[string]TopicCounts, []error) {
	indexPath := filepath.Join(wikiDir, "index.md")

	entries, err := readBucketEntries(indexPath)
	if err != nil {
		return nil, []error{fmt.Errorf("read bucket entries: %w", err)}
	}

	results := map[string]TopicCounts{}
	var errs []error
	for _, e := range entries {
		if !fileExists(e.Path) {
			continue
		}
		counts, rebErrs := RebuildBucketTree(e.Path)
		results[e.Name] = counts
		for _, rebErr := range rebErrs {
			errs = append(errs, fmt.Errorf("bucket %q: %w", e.Name, rebErr))
		}
	}

	if err := rebuildRealmBucketList(wikiDir, entries); err != nil {
		errs = append(errs, err)
	}

	return results, errs
}
