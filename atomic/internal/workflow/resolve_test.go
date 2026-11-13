package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (r Resolved) get(key Key) (Value, bool) {
	for _, v := range r.Values {
		if v.Key == key {
			return v, true
		}
	}
	return Value{}, false
}

// tempDir resolves symlinks so paths match what git rev-parse reports.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func repoWithOrigin(t *testing.T, origin string) string {
	t.Helper()
	repo := tempDir(t)
	run(t, "git", "-C", repo, "init", "-q")
	if origin != "" {
		run(t, "git", "-C", repo, "remote", "add", "origin", origin)
	}
	return repo
}

// ambiguousRepo holds state under .claude and .atomic with state.dir set to
// neither, so the state root refuses.
func ambiguousRepo(t *testing.T, origin string) string {
	t.Helper()
	cfgHome := tempDir(t)
	t.Cleanup(config.SetHomeDirForTest(cfgHome))
	repo := repoWithOrigin(t, origin)
	if err := config.WriteAmbiguousStateForTest(cfgHome, repo); err != nil {
		t.Fatal(err)
	}
	return repo
}

func prefsBlock(level string, lines ...string) string {
	return "<workflow-preferences level='" + level + "'>\n" +
		strings.Join(lines, "\n") + "\n</workflow-preferences>\n"
}

func writeRepoPrefs(t *testing.T, root, level string, lines ...string) {
	t.Helper()
	path, err := config.WorkflowPrefsPath(root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, prefsBlock(level, lines...))
}

func writeProfile(t *testing.T, home string, lines ...string) {
	t.Helper()
	writeFile(t, config.ProfilePath(home),
		"# User profile\n\n## Workflow preferences\n\n"+
			prefsBlock("profile", lines...)+"\n## Environment\n\ntext\n")
}

func resolveAt(t *testing.T, dir, home, harness string) Resolved {
	t.Helper()
	r, err := Resolve(Options{Dir: dir, Home: home, Harness: harness})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return r
}

func want(t *testing.T, r Resolved, key Key, value string, level Level) {
	t.Helper()
	got, ok := r.get(key)
	if !ok || got.Value != value || got.Level != level {
		t.Errorf("%s = %+v (set %v), want %s (%s)", key, got, ok, value, level)
	}
}

func wantUnset(t *testing.T, r Resolved, key Key) {
	t.Helper()
	if got, ok := r.get(key); ok {
		t.Errorf("%s = %+v, want unset", key, got)
	}
}

func TestResolve_LevelsAreConsultedRepoThenRealmThenProfile(t *testing.T) {
	home := tempDir(t)
	realm := tempDir(t)
	writeFile(t, filepath.Join(realm, ".atomic", "atomic.toml"), "scope = \"realm\"\n")
	writeRepoPrefs(t, realm, "realm",
		"- source-control: gitlab", "- issue-tracker: jira", "- issue-tracker-project: REALMPROJ")
	repo := filepath.Join(realm, "app")
	run(t, "git", "init", "-q", repo)
	writeRepoPrefs(t, repo, "repo", "- issue-tracker-project: REPOPROJ")
	writeProfile(t, home, "- source-control: bitbucket", "- issue-tracker: trello", "- worktrees: off")

	r := resolveAt(t, repo, home, "claude")

	want(t, r, KeyIssueTrackerProject, "REPOPROJ", LevelRepo)
	want(t, r, KeySourceControl, "gitlab", LevelRealm)
	want(t, r, KeyIssueTracker, "jira", LevelRealm)
	want(t, r, KeyWorktrees, "off", LevelProfile)
}

func TestResolve_RealmLevelIsSkippedOutsideARealm(t *testing.T) {
	home := tempDir(t)
	parent := tempDir(t)
	writeRepoPrefs(t, parent, "realm", "- source-control: codeberg")
	repo := filepath.Join(parent, "app")
	run(t, "git", "init", "-q", repo)
	writeProfile(t, home, "- source-control: gitlab")

	r := resolveAt(t, repo, home, "claude")

	want(t, r, KeySourceControl, "gitlab", LevelProfile)
	if len(r.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", r.Warnings)
	}
}

