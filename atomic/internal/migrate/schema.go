package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// wikiIndexPath returns the path to docs/wiki/index.md within root.
func wikiIndexPath(root string) string {
	return filepath.Join(root, "docs", "wiki", "index.md")
}

// wikiSchemaRe matches <wiki-schema>N</wiki-schema> blocks.
var wikiSchemaRe = regexp.MustCompile(`<wiki-schema>(\d+)</wiki-schema>`)

// ReadWikiSchema reads the <wiki-schema>N</wiki-schema> block from
// <root>/docs/wiki/index.md and returns N as an int.
// Returns 0 if the file does not exist, cannot be read, or the block is absent.
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

// WriteWikiSchema sets (or replaces) the <wiki-schema>N</wiki-schema> block in
// <root>/docs/wiki/index.md. When the block already exists it is replaced
// in-place; when absent it is prepended to the file.
//
// Returns nil (not an error) when docs/wiki/index.md does not exist — the
// caller must ensure the file is created before calling this if N > 0 is
// meaningful.
func WriteWikiSchema(root string, n int) error {
	path := wikiIndexPath(root)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// No wiki index: nothing to stamp. The step's idempotency guard will
		// re-run next time; treat this as a no-op rather than an error.
		return nil
	}
	if err != nil {
		return fmt.Errorf("migrate: read wiki index: %w", err)
	}

	block := fmt.Sprintf("<wiki-schema>%d</wiki-schema>", n)
	var updated []byte
	if wikiSchemaRe.Match(data) {
		updated = wikiSchemaRe.ReplaceAll(data, []byte(block))
	} else {
		// Prepend the block before existing content.
		updated = append([]byte(block+"\n"), data...)
	}

	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("migrate: write wiki index: %w", err)
	}
	return nil
}
