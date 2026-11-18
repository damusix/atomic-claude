package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
)

func noDetect(t *testing.T) func() harness.Detection {
	return func() harness.Detection {
		t.Error("harness detection ran")
		return harness.Detection{}
	}
}

func workflowCLI(t *testing.T, e workflowEnv, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	if e.detect == nil {
		e.detect = noDetect(t)
	}
	var out, errOut bytes.Buffer
	code = workflowAction(args, e, &out, &errOut)
	return out.String(), errOut.String(), code
}

func gitRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return repo
}

func plainDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWorkflowResolve_PrintsTheResolvedBlock(t *testing.T) {
	repo := gitRepo(t)
	out, _, code := workflowCLI(t, workflowEnv{home: plainDir(t)},
		"resolve", "--cwd", repo, "--harness", "claude")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		"<workflow-preferences level='resolved'>",
		"- source-control: github (default)",
		"- worktrees: .claude/worktrees (default)",
		"warning: none",
		"Read: references/source-control/github.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWorkflowResolve_JSONCarriesTheSameFacts(t *testing.T) {
	repo := gitRepo(t)
	out, _, code := workflowCLI(t, workflowEnv{home: plainDir(t)},
		"resolve", "--json", "--cwd", repo, "--harness", "omp")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Values         []struct{ Key, Value, Level string }
		Warnings, Read []string
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	byKey := map[string]string{}
	for _, v := range got.Values {
		byKey[v.Key] = v.Value + " (" + v.Level + ")"
	}
	if byKey["source-control"] != "github (default)" || byKey["worktrees"] != ".atomic/worktrees (default)" {
		t.Errorf("values = %v", byKey)
	}
	if len(got.Read) != 3 {
		t.Errorf("read = %v", got.Read)
	}
}

func TestWorkflowResolve_OutsideARepositoryExitsZeroWithAWarning(t *testing.T) {
	out, _, code := workflowCLI(t, workflowEnv{home: plainDir(t)},
		"resolve", "--cwd", plainDir(t), "--harness", "claude")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(out, "warning: outside a repository") {
		t.Errorf("no repository warning in:\n%s", out)
	}
}

func TestWorkflowResolve_ReadsTheProfileLevel(t *testing.T) {
	home := plainDir(t)
	path := config.ProfilePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	profileText := "## Workflow preferences\n\n<workflow-preferences level='profile'>\n- issue-tracker-project: ACME\n</workflow-preferences>\n"
	if err := os.WriteFile(path, []byte(profileText), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, _ := workflowCLI(t, workflowEnv{home: home},
		"resolve", "--cwd", plainDir(t), "--harness", "claude")
	if !strings.Contains(out, "- issue-tracker-project: ACME (profile)") {
		t.Errorf("profile value missing:\n%s", out)
	}
}

func TestWorkflowResolve_RepoFlagIsTheDirectoryUnlessCwdIsGiven(t *testing.T) {
	repo := gitRepo(t)
	prefs := filepath.Join(repo, ".atomic", "workflow-preferences.md")
	if err := os.MkdirAll(filepath.Dir(prefs), 0o755); err != nil {
		t.Fatal(err)
	}
	block := "<workflow-preferences level='repo'>\n- issue-tracker-project: ACME\n</workflow-preferences>\n"
	if err := os.WriteFile(prefs, []byte(block), 0o644); err != nil {
		t.Fatal(err)
	}
	e := workflowEnv{home: plainDir(t), cwd: plainDir(t), repo: repo}

	out, _, _ := workflowCLI(t, e, "resolve", "--harness", "claude")
	if !strings.Contains(out, "- issue-tracker-project: ACME (repo)") {
		t.Errorf("--repo ignored:\n%s", out)
	}
	out, _, _ = workflowCLI(t, e, "resolve", "--harness", "claude", "--cwd", plainDir(t))
	if strings.Contains(out, "ACME") {
		t.Errorf("--cwd did not win over --repo:\n%s", out)
	}
}

func TestWorkflowResolve_ExitCodes(t *testing.T) {
	e := workflowEnv{home: plainDir(t)}
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"missing directory", []string{"resolve", "--cwd", filepath.Join(plainDir(t), "nope"), "--harness", "claude"}, 1},
		{"unknown harness", []string{"resolve", "--harness", "vim"}, 2},
		{"unknown flag", []string{"resolve", "--bogus"}, 2},
		{"stray argument", []string{"resolve", "extra", "--harness", "claude"}, 2},
		{"no verb", nil, 2},
		{"unknown verb", []string{"frobnicate"}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, code := workflowCLI(t, e, c.args...); code != c.want {
				t.Errorf("exit %d, want %d", code, c.want)
			}
		})
	}
}

