package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// legacyRepo is a committed repository whose Atomic state still lives in
// .claude beside Claude-native entries that must stay.
func legacyRepo(t *testing.T) string {
	t.Helper()
	withHome(t)
	resetStateResolutionCacheForTest()
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	for path, data := range map[string]string{
		".claude/atomic.toml":                 "scope = \"repo\"\n",
		".claude/workflow-preferences.md":     "- worktrees: off\n",
		".claude/project/followups/a.md":      "a\n",
		".claude/rules/wiki/config.md":        "card\n",
		".claude/rules/contributor.md":        "stays\n",
		".claude/settings.json":               "{}\n",
		".claude/.gitignore":                  "/.scratchpad/\n/worktrees/\n",
		".claude/.scratchpad/loop/BRIEF.md":   "ignored\n",
		".claude/worktrees/feature/README.md": "linked\n",
	} {
		writeFile(t, filepath.Join(repo, filepath.FromSlash(path)), data)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "init")
	return repo
}

func stagedNameStatus(t *testing.T, repo string) string {
	t.Helper()
	return runGit(t, repo, "diff", "--cached", "--name-status", "-M")
}

func TestMigrateRepositoryState_StagesTrackedRenamesAndMovesIgnored(t *testing.T) {
	repo := legacyRepo(t)

	report, err := MigrateRepositoryState(repo)
	if err != nil {
		t.Fatalf("MigrateRepositoryState: %v", err)
	}

	staged := stagedNameStatus(t, repo)
	for _, want := range []string{
		"R100\t.claude/atomic.toml\t.atomic/atomic.toml",
		"R100\t.claude/workflow-preferences.md\t.atomic/workflow-preferences.md",
		"R100\t.claude/project/followups/a.md\t.atomic/project/followups/a.md",
		"R100\t.claude/rules/wiki/config.md\t.atomic/rules/wiki/config.md",
		"A\t.atomic/.gitignore",
	} {
		if !strings.Contains(staged, want) {
			t.Errorf("staged changes lack %q:\n%s", want, staged)
		}
	}
	if len(report.Staged) != 4 {
		t.Errorf("report staged %v, want the four tracked renames", report.Staged)
	}
	if !exists(filepath.Join(repo, ".atomic/.scratchpad/loop/BRIEF.md")) {
		t.Error("the ignored scratchpad bundle did not move")
	}
	for _, kept := range []string{".claude/rules/contributor.md", ".claude/settings.json", ".claude/.gitignore", ".claude/worktrees/feature/README.md"} {
		if !exists(filepath.Join(repo, kept)) {
			t.Errorf("%s moved; harness-native entries stay", kept)
		}
	}
	ignore, _ := os.ReadFile(filepath.Join(repo, ".atomic/.gitignore"))
	for _, rule := range []string{"/.scratchpad/", "/.atomic-index/", "/worktrees/", "/project/doc-surfaces.md"} {
		if !strings.Contains(string(ignore), rule+"\n") {
			t.Errorf(".atomic/.gitignore lacks %s:\n%s", rule, ignore)
		}
	}
	if got := strings.TrimSpace(runGit(t, repo, "rev-list", "--count", "HEAD")); got != "1" {
		t.Errorf("commit count = %s; migration must not commit", got)
	}
	if exists(StateLocationRecordPath(repo)) {
		t.Error("migration wrote state-location.json")
	}
}

// A rename git cannot perform in place still lands, by copy and remove.
func TestMigrateRepositoryState_CrossDeviceCopiesAndRemoves(t *testing.T) {
	repo := legacyRepo(t)
	t.Cleanup(setRenameStateForTest(func(oldpath, newpath string) error {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
	}))

	if _, err := MigrateRepositoryState(repo); err != nil {
		t.Fatalf("MigrateRepositoryState: %v", err)
	}
	if exists(filepath.Join(repo, ".claude/project")) || !exists(filepath.Join(repo, ".atomic/project/followups/a.md")) {
		t.Error("cross-device move did not copy and remove project/")
	}
	if !strings.Contains(stagedNameStatus(t, repo), "R100\t.claude/project/followups/a.md\t.atomic/project/followups/a.md") {
		t.Error("cross-device move lost the staged rename")
	}
}

