package wiki_test

// CP4: tests for atomic wiki stale comparator.
//
// Covers every success criterion from the brief:
//   - all fresh → exit 0, no DRIFT/STALE lines
//   - moved HEAD on a summarized repo → exit 1 + STALE summary <path>
//   - signals.md changed, HEAD unchanged, indexed repo cited by concern → exit 1 + STALE concern (content-hash path)
//   - pending→indexed flip → DRIFT status line
//   - repo added / removed → DRIFT added / DRIFT removed
//   - missing/garbled reflects_* → stale, no crash, no exit 2
//   - repo with no commits (no HEAD) → stale, not exit 2
//   - hard error (wiki/ absent) → exit 2
//   - literal DRIFT/STALE prefixes exactly
//   - read-only: no wiki file is mutated by Stale

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// --- helpers ------------------------------------------------------------------

// makeCommittedRepo creates a git repo under parent/name with one commit.
// Returns the repo dir and the HEAD SHA.
func makeCommittedRepo(t *testing.T, parent, name string) (dir, sha string) {
	t.Helper()
	dir = filepath.Join(parent, name)
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
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	sha = strings.TrimSpace(string(out))
	return dir, sha
}

// addCommit adds a new commit to a git repo and returns the new HEAD SHA.
func addCommit(t *testing.T, dir string) string {
	t.Helper()
	f := filepath.Join(dir, "extra.txt")
	if err := os.WriteFile(f, []byte("extra\n"), 0o644); err != nil {
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
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD after extra commit: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// writeSignalsMD writes .claude/project/signals.md with the given content.
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

// signalsMDSHA returns the sha256 hex of the signals.md content.
func signalsMDSHA(t *testing.T, repoDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoDir, ".claude", "project", "signals.md"))
	if err != nil {
		t.Fatalf("read signals.md: %v", err)
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// buildScanBlock writes the <wiki-scan> block into wiki/index.md by running
// Scan so subsequent Stale calls have the block to parse.
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

// writeSummaryFile writes a repos/<name>.md with the given reflects_rev in frontmatter.
func writeSummaryFile(t *testing.T, wikiDir, name, reflectsRev string) string {
	t.Helper()
	p := filepath.Join(wikiDir, "repos", name+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	var content string
	if reflectsRev != "" {
		content = fmt.Sprintf("---\nreflects_rev: %s\n---\n## Summary\n", reflectsRev)
	} else {
		content = "## Summary (no frontmatter)\n"
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeConcernFile writes a concerns/<name>.md with a reflects: list in frontmatter.
func writeConcernFile(t *testing.T, wikiDir, name string, entries []string) string {
	t.Helper()
	p := filepath.Join(wikiDir, "concerns", name+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString("---\nreflects:\n")
	for _, e := range entries {
		sb.WriteString("  - ")
		sb.WriteString(e)
		sb.WriteString("\n")
	}
	sb.WriteString("---\n## Concern body\n")
	if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// runStale calls wiki.Stale and returns (exitCode, stdout).
// Hard errors (exit 2) are surfaced as t.Logf output; the data buffer is
// guaranteed empty on exit-2 paths per the new contract.
func runStale(t *testing.T, root string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code, err := wiki.Stale(root, &out)
	if err != nil {
		t.Logf("Stale hard error (code %d): %v", code, err)
	}
	return code, out.String()
}

// modtime of a file.
func modtime(t *testing.T, path string) time.Time {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.ModTime()
}

// --- tests --------------------------------------------------------------------

// TestStale_AllFresh verifies that a fully fresh wiki emits no lines and exits 0.
func TestStale_AllFresh(t *testing.T) {
	root := t.TempDir()

	// One indexed repo (has signals.md).
	repoA, sha := makeCommittedRepo(t, root, "repoA")
	writeSignalsMD(t, repoA, "# signals for A\n")

	// One pending repo.
	_, _ = makeCommittedRepo(t, root, "repoB")

	// Scan to write the block.
	runScan(t, root)

	_ = sha

	// No repos/ or concerns/ files with stale reflects — indexed/pending only.
	code, out := runStale(t, root)

	if code != 0 {
		t.Errorf("expected exit 0 (fresh), got %d; stdout: %q", code, out)
	}
	if out != "" {
		t.Errorf("expected no output for fresh wiki, got: %q", out)
	}
}

// TestStale_MovedHEAD verifies that a summarized repo whose HEAD moved emits
// STALE summary <path> and exits 1.
func TestStale_MovedHEAD(t *testing.T) {
	root := t.TempDir()

	// repoA — will be recorded as summarized.
	repoADir, sha1 := makeCommittedRepo(t, root, "repoA")

	// Scan to establish block with pending status.
	runScan(t, root)

	wikiDir := filepath.Join(root, "wiki")

	// Manually update index.md to record repoA as summarized with the initial HEAD.
	summaryRelPath := "repos/repoA.md"
	summaryPath := writeSummaryFile(t, wikiDir, "repoA", sha1)

	// Rewrite the <wiki-scan> block to mark repoA as summarized.
	indexPath := filepath.Join(wikiDir, "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	// Replace pending status with summarized.
	updated := strings.ReplaceAll(string(data),
		fmt.Sprintf(`path="repoA" status="pending"`),
		fmt.Sprintf(`path="repoA" status="summarized" summary=%q`, summaryRelPath))
	if !strings.Contains(updated, "summarized") {
		t.Fatalf("failed to inject summarized status into index.md; content:\n%s", updated)
	}
	if err := os.WriteFile(indexPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write index.md: %v", err)
	}
	_ = summaryPath

	// Now add a new commit — HEAD moves.
	sha2 := addCommit(t, repoADir)
	if sha2 == sha1 {
		t.Fatal("addCommit did not produce a new SHA")
	}

	code, out := runStale(t, root)

	if code != 1 {
		t.Errorf("expected exit 1 (stale), got %d; stdout: %q", code, out)
	}
	wantLine := "STALE summary wiki/repos/repoA.md"
	if !strings.Contains(out, wantLine) {
		t.Errorf("expected %q in output; got: %q", wantLine, out)
	}
}

// TestStale_SignalsMDChanged_IndexedConcern is the KEY test: signals.md changes
// on an indexed repo (HEAD unchanged) — the concern citing it must show STALE.
// This exercises the content-hash fingerprint path.
func TestStale_SignalsMDChanged_IndexedConcern(t *testing.T) {
	root := t.TempDir()

	// repoA — indexed (has signals.md).
	repoADir, _ := makeCommittedRepo(t, root, "repoA")
	signalsContent1 := "# signals v1\n"
	writeSignalsMD(t, repoADir, signalsContent1)

	// Scan to write the block.
	runScan(t, root)

	wikiDir := filepath.Join(root, "wiki")

	// Compute the fingerprint AT scan time.
	fp1 := signalsMDSHA(t, repoADir)

	// Write a concern that cites repoA@<fp1> (the fingerprint at scan time).
	writeConcernFile(t, wikiDir, "cross-cutting", []string{
		fmt.Sprintf("repoA@%s", fp1),
	})

	// Verify fresh first.
	code, out := runStale(t, root)
	if code != 0 {
		t.Fatalf("expected fresh before signals.md change, got exit %d: %q", code, out)
	}

	// Now change signals.md content WITHOUT touching HEAD.
	writeSignalsMD(t, repoADir, "# signals v2 — changed!\n")

	// Stale now: signals.md hash changed, concern cites old fp.
	code, out = runStale(t, root)

	if code != 1 {
		t.Errorf("expected exit 1 (stale via content-hash), got %d; stdout: %q", code, out)
	}
	wantPrefix := "STALE concern wiki/concerns/cross-cutting.md"
	if !strings.Contains(out, wantPrefix) {
		t.Errorf("expected %q in output; got: %q", wantPrefix, out)
	}
	// Must cite the repo.
	if !strings.Contains(out, "repoA") {
		t.Errorf("expected repo name in output; got: %q", out)
	}
}

// TestStale_StatusDrift verifies that a pending→indexed flip emits DRIFT status.
func TestStale_StatusDrift(t *testing.T) {
	root := t.TempDir()

	// repoA starts as pending (no signals.md).
	repoADir, _ := makeCommittedRepo(t, root, "repoA")

	// Scan to establish block with pending status.
	runScan(t, root)

	// Now add signals.md → repoA is now indexed, but the block still says pending.
	writeSignalsMD(t, repoADir, "# signals\n")

	code, out := runStale(t, root)

	if code != 1 {
		t.Errorf("expected exit 1 (status drift), got %d; stdout: %q", code, out)
	}
	wantLine := "DRIFT status repoA pending→indexed"
	if !strings.Contains(out, wantLine) {
		t.Errorf("expected %q in output; got: %q", wantLine, out)
	}
}

// TestStale_RepoAdded verifies that a new repo not in the block emits DRIFT added.
func TestStale_RepoAdded(t *testing.T) {
	root := t.TempDir()

	// Start with one repo.
	_, _ = makeCommittedRepo(t, root, "repoA")

	runScan(t, root)

	// Now add a second repo — not in the block.
	makeGitRepo(t, root, "repoB")

	code, out := runStale(t, root)

	if code != 1 {
		t.Errorf("expected exit 1 (added drift), got %d; stdout: %q", code, out)
	}
	if !strings.Contains(out, "DRIFT added repoB") {
		t.Errorf("expected DRIFT added repoB in output; got: %q", out)
	}
}

// TestStale_RepoRemoved verifies that a repo in the block but gone from disk emits DRIFT removed.
func TestStale_RepoRemoved(t *testing.T) {
	root := t.TempDir()

	// Start with two repos.
	_, _ = makeCommittedRepo(t, root, "repoA")
	repoBDir, _ := makeCommittedRepo(t, root, "repoB")

	runScan(t, root)

	// Remove repoB.
	if err := os.RemoveAll(repoBDir); err != nil {
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

// TestStale_MissingReflectsRev verifies that a summarized repo file without
// reflects_rev counts as stale (fail-safe) — no crash, no exit 2.
func TestStale_MissingReflectsRev(t *testing.T) {
	root := t.TempDir()

	_, _ = makeCommittedRepo(t, root, "repoA")

	runScan(t, root)

	wikiDir := filepath.Join(root, "wiki")
	indexPath := filepath.Join(wikiDir, "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	// Mark repoA as summarized with a summary file that has NO frontmatter.
	summaryRelPath := "repos/repoA.md"
	p := filepath.Join(wikiDir, "repos", "repoA.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	// Write summary file with NO reflects_rev (no frontmatter at all).
	if err := os.WriteFile(p, []byte("## Summary\n\nno frontmatter here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Patch the block to record summarized.
	updated := strings.ReplaceAll(string(data),
		`path="repoA" status="pending"`,
		fmt.Sprintf(`path="repoA" status="summarized" summary=%q`, summaryRelPath))
	if !strings.Contains(updated, "summarized") {
		t.Fatalf("failed to inject summarized; content:\n%s", updated)
	}
	if err := os.WriteFile(indexPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := runStale(t, root)

	// Must be stale (exit 1), not error (exit 2), and not a crash.
	if code != 1 {
		t.Errorf("expected exit 1 (stale due to missing reflects_rev), got %d; stdout: %q", code, out)
	}
	if !strings.Contains(out, "STALE summary") {
		t.Errorf("expected STALE summary in output; got: %q", out)
	}
}

// TestStale_GarbledReflects verifies that a concern with garbled reflects entries
// counts as stale — no crash, no exit 2.
func TestStale_GarbledReflects(t *testing.T) {
	root := t.TempDir()

	repoADir, _ := makeCommittedRepo(t, root, "repoA")
	writeSignalsMD(t, repoADir, "# signals\n")

	runScan(t, root)

	wikiDir := filepath.Join(root, "wiki")

	// Write a concern with a malformed reflects entry (no @ separator).
	writeConcernFile(t, wikiDir, "broken", []string{"garbled-no-at-sign"})

	code, out := runStale(t, root)

	// Garbled → stale, not error.
	if code != 1 {
		t.Errorf("expected exit 1 (stale due to garbled reflects), got %d; stdout: %q", code, out)
	}
	if !strings.Contains(out, "STALE concern") {
		t.Errorf("expected STALE concern in output; got: %q", out)
	}
}

// TestStale_NoHeadRepo verifies that a repo with no commits (no HEAD) is treated
// as stale (always-needs-summary) but does NOT cause exit 2.
func TestStale_NoHeadRepo(t *testing.T) {
	root := t.TempDir()

	// Create a git repo with NO commits (HEAD is invalid).
	repoADir := filepath.Join(root, "repoA")
	if err := os.MkdirAll(repoADir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repoADir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	// Do NOT commit — HEAD does not exist.

	runScan(t, root)

	wikiDir := filepath.Join(root, "wiki")
	indexPath := filepath.Join(wikiDir, "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	// Mark repoA as summarized with a summary file that has a reflects_rev.
	// Since there's no HEAD, the reflects_rev can't match.
	summaryRelPath := "repos/repoA.md"
	p := filepath.Join(wikiDir, "repos", "repoA.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\nreflects_rev: someSHA\n---\n## Summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	updated := strings.ReplaceAll(string(data),
		`path="repoA" status="pending"`,
		fmt.Sprintf(`path="repoA" status="summarized" summary=%q`, summaryRelPath))
	if !strings.Contains(updated, "summarized") {
		t.Fatalf("failed to inject summarized; content:\n%s", updated)
	}
	if err := os.WriteFile(indexPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := runStale(t, root)

	// No HEAD → always stale, but exit 1 not 2.
	if code == 2 {
		t.Errorf("no-HEAD repo must not cause exit 2; got exit %d; stdout: %q", code, out)
	}
	if code != 1 {
		t.Errorf("expected exit 1 (stale), got %d; stdout: %q", code, out)
	}
}

// TestStale_HardError verifies that a missing wiki/ dir causes exit 2 with a
// non-nil error and NO DRIFT/STALE lines written to the data buffer.
func TestStale_HardError(t *testing.T) {
	root := t.TempDir()
	// Do NOT run Scan — wiki/ does not exist.

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

// TestStale_LiteralPrefixes verifies that output lines use the exact required prefixes.
func TestStale_LiteralPrefixes(t *testing.T) {
	root := t.TempDir()

	// Setup: repoA pending, then add repoB to trigger DRIFT added.
	_, _ = makeCommittedRepo(t, root, "repoA")
	runScan(t, root)
	makeGitRepo(t, root, "repoB")

	_, out := runStale(t, root)

	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		hasDRIFT := strings.HasPrefix(line, "DRIFT added ") ||
			strings.HasPrefix(line, "DRIFT removed ") ||
			strings.HasPrefix(line, "DRIFT status ")
		hasSTALE := strings.HasPrefix(line, "STALE summary ") ||
			strings.HasPrefix(line, "STALE concern ")
		if !hasDRIFT && !hasSTALE {
			t.Errorf("line %q does not match any required prefix", line)
		}
	}
}

// TestStale_ReadOnly verifies that Stale does not mutate any wiki file.
func TestStale_ReadOnly(t *testing.T) {
	root := t.TempDir()

	repoADir, _ := makeCommittedRepo(t, root, "repoA")
	writeSignalsMD(t, repoADir, "# signals\n")

	runScan(t, root)

	wikiDir := filepath.Join(root, "wiki")

	// Record mod times of all wiki files.
	before := map[string]time.Time{}
	_ = filepath.Walk(wikiDir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		before[p] = fi.ModTime()
		return nil
	})

	// Add a new repo to trigger DRIFT added → exit 1 (membership drift).
	makeCommittedRepo(t, root, "repoB")
	staleCode, staleErr := wiki.Stale(root, &bytes.Buffer{})
	if staleErr != nil {
		t.Fatalf("Stale returned unexpected hard error: %v", staleErr)
	}
	if staleCode != 1 {
		t.Errorf("expected exit 1 (stale) after adding repoB, got %d", staleCode)
	}

	// Verify no wiki file was mutated.
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

// TestStale_ConcernNoFrontmatter verifies that a concern file with NO frontmatter
// at all is treated as stale (exit 1 + "STALE concern"), not fresh.
// Rationale: no recorded fingerprint baseline → can't prove freshness → re-author.
func TestStale_ConcernNoFrontmatter(t *testing.T) {
	root := t.TempDir()

	repoADir, _ := makeCommittedRepo(t, root, "repoA")
	writeSignalsMD(t, repoADir, "# signals\n")

	runScan(t, root)

	wikiDir := filepath.Join(root, "wiki")
	concernsDir := filepath.Join(wikiDir, "concerns")
	if err := os.MkdirAll(concernsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a concern with NO frontmatter at all — just a markdown body.
	noFMPath := filepath.Join(concernsDir, "no-frontmatter.md")
	if err := os.WriteFile(noFMPath, []byte("## Concern with no frontmatter\n\nSome text.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := runStale(t, root)

	if code != 1 {
		t.Errorf("expected exit 1 (stale) for concern with no frontmatter, got %d; stdout: %q", code, out)
	}
	if !strings.Contains(out, "STALE concern") {
		t.Errorf("expected STALE concern in output; got: %q", out)
	}
	if !strings.Contains(out, "no-frontmatter.md") {
		t.Errorf("expected concern filename in output; got: %q", out)
	}
}

// TestStale_ConcernNoReflectsKey verifies that a concern file with valid frontmatter
// but no "reflects:" key is treated as stale (exit 1 + "STALE concern"), not fresh.
// Rationale: no reflects baseline → can't verify freshness → fail-safe toward re-authoring.
func TestStale_ConcernNoReflectsKey(t *testing.T) {
	root := t.TempDir()

	repoADir, _ := makeCommittedRepo(t, root, "repoA")
	writeSignalsMD(t, repoADir, "# signals\n")

	runScan(t, root)

	wikiDir := filepath.Join(root, "wiki")
	concernsDir := filepath.Join(wikiDir, "concerns")
	if err := os.MkdirAll(concernsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a concern with frontmatter that has OTHER keys but no "reflects:".
	noReflectsPath := filepath.Join(concernsDir, "no-reflects.md")
	content := "---\ntitle: Some Concern\nauthor: test\n---\n## Body\n"
	if err := os.WriteFile(noReflectsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := runStale(t, root)

	if code != 1 {
		t.Errorf("expected exit 1 (stale) for concern with no reflects: key, got %d; stdout: %q", code, out)
	}
	if !strings.Contains(out, "STALE concern") {
		t.Errorf("expected STALE concern in output; got: %q", out)
	}
	if !strings.Contains(out, "no-reflects.md") {
		t.Errorf("expected concern filename in output; got: %q", out)
	}
}
