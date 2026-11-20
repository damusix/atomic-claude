// Markdown full-text search behind /api/search/md: a literal,
// case-insensitive substring scan of every *.md file under NavRoot. The query
// is only ever grepped for, never resolved as a path, so it reaches no
// filesystem call of its own.
package serve

import (
	"fmt"
	"io/fs"
	"strings"
)

const (
	mdSearchResultCap     = 50
	mdSearchSnippetMaxLen = 120
)

// MdSearchOptions configures NewAPIMdSearchHandler.
type MdSearchOptions struct {
	// NavRoot is the directory to walk for .md files.
	NavRoot string
}

type mdSearchHandler struct {
	navRoot string
}

// mdMatch is one matching line inside a .md file.
type mdMatch struct {
	// RelPath is relative to NavRoot, forward-slashed.
	RelPath string
	// Line is 1-based.
	Line int
	// Snippet is the trimmed excerpt shown in the dropdown.
	Snippet string
}

// search collects up to mdSearchResultCap matches; the bool reports the cap.
func (h *mdSearchHandler) search(query string) ([]mdMatch, bool) {
	lower := strings.ToLower(query)
	var matches []mdMatch
	truncated := false

	scope := loadDocumentScope(h.navRoot)
	// The walk's error return is the cap sentinel, not a failure.
	_ = scope.walk(".", func(relPath, _ string, d fs.DirEntry) error {
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") || hiddenFile(d.Name()) {
			return nil
		}

		data, readErr := scope.readFile(relPath)
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
					return fmt.Errorf("cap") // stops the walk; discarded above
				}
			}
		}
		return nil
	})

	return matches, truncated
}