// The user chose the legacy root by record or state.dir, so nothing moves.
func TestMigrateRepositoryState_LegacyDestinationMovesNothing(t *testing.T) {
	for _, how := range []string{"record", "state.dir"} {
		t.Run(how, func(t *testing.T) {
			repo := legacyRepo(t)
			if how == "record" {
				if _, err := PersistStateSelection(repo, ".claude"); err != nil {
					t.Fatal(err)
				}
			} else {
				writeStateDirConfig(t, projectHome(), ".claude")
			}
			report, err := MigrateRepositoryState(repo)
			if err != nil {
				t.Fatalf("MigrateRepositoryState: %v", err)
			}
			if len(report.Moved) != 0 || exists(filepath.Join(repo, ".atomic")) {
				t.Errorf("moved %v; a chosen legacy root stays", report.Moved)
			}
		})
	}
}

func TestMigrateRepositoryState_DestinationPrecedence(t *testing.T) {
	t.Run("record outranks state.dir", func(t *testing.T) {
		repo := legacyRepo(t)
		writeStateDirConfig(t, projectHome(), ".state")
		if _, err := PersistStateSelection(repo, "chosen"); err != nil {
			t.Fatal(err)
		}
		if _, err := MigrateRepositoryState(repo); err != nil {
			t.Fatal(err)
		}
		if !exists(filepath.Join(repo, "chosen/project")) {
			t.Error("project/ did not move to the recorded root")
		}
	})
	t.Run("state.dir", func(t *testing.T) {
		repo := legacyRepo(t)
		writeStateDirConfig(t, projectHome(), ".state")
		if _, err := MigrateRepositoryState(repo); err != nil {
			t.Fatal(err)
		}
		if !exists(filepath.Join(repo, ".state/project")) {
			t.Error("project/ did not move to state.dir")
		}
	})
	t.Run("ATOMIC_STATE_DIR is never a destination", func(t *testing.T) {
		repo := legacyRepo(t)
		t.Setenv(StateDirEnvVar, "elsewhere")
		if _, err := MigrateRepositoryState(repo); err != nil {
			t.Fatal(err)
		}
		if exists(filepath.Join(repo, "elsewhere")) || !exists(filepath.Join(repo, ".atomic/project")) {
			t.Error("migration followed ATOMIC_STATE_DIR instead of the default")
		}
	})
}

// A realm root outside Git migrates with plain renames; federation files stay
// under an .atomic destination and move to any other.
func TestMigrateRepositoryState_NonGitRealm(t *testing.T) {
	setup := func(t *testing.T) string {
		withHome(t)
		resetStateResolutionCacheForTest()
		realm := t.TempDir()
		writeFile(t, filepath.Join(realm, ".claude/atomic.toml"), "scope = \"realm\"\n")
		writeFile(t, filepath.Join(realm, ".atomic/code.toml"), "[[member]]\n")
		writeFile(t, filepath.Join(realm, ".atomic/app.db"), "db")
		return realm
	}
	t.Run(".atomic destination", func(t *testing.T) {
		realm := setup(t)
		if _, err := MigrateRepositoryState(realm); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{".atomic/atomic.toml", ".atomic/code.toml", ".atomic/app.db"} {
			if !exists(filepath.Join(realm, p)) {
				t.Errorf("%s missing after migration", p)
			}
		}
	})
	t.Run("custom destination", func(t *testing.T) {
		realm := setup(t)
		writeStateDirConfig(t, projectHome(), ".state")
		if _, err := MigrateRepositoryState(realm); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{".state/atomic.toml", ".state/code.toml", ".state/app.db"} {
			if !exists(filepath.Join(realm, p)) {
				t.Errorf("%s missing after migration", p)
			}
		}
		if exists(filepath.Join(realm, ".atomic/code.toml")) {
			t.Error("federation files stayed under .atomic beside a custom destination")
		}
	})
}

