// Package signals provides scanners that produce the deterministic-signals.md document.
package signals

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// matchesSignalsIgnore reports whether rel matches any of the provided globs.
// Each glob is tested against rel directly and also against the base filename,
// so patterns like "gen.go" match both "gen.go" and "dir/gen.go".
func matchesSignalsIgnore(rel string, globs []string) bool {
	base := filepath.Base(rel)
	for _, glob := range globs {
		// Match against full repo-relative path.
		if ok, _ := filepath.Match(glob, rel); ok {
			return true
		}
		// Match against base filename (for bare-name patterns like "gen.go").
		if ok, _ := filepath.Match(glob, base); ok {
			return true
		}
	}
	return false
}

func itoa(n int) string { return strconv.Itoa(n) }

// defaultMaxDepth is the default tree depth used when no depth is configured.
const defaultMaxDepth = 3

// skipDirs is the set of directory names excluded from WalkDir-based scans
// (used when not inside a git repo). In git repos, git ls-files is the source
// of truth and skipDirs is not applied.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	".worktrees":   true,
	"tmp":          true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"vendor":       true,
}

// skipPrefixes are repo-relative path prefixes excluded in all enumeration modes.
// .claude/.scratchpad/ is working memory — not interesting for signals.
// .claude/project/ contains signals output — including it would make the scan
// non-idempotent (the output file appears in subsequent scans).
var skipPrefixes = []string{
	".claude/.scratchpad/",
	".claude/project/",
}

// enumerateFiles returns repo-relative file paths for all tracked (and
// untracked-but-not-ignored) files in root.
//
// In a git repo: shells out to git ls-files to get the authoritative list.
// Outside a git repo: walks the filesystem, applying skipDirs.
func enumerateFiles(root string) ([]string, error) {
	if isGitDir(root) {
		return enumGit(root)
	}
	return enumWalk(root)
}

func isGitDir(root string) bool {
	_, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil
}

func enumGit(root string) ([]string, error) {
	// Tracked files.
	tracked, err := gitLsFiles(root, []string{"ls-files", "-z"})
	if err != nil {
		return nil, err
	}
	// Untracked but not ignored.
	untracked, err := gitLsFiles(root, []string{"ls-files", "-z", "--others", "--exclude-standard"})
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(tracked)+len(untracked))
	all := make([]string, 0, len(tracked)+len(untracked))
	for _, p := range append(tracked, untracked...) {
		if p == "" || seen[p] {
			continue
		}
		if isSkippedPrefix(p) {
			continue
		}
		seen[p] = true
		all = append(all, p)
	}
	sort.Strings(all)
	return all, nil
}

func gitLsFiles(root string, args []string) ([]string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	// NUL-delimited output.
	parts := strings.Split(string(out), "\x00")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result, nil
}

