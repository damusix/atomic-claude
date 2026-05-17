// Package signals provides scanners that produce the deterministic-signals.md document.
package signals

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

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

const maxDepth = 3

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

// ScanTree returns a depth-limited (max 3) tree rendering of the repo at root.
// It uses enumerateFiles as the source of truth, so in git repos dotfile
// directories like .claude/ and .github/ appear when they contain tracked files.
// Branch glyphs: ├── for non-last entries, └── for last; continuation prefix
// is "│   " or "    " depending on whether the parent has more siblings.
func ScanTree(root string) (string, error) {
	files, err := enumerateFiles(root)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", nil
	}

	// Build a tree from the flat file list.
	type node struct {
		name     string
		isDir    bool
		children []*node
	}

	rootNode := &node{name: ".", isDir: true}

	for _, rel := range files {
		// Forward-slash normalize (git ls-files uses / on all platforms).
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")

		// Ensure all ancestor directories exist in the tree.
		cur := rootNode
		for d := 0; d < len(parts)-1; d++ {
			seg := parts[d]
			// Find or create child dir node.
			var found *node
			for _, c := range cur.children {
				if c.name == seg && c.isDir {
					found = c
					break
				}
			}
			if found == nil {
				found = &node{name: seg, isDir: true}
				cur.children = append(cur.children, found)
			}
			cur = found
		}

		// Add the file node.
		fname := parts[len(parts)-1]
		fileNode := &node{name: fname, isDir: false}
		cur.children = append(cur.children, fileNode)
	}

	// Sort each node's children: dirs before files, alphabetically within group.
	var sortNode func(n *node)
	sortNode = func(n *node) {
		sort.Slice(n.children, func(i, j int) bool {
			ci, cj := n.children[i], n.children[j]
			if ci.isDir != cj.isDir {
				return ci.isDir // dirs first
			}
			return ci.name < cj.name
		})
		for _, c := range n.children {
			if c.isDir {
				sortNode(c)
			}
		}
	}
	sortNode(rootNode)

	// Prune directory nodes deeper than maxDepth. Directories at depth maxDepth
	// still show their immediate file children, but not subdirectories.
	// depth counts from 1 at root's children.
	var pruneNode func(n *node, depth int)
	pruneNode = func(n *node, depth int) {
		if depth >= maxDepth {
			// At max depth: keep file children, drop directory children.
			kept := make([]*node, 0, len(n.children))
			for _, c := range n.children {
				if !c.isDir {
					kept = append(kept, c)
				}
			}
			n.children = kept
			return
		}
		for _, c := range n.children {
			if c.isDir {
				pruneNode(c, depth+1)
			}
		}
	}
	pruneNode(rootNode, 0)

	// Render using tree glyphs.
	var sb strings.Builder
	var render func(n *node, prefix string)
	render = func(n *node, prefix string) {
		for i, child := range n.children {
			isLast := i == len(n.children)-1
			connector := "├── "
			if isLast {
				connector = "└── "
			}
			name := child.name
			if child.isDir {
				name += "/"
			}
			sb.WriteString(prefix + connector + name + "\n")
			if child.isDir && len(child.children) > 0 {
				var childPrefix string
				if isLast {
					childPrefix = prefix + "    "
				} else {
					childPrefix = prefix + "│   "
				}
				render(child, childPrefix)
			}
		}
	}
	render(rootNode, "")

	result := sb.String()
	// Trim trailing newline.
	result = strings.TrimRight(result, "\n")
	return result, nil
}