// daemonLive installs the liveness seam for one test; nil leaves it unwired. A
// not-in-use answer hands back a release the migration must call.
func daemonLive(t *testing.T, live func(indexDir string) bool) {
	t.Helper()
	prev := IndexInUse
	IndexInUse = nil
	if live != nil {
		IndexInUse = func(dir string) (bool, func(), error) { return live(dir), func() {}, nil }
	}
	t.Cleanup(func() { IndexInUse = prev })
}

// A liveness check that fails must stop the migration, not read as idle.
func TestMigrateRepositoryState_LegacyIndexCheckFailureIsReported(t *testing.T) {
	repo := legacyRepo(t)
	index := filepath.Join(repo, ".claude/.atomic-index")
	writeFile(t, filepath.Join(index, "atomic.db"), "db")
	prev := IndexInUse
	IndexInUse = func(string) (bool, func(), error) { return true, nil, errors.New("lock unreadable") }
	t.Cleanup(func() { IndexInUse = prev })

	if _, err := MigrateRepositoryState(repo); err == nil || !strings.Contains(err.Error(), "lock unreadable") {
		t.Errorf("err = %v, want the liveness failure", err)
	}
	if !exists(index) {
		t.Error("the index was removed despite the failed liveness check")
	}
}

// The legacy index is removed while the caller's hold is still in place, and
// released only afterwards, so a starter cannot respawn into the gap.
func TestMigrateRepositoryState_LegacyIndexRemovedUnderHeldLock(t *testing.T) {
	repo := legacyRepo(t)
	index := filepath.Join(repo, ".claude/.atomic-index")
	writeFile(t, filepath.Join(index, "atomic.db"), "db")
	var existedAtRelease, released bool
	prev := IndexInUse
	IndexInUse = func(string) (bool, func(), error) {
		return false, func() { released, existedAtRelease = true, exists(index) }, nil
	}
	t.Cleanup(func() { IndexInUse = prev })

	if _, err := MigrateRepositoryState(repo); err != nil {
		t.Fatal(err)
	}
	if !released {
		t.Fatal("the hold was never released")
	}
	if existedAtRelease {
		t.Error("the hold was released before the index was removed")
	}
}

func TestMigrateRepositoryState_LegacyIndex(t *testing.T) {
	cases := []struct {
		name   string
		live   func(string) bool
		remove bool
	}{
		{"no daemon removes it", func(string) bool { return false }, true},
		{"live daemon keeps and reports it", func(string) bool { return true }, false},
		{"unwired liveness keeps and reports it", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := legacyRepo(t)
			index := filepath.Join(repo, ".claude/.atomic-index")
			writeFile(t, filepath.Join(index, "atomic.db"), "db")
			var asked string
			live := tc.live
			if live != nil {
				live = func(dir string) bool { asked = dir; return tc.live(dir) }
			}
			daemonLive(t, live)
			report, err := MigrateRepositoryState(repo)
			if err != nil {
				t.Fatal(err)
			}
			if tc.remove {
				if exists(index) || report.IndexRemoved != index {
					t.Errorf("legacy index kept (report %+v)", report)
				}
			} else if !exists(index) || report.IndexInUse != index {
				t.Errorf("index removed or unreported (report %+v)", report)
			}
			if tc.live != nil && asked != index {
				t.Errorf("liveness asked about %q, want %q", asked, index)
			}
		})
	}
}

