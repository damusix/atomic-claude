package main

import (
	"encoding/json"
	"errors"
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

func TestWhereJSON_MainCheckoutReportsPathsPlusBranch(t *testing.T) {
	root := t.TempDir()
	mkGitMarker(t, root)
	writeHEAD(t, filepath.Join(root, ".git"), "ref: refs/heads/main\n")

	report := resolveReport(t, root)
	data, err := whereJSON(report, report.RepoRoot.Path, t.TempDir(), "")
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
	if _, ok := obj["reminders"]; ok {
		t.Errorf("reminders key present: %v", obj["reminders"])
	}
	if _, ok := obj["archive"]; !ok {
		t.Errorf("archive field missing")
	}
}

// The whole point of the project key: a worktree of the same clone must
// report the SAME reports/reports_root/archive paths as its main
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

	mainData, err := whereJSON(mainReport, mainReport.RepoRoot.Path, t.TempDir(), "")
	if err != nil {
		t.Fatalf("whereJSON(main): %v", err)
	}
	wtData, err := whereJSON(wtReport, wtReport.RepoRoot.Path, t.TempDir(), "")
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

	for _, key := range []string{"reports_root", "archive"} {
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
	data, err := whereJSON(report, report.RepoRoot.Path, t.TempDir(), "")
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
	data, err := whereJSON(report, report.RepoRoot.Path, t.TempDir(), "")
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
	data, err := whereJSON(report, report.RepoRoot.Path, t.TempDir(), "")
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

	// reports_root/archive still resolve — they name a project
	// home, not a branch — and must stay inside home's project-keyed subtree,
	// never home itself.
	for _, key := range []string{"reports_root", "archive"} {
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
	extendedData, err := whereJSON(report, report.RepoRoot.Path, t.TempDir(), "")
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

// Agents create worktrees where this points, so it follows harness and
// preference, and reads relative inside the repository, absolute outside.
func TestWhereJSON_StateRootAndWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	const outsideMark = "<outside>"
	cases := []struct {
		name, harness, repoPref, profilePref string
		outside, noRepo                      bool
		want                                 string
	}{
		{name: "claude default", harness: "claude", want: ".claude/worktrees"},
		{name: "omp default", harness: "omp", want: ".atomic/worktrees"},
		{name: "codex default", harness: "codex", want: ".atomic/worktrees"},
		{name: "undetected harness", harness: "", want: ".atomic/worktrees"},
		{name: "unknown harness", harness: "pi", want: ".atomic/worktrees"},
		{name: "repo path outranks claude", harness: "claude", repoPref: "trees", want: "trees"},
		{name: "repo path outranks omp", harness: "omp", repoPref: "trees", want: "trees"},
		{name: "profile path", harness: "codex", profilePref: "ptrees", want: "ptrees"},
		{name: "repo outranks profile", harness: "omp", repoPref: "trees", profilePref: "ptrees", want: "trees"},
		{name: "off is empty", harness: "claude", repoPref: "off", want: ""},
		{name: "off is empty unknown", harness: "", repoPref: "off", want: ""},
		{name: "outside claude default", harness: "claude", outside: true, want: ".claude/worktrees"},
		{name: "outside omp default", harness: "omp", outside: true, want: outsideMark + "/worktrees"},
		{name: "outside unknown default", harness: "", outside: true, want: outsideMark + "/worktrees"},
		{name: "outside repo path", harness: "omp", outside: true, repoPref: "trees", want: "trees"},
		{name: "outside off", harness: "codex", outside: true, repoPref: "off", want: ""},
		{name: "no repository maps against cwd", harness: "omp", noRepo: true, want: ".atomic/worktrees"},
		{name: "no repository profile path", harness: "omp", noRepo: true, profilePref: "ptrees", want: "ptrees"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if !tc.noRepo {
				runGit(t, root, "init", "-q")
			}
			stateRoot := filepath.Join(root, ".atomic")
			want := tc.want
			if tc.outside {
				stateRoot = filepath.Join(t.TempDir(), "state")
				t.Setenv(config.StateDirEnvVar, stateRoot)
				want = strings.Replace(want, outsideMark, stateRoot, 1)
			}
			if tc.repoPref != "" {
				writeWherePrefs(t, filepath.Join(stateRoot, "workflow-preferences.md"),
					worktreesBlock("repo", tc.repoPref))
			}
			if tc.profilePref != "" {
				writeWherePrefs(t, config.ProfilePath(home),
					"# User profile\n\n## Workflow preferences\n\n"+worktreesBlock("profile", tc.profilePref))
			}

			data, err := whereJSON(resolveReport(t, root), root, home, tc.harness)
			if err != nil {
				t.Fatalf("whereJSON: %v", err)
			}
			var obj map[string]any
			if err := json.Unmarshal([]byte(data), &obj); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if obj["state_root"] != stateRoot {
				t.Errorf("state_root = %v, want %s", obj["state_root"], stateRoot)
			}
			got, ok := obj["worktrees"]
			if !ok || got != want {
				t.Errorf("worktrees = %v (present %v), want %q", got, ok, want)
			}
		})
	}
}

// where and workflow resolve must report the same worktrees value, so the realm
// level is found from cwd, not from the repository root above it.
func TestWhereJSON_RealmBetweenRepoRootAndCwdSetsWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "-q")
	realm := filepath.Join(root, "realm")
	cwd := filepath.Join(realm, "cwd")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWherePrefs(t, config.RepoConfigPath(realm), "scope = \"realm\"\n")
	prefs, err := config.WorkflowPrefsPath(realm)
	if err != nil {
		t.Fatal(err)
	}
	writeWherePrefs(t, prefs, worktreesBlock("realm", "realmtrees"))

	report := resolveReport(t, cwd)
	if report.RepoRoot.Path != root {
		t.Fatalf("repo_root = %s, want %s", report.RepoRoot.Path, root)
	}
	data, err := whereJSON(report, cwd, t.TempDir(), "omp")
	if err != nil {
		t.Fatalf("whereJSON: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["worktrees"] != "realmtrees" {
		t.Errorf("worktrees = %v, want realmtrees", obj["worktrees"])
	}
}

