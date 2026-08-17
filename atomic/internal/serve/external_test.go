package serve_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// buildExternalRealm cites one URL from two pages (dedup), one from a single
// page, and buries a third inside a fenced code block (must stay out).
func buildExternalRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "pageA.md"),
		"# A\n\nSee [example](https://example.com/x) and [foo](http://foo.test).\n")
	writeFile(t, filepath.Join(root, "pageB.md"),
		"# B\n\nAlso see [example](https://example.com/x).\n")
	writeFile(t, filepath.Join(root, "pageC.md"),
		"# C\n\nInternal [link](pageA.md) and wikilink [[pageA]].\n\n```\nhttps://inside-code.test/should-be-excluded\n```\n")

	return root
}

func fixedDateFn(d time.Time) serve.FileDateFn {
	return func(_ string) time.Time {
		return d
	}
}

func perFileDateFn(m map[string]time.Time) serve.FileDateFn {
	return func(p string) time.Time {
		if t, ok := m[p]; ok {
			return t
		}
		return time.Time{}
	}
}

func TestExternalRegistry_UniqueURLs(t *testing.T) {
	root := buildExternalRealm(t)
	reg := serve.BuildExternalRegistry(root, fixedDateFn(time.Now()))

	if len(reg) != 2 {
		t.Errorf("expected 2 unique external URLs, got %d: %v", len(reg), reg)
	}

	found := map[string]bool{}
	for _, e := range reg {
		found[e.URL] = true
	}
	if !found["https://example.com/x"] {
		t.Error("expected https://example.com/x in registry")
	}
	if !found["http://foo.test"] {
		t.Error("expected http://foo.test in registry")
	}
}

func TestExternalRegistry_SourcePages(t *testing.T) {
	root := buildExternalRealm(t)
	reg := serve.BuildExternalRegistry(root, fixedDateFn(time.Now()))

	var entry *serve.ExternalEntry
	for i := range reg {
		if reg[i].URL == "https://example.com/x" {
			entry = &reg[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("https://example.com/x not found in registry")
	}

	sources := map[string]bool{}
	for _, s := range entry.Sources {
		sources[s] = true
	}
	if !sources["pageA.md"] {
		t.Errorf("expected pageA.md in sources for https://example.com/x, got %v", entry.Sources)
	}
	if !sources["pageB.md"] {
		t.Errorf("expected pageB.md in sources for https://example.com/x, got %v", entry.Sources)
	}
}

func TestExternalRegistry_InternalLinksExcluded(t *testing.T) {
	root := buildExternalRealm(t)
	reg := serve.BuildExternalRegistry(root, fixedDateFn(time.Now()))

	for _, e := range reg {
		if !strings.HasPrefix(e.URL, "http://") && !strings.HasPrefix(e.URL, "https://") {
			t.Errorf("non-http URL in registry: %q", e.URL)
		}
		if strings.Contains(e.URL, "pageA") {
			t.Errorf("internal link leaked into registry: %q", e.URL)
		}
	}
}

func TestExternalRegistry_FencedCodeExcluded(t *testing.T) {
	root := buildExternalRealm(t)
	reg := serve.BuildExternalRegistry(root, fixedDateFn(time.Now()))

	for _, e := range reg {
		if strings.Contains(e.URL, "inside-code.test") {
			t.Errorf("URL inside fenced code block leaked into registry: %q", e.URL)
		}
	}
}

// First-seen must come from the injected FileDateFn, never the real filesystem.
func TestExternalRegistry_FirstSeenUsesDateSeam(t *testing.T) {
	root := buildExternalRealm(t)

	dateA := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	dateB := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)

	// Both pages cite the same URL, so the earlier of the two dates must win.
	pathA := filepath.Join(root, "pageA.md")
	pathB := filepath.Join(root, "pageB.md")
	pathC := filepath.Join(root, "pageC.md")

	dateFn := perFileDateFn(map[string]time.Time{
		pathA: dateA,
		pathB: dateB,
		pathC: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
	})

	reg := serve.BuildExternalRegistry(root, dateFn)

	var entry *serve.ExternalEntry
	for i := range reg {
		if reg[i].URL == "https://example.com/x" {
			entry = &reg[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("https://example.com/x not found in registry")
	}

	if !entry.FirstSeen.Equal(dateA) {
		t.Errorf("expected first-seen %v (earliest), got %v", dateA, entry.FirstSeen)
	}
}

// Identity is set with --local so the user's global git config is untouched.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "--local", "user.email", "test@example.com")
	run("config", "--local", "user.name", "Test")
}

// The add-date is pinned through GIT_AUTHOR_DATE / GIT_COMMITTER_DATE so the
// assertion does not depend on when the test ran.
func TestGitOrMtimeDateFn_CommittedFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	dir := t.TempDir()
	initGitRepo(t, dir)

	filePath := filepath.Join(dir, "committed.md")
	writeFile(t, filePath, "# Committed\n")

	pinDate := "2023-07-04T12:00:00+00:00" // git echoes RFC3339 back verbatim
	cmd := exec.Command("git", "add", "committed.md")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "add committed.md")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+pinDate,
		"GIT_COMMITTER_DATE="+pinDate,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	want, _ := time.Parse(time.RFC3339, pinDate)
	got := serve.GitOrMtimeDateFn(filePath)

	if !got.Equal(want) {
		t.Errorf("GitOrMtimeDateFn: want %v (git add-date), got %v", want, got)
	}
}

func TestGitOrMtimeDateFn_NonGitDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	dir := t.TempDir()
	// Deliberately no git init.
	filePath := filepath.Join(dir, "plain.md")
	writeFile(t, filePath, "# Plain\n")

	before := time.Now().Add(-time.Second)
	got := serve.GitOrMtimeDateFn(filePath)
	after := time.Now().Add(time.Second)

	if got.Before(before) || got.After(after) {
		t.Errorf("GitOrMtimeDateFn in non-git dir: got %v, expected mtime in [%v, %v]", got, before, after)
	}
}

// git log --diff-filter=A prints nothing for an untracked file, so the mtime
// fallback is the only thing left.
func TestGitOrMtimeDateFn_UntrackedFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	dir := t.TempDir()
	initGitRepo(t, dir)

	// Written but never added, so it stays untracked.
	filePath := filepath.Join(dir, "untracked.md")
	writeFile(t, filePath, "# Untracked\n")

	before := time.Now().Add(-time.Second)
	got := serve.GitOrMtimeDateFn(filePath)
	after := time.Now().Add(time.Second)

	if got.Before(before) || got.After(after) {
		t.Errorf("GitOrMtimeDateFn for untracked file: got %v, expected mtime in [%v, %v]", got, before, after)
	}
}