func TestMigrateRepositoryState_BothPopulated(t *testing.T) {
	t.Run("legacy state refuses", func(t *testing.T) {
		repo := legacyRepo(t)
		writeFile(t, filepath.Join(repo, ".atomic/project/followups/b.md"), "b\n")
		_, err := MigrateRepositoryState(repo)
		if !errors.Is(err, ErrStateMigrationConflict) {
			t.Fatalf("err = %v, want ErrStateMigrationConflict", err)
		}
		if !strings.Contains(err.Error(), filepath.Join(repo, ".claude/project")) {
			t.Errorf("error %q must name the leftover .claude entries", err)
		}
		if !exists(filepath.Join(repo, ".claude/project/followups/a.md")) {
			t.Error("a refused migration moved files")
		}
	})
	for _, held := range []bool{false, true} {
		name := map[bool]string{false: "index only, no daemon", true: "index only, live daemon"}[held]
		t.Run(name, func(t *testing.T) {
			withHome(t)
			resetStateResolutionCacheForTest()
			repo := t.TempDir()
			index := filepath.Join(repo, ".claude/.atomic-index")
			writeFile(t, filepath.Join(index, "atomic.db"), "db")
			populate(t, repo, ".atomic")
			daemonLive(t, func(string) bool { return held })
			report, err := MigrateRepositoryState(repo)
			if err != nil {
				t.Fatalf("index-only leftover: %v", err)
			}
			if held != exists(index) {
				t.Errorf("index exists = %v with lock held = %v", exists(index), held)
			}
			if held && report.IndexInUse != index {
				t.Errorf("held index not reported: %+v", report)
			}
		})
	}
}

func TestMigrateRepositoryState_ReportsHandWiredReferences(t *testing.T) {
	repo := legacyRepo(t)
	steering := "# Repo\n\n@.claude/project/followups/INDEX.md\n"
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), steering)
	writeFile(t, filepath.Join(repo, ".githooks/pre-commit"), "#!/bin/sh\nls .claude/project/\n")

	report, err := MigrateRepositoryState(repo)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(report.References, " ")
	for _, want := range []string{"CLAUDE.md:3", ".githooks/pre-commit:2"} {
		if !strings.Contains(got, want) {
			t.Errorf("references %v lack %s", report.References, want)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "CLAUDE.md")); string(data) != steering {
		t.Error("a hand-wired reference was rewritten")
	}
}

func TestMigrateRepositoryState_InvalidatesResolutionCache(t *testing.T) {
	repo := legacyRepo(t)
	before, err := RepositoryStateDir(repo)
	if err != nil || before != filepath.Join(repo, ".claude") {
		t.Fatalf("before = %s, %v; want the legacy root", before, err)
	}
	if _, err := MigrateRepositoryState(repo); err != nil {
		t.Fatal(err)
	}
	if after, _ := RepositoryStateDir(repo); after != filepath.Join(repo, ".atomic") {
		t.Errorf("after migration the cached root is %s, want .atomic", after)
	}
}

func TestRepoStateMigrationPending(t *testing.T) {
	repo := legacyRepo(t)
	if pending, err := RepoStateMigrationPending(repo); err != nil || !pending {
		t.Fatalf("lone legacy root: pending = %v, %v; want true", pending, err)
	}
	populate(t, repo, ".atomic")
	if pending, _ := RepoStateMigrationPending(repo); pending {
		t.Error("a populated destination is not pending")
	}
}

// A linked worktree on an unmigrated branch keeps its own legacy root when
// the main checkout migrates.
func TestMigrateRepositoryState_LinkedWorktreeKeepsItsLegacyRoot(t *testing.T) {
	repo := legacyRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	runGit(t, repo, "worktree", "add", "-q", "-b", "old", linked)

	if _, err := MigrateRepositoryState(repo); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(linked, ".claude/project/followups/a.md")) {
		t.Fatal("migrating the main checkout moved the linked worktree's state")
	}
	if got, err := RepositoryStateDir(linked); err != nil || got != filepath.Join(linked, ".claude") {
		t.Errorf("linked worktree resolves %s, %v; want its own .claude", got, err)
	}
}