// An ambiguous state root must fail whereJSON rather than report a guessed
// path. The report is built by hand: where.Resolve's index lookup exits first.
func TestWhereJSON_RefusedStateRootIsAnError(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mkGitMarker(t, root)
	writeHEAD(t, filepath.Join(root, ".git"), "ref: refs/heads/main\n")
	cfgHome := t.TempDir()
	defer config.SetHomeDirForTest(cfgHome)()
	if err := config.WriteAmbiguousStateForTest(cfgHome, root); err != nil {
		t.Fatal(err)
	}

	report := where.Report{RepoRoot: where.RepoRootReport{Path: root, Source: config.ScopeSourceGit}}
	if _, err := whereJSON(report, report.RepoRoot.Path, t.TempDir(), "omp"); err == nil ||
		!strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("whereJSON err = %v, want the ambiguous state-root refusal", err)
	}
}

func worktreesBlock(level, line string) string {
	return "<workflow-preferences level='" + level + "'>\n- worktrees: " + line +
		"\n</workflow-preferences>\n"
}

func writeWherePrefs(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --harness picks the recommendation without detection, and an unknown value
// is a usage error rather than a silent fallback.
func TestRunWhere_HarnessFlag(t *testing.T) {
	if os.Getenv("ATOMIC_TEST_RUN_WHERE_HELPER") == "1" {
		runWhere(strings.Fields(os.Getenv("ATOMIC_TEST_RUN_WHERE_ARGS")), os.Getenv("ATOMIC_TEST_RUN_WHERE_REPO"))
		return
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	fixture := t.TempDir()
	home := t.TempDir()
	runGit(t, fixture, "init", "-q")
	run := func(args string) (string, error) {
		cmd := exec.Command(os.Args[0], "-test.run=TestRunWhere_HarnessFlag")
		cmd.Env = append(os.Environ(),
			"HOME="+home,
			"ATOMIC_TEST_RUN_WHERE_HELPER=1",
			"ATOMIC_TEST_RUN_WHERE_REPO="+fixture,
			"ATOMIC_TEST_RUN_WHERE_ARGS="+args,
		)
		out, err := cmd.Output()
		return string(out), err
	}

	for _, tc := range []struct {
		args, want string
	}{
		{"--json --harness omp", ".atomic/worktrees"},
		{"--json --harness claude", ".claude/worktrees"},
		{"--json", ".atomic/worktrees"},
	} {
		out, err := run(tc.args)
		if err != nil {
			t.Fatalf("where %s: %v\n%s", tc.args, err, out)
		}
		var got map[string]any
		if jerr := json.Unmarshal([]byte(out), &got); jerr != nil {
			t.Fatalf("decode: %v\n%s", jerr, out)
		}
		if got["worktrees"] != tc.want {
			t.Errorf("where %s: worktrees = %v, want %s", tc.args, got["worktrees"], tc.want)
		}
	}

	_, err := run("--json --harness pi")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Errorf("--harness pi: err = %v, want exit 2", err)
	}
}

// --repo below the Git root names that root's repository for where and
// workflow resolve alike, so both report the root's worktrees preference.
func TestRunWhere_RepoSubdirMatchesWorkflowResolve(t *testing.T) {
	if os.Getenv("ATOMIC_TEST_RUN_WHERE_HELPER") == "1" {
		runWhere([]string{"--json", "--harness", "omp"}, os.Getenv("ATOMIC_TEST_RUN_WHERE_REPO"))
		return
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "-q")
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWherePrefs(t, filepath.Join(root, ".atomic", "workflow-preferences.md"), worktreesBlock("repo", "trees"))
	home := t.TempDir()

	cmd := exec.Command(os.Args[0], "-test.run=TestRunWhere_RepoSubdirMatchesWorkflowResolve")
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"ATOMIC_TEST_RUN_WHERE_HELPER=1",
		"ATOMIC_TEST_RUN_WHERE_REPO="+sub,
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("where --repo: %v\n%s", err, out)
	}
	var got map[string]any
	if jerr := json.Unmarshal(out, &got); jerr != nil {
		t.Fatalf("decode: %v\n%s", jerr, out)
	}
	if got["worktrees"] != "trees" {
		t.Errorf("where worktrees = %v, want trees", got["worktrees"])
	}

	resolved, _, _ := workflowCLI(t, workflowEnv{home: home, cwd: t.TempDir(), repo: sub},
		"resolve", "--harness", "omp")
	if !strings.Contains(resolved, "- worktrees: trees (repo)") {
		t.Errorf("workflow resolve disagrees with where:\n%s", resolved)
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
