// nav.go — nav tree data for the /api/nav JSON handler (api_handlers.go).
//
// Realm scope (IsRealmScope = true): six groups — Realm / Repos / Concerns /
// Knowledge / Buckets / External. Staleness is computed once per request via
// StalenessFn:
//   - If StalenessFn is set on NavOptions, it is called once and the results
//     (stale member set, bucket diff set) are used to badge nodes.
//   - If StalenessFn is nil, the production default (computeStaleness) is used,
//     which calls wiki.Stale once and parses its DRIFT/STALE output.
//   - Tests may inject a no-op or pre-baked StalenessFn to avoid disk I/O.
//
// Repo scope (IsRealmScope = false): docs file tree (README.md at root +
// docs/**/*.md) plus a "Code" group placeholder.
package serve

import (
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

	// Store is the shared snapshot store (live-reload) backing the
	// repo-scope nav tree's file list, shared with the page/rail/graph-data
	// handlers so a single walk serves all of them. Realm-scope nav does not
	// read Store — members/concerns/knowledge/buckets are read from the wiki
	// index and their own directories directly (unchanged).
	//
	// nil is a valid fallback for callers (tests) that construct NavOptions
	// directly without a shared store: NewNavHandler builds a private
	// one-off store rooted at RealmRoot, so repo-scope nav still works, just
	// without sharing its walk with any other handler.
	Store *snapshotStore
}

// sseLiveParam is the query parameter client-side EventSource-triggered
// nav refetch sets on GET /nav (e.g. "/nav?live=1") so the handler can skip
// computeStaleness — a git-subprocess-backed check — on every live-reload
// refresh. An ordinary navigation request (no param) still runs it.
const sseLiveParam = "live"