func TestResolve_RepoRootThatIsTheRealmRootReadsItsOneFileByLevelAttribute(t *testing.T) {
	cases := []struct {
		name, level string
		lines       []string
		wantLevel   Level
		warnings    int
	}{
		{"declared realm", "realm", []string{"- source-control: gitlab"}, LevelRealm, 0},
		{"declared repo", "repo", []string{"- source-control: gitlab"}, LevelRepo, 0},
		{"missing level", "", []string{"- source-control: gitlab"}, LevelRepo, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := repoWithOrigin(t, "")
			writeFile(t, filepath.Join(root, ".atomic", "atomic.toml"), "scope = \"realm\"\n")
			path, _ := config.WorkflowPrefsPath(root)
			if tc.level == "" {
				writeFile(t, path, "<workflow-preferences>\n"+strings.Join(tc.lines, "\n")+"\n</workflow-preferences>\n")
			} else {
				writeRepoPrefs(t, root, tc.level, tc.lines...)
			}

			r := resolveAt(t, root, tempDir(t), "claude")

			want(t, r, KeySourceControl, "gitlab", tc.wantLevel)
			if len(r.Warnings) != tc.warnings {
				t.Errorf("warnings = %v, want %d", r.Warnings, tc.warnings)
			}
		})
	}
}

// Skipping the repo level must not skip origin inference: the repository is
// still a repository.
func TestResolve_AmbiguousStateRootStillInfersFromOrigin(t *testing.T) {
	repo := ambiguousRepo(t, "https://bitbucket.org/acme/app.git")

	r := resolveAt(t, repo, tempDir(t), "omp")

	want(t, r, KeySourceControl, "bitbucket", LevelOrigin)
	want(t, r, KeyIssueTracker, "filesystem", LevelDerived)
	want(t, r, KeyWorktrees, "auto", LevelDefault)
}

func TestResolve_RegisteredRealmWithoutMarkerIsUsed(t *testing.T) {
	home := tempDir(t)
	realm := tempDir(t)
	writeFile(t, filepath.Join(home, ".atomic", "wikis.md"),
		"<wikis>\n- "+filepath.Join(realm, "wiki", "index.md")+"\n</wikis>\n")
	writeRepoPrefs(t, realm, "realm", "- source-control: codeberg")
	repo := filepath.Join(realm, "app")
	run(t, "git", "init", "-q", repo)

	want(t, resolveAt(t, repo, home, "claude"), KeySourceControl, "codeberg", LevelRealm)
}

func TestResolve_ExplicitOutranksAnySoftValue(t *testing.T) {
	home := tempDir(t)
	repo := repoWithOrigin(t, "")
	writeRepoPrefs(t, repo, "repo", "- source-control: gitlab  # detected")
	writeProfile(t, home, "- source-control: codeberg")

	want(t, resolveAt(t, repo, home, "claude"), KeySourceControl, "codeberg", LevelProfile)
}

func TestResolve_DefaultsWhenNothingIsSet(t *testing.T) {
	r := resolveAt(t, repoWithOrigin(t, ""), tempDir(t), "claude")

	want(t, r, KeySourceControl, "github", LevelDefault)
	want(t, r, KeySourceControlHost, "github.com", LevelDefault)
	want(t, r, KeyIssueTracker, "github", LevelDerived)
	want(t, r, KeyWorktrees, ".claude/worktrees", LevelDefault)
	wantUnset(t, r, KeyIssueTrackerProject)
}

func TestResolve_SourceControlFromOriginOutranksSoftValues(t *testing.T) {
	home := tempDir(t)
	repo := repoWithOrigin(t, "https://bitbucket.org/acme/app.git")
	writeProfile(t, home, "- source-control: github  # detected")

	r := resolveAt(t, repo, home, "claude")

	want(t, r, KeySourceControl, "bitbucket", LevelOrigin)
	want(t, r, KeyIssueTracker, "filesystem", LevelDerived)
	want(t, r, KeySourceControlHost, "bitbucket.org", LevelOrigin)
}

func TestResolve_NoOriginSoftProfileValueResolvesAtProfile(t *testing.T) {
	home := tempDir(t)
	repo := repoWithOrigin(t, "")
	writeProfile(t, home, "- source-control: gitlab  # detected")

	r := resolveAt(t, repo, home, "claude")

	want(t, r, KeySourceControl, "gitlab", LevelProfile)
	want(t, r, KeyIssueTracker, "gitlab", LevelDerived)
}

// The origin host belongs to the origin's provider; borrowing it for a
// different explicit provider would point gitlab at github.com.
func TestResolve_HostFromOriginOnlyWhenProvidersMatch(t *testing.T) {
	repo := repoWithOrigin(t, "git@github.com:acme/app.git")
	writeRepoPrefs(t, repo, "repo", "- source-control: gitlab")

	r := resolveAt(t, repo, tempDir(t), "claude")

	want(t, r, KeySourceControl, "gitlab", LevelRepo)
	want(t, r, KeySourceControlHost, "gitlab.com", LevelDefault)
}

