package wiki

import (
	"fmt"
	"os"
	"strings"
)

// ReadWikiIndexPaths returns the index.md paths in claudeMDPath's <wikis>
// block; a missing file or block yields nothing, never an error.
func ReadWikiIndexPaths(claudeMDPath string) ([]string, error) {
	data, err := os.ReadFile(claudeMDPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("wiki registry read: %w", err)
	}

	// Whole-line tags only: the shipped <atomic> block names "<wikis>" in prose,
	// and a first-occurrence match reads the bullets after it as index paths.
	blockStart, _, blockContent := findBareLineBlock(string(data), wikisMarkerOpen, wikisMarkerClose)
	if blockStart == -1 {
		return nil, nil
	}

	var paths []string
	for _, line := range strings.Split(blockContent, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "- ") {
			continue
		}
		p := strings.TrimPrefix(line, "- ")
		p = strings.TrimSpace(p)
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}