// isSSETriggered reports whether r carries the SSE-triggered nav marker.
func isSSETriggered(r *http.Request) bool {
	return r.URL.Query().Get(sseLiveParam) != ""
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

// ─── JSON nav tree (api_handlers.go: GET /api/nav) ─────────────────────────

// navNodeJSON is one node in the /api/nav tree. Folder nodes carry Children;
// leaf nodes carry RelPath. Fields use `json:"...,omitempty"` so a leaf's
// absent Children and a folder's absent RelPath/Stale are simply omitted.
type navNodeJSON struct {
	Label    string        `json:"label"`
	RelPath  string        `json:"relpath,omitempty"`
	Stale    bool          `json:"stale,omitempty"`
	Children []navNodeJSON `json:"children,omitempty"`
}

// navGroupJSON is one top-level group ("Realm", "Repos", "Docs", ...).
type navGroupJSON struct {
	Name  string        `json:"name"`
	Items []navNodeJSON `json:"items"`
}

// buildRealmNavGroupsJSON mirrors renderRealmNav's six groups as structured
// data instead of an HTML fragment.
func buildRealmNavGroupsJSON(opts NavOptions) []navGroupJSON {
	groups := make([]navGroupJSON, 0, 6)

	// Group 1: Realm.
	groups = append(groups, navGroupJSON{
		Name:  "Realm",
		Items: []navNodeJSON{{Label: "index", RelPath: "wiki/index.md"}},
	})

	members, _ := wiki.ReadScanMembers(opts.WikiIndexPath)

	// Group 2: Repos — each member is a navigable folder (mirrors the Buckets
	// group): an "index" leaf for the member's landing target, then the
	// member's own docs/ tree and the realm-side wiki/repos/<name>/ pages as
	// nested folder subtrees. A member with nothing to expand stays a flat leaf.
	repoItems := make([]navNodeJSON, 0, len(members))
	for _, m := range members {
		name := filepath.Base(m.Path)
		stale := opts.StaleMembers[name] || opts.StaleMembers[m.Path]
		landing := memberLinkRel(opts.RealmRoot, m)

		children := []navNodeJSON{{Label: "index", RelPath: landing}}
		memberDocsRel := filepath.ToSlash(m.Path) + "/docs"
		if docsFiles := walkMarkdownFilesRecursive(filepath.Join(opts.RealmRoot, filepath.FromSlash(memberDocsRel))); len(docsFiles) > 0 {
			children = append(children, navNodeJSON{
				Label:    "docs",
				Children: folderTreeToJSON(memberDocsRel, docsFiles),
			})
		}
		wikiRepoRel := "wiki/repos/" + name
		if wikiFiles := walkMarkdownFilesRecursive(filepath.Join(opts.RealmRoot, "wiki", "repos", name)); len(wikiFiles) > 0 {
			children = append(children, navNodeJSON{
				Label:    "wiki",
				Children: folderTreeToJSON(wikiRepoRel, wikiFiles),
			})
		}

		if len(children) == 1 {
			repoItems = append(repoItems, navNodeJSON{Label: name, RelPath: landing, Stale: stale})
			continue
		}
		repoItems = append(repoItems, navNodeJSON{Label: name, Stale: stale, Children: children})
	}
	groups = append(groups, navGroupJSON{Name: "Repos", Items: repoItems})

	// Group 3: Concerns.
	concerns := walkMarkdownFiles(filepath.Join(opts.RealmRoot, "wiki", "concerns"))
	concernItems := make([]navNodeJSON, 0, len(concerns))
	for _, name := range concerns {
		concernItems = append(concernItems, navNodeJSON{Label: stripMDExt(name), RelPath: "wiki/concerns/" + name})
	}
	groups = append(groups, navGroupJSON{Name: "Concerns", Items: concernItems})

	// Group 4: Knowledge.
	knowledge := walkMarkdownFiles(filepath.Join(opts.RealmRoot, "wiki", "knowledge"))
	knowledgeItems := make([]navNodeJSON, 0, len(knowledge))
	for _, name := range knowledge {
		knowledgeItems = append(knowledgeItems, navNodeJSON{Label: stripMDExt(name), RelPath: "wiki/knowledge/" + name})
	}
	groups = append(groups, navGroupJSON{Name: "Knowledge", Items: knowledgeItems})

	// Group 5: Buckets — each bucket is a folder node whose children are its
	// servable markdown files (recursive folder tree, same shape's
	// generic folderTreeToJSON produces for the repo-scope docs tree).
	buckets, _ := wiki.ReadBucketEntries(opts.WikiIndexPath)
	bucketItems := make([]navNodeJSON, 0, len(buckets))
	for _, b := range buckets {
		dir := b.Path
		if dir == "" {
			dir = filepath.Join(opts.RealmRoot, b.Name)
		}
		prefix := b.Name
		if rel, err := filepath.Rel(opts.RealmRoot, dir); err == nil {
			prefix = filepath.ToSlash(rel)
		}
		files := walkMarkdownFilesRecursive(dir)
		bucketItems = append(bucketItems, navNodeJSON{
			Label:    b.Name,
			Stale:    opts.BucketDiffs[b.Name],
			Children: folderTreeToJSON(prefix, files),
		})
	}
	groups = append(groups, navGroupJSON{Name: "Buckets", Items: bucketItems})

	// Group 6: External — the external-link registry route, not a /page target.
	// RelPath carries the route path (no leading "/page/" prefix is implied
	// for this one leaf; the client recognizes "external" as the dedicated
	// /external screen rather than a markdown page).
	groups = append(groups, navGroupJSON{
		Name:  "External",
		Items: []navNodeJSON{{Label: "External links registry", RelPath: "external"}},
	})

	return groups
}

// buildRepoNavGroupsJSON mirrors renderRepoNav's docs file tree as structured
// data. navPaths is the snapshot's root-relative .md file list (live-reload).
func buildRepoNavGroupsJSON(navPaths []string) []navGroupJSON {
	var topMDs []string
	var docsFiles []string
	for _, p := range navPaths {
		if !strings.Contains(p, "/") {
			topMDs = append(topMDs, p)
			continue
		}
		if rest, ok := strings.CutPrefix(p, "docs/"); ok {
			docsFiles = append(docsFiles, rest)
		}
	}

	items := make([]navNodeJSON, 0, len(topMDs)+len(docsFiles))
	for _, name := range topMDs {
		items = append(items, navNodeJSON{Label: stripMDExt(name), RelPath: name})
	}
	items = append(items, folderTreeToJSON("docs", docsFiles)...)

	return []navGroupJSON{
		{Name: "Docs", Items: items},
		{Name: "Code", Items: nil},
	}
}

// folderTreeToJSON mirrors writeNavFolderTree's traversal (top-level files
// flat, subdirectories as nested folder nodes) as navNodeJSON data instead of
// an HTML fragment.
func folderTreeToJSON(basePrefix string, files []string) []navNodeJSON {
	root := buildNavFolderTree(files)
	items := make([]navNodeJSON, 0, len(root.files)+len(root.childOrder))
	for _, rel := range root.files {
		items = append(items, navNodeJSON{Label: stripMDExt(filepath.Base(rel)), RelPath: basePrefix + "/" + rel})
	}
	for _, dirName := range root.childOrder {
		items = append(items, folderNodeToJSON(root.children[dirName], basePrefix))
	}
	return items
}

// folderNodeToJSON recursively converts one navFolderNode into a navNodeJSON
// folder (Children populated, RelPath empty).
func folderNodeToJSON(node *navFolderNode, basePrefix string) navNodeJSON {
	children := make([]navNodeJSON, 0, len(node.files)+len(node.childOrder))
	for _, rel := range node.files {
		children = append(children, navNodeJSON{Label: stripMDExt(filepath.Base(rel)), RelPath: basePrefix + "/" + rel})
	}
	for _, dirName := range node.childOrder {
		children = append(children, folderNodeToJSON(node.children[dirName], basePrefix))
	}
	return navNodeJSON{Label: node.name, Children: children}
}

// computeStaleness is the production StalenessFn.  It reads the cached
// DRIFT/STALE/STALE-bucket sets and returns two maps:
//   - staleMembers: member name (basename of path) → true
//   - bucketDiffs: bucket name → true
//
// Errors from wiki.Stale (exit code 2) are non-fatal: both maps come back
// empty.  This is intentional: a staleness-check failure must not crash the nav
// tree — it degrades to showing no badges rather than returning an error page.
//
// The function is read-only and does not write any file.
func computeStaleness(realmRoot, _ string) (staleMembers map[string]bool, bucketDiffs map[string]bool) {
	// The walk itself is shared with /api/status through navStalenessCache —
	// it is seconds long, and running it twice per page load was most of what
	// made a page load slow.
	sets := navStalenessCache.get(realmRoot)
	// Concerns are their own category: a stale concern does NOT light up the
	// member-stale badge.  Nav only surfaces member and bucket staleness.
	return sets.Members, sets.Buckets
}
