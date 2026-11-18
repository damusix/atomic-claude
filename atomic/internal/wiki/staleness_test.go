package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadWikiIndexPaths_ReadsAllEntries(t *testing.T) {
	tmp := t.TempDir()
	claudeMD := filepath.Join(tmp, "CLAUDE.md")
	p1 := "/home/user/.claude/wiki1/index.md"
	p2 := "/home/user/.claude/wiki2/index.md"
	content := fmt.Sprintf("<wikis>\n- %s\n- %s\n</wikis>\n", p1, p2)
	if err := os.WriteFile(claudeMD, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := ReadWikiIndexPaths(claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d: %v", len(paths), paths)
	}
	if !containsAll(paths, p1, p2) {
		t.Fatalf("paths missing expected entries: %v", paths)
	}
}

// installedCLAUDEMD mirrors the shipped ~/.claude/CLAUDE.md: the <atomic> block
// names the <wikis> block in prose before the real block, and the bullets
// between them are not registry entries.
func installedCLAUDEMD(indexPath string) string {
	return "<atomic>\n" +
		"- `atomic wiki`: Wiki paths live in a `<wikis>` block in `~/.claude/CLAUDE.md`, outside `<atomic>`.\n" +
		"- `atomic serve`: read-only localhost browser over the wiki and code graph.\n" +
		"</atomic>\n\n" +
		"<wikis>\n- " + indexPath + "\n</wikis>\n"
}

func TestReadWikiIndexPaths_IgnoresInlineMention(t *testing.T) {
	tmp := t.TempDir()
	claudeMD := filepath.Join(tmp, "CLAUDE.md")
	indexPath := "/home/user/realm/wiki/index.md"
	if err := os.WriteFile(claudeMD, []byte(installedCLAUDEMD(indexPath)), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := ReadWikiIndexPaths(claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 1 || paths[0] != indexPath {
		t.Fatalf("want only the registered entry %q, got %v", indexPath, paths)
	}
}

// TestReadWikiIndexPaths_EmptyWhenNoBlock returns empty slice (no error) when
// block is absent.
func TestReadWikiIndexPaths_EmptyWhenNoBlock(t *testing.T) {
	tmp := t.TempDir()
	claudeMD := filepath.Join(tmp, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte("# no wikis\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := ReadWikiIndexPaths(claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected empty slice, got %v", paths)
	}
}

// TestReadWikiIndexPaths_FileAbsent returns empty slice (no error) when CLAUDE.md absent.
func TestReadWikiIndexPaths_FileAbsent(t *testing.T) {
	tmp := t.TempDir()
	paths, err := ReadWikiIndexPaths(filepath.Join(tmp, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected empty slice when file absent, got %v", paths)
	}
}

// containsAll returns true if all want values appear in got.
func containsAll(got []string, want ...string) bool {
	set := make(map[string]bool, len(got))
	for _, g := range got {
		set[strings.TrimSpace(g)] = true
	}
	for _, w := range want {
		if !set[strings.TrimSpace(w)] {
			return false
		}
	}
	return true
}
