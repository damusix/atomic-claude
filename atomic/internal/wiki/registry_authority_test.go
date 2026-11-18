package wiki

import (
	"bytes"
	"fmt"
	"io"
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

// steering is a wikiAction steering resolver over fixed paths.
func steering(paths ...string) func() ([]string, error) {
	return func() ([]string, error) { return paths, nil }
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

	if err := RegisterAndProject(home, indexPath, []string{claudeMD}); err != nil {
		t.Fatalf("RegisterAndProject: %v", err)
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

	if err := RegisterAndProject(home, authoritative, []string{claudeMD}); err != nil {
		t.Fatalf("RegisterAndProject: %v", err)
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

// Realms registered before ~/.atomic/wikis.md existed must survive the first
// authority write, or every reader that prefers the authority loses them.
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
	if code := wikiAction([]string{"scan", "--root=" + root}, home, steering(claudeMD), root, &buf); code != 0 {
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

// scanCapturingStderr runs `wiki scan` over a fresh realm root and returns the
// registered index path and everything written to stderr.
func scanCapturingStderr(t *testing.T, home, claudeMD string) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "member", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	var buf bytes.Buffer
	code := wikiAction([]string{"scan", "--root=" + root}, home, steering(claudeMD), root, &buf)
	w.Close()
	os.Stderr = oldStderr
	stderr, _ := io.ReadAll(r)
	if code != 0 {
		t.Fatalf("wiki scan exit = %d:\n%s", code, stderr)
	}
	return filepath.Join(root, "wiki", "index.md"), string(stderr)
}

// A line the user added to the installed block after the authority existed is
// a divergence: the scan keeps it in place, reports it, and never imports it.
func TestWikiScan_HandAddedProjectionLineSurvivesAsReportedConflict(t *testing.T) {
	home := t.TempDir()
	claudeMD := claudeMDPathFor(t, home)
	registered := filepath.Join(t.TempDir(), "registered", "wiki", "index.md")
	if _, _, err := NewWikiRegistry(home).Add(registered); err != nil {
		t.Fatal(err)
	}
	handAdded := filepath.Join(t.TempDir(), "hand", "wiki", "index.md")
	if err := os.WriteFile(claudeMD, []byte("<wikis>\n- "+registered+"\n- "+handAdded+"\n</wikis>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr := scanCapturingStderr(t, home, claudeMD)

	projected, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(projected), "- "+handAdded+"\n") {
		t.Errorf("scan dropped the hand-added line:\n%s", projected)
	}
	if !strings.Contains(stderr, handAdded) || !strings.Contains(stderr, config.WikisPath(home)) {
		t.Errorf("stderr must name the hand-added entry and the authority:\n%s", stderr)
	}
	authority, err := NewWikiRegistry(home).Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range authority {
		if p == handAdded {
			t.Errorf("hand-added entry imported into the authority: %v", authority)
		}
	}
}

// Registration inserts exactly one line into an existing block: comments, blank
// lines, a trailing slash, and a relative entry keep their bytes.
func TestWikiScan_RegistrationInsertsOneLineAndKeepsEveryOtherByte(t *testing.T) {
	home := t.TempDir()
	claudeMD := claudeMDPathFor(t, home)
	abs := filepath.Join(t.TempDir(), "abs", "wiki", "index.md")
	head := "# mine\n\n<wikis>\n<!-- my realms -->\n- " + abs + "/\n\n- rel/wiki/index.md\n"
	tail := "</wikis>\n\nafter\n"
	if err := os.WriteFile(claudeMD, []byte(head+tail), 0o644); err != nil {
		t.Fatal(err)
	}

	scanned, stderr := scanCapturingStderr(t, home, claudeMD)

	got, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	if want := head + "- " + scanned + "\n" + tail; string(got) != want {
		t.Errorf("projection bytes:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(stderr, abs) {
		t.Errorf("the seeded absolute entry must not report a conflict:\n%s", stderr)
	}
}

// A relative entry is never seeded or matched, so scans from two directories
// neither duplicate it nor disagree about the conflict.
func TestWikiScan_RelativeEntryIsNeverResolvedAgainstCwd(t *testing.T) {
	home := t.TempDir()
	claudeMD := claudeMDPathFor(t, home)
	const rel = "rel/wiki/index.md"
	if err := os.WriteFile(claudeMD, []byte("<wikis>\n- "+rel+"\n</wikis>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())
	scanCapturingStderr(t, home, claudeMD)
	conflictA, err := ProjectionConflict(claudeMD)
	if err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())
	_, stderr := scanCapturingStderr(t, home, claudeMD)
	conflictB, err := ProjectionConflict(claudeMD)
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(got), rel); n != 1 {
		t.Errorf("relative entry appears %d times, want 1:\n%s", n, got)
	}
	if conflictA != conflictB {
		t.Errorf("ProjectionConflict depends on cwd: %v then %v", conflictA, conflictB)
	}
	if !strings.Contains(stderr, "- "+rel) && !strings.Contains(stderr, ": "+rel+" ") {
		t.Errorf("relative entry not reported as a divergence:\n%s", stderr)
	}
	authority, err := NewWikiRegistry(home).Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range authority {
		if strings.HasSuffix(p, rel) {
			t.Errorf("relative entry seeded into the authority as %s", p)
		}
	}
}

// A CRLF block keeps its line terminator on inserted entries.
func TestProjectWikis_InsertsWithTheBlockLineTerminator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	if err := os.WriteFile(path, []byte("<wikis>\r\n- /a/index.md\r\n</wikis>\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ProjectWikis(path, []string{"/a/index.md", "/b/index.md"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "<wikis>\r\n- /a/index.md\r\n- /b/index.md\r\n</wikis>\r\n"; string(got) != want {
		t.Errorf("projection = %q, want %q", got, want)
	}
}
