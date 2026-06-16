// nav.go — CP3 nav tree handler.
//
// NewNavHandler returns an http.Handler for the /nav route that emits an HTML
// fragment containing the collapsible nav tree. The fragment is meant to be
// injected into #nav-pane by the layout shell (htmx OOB or an initial load).
//
// Realm scope (IsRealmScope = true):
//
//	Six groups: Realm / Repos / Concerns / Knowledge / Buckets / External.
//	Each leaf carries hx-get="/page/<relpath>", hx-target="#main-pane",
//	hx-push-url="true" so htmx swaps the content without a full reload.
//
//	Staleness is computed ONCE per request via StalenessFn:
//	  - If StalenessFn is set on NavOptions, it is called once and the results
//	    (stale member set, bucket diff set) are used to render badges.
//	  - If StalenessFn is nil, the production default (computeStaleness) is used,
//	    which calls wiki.Stale once and parses its DRIFT/STALE output.
//	  - Tests may inject a no-op or pre-baked StalenessFn to avoid disk I/O.
//
// Repo scope (IsRealmScope = false):
//
//	Docs file tree: README.md at root + docs/**/*.md, each as a /page/ link.
//	Plus a "Code" group placeholder (filled by CP7/8).
package serve

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// StalenessFn is the injectable seam for staleness computation.
// It returns two sets:
//   - staleMembers: member name (or path) → true when stale
//   - bucketDiffs: bucket name → true when the bucket has a non-empty diff
//
// The function must be read-only and must not hang on missing git or network.
// The production implementation calls wiki.Stale once and parses its output.
type StalenessFn func(realmRoot, wikiIndexPath string) (staleMembers map[string]bool, bucketDiffs map[string]bool)

// NavOptions configures the nav tree handler. Exported so tests can construct
// it directly.
type NavOptions struct {
	// RealmRoot is the root directory being served.
	RealmRoot string

	// IsRealmScope is true when the server is serving a realm (wiki present).
	// false = repo/member scope: render docs file tree.
	IsRealmScope bool

	// WikiIndexPath is the path to wiki/index.md; used to read members and
	// bucket entries.  Required when IsRealmScope = true.
	WikiIndexPath string

	// StaleMembers is a pre-computed staleness map (member name → stale).
	// Deprecated: prefer StalenessFn.  If both are set, StalenessFn takes
	// precedence and overrides StaleMembers.  Left for seam-based tests that
	// set it directly.
	StaleMembers map[string]bool

	// BucketDiffs is a pre-computed diff map (bucket name → has diff).
	// Deprecated: prefer StalenessFn.  If both are set, StalenessFn takes
	// precedence and overrides BucketDiffs.
	BucketDiffs map[string]bool

	// StalenessFn is the seam for staleness computation.  When nil, the
	// production default (computeStaleness) is used.  Tests may inject a
	// stub to avoid disk/git I/O.
	StalenessFn StalenessFn
}

// NewNavHandler returns an http.Handler for the /nav route.
func NewNavHandler(opts NavOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// Resolve staleness once per request.
		if opts.IsRealmScope {
			fn := opts.StalenessFn
			if fn == nil {
				fn = computeStaleness
			}
			staleMembers, bucketDiffs := fn(opts.RealmRoot, opts.WikiIndexPath)
			opts.StaleMembers = staleMembers
			opts.BucketDiffs = bucketDiffs
		}

		var sb strings.Builder
		if opts.IsRealmScope {
			renderRealmNav(&sb, opts)
		} else {
			renderRepoNav(&sb, opts.RealmRoot)
		}

		fmt.Fprint(w, sb.String())
	})
}

// ─── realm nav ───────────────────────────────────────────────────────────────