// A scope-marker root below the work tree's top stages its renames with
// top-relative paths.
func TestMigrateRepositoryState_MarkerRootInSubdirectory(t *testing.T) {
	withHome(t)
	resetStateResolutionCacheForTest()
	top := t.TempDir()
	runGit(t, top, "init", "-q")
	writeFile(t, filepath.Join(top, "sub/.claude/atomic.toml"), "scope = \"repo\"\n")
	writeFile(t, filepath.Join(top, "sub/.claude/project/followups/a.md"), "a\n")
	runGit(t, top, "add", ".")
	runGit(t, top, "commit", "-qm", "init")

	if _, err := MigrateRepositoryState(filepath.Join(top, "sub")); err != nil {
		t.Fatal(err)
	}
	staged := stagedNameStatus(t, top)
	for _, want := range []string{
		"R100\tsub/.claude/project/followups/a.md\tsub/.atomic/project/followups/a.md",
		"A\tsub/.atomic/.gitignore",
	} {
		if !strings.Contains(staged, want) {
			t.Errorf("staged changes lack %q:\n%s", want, staged)
		}
	}
}

// A realm root that is not its own repository moves with plain renames and
// stages nothing into the repository that encloses it.
func TestMigrateRepositoryState_NonGitRealmUnderEnclosingRepo(t *testing.T) {
	withHome(t)
	resetStateResolutionCacheForTest()
	top := t.TempDir()
	runGit(t, top, "init", "-q")
	writeFile(t, filepath.Join(top, "realm/.claude/atomic.toml"), "scope = \"realm\"\n")
	writeFile(t, filepath.Join(top, "realm/.claude/project/x.md"), "x\n")
	runGit(t, top, "add", ".")
	runGit(t, top, "commit", "-qm", "init")

	if _, err := MigrateRepositoryState(filepath.Join(top, "realm")); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(top, "realm/.atomic/project/x.md")) {
		t.Error("realm state did not move")
	}
	if staged := stagedNameStatus(t, top); staged != "" {
		t.Errorf("staged into the enclosing repository:\n%s", staged)
	}
}

// A failure partway reverses the moves already made, so a re-run starts from
// the legacy root.
func TestMigrateRepositoryState_FailurePartwayRollsBack(t *testing.T) {
	repo := legacyRepo(t)
	calls := 0
	t.Cleanup(setRenameStateForTest(func(oldpath, newpath string) error {
		calls++
		if calls == 3 {
			return errors.New("injected failure")
		}
		return os.Rename(oldpath, newpath)
	}))

	if _, err := MigrateRepositoryState(repo); err == nil {
		t.Fatal("the injected failure was not returned")
	}
	for _, p := range []string{".claude/atomic.toml", ".claude/workflow-preferences.md", ".claude/project/followups/a.md", ".claude/rules/wiki/config.md", ".claude/.scratchpad/loop/BRIEF.md"} {
		if !exists(filepath.Join(repo, p)) {
			t.Errorf("%s not restored", p)
		}
	}
	if exists(filepath.Join(repo, ".atomic")) {
		t.Error(".atomic left behind after rollback")
	}
	if staged := stagedNameStatus(t, repo); staged != "" {
		t.Errorf("index not restored:\n%s", staged)
	}
	if _, err := MigrateRepositoryState(repo); err != nil {
		t.Fatalf("re-run after rollback: %v", err)
	}
}

func TestMigrateRepositoryState_RefusesHomeDirectory(t *testing.T) {
	home := withHome(t)
	resetStateResolutionCacheForTest()
	writeFile(t, filepath.Join(home, ".claude/project/x.md"), "x\n")

	if _, err := MigrateRepositoryState(home); !errors.Is(err, ErrHomeStateRoot) {
		t.Errorf("migrate err = %v, want ErrHomeStateRoot", err)
	}
	if _, err := RepoStateMigrationPending(home); !errors.Is(err, ErrHomeStateRoot) {
		t.Errorf("pending err = %v, want ErrHomeStateRoot", err)
	}
	if !exists(filepath.Join(home, ".claude/project/x.md")) {
		t.Error("the home directory's .claude moved")
	}
}

