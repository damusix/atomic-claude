package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitCmd runs git in dir with a synthetic identity, never touching the real
// $HOME's global gitconfig for author info.
func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// mustResolve gives path's symlink-free absolute form for test expectations —
// t.TempDir() on macOS sits under /var, itself a symlink to /private/var, so
// a literal string comparison against an unresolved fixture path would fail
// once mainCheckoutRoot resolves symlinks.
func mustResolve(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", path, err)
	}
	return real
}

// setupMainCheckout creates a fixture main checkout: <root>/main/.git/ as a
// real directory, with a worktrees/<name>/ subdirectory the way git itself
// creates one on `git worktree add`.
func setupMainCheckout(t *testing.T, root, worktreeName string) string {
	t.Helper()
	main := filepath.Join(root, "main")
	if err := os.MkdirAll(filepath.Join(main, ".git", "worktrees", worktreeName), 0o755); err != nil {
		t.Fatalf("seed main checkout: %v", err)
	}
	return main
}

// setupWorktree creates a fixture worktree checkout of mainDir: a directory
// whose `.git` is a *file* containing a `gitdir:` line pointing into
// mainDir's `.git/worktrees/<name>` — the shape git actually produces.
func setupWorktree(t *testing.T, root, mainDir, worktreeName string, gitdirLine string) string {
	t.Helper()
	wt := filepath.Join(root, worktreeName)
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("seed worktree dir: %v", err)
	}
	content := "gitdir: " + gitdirLine + "\n"
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte(content), 0o644); err != nil {
		t.Fatalf("seed worktree .git file: %v", err)
	}
	return wt
}

func TestMainCheckoutRoot_MainCheckoutDirectory(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")

	got := mainCheckoutRoot(main)
	if want := mustResolve(t, main); got != want {
		t.Errorf("mainCheckoutRoot(main checkout) = %q, want %q", got, want)
	}
}

// The central case: a main checkout and a `.git`-file worktree of the same
// clone must resolve to the same main checkout root.
func TestMainCheckoutRoot_WorktreeAgreesWithMainCheckout(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	absGitdir := filepath.Join(main, ".git", "worktrees", "wt1")
	wt := setupWorktree(t, root, main, "wt1", absGitdir)

	gotMain := mainCheckoutRoot(main)
	gotWt := mainCheckoutRoot(wt)
	want := mustResolve(t, main)

	if gotMain != want {
		t.Errorf("mainCheckoutRoot(main) = %q, want %q", gotMain, want)
	}
	if gotWt != want {
		t.Errorf("mainCheckoutRoot(worktree) = %q, want %q (main checkout)", gotWt, want)
	}
	if gotMain != gotWt {
		t.Errorf("main checkout and worktree disagree: %q vs %q", gotMain, gotWt)
	}
}

// A relative `gitdir:` line — the shape a submodule's `.git` file carries —
// resolves relative to the worktree root exactly as written.
func TestMainCheckoutRoot_RelativeGitdirLine(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt2")
	// worktree dir is a sibling of main under root, so "../main/.git/worktrees/wt2"
	// resolves relative to the worktree's own directory.
	wt := setupWorktree(t, root, main, "wt2", filepath.Join("..", "main", ".git", "worktrees", "wt2"))

	got := mainCheckoutRoot(wt)
	if want := mustResolve(t, main); got != want {
		t.Errorf("mainCheckoutRoot(relative gitdir worktree) = %q, want %q", got, want)
	}
}

// repo_root can be a scope="repo" marker directory holding no .git of its
// own — the walk must not assume .git sits exactly at repo_root, and must
// keep walking upward until it finds the clone's actual .git.
func TestMainCheckoutRoot_MarkerRootedAboveGit(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	markerRoot := filepath.Join(main, "subdir", "marker-root")
	if err := os.MkdirAll(markerRoot, 0o755); err != nil {
		t.Fatalf("seed marker root: %v", err)
	}

	got := mainCheckoutRoot(markerRoot)
	if want := mustResolve(t, main); got != want {
		t.Errorf("mainCheckoutRoot(marker root below main .git) = %q, want %q", got, want)
	}
}