func TestResolve_HostFromOriginForGitHubEnterprise(t *testing.T) {
	repo := repoWithOrigin(t, "git@acme.ghe.com:acme/app.git")

	r := resolveAt(t, repo, tempDir(t), "claude")

	want(t, r, KeySourceControl, "github", LevelOrigin)
	want(t, r, KeySourceControlHost, "acme.ghe.com", LevelOrigin)
}

func TestResolve_ExplicitHostOutranksOrigin(t *testing.T) {
	repo := repoWithOrigin(t, "git@github.com:acme/app.git")
	writeRepoPrefs(t, repo, "repo", "- source-control-host: github.acme.example")

	want(t, resolveAt(t, repo, tempDir(t), "claude"), KeySourceControlHost, "github.acme.example", LevelRepo)
}

func TestResolve_ExplicitProviderDifferingFromOriginWarnsNamingBoth(t *testing.T) {
	repo := repoWithOrigin(t, "git@github.com:acme/app.git")
	writeRepoPrefs(t, repo, "repo", "- source-control: gitlab")

	r := resolveAt(t, repo, tempDir(t), "claude")

	if !hasWarning(r.Warnings, "gitlab") || !hasWarning(r.Warnings, "github") {
		t.Errorf("warnings = %v, want one naming gitlab and github", r.Warnings)
	}
}

func TestResolve_UnmappedOriginHostWarnsForExplicitValues(t *testing.T) {
	repo := repoWithOrigin(t, "git@git.acme.example:acme/app.git")

	r := resolveAt(t, repo, tempDir(t), "claude")

	if !hasWarning(r.Warnings, "git.acme.example") || !hasWarning(r.Warnings, "source-control-host") {
		t.Errorf("warnings = %v, want a request for explicit source-control and source-control-host", r.Warnings)
	}
	want(t, r, KeySourceControl, "github", LevelDefault)
}

func TestResolve_UnmappedOriginHostIsQuietOnceBothAreExplicit(t *testing.T) {
	repo := repoWithOrigin(t, "git@git.acme.example:acme/app.git")
	writeRepoPrefs(t, repo, "repo", "- source-control: gitlab", "- source-control-host: git.acme.example")

	if r := resolveAt(t, repo, tempDir(t), "claude"); len(r.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", r.Warnings)
	}
}

func TestResolve_SoftNonHostTrackerBeatsDerived(t *testing.T) {
	home := tempDir(t)
	repo := repoWithOrigin(t, "git@github.com:acme/app.git")
	writeProfile(t, home, "- issue-tracker: linear  # detected")

	want(t, resolveAt(t, repo, home, "claude"), KeyIssueTracker, "linear", LevelProfile)
}

func TestResolve_ExplicitFilesystemTrackerStopsTheFallThrough(t *testing.T) {
	repo := repoWithOrigin(t, "git@github.com:acme/app.git")
	writeRepoPrefs(t, repo, "repo", "- issue-tracker: filesystem")

	want(t, resolveAt(t, repo, tempDir(t), "claude"), KeyIssueTracker, "filesystem", LevelRepo)
}

func TestResolve_SoftHostTrackerIsIgnoredWithAWarning(t *testing.T) {
	home := tempDir(t)
	repo := repoWithOrigin(t, "git@gitlab.com:acme/app.git")
	writeProfile(t, home, "- issue-tracker: github  # detected")

	r := resolveAt(t, repo, home, "claude")

	want(t, r, KeyIssueTracker, "gitlab", LevelDerived)
	if !hasWarning(r.Warnings, "issue-tracker") {
		t.Errorf("warnings = %v, want the soft host tracker warning", r.Warnings)
	}
}

func TestResolve_SourceControlNoneDerivesFilesystemAndNoHost(t *testing.T) {
	repo := repoWithOrigin(t, "")
	writeRepoPrefs(t, repo, "repo", "- source-control: none")

	r := resolveAt(t, repo, tempDir(t), "claude")

	want(t, r, KeyIssueTracker, "filesystem", LevelDerived)
	wantUnset(t, r, KeySourceControlHost)
}

