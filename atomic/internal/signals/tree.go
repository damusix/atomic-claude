// Package signals scans a repo into the deterministic substrate document.
package signals

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// matchesSignalsIgnore tests each glob against the full repo-relative path and
// against the basename, so "gen.go" matches both "gen.go" and "dir/gen.go".
func matchesSignalsIgnore(rel string, globs []string) bool {
	base := filepath.Base(rel)
	for _, glob := range globs {
		if ok, _ := filepath.Match(glob, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(glob, base); ok {
			return true
		}
	}
	return false
}

func itoa(n int) string { return strconv.Itoa(n) }

const defaultMaxDepth = 3

// skipDirs applies only to WalkDir scans; inside a git repo, git ls-files is the
// source of truth.
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

// skippedPrefixes excludes, in every enumeration mode: the harness-relative
// scratchpad (working memory) and project (legacy signals output) dirs, and the
// fixed docs/wiki/ path. Scanning docs/wiki/ would be self-referential — the
// inferrer stamps a <scan-sha> into index.md, changing its blob SHA, changing
// the scan tree, leaving `atomic signals stale` at exit 1 forever.
func skippedPrefixes() []string {
	return []string{
		config.ScratchpadDir("") + "/",
		config.ProjectDir("") + "/",
		"docs/wiki/",
	}
}

// enumerateFiles returns repo-relative paths for tracked and
// untracked-but-not-ignored files, via git ls-files or a skipDirs walk.
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
	tracked, err := gitLsFiles(root, []string{"ls-files", "-z"})
	if err != nil {
		return nil, err
	}
	untracked, err := gitLsFiles(root, []string{"ls-files", "-z", "--others", "--exclude-standard"})
	if err != nil {
		return nil, err
	}

	prefixes := skippedPrefixes()
	seen := make(map[string]bool, len(tracked)+len(untracked))
	all := make([]string, 0, len(tracked)+len(untracked))
	for _, p := range append(tracked, untracked...) {
		if p == "" || seen[p] {
			continue
		}
		if isSkippedPrefix(p, prefixes) {
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
	prefixes := skippedPrefixes()
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
		if isSkippedPrefix(rel, prefixes) {
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

func isSkippedPrefix(rel string, prefixes []string) bool {
	for _, pfx := range prefixes {
		if strings.HasPrefix(rel, pfx) {
			return true
		}
	}
	return false
}

type treeNode struct {
	name     string
	isDir    bool
	children []*treeNode
	meta     fileMeta
	// generated files stay in the output with their metadata; only the inferrer
	// skips them for domain content.
	generated bool
	// depthCapped is a dir at exactly max_depth+1: summary, no children.
	depthCapped bool
	// beyond is past max_depth+1: elided entirely.
	beyond bool
}

// ScanTree renders a depth-limited tree of the repo at root. Because
// enumerateFiles is the source of truth, dotfile dirs like .claude/ appear in
// git repos when they hold tracked files.
//
// Files at depth ≤ max_depth carry "(<sha>, <lines>L, <chars>ch, <bytes>B)";
// dirs at max_depth+1 collapse to "(<N> files, <M> dirs)"; deeper dirs are
// elided, surviving only as counts in that summary.
func ScanTree(root string) (string, error) {
	return ScanTreeWithOptions(root, nil)
}

// scanTreeWithMetaCache also returns the rel → fileMeta cache built during the
// tree pass, sparing assembleBody a second read per file in the language pass.
func scanTreeWithMetaCache(root string, opts *Options) (string, map[string]fileMeta, error) {
	tree, cache, err := scanTreeInternal(root, opts)
	return tree, cache, err
}

// ScanTreeWithOptions takes MaxDepth and the globs from opts, falling back to
// defaultMaxDepth and to reading .signalsignore when they are unset.
func ScanTreeWithOptions(root string, opts *Options) (string, error) {
	tree, _, err := scanTreeInternal(root, opts)
	return tree, err
}

func scanTreeInternal(root string, opts *Options) (string, map[string]fileMeta, error) {
	maxDepth := defaultMaxDepth
	if opts != nil && opts.MaxDepth > 0 {
		maxDepth = opts.MaxDepth
	}

	// Globs already on opts came from ScanWithOptions; re-reading would be double I/O.
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

	rootNode := &treeNode{name: ".", isDir: true}

	for _, rel := range files {
		// git ls-files emits "/" on every platform.
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")

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

	sortTree(rootNode)

	markDepths(rootNode, 1, maxDepth)

	fileNodeByRel := make(map[string]*treeNode, len(files))
	buildFileMap(rootNode, "", fileNodeByRel)

	// One read per non-beyond file yields all four metadata fields at once, and
	// the cache hands them to the later language pass instead of re-reading.
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

	var sb strings.Builder
	renderTree(rootNode, "", &sb)

	result := sb.String()
	result = strings.TrimRight(result, "\n")
	return result, metaCache, nil
}

// sortTree orders dirs before files, alphabetically within each group.
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

// markDepths takes a 1-based depth (rootNode's children are 1). Files are never
// marked directly; they inherit visibility from their parent dir.
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

func markAllBeyond(n *treeNode) {
	for _, c := range n.children {
		c.beyond = true
		if c.isDir {
			markAllBeyond(c)
		}
	}
}

// buildFileMap populates m with repo-relative path → node, file nodes only.
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

func renderTree(n *treeNode, prefix string, sb *strings.Builder) {
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
				df, dd := directChildCounts(child)
				label = child.name + "/ (" + pluralize(df, "file") + ", " + pluralize(dd, "dir") + ")"
			} else {
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

// directChildCounts counts immediate children only, for the capped-dir summary.
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

func pluralize(n int, word string) string {
	if n == 1 {
		return itoa(n) + " " + word
	}
	return itoa(n) + " " + word + "s"
}
