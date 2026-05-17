// Package signals provides scanners that produce the deterministic-signals.md document.
package signals

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// skipDirs is the set of directory names excluded from tree and stale scans.
var skipDirs = map[string]bool{
	".git":         true,
	".claude":      true,
	"node_modules": true,
	".worktrees":   true,
	"tmp":          true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"vendor":       true,
}

const maxDepth = 3

// ScanTree returns a depth-limited (max 3) sorted listing of the repo tree.
// Each entry is indented by two spaces per depth level. Directories end with "/".
// The skip set (skipDirs) is applied at every level.
func ScanTree(root string) (string, error) {
	var lines []string
	if err := walkTree(root, root, 0, &lines); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func walkTree(root, dir string, depth int, lines *[]string) error {
	if depth > maxDepth {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// Sort: directories first, then files, both alphabetically.
	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di // dirs before files
		}
		return entries[i].Name() < entries[j].Name()
	})

	indent := strings.Repeat("  ", depth)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if skipDirs[name] {
				continue
			}
			*lines = append(*lines, indent+name+"/")
			if depth < maxDepth {
				if err := walkTree(root, filepath.Join(dir, name), depth+1, lines); err != nil {
					return err
				}
			}
		} else {
			*lines = append(*lines, indent+name)
		}
	}
	return nil
}
