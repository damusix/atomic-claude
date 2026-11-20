package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func TestRealmCLIShareProjectsGeneratedWikiReadmeRoot(t *testing.T) {
	for _, tc := range []struct {
		name, lineEnding string
		newlineRoot      bool
	}{
		{name: "LF", lineEnding: "\n"},
		{name: "CRLF", lineEnding: "\r\n"},
		{name: "newline-in-root", lineEnding: "\n", newlineRoot: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.newlineRoot && runtime.GOOS == "windows" {
				t.Skip("Windows does not allow newlines in directory names")
			}
			testRealmCLIShareProjectsGeneratedWikiReadmeRoot(t, tc.lineEnding, tc.newlineRoot)
		})
	}
}

func testRealmCLIShareProjectsGeneratedWikiReadmeRoot(t *testing.T, lineEnding string, newlineRoot bool) {
	t.Helper()
	root, home, remote := newRealmShareCLIFixture(t)
	if newlineRoot {
		root = filepath.Join(root, "realm\nparent", "publisher")
		writeRealmShareCLIFiles(t, root, shareContents)
		t.Chdir(root)
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	out, diag, code := runAtomicCLIFull(t, home, []string{"PATH=" + os.Getenv("PATH")}, "root", "wiki", "scan", "--root", root)
	if code != 0 {
		t.Fatalf("wiki scan: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}

	wiki := filepath.Join(root, "wiki")
	readme := filepath.Join(wiki, "README.md")
	generated := readSteeringTestFile(t, readme)
	rootField := "**Realm root:** `" + root + "`\n\n"
	if !strings.Contains(generated, rootField) {
		t.Fatalf("wiki scan did not generate a local root field:\n%s", generated)
	}
	runGit(t, wiki, "add", "README.md")
	runGit(t, wiki, "commit", "--quiet", "-m", "Original wiki scaffold")
	originalHead := runGit(t, wiki, "rev-parse", "HEAD")
	authored := "\n## Team notes\n\nExample of the original local scaffold field:\n\n**Realm root:** `/example/realm`\n\n"
	scaffold := strings.ReplaceAll(generated, "\n", lineEnding)
	original := scaffold + authored
	writeSteeringTestFile(t, readme, original)

	args := []string{"wiki/", "--remote", remote, "--branch", shareBranch}
	preview := mustRunRealmCLI(t, home, "share", append(args, "--dry-run")...)
	if !strings.Contains(preview, `Exclude "wiki/README.md **Realm root:** (local scaffold)"`) {
		t.Errorf("share preview did not report the generated README field exclusion:\n%s", preview)
	}
	mustRunRealmCLI(t, home, "share", append(args, "--yes", "--review", realmReviewDigest(t, preview))...)

	want := strings.Replace(scaffold, strings.ReplaceAll(rootField, "\n", lineEnding), "", 1) + authored
	got := runGit(t, remote, "show", "refs/heads/"+shareBranch+":wiki/README.md")
	if strings.Contains(got, root) {
		t.Errorf("published README contains the local realm root %q", root)
	}
	if got != want {
		t.Errorf("published README differs from the portable projection:\ngot:  %q\nwant: %q", got, want)
	}
	backups := filepath.Join(filepath.Dir(config.RepoConfigPath(root)), "realm-share-backups")
	entries, err := os.ReadDir(backups)
	if err != nil {
		t.Fatal(err)
	}
	dirs := slices.DeleteFunc(entries, func(entry os.DirEntry) bool { return !entry.IsDir() })
	if len(dirs) != 1 {
		t.Fatalf("expected exactly one retained wiki directory, got %v", dirs)
	}
	backup := filepath.Join(backups, dirs[0].Name())
	assertRealmShareCLIFile(t, backup, "README.md", original, false)
	if got := runGit(t, backup, "rev-parse", "HEAD"); got != originalHead {
		t.Errorf("retained wiki HEAD = %q, want %q", got, originalHead)
	}
	if got := runGit(t, backup, "show", "HEAD:README.md"); got != generated {
		t.Errorf("original history README differs from the generated scaffold:\ngot:  %q\nwant: %q", got, generated)
	}
}

func TestRealmCLIShareRejectsMalformedGeneratedWikiReadmeRoot(t *testing.T) {
	root, home, remote := newRealmShareCLIFixture(t)
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	out, diag, code := runAtomicCLIFull(t, home, []string{"PATH=" + os.Getenv("PATH")}, "root", "wiki", "scan", "--root", root)
	if code != 0 {
		t.Fatalf("wiki scan: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	readme := filepath.Join(root, "wiki", "README.md")
	generated := readSteeringTestFile(t, readme)
	field := "**Realm root:** `" + root + "`\n\n"
	if !strings.Contains(generated, field) {
		t.Fatalf("wiki scan did not generate a local root field:\n%s", generated)
	}
	for _, tc := range []struct{ name, field string }{
		{"missing-opening-backtick", "**Realm root:** " + root + "`\n\n"},
		{"missing-closing-backtick", "**Realm root:** `" + root + "\n\nSee the example `overview.md`\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := strings.Replace(generated, field, tc.field, 1)
			writeSteeringTestFile(t, readme, original)
			out, diag, code := runRealmShareCLI(t, home, "wiki/", "--remote", remote, "--branch", shareBranch, "--dry-run")
			const want = "cannot identify the generated realm root field in wiki/README.md; remove the field before sharing"
			if code == 0 || !strings.Contains(diag, want) {
				t.Errorf("share preview did not refuse the malformed root field: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
			}
			assertRealmShareCLIFile(t, root, "wiki/README.md", original, false)
		})
	}
}
