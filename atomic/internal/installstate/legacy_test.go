package installstate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

func adoptReq(home, root string, t *testing.T) AdoptionRequest {
	t.Helper()
	return AdoptionRequest{
		Home:          home,
		NativeRoot:    root,
		Target:        "claude:default",
		Generation:    "gen-selected",
		Tier:          "native",
		Artifacts:     selectedArtifacts(t, root),
		BatchDecision: DecisionReplace,
	}
}

func TestPlanAdoptionBatchesOlderArtifactsAndHonorsDecisions(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	undecided := adoptReq(home, root, t)
	undecided.BatchDecision = ""
	plan, err := PlanAdoption(undecided)
	if err != nil {
		t.Fatalf("PlanAdoption: %v", err)
	}
	if plan.Status != StatusBlocked {
		t.Fatalf("status = %s, want %s", plan.Status, StatusBlocked)
	}
	if len(plan.Batch) != len(embedded.Manifest())-1 {
		t.Fatalf("batch = %d, want %d", len(plan.Batch), len(embedded.Manifest())-1)
	}

	replace, err := PlanAdoption(adoptReq(home, root, t))
	if err != nil {
		t.Fatal(err)
	}
	if replace.Status != StatusReady {
		t.Fatalf("replace status = %s (%v), want ready", replace.Status, replace.Blockers)
	}
	if len(replace.Batch) != 0 {
		t.Errorf("replace still awaits decisions: %v", replace.Batch)
	}
	if actionFor(replace, "commands/commit.md") != ActionReplace {
		t.Errorf("commands/commit.md action = %s, want replace", actionFor(replace, "commands/commit.md"))
	}
	if actionFor(replace, "CLAUDE.md") != ActionAdopt {
		t.Errorf("CLAUDE.md action = %s, want adopt", actionFor(replace, "CLAUDE.md"))
	}

	leave := adoptReq(home, root, t)
	leave.BatchDecision = DecisionLeaveUnowned
	preserved, err := PlanAdoption(leave)
	if err != nil {
		t.Fatal(err)
	}
	if preserved.Status != StatusReady {
		t.Fatalf("leave status = %s (%v), want ready", preserved.Status, preserved.Blockers)
	}
	if actionFor(preserved, "commands/commit.md") != ActionLeaveUnowned {
		t.Errorf("commands/commit.md action = %s, want leave-unowned", actionFor(preserved, "commands/commit.md"))
	}
}

func TestAdoptLegacyCompleteEndToEnd(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	res, err := Adopt(adoptReq(home, root, t))
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if res.Plan.Status != StatusReady {
		t.Fatalf("status = %s, want ready", res.Plan.Status)
	}
	if len(res.Applied) != len(embedded.Manifest()) {
		t.Fatalf("applied %d resources, want %d", len(res.Applied), len(embedded.Manifest()))
	}

	for _, a := range res.Plan.Resources {
		artifact := findArtifact(t, root, a.ID)
		got, err := os.ReadFile(a.Path)
		if err != nil {
			t.Fatalf("read adopted %s: %v", a.Path, err)
		}
		switch a.Kind {
		case managedfile.KindBlock:
			block, err := managedfile.ManagedBlock(got)
			if err != nil {
				t.Fatalf("%s block: %v", a.Path, err)
			}
			want, err := managedfile.ManagedBlock(artifact.Data)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(block, want) {
				t.Errorf("%s block not adopted to the selected generation", a.Path)
			}
		default:
			if !bytes.Equal(got, artifact.Data) {
				t.Errorf("%s not adopted to the selected generation", a.Path)
			}
		}
	}

	ledger, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Rows) != len(embedded.Manifest()) {
		t.Errorf("ledger rows = %d, want %d", len(ledger.Rows), len(embedded.Manifest()))
	}
	row, ok := ledger.Find("claude:default", "commands/commit.md")
	if !ok {
		t.Fatal("no ledger row for commands/commit.md")
	}
	want := findArtifact(t, root, "commands/commit.md").Data
	if row.Applied.Digest != managedfile.Digest(want) {
		t.Errorf("ledger digest = %s, want %s", row.Applied.Digest, managedfile.Digest(want))
	}

	// The write-once snapshot is preserved exactly.
	if _, err := os.Stat(filepath.Join(config.PreInstallDir(home), "manifest.json")); err != nil {
		t.Errorf("pre-install snapshot lost: %v", err)
	}

	steering, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(steering, []byte("user prose before the block\n")) ||
		!bytes.HasSuffix(steering, []byte("user prose after the block\n")) {
		t.Errorf("adoption did not preserve unowned prose around the block:\n%s", steering)
	}
}

