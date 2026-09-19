package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// TestStateAdoptSelectsTheOnlyPopulatedRoot proves the unambiguous case records
// the existing root rather than creating or moving one.
func TestStateAdoptSelectsTheOnlyPopulatedRoot(t *testing.T) {
	repo := t.TempDir()
	markStateRoot(t, repo, ".pi")

	report, err := stateAdopt(stateOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("stateAdopt: %v", err)
	}
	if report.Action != "adopt" || report.StateDir != ".pi" {
		t.Fatalf("report = %+v, want the .pi root adopted", report)
	}
	selection, ok, err := config.ReadStateSelection(repo)
	if err != nil || !ok {
		t.Fatalf("selection = %v, %v, ok=%v", selection, err, ok)
	}
	if selection.StateDir != ".pi" {
		t.Errorf("recorded state_dir = %q, want .pi", selection.StateDir)
	}
}

// TestStateAdoptDryRunWritesNothing proves the planning form is byte-identical.
func TestStateAdoptDryRunWritesNothing(t *testing.T) {
	repo := t.TempDir()
	markStateRoot(t, repo, ".pi")

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

// TestStateAdoptRefusesAmbiguousRoots proves two populated candidates require an
// explicit choice.
func TestStateAdoptRefusesAmbiguousRoots(t *testing.T) {
	repo := t.TempDir()
	markStateRoot(t, repo, ".claude")
	markStateRoot(t, repo, ".pi")

	if _, err := stateAdopt(stateOptions{RepoRoot: repo}); err == nil {
		t.Fatal("ambiguous state roots were adopted without a choice")
	}
	report, err := stateAdopt(stateOptions{RepoRoot: repo, Dir: ".pi"})
	if err != nil {
		t.Fatalf("explicit selection: %v", err)
	}
	if report.Action != "select" || report.StateDir != ".pi" {
		t.Fatalf("report = %+v", report)
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
	markStateRoot(t, repo, ".pi")
	if _, err := stateAdopt(stateOptions{RepoRoot: repo}); err != nil {
		t.Fatalf("adopt: %v", err)
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