func TestResolve_OutsideARepositoryUsesRealmAndProfileWithOneWarning(t *testing.T) {
	home := tempDir(t)
	dir := tempDir(t)
	writeProfile(t, home, "- source-control: gitlab", "- issue-tracker-project: P1")

	r := resolveAt(t, dir, home, "claude")

	want(t, r, KeySourceControl, "gitlab", LevelProfile)
	want(t, r, KeyIssueTrackerProject, "P1", LevelProfile)
	if len(r.Warnings) != 1 {
		t.Errorf("warnings = %v, want exactly one", r.Warnings)
	}
}

func TestResolve_OutsideARepositoryStillReadsTheRealm(t *testing.T) {
	realm := tempDir(t)
	writeFile(t, filepath.Join(realm, ".atomic", "atomic.toml"), "scope = \"realm\"\n")
	writeRepoPrefs(t, realm, "realm", "- source-control: codeberg")

	want(t, resolveAt(t, realm, tempDir(t), "claude"), KeySourceControl, "codeberg", LevelRealm)
}

func TestResolve_AmbiguousStateRootSkipsTheRepoLevelWithAWarning(t *testing.T) {
	home := tempDir(t)
	repo := ambiguousRepo(t, "")
	writeProfile(t, home, "- source-control: gitlab")

	r, err := Resolve(Options{Dir: repo, Home: home, Harness: "claude"})
	if err != nil {
		t.Fatalf("Resolve must still render: %v", err)
	}
	want(t, r, KeySourceControl, "gitlab", LevelProfile)
	if len(r.Warnings) != 1 {
		t.Errorf("warnings = %v, want exactly one", r.Warnings)
	}
}

func TestResolve_MissingDirectoryCannotRender(t *testing.T) {
	if _, err := Resolve(Options{Dir: filepath.Join(tempDir(t), "nope"), Home: tempDir(t)}); err == nil {
		t.Error("Resolve on a missing directory = nil error")
	}
}

func TestResolve_LevelAttributeMismatchAndMissingWarnLocationDecides(t *testing.T) {
	home := tempDir(t)
	repo := repoWithOrigin(t, "")
	path, _ := config.WorkflowPrefsPath(repo)
	writeFile(t, path, "<workflow-preferences level='realm'>\n- source-control: gitlab\n</workflow-preferences>\n")
	writeFile(t, config.ProfilePath(home),
		"## Workflow preferences\n\n<workflow-preferences>\n- issue-tracker-project: P1\n</workflow-preferences>\n")

	r := resolveAt(t, repo, home, "claude")

	want(t, r, KeySourceControl, "gitlab", LevelRepo)
	want(t, r, KeyIssueTrackerProject, "P1", LevelProfile)
	if len(r.Warnings) != 2 {
		t.Errorf("warnings = %v, want one mismatch and one missing-level", r.Warnings)
	}
}

func TestResolve_RefusedLevelIsSkippedWithAWarning(t *testing.T) {
	home := tempDir(t)
	repo := repoWithOrigin(t, "")
	path, _ := config.WorkflowPrefsPath(repo)
	writeFile(t, path, prefsBlock("repo", "- source-control: gitlab")+prefsBlock("repo", "- source-control: codeberg"))
	writeProfile(t, home, "- source-control: bitbucket")

	r := resolveAt(t, repo, home, "claude")

	want(t, r, KeySourceControl, "bitbucket", LevelProfile)
	if len(r.Warnings) != 1 {
		t.Errorf("warnings = %v, want exactly one refusal warning", r.Warnings)
	}
}

func TestResolve_BlockWarningsNameTheirLevel(t *testing.T) {
	repo := repoWithOrigin(t, "")
	writeRepoPrefs(t, repo, "repo", "- frobnicate: yes")

	r := resolveAt(t, repo, tempDir(t), "claude")

	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "repo") {
		t.Errorf("warnings = %v, want one naming the repo level", r.Warnings)
	}
}

func TestResolve_WorktreesGoThroughTheConfigHelper(t *testing.T) {
	repo := repoWithOrigin(t, "")
	home := tempDir(t)

	want(t, resolveAt(t, repo, home, "claude"), KeyWorktrees, ".claude/worktrees", LevelDefault)
	for _, h := range []string{"omp", "codex", ""} {
		want(t, resolveAt(t, repo, home, h), KeyWorktrees, ".atomic/worktrees", LevelDefault)
	}

	writeRepoPrefs(t, repo, "repo", "- worktrees: trees")
	want(t, resolveAt(t, repo, home, "claude"), KeyWorktrees, "trees", LevelRepo)

	writeRepoPrefs(t, repo, "repo", "- worktrees: off")
	want(t, resolveAt(t, repo, home, "omp"), KeyWorktrees, "off", LevelRepo)
}