func TestAdoptLegacyPartialEndToEnd(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	ageInstall(t, home, root)
	deleteListed(t, root, "commands/commit.md")

	classified, err := Classify(classifyReq(home, root, t))
	if err != nil {
		t.Fatal(err)
	}
	if classified.State != StateLegacyPartial {
		t.Fatalf("state = %s, want %s", classified.State, StateLegacyPartial)
	}

	res, err := Adopt(adoptReq(home, root, t))
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if actionFor(res.Plan, "commands/commit.md") != ActionRecreate {
		t.Fatalf("missing listed file action = %s, want recreate", actionFor(res.Plan, "commands/commit.md"))
	}
	got, err := os.ReadFile(filepath.Join(root, "commands", "commit.md"))
	if err != nil {
		t.Fatalf("recreated file: %v", err)
	}
	if !bytes.Equal(got, findArtifact(t, root, "commands/commit.md").Data) {
		t.Error("recreated file does not hold the selected generation's bytes")
	}
}

func TestAdoptLeaveUnownedPreservesArtifacts(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	preserved := filepath.Join(root, "commands", "commit.md")
	oldBytes, err := os.ReadFile(preserved)
	if err != nil {
		t.Fatal(err)
	}

	req := adoptReq(home, root, t)
	req.BatchDecision = DecisionLeaveUnowned
	res, err := Adopt(req)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if actionFor(res.Plan, "commands/commit.md") != ActionLeaveUnowned {
		t.Fatalf("action = %s, want leave-unowned", actionFor(res.Plan, "commands/commit.md"))
	}
	got, err := os.ReadFile(preserved)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, oldBytes) {
		t.Error("leave-unowned artifact was rewritten")
	}
	ledger, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find("claude:default", "commands/commit.md"); ok {
		t.Error("leave-unowned artifact recorded an ownership row")
	}
	if _, ok := ledger.Find("claude:default", "CLAUDE.md"); !ok {
		t.Error("the managed block was not adopted")
	}
}

func TestAdoptRefusesBlockedStates(t *testing.T) {
	t.Run("pending proposal", func(t *testing.T) {
		home := newHome(t)
		root := installLegacy(t, home)
		writeProposal(t, home)
		_, err := Adopt(adoptReq(home, root, t))
		if !errors.Is(err, ErrAdoptionRefused) {
			t.Fatalf("error = %v, want ErrAdoptionRefused", err)
		}
	})

	t.Run("corrupt snapshot", func(t *testing.T) {
		home := newHome(t)
		root := installLegacy(t, home)
		ageInstall(t, home, root)
		corruptSnapshot(t, home)

		_, err := Adopt(adoptReq(home, root, t))
		if !errors.Is(err, ErrAdoptionRefused) {
			t.Fatalf("error = %v, want ErrAdoptionRefused", err)
		}

		req := adoptReq(home, root, t)
		req.AcknowledgeSnapshot = true
		if _, err := Adopt(req); err != nil {
			t.Fatalf("acknowledged adoption failed: %v", err)
		}
	})

	t.Run("mixed", func(t *testing.T) {
		home := newHome(t)
		root := installLegacy(t, home)
		ageInstall(t, home, root)
		drifted := filepath.Join(root, "commands", "commit.md")
		writeLedgerFixture(t, home, nil, []Row{{
			Target:   "claude:default",
			Resource: "commands/commit.md",
			Applied:  AppliedValue{Path: drifted, Kind: managedfile.KindFile, Digest: "0000"},
		}})
		_, err := Adopt(adoptReq(home, root, t))
		if !errors.Is(err, ErrAdoptionRefused) {
			t.Fatalf("error = %v, want ErrAdoptionRefused", err)
		}
	})

	t.Run("unjournaled v2 remnant", func(t *testing.T) {
		home := newHome(t)
		if err := os.MkdirAll(config.TransactionStageDir(home, "op-orphan"), 0o755); err != nil {
			t.Fatal(err)
		}
		req := AdoptionRequest{Home: home, NativeRoot: filepath.Join(home, ".claude"), Target: "claude:default"}
		plan, err := PlanAdoption(req)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Status != StatusBlocked {
			t.Fatalf("status = %s, want %s", plan.Status, StatusBlocked)
		}
		if _, err := Adopt(req); !errors.Is(err, ErrAdoptionRefused) {
			t.Fatalf("error = %v, want ErrAdoptionRefused", err)
		}
	})
}

func TestPlanAdoptionIsFilesystemIdentical(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	before := treeDigest(t, home)
	if _, err := PlanAdoption(adoptReq(home, root, t)); err != nil {
		t.Fatalf("PlanAdoption: %v", err)
	}
	after := treeDigest(t, home)
	if before != after {
		t.Error("dry run changed the filesystem")
	}
	if _, err := os.Stat(config.OperationLockPath(home)); !os.IsNotExist(err) {
		t.Errorf("dry run created the lock path: %v", err)
	}
}