// Reaching the filesystem root without finding a .git falls back to repo_root
// as-is, rather than erroring or returning the filesystem root.
func TestMainCheckoutRoot_NoGitFallsBackToRepoRoot(t *testing.T) {
	root := t.TempDir()
	orphan := filepath.Join(root, "no-git-anywhere")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatalf("seed orphan dir: %v", err)
	}

	got := mainCheckoutRoot(orphan)
	if want := mustResolve(t, orphan); got != want {
		t.Errorf("mainCheckoutRoot(no .git found) = %q, want fallback %q", got, want)
	}
}

// Every worktree of one clone shares one project-key, and therefore the same
// project-keyed state directories.
func TestProjectStateDirs_SharedAcrossWorktrees(t *testing.T) {
	home := t.TempDir()
	defer SetHomeDirForTest(home)()

	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	absGitdir := filepath.Join(main, ".git", "worktrees", "wt1")
	wt := setupWorktree(t, root, main, "wt1", absGitdir)

	if got, want := ProjectStateDir(main), ProjectStateDir(wt); got != want {
		t.Errorf("ProjectStateDir(main) = %q, ProjectStateDir(worktree) = %q, want equal", got, want)
	}
	if got, want := ReportsRoot(main), ReportsRoot(wt); got != want {
		t.Errorf("ReportsRoot(main) = %q, ReportsRoot(worktree) = %q, want equal", got, want)
	}
	if got, want := ProjectRemindersDir(main), ProjectRemindersDir(wt); got != want {
		t.Errorf("ProjectRemindersDir(main) = %q, ProjectRemindersDir(worktree) = %q, want equal", got, want)
	}
	if got, want := ArchiveDir(main, "my-slug", "2026-08-20"), ArchiveDir(wt, "my-slug", "2026-08-20"); got != want {
		t.Errorf("ArchiveDir(main) = %q, ArchiveDir(worktree) = %q, want equal", got, want)
	}

	wantPrefix := filepath.Join(Dir(home), projectKey(main))
	if got := ProjectStateDir(main); got != wantPrefix {
		t.Errorf("ProjectStateDir(main) = %q, want %q", got, wantPrefix)
	}
}

func TestRemindersDir_DelegatesToProjectKeyed(t *testing.T) {
	home := t.TempDir()
	defer SetHomeDirForTest(home)()

	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")

	if got, want := RemindersDir(main), ProjectRemindersDir(main); got != want {
		t.Errorf("RemindersDir(main) = %q, want delegation to ProjectRemindersDir = %q", got, want)
	}
}

func TestRemindersDirLegacy_ResolvesPreRelocationPath(t *testing.T) {
	root := t.TempDir()
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	got := RemindersDirLegacy(root)
	want := filepath.Join(root, ".claude", ".scratchpad", "reminders")
	if got != want {
		t.Errorf("RemindersDirLegacy(root) = %q, want %q", got, want)
	}
}

// ReportsDir falls back to the legacy branch directory only when the
// project-keyed one has no matching report; ReportsRoot always names the
// project-keyed parent regardless.
func TestReportsDir_FallsBackOnlyWhenProjectKeyedEmpty(t *testing.T) {
	home := t.TempDir()
	defer SetHomeDirForTest(home)()
	restoreHarness := SetHarnessDirForTest(".claude")
	defer restoreHarness()

	root := t.TempDir()
	branch := "feature/x"

	// Nothing exists anywhere yet: the new home wins, because this is also
	// where a fresh report is written.
	keyedDefault := filepath.Join(ReportsRoot(root), branchSegment(branch))
	if got := ReportsDir(root, branch); got != keyedDefault {
		t.Errorf("ReportsDir with nothing present = %q, want project-keyed default %q", got, keyedDefault)
	}

	// A pre-migration report in legacy and nothing in the new home: legacy
	// stays readable until migrate moves it.
	legacy := ReportsDirLegacy(root, branch)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "old.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReportsDir(root, branch); got != legacy {
		t.Errorf("ReportsDir with only a legacy report = %q, want legacy %q", got, legacy)
	}

	// A report lands in the project-keyed dir: ReportsDir now prefers it.
	keyed := filepath.Join(ReportsRoot(root), branchSegment(branch))
	if err := os.MkdirAll(keyed, 0o755); err != nil {
		t.Fatalf("seed project-keyed reports dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keyed, "report.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed report file: %v", err)
	}

	// Both present: the new home wins, so a migrated report is never shadowed
	// by a stale legacy copy.
	if got := ReportsDir(root, branch); got != keyed {
		t.Errorf("ReportsDir with both present = %q, want project-keyed %q", got, keyed)
	}

	if root2 := ReportsRoot(root); root2 != filepath.Join(ProjectStateDir(root), "reports") {
		t.Errorf("ReportsRoot(root) = %q, unexpected shape", root2)
	}
}

