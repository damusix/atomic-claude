package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
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

// TestRunUpdate_StagedSwapReExecsReplacementAndRefusesStalePublish drives the
// full swap → re-exec → converge path: a staged archive swaps the binary, the
// stale process refuses to publish its own corpus, and the replacement binary
// converges the enrolled target and records its embedded generation.
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

// enrollCodexTarget enrolls a Codex home at CODEX_HOME through the real install
// engine and returns the native root plus the generation the enrollment
// recorded, so update convergence has a non-Claude owner to reconverge.
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
// covers an enrolled Codex target through the same generic path as any other
// harness: the plugin package is reconverged in place, the recorded generation
// is unchanged, and no second target is invented.
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

// TestRunUpdate_UnresolvableHomePerformsNoConfigIO proves both post-swap paths
// degrade to the raw run when the home cannot be resolved. An unguarded migrate
// resolved ./.atomic/config.toml against the process CWD, recreated it there,
// and ran install migrations against a relative .claude.
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
// unresolvable case: a resolvable home still migrates, and the migration writes
// under that home rather than the process CWD.
func TestRunUpdate_ResolvableHomeMigratesUnderHome(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	home := t.TempDir()
	t.Setenv("HOME", home)

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
	if _, err := os.Stat(config.TOMLPath(home)); err != nil {
		t.Errorf("resolvable-home update did not migrate under $HOME: %v", err)
	}
	entries, err := os.ReadDir(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		t.Errorf("resolvable-home update wrote into the working directory: %v", entries)
	}
}

// TestRunUpdate_SkipOptOutSuppressesConvergence proves --skip-claude-update
// suppresses convergence on the stale path while still delegating migrations to
// the replacement, and suppresses it on the already-current path while keeping
// the migrations that follow.
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

// TestUpdateConvergenceRecoversInterruptedUpdate proves an update interrupted
// between a native write and its ledger commit leaves a journal the next
// convergence recovers before it mutates.
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

// TestUpdateConvergenceBlocksOnLifecycleLock proves update convergence runs
// through the one lifecycle lock: while another operator holds it, convergence
// waits rather than writing around the holder.
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

// TestEnrollConvergenceRegistersHookAndSeedsOutputStyle closes the gap the
// enrollment verbs claimed but no converge path performed: `atomic install
// --harness claude` (and `harness enroll`) must register the SessionStart hook
// and seed the outputStyle key. It drives the production enroll entry, proves
// both settings mutations land, that the user's own keys survive, and that a
// reconvergence is byte-identical rather than duplicating the hook or
// re-seeding.
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

// TestRunUpdatePostSwapRefusesLegacyUnenrolledClaude is the P1-6 regression: a
// legacy Claude install no ledger target owns must not update silently. Before
// the fix convergeEnrolledTargets returned nil for an empty report set, so
// `atomic update` exited 0 and froze the artifacts with no message.
func TestRunUpdatePostSwapRefusesLegacyUnenrolledClaude(t *testing.T) {
	home := t.TempDir()
	if out, code := runAtomicCLI(t, home, "claude", "install", "--no-hooks"); code != 0 {
		t.Fatalf("legacy install exited %d:\n%s", code, out)
	}
	if _, err := os.Stat(config.LedgerPath(home)); !os.IsNotExist(err) {
		t.Fatalf("the legacy install wrote a ledger (stat err = %v)", err)
	}

	deps := stubUpdateDeps(t)
	deps.converge = convergeEnrolledTargets
	deps.migrate = func(string) error { return nil }
	deps.doctor = func(io.Writer) {}
	var errOut strings.Builder
	deps.errOut = &errOut

	if code := runUpdatePostSwap(home, true, false, deps); code == 0 {
		t.Fatal("a legacy unenrolled install converged as a no-op (exit 0)")
	}
	if !strings.Contains(errOut.String(), "atomic harness adopt claude") {
		t.Errorf("stderr = %q, want the adopt instruction", errOut.String())
	}
}

// TestRunUpdatePostSwapNoopOnEmptyHome proves the refusal narrows to legacy
// evidence: a home with neither an installation nor an enrollment stays a
// silent no-op.
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

// TestObsoleteHarnessRefusalAtRoot is the P1-7 regression: the root command must
// surface the retired ATOMIC_HARNESS refusal with instructions instead of the
// quiet repo-local resolver ignoring it.
func TestObsoleteHarnessRefusalAtRoot(t *testing.T) {
	t.Setenv(config.StateDirEnvVar, "")
	t.Setenv(config.ObsoleteHarnessEnvVar, "")
	var out strings.Builder
	if obsoleteHarnessRefusal(&out) {
		t.Fatalf("an unset ATOMIC_HARNESS refused: %s", out.String())
	}
	if out.Len() != 0 {
		t.Errorf("unset variable wrote %q", out.String())
	}

	t.Setenv(config.ObsoleteHarnessEnvVar, "pi")
	out.Reset()
	if !obsoleteHarnessRefusal(&out) {
		t.Fatal("an obsolete ATOMIC_HARNESS did not refuse at the root")
	}
	if !strings.Contains(out.String(), config.StateDirEnvVar) {
		t.Errorf("refusal %q does not name %s", out.String(), config.StateDirEnvVar)
	}
}
