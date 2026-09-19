// CP8A determinism and concurrency scenarios: byte-identical dry runs, advisory
// recovery plans, blocked later-edit recovery, and the one advisory lifecycle
// lock that serializes update, uninstall, and `doctor --fix`.
package runtimeproof

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// yesPrompter answers every fix prompt affirmatively.
type yesPrompter struct{}

func (yesPrompter) Confirm(string) doctor.Decision { return doctor.DecisionYes }
func (yesPrompter) Indexed([]string) int           { return 0 }

// TestCP8ADryRunsAreFilesystemIdentical proves the planning forms of install,
// uninstall, and adoption leave the isolated home byte-identical.
//
// Criterion: a dry run leaves the complete filesystem byte-identical.
func TestCP8ADryRunsAreFilesystemIdentical(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp8a/determinism/dry-runs-identical",
		Group:     "determinism",
		Engine:    "install + installstate",
		Criterion: "install, uninstall, and adoption dry runs leave the filesystem byte-identical",
		Command:   "Converge(dry) → UninstallAll(dry) → PlanAdoption",
	}

	t.Run("install dry run", func(t *testing.T) {
		home := isolatedHome(t)
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		steps := scenarioSteps(home)
		steps.DryRun = true
		before := treeDigest(t, home)
		if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}, Enroll: true}); err != nil {
			t.Fatalf("dry-run install: %v", err)
		}
		if after := treeDigest(t, home); after != before {
			t.Errorf("install dry run changed the filesystem")
		}
		if _, err := os.Stat(filepath.Join(home, ".atomic")); !os.IsNotExist(err) {
			t.Errorf(".atomic was created by a dry run (stat err = %v)", err)
		}
		ev.Paths = append(ev.Paths, home)
	})

	t.Run("uninstall dry run", func(t *testing.T) {
		home := isolatedHome(t)
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		steps := scenarioSteps(home)
		if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}, Enroll: true}); err != nil {
			t.Fatalf("install: %v", err)
		}
		steps.DryRun = true
		before := treeDigest(t, home)
		if _, err := steps.UninstallAll(); err != nil {
			t.Fatalf("dry-run uninstall: %v", err)
		}
		if after := treeDigest(t, home); after != before {
			t.Errorf("uninstall dry run changed the filesystem")
		}
		ev.Paths = append(ev.Paths, home)
	})

	t.Run("adoption dry run", func(t *testing.T) {
		home := isolatedHome(t)
		root := legacyClaudeInstall(t, home)
		ageClaudeInstall(t, home, root)
		before := treeDigest(t, home)
		if _, err := installstate.PlanAdoption(cp8aAdoptReq(t, home, root)); err != nil {
			t.Fatalf("plan adoption: %v", err)
		}
		if after := treeDigest(t, home); after != before {
			t.Errorf("adoption dry run changed the filesystem")
		}
		if _, err := os.Stat(config.OperationLockPath(home)); !os.IsNotExist(err) {
			t.Errorf("adoption dry run created the lock path: %v", err)
		}
		ev.Paths = append(ev.Paths, home, root)
	})

	ev.Outcome = "install, uninstall, and adoption dry runs were byte-identical"
	recordScenario(t, ev)
}

