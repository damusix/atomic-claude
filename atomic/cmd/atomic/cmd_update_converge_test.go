package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/migrate"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

func TestStripTargetConvergeMarker(t *testing.T) {
	found, cleaned := stripTargetConvergeMarker([]string{"--no-doctor", targetConvergeMarker, "--force"})
	if !found {
		t.Error("marker not reported as present")
	}
	want := []string{"--no-doctor", "--force"}
	if len(cleaned) != len(want) {
		t.Fatalf("cleaned = %v, want %v", cleaned, want)
	}
	for i, a := range cleaned {
		if a != want[i] {
			t.Errorf("cleaned[%d] = %q, want %q", i, a, want[i])
		}
	}

	if found, _ := stripTargetConvergeMarker([]string{"--check"}); found {
		t.Error("marker reported on args that do not carry it")
	}
}

// stubUpdateDeps returns deps whose side-effecting collaborators abort the test
// if they run, so each test opts in to exactly the collaborators it expects.
func stubUpdateDeps(t *testing.T) updateDeps {
	t.Helper()
	return updateDeps{
		client:       &selfupdate.Client{},
		executable:   func() (string, error) { return "/tmp/atomic", nil },
		evalSymlinks: func(p string) (string, error) { return p, nil },
		runChild:     func(string, ...string) error { return errors.New("unexpected runChild") },
		apply: func(context.Context, string, *selfupdate.Client, string, string, string, bool, func() time.Time, io.Writer) (bool, error) {
			return false, errors.New("unexpected apply")
		},
		converge: func(string, io.Writer) error { return errors.New("unexpected converge") },
		migrate:  func(string) error { return errors.New("unexpected migrate") },
		doctor:   func(io.Writer) { t.Error("unexpected doctor") },
		terminal: func() bool { return false },
		out:      io.Discard,
		errOut:   io.Discard,
		now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}
}

// enrollClaudeTarget enrolls the default Claude home through the real install
// engine, so update convergence has an owner to reconverge.
func enrollClaudeTarget(t *testing.T, home string) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	steps := install.DefaultSteps(home)
	steps.AssumeYes = true
	reports, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}, Enroll: true})
	if err != nil {
		t.Fatalf("enroll claude target: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Blockers) > 0 {
		t.Fatalf("enroll reports = %+v, want one converged target", reports)
	}
}

// ledgerGeneration returns the generation of the first ownership row, which is
// the generation the last convergence recorded.
func ledgerGeneration(t *testing.T, home string) string {
	t.Helper()
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	for _, row := range ledger.Rows {
		return row.Generation
	}
	t.Fatal("ledger holds no ownership rows")
	return ""
}

func fixedNow() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

// TestRunUpdate_StagedSwapReExecsReplacementAndRefusesStalePublish proves the stale
// process never publishes its own corpus after a swap; only the replacement converges.
func TestRunUpdate_StagedSwapReExecsReplacementAndRefusesStalePublish(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	enrollClaudeTarget(t, home)

	f := newFakeSwapServer(t, "v2.0.0", "new-binary-v2-content")
	stageDir := selfupdate.StageDir(home)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stagedPath := filepath.Join(stageDir, f.assetName)
	data, err := os.ReadFile(f.archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	seed := selfupdate.State{}
	seed.Update.Staged = selfupdate.StagedInfo{Version: "2.0.0", Path: stagedPath, SHA256: f.sha256}
	if err := selfupdate.WriteState(config.StatePath(home), seed); err != nil {
		t.Fatal(err)
	}

	currentBin := filepath.Join(t.TempDir(), "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	deps := stubUpdateDeps(t)
	deps.client = f.client
	deps.executable = func() (string, error) { return currentBin, nil }
	deps.now = fixedNow
	// Drive the real staged swap; the running version is pinned below the
	// release tag because the test binary's own version is "dev".
	deps.apply = func(ctx context.Context, h string, c *selfupdate.Client, ch, _ string, bin string, force bool, now func() time.Time, w io.Writer) (bool, error) {
		return runUpdateApplyOutcome(ctx, h, c, ch, "1.0.0", bin, force, now, w)
	}
	var errBuf strings.Builder
	deps.errOut = &errBuf
	// The swapped-out process must never publish its corpus.
	deps.converge = func(string, io.Writer) error {
		t.Error("stale process ran target convergence")
		return nil
	}
	var childArgs []string
	deps.runChild = func(exe string, args ...string) error {
		childArgs = append([]string{exe}, args...)
		out, code := runAtomicCLI(t, home, args...)
		if code != 0 {
			return fmt.Errorf("replacement child exited %d: %s", code, out)
		}
		return nil
	}

	if code := runUpdateWith([]string{"--no-doctor"}, deps); code != 0 {
		t.Fatalf("update exit = %d, want 0 (stderr: %s)", code, errBuf.String())
	}

	got, err := os.ReadFile(currentBin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-binary-v2-content" {
		t.Errorf("binary content = %q, want the staged payload", got)
	}
	if len(childArgs) < 3 || childArgs[1] != "update" || childArgs[2] != targetConvergeMarker {
		t.Fatalf("re-exec argv = %v, want [exe update %s ...]", childArgs, targetConvergeMarker)
	}
	if generation := ledgerGeneration(t, home); generation != claude.Generation() {
		t.Errorf("recorded generation = %q, want the replacement's %q", generation, claude.Generation())
	}
}

// TestRunUpdate_AlreadyCurrentConvergesInProcess proves an up-to-date binary
// skips the re-exec and converges enrolled targets itself.
func TestRunUpdate_AlreadyCurrentConvergesInProcess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	enrollClaudeTarget(t, home)

	deps := stubUpdateDeps(t)
	deps.apply = func(context.Context, string, *selfupdate.Client, string, string, string, bool, func() time.Time, io.Writer) (bool, error) {
		return false, nil
	}
	converged := 0
	deps.converge = func(h string, w io.Writer) error {
		converged++
		return convergeEnrolledTargets(h, w)
	}
	deps.runChild = func(string, ...string) error {
		t.Error("an already-current update must not re-exec")
		return nil
	}
	deps.migrate = func(string) error { return nil }

	if code := runUpdateWith([]string{"--no-doctor"}, deps); code != 0 {
		t.Fatalf("update exit = %d, want 0", code)
	}
	if converged != 1 {
		t.Errorf("converge calls = %d, want 1 in the already-current process", converged)
	}
	if generation := ledgerGeneration(t, home); generation != claude.Generation() {
		t.Errorf("recorded generation = %q, want the running binary's %q", generation, claude.Generation())
	}
}

// enrollCodexTarget enrolls a Codex home through the real install engine and
// returns the native root plus the recorded generation.
func enrollCodexTarget(t *testing.T, home string) (root, generation string) {
	t.Helper()
	root = filepath.Join(home, "codex-home")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", root)
	steps := install.DefaultSteps(home)
	steps.AssumeYes = true
	reports, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindCodex}, Enroll: true})
	if err != nil {
		t.Fatalf("enroll codex target: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Blockers) > 0 {
		t.Fatalf("enroll reports = %+v, want one converged target", reports)
	}
	return root, reports[0].Generation
}

