// Nav tree data behind /api/nav. Realm scope produces six groups (Realm,
// Repos, Concerns, Knowledge, Buckets, External) with staleness badges; repo
// scope produces the docs file tree plus a Code placeholder.
package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// StalenessFn computes the stale-member and dirty-bucket sets that badge nav
// nodes. Implementations must be read-only and must not hang on missing git.
type StalenessFn func(realmRoot, wikiIndexPath string) (staleMembers map[string]bool, bucketDiffs map[string]bool)

// NavOptions configures the nav tree. Exported so tests can construct it.
type NavOptions struct {
	// RealmRoot is the root directory being served.
	RealmRoot string

	// IsRealmScope false means repo/member scope: render the docs file tree.
	IsRealmScope bool

	// WikiIndexPath is required when IsRealmScope is true.
	WikiIndexPath string

	// StaleMembers is pre-computed and overridden by StalenessFn when both
	// are set. Deprecated: prefer StalenessFn.
	StaleMembers map[string]bool

	// BucketDiffs is pre-computed and overridden by StalenessFn when both
	// are set. Deprecated: prefer StalenessFn.
	BucketDiffs map[string]bool

	// StalenessFn nil takes computeStaleness; tests inject a stub to avoid
	// disk and git I/O.
	StalenessFn StalenessFn

	// Store backs the repo-scope file list, shared with the page, rail, and
	// graph handlers so one walk serves them all. Realm-scope nav reads the
	// wiki index instead. A nil Store gets a private one rooted at RealmRoot.
	Store *snapshotStore
}

// sseLiveParam marks a nav refetch triggered by the live-reload stream, so the
// handler can skip the git-backed staleness check on every refresh. An
// ordinary navigation still runs it.
const sseLiveParam = "live"

// isSSETriggered reports whether r carries the SSE-triggered nav marker.
func isSSETriggered(r *http.Request) bool {
	return r.URL.Query().Get(sseLiveParam) != ""
}

// memberLinkRel returns a member's page target, mirroring the wiki index's
// Members block so nav and index never disagree. It never guesses
// "wiki/repos/<name>.md" — summaries exist only for summarized members, and
// guessing produced 404s in the nav.
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

// navFolderNode is one directory in the nav tree. childOrder exists to keep
// traversal deterministic, which the map alone cannot.
type navFolderNode struct {
	name       string
	childOrder []string
	children   map[string]*navFolderNode
	files      []string
}

// buildNavFolderTree builds the tree from slash-separated relative paths.
func buildNavFolderTree(paths []string) *navFolderNode {
	root := &navFolderNode{children: make(map[string]*navFolderNode)}
	for _, rel := range paths {
		parts := strings.Split(rel, "/")
		node := root
		for i, part := range parts {
			if i == len(parts)-1 {
				node.files = append(node.files, rel)
			} else {
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

// walkMarkdownFiles returns the sorted *.md basenames directly in dir.
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

// walkMarkdownFilesRecursive returns sorted *.md paths relative to dir.
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
		results = append(results, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(results)
	return results
}

func stripMDExt(name string) string {
	return strings.TrimSuffix(name, ".md")
}

// navNodeJSON is one node in the /api/nav tree: folders carry Children, leaves
// carry RelPath, and omitempty keeps the unused half out of the payload.
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

// buildRealmNavGroupsJSON builds the six realm-scope nav groups.
func buildRealmNavGroupsJSON(opts NavOptions) []navGroupJSON {
	groups := make([]navGroupJSON, 0, 6)

	groups = append(groups, navGroupJSON{
		Name:  "Realm",
		Items: []navNodeJSON{{Label: "index", RelPath: "wiki/index.md"}},
	})

	members, _ := wiki.ReadScanMembers(opts.WikiIndexPath)

	// A member with nothing to expand stays a flat leaf rather than a folder
	// holding only its own index.
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

	concerns := walkMarkdownFiles(filepath.Join(opts.RealmRoot, "wiki", "concerns"))
	concernItems := make([]navNodeJSON, 0, len(concerns))
	for _, name := range concerns {
		concernItems = append(concernItems, navNodeJSON{Label: stripMDExt(name), RelPath: "wiki/concerns/" + name})
	}
	groups = append(groups, navGroupJSON{Name: "Concerns", Items: concernItems})

	knowledge := walkMarkdownFiles(filepath.Join(opts.RealmRoot, "wiki", "knowledge"))
	knowledgeItems := make([]navNodeJSON, 0, len(knowledge))
	for _, name := range knowledge {
		knowledgeItems = append(knowledgeItems, navNodeJSON{Label: stripMDExt(name), RelPath: "wiki/knowledge/" + name})
	}
	groups = append(groups, navGroupJSON{Name: "Knowledge", Items: knowledgeItems})

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

	// RelPath here is a route, not a page: the client recognises "external" as
	// its own screen rather than a markdown path.
	groups = append(groups, navGroupJSON{
		Name:  "External",
		Items: []navNodeJSON{{Label: "External links registry", RelPath: "external"}},
	})

	return groups
}

// buildRepoNavGroupsJSON builds the repo-scope docs tree from the snapshot's
// root-relative .md file list.
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

// folderTreeToJSON flattens top-level files and nests subdirectories.
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

// folderNodeToJSON converts one node into a folder entry, leaving RelPath empty.
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

// computeStaleness is the production StalenessFn. A staleness-check failure
// yields empty maps rather than an error: the nav degrades to no badges
// instead of failing the page.
func computeStaleness(realmRoot, _ string) (staleMembers map[string]bool, bucketDiffs map[string]bool) {
	// Cached because the walk takes seconds and /api/status wants it too.
	sets := navStalenessCache.get(realmRoot)
	// Concerns are deliberately excluded — a stale concern does not badge its
	// member.
	return sets.Members, sets.Buckets
}