// TestCP8AAdvisoryRecoveryPlans proves a dry run over an unresolved journal
// yields the advisory post-recovery plan its own evidence determines:
// commit-applied for an applied-but-unrecorded write, discard-staging for a
// provably unused staged projection.
//
// Criterion: current journal/ledger evidence that determines one safe result
// yields an advisory post-recovery plan.
func TestCP8AAdvisoryRecoveryPlans(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp8a/determinism/advisory-recovery-plans",
		Group:     "determinism",
		Engine:    "installstate",
		Criterion: "dry-run recovery reports the advisory plan the journal evidence determines",
		Command:   "PlanAdoption over an applied-unrecorded journal and a staged journal",
	}

	t.Run("applied but unrecorded", func(t *testing.T) {
		home := isolatedHome(t)
		root := filepath.Join(home, ".claude")
		path := filepath.Join(root, "commands", "commit.md")
		data := []byte("selected bytes\n")
		cp8aMkfile(t, path, string(data))
		m := cp8aMutation(t, "commands.commit.md", path, data)
		m.PriorObserved = true
		m.Backup = filepath.Join(config.TransactionBackupDir(home, "op-apply"), "commands.commit.md")
		m.BackupSum = "unused"
		cp8aWriteJournal(t, home, cp8aJournal("op-apply", []installstate.Mutation{m},
			[]installstate.Progress{{Unit: m.Unit, State: installstate.StateApplied, At: cp8aFixedTime()}}))

		plan, err := installstate.PlanAdoption(installstate.AdoptionRequest{Home: home, NativeRoot: root, Target: "claude:default"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Status == installstate.StatusBlockedOnRecovery {
			t.Fatalf("status = %s, want an advisory plan", plan.Status)
		}
		if !cp8aHasRecovery(plan, installstate.DecisionCommitApplied) {
			t.Errorf("recovery advisory = %+v, want commit-applied", plan.Recovery)
		}
		ev.Paths = append(ev.Paths, home)
	})

	t.Run("discardable staging", func(t *testing.T) {
		home := isolatedHome(t)
		root := filepath.Join(home, ".claude")
		path := filepath.Join(root, "commands", "plan.md")
		cp8aMkfile(t, path, "pre-mutation bytes\n")
		m := cp8aMutation(t, "commands.plan.md", path, []byte("staged bytes\n"))
		m.Stage = filepath.Join(config.TransactionStageDir(home, "op-stage"), "commands.plan.md")
		cp8aMkfile(t, m.Stage, "staged bytes\n")
		cp8aWriteJournal(t, home, cp8aJournal("op-stage", []installstate.Mutation{m},
			[]installstate.Progress{{Unit: m.Unit, State: installstate.StateStaged, At: cp8aFixedTime()}}))

		plan, err := installstate.PlanAdoption(installstate.AdoptionRequest{Home: home, NativeRoot: root, Target: "claude:default"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Status == installstate.StatusBlockedOnRecovery {
			t.Fatalf("status = %s, want an advisory plan", plan.Status)
		}
		if !cp8aHasRecovery(plan, installstate.DecisionDiscardStaging) {
			t.Errorf("recovery advisory = %+v, want discard-staging", plan.Recovery)
		}
		ev.Paths = append(ev.Paths, home)
	})

	ev.Outcome = "applied-unrecorded journal yielded commit-applied; staged journal yielded discard-staging"
	recordScenario(t, ev)
}

// TestCP8ABlockedLaterEditRecovery proves a journal whose result depends on a
// later edit cannot be reconciled to one safe result: the dry run reports
// blocked_on_recovery and changes nothing.
//
// Criterion: a later edit yields blocked_on_recovery with a byte-identical
// filesystem.
func TestCP8ABlockedLaterEditRecovery(t *testing.T) {
	home := isolatedHome(t)
	root := filepath.Join(home, ".claude")
	path := filepath.Join(root, "commands", "commit.md")
	cp8aMkfile(t, path, "edited after the operation\n")

	m := cp8aMutation(t, "commands.commit.md", path, []byte("intended bytes\n"))
	m.PriorObserved = true
	m.BackupSum = "different"
	cp8aWriteJournal(t, home, cp8aJournal("op-edit", []installstate.Mutation{m},
		[]installstate.Progress{{Unit: m.Unit, State: installstate.StateApplied, At: cp8aFixedTime()}}))

	ev := ScenarioEvidence{
		Scenario:  "cp8a/determinism/blocked-later-edit",
		Group:     "determinism",
		Engine:    "installstate",
		Criterion: "a later edit yields blocked_on_recovery and a byte-identical filesystem",
		Command:   "PlanAdoption over an applied journal with a later edit",
		Paths:     []string{home, root},
	}

	before := treeDigest(t, home)
	plan, err := installstate.PlanAdoption(installstate.AdoptionRequest{Home: home, NativeRoot: root, Target: "claude:default"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != installstate.StatusBlockedOnRecovery {
		t.Fatalf("status = %s, want %s", plan.Status, installstate.StatusBlockedOnRecovery)
	}
	if after := treeDigest(t, home); after != before {
		t.Errorf("a blocked dry run changed the filesystem")
	}

	ev.Outcome = "later-edit journal blocked on recovery; filesystem unchanged"
	recordScenario(t, ev)
}

// TestCP8AConcurrentLifecycleOperations proves update convergence, uninstall,
// and `doctor --fix` all serialize on the one advisory lifecycle lock: each
// blocks while the lock is held and completes once it is released.
//
// Criterion: install, update, repair, and uninstall acquire one global advisory
// lifecycle lock; a ledger-managed doctor --fix acquires it too.
func TestCP8AConcurrentLifecycleOperations(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp8a/determinism/concurrent-lifecycle-lock",
		Group:     "determinism",
		Engine:    "install + doctor + installstate",
		Criterion: "update convergence, uninstall, and doctor --fix serialize on the one advisory lifecycle lock",
		Command:   "hold AcquireLock → run each operation → release",
	}

	// blockThenRun holds the home's lifecycle lock, starts op, asserts it has not
	// completed while the lock is held, releases, and asserts op finishes.
	blockThenRun := func(t *testing.T, home string, op func() error) {
		t.Helper()
		lock, err := installstate.AcquireLock(home, installstate.WriterIdentity{OperationID: "cp8a-concurrent"})
		if err != nil {
			t.Fatalf("acquire lock: %v", err)
		}
		done := make(chan error, 1)
		go func() { done <- op() }()
		select {
		case err := <-done:
			_ = lock.Release()
			t.Fatalf("operation completed while the lifecycle lock was held (err=%v)", err)
		case <-time.After(250 * time.Millisecond):
		}
		if err := lock.Release(); err != nil {
			t.Fatalf("release lock: %v", err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("operation after lock release: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("operation did not proceed after the lock was released")
		}
	}

	t.Run("update convergence", func(t *testing.T) {
		home := isolatedHome(t)
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		steps := scenarioSteps(home)
		if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}, Enroll: true}); err != nil {
			t.Fatalf("enroll: %v", err)
		}
		blockThenRun(t, home, func() error {
			_, err := steps.ConvergeEnrolled()
			return err
		})
		ev.Paths = append(ev.Paths, home)
	})

	t.Run("uninstall", func(t *testing.T) {
		home := isolatedHome(t)
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		steps := scenarioSteps(home)
		if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}, Enroll: true}); err != nil {
			t.Fatalf("enroll: %v", err)
		}
		blockThenRun(t, home, func() error {
			_, err := steps.UninstallAll()
			return err
		})
		ev.Paths = append(ev.Paths, home)
	})

	t.Run("doctor fix", func(t *testing.T) {
		home := isolatedHome(t)
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		t.Setenv("HOME", home)
		steps := scenarioSteps(home)
		if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}, Enroll: true}); err != nil {
			t.Fatalf("enroll: %v", err)
		}
		results := []doctor.Result{{
			Index:    21,
			Name:     "staleness",
			Severity: doctor.WARN,
			Detail:   "materialized generation is behind the selected projection",
		}}
		var summary doctor.RepairSummary
		blockThenRun(t, home, func() error {
			summary = doctor.Repair(results, doctor.Opts{Home: home}, yesPrompter{}, io.Discard)
			return nil
		})
		if summary.Applied != 1 {
			t.Errorf("doctor fix summary = %+v, want one applied repair after the lock was released", summary)
		}
		ev.Paths = append(ev.Paths, home)
	})

	ev.Outcome = "update convergence, uninstall, and doctor --fix each blocked while the lock was held and completed after release"
	recordScenario(t, ev)
}
