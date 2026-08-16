package wiki_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// fixedClock returns a fixed time for deterministic test output.
func fixedClock() func() time.Time {
	t := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

// makeGitRepo creates a directory and runs git init in it.
func makeGitRepo(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", dir)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init %s: %v", dir, err)
	}
	return dir
}

// writeSignals creates the .claude/project/signals.md file in dir.
func writeSignals(t *testing.T, dir string) {
	t.Helper()
	p := filepath.Join(dir, ".claude", "project", "signals.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("# signals\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupFixtureTree builds:
//
//	root/
//	  repoA/           — git repo WITH signals  → indexed
//	  repoB/           — git repo WITHOUT signals → pending
//	  not-a-repo/
//	    repoC/         — git repo nested inside non-repo dir → pending
//	  node_modules/    — junk skip dir
//	  dist/            — junk skip dir
//	  tmp/             — junk skip dir
//
// root itself IS a git repo (must be excluded from membership).
func setupFixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Make root a git repo itself — must NOT appear as a member.
	cmd := exec.Command("git", "init", root)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init root: %v", err)
	}

	// repoA — has signals
	repoA := makeGitRepo(t, root, "repoA")
	writeSignals(t, repoA)

	// repoB — no signals
	makeGitRepo(t, root, "repoB")

	// not-a-repo/repoC
	notRepo := filepath.Join(root, "not-a-repo")
	if err := os.MkdirAll(notRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	makeGitRepo(t, notRepo, "repoC")

	// Junk dirs
	for _, junk := range []string{"node_modules", "dist", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, junk), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

// indexMDPath returns the path to wiki/index.md under root.
func indexMDPath(root string) string {
	return filepath.Join(root, "wiki", "index.md")
}

// readIndexMD reads wiki/index.md content.
func readIndexMD(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(indexMDPath(root))
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	return string(data)
}

// ---- Tests ----

func TestScan_HappyPath(t *testing.T) {
	root := setupFixtureTree(t)
	opts := wiki.Options{Clock: fixedClock()}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// Scaffold created
	for _, sub := range []string{"wiki/index.md", "wiki/README.md", "wiki/.gitignore", "wiki/repos", "wiki/concerns"} {
		p := filepath.Join(root, sub)
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("expected scaffold path %s to exist: %v", sub, err)
		}
	}

	// wiki/ is a git repo
	gitDir := filepath.Join(root, "wiki", ".git")
	if _, err := os.Lstat(gitDir); err != nil {
		t.Errorf("wiki/.git not found — git init not run: %v", err)
	}

	// .gitignore ignores .dirty
	gi, err := os.ReadFile(filepath.Join(root, "wiki", ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(gi), ".dirty") {
		t.Error(".gitignore does not contain .dirty")
	}

	content := readIndexMD(t, root)

	// Block markers present
	if !strings.Contains(content, `<wiki-scan`) {
		t.Error("index.md missing <wiki-scan open tag")
	}
	if !strings.Contains(content, `</wiki-scan>`) {
		t.Error("index.md missing </wiki-scan> close tag")
	}

	// root attribute present
	if !strings.Contains(content, `root="`) {
		t.Error("wiki-scan missing root attribute")
	}

	// generated attribute with injected date
	if !strings.Contains(content, `generated="2026-06-06"`) {
		t.Errorf("wiki-scan missing generated date; content:\n%s", content)
	}

	// repoA → indexed (has signals): both path AND status must appear together on the same line.
	{
		found := false
		for _, line := range strings.Split(content, "\n") {
			if strings.Contains(line, `path="repoA"`) && strings.Contains(line, `status="indexed"`) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("repoA not indexed (path and status must be on the same <repo/> tag); content:\n%s", content)
		}
	}

	// repoB → pending: both path AND status must appear together on the same line.
	{
		found := false
		for _, line := range strings.Split(content, "\n") {
			if strings.Contains(line, `path="repoB"`) && strings.Contains(line, `status="pending"`) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("repoB not pending (path and status must be on the same <repo/> tag); content:\n%s", content)
		}
	}

	// repoC (nested) → pending
	if !strings.Contains(content, `repoC`) {
		t.Errorf("repoC (nested in not-a-repo) not found; content:\n%s", content)
	}

	// root itself NOT a member
	if strings.Contains(content, `path="."`) || strings.Contains(content, `path=""`) {
		t.Errorf("root should not appear as a member; content:\n%s", content)
	}

	// Junk dirs not present as members
	for _, junk := range []string{"node_modules", "dist", "tmp"} {
		if strings.Contains(content, `path="`+junk+`"`) {
			t.Errorf("junk dir %q should not appear as member", junk)
		}
	}
}

func TestScan_IndexedStatus(t *testing.T) {
	root := t.TempDir()
	repoA := makeGitRepo(t, root, "repoA")
	writeSignals(t, repoA)
	makeGitRepo(t, root, "repoB") // no signals → pending

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	content := readIndexMD(t, root)
	if !strings.Contains(content, `status="indexed"`) {
		t.Errorf("repoA should be indexed: %s", content)
	}
	if !strings.Contains(content, `status="pending"`) {
		t.Errorf("repoB should be pending: %s", content)
	}
}

func TestScan_RootExcludedFromMembership(t *testing.T) {
	root := t.TempDir()
	// root is itself a git repo
	cmd := exec.Command("git", "init", root)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	// Add one child repo so there's something in the block
	makeGitRepo(t, root, "child")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	content := readIndexMD(t, root)
	// root must not appear
	if strings.Contains(content, `path="."`) || strings.Contains(content, `path=""`) {
		t.Errorf("root must not appear as member; content:\n%s", content)
	}
	// child must appear
	if !strings.Contains(content, `path="child"`) {
		t.Errorf("child repo not found in members; content:\n%s", content)
	}
}

func TestScan_Idempotent_NarrativePreserved(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Inject narrative content OUTSIDE the wiki-scan block
	narrative := "\n## My notes\n\nSome important context about this realm.\n"
	content := readIndexMD(t, root)
	contentWithNarrative := content + narrative
	if err := os.WriteFile(indexMDPath(root), []byte(contentWithNarrative), 0o644); err != nil {
		t.Fatal(err)
	}

	// Re-scan
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	afterRescan := readIndexMD(t, root)

	// Narrative must be preserved byte-for-byte
	if !strings.Contains(afterRescan, narrative) {
		t.Errorf("narrative lost after re-scan\nbefore rescan had: %q\nafter rescan: %s", narrative, afterRescan)
	}

	// Block still present
	if !strings.Contains(afterRescan, `<wiki-scan`) {
		t.Error("wiki-scan block missing after re-scan")
	}
}

func TestScan_SummarizedPreserved(t *testing.T) {
	root := t.TempDir()
	repoA := makeGitRepo(t, root, "repoA")
	_ = repoA

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Manually mark repoA as summarized and create the summary file.
	summaryPath := filepath.Join(root, "wiki", "repos", "repoA.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(summaryPath, []byte("# repoA summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Rewrite index.md to mark repoA as summarized.
	content := readIndexMD(t, root)
	content = strings.ReplaceAll(content, `status="pending"`, `status="summarized" summary="repos/repoA.md"`)
	if err := os.WriteFile(indexMDPath(root), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Re-scan — summarized entry with existing summary file must be preserved.
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	afterRescan := readIndexMD(t, root)
	if !strings.Contains(afterRescan, `status="summarized"`) {
		t.Errorf("summarized status not preserved; content:\n%s", afterRescan)
	}
}

func TestScan_SummarizedDowngradedWhenFileMissing(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Mark repoA as summarized but DO NOT create the summary file.
	content := readIndexMD(t, root)
	content = strings.ReplaceAll(content, `status="pending"`, `status="summarized" summary="repos/repoA.md"`)
	if err := os.WriteFile(indexMDPath(root), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Re-scan — with no summary file, must downgrade to pending.
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	afterRescan := readIndexMD(t, root)
	if strings.Contains(afterRescan, `status="summarized"`) {
		t.Errorf("summarized without summary file should have been downgraded; content:\n%s", afterRescan)
	}
	if !strings.Contains(afterRescan, `status="pending"`) {
		t.Errorf("repoA should be pending after downgrade; content:\n%s", afterRescan)
	}
}

func TestScan_SummarizedDiscoveredFromDisk(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoB")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Summary file exists on disk but no prior index entry says "summarized".
	summaryPath := filepath.Join(root, "wiki", "repos", "repoB.md")
	if err := os.WriteFile(summaryPath, []byte("# repoB summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	content := readIndexMD(t, root)
	if !strings.Contains(content, `<repo path="repoB" status="summarized" summary="repos/repoB.md"/>`) {
		t.Errorf("summary on disk should classify repoB as summarized; content:\n%s", content)
	}
	if !strings.Contains(content, `- [repoB](repos/repoB.md)`) {
		t.Errorf("Members section should link summarized repoB to its wiki page; content:\n%s", content)
	}
}

func TestScan_SummarizedDiscoveredDirForm(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoB")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Domain-split summary: wiki/repos/repoB/<domain>.md, no single repoB.md.
	domainPath := filepath.Join(root, "wiki", "repos", "repoB", "cloud.md")
	if err := os.MkdirAll(filepath.Dir(domainPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(domainPath, []byte("# repoB cloud domain\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	content := readIndexMD(t, root)
	if !strings.Contains(content, `<repo path="repoB" status="summarized" summary="repos/repoB/"/>`) {
		t.Errorf("domain-dir summary should classify repoB as summarized; content:\n%s", content)
	}
	if !strings.Contains(content, `- [repoB](repos/repoB/)`) {
		t.Errorf("Members section should link summarized repoB to its summary dir; content:\n%s", content)
	}
}

func TestScan_SignalsWinOverDiscoveredSummary(t *testing.T) {
	root := t.TempDir()
	repoA := makeGitRepo(t, root, "repoA")
	writeSignals(t, repoA)

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Leftover summary from before the repo graduated to signals.
	summaryPath := filepath.Join(root, "wiki", "repos", "repoA.md")
	if err := os.WriteFile(summaryPath, []byte("# repoA summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	content := readIndexMD(t, root)
	if !strings.Contains(content, `<repo path="repoA" status="indexed"`) {
		t.Errorf("repo with signals should stay indexed even with a leftover summary; content:\n%s", content)
	}
}

func TestScan_CollisionNoIndexMD(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	// Create wiki/ dir without index.md
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	opts := wiki.Options{Clock: fixedClock()}
	_, err := wiki.Scan(root, opts)
	if err == nil {
		t.Fatal("expected error for wiki/ without index.md, got nil")
	}
	if !strings.Contains(err.Error(), wikiDir) && !strings.Contains(err.Error(), "wiki") {
		t.Errorf("error should name the path; got: %v", err)
	}
}

func TestScan_CollisionIndexMDMissingMarker(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	// Create wiki/index.md without the wiki-scan marker
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(wikiDir, "index.md")
	if err := os.WriteFile(indexPath, []byte("# Some existing wiki without marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := wiki.Options{Clock: fixedClock()}
	_, err := wiki.Scan(root, opts)
	if err == nil {
		t.Fatal("expected error for index.md without wiki-scan marker, got nil")
	}
	if !strings.Contains(err.Error(), indexPath) && !strings.Contains(err.Error(), "wiki") {
		t.Errorf("error should name the path; got: %v", err)
	}
}

func TestScan_GitInitSkippedIfAlreadyRepo(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}

	// First scan — creates wiki/ and runs git init
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}
	// Verify wiki is a git repo
	if _, err := os.Lstat(filepath.Join(root, "wiki", ".git")); err != nil {
		t.Fatal("wiki/.git not found after first scan")
	}

	// Second scan — must not error (git init skipped gracefully)
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan (re-scan existing wiki): %v", err)
	}
}

func TestScan_NestedRepoFound(t *testing.T) {
	root := t.TempDir()
	// not-a-repo/deeply/repoC
	deepDir := filepath.Join(root, "not-a-repo", "deeply")
	if err := os.MkdirAll(deepDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeGitRepo(t, deepDir, "repoC")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	content := readIndexMD(t, root)
	if !strings.Contains(content, "repoC") {
		t.Errorf("nested repo repoC not found; content:\n%s", content)
	}
}

func TestScan_WikiDirSkippedDuringWalk(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	// First scan creates wiki/
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Run git init in wiki/ (already done by scan), verify wiki itself not counted as member
	content := readIndexMD(t, root)
	if strings.Contains(content, `path="wiki"`) {
		t.Errorf("wiki dir must not be counted as member; content:\n%s", content)
	}
}

func TestScan_StableSort(t *testing.T) {
	root := t.TempDir()
	// Create repos in non-alphabetical order
	for _, name := range []string{"zebra", "alpha", "mango"} {
		makeGitRepo(t, root, name)
	}

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	content := readIndexMD(t, root)
	alphaIdx := strings.Index(content, `path="alpha"`)
	mangoIdx := strings.Index(content, `path="mango"`)
	zebraIdx := strings.Index(content, `path="zebra"`)

	if alphaIdx == -1 || mangoIdx == -1 || zebraIdx == -1 {
		t.Fatalf("not all repos in content:\n%s", content)
	}
	if !(alphaIdx < mangoIdx && mangoIdx < zebraIdx) {
		t.Errorf("repos not in stable sorted order: alpha=%d mango=%d zebra=%d", alphaIdx, mangoIdx, zebraIdx)
	}
}

// TestScan_IndexedMemberHasSignalsAttribute asserts that a repo classified as
// "indexed" emits a non-empty signals= attribute pointing at the repo's
// .claude/project/signals.md. This is the <wiki-scan> block contract for
// indexed members.
func TestScan_IndexedMemberHasSignalsAttribute(t *testing.T) {
	root := t.TempDir()
	repoA := makeGitRepo(t, root, "repoA")
	writeSignals(t, repoA)

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	content := readIndexMD(t, root)

	// Find the <repo .../> line for repoA.
	var repoALine string
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, `path="repoA"`) {
			repoALine = line
			break
		}
	}
	if repoALine == "" {
		t.Fatalf("repoA tag not found; content:\n%s", content)
	}

	// Must be indexed.
	if !strings.Contains(repoALine, `status="indexed"`) {
		t.Errorf("repoA should be indexed; line: %s", repoALine)
	}

	// Must carry a non-empty signals= attribute pointing at signals.md.
	if !strings.Contains(repoALine, `signals="`) {
		t.Errorf("indexed repoA missing signals= attribute; line: %s", repoALine)
	}
	// Extract the attribute value and verify it names signals.md.
	sigStart := strings.Index(repoALine, `signals="`) + len(`signals="`)
	sigEnd := strings.Index(repoALine[sigStart:], `"`)
	if sigEnd == -1 {
		t.Fatalf("malformed signals= attribute; line: %s", repoALine)
	}
	signalsVal := repoALine[sigStart : sigStart+sigEnd]
	if signalsVal == "" {
		t.Errorf("signals= attribute is empty; line: %s", repoALine)
	}
	if !strings.HasSuffix(signalsVal, "signals.md") {
		t.Errorf("signals= attribute %q should end with signals.md; line: %s", signalsVal, repoALine)
	}
}

// --- ## Members section tests ---

// TestScan_MembersSectionPresent verifies that after a scan, wiki/index.md
// contains a managed ## Members section wrapped in the wiki-member-list XML
// region, with no legacy comment markers.
func TestScan_MembersSectionPresent(t *testing.T) {
	root := t.TempDir()
	repoA := makeGitRepo(t, root, "repoA")
	writeSignals(t, repoA)
	makeGitRepo(t, root, "repoB") // pending

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	content := readIndexMD(t, root)
	if !strings.Contains(content, "<wiki-member-list>") {
		t.Errorf("index.md missing <wiki-member-list> open tag:\n%s", content)
	}
	if !strings.Contains(content, "</wiki-member-list>") {
		t.Errorf("index.md missing </wiki-member-list> close tag:\n%s", content)
	}
	if !strings.Contains(content, "## Members") {
		t.Errorf("index.md missing ## Members heading:\n%s", content)
	}
	if strings.Contains(content, "<!-- wiki-members:start -->") || strings.Contains(content, "<!-- wiki-members:end -->") {
		t.Errorf("index.md must not carry legacy comment markers on a fresh scan:\n%s", content)
	}
}

// TestScan_MembersSectionLinksIndexed verifies that an "indexed" member links to
// ../<repo>/.claude/project/signals.md (relative to index.md which is at wiki/index.md).
func TestScan_MembersSectionLinksIndexed(t *testing.T) {
	root := t.TempDir()
	repoA := makeGitRepo(t, root, "repoA")
	writeSignals(t, repoA)

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	content := readIndexMD(t, root)
	// indexed → link to ../<repo>/.claude/project/signals.md
	// index.md is at wiki/index.md; repoA is at root/repoA → rel = ../repoA/.claude/project/signals.md
	if !strings.Contains(content, "../repoA/.claude/project/signals.md") {
		t.Errorf("indexed repoA missing signals.md link:\n%s", content)
	}
	if !strings.Contains(content, "[repoA]") {
		t.Errorf("indexed repoA missing [repoA] link text:\n%s", content)
	}
}

// TestScan_MembersSectionLinksPending verifies that a "pending" member links to
// ../<repo>/ (relative to index.md).
func TestScan_MembersSectionLinksPending(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoB") // no signals → pending

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	content := readIndexMD(t, root)
	// pending → link to ../<repo>/
	if !strings.Contains(content, "../repoB/") {
		t.Errorf("pending repoB missing ../<repo>/ link:\n%s", content)
	}
	if !strings.Contains(content, "[repoB]") {
		t.Errorf("pending repoB missing [repoB] link text:\n%s", content)
	}
}

// TestScan_MembersSectionLinksSummarized verifies that a "summarized" member links to
// repos/<repo>.md (relative to index.md).
func TestScan_MembersSectionLinksSummarized(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Manually mark repoA as summarized.
	summaryPath := filepath.Join(root, "wiki", "repos", "repoA.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(summaryPath, []byte("# repoA summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := readIndexMD(t, root)
	content = strings.ReplaceAll(content, `status="pending"`, `status="summarized" summary="repos/repoA.md"`)
	if err := os.WriteFile(indexMDPath(root), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	content = readIndexMD(t, root)
	// summarized → link to repos/<repo>.md (relative to wiki/index.md → repos/repoA.md)
	if !strings.Contains(content, "repos/repoA.md") {
		t.Errorf("summarized repoA missing repos/<repo>.md link:\n%s", content)
	}
	if !strings.Contains(content, "[repoA]") {
		t.Errorf("summarized repoA missing [repoA] link text:\n%s", content)
	}
}

// TestScan_MembersSectionIdempotent verifies that re-scanning replaces the managed
// Members section in-place while preserving narrative outside the managed region.
func TestScan_MembersSectionIdempotent(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	after1 := readIndexMD(t, root)

	// Inject narrative outside the managed region.
	narrative := "\n## My realm notes\n\nThis realm contains interesting projects.\n"
	if err := os.WriteFile(indexMDPath(root), []byte(after1+narrative), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	after2 := readIndexMD(t, root)
	// Narrative must be preserved.
	if !strings.Contains(after2, narrative) {
		t.Errorf("narrative lost after re-scan:\n%s", after2)
	}
	// Members region must still be present.
	if !strings.Contains(after2, "<wiki-member-list>") {
		t.Errorf("Members region missing after re-scan:\n%s", after2)
	}
	// Members region must appear exactly once.
	if strings.Count(after2, "<wiki-member-list>") > 1 {
		t.Errorf("Members region duplicated after re-scan:\n%s", after2)
	}
}

// TestScan_MembersSectionNarrativePreservedByExistingTests verifies that the new
// Members managed section does not break the existing narrative preservation test.
// (This documents intent — the existing TestScan_Idempotent_NarrativePreserved
// already covers this, but we re-check the Members-section variant.)
func TestScan_MembersSectionDoesNotBreakNarrativePreservation(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	content := readIndexMD(t, root)
	existingNarrative := "\n## My notes\n\nSome important context about this realm.\n"
	contentWithNarrative := content + existingNarrative
	if err := os.WriteFile(indexMDPath(root), []byte(contentWithNarrative), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	afterRescan := readIndexMD(t, root)
	if !strings.Contains(afterRescan, existingNarrative) {
		t.Errorf("narrative lost after re-scan:\n%s", afterRescan)
	}
}

// --- OKF §6 Members listing description tests ---

// TestDeriveMemberDescription_FrontmatterDescription asserts that a summary file
// with a "description:" frontmatter key returns that value.
func TestDeriveMemberDescription_FrontmatterDescription(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "repos", "repoA.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ndescription: Handles user authentication and token lifecycle\n---\n\n## Overview\n\nSome body text here.\n"
	if err := os.WriteFile(summaryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := wiki.DeriveMemberDescription(summaryPath)
	want := "Handles user authentication and token lifecycle"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestDeriveMemberDescription_FirstBodySentence asserts that when no "description:"
// frontmatter is present, the first non-heading prose sentence is used.
func TestDeriveMemberDescription_FirstBodySentence(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "repos", "repoB.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# repoB\n\n## Overview\n\nManages billing and subscription state for the SaaS platform.\n\n## Another section\n\nMore text.\n"
	if err := os.WriteFile(summaryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := wiki.DeriveMemberDescription(summaryPath)
	want := "Manages billing and subscription state for the SaaS platform."
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestDeriveMemberDescription_NoMatch asserts that a file with no usable
// description returns an empty string (link-only entry is valid per OKF §6).
func TestDeriveMemberDescription_NoMatch(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "repos", "repoC.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Only headings, no prose sentence.
	content := "# repoC\n\n## Overview\n\n## Details\n\n"
	if err := os.WriteFile(summaryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := wiki.DeriveMemberDescription(summaryPath)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestDeriveMemberDescription_MissingFile asserts that an unreadable path
// returns an empty string without panicking.
func TestDeriveMemberDescription_MissingFile(t *testing.T) {
	got := wiki.DeriveMemberDescription("/nonexistent/path/that/does/not/exist.md")
	if got != "" {
		t.Errorf("expected empty string for missing file, got %q", got)
	}
}

// TestDeriveMemberDescription_SingleLine asserts that the returned description
// never contains embedded newlines.
func TestDeriveMemberDescription_SingleLine(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "repos", "repoD.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ndescription: \"Line one\\nLine two\"\n---\n\nBody text.\n"
	if err := os.WriteFile(summaryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := wiki.DeriveMemberDescription(summaryPath)
	if strings.Contains(got, "\n") {
		t.Errorf("description must not contain newlines, got %q", got)
	}
}

// TestDeriveMemberDescription_LengthBound asserts that the returned description
// is at most 120 characters.
func TestDeriveMemberDescription_LengthBound(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "repos", "repoE.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// A very long first sentence.
	longSentence := strings.Repeat("a", 200) + "."
	content := "# repoE\n\n## Overview\n\n" + longSentence + "\n"
	if err := os.WriteFile(summaryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := wiki.DeriveMemberDescription(summaryPath)
	if len(got) > 120 {
		t.Errorf("description must be <= 120 chars, got %d: %q", len(got), got)
	}
}

// --- body-extraction filter tests ---

// TestDeriveMemberDescription_NavLineSkipped asserts that a body whose first
// non-heading line is a nav-bar pattern (Repo: [url] | Signal: [url]) is skipped
// and the result does not contain "](" or " | ".
func TestDeriveMemberDescription_NavLineSkipped(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "repos", "accept.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// First non-heading body line is a nav bar — must be skipped.
	// Second non-heading body line is real prose.
	content := "# accept\n\n" +
		"Repo: [../../repos/accept/](../../repos/accept/) | Signal: [signals](../../repos/accept/.claude/project/signals.md)\n\n" +
		"Handles incoming HTTP requests and route dispatching for the API layer.\n"
	if err := os.WriteFile(summaryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := wiki.DeriveMemberDescription(summaryPath)
	if strings.Contains(got, "](") {
		t.Errorf("result contains markdown link syntax '](': %q", got)
	}
	if strings.Contains(got, " | ") {
		t.Errorf("result contains nav separator ' | ': %q", got)
	}
	// The real prose line should be used instead.
	if !strings.Contains(got, "Handles incoming HTTP requests") {
		t.Errorf("expected prose line to be returned, got: %q", got)
	}
}

// TestDeriveMemberDescription_NavLineOnlyBody asserts that when the only body
// line is a nav pattern and no prose follows, the result is "" (link-only).
func TestDeriveMemberDescription_NavLineOnlyBody(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "repos", "navonly.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# navonly\n\n" +
		"Repo: [../../repos/navonly/](../../repos/navonly/) | Signal: [signals](./signals.md)\n"
	if err := os.WriteFile(summaryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := wiki.DeriveMemberDescription(summaryPath)
	if got != "" {
		t.Errorf("expected empty string (link-only fallback) for nav-only body, got %q", got)
	}
}

// TestDeriveMemberDescription_ProseWithInlineLink asserts that a prose line
// containing a single inline link survives after normalization — the link is
// reduced to its visible text and the result has no "](" or " | ".
func TestDeriveMemberDescription_ProseWithInlineLink(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "repos", "alpha.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# alpha\n\n## Overview\n\nAlpha depends on [Beta](../beta/) for retry orchestration and circuit-breaking.\n"
	if err := os.WriteFile(summaryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := wiki.DeriveMemberDescription(summaryPath)
	// Must not contain raw link syntax.
	if strings.Contains(got, "](") {
		t.Errorf("result contains markdown link syntax '](': %q", got)
	}
	if strings.Contains(got, " | ") {
		t.Errorf("result contains nav separator ' | ': %q", got)
	}
	// Visible link text must be included, URL must not.
	if !strings.Contains(got, "Beta") {
		t.Errorf("expected link visible text 'Beta' in result, got: %q", got)
	}
	if strings.Contains(got, "../beta/") {
		t.Errorf("result must not contain the raw URL '../beta/': %q", got)
	}
	// Must be non-empty (real prose survives).
	if got == "" {
		t.Error("expected non-empty result for prose line with one inline link")
	}
}

// TestDeriveMemberDescription_TableRowSkipped asserts that a body whose first
// non-heading line is a markdown table row (leading |) is skipped.
func TestDeriveMemberDescription_TableRowSkipped(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "repos", "tablerow.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Table row first, then real prose.
	content := "# tablerow\n\n| Col A | Col B |\n|-------|-------|\n| val1  | val2  |\n\nReal prose description here.\n"
	if err := os.WriteFile(summaryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := wiki.DeriveMemberDescription(summaryPath)
	if strings.Contains(got, "|") {
		t.Errorf("result must not contain table-row pipe '|', got: %q", got)
	}
	if !strings.Contains(got, "Real prose description") {
		t.Errorf("expected prose line after table to be returned, got: %q", got)
	}
}

// TestDeriveMemberDescription_ListItemSkipped asserts that a body whose first
// non-heading lines are list items (-, *, +, N.) is skipped.
func TestDeriveMemberDescription_ListItemSkipped(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "repos", "listfirst.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// List items first (all four kinds), then real prose.
	content := "# listfirst\n\n- item one\n* item two\n+ item three\n1. ordered item\n\nActual description sentence.\n"
	if err := os.WriteFile(summaryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := wiki.DeriveMemberDescription(summaryPath)
	// Must not start with a list marker.
	if len(got) > 0 && (got[0] == '-' || got[0] == '*' || got[0] == '+') {
		t.Errorf("result must not be a list item, got: %q", got)
	}
	// Digits-followed-by-dot is also rejected.
	if strings.HasPrefix(got, "1.") {
		t.Errorf("result must not be an ordered list item, got: %q", got)
	}
	if !strings.Contains(got, "Actual description sentence") {
		t.Errorf("expected prose sentence after list items, got: %q", got)
	}
}

// TestScan_IdempotentWithDescriptions verifies that a third Scan on a fixture
// with summarized members that have derivable descriptions produces a byte-identical
// Members section to the second Scan (idempotency extends to description-carrying entries).
func TestScan_IdempotentWithDescriptions(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}

	// First scan — scaffolds wiki/.
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Create a summary file with a description: frontmatter key.
	summaryPath := filepath.Join(root, "wiki", "repos", "repoA.md")
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(summaryPath, []byte("---\ndescription: Manages user sessions and auth tokens\n---\n\n## Overview\n\nBody text.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Mark as summarized in index.md.
	content := readIndexMD(t, root)
	content = strings.ReplaceAll(content, `status="pending"`, `status="summarized" summary="repos/repoA.md"`)
	if err := os.WriteFile(indexMDPath(root), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second scan — builds Members section with descriptions.
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	after2 := readIndexMD(t, root)

	// Third scan — must produce byte-identical Members section.
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("third Scan: %v", err)
	}
	after3 := readIndexMD(t, root)

	if after2 != after3 {
		t.Errorf("third Scan changed the output — not idempotent\nbefore:\n%s\nafter:\n%s", after2, after3)
	}

	// Confirm the description is actually present.
	if !strings.Contains(after3, "Manages user sessions and auth tokens") {
		t.Errorf("expected description in Members section, got:\n%s", after3)
	}
}

// TestBuildMembersSection_OKFListingForm is a table-driven integration test that
// exercises the OKF §6 listing form of buildMembersSection through a full Scan,
// verifying that entries carry " - <description>" when derivable, and link-only
// when no description exists.
func TestBuildMembersSection_OKFListingForm(t *testing.T) {
	root := t.TempDir()

	// repoA: git repo + summary with description: frontmatter
	repoA := makeGitRepo(t, root, "repoA")
	_ = repoA
	summaryA := filepath.Join(root, "wiki", "repos", "repoA.md")

	// repoB: git repo + summary with prose Overview (no frontmatter description)
	repoB := makeGitRepo(t, root, "repoB")
	_ = repoB
	summaryB := filepath.Join(root, "wiki", "repos", "repoB.md")

	// repoC: git repo, no summary (pending) — link-only
	makeGitRepo(t, root, "repoC")

	opts := wiki.Options{Clock: fixedClock()}

	// First scan to scaffold wiki/.
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Create summary files after scaffolding.
	if err := os.MkdirAll(filepath.Dir(summaryA), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(summaryA, []byte("---\ndescription: Auth service — handles tokens and sessions\n---\n\n## Overview\n\nSome body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(summaryB), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(summaryB, []byte("# repoB\n\n## Overview\n\nBilling and subscription management platform.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Mark repos as summarized in index.md.
	content := readIndexMD(t, root)
	content = strings.ReplaceAll(content, `path="repoA" status="pending"`, `path="repoA" status="summarized" summary="repos/repoA.md"`)
	content = strings.ReplaceAll(content, `path="repoB" status="pending"`, `path="repoB" status="summarized" summary="repos/repoB.md"`)
	if err := os.WriteFile(indexMDPath(root), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second scan to regenerate Members section with descriptions.
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	after := readIndexMD(t, root)

	// repoA: frontmatter description present → link + description.
	if !strings.Contains(after, "- [repoA](repos/repoA.md) - Auth service") {
		t.Errorf("repoA Members entry missing description; content:\n%s", after)
	}

	// repoB: prose Overview sentence → link + description.
	if !strings.Contains(after, "- [repoB](repos/repoB.md) - Billing and subscription management platform") {
		t.Errorf("repoB Members entry missing description; content:\n%s", after)
	}

	// repoC: pending, no summary → link-only (no trailing " - ...").
	var repoCLine string
	for _, line := range strings.Split(after, "\n") {
		if strings.Contains(line, "[repoC]") {
			repoCLine = line
			break
		}
	}
	if repoCLine == "" {
		t.Fatalf("repoC Members entry not found; content:\n%s", after)
	}
	if strings.Contains(repoCLine, " - ") {
		t.Errorf("repoC is pending (no summary) — should be link-only, got: %q", repoCLine)
	}
}

// --- dual-layout indexed detection tests ---

// writeWikiIndex creates the docs/wiki/index.md file in dir (new layout).
func writeWikiIndex(t *testing.T, dir string) {
	t.Helper()
	p := filepath.Join(dir, "docs", "wiki", "index.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("# wiki index\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScan_IndexedByNewLayout verifies that a member repo with docs/wiki/index.md
// (new layout) is classified "indexed" even without the old .claude/project/signals.md.
func TestScan_IndexedByNewLayout(t *testing.T) {
	root := t.TempDir()
	repoA := makeGitRepo(t, root, "repoA")
	writeWikiIndex(t, repoA) // new layout only — no old signals.md

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	content := readIndexMD(t, root)
	if !strings.Contains(content, `status="indexed"`) {
		t.Errorf("repoA with docs/wiki/index.md should be indexed; content:\n%s", content)
	}
}

// TestScan_IndexedByNewLayout_LinksToWikiIndex verifies that the Members section
// for a new-layout indexed member links to docs/wiki/index.md.
func TestScan_IndexedByNewLayout_LinksToWikiIndex(t *testing.T) {
	root := t.TempDir()
	repoA := makeGitRepo(t, root, "repoA")
	writeWikiIndex(t, repoA)

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	content := readIndexMD(t, root)
	// Members section for an indexed new-layout member must link to docs/wiki/index.md.
	if !strings.Contains(content, "docs/wiki/index.md") {
		t.Errorf("new-layout indexed repoA should link to docs/wiki/index.md; content:\n%s", content)
	}
}

// TestScan_IndexedByOldLayout_BackCompat verifies that a member repo with only
// .claude/project/signals.md (old layout) is still classified "indexed" — backward compat.
func TestScan_IndexedByOldLayout_BackCompat(t *testing.T) {
	root := t.TempDir()
	repoA := makeGitRepo(t, root, "repoA")
	writeSignals(t, repoA) // old layout only — no docs/wiki/index.md

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	content := readIndexMD(t, root)
	if !strings.Contains(content, `status="indexed"`) {
		t.Errorf("repoA with .claude/project/signals.md should still be indexed; content:\n%s", content)
	}
	// The Members section must still link to the old signals.md path.
	if !strings.Contains(content, ".claude/project/signals.md") {
		t.Errorf("old-layout indexed repoA should still link to .claude/project/signals.md; content:\n%s", content)
	}
}

// TestScan_NewLayoutPreferredOverOld verifies that when a member has BOTH
// docs/wiki/index.md and .claude/project/signals.md, the new layout wins
// and the link points to docs/wiki/index.md.
func TestScan_NewLayoutPreferredOverOld(t *testing.T) {
	root := t.TempDir()
	repoA := makeGitRepo(t, root, "repoA")
	writeWikiIndex(t, repoA) // new layout
	writeSignals(t, repoA)   // old layout also present

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	content := readIndexMD(t, root)
	if !strings.Contains(content, `status="indexed"`) {
		t.Errorf("repoA should be indexed; content:\n%s", content)
	}
	// New layout must win: link to docs/wiki/index.md, not .claude/project/signals.md.
	if !strings.Contains(content, "docs/wiki/index.md") {
		t.Errorf("new-layout should be preferred; expected docs/wiki/index.md link; content:\n%s", content)
	}
}

// --- ## Members legacy-marker migration ---

// TestScan_MigratesLegacyMemberMarkers verifies that a wiki/index.md carrying
// the legacy HTML-comment Members section is rewritten to the
// wiki-member-list XML region on the next Scan: no duplicate "## Members"
// heading, no leftover comment markers, narrative outside the legacy region
// preserved, and the migration is idempotent on a subsequent Scan.
func TestScan_MigratesLegacyMemberMarkers(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	after1 := readIndexMD(t, root)
	idx := strings.Index(after1, "<wiki-member-list>")
	if idx == -1 {
		t.Fatalf("expected <wiki-member-list> region after first scan:\n%s", after1)
	}
	// Everything before the region (the <wiki-scan> block + narrative) stands
	// in for what a pre-migration realm looked like; splice in a legacy
	// comment-delimited Members section plus trailing narrative in its place.
	prefix := strings.TrimRight(after1[:idx], "\n")
	legacyContent := prefix + "\n\n" +
		"## Members\n\n" +
		"<!-- wiki-members:start -->\n" +
		"- [repoA](../repoA/)\n" +
		"<!-- wiki-members:end -->\n\n" +
		"## Trailer\n\nNarrative after the legacy section.\n"
	if err := os.WriteFile(indexMDPath(root), []byte(legacyContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("migration Scan: %v", err)
	}

	migrated := readIndexMD(t, root)
	if strings.Contains(migrated, "<!-- wiki-members:start -->") || strings.Contains(migrated, "<!-- wiki-members:end -->") {
		t.Errorf("legacy comment markers must be gone after migration:\n%s", migrated)
	}
	if !strings.Contains(migrated, "<wiki-member-list>") {
		t.Errorf("migrated index.md missing <wiki-member-list> region:\n%s", migrated)
	}
	if got := strings.Count(migrated, "## Members"); got != 1 {
		t.Errorf("migrated index.md must carry exactly one ## Members heading, got %d:\n%s", got, migrated)
	}
	if !strings.Contains(migrated, "Narrative after the legacy section.") {
		t.Errorf("narrative outside the legacy region must be preserved:\n%s", migrated)
	}

	// Ordering: migration must relocate the region IN PLACE, not excise-then-
	// append. The realm narrative that precedes the legacy block must stay
	// before the migrated region, and the "## Trailer" narrative that
	// followed the legacy block must stay after it — never reordered to
	// precede the migrated region because spliceManagedRegion mistook it for
	// absent and appended a fresh copy at EOF.
	overviewIdx := strings.Index(migrated, "## Realm overview")
	memberIdx := strings.Index(migrated, "<wiki-member-list>")
	trailerIdx := strings.Index(migrated, "## Trailer")
	if overviewIdx == -1 || memberIdx == -1 || trailerIdx == -1 {
		t.Fatalf("expected all three markers present: overview=%d member=%d trailer=%d\n%s", overviewIdx, memberIdx, trailerIdx, migrated)
	}
	if overviewIdx > memberIdx {
		t.Errorf("pre-region narrative must stay before the migrated region: overview at %d, region at %d:\n%s", overviewIdx, memberIdx, migrated)
	}
	if memberIdx > trailerIdx {
		t.Errorf("migrated region must stay in place, before the post-region narrative — got reordered: region at %d, trailer at %d:\n%s", memberIdx, trailerIdx, migrated)
	}

	// Idempotent: a second Scan with no member-set change must not re-trigger
	// the migration path (no legacy markers left to find) and must produce a
	// byte-identical file.
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	afterSecond := readIndexMD(t, root)
	if afterSecond != migrated {
		t.Errorf("migration must be idempotent — second Scan changed the file\nfirst:\n%s\nsecond:\n%s", migrated, afterSecond)
	}
}

// TestScan_MigratesLegacyMemberMarkers_BlankLineBoundary asserts the exact
// boundary bytes on both sides of the migrated <wiki-member-list> region —
// not just index ordering. The legacy "## Members" heading follows the
// preceding narrative with a SINGLE newline (no blank line) — the L2
// leading-boundary sub-case: spliceRegionAt must pad it to a blank line, not
// merely preserve whatever whitespace it finds. A missing blank line on the
// trailing side lets "## Trailer" get absorbed into the preceding HTML block
// under CommonMark, rendering as literal text instead of a heading; this
// must never regress either.
func TestScan_MigratesLegacyMemberMarkers_BlankLineBoundary(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	after1 := readIndexMD(t, root)
	idx := strings.Index(after1, "<wiki-member-list>")
	if idx == -1 {
		t.Fatalf("expected <wiki-member-list> region after first scan:\n%s", after1)
	}
	// Single trailing newline — no blank line — before the heading.
	prefix := strings.TrimRight(after1[:idx], "\n")
	legacyContent := prefix + "\n" +
		"## Members\n\n" +
		"<!-- wiki-members:start -->\n" +
		"- [repoA](../repoA/)\n" +
		"<!-- wiki-members:end -->\n\n" +
		"## Trailer\n\nNarrative after the legacy section.\n"
	if err := os.WriteFile(indexMDPath(root), []byte(legacyContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("migration Scan: %v", err)
	}

	migrated := readIndexMD(t, root)
	if !strings.Contains(migrated, "\n\n<wiki-member-list>") {
		t.Errorf("expected a padded blank-line boundary before <wiki-member-list> (single \\n input must become \\n\\n):\n%s", migrated)
	}
	if !strings.Contains(migrated, "</wiki-member-list>\n\n## Trailer") {
		t.Errorf("expected a blank-line boundary after </wiki-member-list> — \"## Trailer\" would otherwise render as literal text, not a heading:\n%s", migrated)
	}
}

// TestScan_MigratesLegacyMemberMarkers_NoHeadingLeadingBoundary covers the
// same L2 leading-boundary padding when NO "## Members" heading precedes the
// legacy comment markers (bare markers only) — the adjacency-bounded heading
// lookup in migrateLegacyMemberMarkers must not be the only path that
// exercises spliceRegionAt's LEAD padding.
func TestScan_MigratesLegacyMemberMarkers_NoHeadingLeadingBoundary(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	after1 := readIndexMD(t, root)
	idx := strings.Index(after1, "<wiki-member-list>")
	if idx == -1 {
		t.Fatalf("expected <wiki-member-list> region after first scan:\n%s", after1)
	}
	prefix := strings.TrimRight(after1[:idx], "\n")
	legacyContent := prefix + "\n" +
		"<!-- wiki-members:start -->\n" +
		"- [repoA](../repoA/)\n" +
		"<!-- wiki-members:end -->\n\n" +
		"## Trailer\n\nNarrative after the legacy section.\n"
	if err := os.WriteFile(indexMDPath(root), []byte(legacyContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("migration Scan: %v", err)
	}

	migrated := readIndexMD(t, root)
	if !strings.Contains(migrated, "\n\n<wiki-member-list>") {
		t.Errorf("expected a padded blank-line boundary before <wiki-member-list> with no heading present:\n%s", migrated)
	}
	if got := strings.Count(migrated, "## Members"); got != 1 {
		t.Errorf("migrated index.md must carry exactly one ## Members heading (the region's own), got %d:\n%s", got, migrated)
	}
}

// TestScan_MigratesLegacyMemberMarkers_EOFBoundary verifies that when the
// legacy Members section is the very last thing in the file, migration ends
// the document with exactly one trailing newline — never no-newline-at-EOF,
// never a trailing blank line.
func TestScan_MigratesLegacyMemberMarkers_EOFBoundary(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	after1 := readIndexMD(t, root)
	idx := strings.Index(after1, "<wiki-member-list>")
	if idx == -1 {
		t.Fatalf("expected <wiki-member-list> region after first scan:\n%s", after1)
	}
	prefix := strings.TrimRight(after1[:idx], "\n")
	legacyContent := prefix + "\n\n" +
		"## Members\n\n" +
		"<!-- wiki-members:start -->\n" +
		"- [repoA](../repoA/)\n" +
		"<!-- wiki-members:end -->\n"
	if err := os.WriteFile(indexMDPath(root), []byte(legacyContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("migration Scan: %v", err)
	}

	migrated := readIndexMD(t, root)
	if !strings.HasSuffix(migrated, "\n") {
		t.Fatalf("expected file to end in a newline:\n%q", migrated)
	}
	if strings.HasSuffix(migrated, "\n\n") {
		t.Errorf("expected exactly one trailing newline at EOF, not a trailing blank line:\n%q", migrated)
	}
	lastTagIdx := strings.LastIndex(migrated, "</wiki-member-list>")
	if lastTagIdx == -1 {
		t.Fatalf("missing </wiki-member-list> close tag:\n%s", migrated)
	}
	if got := migrated[lastTagIdx+len("</wiki-member-list>"):]; got != "\n" {
		t.Errorf("expected exactly \"\\n\" after the close tag at EOF, got %q", got)
	}
}

// TestScan_MigratesLegacyMemberMarkers_ProseBetweenHeadingAndMarkerSurvives
// verifies the H2 adjacency guard: when a user typed prose between a
// "## Members" heading and the legacy start marker (non-adjacent), that
// prose — and the heading — must survive migration untouched. Absorbing a
// non-adjacent heading into the relocated span would silently delete user
// content; a transient duplicate "## Members" heading is the accepted
// tradeoff instead.
func TestScan_MigratesLegacyMemberMarkers_ProseBetweenHeadingAndMarkerSurvives(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	after1 := readIndexMD(t, root)
	idx := strings.Index(after1, "<wiki-member-list>")
	if idx == -1 {
		t.Fatalf("expected <wiki-member-list> region after first scan:\n%s", after1)
	}
	prefix := strings.TrimRight(after1[:idx], "\n")
	legacyContent := prefix + "\n\n" +
		"## Members\n\n" +
		"Some hand-written prose the user typed between the heading and the legacy markers.\n\n" +
		"<!-- wiki-members:start -->\n" +
		"- [repoA](../repoA/)\n" +
		"<!-- wiki-members:end -->\n\n" +
		"## Trailer\n\nNarrative after the legacy section.\n"
	if err := os.WriteFile(indexMDPath(root), []byte(legacyContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("migration Scan: %v", err)
	}

	migrated := readIndexMD(t, root)
	if !strings.Contains(migrated, "Some hand-written prose the user typed between the heading and the legacy markers.") {
		t.Errorf("prose between the heading and the legacy marker must survive migration:\n%s", migrated)
	}
	if !strings.Contains(migrated, "## Members\n\nSome hand-written prose") {
		t.Errorf("the non-adjacent heading must stay in place, immediately followed by the user's prose:\n%s", migrated)
	}
}

// TestScan_UnpairedLegacyStartMarkerSkipsMembersWrite verifies that a legacy
// start marker with no matching end marker leaves the file byte-for-byte
// untouched (no half-migration, no fresh region appended at EOF) and the
// members write is skipped this scan.
func TestScan_UnpairedLegacyStartMarkerSkipsMembersWrite(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	after1 := readIndexMD(t, root)
	idx := strings.Index(after1, "<wiki-member-list>")
	if idx == -1 {
		t.Fatalf("expected <wiki-member-list> region after first scan:\n%s", after1)
	}
	prefix := strings.TrimRight(after1[:idx], "\n")
	unpaired := prefix + "\n\n" +
		"## Members\n\n" +
		"<!-- wiki-members:start -->\n" +
		"- [repoA](../repoA/)\n"
	if err := os.WriteFile(indexMDPath(root), []byte(unpaired), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan with unpaired legacy marker: %v", err)
	}

	afterScan := readIndexMD(t, root)
	if afterScan != unpaired {
		t.Errorf("file must be left byte-for-byte untouched when a stray unpaired legacy marker is present\ngot:  %q\nwant: %q", afterScan, unpaired)
	}
}

// TestScan_UnpairedLegacyEndMarkerSkipsMembersWrite mirrors the P1 case for
// the opposite unpaired shape: a legacy end marker with no matching start.
func TestScan_UnpairedLegacyEndMarkerSkipsMembersWrite(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	after1 := readIndexMD(t, root)
	idx := strings.Index(after1, "<wiki-member-list>")
	if idx == -1 {
		t.Fatalf("expected <wiki-member-list> region after first scan:\n%s", after1)
	}
	prefix := strings.TrimRight(after1[:idx], "\n")
	unpaired := prefix + "\n\n" +
		"## Members\n\n" +
		"- [repoA](../repoA/)\n" +
		"<!-- wiki-members:end -->\n"
	if err := os.WriteFile(indexMDPath(root), []byte(unpaired), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan with unpaired legacy marker: %v", err)
	}

	afterScan := readIndexMD(t, root)
	if afterScan != unpaired {
		t.Errorf("file must be left byte-for-byte untouched when a stray unpaired legacy marker is present\ngot:  %q\nwant: %q", afterScan, unpaired)
	}
}

// TestScan_PreexistingRegionWithStrayLegacyMarkersNoDuplicate verifies the
// P3 guard: a document that already has a well-formed <wiki-member-list>
// region (migration already ran) plus stray leftover legacy comment markers
// elsewhere must not gain a second region — migration is one-shot.
func TestScan_PreexistingRegionWithStrayLegacyMarkersNoDuplicate(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	after1 := readIndexMD(t, root)
	if !strings.Contains(after1, "<wiki-member-list>") {
		t.Fatalf("expected <wiki-member-list> region after first scan:\n%s", after1)
	}
	// Simulate a user pasting stray, already-superseded legacy markers back
	// in after a well-formed region already exists.
	stray := after1 + "\n\n<!-- wiki-members:start -->\n- stray\n<!-- wiki-members:end -->\n"
	if err := os.WriteFile(indexMDPath(root), []byte(stray), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("Scan with pre-existing region + stray legacy markers: %v", err)
	}

	afterScan := readIndexMD(t, root)
	if got := strings.Count(afterScan, "<wiki-member-list>"); got != 1 {
		t.Errorf("expected exactly one <wiki-member-list> region, got %d:\n%s", got, afterScan)
	}
}

// TestScan_MigratesLegacyMemberMarkers_StartOfFileNoLeadingArtifact verifies
// the L0 start-of-file LEAD case: when the legacy span sits at the very
// start of the document (nothing precedes it), migration must not introduce
// a leading blank-line artifact.
func TestScan_MigratesLegacyMemberMarkers_StartOfFileNoLeadingArtifact(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	opts := wiki.Options{Clock: fixedClock()}
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}
	after1 := readIndexMD(t, root)
	idx := strings.Index(after1, "<wiki-member-list>")
	if idx == -1 {
		t.Fatalf("expected <wiki-member-list> region after first scan:\n%s", after1)
	}
	// tail is the <wiki-scan> block + narrative WITHOUT any wiki-member-list
	// region — using the full after1 here would trip the P3 guard (a
	// well-formed region already present) before the LEAD case under test
	// ever gets exercised.
	tail := after1[:idx]

	// Put the legacy markers as the very first bytes of the file, followed
	// by the rest of the already-scanned content (including its own
	// <wiki-scan> block) — so the next Scan's writeWikiScanBlock replaces
	// that block in place rather than appending a second one, and the
	// legacy span stays genuinely at start-of-file.
	legacyContent := "<!-- wiki-members:start -->\n" +
		"- [repoA](../repoA/)\n" +
		"<!-- wiki-members:end -->\n\n" +
		tail
	if err := os.WriteFile(indexMDPath(root), []byte(legacyContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("migration Scan: %v", err)
	}

	migrated := readIndexMD(t, root)
	if !strings.HasPrefix(migrated, "<wiki-member-list>") {
		t.Errorf("region at start-of-file must have no leading blank-line artifact:\n%q", migrated)
	}
}

// --- Scan wiring: bucket index rebuild ---

// TestScan_RebuildsBucketIndexesAndSurvivesBrokenBucket verifies that Scan
// rebuilds both the realm <wiki-bucket-list> region and each registered
// bucket's own <bucket-docs> region, and that an unpaired region in one
// bucket does not fail the scan or block a sibling bucket's rebuild.
func TestScan_RebuildsBucketIndexesAndSurvivesBrokenBucket(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root, "repoA")

	researchDir := filepath.Join(root, "research")
	if err := os.MkdirAll(researchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(researchDir, "index.md"), []byte("---\ndescription: Research capture bucket\n---\n\n## Conventions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(researchDir, "seo.md"), []byte("---\ntitle: SEO\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	brokenDir := filepath.Join(root, "broken")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	brokenOriginal := "# broken\n\n<bucket-docs>\n\nstray content, no close tag\n"
	if err := os.WriteFile(filepath.Join(brokenDir, "index.md"), []byte(brokenOriginal), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := wiki.Options{Clock: fixedClock()}

	// First scan scaffolds wiki/.
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Register both buckets by hand via the same <wiki-buckets> block shape
	// the registry writes — the external test package has no access to the
	// unexported bucket-registry helpers.
	content := readIndexMD(t, root)
	content += "\n<wiki-buckets>\n" +
		fmt.Sprintf("<bucket name=%q path=%q/>\n", "research", researchDir) +
		fmt.Sprintf("<bucket name=%q path=%q/>\n", "broken", brokenDir) +
		"</wiki-buckets>\n"
	if err := os.WriteFile(indexMDPath(root), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second scan must rebuild both index levels; the broken bucket's
	// unpaired region must not fail the scan.
	if _, err := wiki.Scan(root, opts); err != nil {
		t.Fatalf("second Scan must not fail on a broken bucket region: %v", err)
	}

	after := readIndexMD(t, root)
	if !strings.Contains(after, "<wiki-member-list>") {
		t.Errorf("wiki/index.md missing <wiki-member-list> region:\n%s", after)
	}
	if !strings.Contains(after, "<wiki-bucket-list>") {
		t.Errorf("wiki/index.md missing <wiki-bucket-list> region:\n%s", after)
	}
	if !strings.Contains(after, "- [research](../research) - Research capture bucket") {
		t.Errorf("realm bucket list missing research entry:\n%s", after)
	}

	researchIndex, err := os.ReadFile(filepath.Join(researchDir, "index.md"))
	if err != nil {
		t.Fatalf("read research index.md: %v", err)
	}
	if !strings.Contains(string(researchIndex), "<bucket-docs>") || !strings.Contains(string(researchIndex), "[SEO](seo.md)") {
		t.Errorf("research bucket-docs region not rebuilt:\n%s", researchIndex)
	}

	brokenAfter, err := os.ReadFile(filepath.Join(brokenDir, "index.md"))
	if err != nil {
		t.Fatalf("read broken index.md: %v", err)
	}
	if string(brokenAfter) != brokenOriginal {
		t.Errorf("broken bucket index.md must be left untouched by the unpaired region\ngot:  %q\nwant: %q", brokenAfter, brokenOriginal)
	}
}