func TestPlanAdoptionAdvisoryRecoveryOutcomes(t *testing.T) {
	t.Run("applied but unrecorded", func(t *testing.T) {
		home := newHome(t)
		path := filepath.Join(home, ".claude", "commands", "commit.md")
		data := []byte("selected bytes\n")
		mkfile(t, path, string(data))

		m := fileMutationAt(t, "commands.commit.md", path, data)
		m.PriorObserved = true
		m.Backup = filepath.Join(config.TransactionBackupDir(home, "op-apply"), "commands.commit.md")
		m.BackupSum = "unused"
		writeJournalFixture(t, home, journalWith("op-apply", []Mutation{m},
			[]Progress{{Unit: m.Unit, State: StateApplied, At: testTime()}}))

		plan, err := PlanAdoption(AdoptionRequest{Home: home, NativeRoot: filepath.Join(home, ".claude"), Target: "claude:default"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Status == StatusBlockedOnRecovery {
			t.Fatalf("status = %s, want an advisory plan", plan.Status)
		}
		if !hasDecision(plan, DecisionCommitApplied) {
			t.Errorf("recovery advisory = %+v, want commit-applied", plan.Recovery)
		}
	})

	t.Run("discardable staging", func(t *testing.T) {
		home := newHome(t)
		path := filepath.Join(home, ".claude", "commands", "plan.md")
		mkfile(t, path, "pre-mutation bytes\n")

		m := fileMutationAt(t, "commands.plan.md", path, []byte("staged bytes\n"))
		m.Stage = filepath.Join(config.TransactionStageDir(home, "op-stage"), "commands.plan.md")
		mkfile(t, m.Stage, "staged bytes\n")
		writeJournalFixture(t, home, journalWith("op-stage", []Mutation{m},
			[]Progress{{Unit: m.Unit, State: StateStaged, At: testTime()}}))

		plan, err := PlanAdoption(AdoptionRequest{Home: home, NativeRoot: filepath.Join(home, ".claude"), Target: "claude:default"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Status == StatusBlockedOnRecovery {
			t.Fatalf("status = %s, want an advisory plan", plan.Status)
		}
		if !hasDecision(plan, DecisionDiscardStaging) {
			t.Errorf("recovery advisory = %+v, want discard-staging", plan.Recovery)
		}
	})

	t.Run("later edit blocks", func(t *testing.T) {
		home := newHome(t)
		path := filepath.Join(home, ".claude", "commands", "commit.md")
		mkfile(t, path, "edited after the operation\n")

		m := fileMutationAt(t, "commands.commit.md", path, []byte("intended bytes\n"))
		m.PriorObserved = true
		m.BackupSum = "different"
		writeJournalFixture(t, home, journalWith("op-edit", []Mutation{m},
			[]Progress{{Unit: m.Unit, State: StateApplied, At: testTime()}}))

		before := treeDigest(t, home)
		plan, err := PlanAdoption(AdoptionRequest{Home: home, NativeRoot: filepath.Join(home, ".claude"), Target: "claude:default"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Status != StatusBlockedOnRecovery {
			t.Fatalf("status = %s, want %s", plan.Status, StatusBlockedOnRecovery)
		}
		if after := treeDigest(t, home); before != after {
			t.Error("blocked dry run changed the filesystem")
		}
	})
}

func TestAdoptResumesInterruptedOperation(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	artifacts := selectedArtifacts(t, root)
	victim := findArtifact(t, root, "commands/commit.md")
	digest, err := managedfile.DigestResourceBytes(victim.Data, victim.Kind)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := NewTransaction(home, "op-interrupted", Plan{Mutations: []Mutation{{
		Unit: "commands.commit.md", Resource: victim.ID, Target: "claude:default",
		Kind: victim.Kind, Path: victim.Path, Intended: digest,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile("commands.commit.md", victim.Data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishFile("commands.commit.md"); err != nil {
		t.Fatal(err)
	}
	// Deliberately not Complete: the journal owns applied-but-unrecorded work.

	req := adoptReq(home, root, t)
	req.Artifacts = artifacts
	res, err := Adopt(req)
	if err != nil {
		t.Fatalf("Adopt after interruption: %v", err)
	}
	if !hasDecisionIn(res.Recovery, DecisionCommitApplied) {
		t.Errorf("recovery actions = %+v, want commit-applied", res.Recovery)
	}
	ledger, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find("claude:default", victim.ID); !ok {
		t.Error("recovered resource was not ledger-committed")
	}
	j, err := LoadJournal(config.JournalPath(home, "op-interrupted"))
	if err != nil {
		t.Fatal(err)
	}
	if !j.Completed {
		t.Error("recovered journal was not consumed")
	}
}

func TestAdoptIsIdempotentOnRerun(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	if _, err := Adopt(adoptReq(home, root, t)); err != nil {
		t.Fatalf("first Adopt: %v", err)
	}
	journals := journalCount(t, home)

	res, err := Adopt(adoptReq(home, root, t))
	if err != nil {
		t.Fatalf("second Adopt: %v", err)
	}
	if res.Plan.Status != StatusConverged {
		t.Fatalf("second status = %s, want %s", res.Plan.Status, StatusConverged)
	}
	if len(res.Applied) != 0 {
		t.Errorf("second adoption applied %v, want nothing", res.Applied)
	}
	if got := journalCount(t, home); got != journals {
		t.Errorf("journal count = %d, want %d: an already-converged plan creates no journal", got, journals)
	}
}

func TestAdoptSerializesOnLifecycleLock(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	lock, err := AcquireLock(home, WriterIdentity{OperationID: "concurrent-holder"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := Adopt(adoptReq(home, root, t))
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("Adopt completed while the lifecycle lock was held: %v", err)
	case <-time.After(250 * time.Millisecond):
	}

	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Adopt after lock release: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Adopt did not proceed after the lock was released")
	}
}

func TestAdoptStateRootPersistenceAndRollback(t *testing.T) {
	t.Run("persists the selection", func(t *testing.T) {
		home := newHome(t)
		root := installLegacy(t, home)
		ageInstall(t, home, root)
		repoRoot := t.TempDir()
		mkfile(t, filepath.Join(repoRoot, ".claude", "atomic.toml"), "scope marker\n")

		req := adoptReq(home, root, t)
		req.RepoRoot = repoRoot
		if _, err := Adopt(req); err != nil {
			t.Fatalf("Adopt: %v", err)
		}
		sel, ok, err := config.ReadStateSelection(repoRoot)
		if err != nil || !ok {
			t.Fatalf("state selection = %+v ok=%v err=%v", sel, ok, err)
		}
		if sel.StateDir != ".claude" {
			t.Errorf("state_dir = %q, want .claude", sel.StateDir)
		}
	})

	t.Run("rolls back when no later write occurred", func(t *testing.T) {
		home := newHome(t)
		root := installLegacy(t, home)
		ageInstall(t, home, root)
		repoRoot := t.TempDir()
		mkfile(t, filepath.Join(repoRoot, ".claude", "atomic.toml"), "scope marker\n")

		// Make the first mutation unpublishable: its parent directory denies writes.
		blockedDir := filepath.Join(root, "commands")
		if err := os.Chmod(blockedDir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(blockedDir, 0o755) })

		req := adoptReq(home, root, t)
		req.RepoRoot = repoRoot
		victim := findArtifact(t, root, "commands/atomic-help.md")
		req.Artifacts = append([]Artifact{victim}, selectedArtifacts(t, root)...)

		if _, err := Adopt(req); err == nil {
			t.Fatal("Adopt succeeded despite the unpublishable resource")
		}
		if _, ok, err := config.ReadStateSelection(repoRoot); err != nil {
			t.Fatal(err)
		} else if ok {
			t.Error("state selection survived a failed adoption; want rollback")
		}
	})
}

func TestAdoptSurfacesOldBinaryDrift(t *testing.T) {
	home := newHome(t)
	root := installLegacy(t, home)
	ageInstall(t, home, root)

	if _, err := Adopt(adoptReq(home, root, t)); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	clean, err := Classify(classifyReq(home, root, t))
	if err != nil {
		t.Fatal(err)
	}
	if clean.State != StateV2Clean {
		t.Fatalf("post-adoption state = %s (%v), want %s", clean.State, clean.Conflicts, StateV2Clean)
	}

	// An old Claude-only binary rewrites one artifact with its own generation.
	mkfile(t, filepath.Join(root, "commands", "commit.md"), "old binary wrote this\n")

	drifted, err := Classify(classifyReq(home, root, t))
	if err != nil {
		t.Fatal(err)
	}
	if drifted.State != StateMixed {
		t.Fatalf("drifted state = %s, want %s", drifted.State, StateMixed)
	}
	if len(drifted.Conflicts) == 0 {
		t.Error("old-binary drift reported no conflict")
	}
}

func actionFor(p AdoptionPlan, id string) AdoptionAction {
	for _, r := range p.Resources {
		if r.ID == id {
			return r.Action
		}
	}
	return ""
}

func hasDecision(plan AdoptionPlan, want RecoveryDecision) bool {
	for _, sim := range plan.Recovery {
		for _, a := range sim.Actions {
			if a.Decision == want {
				return true
			}
		}
	}
	return false
}

func hasDecisionIn(actions []RecoveryAction, want RecoveryDecision) bool {
	for _, a := range actions {
		if a.Decision == want {
			return true
		}
	}
	return false
}

func findArtifact(t *testing.T, nativeRoot, id string) Artifact {
	t.Helper()
	for _, a := range selectedArtifacts(t, nativeRoot) {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("artifact %s not found", id)
	return Artifact{}
}