// A `.git` file with no `gitdir:` line (malformed) degrades to repoRoot as-is
// rather than crashing or resolving to a bogus location.
func TestMainCheckoutRoot_MalformedGitFileFallsBackToRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("not a gitdir line at all\n"), 0o644); err != nil {
		t.Fatalf("seed malformed .git file: %v", err)
	}

	got := mainCheckoutRoot(root)
	if want := mustResolve(t, root); got != want {
		t.Errorf("mainCheckoutRoot(malformed .git file) = %q, want fallback %q", got, want)
	}
}

// An empty `.git` file degrades to repoRoot the same way a malformed one does.
func TestMainCheckoutRoot_EmptyGitFileFallsBackToRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte(""), 0o644); err != nil {
		t.Fatalf("seed empty .git file: %v", err)
	}

	got := mainCheckoutRoot(root)
	if want := mustResolve(t, root); got != want {
		t.Errorf("mainCheckoutRoot(empty .git file) = %q, want fallback %q", got, want)
	}
}

// A stray dotfile in the project-keyed branch reports directory must not
// suppress the legacy fallback — dirHasReport has to match on what a report
// actually is (a .md file), not on any directory entry.
// A stray dotfile in the project-keyed directory is not a report. With a
// real pre-migration report in legacy, the dotfile must not make the new
// directory look populated and shadow the report a reader actually needs.
func TestReportsDir_StrayDotfileIsNotAReport(t *testing.T) {
	home := t.TempDir()
	defer SetHomeDirForTest(home)()
	restoreHarness := SetHarnessDirForTest(".claude")
	defer restoreHarness()

	root := t.TempDir()
	branch := "feature/y"

	keyed := filepath.Join(ReportsRoot(root), branchSegment(branch))
	if err := os.MkdirAll(keyed, 0o755); err != nil {
		t.Fatalf("seed project-keyed reports dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keyed, ".DS_Store"), []byte("junk"), 0o644); err != nil {
		t.Fatalf("seed stray dotfile: %v", err)
	}
	legacy := ReportsDirLegacy(root, branch)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "old.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := ReportsDir(root, branch); got != legacy {
		t.Errorf("ReportsDir with a dotfile in keyed and a report in legacy = %q, want legacy %q", got, legacy)
	}
}

// The whole resolution chain must spawn no git subprocess: proven by running
// it with a PATH containing no git binary at all.
func TestMainCheckoutRoot_NoGitSubprocessSpawned(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH in this environment; nothing to prove by removing it")
	}

	emptyPathDir := t.TempDir()
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", emptyPathDir); err != nil {
		t.Fatalf("set empty PATH: %v", err)
	}
	defer os.Setenv("PATH", oldPath)

	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	absGitdir := filepath.Join(main, ".git", "worktrees", "wt1")
	wt := setupWorktree(t, root, main, "wt1", absGitdir)

	if got, want := mainCheckoutRoot(wt), mustResolve(t, main); got != want {
		t.Errorf("mainCheckoutRoot resolved with no git on PATH = %q, want %q", got, want)
	}
}

// setupHEAD writes <gitdir>/HEAD, where gitdir is a real directory (a main
// checkout's .git, or the worktrees/<name> dir a real git worktree uses).
func setupHEAD(t *testing.T, gitdir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(gitdir, "HEAD"), []byte(content), 0o644); err != nil {
		t.Fatalf("seed HEAD: %v", err)
	}
}

