package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/mcp"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// TestStateAdoptSelectsTheOnlyPopulatedRoot proves the unambiguous case records
// the existing root rather than creating or moving one.
func TestStateAdoptSelectsTheOnlyPopulatedRoot(t *testing.T) {
	repo := t.TempDir()
	segment := builtinStateDir(t, repo)
	markStateRoot(t, repo, segment)

	report, err := stateAdopt(stateOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("stateAdopt: %v", err)
	}
	if report.Action != "adopt" || report.StateDir != segment {
		t.Fatalf("report = %+v, want the %s root adopted", report, segment)
	}
	selection, ok, err := config.ReadStateSelection(repo)
	if err != nil || !ok {
		t.Fatalf("selection = %v, %v, ok=%v", selection, err, ok)
	}
	if selection.StateDir != segment {
		t.Errorf("recorded state_dir = %q, want %s", selection.StateDir, segment)
	}
}

// TestStateAdoptDryRunWritesNothing proves the planning form is byte-identical.
func TestStateAdoptDryRunWritesNothing(t *testing.T) {
	repo := t.TempDir()
	markStateRoot(t, repo, builtinStateDir(t, repo))

	report, err := stateAdopt(stateOptions{RepoRoot: repo, DryRun: true})
	if err != nil {
		t.Fatalf("stateAdopt: %v", err)
	}
	if report.Action != "adopt" {
		t.Errorf("action = %q, want adopt", report.Action)
	}
	if _, err := os.Stat(config.StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Errorf("dry run wrote the selection record (stat err = %v)", err)
	}
}

// TestStateAdoptLegacyClaudeRootInPlace proves a populated `.claude` root is
// adopted where it sits and nothing moves into `.atomic`.
func TestStateAdoptLegacyClaudeRootInPlace(t *testing.T) {
	repo := t.TempDir()
	builtinStateDir(t, repo)
	markStateRoot(t, repo, ".claude")

	report, err := stateAdopt(stateOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("stateAdopt: %v", err)
	}
	if report.Action != "adopt" || report.StateDir != ".claude" {
		t.Fatalf("report = %+v, want the .claude root adopted", report)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude", "project", "signals.md")); err != nil {
		t.Errorf("adoption moved the legacy state: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".atomic")); !os.IsNotExist(err) {
		t.Errorf("adoption created .atomic (stat err = %v)", err)
	}
	loc, err := config.ResolveStateLocation(repo)
	if err != nil || loc.Root != filepath.Join(repo, ".claude") || loc.Source != config.StateSourceRecord {
		t.Errorf("resolution after adopt = %+v, %v; want the recorded .claude root", loc, err)
	}
}

// TestStateAdoptPopulatedDestinationBesideLegacy proves a populated `.atomic`
// is adopted without a choice even when `.claude` also holds state.
func TestStateAdoptPopulatedDestinationBesideLegacy(t *testing.T) {
	repo := t.TempDir()
	segment := builtinStateDir(t, repo)
	markStateRoot(t, repo, segment)
	markStateRoot(t, repo, ".claude")

	report, err := stateAdopt(stateOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("stateAdopt: %v", err)
	}
	if report.Action != "adopt" || report.StateDir != segment {
		t.Fatalf("report = %+v, want the %s destination adopted", report, segment)
	}
}

// TestStateAdoptDirPersistsPathsLinkedWorktreesResolve proves a linked worktree
// resolves a relative --dir under its own root, and an absolute one as-is.
func TestStateAdoptDirPersistsPathsLinkedWorktreesResolve(t *testing.T) {
	mainRoot := t.TempDir()
	builtinStateDir(t, mainRoot)
	linked := linkedWorktree(t, mainRoot)

	if _, err := stateAdopt(stateOptions{RepoRoot: mainRoot, Dir: "state/atomic"}); err != nil {
		t.Fatalf("relative multi-segment --dir: %v", err)
	}
	for _, root := range []string{mainRoot, linked} {
		loc, err := config.ResolveStateLocation(root)
		if err != nil {
			t.Fatalf("ResolveStateLocation(%s): %v", root, err)
		}
		if want := filepath.Join(root, "state", "atomic"); loc.Root != want || loc.Source != config.StateSourceRecord {
			t.Errorf("resolution from %s = %+v, want %s (record)", root, loc, want)
		}
	}

	abs := filepath.Join(t.TempDir(), "elsewhere")
	if _, err := stateAdopt(stateOptions{RepoRoot: mainRoot, Dir: abs}); err != nil {
		t.Fatalf("absolute --dir: %v", err)
	}
	for _, root := range []string{mainRoot, linked} {
		if loc, err := config.ResolveStateLocation(root); err != nil || loc.Root != abs {
			t.Errorf("resolution from %s = %+v, %v; want %s", root, loc, err, abs)
		}
	}

	for _, bad := range []string{"../escape", "a/../../b"} {
		if _, err := stateAdopt(stateOptions{RepoRoot: mainRoot, Dir: bad}); err == nil {
			t.Errorf("--dir %q was accepted", bad)
		}
	}
}

// linkedWorktree shares mainRoot's project key, as a git linked worktree does.
func linkedWorktree(t *testing.T, mainRoot string) string {
	t.Helper()
	gitDir := filepath.Join(mainRoot, ".git", "worktrees", "wt")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linked := t.TempDir()
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return linked
}

// TestStateAdoptRefusesAmbiguousRoots proves two populated non-destination
// candidates require an explicit choice, and --dir still makes it.
func TestStateAdoptRefusesAmbiguousRoots(t *testing.T) {
	repo := t.TempDir()
	segment := builtinStateDir(t, repo)
	home := t.TempDir()
	t.Cleanup(config.SetHomeDirForTest(home))
	cfg := config.Default()
	if err := config.Set(cfg, "state.dir", ".state"); err != nil {
		t.Fatal(err)
	}
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}
	markStateRoot(t, repo, segment)
	markStateRoot(t, repo, ".claude")

	if _, err := stateAdopt(stateOptions{RepoRoot: repo}); err == nil {
		t.Fatal("ambiguous state roots were adopted without a choice")
	}
	report, err := stateAdopt(stateOptions{RepoRoot: repo, Dir: ".claude"})
	if err != nil {
		t.Fatalf("explicit selection: %v", err)
	}
	if report.Action != "select" || report.StateDir != ".claude" {
		t.Fatalf("report = %+v", report)
	}
}

