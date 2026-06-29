package migrate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/migrate"
)

// setupWikiIndex creates docs/wiki/index.md in root with the given content.
func setupWikiIndex(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, "docs", "wiki")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write index.md: %v", err)
	}
}

// TestReadWikiSchemaAbsent returns 0 when docs/wiki/index.md does not exist.
func TestReadWikiSchemaAbsent(t *testing.T) {
	root := t.TempDir()
	if got := migrate.ReadWikiSchema(root); got != 0 {
		t.Errorf("absent file: got %d, want 0", got)
	}
}

// TestReadWikiSchemaNoBlock returns 0 when the file exists but has no block.
func TestReadWikiSchemaNoBlock(t *testing.T) {
	root := t.TempDir()
	setupWikiIndex(t, root, "# project signals\n\nsome content\n")
	if got := migrate.ReadWikiSchema(root); got != 0 {
		t.Errorf("no block: got %d, want 0", got)
	}
}

// TestReadWikiSchemaPresent parses the N from the block.
func TestReadWikiSchemaPresent(t *testing.T) {
	root := t.TempDir()
	setupWikiIndex(t, root, "---\ntype: Index\n---\n\n<wiki-schema>3</wiki-schema>\n")
	if got := migrate.ReadWikiSchema(root); got != 3 {
		t.Errorf("block present: got %d, want 3", got)
	}
}

// TestWriteWikiSchemaRoundTrip: write then read returns the same value.
func TestWriteWikiSchemaRoundTrip(t *testing.T) {
	root := t.TempDir()
	setupWikiIndex(t, root, "# index\n")

	if err := migrate.WriteWikiSchema(root, 2); err != nil {
		t.Fatalf("WriteWikiSchema: %v", err)
	}
	if got := migrate.ReadWikiSchema(root); got != 2 {
		t.Errorf("round-trip: got %d, want 2", got)
	}
}

// TestWriteWikiSchemaReplacesExisting: updating an existing block in-place.
func TestWriteWikiSchemaReplacesExisting(t *testing.T) {
	root := t.TempDir()
	setupWikiIndex(t, root, "<wiki-schema>1</wiki-schema>\n# rest of file\n")

	if err := migrate.WriteWikiSchema(root, 5); err != nil {
		t.Fatalf("WriteWikiSchema: %v", err)
	}
	if got := migrate.ReadWikiSchema(root); got != 5 {
		t.Errorf("replace: got %d, want 5", got)
	}
	// Ensure other content survived.
	data, _ := os.ReadFile(filepath.Join(root, "docs", "wiki", "index.md"))
	if string(data) != "<wiki-schema>5</wiki-schema>\n# rest of file\n" {
		t.Errorf("unexpected file content after replace:\n%s", data)
	}
}

// TestWriteWikiSchemaMissingFileNoError: no file → no error (graceful no-op).
func TestWriteWikiSchemaMissingFileNoError(t *testing.T) {
	root := t.TempDir()
	if err := migrate.WriteWikiSchema(root, 1); err != nil {
		t.Errorf("missing file: expected no error, got %v", err)
	}
}