// TestRunUpdate_AlreadyCurrentConvergesEnrolledCodex proves update convergence
// covers a non-Claude target through the same generic path.
func TestRunUpdate_AlreadyCurrentConvergesEnrolledCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root, generation := enrollCodexTarget(t, home)

	deps := stubUpdateDeps(t)
	deps.apply = func(context.Context, string, *selfupdate.Client, string, string, string, bool, func() time.Time, io.Writer) (bool, error) {
		return false, nil
	}
	converged := 0
	deps.converge = func(h string, w io.Writer) error {
		converged++
		return convergeEnrolledTargets(h, w)
	}
	deps.runChild = func(string, ...string) error {
		t.Error("an already-current update must not re-exec")
		return nil
	}
	deps.migrate = func(string) error { return nil }

	if code := runUpdateWith([]string{"--no-doctor"}, deps); code != 0 {
		t.Fatalf("update exit = %d, want 0", code)
	}
	if converged != 1 {
		t.Errorf("converge calls = %d, want 1 in the already-current process", converged)
	}
	if got := ledgerGeneration(t, home); got != generation {
		t.Errorf("recorded generation = %q, want the enrolled %q", got, generation)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	if _, ok := ledger.FindTarget(string(harness.KindCodex), root); !ok {
		t.Errorf("the codex target was dropped by update convergence: %+v", ledger.Targets)
	}
	if _, err := os.Stat(filepath.Join(home, ".atomic", "packages", "codex", "atomic", ".agents", "plugins", "marketplace.json")); err != nil {
		t.Errorf("update convergence dropped the codex plugin package: %v", err)
	}
}

// TestRunUpdate_ConvergeChildConvergesWithoutSelecting proves the re-exec'd
// child owns convergence and performs no binary selection of its own.
func TestRunUpdate_ConvergeChildConvergesWithoutSelecting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	deps := stubUpdateDeps(t)
	converged := 0
	deps.converge = func(got string, _ io.Writer) error {
		converged++
		if got != home {
			t.Errorf("converge home = %q, want %q", got, home)
		}
		return nil
	}
	deps.migrate = func(string) error { return nil }

	if code := runUpdateWith([]string{targetConvergeMarker, "--no-doctor"}, deps); code != 0 {
		t.Fatalf("converge child exit = %d, want 0", code)
	}
	if converged != 1 {
		t.Errorf("converge calls = %d, want 1", converged)
	}
}

