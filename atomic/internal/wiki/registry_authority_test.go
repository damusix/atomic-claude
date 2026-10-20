package wiki

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// samePaths compares ordered path lists.
func samePaths(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// claudeMDPathFor builds the conventional <home>/.claude/CLAUDE.md shape the
// authority projection is derived from.
func claudeMDPathFor(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "CLAUDE.md")
}

// The authority lives at ~/.atomic/wikis.md and is written before the
// projection.
func TestWikiRegistry_AuthorityBeforeProjection(t *testing.T) {
	home := t.TempDir()
	claudeMD := claudeMDPathFor(t, home)
	indexPath := filepath.Join(t.TempDir(), "realm", "wiki", "index.md")

	if err := RegisterWiki(claudeMD, indexPath); err != nil {
		t.Fatalf("RegisterWiki: %v", err)
	}

	authority := config.WikisPath(home)
	paths, err := NewWikiRegistry(home).Load()
	if err != nil {
		t.Fatalf("authority Load: %v", err)
	}
	if len(paths) != 1 || paths[0] != indexPath {
		t.Fatalf("authority entries = %v, want [%s]", paths, indexPath)
	}
	data, err := os.ReadFile(authority)
	if err != nil {
		t.Fatalf("read authority: %v", err)
	}
	if !strings.Contains(string(data), "- "+indexPath) {
		t.Errorf("authority bytes missing entry:\n%s", data)
	}

	projected, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read projection: %v", err)
	}
	if !strings.Contains(string(projected), "- "+indexPath) {
		t.Errorf("projection bytes missing entry:\n%s", projected)
	}
}