// TestStatefulVerbRefusesInvalidStateDirEnv proves a bad ATOMIC_STATE_DIR stops
// the verb before it writes, so no `.atomic` appears to orphan the real root.
func TestStatefulVerbRefusesInvalidStateDirEnv(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	markStateRoot(t, repo, ".claude")
	env := []string{config.StateDirEnvVar + "=../escape"}

	_, diag, code := runAtomicCLIFull(t, home, env, "root", "scratchpad", "new", "probe", "--purpose", "plan", "--repo", repo)
	if code == 0 || !strings.Contains(diag, config.StateDirEnvVar) {
		t.Fatalf("exit = %d, diagnostics %q; want a non-zero %s refusal", code, diag, config.StateDirEnvVar)
	}
	if _, err := os.Stat(filepath.Join(repo, ".atomic")); !os.IsNotExist(err) {
		t.Errorf("refused verb created .atomic (stat err = %v)", err)
	}
}

// TestStatefulVerbRefusesAmbiguousRoots proves two populated non-destination
// roots stop the verb before it creates the configured destination.
func TestStatefulVerbRefusesAmbiguousRoots(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	cfg := config.Default()
	if err := config.Set(cfg, "state.dir", ".state"); err != nil {
		t.Fatal(err)
	}
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}
	markStateRoot(t, repo, ".atomic")
	markStateRoot(t, repo, ".claude")

	_, diag, code := runAtomicCLIFull(t, home, nil, "root", "scratchpad", "new", "probe", "--purpose", "plan", "--repo", repo)
	if code == 0 || !strings.Contains(diag, "atomic state adopt --dir") {
		t.Fatalf("exit = %d, diagnostics %q; want a non-zero refusal naming `atomic state adopt --dir`", code, diag)
	}
	if _, err := os.Stat(filepath.Join(repo, ".state")); !os.IsNotExist(err) {
		t.Errorf("refused verb created .state (stat err = %v)", err)
	}
}

