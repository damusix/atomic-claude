package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func TestSetValue_AtASharedRootRefusesALevelConflict(t *testing.T) {
	cases := []struct {
		name, block  string
		level        Level
		conflict     bool
		wantExisting Level
	}{
		{"repo block, realm requested", "repo", LevelRealm, true, LevelRepo},
		{"realm block, repo requested", "realm", LevelRepo, true, LevelRealm},
		{"missing level reads as repo", "", LevelRealm, true, LevelRepo},
		{"missing level, repo requested", "", LevelRepo, false, ""},
		{"matching level", "realm", LevelRealm, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			open := "<workflow-preferences>"
			if c.block != "" {
				open = "<workflow-preferences level='" + c.block + "'>"
			}
			in := open + "\n- worktrees: off\n</workflow-preferences>\n"
			got, err := SetValue(in, c.level, KeyIssueTracker, "jira", true)
			if c.conflict {
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("level %q", c.wantExisting)) ||
					!strings.Contains(err.Error(), fmt.Sprintf("requested %q", c.level)) {
					t.Errorf("err = %v, want a conflict naming both levels", err)
				}
				return
			}
			if err != nil || !strings.Contains(got, "- issue-tracker: jira\n") {
				t.Errorf("got %q, %v", got, err)
			}
		})
	}
}

func TestSetValue_AMismatchedLevelOutsideASharedRootIsLeftToTheResolver(t *testing.T) {
	in := prefsBlock("realm", "- worktrees: off")
	if _, err := SetValue(in, LevelRepo, KeyIssueTracker, "jira", false); err != nil {
		t.Errorf("err = %v, want none", err)
	}
}

func TestSetValue_DropsLaterDuplicatesOfTheKey(t *testing.T) {
	in := prefsBlock("repo",
		"- issue-tracker: github",
		"- worktrees: off",
		"- issue-tracker: jira  # detected: x")
	got, err := SetValue(in, LevelRepo, KeyIssueTracker, "linear", false)
	if err != nil {
		t.Fatal(err)
	}
	want := prefsBlock("repo", "- issue-tracker: linear", "- worktrees: off")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if b, _ := ParseBlock(got); len(b.Warnings) != 0 {
		t.Errorf("warnings after set: %v", b.Warnings)
	}
}

func TestTargetPath_ReportsASharedRepoAndRealmRoot(t *testing.T) {
	home := tempDir(t)
	repo := repoWithOrigin(t, "")
	writeFile(t, config.RepoConfigPath(repo), "scope = \"realm\"\n")
	for _, level := range []Level{LevelRepo, LevelRealm} {
		_, shared, err := TargetPath(level, repo, home)
		if err != nil || !shared {
			t.Errorf("%s: shared = %v, err = %v; want shared", level, shared, err)
		}
	}
	_, shared, err := TargetPath(LevelRepo, repoWithOrigin(t, ""), home)
	if err != nil || shared {
		t.Errorf("plain repo: shared = %v, err = %v", shared, err)
	}
	if _, shared, _ = TargetPath(LevelProfile, repo, home); shared {
		t.Error("profile is never shared")
	}
}

func TestTargetPath_PicksTheFileForEachLevel(t *testing.T) {
	home := tempDir(t)
	repo := repoWithOrigin(t, "")
	wantRepo, err := config.WorkflowPrefsPath(repo)
	if err != nil {
		t.Fatal(err)
	}

	got, _, err := TargetPath(LevelRepo, repo, home)
	if err != nil || got != wantRepo {
		t.Errorf("repo: %q, %v; want %q", got, err, wantRepo)
	}
	got, _, err = TargetPath(LevelProfile, repo, home)
	if err != nil || got != config.ProfilePath(home) {
		t.Errorf("profile: %q, %v; want %q", got, err, config.ProfilePath(home))
	}

	realm := tempDir(t)
	writeFile(t, config.RepoConfigPath(realm), "scope = \"realm\"\n")
	wantRealm, err := config.WorkflowPrefsPath(realm)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err = TargetPath(LevelRealm, realm, home)
	if err != nil || got != wantRealm {
		t.Errorf("realm: %q, %v; want %q", got, err, wantRealm)
	}
}

func TestTargetPath_NoLocationOutsideARepositoryOrRealm(t *testing.T) {
	home, dir := tempDir(t), tempDir(t)
	for _, level := range []Level{LevelRepo, LevelRealm} {
		if _, _, err := TargetPath(level, dir, home); !errors.Is(err, ErrNoLocation) {
			t.Errorf("%s: err = %v, want ErrNoLocation", level, err)
		}
	}
}

func TestTargetPath_AmbiguousStateRootIsAnErrorNotAMissingLocation(t *testing.T) {
	repo := ambiguousRepo(t, "")
	_, _, err := TargetPath(LevelRepo, repo, tempDir(t))
	if err == nil || errors.Is(err, ErrNoLocation) {
		t.Errorf("err = %v, want the state-root refusal", err)
	}
}