// Saving rewrites only the <wikis> block: every other authority byte survives.
func TestWikiRegistry_BytePreserving(t *testing.T) {
	home := t.TempDir()
	authority := config.WikisPath(home)
	if err := os.MkdirAll(filepath.Dir(authority), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "# my registry\n\nkeep me\n\n<wikis>\n- /old/index.md\n</wikis>\n\ntrailing prose\n"
	if err := os.WriteFile(authority, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	first := filepath.Join(t.TempDir(), "one", "index.md")
	if _, _, err := NewWikiRegistry(home).Add(first); err != nil {
		t.Fatalf("Add: %v", err)
	}

	data, err := os.ReadFile(authority)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, kept := range []string{"# my registry", "keep me", "- /old/index.md", "trailing prose"} {
		if !strings.Contains(content, kept) {
			t.Errorf("authority lost %q after Add:\n%s", kept, content)
		}
	}
	if !strings.Contains(content, "- "+first) {
		t.Errorf("authority missing new entry:\n%s", content)
	}
}

// Add is idempotent and Remove drops exactly one entry.
func TestWikiRegistry_AddRemove(t *testing.T) {
	home := t.TempDir()
	reg := NewWikiRegistry(home)
	one := filepath.Join(t.TempDir(), "a", "index.md")
	two := filepath.Join(t.TempDir(), "b", "index.md")

	if paths, changed, err := reg.Add(one); err != nil || !changed || len(paths) != 1 {
		t.Fatalf("first Add: paths=%v changed=%v err=%v", paths, changed, err)
	}
	if _, changed, err := reg.Add(one); err != nil || changed {
		t.Fatalf("repeat Add must be a no-op: changed=%v err=%v", changed, err)
	}
	if _, _, err := reg.Add(two); err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if removed, err := reg.Remove(one); err != nil || !removed {
		t.Fatalf("Remove: removed=%v err=%v", removed, err)
	}
	if removed, err := reg.Remove(one); err != nil || removed {
		t.Fatalf("repeat Remove: removed=%v err=%v", removed, err)
	}
	paths, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(paths) != 1 || paths[0] != two {
		t.Fatalf("remaining entries = %v, want [%s]", paths, two)
	}
}

// After adoption the projection is derived: a hand-edited block is a conflict
// and never overrides the authority.
func TestRegisteredIndexPaths_AuthorityWinsOverChangedProjection(t *testing.T) {
	home := t.TempDir()
	claudeMD := claudeMDPathFor(t, home)
	authoritative := filepath.Join(t.TempDir(), "realm", "wiki", "index.md")

	if err := RegisterWiki(claudeMD, authoritative); err != nil {
		t.Fatalf("RegisterWiki: %v", err)
	}

	conflict, err := ProjectionConflict(claudeMD)
	if err != nil {
		t.Fatalf("ProjectionConflict: %v", err)
	}
	if conflict {
		t.Fatal("fresh projection must not report a conflict")
	}

	rogue := filepath.Join(t.TempDir(), "rogue", "wiki", "index.md")
	edited := "<wikis>\n- " + rogue + "\n</wikis>\n"
	if err := os.WriteFile(claudeMD, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	conflict, err = ProjectionConflict(claudeMD)
	if err != nil {
		t.Fatalf("ProjectionConflict after edit: %v", err)
	}
	if !conflict {
		t.Error("changed projection must report a conflict")
	}

	paths, err := RegisteredIndexPaths(claudeMD)
	if err != nil {
		t.Fatalf("RegisteredIndexPaths: %v", err)
	}
	if len(paths) != 1 || paths[0] != authoritative {
		t.Fatalf("resolution = %v, want the authority entry [%s]", paths, authoritative)
	}
}

// Before adoption there is no authority file: the legacy <wikis> block still
// resolves.
func TestRegisteredIndexPaths_FallsBackToLegacyBlock(t *testing.T) {
	home := t.TempDir()
	claudeMD := claudeMDPathFor(t, home)
	legacy := filepath.Join(t.TempDir(), "legacy", "wiki", "index.md")
	if err := os.WriteFile(claudeMD, []byte(buildWikisBlock([]string{legacy})), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := RegisteredIndexPaths(claudeMD)
	if err != nil {
		t.Fatalf("RegisteredIndexPaths: %v", err)
	}
	if len(paths) != 1 || paths[0] != legacy {
		t.Fatalf("legacy resolution = %v, want [%s]", paths, legacy)
	}
}

// A non-conventional install root has no derivable authority, so only the
// projection is written and no ~/.atomic file is created there.
func TestRegisterWiki_NonConventionalRootWritesProjectionOnly(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	indexPath := filepath.Join(t.TempDir(), "realm", "wiki", "index.md")

	if err := RegisterWiki(claudeMD, indexPath); err != nil {
		t.Fatalf("RegisterWiki: %v", err)
	}
	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- "+indexPath) {
		t.Errorf("projection missing entry:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), ".atomic", "wikis.md")); err == nil {
		t.Error("non-conventional install root must not write a sibling authority")
	}
}

// makeIndex writes a scanned wikiDir/index.md and returns its path.
func makeIndex(t *testing.T, wikiDir string) string {
	t.Helper()
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(wikiDir, "index.md")
	content := fmt.Sprintf("<wiki-scan root=%q>\n</wiki-scan>\n", wikiDir)
	if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return indexPath
}

// The pre-authority install is the <wikis> block in the installed CLAUDE.md. The
// first authority write — `atomic wiki scan` registering one more realm — adopts
// that block first, so realms registered before the authority survive in both
// ~/.atomic/wikis.md and the derived projection, and the readers that prefer the
// authority (`where`, the realm resolver, doctor) still see all of them.
func TestWikiScan_SeedsAuthorityFromLegacyBlock(t *testing.T) {
	home := t.TempDir()
	claudeHome := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")

	legacy := []string{
		makeIndex(t, filepath.Join(t.TempDir(), "one", "wiki")),
		makeIndex(t, filepath.Join(t.TempDir(), "two", "wiki")),
	}
	if err := os.WriteFile(claudeMD, []byte(buildWikisBlock(legacy)), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "member", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := wikiAction([]string{"scan", "--root=" + root}, claudeHome, root, &buf); code != 0 {
		t.Fatalf("wiki scan exit = %d:\n%s", code, buf.String())
	}
	scanned := makeIndex(t, filepath.Join(root, "wiki"))
	want := append(append([]string{}, legacy...), scanned)

	authority, err := NewWikiRegistry(home).Load()
	if err != nil {
		t.Fatalf("authority Load: %v", err)
	}
	if !samePaths(authority, want) {
		t.Fatalf("authority entries = %v, want %v", authority, want)
	}

	projected, err := ReadWikiIndexPaths(claudeMD)
	if err != nil {
		t.Fatalf("ReadWikiIndexPaths: %v", err)
	}
	if !samePaths(projected, want) {
		t.Fatalf("projected entries = %v, want %v", projected, want)
	}

	conflict, err := ProjectionConflict(claudeMD)
	if err != nil {
		t.Fatalf("ProjectionConflict: %v", err)
	}
	if conflict {
		t.Error("projection must match the authority after seeding")
	}

	resolved, err := RegisteredIndexPaths(claudeMD)
	if err != nil {
		t.Fatalf("RegisteredIndexPaths: %v", err)
	}
	if !samePaths(resolved, want) {
		t.Fatalf("registry resolution = %v, want %v", resolved, want)
	}
}
