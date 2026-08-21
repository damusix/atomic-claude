package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/where"
)

// mkGitMarker seeds a real main-checkout `.git` directory.
func mkGitMarker(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "worktrees", "wt1"), 0o755); err != nil {
		t.Fatalf("mkdir .git marker: %v", err)
	}
}

func writeHEAD(t *testing.T, gitdir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(gitdir, "HEAD"), []byte(content), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}
}

func resolveReport(t *testing.T, cwd string) where.Report {
	t.Helper()
	report, err := where.Resolve(cwd, filepath.Join(t.TempDir(), "CLAUDE.md"))
	if err != nil {
		t.Fatalf("where.Resolve: %v", err)
	}
	return report
}

func TestWhereJSON_MainCheckoutReportsAllFourPathsPlusBranch(t *testing.T) {
	root := t.TempDir()
	mkGitMarker(t, root)
	writeHEAD(t, filepath.Join(root, ".git"), "ref: refs/heads/main\n")

	report := resolveReport(t, root)
	data, err := whereJSON(report)
	if err != nil {
		t.Fatalf("whereJSON: %v", err)
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if obj["branch"] != "main" {
		t.Errorf("branch = %v, want %q", obj["branch"], "main")
	}
	wantReports := config.ReportsDir(root, "main")
	if obj["reports"] != wantReports {
		t.Errorf("reports = %v, want %q", obj["reports"], wantReports)
	}
	wantReportsRoot := config.ReportsRoot(root)
	if obj["reports_root"] != wantReportsRoot {
		t.Errorf("reports_root = %v, want %q", obj["reports_root"], wantReportsRoot)
	}
	wantReminders := config.ProjectRemindersDir(root)
	if obj["reminders"] != wantReminders {
		t.Errorf("reminders = %v, want %q", obj["reminders"], wantReminders)
	}
	if _, ok := obj["archive"]; !ok {
		t.Errorf("archive field missing")
	}
}

// The whole point of CP2's project key: a worktree of the same clone must
// report the SAME reports/reports_root/reminders/archive paths as its main
// checkout, even though its own branch differs — and must resolve the actual
// "reports" value correctly for a branch containing "/". A synthetic
// (non-real-git) fixture proving this cannot fail for a symlink-divergence or
// nested-vs-flat reports-path bug (a real `.git` worktree resolves its gitdir
// target with realpath; a hand-written `.git` file skips that step), so this
// test uses a REAL `git worktree add` clone rather than a synthetic fixture.
func TestWhereJSON_RealGitWorktreeAgreesWithMainCheckoutOnReportsValue(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	home := t.TempDir()
	defer config.SetHomeDirForTest(home)()

	root := t.TempDir()
	main := filepath.Join(root, "main")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatalf("mkdir main: %v", err)
	}
	runGit(t, main, "init", "-b", "main")
	runGit(t, main, "commit", "--allow-empty", "-m", "init")

	wt := filepath.Join(root, "wt1")
	runGit(t, main, "worktree", "add", "-b", "feature/plans-page", wt)

	// Seed a project-keyed report so "reports" resolves there instead of
	// taking the legacy fallback (which is correct behavior, just not what
	// this test is proving).
	keyedReportsDir := filepath.Join(config.ReportsRoot(main), "feature-plans-page")
	if err := os.MkdirAll(keyedReportsDir, 0o755); err != nil {
		t.Fatalf("seed project-keyed reports dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keyedReportsDir, "r.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}

	mainReport := resolveReport(t, main)
	wtReport := resolveReport(t, wt)

	mainData, err := whereJSON(mainReport)
	if err != nil {
		t.Fatalf("whereJSON(main): %v", err)
	}
	wtData, err := whereJSON(wtReport)
	if err != nil {
		t.Fatalf("whereJSON(worktree): %v", err)
	}

	var mainObj, wtObj map[string]any
	if err := json.Unmarshal([]byte(mainData), &mainObj); err != nil {
		t.Fatalf("unmarshal main: %v", err)
	}
	if err := json.Unmarshal([]byte(wtData), &wtObj); err != nil {
		t.Fatalf("unmarshal worktree: %v", err)
	}

	for _, key := range []string{"reports_root", "reminders", "archive"} {
		if mainObj[key] != wtObj[key] {
			t.Errorf("%s differs: main=%v worktree=%v", key, mainObj[key], wtObj[key])
		}
	}
	if wtObj["branch"] != "feature/plans-page" {
		t.Errorf("worktree branch = %v, want %q", wtObj["branch"], "feature/plans-page")
	}
	if mainObj["branch"] != "main" {
		t.Errorf("main branch = %v, want %q", mainObj["branch"], "main")
	}

	wantReportsPrefix, _ := mainObj["reports_root"].(string)
	wtReports, _ := wtObj["reports"].(string)
	if wantReportsPrefix == "" || !strings.HasPrefix(wtReports, wantReportsPrefix+string(filepath.Separator)) {
		t.Fatalf("worktree reports = %q, want a child of reports_root %q", wtReports, wantReportsPrefix)
	}
	if filepath.Base(wtReports) != "feature-plans-page" {
		t.Errorf("worktree reports basename = %q, want flattened %q", filepath.Base(wtReports), "feature-plans-page")
	}
}

// runGit runs git in dir with a synthetic identity, never touching the real
// $HOME's global gitconfig for author info.
func runGit(t *testing.T, dir string, args ...string) string {
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

func TestWhereJSON_DetachedHEADFallsBackToShortSHA(t *testing.T) {
	root := t.TempDir()
	mkGitMarker(t, root)
	writeHEAD(t, filepath.Join(root, ".git"), "1234567890abcdef1234567890abcdef12345678\n")

	report := resolveReport(t, root)
	data, err := whereJSON(report)
	if err != nil {
		t.Fatalf("whereJSON: %v", err)
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["branch"] != "1234567" {
		t.Errorf("branch = %v, want short SHA %q", obj["branch"], "1234567")
	}
}

// reports must take the legacy fallback for a branch with no project-keyed
// report, while reports_root still names the project-keyed parent in the
// SAME response — reports_root always names the project-keyed parent
// regardless of the fallback reports may have taken.
func TestWhereJSON_ReportsLegacyFallbackWithReportsRootStillProjectKeyed(t *testing.T) {
	root := t.TempDir()
	mkGitMarker(t, root)
	writeHEAD(t, filepath.Join(root, ".git"), "ref: refs/heads/legacy-branch\n")

	legacyDir := config.ReportsDirLegacy(root, "legacy-branch")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy reports dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "2026-08-20-1200.md"), []byte("# report\n"), 0o644); err != nil {
		t.Fatalf("write legacy report: %v", err)
	}
	// No project-keyed reports/legacy-branch/ directory exists.

	report := resolveReport(t, root)
	data, err := whereJSON(report)
	if err != nil {
		t.Fatalf("whereJSON: %v", err)
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if obj["reports"] != legacyDir {
		t.Errorf("reports = %v, want legacy fallback %q", obj["reports"], legacyDir)
	}
	wantReportsRoot := config.ReportsRoot(root)
	if obj["reports_root"] != wantReportsRoot {
		t.Errorf("reports_root = %v, want project-keyed parent %q", obj["reports_root"], wantReportsRoot)
	}
}

// A crafted `.git/HEAD` (repo-controlled, hostile-or-corrupt-repo state) must
// never let its content escape onto disk as a path segment: with no legal
// branch shape to report, "branch"/"reports" are omitted rather than
// resolving into a fabricated report path.
func TestWhereJSON_CraftedHEADTraversalNeverEscapesReportsPath(t *testing.T) {
	home := t.TempDir()
	defer config.SetHomeDirForTest(home)()

	root := t.TempDir()
	mkGitMarker(t, root)
	writeHEAD(t, filepath.Join(root, ".git"), "../../../../../../etc/evil\n")

	report := resolveReport(t, root)
	data, err := whereJSON(report)
	if err != nil {
		t.Fatalf("whereJSON: %v", err)
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := obj["branch"]; ok {
		t.Errorf("branch present for a HEAD with no legal shape: %v", obj["branch"])
	}
	if reports, ok := obj["reports"]; ok {
		t.Errorf("reports present for a HEAD with no legal shape: %v", reports)
	}

	// reports_root/reminders/archive still resolve — they name a project
	// home, not a branch — and must stay inside home's project-keyed subtree,
	// never home itself.
	for _, key := range []string{"reports_root", "reminders", "archive"} {
		val, _ := obj[key].(string)
		if val == "" {
			t.Errorf("%s missing from response", key)
			continue
		}
		if val == home || !strings.HasPrefix(val, home+string(filepath.Separator)) {
			t.Errorf("%s = %q, escaped the project-keyed state home %q", key, val, home)
		}
	}
}

// Existing atomic where consumers must be unaffected: the JSON only grows.
func TestWhereJSON_ExistingFieldsUnchanged(t *testing.T) {
	root := t.TempDir()
	mkGitMarker(t, root)
	writeHEAD(t, filepath.Join(root, ".git"), "ref: refs/heads/main\n")

	report := resolveReport(t, root)
	baseData, err := where.FormatJSON(report)
	if err != nil {
		t.Fatalf("where.FormatJSON: %v", err)
	}
	extendedData, err := whereJSON(report)
	if err != nil {
		t.Fatalf("whereJSON: %v", err)
	}

	var base, extended map[string]any
	if err := json.Unmarshal([]byte(baseData), &base); err != nil {
		t.Fatalf("unmarshal base: %v", err)
	}
	if err := json.Unmarshal([]byte(extendedData), &extended); err != nil {
		t.Fatalf("unmarshal extended: %v", err)
	}

	for key, wantVal := range base {
		gotVal, ok := extended[key]
		if !ok {
			t.Errorf("existing field %q missing from extended output", key)
			continue
		}
		gotJSON, _ := json.Marshal(gotVal)
		wantJSON, _ := json.Marshal(wantVal)
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("existing field %q changed: base=%s extended=%s", key, wantJSON, gotJSON)
		}
	}
}

// runWhere resolves from cwd unless --repo relocates it. The whereJSON tests
// above build their own report from a chosen root and so never exercise that
// relocation; this one drives runWhere itself, in a subprocess because it
// exits, and asserts the fixture's branch comes back rather than the cwd's.
func TestRunWhere_RepoOverrideRelocatesTheWholeReport(t *testing.T) {
	if os.Getenv("ATOMIC_TEST_RUN_WHERE_HELPER") == "1" {
		runWhere([]string{"--json"}, os.Getenv("ATOMIC_TEST_RUN_WHERE_REPO"))
		return
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	fixture := t.TempDir()
	runGit(t, fixture, "init", "-b", "fixture-branch")
	runGit(t, fixture, "commit", "--allow-empty", "-m", "init")

	cmd := exec.Command(os.Args[0], "-test.run=TestRunWhere_RepoOverrideRelocatesTheWholeReport")
	cmd.Env = append(os.Environ(),
		"ATOMIC_TEST_RUN_WHERE_HELPER=1",
		"ATOMIC_TEST_RUN_WHERE_REPO="+fixture,
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("runWhere --repo: %v\n%s", err, out)
	}
	var got map[string]any
	if jerr := json.Unmarshal(out, &got); jerr != nil {
		t.Fatalf("decode: %v\n%s", jerr, out)
	}
	if got["branch"] != "fixture-branch" {
		t.Errorf("branch = %v, want fixture-branch (--repo was ignored and cwd was reported)", got["branch"])
	}
	rr, _ := got["repo_root"].(map[string]any)
	wantRoot, _ := filepath.EvalSymlinks(fixture)
	gotRoot, _ := filepath.EvalSymlinks(fmt.Sprint(rr["path"]))
	if gotRoot != wantRoot {
		t.Errorf("repo_root.path = %v, want %v", rr["path"], fixture)
	}
}