func enumWalk(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if isSkippedPrefix(rel) {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func isSkippedPrefix(rel string) bool {
	for _, pfx := range skipPrefixes {
		if strings.HasPrefix(rel, pfx) {
			return true
		}
	}
	return false
}

// treeNode is an internal node in the in-memory directory tree.
type treeNode struct {
	name     string
	isDir    bool
	children []*treeNode
	// File nodes: per-file metadata (from single read).
	meta fileMeta
	// generated: file matched a .signalsignore glob — inferrer skips it for
	// domain content but the node (and its metadata) are still emitted.
	generated bool
	// depthCapped: this dir is at exactly max_depth+1 — render summary, no children.
	depthCapped bool
	// beyond: this dir is > max_depth+1 — elide entirely from output.
	beyond bool
}

// ScanTree returns a depth-limited (default max_depth=3) tree rendering of the
// repo at root. It uses enumerateFiles as the source of truth, so in git repos
// dotfile directories like .claude/ and .github/ appear when they contain
// tracked files. Branch glyphs: ├── for non-last entries, └── for last;
// continuation prefix is "│   " or "    " depending on whether the parent has
// more siblings.
//
// Per-file metadata format (for files at depth ≤ max_depth):
//
//	<filename> (<sha>, <lines>L, <chars>ch, <bytes>B)
//
// Directories at max_depth+1: <dirname>/ (<N> files, <M> dirs)
// Directories > max_depth+1: elided (appear only as counts in parent summary)
func ScanTree(root string) (string, error) {
	return ScanTreeWithOptions(root, nil)
}

// scanTreeWithMetaCache is the internal variant used by assembleBody. It returns
// the tree rendering AND the metadata cache built during the tree pass (rel →
// fileMeta for all non-beyond files). The cache lets assembleBody avoid a second
// os.ReadFile call for those files in the language-LOC pass.
func scanTreeWithMetaCache(root string, opts *Options) (string, map[string]fileMeta, error) {
	tree, cache, err := scanTreeInternal(root, opts)
	return tree, cache, err
}

// ScanTreeWithOptions is like ScanTree but reads MaxDepth, ExcludeGlobs, and
// GeneratedGlobs from opts.
// When opts is nil or opts.MaxDepth is 0, defaultMaxDepth (3) is used.
// When opts is nil and both glob slices are empty, .signalsignore is read from root.
// ExcludeGlobs: matching files are omitted from the tree entirely (not shown).
// GeneratedGlobs: matching files appear in the tree with a [generated] marker.
func ScanTreeWithOptions(root string, opts *Options) (string, error) {
	tree, _, err := scanTreeInternal(root, opts)
	return tree, err
}

// scanTreeInternal is the shared implementation for ScanTreeWithOptions and
// scanTreeWithMetaCache. It returns the rendered tree and the per-file metadata
// cache populated during the read pass.
func scanTreeInternal(root string, opts *Options) (string, map[string]fileMeta, error) {
	maxDepth := defaultMaxDepth
	if opts != nil && opts.MaxDepth > 0 {
		maxDepth = opts.MaxDepth
	}

	// Load .signalsignore globs. When opts already has globs set
	// (populated by ScanWithOptions), skip the file read to avoid double I/O.
	var excludeGlobs, generatedGlobs []string
	if opts != nil && (len(opts.ExcludeGlobs) > 0 || len(opts.GeneratedGlobs) > 0) {
		excludeGlobs = opts.ExcludeGlobs
		generatedGlobs = opts.GeneratedGlobs
	} else {
		excl, gen, err := readSignalsIgnore(root)
		if err != nil {
			return "", nil, fmt.Errorf("tree scanner: %w", err)
		}
		excludeGlobs = excl
		generatedGlobs = gen
	}

	files, err := enumerateFiles(root)
	if err != nil {
		return "", nil, err
	}

	// Filter out files matching ExcludeGlobs before building the tree.
	if len(excludeGlobs) > 0 {
		kept := files[:0]
		for _, rel := range files {
			if !matchesSignalsIgnore(rel, excludeGlobs) {
				kept = append(kept, rel)
			}
		}
		files = kept
	}

	if len(files) == 0 {
		return "", map[string]fileMeta{}, nil
	}

	// Build a tree from the flat file list.
	rootNode := &treeNode{name: ".", isDir: true}

	for _, rel := range files {
		// Forward-slash normalize (git ls-files uses / on all platforms).
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")

		// Ensure all ancestor directories exist in the tree.
		cur := rootNode
		for d := 0; d < len(parts)-1; d++ {
			seg := parts[d]
			var found *treeNode
			for _, c := range cur.children {
				if c.name == seg && c.isDir {
					found = c
					break
				}
			}
			if found == nil {
				found = &treeNode{name: seg, isDir: true}
				cur.children = append(cur.children, found)
			}
			cur = found
		}

		fname := parts[len(parts)-1]
		cur.children = append(cur.children, &treeNode{name: fname, isDir: false})
	}

	// Sort each node's children: dirs before files, alphabetically within group.
	sortTree(rootNode)

	// Mark directory nodes as depthCapped or beyond based on depth.
	markDepths(rootNode, 1, maxDepth)

	// Build a map from repo-relative path → *treeNode for file nodes only.
	// Used to load per-file metadata in one pass.
	fileNodeByRel := make(map[string]*treeNode, len(files))
	buildFileMap(rootNode, "", fileNodeByRel)

	// Load per-file metadata for all non-hidden (non-beyond) file nodes.
	// Files are hidden only when their parent dir is depthCapped or beyond
	// (markAllBeyond propagates the flag). A single file read computes all 4
	// metadata fields (SHA, lines, chars, bytes) at once — no double reads.
	// Also mark generated nodes based on GeneratedGlobs from .signalsignore.
	//
	// The metadata is also returned as a cache (metaCache) so assembleBody can
	// pass it to scanLanguagesFromCache — avoiding a second os.ReadFile call for
	// every non-beyond file (f-2: double-read elimination).
	metaCache := make(map[string]fileMeta, len(fileNodeByRel))
	for rel, node := range fileNodeByRel {
		if len(generatedGlobs) > 0 && matchesSignalsIgnore(rel, generatedGlobs) {
			node.generated = true
		}
		if !node.beyond {
			absPath := filepath.Join(root, filepath.FromSlash(rel))
			if m, err := readFileMeta(absPath); err == nil {
				node.meta = m
				metaCache[rel] = m
			}
		}
	}

	// Render using tree glyphs.
	var sb strings.Builder
	renderTree(rootNode, "", &sb)

	result := sb.String()
	result = strings.TrimRight(result, "\n")
	return result, metaCache, nil
}

// sortTree sorts children of every directory node: dirs before files,
// alphabetically within each group.
func sortTree(n *treeNode) {
	sort.Slice(n.children, func(i, j int) bool {
		ci, cj := n.children[i], n.children[j]
		if ci.isDir != cj.isDir {
			return ci.isDir
		}
		return ci.name < cj.name
	})
	for _, c := range n.children {
		if c.isDir {
			sortTree(c)
		}
	}
}

// markDepths marks directory nodes at depth == maxDepth+1 as depthCapped,
// and nodes at depth > maxDepth+1 as beyond.
// depth is 1-based: rootNode's children are at depth 1.
// Files are never marked directly — they inherit visibility from their parent dir.
func markDepths(n *treeNode, depth, maxDepth int) {
	for _, c := range n.children {
		if !c.isDir {
			continue
		}
		if depth == maxDepth+1 {
			c.depthCapped = true
			markAllBeyond(c)
		} else if depth > maxDepth+1 {
			c.beyond = true
			markAllBeyond(c)
		} else {
			markDepths(c, depth+1, maxDepth)
		}
	}
}

// markAllBeyond marks all descendants of n as beyond.
func markAllBeyond(n *treeNode) {
	for _, c := range n.children {
		c.beyond = true
		if c.isDir {
			markAllBeyond(c)
		}
	}
}

// buildFileMap populates m with repo-relative path → *treeNode for all file nodes.
func buildFileMap(n *treeNode, prefix string, m map[string]*treeNode) {
	for _, c := range n.children {
		var p string
		if prefix == "" {
			p = c.name
		} else {
			p = prefix + "/" + c.name
		}
		if c.isDir {
			buildFileMap(c, p, m)
		} else {
			m[p] = c
		}
	}
}

// renderTree writes the tree output for n's children into sb.
func renderTree(n *treeNode, prefix string, sb *strings.Builder) {
	// Collect visible children (exclude beyond nodes).
	visible := make([]*treeNode, 0, len(n.children))
	for _, c := range n.children {
		if !c.beyond {
			visible = append(visible, c)
		}
	}

	for i, child := range visible {
		isLast := i == len(visible)-1
		connector := "├── "
		if isLast {
			connector = "└── "
		}

		if child.isDir {
			var label string
			if child.depthCapped {
				// max_depth+1: show file/dir summary.
				// Count direct file children and direct dir children of this capped node.
				df, dd := directChildCounts(child)
				label = child.name + "/ (" + pluralize(df, "file") + ", " + pluralize(dd, "dir") + ")"
			} else {
				// Normal: show count of visible (non-beyond) children.
				vc := 0
				for _, c := range child.children {
					if !c.beyond {
						vc++
					}
				}
				label = child.name + "/ (" + itoa(vc) + ")"
			}
			sb.WriteString(prefix + connector + label + "\n")
			if !child.depthCapped && len(child.children) > 0 {
				var childPrefix string
				if isLast {
					childPrefix = prefix + "    "
				} else {
					childPrefix = prefix + "│   "
				}
				renderTree(child, childPrefix, sb)
			}
		} else {
			// File node: append metadata when available (depth ≤ maxDepth).
			name := child.name
			if child.meta.sha != "" {
				name += fmt.Sprintf(" (%s, %dL, %dch, %dB)",
					child.meta.sha, child.meta.lines, child.meta.chars, child.meta.bytes)
			}
			if child.generated {
				name += " [generated]"
			}
			sb.WriteString(prefix + connector + name + "\n")
		}
	}
}

// directChildCounts returns the number of direct file children and direct dir
// children of a depth-capped node. Used for the summary annotation.
// Direct means immediate — no recursion.
func directChildCounts(n *treeNode) (files, dirs int) {
	for _, c := range n.children {
		if c.isDir {
			dirs++
		} else {
			files++
		}
	}
	return files, dirs
}

// pluralize returns "N word" or "N words" (singular when N==1).
func pluralize(n int, word string) string {
	if n == 1 {
		return itoa(n) + " " + word
	}
	return itoa(n) + " " + word + "s"
}
