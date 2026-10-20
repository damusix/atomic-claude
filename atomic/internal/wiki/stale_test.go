package wiki_test

// Tests for the `atomic wiki stale` comparator: membership drift against the
// recorded <wiki-scan> block, and the read-only contract.

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// makeCommittedRepo returns the repo dir with one commit on HEAD.
func makeCommittedRepo(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "t@t.com"},
		{"git", "-C", dir, "config", "user.name", "T"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][]string{
		{"git", "-C", dir, "add", "."},
		{"git", "-C", dir, "commit", "-m", "init"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	return dir
}

func addCommit(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][]string{
		{"git", "-C", dir, "add", "."},
		{"git", "-C", dir, "commit", "-m", "extra"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
}

func writeSignalsMD(t *testing.T, repoDir, content string) {
	t.Helper()
	p := filepath.Join(repoDir, ".claude", "project", "signals.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runScan runs a real Scan, so Stale has a block to parse.
func runScan(t *testing.T, root string) []wiki.Member {
	t.Helper()
	members, err := wiki.Scan(root, wiki.Options{Clock: func() time.Time {
		return time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return members
}

// runStale surfaces a hard error via t.Logf; the data buffer is empty on that
// path by contract.
func runStale(t *testing.T, root string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code, err := wiki.Stale(root, &out)
	if err != nil {
		t.Logf("Stale hard error (code %d): %v", code, err)
	}
	return code, out.String()
}

func TestStale_AllFresh(t *testing.T) {
	root := t.TempDir()

	repoA := makeCommittedRepo(t, root, "repoA")
	writeSignalsMD(t, repoA, "# signals for A\n")
	makeCommittedRepo(t, root, "repoB")

	runScan(t, root)

	code, out := runStale(t, root)
	if code != 0 {
		t.Errorf("expected exit 0 (fresh), got %d; stdout: %q", code, out)
	}
	if out != "" {
		t.Errorf("expected no output for fresh wiki, got: %q", out)
	}
}

// Summaries, concerns, and bucket contents carry no freshness, so none of them
// can make stale speak, even after the summarized repo's HEAD moves.
func TestStale_IgnoresSummariesConcernsAndBuckets(t *testing.T) {
	root := t.TempDir()
	repoA := makeCommittedRepo(t, root, "repoA")

	runScan(t, root)

	wikiDir := filepath.Join(root, "wiki")
	summary := filepath.Join(wikiDir, "repos", "repoA.md")
	if err := os.MkdirAll(filepath.Dir(summary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(summary, []byte("## Summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runScan(t, root)
	addCommit(t, repoA)

	concern := filepath.Join(wikiDir, "concerns", "cross-cutting.md")
	if err := os.MkdirAll(filepath.Dir(concern), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(concern, []byte("## Concern\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := wiki.WikiAction([]string{"bucket", "add", "--root=" + root, "notes"}, t.TempDir(), root, &bytes.Buffer{}); code != 0 {
		t.Fatalf("bucket add returned %d", code)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "idea.md"), []byte("an idea\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := runStale(t, root)
	if code != 0 {
		t.Errorf("expected exit 0, got %d; stdout: %q", code, out)
	}
	if out != "" {
		t.Errorf("expected no output, got: %q", out)
	}
}

func TestStale_StatusDrift(t *testing.T) {
	root := t.TempDir()
	repoA := makeCommittedRepo(t, root, "repoA")

	runScan(t, root)

	writeSignalsMD(t, repoA, "# signals\n")

	code, out := runStale(t, root)
	if code != 1 {
		t.Errorf("expected exit 1 (status drift), got %d; stdout: %q", code, out)
	}
	if want := "DRIFT status repoA pending→indexed"; !strings.Contains(out, want) {
		t.Errorf("expected %q in output; got: %q", want, out)
	}
}

func TestStale_RepoAdded(t *testing.T) {
	root := t.TempDir()
	makeCommittedRepo(t, root, "repoA")

	runScan(t, root)

	makeGitRepo(t, root, "repoB")

	code, out := runStale(t, root)
	if code != 1 {
		t.Errorf("expected exit 1 (added drift), got %d; stdout: %q", code, out)
	}
	if !strings.Contains(out, "DRIFT added repoB") {
		t.Errorf("expected DRIFT added repoB in output; got: %q", out)
	}
}

func TestStale_RepoRemoved(t *testing.T) {
	root := t.TempDir()
	makeCommittedRepo(t, root, "repoA")
	repoB := makeCommittedRepo(t, root, "repoB")

	runScan(t, root)

	if err := os.RemoveAll(repoB); err != nil {
		t.Fatalf("remove repoB: %v", err)
	}

	code, out := runStale(t, root)
	if code != 1 {
		t.Errorf("expected exit 1 (removed drift), got %d; stdout: %q", code, out)
	}
	if !strings.Contains(out, "DRIFT removed repoB") {
		t.Errorf("expected DRIFT removed repoB in output; got: %q", out)
	}
}

// Narrowing [realm] repos after a scan must surface the dropped repo as drift,
// and a discovery warning goes to stderr without polluting the DRIFT stream.
func TestStale_RepoOutsideDeclaredPathsIsRemoved(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()
	root := t.TempDir()
	makeCommittedRepo(t, filepath.Join(root, "agency"), "repoA")
	makeCommittedRepo(t, filepath.Join(root, "scratch"), "repoB")

	runScan(t, root)

	cfg := config.RepoConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("[realm]\nrepos = [\"agency\", \"gone\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var code int
	var out string
	stderr := captureStderr(t, func() { code, out = runStale(t, root) })
	if code != 1 {
		t.Errorf("expected exit 1 (removed drift), got %d; stdout: %q", code, out)
	}
	if out != "DRIFT removed scratch/repoB\n" {
		t.Errorf("stdout = %q, want only the removal of scratch/repoB", out)
	}
	if !strings.HasPrefix(stderr, "atomic wiki stale: warning: ") || !strings.Contains(stderr, `"gone"`) {
		t.Errorf("stderr = %q, want a prefixed warning about \"gone\"", stderr)
	}
}

func TestScan_PrintsDiscoveryWarningsToStderr(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()
	root := t.TempDir()
	makeCommittedRepo(t, filepath.Join(root, "agency"), "repoA")
	cfg := config.RepoConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("[realm]\nrepos = [\"agency\", \"gone\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stderr := captureStderr(t, func() { runScan(t, root) })
	if !strings.HasPrefix(stderr, "atomic wiki scan: warning: ") || !strings.Contains(stderr, `"gone"`) {
		t.Errorf("stderr = %q, want a prefixed warning about \"gone\"", stderr)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() {
		os.Stderr = old
		w.Close()
		r.Close()
	}()
	var captured []byte
	var readErr error
	done := make(chan struct{})
	go func() {
		captured, readErr = io.ReadAll(r)
		close(done)
	}()
	fn()
	w.Close()
	<-done
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(captured)
}

func TestStale_HardError(t *testing.T) {
	root := t.TempDir()

	var buf bytes.Buffer
	code, err := wiki.Stale(root, &buf)

	if code != 2 {
		t.Errorf("expected exit 2 (hard error), got %d", code)
	}
	if err == nil {
		t.Errorf("expected non-nil error for hard-error path, got nil")
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty data buffer on hard-error path, got: %q", buf.String())
	}
}

func TestStale_LiteralPrefixes(t *testing.T) {
	root := t.TempDir()
	makeCommittedRepo(t, root, "repoA")
	runScan(t, root)
	makeGitRepo(t, root, "repoB")

	_, out := runStale(t, root)

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "DRIFT added ") &&
			!strings.HasPrefix(line, "DRIFT removed ") &&
			!strings.HasPrefix(line, "DRIFT status ") {
			t.Errorf("line %q does not match any DRIFT prefix", line)
		}
	}
}

func TestStale_ReadOnly(t *testing.T) {
	root := t.TempDir()
	repoA := makeCommittedRepo(t, root, "repoA")
	writeSignalsMD(t, repoA, "# signals\n")

	runScan(t, root)

	wikiDir := filepath.Join(root, "wiki")

	before := map[string]time.Time{}
	_ = filepath.Walk(wikiDir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		before[p] = fi.ModTime()
		return nil
	})

	makeCommittedRepo(t, root, "repoB")
	staleCode, staleErr := wiki.Stale(root, &bytes.Buffer{})
	if staleErr != nil {
		t.Fatalf("Stale returned unexpected hard error: %v", staleErr)
	}
	if staleCode != 1 {
		t.Errorf("expected exit 1 (stale) after adding repoB, got %d", staleCode)
	}

	_ = filepath.Walk(wikiDir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		mt, ok := before[p]
		if !ok {
			t.Errorf("new file appeared in wiki/ after Stale: %s", p)
			return nil
		}
		if fi.ModTime() != mt {
			t.Errorf("file %s was modified by Stale (before: %v, after: %v)", p, mt, fi.ModTime())
		}
		return nil
	})
}