func TestBranchFromHEAD_MainCheckoutRefBranch(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	setupHEAD(t, filepath.Join(main, ".git"), "ref: refs/heads/main\n")

	got, ok := BranchFromHEAD(main)
	if !ok {
		t.Fatalf("BranchFromHEAD(main) ok = false, want true")
	}
	if got != "main" {
		t.Errorf("BranchFromHEAD(main) = %q, want %q", got, "main")
	}
}

func TestBranchFromHEAD_WorktreeReadsOwnHEAD(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	absGitdir := filepath.Join(main, ".git", "worktrees", "wt1")
	setupHEAD(t, absGitdir, "ref: refs/heads/feature/plans-page\n")
	// Main checkout stays on a different branch than the worktree.
	setupHEAD(t, filepath.Join(main, ".git"), "ref: refs/heads/main\n")
	wt := setupWorktree(t, root, main, "wt1", absGitdir)

	got, ok := BranchFromHEAD(wt)
	if !ok {
		t.Fatalf("BranchFromHEAD(worktree) ok = false, want true")
	}
	if got != "feature/plans-page" {
		t.Errorf("BranchFromHEAD(worktree) = %q, want %q", got, "feature/plans-page")
	}
}

func TestBranchFromHEAD_DetachedHEADFallsBackToShortSHA(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	setupHEAD(t, filepath.Join(main, ".git"), "1234567890abcdef1234567890abcdef12345678\n")

	got, ok := BranchFromHEAD(main)
	if !ok {
		t.Fatalf("BranchFromHEAD(detached) ok = false, want true")
	}
	if got != "1234567" {
		t.Errorf("BranchFromHEAD(detached) = %q, want short SHA %q", got, "1234567")
	}
}

func TestBranchFromHEAD_NoGitEntry(t *testing.T) {
	root := t.TempDir()

	if _, ok := BranchFromHEAD(root); ok {
		t.Errorf("BranchFromHEAD(no .git) ok = true, want false")
	}
}

// A crafted HEAD carrying a path-traversal payload must never surface as a
// branch label — the value never becomes a path segment at all.
func TestBranchFromHEAD_PathTraversalHEADRejected(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	setupHEAD(t, filepath.Join(main, ".git"), "../../../../../../etc/evil\n")

	got, ok := BranchFromHEAD(main)
	if ok {
		t.Errorf("BranchFromHEAD(traversal payload) ok = true, got %q, want false", got)
	}
}

// A ref outside refs/heads/ (a tag) is not a branch and must not surface a
// truncated prefix of the raw ref line.
func TestBranchFromHEAD_TagRefReportsNoBranch(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	setupHEAD(t, filepath.Join(main, ".git"), "ref: refs/tags/v1.0\n")

	got, ok := BranchFromHEAD(main)
	if ok {
		t.Errorf("BranchFromHEAD(tag ref) ok = true, got %q, want false", got)
	}
}

// A traversal payload disguised inside a well-shaped ref line (refs/heads/)
// must also be rejected — the shape check alone (the "ref: refs/heads/"
// prefix) is not sufficient; the name after it needs its own validation.
func TestBranchFromHEAD_TraversalInsideRefsHeadsRejected(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	setupHEAD(t, filepath.Join(main, ".git"), "ref: refs/heads/../../../../../../etc/evil\n")

	got, ok := BranchFromHEAD(main)
	if ok {
		t.Errorf("BranchFromHEAD(traversal inside refs/heads/) ok = true, got %q, want false", got)
	}
}

