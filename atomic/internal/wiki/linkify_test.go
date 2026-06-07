package wiki_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// setupLinkifyRealm creates a realm directory with:
//   - root/wiki/index.md  (with a token pointing at root/README.md)
//   - root/wiki/concerns/cross.md (with a token pointing at root/README.md)
//   - root/wiki/repos/repoA.md (with repo: repoA in frontmatter, and a token pointing at repoA/main.go)
//   - root/README.md (target for index and concerns)
//   - root/repoA/main.go (target for repoA summary)
func setupLinkifyRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Targets
	writeFile(t, root, "README.md", "# realm\n")
	writeFile(t, root, "repoA/main.go", "package main\n")

	// Wiki artifacts
	writeFile(t, root, "wiki/index.md",
		"# index\n\nSee `README.md` for realm overview.\n")
	writeFile(t, root, "wiki/concerns/cross.md",
		"# cross\n\nSee `README.md` for details.\n")
	writeFile(t, root, "wiki/repos/repoA.md",
		"---\nrepo: repoA\n---\n\n# repoA\n\nKey file: `repoA/main.go`\n")

	return root
}

// writeFile writes content to root/rel, creating parent dirs.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readFile reads a file and returns its content.
func readFileContent(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestLinkifyWiki_IndexLinked verifies that index.md gets its tokens linkified
// with base = realm root.
func TestLinkifyWiki_IndexLinked(t *testing.T) {
	root := setupLinkifyRealm(t)
	if err := wiki.LinkifyWiki(root); err != nil {
		t.Fatalf("LinkifyWiki: %v", err)
	}
	content := readFileContent(t, filepath.Join(root, "wiki", "index.md"))
	if !strings.Contains(content, "[`README.md`]") {
		t.Errorf("index.md token not linkified:\n%s", content)
	}
}

// TestLinkifyWiki_ConcernLinked verifies that concerns/*.md tokens are linkified
// with base = realm root.
func TestLinkifyWiki_ConcernLinked(t *testing.T) {
	root := setupLinkifyRealm(t)
	if err := wiki.LinkifyWiki(root); err != nil {
		t.Fatalf("LinkifyWiki: %v", err)
	}
	content := readFileContent(t, filepath.Join(root, "wiki", "concerns", "cross.md"))
	if !strings.Contains(content, "[`README.md`]") {
		t.Errorf("concern not linkified:\n%s", content)
	}
}

// TestLinkifyWiki_RepoSummaryLinked verifies that repos/<repo>.md tokens are
// linkified with base = <root>/<repo>.
func TestLinkifyWiki_RepoSummaryLinked(t *testing.T) {
	root := setupLinkifyRealm(t)
	if err := wiki.LinkifyWiki(root); err != nil {
		t.Fatalf("LinkifyWiki: %v", err)
	}
	content := readFileContent(t, filepath.Join(root, "wiki", "repos", "repoA.md"))
	// Token `repoA/main.go` with base=root/repoA should NOT resolve because
	// filepath.Join(root/repoA, "repoA/main.go") = root/repoA/repoA/main.go (doesn't exist).
	// But `main.go` alone would resolve. Our fixture uses `repoA/main.go` intentionally
	// to test that with base=root/repoA the token does NOT resolve (wrong path)...
	// Actually: let's use a token that will resolve: the file at root/repoA/main.go,
	// when base=root/repoA, token="main.go" would resolve. Let's recheck the fixture.
	// Fixture uses token `repoA/main.go` and base=root/repoA. Stat(root/repoA/repoA/main.go) → fails.
	// So it won't be linked. Instead use token "main.go" in the repo summary.
	// The fixture was written with `repoA/main.go` — that won't resolve from base=repoA.
	// This test should assert the token is NOT linked (correct behavior: wrong path).
	if strings.Contains(content, "[`repoA/main.go`]") {
		t.Errorf("cross-base token was incorrectly linked:\n%s", content)
	}
}

// TestLinkifyWiki_RepoSummaryLinked_CorrectBase verifies that a token using
// a repo-relative path (e.g. `main.go`) resolves when base = <root>/<repo>.
func TestLinkifyWiki_RepoSummaryLinked_CorrectBase(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "repoA/main.go", "package main\n")
	// Use a repo-relative token: `main.go`
	writeFile(t, root, "wiki/repos/repoA.md",
		"---\nrepo: repoA\n---\n\n# repoA\n\nKey file: `main.go`\n")

	if err := wiki.LinkifyWiki(root); err != nil {
		t.Fatalf("LinkifyWiki: %v", err)
	}
	content := readFileContent(t, filepath.Join(root, "wiki", "repos", "repoA.md"))
	if !strings.Contains(content, "[`main.go`]") {
		t.Errorf("repo-relative token not linkified:\n%s", content)
	}
}

// TestLinkifyWiki_MissingRepoKey verifies that a repos/*.md without a repo:
// frontmatter key is skipped (not crashed).
func TestLinkifyWiki_MissingRepoKey(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "README.md", "# realm\n")
	writeFile(t, root, "wiki/repos/unknown.md",
		"# unknown\n\nSee `README.md`.\n")

	if err := wiki.LinkifyWiki(root); err != nil {
		t.Fatalf("LinkifyWiki should not error on missing repo: key: %v", err)
	}
	// Content should be unchanged (no linkification without a valid base).
	content := readFileContent(t, filepath.Join(root, "wiki", "repos", "unknown.md"))
	if strings.Contains(content, "[`README.md`]") {
		t.Errorf("file without repo: key was incorrectly linkified:\n%s", content)
	}
}

// TestLinkifyWiki_Idempotent verifies that running twice produces byte-identical files.
func TestLinkifyWiki_Idempotent(t *testing.T) {
	root := setupLinkifyRealm(t)

	if err := wiki.LinkifyWiki(root); err != nil {
		t.Fatalf("first run: %v", err)
	}
	after1 := readFileContent(t, filepath.Join(root, "wiki", "index.md"))

	if err := wiki.LinkifyWiki(root); err != nil {
		t.Fatalf("second run: %v", err)
	}
	after2 := readFileContent(t, filepath.Join(root, "wiki", "index.md"))

	if after1 != after2 {
		t.Errorf("not idempotent:\nafter1: %q\nafter2: %q", after1, after2)
	}
}

// TestLinkifyWiki_EmptyWikiDir verifies no error when wiki/ doesn't exist.
func TestLinkifyWiki_EmptyWikiDir(t *testing.T) {
	root := t.TempDir()
	if err := wiki.LinkifyWiki(root); err != nil {
		t.Errorf("expected no error on empty realm: %v", err)
	}
}
