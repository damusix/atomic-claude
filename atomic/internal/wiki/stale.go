package wiki

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// StaleResult codes mirror the signals stale convention.
const (
	StaleCodeFresh = 0
	StaleCodeStale = 1
	StaleCodeError = 2
)

// Stale writes one DRIFT line per member added, removed, or reclassified since
// the <wiki-scan> block, returning 0 / 1 drift / 2 error. Read-only.
func Stale(root string, out io.Writer) (int, error) {
	wikiDir := filepath.Join(root, "wiki")
	indexPath := filepath.Join(wikiDir, "index.md")

	if _, err := os.Lstat(wikiDir); err != nil {
		return StaleCodeError, fmt.Errorf("wiki/ not found at %s", wikiDir)
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return StaleCodeError, fmt.Errorf("index.md not found at %s", indexPath)
		}
		return StaleCodeError, fmt.Errorf("read index.md: %w", err)
	}

	block := extractBlockContent(string(data))
	if block == "" {
		return StaleCodeError, fmt.Errorf("no <wiki-scan> block in %s", indexPath)
	}

	recorded := parseBlockMembers(block)

	current, warns, err := discoverMembers(root, wikiDir)
	for _, w := range warns {
		fmt.Fprintf(os.Stderr, "atomic wiki stale: warning: %s\n", w)
	}
	if err != nil {
		return StaleCodeError, fmt.Errorf("discover members: %w", err)
	}

	// Nil prior map: we want live classification, not preserved summarized
	// state — the block already records what was claimed.
	classified := classifyMembers(root, wikiDir, current, nil)

	currentByPath := map[string]Member{}
	for _, m := range classified {
		currentByPath[m.Path] = m
	}

	var lines []string
	for path := range recorded {
		if _, ok := currentByPath[path]; !ok {
			lines = append(lines, fmt.Sprintf("DRIFT removed %s", path))
		}
	}

	for _, m := range classified {
		rec, inBlock := recorded[m.Path]
		if !inBlock {
			lines = append(lines, fmt.Sprintf("DRIFT added %s", m.Path))
			continue
		}
		if rec.status != m.Status {
			lines = append(lines, fmt.Sprintf("DRIFT status %s %s→%s", m.Path, rec.status, m.Status))
		}
	}

	sort.Strings(lines)
	for _, l := range lines {
		fmt.Fprintln(out, l)
	}

	if len(lines) > 0 {
		return StaleCodeStale, nil
	}
	return StaleCodeFresh, nil
}