// ReportsDir (project-keyed) and ReportsDirLegacy must agree on how a branch
// containing "/" flattens to a single path component — one nesting while the
// other stays flat would give /git-cleanup two shapes to sweep.
func TestReportsDir_SlashBranchAgreesWithLegacyFlattening(t *testing.T) {
	home := t.TempDir()
	defer SetHomeDirForTest(home)()
	root := t.TempDir()
	restoreHarness := SetHarnessDirForTest(".claude")
	defer restoreHarness()

	branch := "feature/plans-page"
	legacy := ReportsDirLegacy(root, branch)
	if filepath.Base(legacy) != "feature-plans-page" {
		t.Fatalf("legacy flattening sanity check failed: base = %q", filepath.Base(legacy))
	}

	// With nothing present, ReportsDir falls back to legacy — compare the
	// project-keyed shape directly by seeding a report there instead.
	keyed := filepath.Join(ReportsRoot(root), "feature-plans-page")
	if err := os.MkdirAll(keyed, 0o755); err != nil {
		t.Fatalf("seed project-keyed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keyed, "r.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}

	got := ReportsDir(root, branch)
	if got != keyed {
		t.Errorf("ReportsDir(%q) = %q, want flattened %q (matching legacy's flattening)", branch, got, keyed)
	}
}

// The central symlink case: a main checkout and a REAL `git worktree add`
// worktree of it, placed under a symlinked ancestor (t.TempDir() on macOS
// sits under /var/folders, itself reached through the /var -> /private/var
// symlink), must resolve to the identical main-checkout root. The synthetic
// fixtures elsewhere in this file cannot catch this: git itself writes an
// already-realpath-resolved absolute `gitdir:` target when creating a real
// worktree, which is exactly the asymmetry this test exists to catch against
// the main-checkout branch's unresolved return.
func TestMainCheckoutRoot_RealGitWorktreeUnderSymlinkedPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	main := filepath.Join(root, "main")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatalf("mkdir main: %v", err)
	}
	gitCmd(t, main, "init", "-b", "main")
	gitCmd(t, main, "commit", "--allow-empty", "-m", "init")

	wt := filepath.Join(root, "wt1")
	gitCmd(t, main, "worktree", "add", wt, "-b", "feature-x")

	gotMain := mainCheckoutRoot(main)
	gotWt := mainCheckoutRoot(wt)

	if gotMain != gotWt {
		t.Errorf("main checkout and real git worktree disagree: main=%q worktree=%q", gotMain, gotWt)
	}
}

// Git forbids a ref component starting with "." — a lone "." collapses via
// filepath.Join+Clean to the parent directory itself, so branchFromHEAD must
// reject it rather than surface a garbled branch label.
func TestBranchFromHEAD_LoneDotComponentRejected(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	setupHEAD(t, filepath.Join(main, ".git"), "ref: refs/heads/.\n")

	got, ok := BranchFromHEAD(main)
	if ok {
		t.Errorf("BranchFromHEAD(lone dot) ok = true, got %q, want false", got)
	}
}

// Probing the same allow-list gap on its neighbours: a component that merely
// starts with a dot, and a "." component embedded mid-ref rather than as the
// whole name.
func TestBranchFromHEAD_LeadingDotComponentRejected(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	setupHEAD(t, filepath.Join(main, ".git"), "ref: refs/heads/.hidden\n")

	got, ok := BranchFromHEAD(main)
	if ok {
		t.Errorf("BranchFromHEAD(leading-dot component) ok = true, got %q, want false", got)
	}
}

func TestBranchFromHEAD_EmbeddedDotComponentRejected(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	setupHEAD(t, filepath.Join(main, ".git"), "ref: refs/heads/./foo\n")

	got, ok := BranchFromHEAD(main)
	if ok {
		t.Errorf("BranchFromHEAD(embedded dot component) ok = true, got %q, want false", got)
	}
}

// A trailing-dot component (e.g. "foo.") is a separate allow-list gap real
// git also forbids ("refs cannot end with a dot"); this checkpoint's fix
// targets a leading dot only, so this test pins today's actual (unfixed)
// behavior rather than asserting a fix that wasn't requested.
func TestBranchFromHEAD_TrailingDotComponentStillAccepted(t *testing.T) {
	root := t.TempDir()
	main := setupMainCheckout(t, root, "wt1")
	setupHEAD(t, filepath.Join(main, ".git"), "ref: refs/heads/foo.\n")

	got, ok := BranchFromHEAD(main)
	if !ok || got != "foo." {
		t.Errorf("BranchFromHEAD(trailing dot) = (%q, %v), want (\"foo.\", true)", got, ok)
	}
}