func TestTargetPath_PopulatedLegacyRootIsUsedInPlace(t *testing.T) {
	repo := repoWithOrigin(t, "")
	writeFile(t, filepath.Join(repo, ".claude", "project", "followups", "a.md"), "x\n")
	got, _, err := TargetPath(LevelRepo, repo, tempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(repo, ".claude", "workflow-preferences.md"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Setting a profile value must not depend on where the command runs, so it
// never consults Git.
func TestTargetPath_ProfileLevelNeverRunsGit(t *testing.T) {
	bin, marker := tempDir(t), filepath.Join(tempDir(t), "git-ran")
	writeFile(t, filepath.Join(bin, "git"), "#!/bin/sh\n: > "+marker+"\nexit 1\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	home := tempDir(t)
	got, _, err := TargetPath(LevelProfile, tempDir(t), home)
	if err != nil || got != config.ProfilePath(home) {
		t.Errorf("got %q, %v; want %q", got, err, config.ProfilePath(home))
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("profile target ran git")
	}
}

func TestTargetPath_ASubdirectoryNamesItsGitRoot(t *testing.T) {
	repo := repoWithOrigin(t, "")
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	want, err := config.WorkflowPrefsPath(repo)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := TargetPath(LevelRepo, sub, tempDir(t))
	if err != nil || got != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
}

func TestSetValue_ReplacesTheKeyLineAndKeepsEveryOtherByte(t *testing.T) {
	in := "# Prefs\n\nprose\n" +
		"<workflow-preferences level='repo'>\n" +
		"- source-control: gitlab  # detected: glab authed\n" +
		"- mystery: 1\n" +
		"\n" +
		"- worktrees: off\n" +
		"</workflow-preferences>\ntrailing\n"
	got, err := SetValue(in, LevelRepo, KeySourceControl, "codeberg", false)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(in,
		"- source-control: gitlab  # detected: glab authed",
		"- source-control: codeberg", 1)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	b, err := ParseBlock(got)
	if err != nil {
		t.Fatal(err)
	}
	if e := b.Values[KeySourceControl]; e.Soft || e.Value != "codeberg" {
		t.Errorf("entry = %+v, want explicit codeberg", e)
	}
}

func TestSetValue_AddsAMissingKeyBeforeTheCloseTag(t *testing.T) {
	in := "<workflow-preferences level='realm'>\n- worktrees: off\n</workflow-preferences>\n"
	got, err := SetValue(in, LevelRealm, KeyIssueTracker, "linear", false)
	if err != nil {
		t.Fatal(err)
	}
	want := "<workflow-preferences level='realm'>\n- worktrees: off\n" +
		"- issue-tracker: linear\n</workflow-preferences>\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetValue_CreatesTheBlockWithTheLevelAttribute(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"empty file", "",
			"<workflow-preferences level='repo'>\n- worktrees: off\n</workflow-preferences>\n"},
		{"prose without trailing newline", "notes",
			"notes\n\n<workflow-preferences level='repo'>\n- worktrees: off\n</workflow-preferences>\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SetValue(c.in, LevelRepo, KeyWorktrees, "off", false)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSetValue_KeepsCRLF(t *testing.T) {
	in := "<workflow-preferences level='repo'>\r\n- worktrees: off\r\n</workflow-preferences>\r\n"
	got, err := SetValue(in, LevelRepo, KeyIssueTracker, "jira", false)
	if err != nil {
		t.Fatal(err)
	}
	want := "<workflow-preferences level='repo'>\r\n- worktrees: off\r\n" +
		"- issue-tracker: jira\r\n</workflow-preferences>\r\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetValue_RefusesADuplicateOrUnpairedBlock(t *testing.T) {
	block := prefsBlock("repo", "- worktrees: off")
	for name, in := range map[string]string{
		"two blocks": block + block,
		"unpaired":   "<workflow-preferences level='repo'>\n- worktrees: off\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := SetValue(in, LevelRepo, KeyIssueTracker, "jira", false)
			if err == nil {
				t.Fatalf("want a refusal, got %q", got)
			}
			if errors.Is(err, ErrInvalidSetting) {
				t.Errorf("a refusal is not a usage error: %v", err)
			}
		})
	}
}

func TestValidateSetting_RejectsWhatTheParserWouldRead(t *testing.T) {
	bad := []struct{ key, value string }{
		{"source-contol", "github"},
		{"source-control", "svn"},
		{"source-control", ""},
		{"source-control-host", "not a host"},
		{"source-control-host", "a..b"},
		{"source-control-host", "a.-b"},
		{"source-control-host", "a-.b"},
		{"source-control-host", "a.b."},
		{"source-control-host", strings.Repeat("a", 64) + ".example"},
		{"source-control-host", strings.Repeat(strings.Repeat("a", 63)+".", 4) + "ab"},
		{"issue-tracker", "bitbucket"},
		{"issue-tracker-project", "two words"},
		{"issue-tracker-project", "A#B"},
		{"worktrees", "../escape"},
		{"worktrees", ""},
		{"worktrees", "a\nb"},
		{"worktrees", " /abs"},
		{"worktrees", "off "},
		{"issue-tracker-project", "\tACME"},
	}
	for _, c := range bad {
		if err := ValidateSetting(c.key, c.value); !errors.Is(err, ErrInvalidSetting) {
			t.Errorf("%s=%q: err = %v, want ErrInvalidSetting", c.key, c.value, err)
		}
	}
	good := []struct{ key, value string }{
		{"source-control", "none"},
		{"source-control-host", "git.acme.example"},
		{"source-control-host", "localhost"},
		{"source-control-host", "my-git.acme-corp.example"},
		{"source-control-host", strings.Repeat("a", 63) + ".example"},
		{"issue-tracker", "filesystem"},
		{"issue-tracker-project", "ACME-BILLING"},
		{"worktrees", "auto"},
		{"worktrees", ".trees/wt"},
	}
	for _, c := range good {
		if err := ValidateSetting(c.key, c.value); err != nil {
			t.Errorf("%s=%q: %v", c.key, c.value, err)
		}
	}
}
