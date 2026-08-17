package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

func wikiIndexPath(root string) string {
	return filepath.Join(root, "docs", "wiki", "index.md")
}

var wikiSchemaRe = regexp.MustCompile(`<wiki-schema>(\d+)</wiki-schema>`)

// ReadWikiSchema returns N from the <wiki-schema> block, or 0 when the index
// is missing, unreadable, or carries no block.
func ReadWikiSchema(root string) int {
	data, err := os.ReadFile(wikiIndexPath(root))
	if err != nil {
		return 0
	}
	m := wikiSchemaRe.FindSubmatch(data)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return 0
	}
	return n
}

// WriteWikiSchema replaces the <wiki-schema> block in place, or prepends it.
// An absent index is a no-op, not an error, so callers wanting N > 0 stamped
// must create the file first.
func WriteWikiSchema(root string, n int) error {
	path := wikiIndexPath(root)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // the step's idempotency guard re-runs this next time
	}
	if err != nil {
		return fmt.Errorf("migrate: read wiki index: %w", err)
	}

	block := fmt.Sprintf("<wiki-schema>%d</wiki-schema>", n)
	var updated []byte
	if wikiSchemaRe.Match(data) {
		updated = wikiSchemaRe.ReplaceAll(data, []byte(block))
	} else {
		updated = append([]byte(block+"\n"), data...)
	}

	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("migrate: write wiki index: %w", err)
	}
	return nil
}