// renderRealmNav writes the six-group realm nav tree to sb.
func renderRealmNav(sb *strings.Builder, opts NavOptions) {
	// ── Group 1: Realm ────────────────────────────────────────────────────
	sb.WriteString("<details open>\n")
	sb.WriteString("<summary class=\"nav-group\">Realm</summary>\n")
	// Link to wiki/index.md via /page/ relative to RealmRoot.
	wikiIndexRel := "wiki/index.md"
	writeNavLeaf(sb, "index", wikiIndexRel)
	sb.WriteString("</details>\n")

	// Read members from the <wiki-scan> block.
	members, _ := wiki.ReadScanMembers(opts.WikiIndexPath)

	// ── Group 2: Repos ────────────────────────────────────────────────────
	sb.WriteString("<details open>\n")
	sb.WriteString("<summary class=\"nav-group\">Repos</summary>\n")
	for _, m := range members {
		name := filepath.Base(m.Path)
		stale := opts.StaleMembers[name] || opts.StaleMembers[m.Path]
		writeNavLeafWithBadge(sb, name, memberLinkRel(opts.RealmRoot, m), stale)
	}
	sb.WriteString("</details>\n")

	// ── Group 3: Concerns ─────────────────────────────────────────────────
	sb.WriteString("<details open>\n")
	sb.WriteString("<summary class=\"nav-group\">Concerns</summary>\n")
	concerns := walkMarkdownFiles(filepath.Join(opts.RealmRoot, "wiki", "concerns"))
	wikiConcernsBase := "wiki/concerns/"
	for _, name := range concerns {
		writeNavLeaf(sb, stripMDExt(name), wikiConcernsBase+name)
	}
	sb.WriteString("</details>\n")

	// ── Group 4: Knowledge ────────────────────────────────────────────────
	sb.WriteString("<details open>\n")
	sb.WriteString("<summary class=\"nav-group\">Knowledge</summary>\n")
	knowledge := walkMarkdownFiles(filepath.Join(opts.RealmRoot, "wiki", "knowledge"))
	wikiKnowledgeBase := "wiki/knowledge/"
	for _, name := range knowledge {
		writeNavLeaf(sb, stripMDExt(name), wikiKnowledgeBase+name)
	}
	sb.WriteString("</details>\n")

	// ── Group 5: Buckets ──────────────────────────────────────────────────
	sb.WriteString("<details open>\n")
	sb.WriteString("<summary class=\"nav-group\">Buckets</summary>\n")
	buckets, _ := wiki.ReadBucketEntries(opts.WikiIndexPath)
	for _, b := range buckets {
		hasDiff := opts.BucketDiffs[b.Name]
		writeNavBucketFolder(sb, opts.RealmRoot, b, hasDiff)
	}
	if len(buckets) == 0 {
		sb.WriteString("<span class=\"nav-empty\">no buckets registered</span>\n")
	}
	sb.WriteString("</details>\n")

	// ── Group 6: External ─────────────────────────────────────────────────
	sb.WriteString("<details open>\n")
	sb.WriteString("<summary class=\"nav-group\">External</summary>\n")
	// /external is the external-link registry page (CP5 — NewExternalHandler).
	// Load it into #main-pane via htmx so the shell (nav + rail) is preserved;
	// the href fallback keeps it navigable without JS.
	sb.WriteString("<a class=\"nav-item\" hx-get=\"/external\" hx-target=\"#main-pane\" hx-push-url=\"true\" href=\"/external\">External links registry</a>\n")
	sb.WriteString("</details>\n")
}