// TestStateRepoRootToleratesAmbiguity proves `state adopt` can still resolve
// the repository root it needs to record --dir for.
func TestStateRepoRootToleratesAmbiguity(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	t.Cleanup(config.SetHomeDirForTest(home))
	cfg := config.Default()
	if err := config.Set(cfg, "state.dir", ".state"); err != nil {
		t.Fatal(err)
	}
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}
	markStateRoot(t, repo, ".atomic")
	markStateRoot(t, repo, ".claude")

	if _, err := stateRepoRoot(repo, nil); err != nil {
		t.Fatalf("stateRepoRoot: %v", err)
	}
}

// TestStateAdoptEmptyRepositoryWritesNothing proves an empty repository creates
// no candidate and no record until a stateful operation needs one.
func TestStateAdoptEmptyRepositoryWritesNothing(t *testing.T) {
	repo := t.TempDir()
	report, err := stateAdopt(stateOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("stateAdopt: %v", err)
	}
	if report.Action != "none" {
		t.Errorf("action = %q, want none", report.Action)
	}
	if _, err := os.Stat(config.StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Errorf("empty repository wrote a selection record (stat err = %v)", err)
	}
}

// TestStateAdoptClearWithdrawsTheSelection proves --clear restores the ladder.
func TestStateAdoptClearWithdrawsTheSelection(t *testing.T) {
	repo := t.TempDir()
	markStateRoot(t, repo, builtinStateDir(t, repo))
	if _, err := stateAdopt(stateOptions{RepoRoot: repo}); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if _, err := os.Stat(config.StateLocationRecordPath(repo)); err != nil {
		t.Fatalf("adopt wrote no selection record: %v", err)
	}

	if _, err := stateAdopt(stateOptions{RepoRoot: repo, Clear: true}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(config.StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Errorf("clear left the selection record (stat err = %v)", err)
	}
}

// TestStateAdoptRejectsUnsafeDirSegment proves a path-escaping segment is
// refused before anything is recorded.
func TestStateAdoptRejectsUnsafeDirSegment(t *testing.T) {
	repo := t.TempDir()
	if _, err := stateAdopt(stateOptions{RepoRoot: repo, Dir: "../escape"}); err == nil {
		t.Fatal("an escaping state.dir segment was accepted")
	}
}

// builtinStateDir reads the built-in segment from an empty repository, so no
// fixture hard-codes it. Call it before a test installs its own home.
func builtinStateDir(t *testing.T, repo string) string {
	t.Helper()
	t.Setenv(config.StateDirEnvVar, "")
	t.Cleanup(config.SetHomeDirForTest(t.TempDir()))
	loc, err := config.ResolveStateLocation(repo)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Source != config.StateSourceDefault {
		t.Fatalf("state source = %q, want the built-in default", loc.Source)
	}
	return filepath.Base(loc.Root)
}

// markStateRoot gives a repository one populated state candidate.
func markStateRoot(t *testing.T, repo, name string) {
	t.Helper()
	dir := filepath.Join(repo, name, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "signals.md"), []byte("signals\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// legacyStateRepo is a committed repository whose Atomic state still lives in
// .claude, the shape every pre-migration install has.
func legacyStateRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	for path, data := range map[string]string{
		".claude/atomic.toml":            "scope = \"repo\"\n",
		".claude/project/followups/a.md": "a\n",
	} {
		full := filepath.Join(repo, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "init")
	return repo
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestStateMigrateMovesStateAndPrintsStagedRenames(t *testing.T) {
	repo := legacyStateRepo(t)
	var out, errOut strings.Builder
	if code := stateMigrate(repo, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !pathExists(filepath.Join(repo, ".atomic/project/followups/a.md")) {
		t.Error("project/ did not move to .atomic")
	}
	if want := "staged\t.claude/project/followups/a.md -> .atomic/project/followups/a.md"; !strings.Contains(out.String(), want) {
		t.Errorf("stdout lacks %q:\n%s", want, out.String())
	}
}

func TestStateMigrateBothPopulatedExitsOneNamingLeftovers(t *testing.T) {
	repo := legacyStateRepo(t)
	markStateRoot(t, repo, ".atomic")
	var out, errOut strings.Builder
	if code := stateMigrate(repo, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), filepath.Join(repo, ".claude", "project")) {
		t.Errorf("stderr must name the leftover .claude entries:\n%s", errOut.String())
	}
}

func TestStateMigrateIndexOnlyLeftoverExitsZero(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	markStateRoot(t, repo, ".atomic")
	index := filepath.Join(repo, ".claude", ".atomic-index")
	if err := os.MkdirAll(index, 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut strings.Builder
	if code := stateMigrate(repo, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if pathExists(index) || !strings.Contains(out.String(), index) {
		t.Errorf("index-only leftover not removed and reported:\n%s", out.String())
	}
}

func TestMigrateStateIfPendingMovesOnceWithNotice(t *testing.T) {
	repo := legacyStateRepo(t)
	var notice strings.Builder
	if err := migrateStateIfPending(repo, &notice); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice.String(), "moving repository state") {
		t.Errorf("no notice on stderr:\n%s", notice.String())
	}
	if !pathExists(filepath.Join(repo, ".atomic/atomic.toml")) {
		t.Error("the trigger did not migrate")
	}
	notice.Reset()
	if err := migrateStateIfPending(repo, &notice); err != nil || notice.Len() != 0 {
		t.Errorf("second run: err %v, output %q; want a silent no-op", err, notice.String())
	}
}

// With both roots populated the trigger moves nothing and the resolver's one
// leftover warning is the only message.
func TestMigrateStateIfPendingBothPopulatedWarnsOnce(t *testing.T) {
	repo := legacyStateRepo(t)
	markStateRoot(t, repo, ".atomic")
	var warn strings.Builder
	t.Cleanup(config.SetStateWarnOutForTest(&warn))
	var notice strings.Builder
	count := func() int {
		return strings.Count(warn.String()+notice.String(), "also holds Atomic state")
	}
	if err := migrateStateIfPending(repo, &notice); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Errorf("leftover warnings = %d, want 1:\n%s%s", n, warn.String(), notice.String())
	}
	if _, err := config.RepositoryStateDir(repo); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Errorf("leftover warnings after the verb resolves again = %d, want 1", n)
	}
	if !pathExists(filepath.Join(repo, ".claude/project/followups/a.md")) {
		t.Error("the trigger moved state beside a populated destination")
	}
}

// An index alone is rebuildable, so the trigger leaves it and says nothing.
func TestMigrateStateIfPendingIgnoresIndexOnlyLegacyRoot(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	if err := os.MkdirAll(filepath.Join(repo, ".claude", ".atomic-index"), 0o755); err != nil {
		t.Fatal(err)
	}
	var notice strings.Builder
	if err := migrateStateIfPending(repo, &notice); err != nil || notice.Len() != 0 {
		t.Errorf("err %v, notice %q; want a silent no-op", err, notice.String())
	}
	if pathExists(filepath.Join(repo, ".atomic")) || !pathExists(filepath.Join(repo, ".claude", ".atomic-index")) {
		t.Error("an index-only legacy root was migrated")
	}
}

// homeRepo makes a legacy-state fixture the home directory for one test.
func homeRepo(t *testing.T) string {
	t.Helper()
	repo := legacyStateRepo(t)
	t.Cleanup(config.SetHomeDirForTest(repo))
	return repo
}

func TestStateMigrateRefusesHomeDirectory(t *testing.T) {
	repo := homeRepo(t)
	var out, errOut strings.Builder
	if code := stateMigrate(repo, &out, &errOut); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "home directory") {
		t.Errorf("stderr %q must name the home-directory refusal", errOut.String())
	}
	if !pathExists(filepath.Join(repo, ".claude/project/followups/a.md")) {
		t.Error("the home directory's .claude moved")
	}
}

func TestMigrateStateIfPendingRefusesHomeDirectory(t *testing.T) {
	repo := homeRepo(t)
	var notice strings.Builder
	if err := migrateStateIfPending(repo, &notice); err != nil {
		t.Fatalf("the trigger must report the refusal and continue: %v", err)
	}
	if !strings.Contains(notice.String(), "home directory") {
		t.Errorf("notice %q must name the home-directory refusal", notice.String())
	}
	if !pathExists(filepath.Join(repo, ".claude/project/followups/a.md")) {
		t.Error("the home directory's .claude moved")
	}
}

// bareHomeRepo is a home directory that is a Git work tree whose .claude holds
// only Claude's own files.
func bareHomeRepo(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, home, "init", "-q")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(config.SetHomeDirForTest(home))
	return home
}

func TestMigrateStateIfPendingSilentInHomeWithoutState(t *testing.T) {
	home := bareHomeRepo(t)
	var notice strings.Builder
	if err := migrateStateIfPending(home, &notice); err != nil || notice.Len() != 0 {
		t.Errorf("err %v, notice %q; want a silent no-op", err, notice.String())
	}
}

func TestStateMigrateInHomeWithoutStateHasNothingToMove(t *testing.T) {
	home := bareHomeRepo(t)
	var out, errOut strings.Builder
	if code := stateMigrate(home, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "nothing to move") {
		t.Errorf("stdout %q must say nothing to move", out.String())
	}
	if pathExists(filepath.Join(home, ".atomic")) {
		t.Error("a home directory with no state gained .atomic")
	}
}

// `state migrate` takes no arguments, so `--help` is never read as a root.
func TestStateMigrateRejectsArguments(t *testing.T) {
	repo := legacyStateRepo(t)
	for _, args := range [][]string{{"--help"}, {repo}} {
		var out, errOut strings.Builder
		if code := stateMigrateCmd(args, repo, &out, &errOut); code != 2 {
			t.Errorf("args %q: exit %d, want 2", args, code)
		}
	}
	if pathExists(filepath.Join(repo, ".atomic")) {
		t.Error("a rejected invocation migrated")
	}
}

// A daemon is live while one answers on the socket or a starter holds the
// spawn lock; either keeps the legacy index.
func TestCodeIndexInUse(t *testing.T) {
	dir, err := os.MkdirTemp("", "cp11a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	db := filepath.Join(dir, "atomic.db")

	inUse, release, err := codeIndexInUse(dir)
	if inUse || err != nil {
		t.Fatalf("an idle index reads as in use (%v, err %v)", inUse, err)
	}
	// A starter arriving while the migration removes the index must wait.
	if !mcp.SpawnLockHeld(db) {
		t.Error("an idle verdict left the spawn lock free for a starter")
	}
	release()
	if mcp.SpawnLockHeld(db) {
		t.Error("release left the spawn lock held")
	}

	ln, err := net.Listen("unix", mcp.SocketPathFromDB(db))
	if err != nil {
		t.Fatal(err)
	}
	if inUse, _, _ := codeIndexInUse(dir); !inUse {
		t.Error("a listening socket with no lock file reads as idle")
	}
	ln.Close()

	lock, err := os.OpenFile(mcp.LockPathFromDB(db), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if inUse, _, _ := codeIndexInUse(dir); !inUse {
		t.Error("a held spawn lock reads as idle")
	}
}

// Only state migrate, repo init, and update move state; ordinary verbs keep
// using the populated legacy root.
func TestStatefulVerbsMoveNoFile(t *testing.T) {
	repo := legacyStateRepo(t)
	var out, errOut strings.Builder
	if code := scratchpadDispatch([]string{"new", "loop", "--purpose", "plan"}, repo, &out, &errOut); code != 0 {
		t.Fatalf("scratchpad new exit %d: %s", code, errOut.String())
	}
	if _, stderr, code := workflowCLI(t, workflowEnv{home: t.TempDir(), cwd: repo},
		"set", "--level", "repo", "worktrees", "off"); code != 0 {
		t.Fatalf("workflow set exit %d: %s", code, stderr)
	}
	if pathExists(filepath.Join(repo, ".atomic")) {
		t.Error("an ordinary verb created .atomic")
	}
	for _, p := range []string{".claude/project/followups/a.md", ".claude/.scratchpad/loop", ".claude/workflow-preferences.md"} {
		if !pathExists(filepath.Join(repo, p)) {
			t.Errorf("%s missing; the verb must write the legacy root in place", p)
		}
	}
}
