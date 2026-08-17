package wiki_test

// Tests for the `atomic wiki stale` comparator, covering membership drift,
// both fingerprint paths (git HEAD and content hash), the fail-safe cases,
// and the read-only contract.

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

// makeCommittedRepo returns the repo dir and its HEAD SHA.
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

func signalsMDSHA(t *testing.T, repoDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoDir, ".claude", "project", "signals.md"))
	if err != nil {
		t.Fatalf("read signals.md: %v", err)
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// buildScanBlock runs a real Scan, so Stale has a block to parse.
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

func writeSplitSummaryFile(t *testing.T, wikiDir, name, domain, reflectsRev string) string {
	t.Helper()
	p := filepath.Join(wikiDir, "repos", name, domain+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nreflects_rev: %s\n---\n## Summary\n", reflectsRev)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// patchMemberSummarized rewrites <repo path="path" status="pending"/> in
// wikiDir/index.md's <wiki-scan> block to status="summarized" with the given
// summary attribute, so it matches what the live classifyMembers call inside
// Stale will independently derive from the summary files already on disk.
func patchMemberSummarized(t *testing.T, wikiDir, path, summaryRelPath string) {
	t.Helper()
	indexPath := filepath.Join(wikiDir, "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	old := fmt.Sprintf(`path=%q status="pending"`, path)
	replacement := fmt.Sprintf(`path=%q status="summarized" summary=%q`, path, summaryRelPath)
	updated := strings.ReplaceAll(string(data), old, replacement)
	if updated == string(data) {
		t.Fatalf("failed to patch %q into summarized in index.md; content:\n%s", path, data)
	}
	if err := os.WriteFile(indexPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write index.md: %v", err)
	}
}

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

// modtime of a file.
func modtime(t *testing.T, path string) time.Time {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.ModTime()
}

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

// An indexed repo whose signals.md changed without a new commit: only the
// content-hash path can catch this, since HEAD has not moved.
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

// No baseline means freshness is unprovable, so the fail-safe is stale.
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

// All four member-location x summary-layout shapes at once: three of them
// used to resolve to a nonexistent repo dir and report a false STALE.
func TestStale_ShapeMatrixAllFresh(t *testing.T) {
	root := t.TempDir()

	_, alphaSHA := makeCommittedRepo(t, root, "alpha")          // root + flat
	_, betaSHA := makeCommittedRepo(t, root, "beta")            // root + domain-split
	_, gammaSHA := makeCommittedRepo(t, root, "packages/gamma") // nested + domain-split
	_, deltaSHA := makeCommittedRepo(t, root, "packages/delta") // nested + flat

	runScan(t, root)

	wikiDir := filepath.Join(root, "wiki")

	writeSummaryFile(t, wikiDir, "alpha", alphaSHA)
	patchMemberSummarized(t, wikiDir, "alpha", "repos/alpha.md")

	writeSplitSummaryFile(t, wikiDir, "beta", "design", betaSHA)
	patchMemberSummarized(t, wikiDir, "beta", "repos/beta/")

	writeSplitSummaryFile(t, wikiDir, "gamma", "design", gammaSHA)
	patchMemberSummarized(t, wikiDir, "packages/gamma", "repos/gamma/")

	writeSummaryFile(t, wikiDir, "delta", deltaSHA)
	patchMemberSummarized(t, wikiDir, "packages/delta", "repos/delta.md")

	code, out := runStale(t, root)

	if code != 0 {
		t.Errorf("expected exit 0 (fresh) across all four shapes, got %d; stdout: %q", code, out)
	}
	if out != "" {
		t.Errorf("expected zero output for a fully-fresh shape matrix, got: %q", out)
	}
}

// One HEAD moves in the same four-shape realm; only that summary goes stale.
func TestStale_ShapeMatrixOneMovedHEAD(t *testing.T) {
	root := t.TempDir()

	_, alphaSHA := makeCommittedRepo(t, root, "alpha")
	_, betaSHA := makeCommittedRepo(t, root, "beta")
	gammaDir, gammaSHA := makeCommittedRepo(t, root, "packages/gamma")
	_, deltaSHA := makeCommittedRepo(t, root, "packages/delta")

	runScan(t, root)

	wikiDir := filepath.Join(root, "wiki")

	writeSummaryFile(t, wikiDir, "alpha", alphaSHA)
	patchMemberSummarized(t, wikiDir, "alpha", "repos/alpha.md")

	writeSplitSummaryFile(t, wikiDir, "beta", "design", betaSHA)
	patchMemberSummarized(t, wikiDir, "beta", "repos/beta/")

	writeSplitSummaryFile(t, wikiDir, "gamma", "design", gammaSHA)
	patchMemberSummarized(t, wikiDir, "packages/gamma", "repos/gamma/")

	writeSummaryFile(t, wikiDir, "delta", deltaSHA)
	patchMemberSummarized(t, wikiDir, "packages/delta", "repos/delta.md")

	// Confirm the baseline is fresh before moving anything.
	if code, out := runStale(t, root); code != 0 {
		t.Fatalf("expected fresh baseline before moving HEAD, got exit %d: %q", code, out)
	}

	// Move only packages/gamma's HEAD — a nested + domain-split member,
	// one of the three shapes the pre-fix code got wrong.
	addCommit(t, gammaDir)

	code, out := runStale(t, root)

	if code != 1 {
		t.Errorf("expected exit 1 (stale) after moving packages/gamma's HEAD, got %d; stdout: %q", code, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected exactly one output line, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "STALE summary wiki/repos/gamma/design.md") {
		t.Errorf("expected STALE summary line for packages/gamma's summary, got: %q", lines[0])
	}
}