// TestRunUpdate_UnresolvableHomePerformsNoConfigIO exists because an unguarded
// migrate recreated ./.atomic/config.toml under the process CWD.
func TestRunUpdate_UnresolvableHomePerformsNoConfigIO(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("HOME", "")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"converge-child", []string{targetConvergeMarker, "--no-doctor"}},
		{"already-current", []string{"--no-doctor"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := stubUpdateDeps(t)
			deps.apply = func(context.Context, string, *selfupdate.Client, string, string, string, bool, func() time.Time, io.Writer) (bool, error) {
				return false, nil
			}
			deps.converge = convergeEnrolledTargets // real: a no-op for an unresolvable home
			deps.migrate = runMigrateInstall        // real: must never be reached
			deps.doctor = func(io.Writer) {}
			var errBuf strings.Builder
			deps.errOut = &errBuf

			if code := runUpdateWith(tc.args, deps); code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errBuf.String())
			}
			entries, err := os.ReadDir(cwd)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) > 0 {
				names := make([]string, 0, len(entries))
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Fatalf("unresolvable-home update wrote into the working directory: %v", names)
			}
		})
	}
}

// TestRunUpdate_ResolvableHomeMigratesUnderHome proves the guard narrows to the
// unresolvable case.
func TestRunUpdate_ResolvableHomeMigratesUnderHome(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Migrations only version a home that carries a legacy record.
	seed := config.Default()
	seed.Install.Version = "0.0.1"
	seed.Install.Adopted = true
	if err := config.WritePersist(config.TOMLPath(home), seed); err != nil {
		t.Fatal(err)
	}
	origRegistry := migrate.Registry
	t.Cleanup(func() { migrate.Registry = origRegistry })
	migrate.Registry = append(append([]migrate.Migration{}, origRegistry...), migrate.Migration{
		TargetVersion: "99.0.0",
		Scope:         "install",
		Up:            func(*migrate.Context) error { return nil },
	})

	deps := stubUpdateDeps(t)
	deps.apply = func(context.Context, string, *selfupdate.Client, string, string, string, bool, func() time.Time, io.Writer) (bool, error) {
		return false, nil
	}
	deps.converge = convergeEnrolledTargets // real: no enrollment is a no-op
	deps.migrate = runMigrateInstall        // real: applies the pending install migrations
	deps.doctor = func(io.Writer) {}

	if code := runUpdateWith([]string{"--no-doctor"}, deps); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if cfg, _, err := config.Load(config.TOMLPath(home)); err != nil || cfg.Install.Version != "99.0.0" {
		t.Errorf("resolvable-home update did not migrate under $HOME (version %q, err %v)", cfg.Install.Version, err)
	}
	entries, err := os.ReadDir(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		t.Errorf("resolvable-home update wrote into the working directory: %v", entries)
	}
}

// TestRunUpdate_SkipOptOutSuppressesConvergence proves --skip-claude-update drops
// convergence on both paths but keeps the migrations.
func TestRunUpdate_SkipOptOutSuppressesConvergence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	deps := stubUpdateDeps(t)
	deps.converge = func(string, io.Writer) error {
		t.Error("convergence ran despite --skip-claude-update")
		return nil
	}

	t.Run("swapped-delegates-with-skip", func(t *testing.T) {
		d := deps
		d.apply = func(context.Context, string, *selfupdate.Client, string, string, string, bool, func() time.Time, io.Writer) (bool, error) {
			return true, nil
		}
		var childArgs []string
		d.runChild = func(_ string, args ...string) error {
			childArgs = args
			return nil
		}
		if code := runUpdateWith([]string{"--skip-claude-update", "--no-doctor"}, d); code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
		if !containsArg(childArgs, "--skip-claude-update") || !containsArg(childArgs, targetConvergeMarker) {
			t.Fatalf("replacement argv = %v, want the converge marker and --skip-claude-update", childArgs)
		}
	})

	t.Run("already-current-runs-migrations", func(t *testing.T) {
		d := deps
		d.apply = func(context.Context, string, *selfupdate.Client, string, string, string, bool, func() time.Time, io.Writer) (bool, error) {
			return false, nil
		}
		migrated := false
		d.migrate = func(string) error { migrated = true; return nil }
		if code := runUpdateWith([]string{"--skip-claude-update", "--no-doctor"}, d); code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
		if !migrated {
			t.Error("migrations must still run when convergence is skipped")
		}
	})
}

