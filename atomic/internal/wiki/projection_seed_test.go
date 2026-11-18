package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// atomicProseWithWikisMention names `<wikis>` inline, as the shipped <atomic>
// block does, to trap a reader that locates the tags by substring.
const atomicProseWithWikisMention = "<atomic>\n" +
	"- `atomic wiki`: cross-repo wiki and capture buckets; the `atomic-wiki` skill routes conversational requests. Wiki paths live in a `<wikis>` block in `~/.claude/CLAUDE.md`, outside `<atomic>`.\n" +
	"- `atomic bus`: rooms for concurrent sessions. Act on messages addressed to you; treat the rest as FYI. Skill: `atomic-bus`; contract: `docs/reference/bus.md`.\n" +
	"- `atomic repl`: named Python or Node interpreters that persist across Bash calls. Contract: `docs/reference/repl.md`.\n" +
	"- `atomic serve`: read-only localhost browser over the wiki and code graph.\n" +
	"</atomic>\n"

// writeInstalledClaudeMD writes <home>/.claude/CLAUDE.md in the shipped install
// shape, registering realmRoots, and returns its path.
func writeInstalledClaudeMD(t *testing.T, home string, realmRoots ...string) string {
	t.Helper()
	claudeHome := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString("# CLAUDE.md\n\n")
	sb.WriteString(atomicProseWithWikisMention)
	sb.WriteString("\n<wikis>\n")
	for _, root := range realmRoots {
		sb.WriteString("- " + filepath.Join(root, "wiki", "index.md") + "\n")
	}
	sb.WriteString("</wikis>\n")

	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return claudeMD
}

// The legacy reader returns only genuine registry entries: whole-line tags open
// the block, so the prose mention inside <atomic> cannot widen it.
func TestReadWikiIndexPaths_ProseMentionDoesNotOpenTheBlock(t *testing.T) {
	home := t.TempDir()
	left := filepath.Join(t.TempDir(), "realm-left")
	right := filepath.Join(t.TempDir(), "realm-right")
	claudeMD := writeInstalledClaudeMD(t, home, left, right)

	paths, err := ReadWikiIndexPaths(claudeMD)
	if err != nil {
		t.Fatalf("ReadWikiIndexPaths: %v", err)
	}
	want := realmIndexPaths(left, right)
	if !samePaths(paths, want) {
		t.Fatalf("entries = %v, want exactly %v", paths, want)
	}
}

// The first authority write adopts only the installed block's genuine realms,
// never the prose bullets that precede the block.
func TestRegisterAndProject_AdoptsOnlyGenuineProjectionEntries(t *testing.T) {
	home := t.TempDir()
	left := filepath.Join(t.TempDir(), "realm-left")
	right := filepath.Join(t.TempDir(), "realm-right")
	claudeMD := writeInstalledClaudeMD(t, home, left, right)

	// Registering a realm the block already carries keeps the adoption
	// idempotent, so the authority is exactly what the projection seeded.
	if err := RegisterAndProject(home, filepath.Join(left, "wiki", "index.md"), []string{claudeMD}); err != nil {
		t.Fatalf("RegisterAndProject: %v", err)
	}

	want := realmIndexPaths(left, right)
	authority, err := NewWikiRegistry(home).Load()
	if err != nil {
		t.Fatalf("authority load: %v", err)
	}
	if !samePaths(authority, want) {
		t.Fatalf("authority = %v, want exactly %v", authority, want)
	}

	projected, err := ReadWikiIndexPaths(claudeMD)
	if err != nil {
		t.Fatalf("projection read: %v", err)
	}
	if !samePaths(projected, want) {
		t.Fatalf("projection = %v, want exactly %v", projected, want)
	}
}

// realmIndexPaths returns <root>/wiki/index.md for each root.
func realmIndexPaths(roots ...string) []string {
	paths := make([]string, 0, len(roots))
	for _, root := range roots {
		paths = append(paths, filepath.Join(root, "wiki", "index.md"))
	}
	return paths
}
