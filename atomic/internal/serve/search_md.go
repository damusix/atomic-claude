// search_md.go — markdown full-text search (/api/search/md).
//
// Performs a literal, case-insensitive substring search across all *.md files
// reachable from NavRoot (using shouldSkipDir for directory filtering).
//
// For each matching line the handler emits one result item:
//
//	file path (realm-root-relative)  ·  line number  ·  trimmed snippet
//
// Design constraints:
//   - Empty/whitespace query → empty fragment (200).
//   - Results capped at 50; a truncation note is appended when the cap fires.
//   - Query is treated as a literal substring to grep, not a file path;
//     no filesystem access is performed on the query value itself.
//   - Snippet is trimmed to ≤120 chars to stay usable in a narrow dropdown.
package serve

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	mdSearchResultCap     = 50
	mdSearchSnippetMaxLen = 120
)

// MdSearchOptions configures the /api/search/md handler (NewAPIMdSearchHandler).
type MdSearchOptions struct {
	// NavRoot is the directory to walk for .md files.
	// Subdirectories matching shouldSkipDir are excluded.
	NavRoot string
}

// mdSearchHandler holds the search root for /api/search/md.
type mdSearchHandler struct {
	navRoot string
}

// mdMatch is one matching line inside a .md file.
type mdMatch struct {
	// RelPath is the file path relative to NavRoot (forward slashes).
	RelPath string
	// Line is the 1-based line number of the match.
	Line int
	// Snippet is a trimmed excerpt of the matching line.
	Snippet string
}

// search walks navRoot and collects up to mdSearchResultCap matching lines.
// Returns the matches and a bool indicating whether the cap was hit.
func (h *mdSearchHandler) search(query string) ([]mdMatch, bool) {
	lower := strings.ToLower(query)
	var matches []mdMatch
	truncated := false

	// A non-nil callback return signals the result cap was hit; intentionally discarded.
	_ = filepath.WalkDir(h.navRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if path == h.navRoot {
				return nil // never skip the root itself
			}
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || hiddenFile(d.Name()) {
			return nil
		}

		relPath, relErr := filepath.Rel(h.navRoot, path)
		if relErr != nil {
			return nil
		}
		// Normalize to forward slashes for URL construction.
		relPath = filepath.ToSlash(relPath)

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		lines := strings.Split(string(data), "\n")
		for lineIdx, line := range lines {
			if strings.Contains(strings.ToLower(line), lower) {
				snippet := strings.TrimSpace(line)
				if len(snippet) > mdSearchSnippetMaxLen {
					snippet = snippet[:mdSearchSnippetMaxLen] + "…"
				}
				matches = append(matches, mdMatch{
					RelPath: relPath,
					Line:    lineIdx + 1,
					Snippet: snippet,
				})
				if len(matches) >= mdSearchResultCap {
					truncated = true
					// Signal early termination by returning a sentinel.
					return fmt.Errorf("cap") // WalkDir will stop; error is discarded by caller
				}
			}
		}
		return nil
	})

	return matches, truncated
}
