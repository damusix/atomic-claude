package wiki_test

// CP3: tests for atomic wiki stamp.
// Covers:
//   - Summary mode: reflects_rev written from git HEAD; rest of file preserved.
//   - Concern mode: reflects: list carries HEAD-sha (summarized) and signals.md
//     sha256 (indexed); unresolvable cited repo is skipped, not crashed.

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// makeGitRepo creates a git repo with one commit and returns the HEAD SHA.
func makeCommittedGitRepo(t *testing.T) (dir, sha string) {
	t.Helper()
	dir = t.TempDir()
	cmds := [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "t@t.com"},
		{"git", "-C", dir, "config", "user.name", "T"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	// Write a file and commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	addCmds := [][]string{
		{"git", "-C", dir, "add", "."},
		{"git", "-C", dir, "commit", "-m", "init"},
	}
	for _, c := range addCmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	sha = strings.TrimSpace(string(out))
	return dir, sha
}

// writeFrontmatterFile writes a markdown file with YAML frontmatter + body.
func writeFrontmatterFile(t *testing.T, path string, meta map[string]any, body string) {
	t.Helper()
	doc, err := frontmatter.Emit(meta, body)
	if err != nil {
		t.Fatalf("emit frontmatter: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

// TestStamp_SummaryMode verifies that StampSummary writes the correct
// reflects_rev from git HEAD and preserves the rest of the frontmatter.
func TestStamp_SummaryMode(t *testing.T) {
	repoDir, wantSHA := makeCommittedGitRepo(t)

	summaryFile := filepath.Join(t.TempDir(), "repos", "repoA.md")
	body := "## Summary\n\nSome content here.\n"
	writeFrontmatterFile(t, summaryFile, map[string]any{
		"title": "repoA summary",
		"kind":  "summary",
	}, body)

	if err := wiki.StampSummary(summaryFile, repoDir); err != nil {
		t.Fatalf("StampSummary: %v", err)
	}

	data, err := os.ReadFile(summaryFile)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}

	meta, gotBody, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse stamped file: %v", err)
	}

	// reflects_rev must equal git HEAD.
	rev, ok := meta["reflects_rev"]
	if !ok {
		t.Fatalf("reflects_rev not written; frontmatter: %v", meta)
	}
	if rev != wantSHA {
		t.Errorf("reflects_rev = %q, want %q", rev, wantSHA)
	}

	// Pre-existing fields must be preserved.
	if meta["title"] != "repoA summary" {
		t.Errorf("title lost; meta: %v", meta)
	}
	if meta["kind"] != "summary" {
		t.Errorf("kind lost; meta: %v", meta)
	}

	// Body must be preserved byte-for-byte.
	if gotBody != body {
		t.Errorf("body changed:\ngot: %q\nwant: %q", gotBody, body)
	}
}

// TestStamp_SummaryMode_Idempotent verifies that stamping twice produces the
// same result (no duplicate keys, no data loss).
func TestStamp_SummaryMode_Idempotent(t *testing.T) {
	repoDir, wantSHA := makeCommittedGitRepo(t)
	summaryFile := filepath.Join(t.TempDir(), "repos", "repoA.md")
	writeFrontmatterFile(t, summaryFile, map[string]any{"title": "repoA"}, "body\n")

	for i := 0; i < 2; i++ {
		if err := wiki.StampSummary(summaryFile, repoDir); err != nil {
			t.Fatalf("StampSummary pass %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(summaryFile)
	if err != nil {
		t.Fatalf("read summary file: %v", err)
	}
	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse summary frontmatter: %v", err)
	}
	if meta["reflects_rev"] != wantSHA {
		t.Errorf("reflects_rev = %q after 2 stamps, want %q", meta["reflects_rev"], wantSHA)
	}
}

// TestStamp_ConcernMode verifies that StampConcern writes the reflects: list
// with the correct fingerprint for each cited repo:
//   - summarized repo → HEAD SHA
//   - indexed repo    → sha256 of signals.md content
func TestStamp_ConcernMode(t *testing.T) {
	wikiRoot := t.TempDir()

	// Summarized repo: built directly under wikiRoot/repoA to avoid cross-device
	// rename failures. No signals.md — fingerprint comes from git HEAD.
	repoADir := filepath.Join(wikiRoot, "repoA")
	if err := os.MkdirAll(repoADir, 0o755); err != nil {
		t.Fatalf("mkdir repoA: %v", err)
	}
	for _, c := range [][]string{
		{"git", "-C", repoADir, "init"},
		{"git", "-C", repoADir, "config", "user.email", "t@t.com"},
		{"git", "-C", repoADir, "config", "user.name", "T"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repoADir, "README.md"), []byte("# repoA\n"), 0o644); err != nil {
		t.Fatalf("write repoA README: %v", err)
	}
	for _, c := range [][]string{
		{"git", "-C", repoADir, "add", "."},
		{"git", "-C", repoADir, "commit", "-m", "init"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	out, err := exec.Command("git", "-C", repoADir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse repoA HEAD: %v", err)
	}
	summarizedSHA := strings.TrimSpace(string(out))

	// Indexed repo: a git repo WITH .claude/project/signals.md, built directly
	// under wikiRoot/repoB.
	repoBDir := filepath.Join(wikiRoot, "repoB")
	if err := os.MkdirAll(repoBDir, 0o755); err != nil {
		t.Fatalf("mkdir repoB: %v", err)
	}
	for _, c := range [][]string{
		{"git", "-C", repoBDir, "init"},
		{"git", "-C", repoBDir, "config", "user.email", "t@t.com"},
		{"git", "-C", repoBDir, "config", "user.name", "T"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	signalsContent := "# Deterministic signals\n\n## Tree\n\nfoo bar\n"
	signalsMDPath := filepath.Join(repoBDir, ".claude", "project", "signals.md")
	if err := os.MkdirAll(filepath.Dir(signalsMDPath), 0o755); err != nil {
		t.Fatalf("mkdir signals dir: %v", err)
	}
	if err := os.WriteFile(signalsMDPath, []byte(signalsContent), 0o644); err != nil {
		t.Fatalf("write signals.md: %v", err)
	}
	// Compute expected sha256.
	h := sha256.Sum256([]byte(signalsContent))
	wantIndexedFP := fmt.Sprintf("%x", h)

	// Concern file to stamp.
	concernFile := filepath.Join(wikiRoot, "concerns", "shared.md")
	writeFrontmatterFile(t, concernFile, map[string]any{"title": "shared concern"}, "concern body\n")

	// Stamp: cite repoA (summarized) and repoB (indexed).
	if err := wiki.StampConcern(concernFile, wikiRoot, []string{"repoA", "repoB"}); err != nil {
		t.Fatalf("StampConcern: %v", err)
	}

	data, _ := os.ReadFile(concernFile)
	meta, gotBody, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse stamped concern: %v", err)
	}

	// reflects: must be a list.
	rawReflects, ok := meta["reflects"]
	if !ok {
		t.Fatalf("reflects not written; meta: %v", meta)
	}
	reflects, ok := rawReflects.([]any)
	if !ok {
		t.Fatalf("reflects is not a list: %T %v", rawReflects, rawReflects)
	}
	if len(reflects) != 2 {
		t.Fatalf("reflects has %d entries, want 2: %v", len(reflects), reflects)
	}

	// Build a lookup for easy assertions.
	byRepo := map[string]string{}
	for _, item := range reflects {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("reflects item is not string: %T", item)
		}
		parts := strings.SplitN(s, "@", 2)
		if len(parts) != 2 {
			t.Fatalf("reflects item %q not in <id>@<fp> format", s)
		}
		byRepo[parts[0]] = parts[1]
	}

	if fp, ok := byRepo["repoA"]; !ok {
		t.Errorf("repoA missing from reflects; got %v", byRepo)
	} else if fp != summarizedSHA {
		t.Errorf("repoA fingerprint = %q, want HEAD SHA %q", fp, summarizedSHA)
	}

	if fp, ok := byRepo["repoB"]; !ok {
		t.Errorf("repoB missing from reflects; got %v", byRepo)
	} else if fp != wantIndexedFP {
		t.Errorf("repoB fingerprint = %q, want signals.md sha256 %q", fp, wantIndexedFP)
	}

	// Body must be preserved.
	if gotBody != "concern body\n" {
		t.Errorf("body changed: %q", gotBody)
	}

	// Pre-existing fields preserved.
	if meta["title"] != "shared concern" {
		t.Errorf("title lost; meta: %v", meta)
	}
}

// TestStamp_ConcernMode_AllUnresolvable verifies that when EVERY cited id is
// unresolvable (no dir, no HEAD), StampConcern returns no error and writes an
// empty reflects: sequence. This regression-locks the nil-entries bug that
// caused the unsupported-<nil>-type path in anyToNode.
func TestStamp_ConcernMode_AllUnresolvable(t *testing.T) {
	wikiRoot := t.TempDir()

	concernFile := filepath.Join(wikiRoot, "concerns", "c.md")
	writeFrontmatterFile(t, concernFile, map[string]any{"title": "missing concern"}, "body\n")

	// "ghost1" and "ghost2" do not exist under wikiRoot at all.
	if err := wiki.StampConcern(concernFile, wikiRoot, []string{"ghost1", "ghost2"}); err != nil {
		t.Fatalf("StampConcern must not error when all ids are unresolvable: %v", err)
	}

	data, err := os.ReadFile(concernFile)
	if err != nil {
		t.Fatalf("read stamped concern: %v", err)
	}
	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse stamped concern: %v", err)
	}

	// reflects: must be present and empty (not absent, not a crash).
	rawReflects, ok := meta["reflects"]
	if !ok {
		t.Fatalf("reflects key not written; meta: %v", meta)
	}
	reflects, ok := rawReflects.([]any)
	if !ok {
		t.Fatalf("reflects is not a list: %T %v", rawReflects, rawReflects)
	}
	if len(reflects) != 0 {
		t.Errorf("reflects has %d entries, want 0 (all unresolvable): %v", len(reflects), reflects)
	}

	// Pre-existing fields must survive.
	if meta["title"] != "missing concern" {
		t.Errorf("title lost; meta: %v", meta)
	}
}

// TestStamp_ConcernMode_UnresolvableSkipped verifies that a cited repo id that
// does not exist under wikiRoot is silently skipped; the command does not crash.
func TestStamp_ConcernMode_UnresolvableSkipped(t *testing.T) {
	wikiRoot := t.TempDir()

	// One valid summarized repo.
	repoADir := filepath.Join(wikiRoot, "repoA")
	if err := os.MkdirAll(repoADir, 0o755); err != nil {
		t.Fatalf("mkdir repoA: %v", err)
	}
	initCmds := [][]string{
		{"git", "-C", repoADir, "init"},
		{"git", "-C", repoADir, "config", "user.email", "t@t.com"},
		{"git", "-C", repoADir, "config", "user.name", "T"},
	}
	for _, c := range initCmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repoADir, "f.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, c := range [][]string{
		{"git", "-C", repoADir, "add", "."},
		{"git", "-C", repoADir, "commit", "-m", "init"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	concernFile := filepath.Join(wikiRoot, "concerns", "c.md")
	writeFrontmatterFile(t, concernFile, nil, "body\n")

	// "missing" does not exist under wikiRoot.
	if err := wiki.StampConcern(concernFile, wikiRoot, []string{"repoA", "missing"}); err != nil {
		t.Fatalf("StampConcern must not error on unresolvable repo: %v", err)
	}

	data, _ := os.ReadFile(concernFile)
	meta, _, _ := frontmatter.Parse(string(data))

	rawReflects := meta["reflects"]
	reflects, ok := rawReflects.([]any)
	if !ok {
		t.Fatalf("reflects not a list: %T %v", rawReflects, rawReflects)
	}

	// Only repoA should appear; "missing" is skipped.
	if len(reflects) != 1 {
		t.Errorf("reflects has %d entries, want 1 (unresolvable skipped): %v", len(reflects), reflects)
	}
	s, _ := reflects[0].(string)
	if !strings.HasPrefix(s, "repoA@") {
		t.Errorf("reflects[0] = %q, want repoA@<sha>", s)
	}
}
