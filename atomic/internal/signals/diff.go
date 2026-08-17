package signals

import (
	"os"
	"sort"
	"strings"
)

// ChangedPaths is a content-SHA diff between two consecutive scans. All slices
// are sorted.
type ChangedPaths struct {
	// Changed holds paths whose content SHA differs between prev and current.
	Changed []string
	// Added holds paths present in current but absent in prev.
	Added []string
	// Removed holds paths present in prev but absent in current.
	Removed []string
}

// ParseTreeSHAs extracts a path→SHA map from a scan document's ## Tree section,
// dropping [generated] paths, which do not drive domain refresh.
//
// The directory stack is tracked from indentation so two files sharing a leaf
// name in different directories get distinct keys. A file line is recognized by
// a metadata block opening with a 7-char alphanum token and ", "; directory
// annotations ("3", "2 files, 1 dir") never take that shape. That distinction
// holds only as long as renderTree's format does.
func ParseTreeSHAs(content string) map[string]string {
	shas := make(map[string]string)

	// dirStack is 0-based: depth 0 holds immediate children of the repo root.
	var dirStack []string

	inTree := false
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "## Tree" {
			inTree = true
			continue
		}
		if inTree && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			break
		}
		if !inTree {
			continue
		}

		if strings.Contains(line, "[generated]") {
			continue
		}

		depth := lineDepth(line)

		if depth < len(dirStack) {
			dirStack = dirStack[:depth]
		}

		open := strings.Index(line, "(")
		close := strings.LastIndex(line, ")")
		if open == -1 || close == -1 || close <= open {
			continue
		}

		meta := line[open+1 : close]
		parts := strings.SplitN(meta, ", ", 2)
		sha := parts[0]
		if len(sha) != 7 || strings.ContainsAny(sha, " \t") {
			// A directory entry instead — push its leaf onto the stack.
			raw := line[:open]
			leaf := stripTreeGlyphs(raw)
			if strings.HasSuffix(leaf, "/") {
				dirStack = append(dirStack[:depth], strings.TrimSuffix(leaf, "/"))
			}
			continue
		}

		raw := line[:open]
		leaf := stripTreeGlyphs(raw)
		if leaf == "" || strings.HasSuffix(leaf, "/") {
			continue
		}

		var fullPath string
		if len(dirStack) == 0 {
			fullPath = leaf
		} else {
			fullPath = strings.Join(dirStack, "/") + "/" + leaf
		}

		shas[fullPath] = sha
	}
	return shas
}

// lineDepth counts 4-rune prefix blocks before the connector; 0 is a direct
// child of the root. "│" is a 3-byte rune, so byte indexing will not do.
func lineDepth(line string) int {
	runes := []rune(line)
	depth := 0
	i := 0
	for i+4 <= len(runes) {
		chunk := string(runes[i : i+4])
		if chunk == "│   " || chunk == "    " {
			depth++
			i += 4
		} else {
			break
		}
	}
	return depth
}

// DiffSHAs diffs two path→SHA maps, output sorted. ParseTreeSHAs already drops
// [generated] paths; callers building their own maps must do the same.
func DiffSHAs(prev, current map[string]string) ChangedPaths {
	var cp ChangedPaths

	for path, sha := range current {
		if prevSHA, ok := prev[path]; !ok {
			cp.Added = append(cp.Added, path)
		} else if prevSHA != sha {
			cp.Changed = append(cp.Changed, path)
		}
	}

	for path := range prev {
		if _, ok := current[path]; !ok {
			cp.Removed = append(cp.Removed, path)
		}
	}

	sort.Strings(cp.Changed)
	sort.Strings(cp.Added)
	sort.Strings(cp.Removed)
	return cp
}

// DiffPaths diffs the stored prev and current scans, using no git commands so it
// works in any directory. A missing prev file yields everything under Added.
func DiffPaths(root string) (ChangedPaths, error) {
	currentPath := SignalsPath(root)
	prevPath := PrevPath(root)

	currentContent, err := os.ReadFile(currentPath)
	if err != nil {
		return ChangedPaths{}, err
	}
	currentSHAs := ParseTreeSHAs(string(currentContent))

	prevContent, readErr := os.ReadFile(prevPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			var added []string
			for path := range currentSHAs {
				added = append(added, path)
			}
			sort.Strings(added)
			return ChangedPaths{Added: added}, nil
		}
		return ChangedPaths{}, readErr
	}

	prevSHAs := ParseTreeSHAs(string(prevContent))
	return DiffSHAs(prevSHAs, currentSHAs), nil
}

// stripTreeGlyphs turns "│   ├── main.go " into "main.go"
func stripTreeGlyphs(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '├', '└', '─', '│':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