// The replacement child already printed its own failure and next step, so the
// parent must exit with the child's status and add no second, contradicting hint.
func TestRunUpdate_SwappedExitsWithChildStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	deps := stubUpdateDeps(t)
	deps.apply = func(context.Context, string, *selfupdate.Client, string, string, string, bool, func() time.Time, io.Writer) (bool, error) {
		return true, nil
	}
	var errBuf strings.Builder
	deps.errOut = &errBuf
	deps.runChild = func(string, ...string) error {
		return exec.Command("sh", "-c", "exit 3").Run()
	}
	if code := runUpdateWith([]string{"--no-doctor"}, deps); code != 3 {
		t.Fatalf("exit = %d, want the child's 3", code)
	}
	if errBuf.Len() != 0 {
		t.Errorf("stderr = %q, want nothing beyond the child's own output", errBuf.String())
	}

	deps.runChild = func(string, ...string) error { return errors.New("exec format error") }
	errBuf.Reset()
	if code := runUpdateWith([]string{"--no-doctor"}, deps); code != 1 {
		t.Fatalf("exit = %d, want 1 when the child never ran", code)
	}
	if !strings.Contains(errBuf.String(), "exec format error") {
		t.Errorf("stderr = %q, want the start failure named", errBuf.String())
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// TestRunUpdateCheck_LeavesTargetStateByteIdentical proves --check reads version
// state without touching native files, ownership state, journals, or locks.
func TestRunUpdateCheck_LeavesTargetStateByteIdentical(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	enrollClaudeTarget(t, home)

	targetDir := filepath.Join(home, ".claude")
	installDir := config.InstallDir(home)
	beforeTarget := treeDigestCLI(t, targetDir)
	beforeInstall := treeDigestCLI(t, installDir)

	f := newFakeReleaseServer(t, "v9.9.9", "payload")
	deps := stubUpdateDeps(t)
	deps.client = f.client

	if code := runUpdateWith([]string{"--check"}, deps); code != 0 {
		t.Fatalf("--check exit = %d, want 0", code)
	}
	if after := treeDigestCLI(t, targetDir); after != beforeTarget {
		t.Errorf("--check changed the native target tree:\nbefore %s\nafter  %s", beforeTarget, after)
	}
	if after := treeDigestCLI(t, installDir); after != beforeInstall {
		t.Errorf("--check changed ownership state:\nbefore %s\nafter  %s", beforeInstall, after)
	}
}

// TestUpdateConvergenceRecoversInterruptedUpdate proves the next convergence
// recovers an interrupted update's journal before it mutates.
func TestUpdateConvergenceRecoversInterruptedUpdate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	enrollClaudeTarget(t, home)

	target := harness.Target{Kind: harness.KindClaude, Instance: filepath.Join(home, ".claude")}
	probe := filepath.Join(home, "interrupted-write.txt")
	content := []byte("bytes that landed before the ledger commit\n")
	if err := os.WriteFile(probe, content, 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := managedfile.DigestResourceBytes(content, managedfile.KindFile)
	if err != nil {
		t.Fatal(err)
	}
	const operationID = "interrupted-update-op"
	journal := installstate.NewJournal(operationID, time.Unix(0, 0).UTC())
	journal.Mutations = []installstate.Mutation{{
		Unit: "probe", Resource: "probe", Target: target.Key(), Consumer: target.Key(),
		Generation: "gen-1", Tier: "unsupported",
		Kind: managedfile.KindFile, Path: probe, Intended: digest,
	}}
	journal.Record("probe", installstate.StateApplied, digest, time.Unix(0, 0).UTC())
	journalPath := config.JournalPath(home, operationID)
	if err := installstate.WriteJournal(journalPath, journal); err != nil {
		t.Fatal(err)
	}

	if err := convergeEnrolledTargets(home, io.Discard); err != nil {
		t.Fatalf("update convergence: %v", err)
	}

	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Errorf("the recovered journal was not consumed and cleaned up (stat err = %v)", err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find(target.Key(), "probe"); !ok {
		t.Error("recovery did not commit the applied-but-unrecorded resource")
	}
}

// TestUpdateConvergenceBlocksOnLifecycleLock proves convergence waits on a held
// lifecycle lock rather than writing around the holder.
func TestUpdateConvergenceBlocksOnLifecycleLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	enrollClaudeTarget(t, home)

	lock, err := installstate.AcquireLock(home, installstate.WriterIdentity{OperationID: "test-lock-holder"})
	if err != nil {
		t.Fatalf("acquire lifecycle lock: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- convergeEnrolledTargets(home, io.Discard) }()

	select {
	case err := <-done:
		t.Fatalf("convergence finished while the lifecycle lock was held: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("release lifecycle lock: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("convergence after release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("convergence did not finish after the lifecycle lock was released")
	}
}

// TestEnrollConvergenceRegistersHookAndSeedsOutputStyle exists because enrollment
// claimed to register the SessionStart hook and seed outputStyle but never did.
func TestEnrollConvergenceRegistersHookAndSeedsOutputStyle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	root := filepath.Join(home, ".claude")
	settingsPath := filepath.Join(root, "settings.json")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	userSettings := "{\n  \"permissions\": {\"allow\": [\"Bash(ls:*)\"]}\n}\n"
	if err := os.WriteFile(settingsPath, []byte(userSettings), 0o644); err != nil {
		t.Fatal(err)
	}

	enrollClaudeTarget(t, home)

	installed, drifted, err := hooks.IsInstalled(home)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed || drifted {
		t.Fatalf("session-start hook installed=%v drifted=%v, want true/false", installed, drifted)
	}
	value, present, err := hooks.ReadOutputStyle(settingsPath)
	if err != nil {
		t.Fatalf("ReadOutputStyle: %v", err)
	}
	if !present || value != hooks.OutputStyleName {
		t.Fatalf("outputStyle = %q present=%v, want %q", value, present, hooks.OutputStyleName)
	}

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"Bash(ls:*)"`) {
		t.Fatalf("user setting did not survive convergence: %s", raw)
	}

	before := string(raw)
	steps := install.DefaultSteps(home)
	steps.AssumeYes = true
	if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}}); err != nil {
		t.Fatalf("reconverge: %v", err)
	}
	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Fatalf("reconvergence rewrote settings.json:\nbefore %s\nafter  %s", before, after)
	}
}

// agedLegacyClaude builds a legacy install with older-version artifacts and a
// corrupt snapshot; it returns the root and the older bytes by path.
func agedLegacyClaude(t *testing.T, home string) (string, map[string]string) {
	t.Helper()
	root := filepath.Join(home, ".claude")
	// The snapshot only records files that existed before the first install.
	mustWrite(t, filepath.Join(root, "settings.json"), `{"model":"sonnet"}`+"\n")
	if out, code := runAtomicCLI(t, home, "claude", "install", "--no-hooks"); code != 0 {
		t.Fatalf("legacy install exited %d:\n%s", code, out)
	}
	if _, err := os.Stat(config.LedgerPath(home)); !os.IsNotExist(err) {
		t.Fatalf("the legacy install wrote a ledger (stat err = %v)", err)
	}
	older := map[string]string{}
	for _, a := range embedded.Manifest() {
		if a.Kind == "claude-md" {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(a.Target))
		older[path] = "older " + a.Target + "\n"
		mustWrite(t, path, older[path])
	}
	dir := config.PreInstallDir(home)
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m claudeinstall.PreInstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	corrupted := false
	for _, f := range m.Files {
		if !f.Existed {
			continue
		}
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(f.Path))); err == nil {
			corrupted = true
			break
		}
	}
	if !corrupted {
		t.Fatal("no pre-install copy found to corrupt")
	}
	return root, older
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runUpdateConverge runs the post-swap half of `atomic update` against home
// with the real convergence, returning stdout, stderr, and the exit code.
func runUpdateConverge(t *testing.T, home string) (string, string, int) {
	t.Helper()
	deps := stubUpdateDeps(t)
	deps.converge = convergeEnrolledTargets
	deps.migrate = func(string) error { return nil }
	deps.doctor = func(io.Writer) {}
	var out, errOut strings.Builder
	deps.out = &out
	deps.errOut = &errOut
	code := runUpdatePostSwap(home, true, false, deps)
	return out.String(), errOut.String(), code
}

// backedUpBytes returns the recovery copies by unit name, requiring the one
// adopt-* operation directory a single adoption leaves under ~/.atomic/backups.
func backedUpBytes(t *testing.T, home string) map[string]string {
	t.Helper()
	ops, err := filepath.Glob(filepath.Join(config.BackupDir(home), "adopt-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("adopt-* backup operation dirs = %v, want exactly one", ops)
	}
	entries, err := os.ReadDir(ops[0])
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(ops[0], e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got[e.Name()] = string(data)
	}
	return got
}

// assertRecoveryCopies requires each older file's bytes under its own unit name
// in the single adopt-* backup operation.
func assertRecoveryCopies(t *testing.T, home, root string, older map[string]string) {
	t.Helper()
	copies := backedUpBytes(t, home)
	for path, was := range older {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		unit := strings.ReplaceAll(filepath.ToSlash(rel), "/", ".2f")
		got, ok := copies[unit]
		if !ok {
			t.Errorf("no recovery copy named %s for %s", unit, path)
		} else if got != was {
			t.Errorf("recovery copy %s = %q, want the pre-adoption bytes %q", unit, got, was)
		}
	}
}

// TestUpdateAdoptsLegacyClaudeInstall: update adopts the aged install in one
// step and leaves a recovery copy of every replaced file.
func TestUpdateAdoptsLegacyClaudeInstall(t *testing.T) {
	home := t.TempDir()
	root, older := agedLegacyClaude(t, home)

	out, errOut, code := runUpdateConverge(t, home)
	if code != 0 {
		t.Fatalf("update exited %d:\n%s\n%s", code, out, errOut)
	}
	if want := "adopting existing Claude install at " + root; !strings.Contains(out, want) {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.FindTarget("claude", root); !ok {
		t.Errorf("update did not enroll the Claude root: %+v", ledger.Targets)
	}
	for path, was := range older {
		now, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(now) == was {
			t.Errorf("%s still holds the older-version bytes", path)
		}
	}
	assertRecoveryCopies(t, home, root, older)
	assertAdoptedMarker(t, home)

	out, errOut, code = runUpdateConverge(t, home)
	if code != 0 {
		t.Fatalf("second update exited %d:\n%s\n%s", code, out, errOut)
	}
	if strings.Contains(out, "adopting existing Claude install") {
		t.Errorf("second update adopted again: %q", out)
	}
}

// A marker the update cannot write is a warning, not a failed update: the
// enrolled targets still converge and the next update retries the marker.
func TestUpdateWarnsAndConvergesWhenMarkerWriteFails(t *testing.T) {
	home := t.TempDir()
	_, older := agedLegacyClaude(t, home)
	if _, errOut, code := runUpdateConverge(t, home); code != 0 {
		t.Fatalf("adopting update exited %d: %s", code, errOut)
	}
	var owned string
	for path := range older {
		owned = path
		break
	}
	if err := os.Remove(owned); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Install.Adopted = false
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}
	dir := config.Dir(home)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	out, errOut, code := runUpdateConverge(t, home)
	if code != 0 {
		t.Fatalf("update exited %d, want 0 with a warning:\n%s\n%s", code, out, errOut)
	}
	if got := out + errOut; !strings.Contains(got, "could not record") {
		t.Errorf("no warning about the unwritten marker:\n%s", got)
	}
	if !pathExists(owned) {
		t.Errorf("convergence did not restore %s after the marker warning", owned)
	}
}

// Legacy adoption pins no state root, so the same update that adopts the
// install moves a lone populated .claude to .atomic.
func TestUpdateAdoptionLeavesNoStateRecordAndMigrates(t *testing.T) {
	home := t.TempDir()
	agedLegacyClaude(t, home)
	repo := legacyStateRepo(t)

	deps := stubUpdateDeps(t)
	deps.converge = convergeEnrolledTargets
	deps.migrate = func(string) error { return nil }
	deps.repoRoot = func() (string, bool) { return repo, true }
	var out, errOut strings.Builder
	deps.out = &out
	deps.errOut = &errOut
	if code := runUpdatePostSwap(home, true, false, deps); code != 0 {
		t.Fatalf("update exited %d:\n%s\n%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "adopting existing Claude install") {
		t.Fatalf("update did not adopt the legacy install:\n%s", out.String())
	}
	if _, err := os.Stat(config.StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Errorf("adoption wrote a state selection record (stat err = %v)", err)
	}
	if !strings.Contains(errOut.String(), "moving repository state") {
		t.Errorf("no migration notice on stderr:\n%s", errOut.String())
	}
	if !pathExists(filepath.Join(repo, ".atomic/project/followups/a.md")) {
		t.Error("update did not migrate the lone .claude root")
	}
}

// TestUpdateAdoptsLegacyClaudeBesideEnrolledOMP: another enrolled target must
// not hide an unadopted legacy Claude root.
func TestUpdateAdoptsLegacyClaudeBesideEnrolledOMP(t *testing.T) {
	home := t.TempDir()
	root, _ := agedLegacyClaude(t, home)
	ompRoot := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(ompRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, code := runAtomicCLIEnv(t, home, nil, "harness", "enroll", "omp", "--instance", ompRoot, "--yes"); code != 0 {
		t.Fatalf("enroll omp exited %d:\n%s", code, out)
	}

	out, errOut, code := runUpdateConverge(t, home)
	if code != 0 {
		t.Fatalf("update exited %d:\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "adopting existing Claude install at "+root) {
		t.Errorf("stdout = %q, want the adoption line", out)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.FindTarget("claude", root); !ok {
		t.Errorf("Claude root not enrolled beside OMP: %+v", ledger.Targets)
	}
}

// TestUpdateLegacyClaudeBlockersStopWithoutWriting: a user-content blocker
// exits 1 naming what to resolve, with no native change and no ledger.
func TestUpdateLegacyClaudeBlockersStopWithoutWriting(t *testing.T) {
	cases := []struct {
		name string
		seed func(t *testing.T, home, root string) []string
	}{
		{"pending proposed CLAUDE.md", func(t *testing.T, home, root string) []string {
			path := claudeinstall.ProposedPath(home)
			mustWrite(t, path, "proposed\n")
			return []string{path, "atomic prompt claude-merge", "or delete it"}
		}},
		{"pending CLAUDE.md.atomic-merged", func(t *testing.T, home, root string) []string {
			path := claudeinstall.MergedPath(root)
			mustWrite(t, path, "merged\n")
			return []string{path, "apply it over CLAUDE.md or delete it"}
		}},
		{"unjournaled v2 remnant", func(t *testing.T, home, root string) []string {
			dir := config.TransactionDir(home, "stray-op")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			return []string{dir}
		}},
		{"ambiguous managed block", func(t *testing.T, home, root string) []string {
			path := filepath.Join(root, "CLAUDE.md")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			mustWrite(t, path, string(data)+string(data))
			return []string{path}
		}},
		{"legacy and v2 overlap via unreadable journal", func(t *testing.T, home, root string) []string {
			path := filepath.Join(config.JournalsDir(home), "broken.json")
			mustWrite(t, path, "{not json")
			return []string{path}
		}},
		{"unrecoverable journal", func(t *testing.T, home, root string) []string {
			journal, _ := seedConflictedClaudeJournal(t, home)
			return []string{journal, "atomic harness recover"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			root, _ := agedLegacyClaude(t, home)
			names := tc.seed(t, home, root)
			before := treeDigestCLI(t, root)

			out, errOut, code := runUpdateConverge(t, home)
			if code != 1 {
				t.Fatalf("exit = %d, want 1:\n%s\n%s", code, out, errOut)
			}
			for _, want := range append(names, "re-run `atomic update`") {
				if !strings.Contains(errOut, want) {
					t.Errorf("stderr = %q, want %q", errOut, want)
				}
			}
			if tc.name == "unjournaled v2 remnant" && strings.Contains(errOut, "atomic harness recover") {
				t.Errorf("a remnant is not a journal, but stderr sends the user to recover: %q", errOut)
			}
			if after := treeDigestCLI(t, root); after != before {
				t.Error("a blocked adoption changed the native tree")
			}
			if _, err := os.Stat(config.LedgerPath(home)); !os.IsNotExist(err) {
				t.Errorf("a blocked adoption wrote a ledger (stat err = %v)", err)
			}
		})
	}
}

// TestUpdateAdoptionErrorExitsOneWithAdoptInstruction: repair cannot act on an
// unenrolled root, so a failed adoption exits 1 and names the adopt verb.
func TestUpdateAdoptionErrorExitsOneWithAdoptInstruction(t *testing.T) {
	home := t.TempDir()
	agedLegacyClaude(t, home)
	if err := os.MkdirAll(config.OperationLockPath(home), 0o755); err != nil {
		t.Fatal(err)
	}

	out, errOut, code := runUpdateConverge(t, home)
	if code != 1 {
		t.Fatalf("exit = %d, want 1:\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "atomic harness adopt claude") || strings.Contains(errOut, "atomic harness repair") {
		t.Errorf("stderr = %q, want the adopt instruction and no repair advice", errOut)
	}
}

// TestUpdateEnrolledClaudeCorruptLedgerWarnsWithoutAdopting: an unreadable
// ledger leaves enrollment unknown, so update adopts nothing and warns.
func TestUpdateEnrolledClaudeCorruptLedgerWarnsWithoutAdopting(t *testing.T) {
	home := t.TempDir()
	if out, code := runAtomicCLI(t, home, "install", "--harness", "claude", "--yes"); code != 0 {
		t.Fatalf("install exited %d:\n%s", code, out)
	}
	mustWrite(t, config.LedgerPath(home), "{not json")

	out, errOut, code := runUpdateConverge(t, home)
	if code != 0 {
		t.Fatalf("exit = %d, want 0:\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "atomic harness repair") || strings.Contains(out+errOut, "adopt") {
		t.Errorf("output = %q %q, want repair advice and no adopt text", out, errOut)
	}
}

// TestUpdateAdoptsFreshLegacyInstallAfterUninstall: a new legacy install is a
// new install to adopt, so it clears the mark the earlier adoption left.
func TestUpdateAdoptsFreshLegacyInstallAfterUninstall(t *testing.T) {
	home := t.TempDir()
	root, _ := agedLegacyClaude(t, home)
	if out, errOut, code := runUpdateConverge(t, home); code != 0 {
		t.Fatalf("adopting update exited %d:\n%s\n%s", code, out, errOut)
	}
	if out, code := runAtomicCLI(t, home, "harness", "uninstall", "claude:"+root, "--yes"); code != 0 {
		t.Fatalf("uninstall exited %d:\n%s", code, out)
	}
	if out, code := runAtomicCLI(t, home, "claude", "install", "--no-hooks"); code != 0 {
		t.Fatalf("legacy reinstall exited %d:\n%s", code, out)
	}

	proposed := claudeinstall.ProposedPath(home)
	out, errOut, code := runUpdateConverge(t, home)
	if code != 1 {
		t.Fatalf("update with a pending proposal exited %d, want 1:\n%s\n%s", code, out, errOut)
	}
	for _, want := range []string{proposed, "atomic prompt claude-merge", "re-run `atomic update`"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	}
	if err := os.Remove(proposed); err != nil {
		t.Fatal(err)
	}

	out, errOut, code = runUpdateConverge(t, home)
	if code != 0 {
		t.Fatalf("update exited %d:\n%s\n%s", code, out, errOut)
	}
	if want := "adopting existing Claude install at " + root; !strings.Contains(out, want) {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

// TestFreshV2InstallWritesNoAdoptedMarker: the mark records that adoption took
// over a legacy record, and an empty home has none.
func TestFreshV2InstallWritesNoAdoptedMarker(t *testing.T) {
	home := t.TempDir()
	if out, code := runAtomicCLI(t, home, "install", "--harness", "claude", "--yes"); code != 0 {
		t.Fatalf("install exited %d:\n%s", code, out)
	}
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Install.Adopted {
		t.Error("a fresh v2 install marked a legacy install adopted")
	}
}

// TestUpdateDoesNotReadoptAfterUninstall: config.toml survives uninstall, so
// the adopted marker is what keeps update from re-adopting a removed target.
func TestUpdateDoesNotReadoptAfterUninstall(t *testing.T) {
	home := t.TempDir()
	root, _ := agedLegacyClaude(t, home)
	if out, errOut, code := runUpdateConverge(t, home); code != 0 {
		t.Fatalf("adopting update exited %d:\n%s\n%s", code, out, errOut)
	}
	if out, code := runAtomicCLI(t, home, "harness", "uninstall", "claude:"+root, "--yes"); code != 0 {
		t.Fatalf("uninstall exited %d:\n%s", code, out)
	}

	out, errOut, code := runUpdateConverge(t, home)
	if code != 0 {
		t.Fatalf("update after uninstall exited %d:\n%s\n%s", code, out, errOut)
	}
	if strings.Contains(out, "adopting existing Claude install") {
		t.Errorf("update re-adopted an uninstalled root: %q", out)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.FindTarget("claude", root); ok {
		t.Error("update re-enrolled the uninstalled Claude root")
	}
}

// TestUpdateUnreadableInstallRecordIsReported: an unparseable config.toml is
// surfaced, not read as "no legacy install".
func TestUpdateUnreadableInstallRecordIsReported(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, config.TOMLPath(home), "[install\n")

	out, errOut, code := runUpdateConverge(t, home)
	if code != 0 {
		t.Fatalf("exit = %d, want 0:\n%s\n%s", code, out, errOut)
	}
	for _, want := range []string{"target convergence failed", "read legacy install record", "~/.atomic/config.toml", "re-run `atomic update`"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	}
	if strings.Contains(errOut, "harness repair") {
		t.Errorf("stderr = %q, repair cannot fix a parse error", errOut)
	}
}

// assertAdoptedMarker requires config.toml to record the legacy install as
// adopted.
func assertAdoptedMarker(t *testing.T, home string) {
	t.Helper()
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Install.Adopted {
		t.Error("config.toml [install] adopted is not true after adoption")
	}
}

// TestUpdateSkipClaudeUpdateLeavesLegacyHomeUntouched: the opt-out skips the
// adoption along with convergence.
func TestUpdateSkipClaudeUpdateLeavesLegacyHomeUntouched(t *testing.T) {
	home := t.TempDir()
	root, _ := agedLegacyClaude(t, home)
	before := treeDigestCLI(t, root)

	deps := stubUpdateDeps(t)
	deps.migrate = func(string) error { return nil }
	deps.doctor = func(io.Writer) {}
	if code := runUpdatePostSwap(home, true, true, deps); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if treeDigestCLI(t, root) != before {
		t.Error("--skip-claude-update changed the legacy tree")
	}
	if _, err := os.Stat(config.LedgerPath(home)); !os.IsNotExist(err) {
		t.Errorf("--skip-claude-update wrote a ledger (stat err = %v)", err)
	}
}

// TestAdoptVerbStillRefusesOlderVersionBatchAndCorruptSnapshot: only update's
// adoption defaults them; the bare verb still asks.
func TestAdoptVerbStillRefusesOlderVersionBatchAndCorruptSnapshot(t *testing.T) {
	home := t.TempDir()
	root, _ := agedLegacyClaude(t, home)
	before := treeDigestCLI(t, root)

	out, code := runAtomicCLI(t, home, "harness", "adopt", "claude", "--yes")
	if code == 0 {
		t.Fatalf("bare adopt succeeded:\n%s", out)
	}
	if !strings.Contains(out, "await a replace-or-leave-unowned decision") || !strings.Contains(out, "corrupt") {
		t.Errorf("output = %q, want the batch and snapshot blockers", out)
	}
	if treeDigestCLI(t, root) != before {
		t.Error("refused adopt changed the native tree")
	}
}

// TestRunUpdatePostSwapNoopOnEmptyHome: a home with neither a legacy install
// nor an enrollment is a no-op.
func TestRunUpdatePostSwapNoopOnEmptyHome(t *testing.T) {
	home := t.TempDir()
	deps := stubUpdateDeps(t)
	deps.converge = convergeEnrolledTargets
	deps.migrate = func(string) error { return nil }
	deps.doctor = func(io.Writer) {}

	if code := runUpdatePostSwap(home, true, false, deps); code != 0 {
		t.Fatalf("empty home exit = %d, want 0", code)
	}
}

// TestAdoptReplaceVerbKeepsRecoveryCopies: the explicit verb keeps the same
// recovery copies update does.
func TestAdoptReplaceVerbKeepsRecoveryCopies(t *testing.T) {
	home := t.TempDir()
	root, older := agedLegacyClaude(t, home)

	out, code := runAtomicCLI(t, home, "harness", "adopt", "claude", "--replace", "--acknowledge-snapshot", "--yes")
	if code != 0 {
		t.Fatalf("adopt --replace exited %d:\n%s", code, out)
	}
	assertRecoveryCopies(t, home, root, older)
	assertAdoptedMarker(t, home)
}

// TestUpdateCorruptLedgerWithoutLegacyClaudeWarns: no legacy Claude install
// means no adoption to fail, so a bad ledger stays a repair warning.
func TestUpdateCorruptLedgerWithoutLegacyClaudeWarns(t *testing.T) {
	home := t.TempDir()
	ompRoot := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(ompRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, code := runAtomicCLIEnv(t, home, nil, "harness", "enroll", "omp", "--instance", ompRoot, "--yes"); code != 0 {
		t.Fatalf("enroll omp exited %d:\n%s", code, out)
	}
	mustWrite(t, config.LedgerPath(home), "{not json")

	out, errOut, code := runUpdateConverge(t, home)
	if code != 0 {
		t.Fatalf("exit = %d, want 0:\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "atomic harness repair") || strings.Contains(errOut, "adopt") {
		t.Errorf("stderr = %q, want repair advice and no adopt instruction", errOut)
	}
}