// atomic where reports Worktrees and must never disagree with Resolve, which
// atomic workflow resolve prints, for a repository.
func TestWorktrees_AgreesWithResolve(t *testing.T) {
	repo := repoWithOrigin(t, "")
	home := tempDir(t)
	harnesses := []string{"claude", "omp", "codex", ""}
	check := func(want ...string) {
		t.Helper()
		for i, h := range harnesses {
			r := resolveAt(t, repo, home, h)
			v, _ := r.get(KeyWorktrees)
			got := Worktrees(repo, repo, home, h)
			if got != v.Value || got != want[i] {
				t.Errorf("harness %q: Worktrees = %q, Resolve = %q, want %q", h, got, v.Value, want[i])
			}
		}
	}

	check(".claude/worktrees", ".atomic/worktrees", ".atomic/worktrees", ".atomic/worktrees")
	writeProfile(t, home, "- worktrees: ptrees")
	check("ptrees", "ptrees", "ptrees", "ptrees")
	writeRepoPrefs(t, repo, "repo", "- worktrees: trees")
	check("trees", "trees", "trees", "trees")
	writeRepoPrefs(t, repo, "repo", "- worktrees: off")
	check("off", "off", "off", "off")
}

// Outside a repository Resolve skips the repo level and leaves the preference
// unmapped; atomic where maps it against its own cwd root instead.
func TestResolve_WorktreesOutsideARepositoryStayRaw(t *testing.T) {
	dir := tempDir(t)
	home := tempDir(t)
	want(t, resolveAt(t, dir, home, "omp"), KeyWorktrees, "auto", LevelDefault)
	writeProfile(t, home, "- worktrees: ptrees")
	want(t, resolveAt(t, dir, home, "omp"), KeyWorktrees, "ptrees", LevelProfile)
}

// A refused state root must not reach the path helpers, whose backstop exits.
func TestWorktrees_RefusedStateRootReturnsTheRawValue(t *testing.T) {
	repo := ambiguousRepo(t, "")
	home := tempDir(t)
	if got := Worktrees(repo, repo, home, "omp"); got != "auto" {
		t.Errorf("Worktrees = %q, want the unmapped default auto", got)
	}
	writeProfile(t, home, "- worktrees: ptrees")
	if got := Worktrees(repo, repo, home, "omp"); got != "ptrees" {
		t.Errorf("Worktrees = %q, want the unmapped profile value ptrees", got)
	}
}

// atomic where must not spawn git; a git shim on PATH records any call.
func TestWorktrees_SpawnsNoGit(t *testing.T) {
	repo := repoWithOrigin(t, "")
	home := tempDir(t)
	bin := tempDir(t)
	trace := filepath.Join(bin, "called")
	writeFile(t, filepath.Join(bin, "git"), "#!/bin/sh\n/usr/bin/touch "+trace+"\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	for _, h := range []string{"claude", "omp", ""} {
		Worktrees(repo, repo, home, h)
	}
	if _, err := os.Stat(trace); err == nil {
		t.Fatal("Worktrees spawned git")
	}
	OriginURL(repo)
	if _, err := os.Stat(trace); err != nil {
		t.Fatal("git shim never ran, so the check above proves nothing")
	}
}

func TestResolve_ReadListsTheThreeReferences(t *testing.T) {
	home := tempDir(t)
	repo := repoWithOrigin(t, "")
	writeProfile(t, home, "- source-control: gitlab", "- issue-tracker: jira")

	r := resolveAt(t, repo, home, "claude")

	wantRead := []string{
		"references/source-control/gitlab.md",
		"references/issue-tracking/jira.md",
		"references/worktrees.md",
	}
	if strings.Join(r.Read, ",") != strings.Join(wantRead, ",") {
		t.Errorf("Read = %v, want %v", r.Read, wantRead)
	}
}

func TestResolve_CachesNothingBetweenCalls(t *testing.T) {
	home := tempDir(t)
	repo := repoWithOrigin(t, "")
	writeRepoPrefs(t, repo, "repo", "- source-control: gitlab")
	want(t, resolveAt(t, repo, home, "claude"), KeySourceControl, "gitlab", LevelRepo)

	writeRepoPrefs(t, repo, "repo", "- source-control: codeberg")
	want(t, resolveAt(t, repo, home, "claude"), KeySourceControl, "codeberg", LevelRepo)
}