func TestMigrateRepositoryState_HomeWithoutStateIsNotRefused(t *testing.T) {
	home := withHome(t)
	resetStateResolutionCacheForTest()
	writeFile(t, filepath.Join(home, ".claude/settings.json"), "{}\n")

	if pending, err := RepoStateMigrationPending(home); err != nil || pending {
		t.Errorf("pending = %v, err = %v; want false, nil", pending, err)
	}
	report, err := MigrateRepositoryState(home)
	if err != nil || len(report.Moved) > 0 {
		t.Errorf("report %+v, err %v; want nothing moved and no error", report, err)
	}
	if exists(filepath.Join(home, ".atomic")) {
		t.Error("a home directory with no state gained .atomic")
	}
}

// An index alone is rebuildable, so it is no reason to migrate.
func TestMigrateRepositoryState_IndexOnlyLegacyIsNotPending(t *testing.T) {
	withHome(t)
	resetStateResolutionCacheForTest()
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	index := filepath.Join(repo, ".claude/.atomic-index")
	writeFile(t, filepath.Join(index, "atomic.db"), "db")
	daemonLive(t, func(string) bool { return false })

	if pending, err := RepoStateMigrationPending(repo); err != nil || pending {
		t.Errorf("pending = %v, %v; want false", pending, err)
	}
	report, err := MigrateRepositoryState(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !exists(index) || exists(filepath.Join(repo, ".atomic")) || report.IndexRemoved != "" {
		t.Errorf("an index-only legacy root was migrated (report %+v)", report)
	}
	if staged := stagedNameStatus(t, repo); staged != "" {
		t.Errorf("staged:\n%s", staged)
	}
}

// git mv refuses or mishandles gitlinks and unmerged paths, so the migration
// refuses them before anything moves.
func TestMigrateRepositoryState_RefusesGitlinkAndUnmergedEntries(t *testing.T) {
	t.Run("gitlink", func(t *testing.T) {
		repo := legacyRepo(t)
		head := strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD"))
		runGit(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+head+",.claude/project/vendored")
		assertRefusedUnmoved(t, repo, ".claude/project/vendored")
	})
	t.Run("unmerged", func(t *testing.T) {
		repo := legacyRepo(t)
		blob := strings.TrimSpace(runGit(t, repo, "hash-object", "-w", ".claude/atomic.toml"))
		cmd := exec.Command("git", "update-index", "--index-info")
		cmd.Dir = repo
		cmd.Stdin = strings.NewReader("0 " + strings.Repeat("0", len(blob)) + "\t.claude/atomic.toml\n" +
			"100644 " + blob + " 2\t.claude/atomic.toml\n" +
			"100644 " + blob + " 3\t.claude/atomic.toml\n")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("update-index: %v\n%s", err, out)
		}
		assertRefusedUnmoved(t, repo, ".claude/atomic.toml")
	})
}

func assertRefusedUnmoved(t *testing.T, repo, entry string) {
	t.Helper()
	_, err := MigrateRepositoryState(repo)
	if err == nil || !strings.Contains(err.Error(), entry) {
		t.Fatalf("err = %v, want a refusal naming %s", err, entry)
	}
	if !exists(filepath.Join(repo, ".claude/project/followups/a.md")) || exists(filepath.Join(repo, ".atomic")) {
		t.Error("a refused migration moved files")
	}
}

// The cross-device copy keeps symlinks as links, at the top level and inside
// a tree.
func TestMigrateRepositoryState_CrossDeviceKeepsSymlinks(t *testing.T) {
	repo := legacyRepo(t)
	if err := os.Symlink("followups/a.md", filepath.Join(repo, ".claude/project/latest")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, ".claude/workflow-preferences.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../prefs.md", filepath.Join(repo, ".claude/workflow-preferences.md")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(setRenameStateForTest(func(oldpath, newpath string) error {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
	}))

	if _, err := MigrateRepositoryState(repo); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		".atomic/project/latest":          "followups/a.md",
		".atomic/workflow-preferences.md": "../prefs.md",
	} {
		if got, err := os.Readlink(filepath.Join(repo, link)); err != nil || got != target {
			t.Errorf("%s -> %q (%v), want a link to %q", link, got, err, target)
		}
	}
}
