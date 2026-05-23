package signals

import (
	"os"
	"sort"
	"strings"
)

// ChangedPaths holds the result of a content-SHA diff between two consecutive
// deterministic scans. All slices are sorted for deterministic output.
type ChangedPaths struct {
	// Changed holds paths whose content SHA differs between prev and current.
	Changed []string
	// Added holds paths present in current but absent in prev.
	Added []string
	// Removed holds paths present in prev but absent in current.
	Removed []string
}

// ParseTreeSHAs extracts a path→SHA map from the body of a deterministic-signals
// document. Only the ## Tree section is parsed. Paths tagged [generated] are
// excluded — generated files do not drive domain refresh (spec §Change detection).
//
// Line format expected inside ## Tree:
//
//	<glyphs> <path> (<sha7>, <NL>L, <Nch>ch, <NB>B) [generated]
//
// Directory lines (ending with /) and summary lines (N files, M dirs) carry no
// SHA and are silently skipped. The parser tracks the directory stack using the
// indentation depth encoded by 4-char prefix blocks ("│   " or "    ") so that
// two files with the same leaf name in different directories produce distinct keys.
//
// Why 7-char SHA detection works: renderTree emits per-file metadata as
// "(<sha7>, <N>L, <N>ch, <N>B)" where sha7 is the 7-char hex prefix of the
// file's SHA-256. Directory annotations are either plain integers ("3") or
// "N files, M dirs" — neither begins with a 7-char alphanum token followed by
// ", ". This property is stable as long as renderTree's format is unchanged.
func ParseTreeSHAs(content string) map[string]string {
	shas := make(map[string]string)

	// dirStack holds the directory names at each nesting depth (0-based).
	// depth 0 = immediate children of the repo root.
	var dirStack []string

	inTree := false
	for _, line := range strings.Split(content, "\n") {
		// Detect the ## Tree section header.
		if strings.TrimSpace(line) == "## Tree" {
			inTree = true
			continue
		}
		// Stop at the next ## heading.
		if inTree && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			break
		}
		if !inTree {
			continue
		}

		// Skip [generated] lines before any further parsing.
		if strings.Contains(line, "[generated]") {
			continue
		}

		// Determine the nesting depth from the prefix length.
		// Each depth level is encoded by a 4-char block ("│   " or "    ") before
		// the connector ("├── " or "└── "). Depth 0 = no prefix blocks.
		depth := lineDepth(line)

		// Pop stack to current depth before pushing new dir.
		if depth < len(dirStack) {
			dirStack = dirStack[:depth]
		}

		// A file line has a metadata block "(sha, NL, Nch, NB)".
		// Directory lines end with "/ (N)" or "/ (N files, M dirs)" — no SHA.
		open := strings.Index(line, "(")
		close := strings.LastIndex(line, ")")
		if open == -1 || close == -1 || close <= open {
			continue
		}

		meta := line[open+1 : close]
		// Meta must start with a 7-char SHA followed by ", ".
		// Directory summaries look like "2 files, 1 dir" or "3" — they contain
		// spaces or are numeric-only, not a 7-char alphanum token.
		parts := strings.SplitN(meta, ", ", 2)
		sha := parts[0]
		if len(sha) != 7 || strings.ContainsAny(sha, " \t") {
			// Not a file-metadata line — could be a directory annotation.
			// If it's a directory entry (leaf ends with /), push to stack.
			raw := line[:open]
			leaf := stripTreeGlyphs(raw)
			if strings.HasSuffix(leaf, "/") {
				dirStack = append(dirStack[:depth], strings.TrimSuffix(leaf, "/"))
			}
			continue
		}

		// Extract the leaf name: strip tree glyphs from the part before the " (".
		raw := line[:open]
		leaf := stripTreeGlyphs(raw)
		if leaf == "" || strings.HasSuffix(leaf, "/") {
			continue
		}

		// Reconstruct the repo-relative path from the current directory stack.
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

// lineDepth returns the nesting depth of a tree line based on the number of
// 4-rune prefix blocks ("│   " or "    ") before the "├── " or "└── "
// connector. Depth 0 means the line is a direct child of the root.
// Note: "│" is a 3-byte UTF-8 rune (U+2502), so byte indexing cannot be used.
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

// DiffSHAs computes the structural diff between two path→SHA maps.
// [generated] paths are already excluded by ParseTreeSHAs; callers that
// build their own maps must ensure generated paths are not included.
// All output slices are sorted.
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

// DiffPaths reads the prev and current deterministic-signals files from the
// repo at root, parses their ## Tree sections, and returns the structural
// content-SHA diff. No git commands are used — this works in any directory.
//
// If the prev file is absent (first scan), Added contains all paths from the
// current scan and Changed/Removed are empty.
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
			// No prior scan — everything in current is "added".
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

// stripTreeGlyphs removes the leading tree-drawing prefix characters and
// trailing whitespace, returning just the path component.
// Input example: "│   ├── main.go " → "main.go"
func stripTreeGlyphs(s string) string {
	// Replace tree-drawing runes with spaces, then trim.
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