func TestWorkflowResolve_HarnessFlagSkipsDetection(t *testing.T) {
	e := workflowEnv{home: plainDir(t), detect: noDetect(t)}
	if _, _, code := workflowCLI(t, e, "resolve", "--cwd", plainDir(t), "--harness", "codex"); code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestWorkflowResolve_WithoutTheFlagUsesDetection(t *testing.T) {
	repo := gitRepo(t)
	called := false
	e := workflowEnv{home: plainDir(t), detect: func() harness.Detection {
		called = true
		return harness.Detection{Kind: harness.KindOMP, Confidence: harness.ConfidenceStrong}
	}}
	out, _, code := workflowCLI(t, e, "resolve", "--cwd", repo)
	if code != 0 || !called {
		t.Fatalf("exit %d, detect called %v", code, called)
	}
	if !strings.Contains(out, "- worktrees: .atomic/worktrees (default)") {
		t.Errorf("detected OMP did not shape the worktrees line:\n%s", out)
	}
}

func TestWorkflowSet_WritesAtEachLevel(t *testing.T) {
	home := plainDir(t)
	repo := gitRepo(t)
	realm := plainDir(t)
	if err := os.MkdirAll(filepath.Dir(config.RepoConfigPath(realm)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.RepoConfigPath(realm), []byte("scope = \"realm\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		level, cwd, path, wantIn string
	}{
		{"profile", repo, config.ProfilePath(home), "<workflow-preferences level='profile'>"},
		{"repo", repo, filepath.Join(repo, ".atomic", "workflow-preferences.md"), "<workflow-preferences level='repo'>"},
		{"realm", realm, filepath.Join(realm, ".atomic", "workflow-preferences.md"), "<workflow-preferences level='realm'>"},
	}
	for _, c := range cases {
		t.Run(c.level, func(t *testing.T) {
			out, _, code := workflowCLI(t, workflowEnv{home: home, cwd: c.cwd},
				"set", "issue-tracker", "linear", "--level", c.level)
			if code != 0 {
				t.Fatalf("exit %d", code)
			}
			if out != "wrote "+c.path+"\n" {
				t.Errorf("stdout = %q, want only the wrote line", out)
			}
			body := readFile(t, c.path)
			if !strings.Contains(body, c.wantIn) || !strings.Contains(body, "\n- issue-tracker: linear\n") {
				t.Errorf("file:\n%s", body)
			}
		})
	}
}

func TestWorkflowSet_ProfileDefaultAndFlagBeforeThePositionals(t *testing.T) {
	home := plainDir(t)
	if _, _, code := workflowCLI(t, workflowEnv{home: home, cwd: plainDir(t)},
		"set", "worktrees", "off"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(readFile(t, config.ProfilePath(home)), "- worktrees: off\n") {
		t.Error("default level is not profile")
	}
	if _, _, code := workflowCLI(t, workflowEnv{home: home, cwd: gitRepo(t)},
		"set", "--level", "repo", "worktrees", "off"); code != 0 {
		t.Errorf("flag before positionals: exit %d", code)
	}
}

func TestWorkflowSet_ReplacesAndNeverTouchesOtherProfileBytes(t *testing.T) {
	home := plainDir(t)
	path := config.ProfilePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	before := "# Me\n\nprose\n\n## Workflow preferences\n\n" +
		"<workflow-preferences level='profile'>\n" +
		"- source-control: gitlab  # detected: glab authed\n" +
		"</workflow-preferences>\n\n## Environment\n\nenv text\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, code := workflowCLI(t, workflowEnv{home: home, cwd: plainDir(t)},
		"set", "source-control", "codeberg"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := strings.Replace(before, "- source-control: gitlab  # detected: glab authed", "- source-control: codeberg", 1)
	if got := readFile(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWorkflowSet_CreatesAProfileSectionWhenTheFileHasNone(t *testing.T) {
	home := plainDir(t)
	path := config.ProfilePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Me\n\n## Environment\n\nenv text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, code := workflowCLI(t, workflowEnv{home: home, cwd: plainDir(t)},
		"set", "worktrees", "off"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := "# Me\n\n## Workflow preferences\n\n<workflow-preferences level='profile'>\n" +
		"- worktrees: off\n</workflow-preferences>\n\n## Environment\n\nenv text\n"
	if got := readFile(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWorkflowSet_ExitCodes(t *testing.T) {
	home := plainDir(t)
	repo := gitRepo(t)
	dup := "<workflow-preferences level='repo'>\n</workflow-preferences>\n"
	dupPath := filepath.Join(repo, ".atomic", "workflow-preferences.md")
	if err := os.MkdirAll(filepath.Dir(dupPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dupPath, []byte(dup+dup), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		cwd  string
		args []string
		want int
	}{
		{"unknown key", repo, []string{"set", "colour", "red"}, 2},
		{"invalid value", repo, []string{"set", "source-control", "svn"}, 2},
		{"unknown level", repo, []string{"set", "worktrees", "off", "--level", "galaxy"}, 2},
		{"missing value", repo, []string{"set", "worktrees"}, 2},
		{"extra argument", repo, []string{"set", "worktrees", "off", "more"}, 2},
		{"realm outside a realm", plainDir(t), []string{"set", "worktrees", "off", "--level", "realm"}, 1},
		{"repo outside a repository", plainDir(t), []string{"set", "worktrees", "off", "--level", "repo"}, 1},
		{"refusal case", repo, []string{"set", "worktrees", "off", "--level", "repo"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, code := workflowCLI(t, workflowEnv{home: home, cwd: c.cwd}, c.args...); code != c.want {
				t.Errorf("exit %d, want %d", code, c.want)
			}
		})
	}
	if got := readFile(t, dupPath); got != dup+dup {
		t.Errorf("a refused file was rewritten:\n%s", got)
	}
}

func TestWorkflowSet_RefusedProfileIsLeftByteIdentical(t *testing.T) {
	block := "<workflow-preferences level='profile'>\n- worktrees: off\n</workflow-preferences>\n"
	cases := map[string]string{
		"duplicate block":                   "## Workflow preferences\n\n" + block + block,
		"unpaired tag":                      "## Workflow preferences\n\n<workflow-preferences level='profile'>\n- worktrees: off\n",
		"block inside the Environment span": "## Environment\n\n" + block,
	}
	for name, before := range cases {
		t.Run(name, func(t *testing.T) {
			home := plainDir(t)
			path := config.ProfilePath(home)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
				t.Fatal(err)
			}
			_, errOut, code := workflowCLI(t, workflowEnv{home: home, cwd: plainDir(t)},
				"set", "issue-tracker", "linear")
			if code != 1 || errOut == "" {
				t.Errorf("exit %d, stderr %q; want 1 with a reason", code, errOut)
			}
			if got := readFile(t, path); got != before {
				t.Errorf("profile changed:\n%s", got)
			}
		})
	}
}

func TestWorkflowSet_SharedRepoAndRealmRootRefusesALevelConflict(t *testing.T) {
	repo := gitRepo(t)
	if err := os.MkdirAll(filepath.Dir(config.RepoConfigPath(repo)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.RepoConfigPath(repo), []byte("scope = \"realm\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prefs := filepath.Join(repo, ".atomic", "workflow-preferences.md")
	e := workflowEnv{home: plainDir(t), cwd: repo}

	for _, c := range []struct{ block, requested string }{
		{"repo", "realm"},
		{"realm", "repo"},
	} {
		before := "<workflow-preferences level='" + c.block + "'>\n- worktrees: off\n</workflow-preferences>\n"
		if err := os.WriteFile(prefs, []byte(before), 0o644); err != nil {
			t.Fatal(err)
		}
		_, errOut, code := workflowCLI(t, e, "set", "issue-tracker", "linear", "--level", c.requested)
		if code != 1 || !strings.Contains(errOut, prefs) ||
			!strings.Contains(errOut, c.block) || !strings.Contains(errOut, c.requested) {
			t.Errorf("block %s, requested %s: exit %d, stderr %q", c.block, c.requested, code, errOut)
		}
		if got := readFile(t, prefs); got != before {
			t.Errorf("block %s, requested %s: file changed:\n%s", c.block, c.requested, got)
		}
		if _, _, code := workflowCLI(t, e, "set", "issue-tracker", "linear", "--level", c.block); code != 0 {
			t.Errorf("matching level %s: exit %d", c.block, code)
		}
	}
}

func TestWorkflowSet_AmbiguousStateRootExitsOne(t *testing.T) {
	cfgHome := plainDir(t)
	t.Cleanup(config.SetHomeDirForTest(cfgHome))
	cfg := config.Default()
	if err := config.Set(cfg, "state.dir", ".state"); err != nil {
		t.Fatal(err)
	}
	if err := config.WritePersist(config.TOMLPath(cfgHome), cfg); err != nil {
		t.Fatal(err)
	}
	repo := gitRepo(t)
	for _, seg := range []string{".claude", ".atomic"} {
		if err := os.MkdirAll(filepath.Join(repo, seg, "project"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, seg, "project", "x.md"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, errOut, code := workflowCLI(t, workflowEnv{home: plainDir(t), cwd: repo},
		"set", "worktrees", "off", "--level", "repo")
	if code != 1 {
		t.Errorf("exit %d, want 1 (stderr %q)", code, errOut)
	}
}

func TestWorkflowSet_RepoFlagOverridesTheDirectory(t *testing.T) {
	repo := gitRepo(t)
	if _, _, code := workflowCLI(t, workflowEnv{home: plainDir(t), cwd: plainDir(t), repo: repo},
		"set", "worktrees", "off", "--level", "repo"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(readFile(t, filepath.Join(repo, ".atomic", "workflow-preferences.md")), "- worktrees: off") {
		t.Error("--repo root not written")
	}
}

// --repo outside any repository is no repository, as it is for where.
func TestWorkflow_NonRepositoryRepoFlagIsOutsideARepository(t *testing.T) {
	e := workflowEnv{home: plainDir(t), cwd: gitRepo(t), repo: plainDir(t)}
	out, _, code := workflowCLI(t, e, "resolve", "--harness", "claude")
	if code != 0 || !strings.Contains(out, "warning: outside a repository") {
		t.Errorf("resolve exit %d, want 0 with the repository warning:\n%s", code, out)
	}
	_, errOut, code := workflowCLI(t, e, "set", "worktrees", "off", "--level", "repo")
	if code != 1 || !strings.Contains(errOut, "outside a repository") {
		t.Errorf("set exit %d, want 1 naming the missing repository: %q", code, errOut)
	}
}

// A profile set waits for a running profile refresh instead of racing it.
func TestWorkflowSet_ProfileWaitsForTheProfileLock(t *testing.T) {
	home, cwd := plainDir(t), plainDir(t)
	unlock, err := profile.LockProfile(home)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan int)
	go func() {
		var out, errOut bytes.Buffer
		done <- workflowAction([]string{"set", "issue-tracker-project", "ACME"},
			workflowEnv{home: home, cwd: cwd}, &out, &errOut)
	}()
	select {
	case <-done:
		t.Fatal("set wrote the profile while the lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("set never finished after the lock was released")
	}
	if !strings.Contains(readFile(t, config.ProfilePath(home)), "- issue-tracker-project: ACME") {
		t.Error("value not written")
	}
}

// A value set through --repo must be the value resolve reads back through the
// same --repo, even when it names a directory below the Git root.
func TestWorkflowSetAndResolve_AgreeOnANonRootRepoFlag(t *testing.T) {
	sub := filepath.Join(gitRepo(t), "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	e := workflowEnv{home: plainDir(t), cwd: plainDir(t), repo: sub}
	if _, errOut, code := workflowCLI(t, e, "set", "issue-tracker-project", "ACME", "--level", "repo"); code != 0 {
		t.Fatalf("set exit %d: %s", code, errOut)
	}
	out, _, _ := workflowCLI(t, e, "resolve", "--harness", "claude")
	if !strings.Contains(out, "- issue-tracker-project: ACME (repo)") {
		t.Errorf("resolve did not read the file set wrote:\n%s", out)
	}
}

func TestWorkflowSet_PopulatedLegacyRootIsWrittenInPlaceAndNothingMoves(t *testing.T) {
	repo := gitRepo(t)
	legacy := filepath.Join(repo, ".claude", "project", "followups", "a.md")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, code := workflowCLI(t, workflowEnv{home: plainDir(t), cwd: repo},
		"set", "worktrees", "off", "--level", "repo"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(readFile(t, filepath.Join(repo, ".claude", "workflow-preferences.md")), "- worktrees: off") {
		t.Error("not written into the legacy root")
	}
	if readFile(t, legacy) != "entry\n" {
		t.Error("legacy entry changed")
	}
	if _, err := os.Stat(filepath.Join(repo, ".atomic")); !os.IsNotExist(err) {
		t.Errorf("destination root was created: %v", err)
	}
}

// PATH is kept because repository detection runs git; an empty one reads as
// "outside a repository".
func builtBinary(t *testing.T, home string, args ...string) (string, string, int) {
	t.Helper()
	return runAtomicCLIFull(t, home, []string{config.StateDirEnvVar + "=", "PATH=" + os.Getenv("PATH")},
		append([]string{"root"}, args...)...)
}

func TestWorkflowBinary_ResolveAndSetUnderASandboxHome(t *testing.T) {
	for _, name := range profile.WorkflowEnvVars() {
		if os.Getenv(name) != "" {
			t.Fatalf("%s leaked into the test process", name)
		}
	}
	home := plainDir(t)
	repo := gitRepo(t)

	out, _, code := builtBinary(t, home, "workflow", "resolve", "--cwd", repo, "--harness", "claude")
	if code != 0 || !strings.Contains(out, "<workflow-preferences level='resolved'>") {
		t.Fatalf("resolve: exit %d\n%s", code, out)
	}

	out, _, code = builtBinary(t, home, "workflow", "resolve", "--json", "--cwd", repo, "--harness", "claude")
	var parsed map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &parsed) != nil {
		t.Fatalf("resolve --json: exit %d, not JSON:\n%s", code, out)
	}

	out, _, code = builtBinary(t, home, "workflow", "set", "source-control", "gitlab")
	if want := "wrote " + config.ProfilePath(home) + "\n"; code != 0 || out != want {
		t.Fatalf("set profile: exit %d, stdout %q, want %q", code, out, want)
	}
	out, _, code = builtBinary(t, home, "--repo", repo, "workflow", "set", "worktrees", "off", "--level", "repo")
	if want := "wrote " + filepath.Join(repo, ".atomic", "workflow-preferences.md") + "\n"; code != 0 || out != want {
		t.Fatalf("set repo: exit %d, stdout %q, want %q", code, out, want)
	}

	out, _, code = builtBinary(t, home, "workflow", "resolve", "--cwd", repo, "--harness", "claude")
	for _, want := range []string{"- worktrees: off (repo)", "- source-control: gitlab (profile)"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("resolve after set: exit %d, missing %q in:\n%s", code, want, out)
		}
	}
}

func TestWorkflowBinary_SetRealmAndUsageExit(t *testing.T) {
	home := plainDir(t)
	realm := plainDir(t)
	if err := os.MkdirAll(filepath.Dir(config.RepoConfigPath(realm)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.RepoConfigPath(realm), []byte("scope = \"realm\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, code := builtBinary(t, home, "--repo", realm, "workflow", "set", "issue-tracker", "jira", "--level", "realm")
	if want := "wrote " + filepath.Join(realm, ".atomic", "workflow-preferences.md") + "\n"; code != 0 || out != want {
		t.Fatalf("set realm: exit %d, stdout %q, want %q", code, out, want)
	}
	if _, _, code = builtBinary(t, home, "workflow", "set", "bogus", "x"); code != 2 {
		t.Errorf("unknown key: exit %d, want 2", code)
	}
}