// memberLinkRel returns the realm-root-relative /page target for a realm member,
// mirroring the wiki index's Members block so nav and index never disagree:
//   - summarized → its summary file/dir under wiki/ (e.g. "wiki/repos/alpha.md");
//   - indexed    → its signals page (realm-relative form of the absolute
//     SignalsPath), e.g. "alpha/.claude/project/signals.md";
//   - pending / unresolved → the member directory itself, served as a folder
//     index or listing by the /page handler.
//
// It never returns "wiki/repos/<name>.md" by guess: that file frequently does
// not exist on disk (summaries are written only for summarized members), which
// is exactly what produced the 404s in the left nav.
func memberLinkRel(realmRoot string, m wiki.Member) string {
	if m.SummaryPath != "" {
		return "wiki/" + m.SummaryPath
	}
	if m.SignalsPath != "" {
		if rel, err := filepath.Rel(realmRoot, m.SignalsPath); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(m.Path) + "/"
}

// ─── repo nav ────────────────────────────────────────────────────────────────

// renderRepoNav writes a docs file tree nav for a bare repo (no wiki).
func renderRepoNav(sb *strings.Builder, root string) {
	// ── README.md (top-level markdown files) ──────────────────────────────
	sb.WriteString("<details open>\n")
	sb.WriteString("<summary class=\"nav-group\">Docs</summary>\n")

	// Top-level .md files (README.md and siblings).
	topMDs := walkMarkdownFiles(root)
	for _, name := range topMDs {
		writeNavLeaf(sb, stripMDExt(name), name)
	}

	// docs/**/*.md — render as nested <details> folder tree.
	docsDir := filepath.Join(root, "docs")
	docsFiles := walkMarkdownFilesRecursive(docsDir)
	writeNavFolderTree(sb, "docs", docsFiles)

	sb.WriteString("</details>\n")

	// ── Code (placeholder; CP7/8 fill it) ────────────────────────────────
	sb.WriteString("<details>\n")
	sb.WriteString("<summary class=\"nav-group\">Code</summary>\n")
	sb.WriteString("<span class=\"nav-empty\">code explorer coming soon</span>\n")
	sb.WriteString("</details>\n")
}

// navFolderNode is a node in the recursive folder tree.
// name is the directory segment name (empty for the root node).
// childOrder preserves deterministic insertion order for child dirs.
// children maps dir-segment → child node.
// files holds the relpath (from baseDir) of *.md files directly in this dir.
type navFolderNode struct {
	name       string
	childOrder []string
	children   map[string]*navFolderNode
	files      []string
}

// buildNavFolderTree constructs a tree from the relative paths returned by
// walkMarkdownFilesRecursive. paths use forward slashes.
func buildNavFolderTree(paths []string) *navFolderNode {
	root := &navFolderNode{children: make(map[string]*navFolderNode)}
	for _, rel := range paths {
		parts := strings.Split(rel, "/")
		node := root
		for i, part := range parts {
			if i == len(parts)-1 {
				// Leaf file.
				node.files = append(node.files, rel)
			} else {
				// Intermediate directory segment.
				if _, ok := node.children[part]; !ok {
					node.children[part] = &navFolderNode{
						name:     part,
						children: make(map[string]*navFolderNode),
					}
					node.childOrder = append(node.childOrder, part)
				}
				node = node.children[part]
			}
		}
	}
	return root
}

// writeNavFolderTree renders the files returned by walkMarkdownFilesRecursive
// as a true recursive nested <details>/<summary> folder tree. basePrefix is
// the path prefix used to construct the /page/ relpath (e.g. "docs").
//
// Files at the top level (no subdirectory) are written as flat leaves.
// Files in subdirectories are grouped under collapsible <details> per folder,
// recursively to arbitrary depth. A file at docs/a/b/c.md appears as "c"
// inside "b" inside "a", with hx-get="/page/docs/a/b/c.md".
func writeNavFolderTree(sb *strings.Builder, basePrefix string, files []string) {
	root := buildNavFolderTree(files)
	// Top-level files (direct children of the base dir) are emitted flat.
	for _, rel := range root.files {
		fullRel := basePrefix + "/" + rel
		writeNavLeaf(sb, stripMDExt(filepath.Base(rel)), fullRel)
	}
	// Recursively render subdirectory nodes.
	for _, dirName := range root.childOrder {
		child := root.children[dirName]
		writeNavFolderNode(sb, child, basePrefix, 1)
	}
}

// writeNavFolderNode recursively renders a folder node as a <details> block.
// depth is 1 for the first level under the base, incrementing with each level.
// Leaf items get inline padding-left scaled by depth so nesting is visually
// distinct at any depth (not just depth 1).
func writeNavFolderNode(sb *strings.Builder, node *navFolderNode, basePrefix string, depth int) {
	escapedName := template.HTMLEscapeString(node.name)
	// Indent the summary label proportionally so nested folder labels step in.
	summaryPad := 24 + (depth-1)*12
	sb.WriteString("<details open class=\"nav-folder\">\n")
	fmt.Fprintf(sb,
		"<summary class=\"nav-folder-label\" style=\"padding-left: %dpx\">%s</summary>\n",
		summaryPad, escapedName)
	// Files directly in this folder.
	for _, rel := range node.files {
		fullRel := basePrefix + "/" + rel
		leafPad := 20 + depth*12
		writeNavLeafIndented(sb, stripMDExt(filepath.Base(rel)), fullRel, leafPad)
	}
	// Child folders (sorted order preserved from buildNavFolderTree).
	for _, dirName := range node.childOrder {
		child := node.children[dirName]
		writeNavFolderNode(sb, child, basePrefix, depth+1)
	}
	sb.WriteString("</details>\n")
}

// writeNavLeafIndented writes a nav leaf with an explicit padding-left (pixels).
// Used by writeNavFolderNode to produce depth-scaled indentation.
func writeNavLeafIndented(sb *strings.Builder, label, relPath string, paddingLeftPx int) {
	escapedLabel := template.HTMLEscapeString(label)
	escapedPath := template.HTMLEscapeString(relPath)
	fmt.Fprintf(sb,
		`<a class="nav-item" style="padding-left: %dpx" hx-get="/page/%s" hx-target="#main-pane" hx-push-url="true" href="/page/%s">%s</a>`+"\n",
		paddingLeftPx, escapedPath, escapedPath, escapedLabel)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// writeNavLeaf writes a single nav leaf <a> element with htmx attributes.
// label is the display text; relPath is the path segment after /page/.
func writeNavLeaf(sb *strings.Builder, label, relPath string) {
	writeNavLeafWithBadge(sb, label, relPath, false)
}

// writeNavLeafWithBadge writes a nav leaf; if stale is true appends a badge.
func writeNavLeafWithBadge(sb *strings.Builder, label, relPath string, stale bool) {
	escapedLabel := template.HTMLEscapeString(label)
	escapedPath := template.HTMLEscapeString(relPath)
	fmt.Fprintf(sb,
		`<a class="nav-item" hx-get="/page/%s" hx-target="#main-pane" hx-push-url="true" href="/page/%s">%s`,
		escapedPath, escapedPath, escapedLabel)
	if stale {
		sb.WriteString(` <span class="badge badge-stale" title="stale">stale</span>`)
	}
	sb.WriteString("</a>\n")
}

// walkMarkdownFiles returns the base filenames of *.md files in dir (one level
// only, sorted). Non-existent dir returns nil.
func walkMarkdownFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") && !hiddenFile(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// walkMarkdownFilesRecursive returns *.md paths relative to dir (forward
// slashes), sorted. Non-existent dir returns nil.
func walkMarkdownFilesRecursive(dir string) []string {
	var results []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || hiddenFile(d.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		// Use forward slashes for URL construction.
		results = append(results, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(results)
	return results
}

// stripMDExt removes the ".md" suffix from a filename.
func stripMDExt(name string) string {
	return strings.TrimSuffix(name, ".md")
}

// writeNavBucketFolder renders a capture bucket as a collapsible folder of its
// servable markdown files.  Buckets hold loose material at the realm root; their
// .md files are servable via /page/<bucket>/<file>, so the nav lists them as a
// browsable tree (collapsed by default — a bucket can hold many files).  A diff
// badge is shown on the folder summary when the bucket has a non-empty diff.
// Buckets with no markdown files render an empty note (a raw-dump bucket may hold
// only non-.md material, which is not navigable as a page).
func writeNavBucketFolder(sb *strings.Builder, realmRoot string, b wiki.BucketEntry, hasDiff bool) {
	// Resolve the bucket directory and its realm-root-relative prefix.
	dir := b.Path
	if dir == "" {
		dir = filepath.Join(realmRoot, b.Name)
	}
	prefix := b.Name
	if rel, err := filepath.Rel(realmRoot, dir); err == nil {
		prefix = filepath.ToSlash(rel)
	}

	escapedName := template.HTMLEscapeString(b.Name)
	sb.WriteString("<details class=\"nav-folder\">\n")
	sb.WriteString("<summary class=\"nav-folder-label nav-group\">")
	sb.WriteString(escapedName)
	if hasDiff {
		sb.WriteString(` <span class="badge badge-diff" title="bucket has new/changed/removed files">diff</span>`)
	}
	sb.WriteString("</summary>\n")

	files := walkMarkdownFilesRecursive(dir)
	if len(files) == 0 {
		sb.WriteString("<span class=\"nav-empty\">no markdown files</span>\n")
	} else {
		writeNavFolderTree(sb, prefix, files)
	}
	sb.WriteString("</details>\n")
}

// computeStaleness is the production StalenessFn.  It calls wiki.Stale once,
// parses its DRIFT/STALE/STALE-bucket output, and returns two maps:
//   - staleMembers: member name (basename of path) → true
//   - bucketDiffs: bucket name → true
//
// Errors from wiki.Stale (exit code 2) are non-fatal: both maps are returned
// empty.  This is intentional: a staleness-check failure must not crash the nav
// tree — it degrades to showing no badges rather than returning an error page.
//
// The function is read-only and does not write any file.
func computeStaleness(realmRoot, _ string) (staleMembers map[string]bool, bucketDiffs map[string]bool) {
	var buf strings.Builder
	code, err := wiki.Stale(realmRoot, &buf)
	if err != nil || code == wiki.StaleCodeError {
		// Hard error (wiki/ absent, unreadable index, etc.) — degrade gracefully.
		return map[string]bool{}, map[string]bool{}
	}

	sets := parseStaleLines(buf.String())
	// Concerns are their own category: a stale concern does NOT light up the
	// member-stale badge.  Nav only surfaces member and bucket staleness.
	return sets.Members, sets.Buckets
}
