package installstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// publishedBlock journals and publishes one block mutation for target, leaving
// the unit applied and uncommitted: the crash window before Complete.
func publishedBlock(t *testing.T, home, op, unit, target string, consumers []string, records []TargetRecord) *Transaction {
	t.Helper()
	path := filepath.Join(home, unit, "AGENTS.md")
	writeBlockFile(t, path, blockDoc)
	intended := replaceBlockBody(blockDoc, "new body")
	m := fileMutation(t, unit, path, intended)
	m.Target, m.Consumer, m.Consumers = target, target, consumers
	tx, err := NewTransaction(home, op, Plan{Mutations: []Mutation{m}, Targets: records})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile(unit, []byte(intended), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishFile(unit); err != nil {
		t.Fatal(err)
	}
	return tx
}

func recoverJournal(t *testing.T, home string, tx *Transaction, ledger *Ledger) RecoveryResult {
	t.Helper()
	journal, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewRecovery(home, journal, ledger).Recover()
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// A sibling uninstalled while the journal stayed unresolved is no longer a
// consumer; a row for it would pin the shared resource forever.
func TestRecoveryWritesNoRowForAnUninstalledConsumer(t *testing.T) {
	home := newHome(t)
	tx := publishedBlock(t, home, "op-orphan", "pkg", "omp:a", []string{"omp:b", "omp:c"}, nil)
	ledger := &Ledger{Header: NewHeader()}
	ledger.UpsertTarget(TargetRecord{Harness: "omp", Instance: "c"})

	recoverJournal(t, home, tx, ledger)

	if _, ok := ledger.Find("omp:b", "pkg"); ok {
		t.Error("recovery wrote a row for a consumer whose target is no longer enrolled")
	}
	if _, ok := ledger.Find("omp:c", "pkg"); !ok {
		t.Error("recovery dropped the row of a consumer that is still enrolled")
	}
}

// A target record is restored only for a target whose unit this pass committed,
// and an existing record keeps its recorded state.
func TestRecoveryRestoresOnlyCommittedTargetsWithoutOverwriting(t *testing.T) {
	home := newHome(t)
	records := []TargetRecord{
		{Harness: "omp", Instance: "a", NativeRoot: "/a", Status: "converged"},
		{Harness: "omp", Instance: "b", NativeRoot: "/b", Status: "converged"},
	}
	committed := publishedBlock(t, home, "op-a", "a", "omp:a", nil, records)
	conflicted := publishedBlock(t, home, "op-b", "b", "omp:b", nil, records)
	writeBlockFile(t, filepath.Join(home, "b", "AGENTS.md"), replaceBlockBody(blockDoc, "user edit"))

	ledger := &Ledger{Header: NewHeader()}
	recoverJournal(t, home, committed, ledger)
	if result := recoverJournal(t, home, conflicted, ledger); len(result.Conflicts) != 1 {
		t.Fatalf("result = %+v, want the edited unit as a conflict", result)
	}
	if _, ok := ledger.FindTarget("omp", "a"); !ok {
		t.Error("the committed target's record was not restored")
	}
	if _, ok := ledger.FindTarget("omp", "b"); ok {
		t.Error("a target whose unit conflicted was restored as enrolled")
	}

	held := &Ledger{Header: NewHeader()}
	held.UpsertTarget(TargetRecord{Harness: "omp", Instance: "a", NativeRoot: "/a", Status: "drifted"})
	again := publishedBlock(t, home, "op-a2", "a2", "omp:a", nil, records)
	recoverJournal(t, home, again, held)
	if record, _ := held.FindTarget("omp", "a"); record.Status != "drifted" {
		t.Errorf("recovery overwrote the existing record: %+v", record)
	}
}

// A journal written before targets and consumers were journaled still rolls
// forward and touches no target record.
func TestRecoveryOfAJournalWithoutTargets(t *testing.T) {
	home := newHome(t)
	tx := publishedBlock(t, home, "op-old", "old", "omp:a", nil, nil)
	raw, err := os.ReadFile(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["targets"]; ok {
		t.Fatal("the fixture journal carries a targets key")
	}

	ledger := &Ledger{Header: NewHeader()}
	result := recoverJournal(t, home, tx, ledger)
	if len(result.Conflicts) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if _, ok := ledger.Find("omp:a", "old"); !ok {
		t.Error("the applied unit was not committed")
	}
	if len(ledger.Targets) != 0 {
		t.Errorf("targets = %+v, want none", ledger.Targets)
	}
}

// A left-unowned decision made in the same operation must survive a crash
// before the ledger commit, so the next verb does not refuse it again.
func TestRecoveryRestoresLeftDecisionsOfCommittedTargets(t *testing.T) {
	home := newHome(t)
	records := []TargetRecord{{Harness: "omp", Instance: "a", NativeRoot: "/a", Status: "converged"}}
	path := filepath.Join(home, "a", "AGENTS.md")
	writeBlockFile(t, path, blockDoc)
	intended := replaceBlockBody(blockDoc, "new body")
	m := fileMutation(t, "a", path, intended)
	m.Target, m.Consumer = "omp:a", "omp:a"
	left := []LeftResource{{Target: "omp:a", Path: "/a/agents/mine.md"}}
	tx, err := NewTransaction(home, "op-left", Plan{Mutations: []Mutation{m}, Targets: records, Left: left, LeftSnapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile("a", []byte(intended), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishFile("a"); err != nil {
		t.Fatal(err)
	}

	ledger := &Ledger{Header: NewHeader()}
	recoverJournal(t, home, tx, ledger)

	if !ledger.IsLeft("omp:a", "/a/agents/mine.md") {
		t.Errorf("recovery dropped the left decision: %+v", ledger.Left)
	}
}

// The journal's left set replaces the target's: a decision the operation
// cleared must not outlive recovery, and other targets keep theirs.
func TestRecoveryReplacesLeftSetOfCommittedTargets(t *testing.T) {
	home := newHome(t)
	records := []TargetRecord{{Harness: "omp", Instance: "a", NativeRoot: "/a", Status: "converged"}}
	path := filepath.Join(home, "a", "AGENTS.md")
	writeBlockFile(t, path, blockDoc)
	intended := replaceBlockBody(blockDoc, "new body")
	m := fileMutation(t, "a", path, intended)
	m.Target, m.Consumer = "omp:a", "omp:a"
	tx, err := NewTransaction(home, "op-left-clear", Plan{Mutations: []Mutation{m}, Targets: records, LeftSnapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile("a", []byte(intended), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishFile("a"); err != nil {
		t.Fatal(err)
	}

	ledger := &Ledger{Header: NewHeader(), Left: []LeftResource{
		{Target: "omp:a", Path: "/a/agents/old.md"},
		{Target: "omp:b", Path: "/b/agents/keep.md"},
	}}
	recoverJournal(t, home, tx, ledger)

	if ledger.IsLeft("omp:a", "/a/agents/old.md") {
		t.Error("a decision the operation cleared survived recovery")
	}
	if !ledger.IsLeft("omp:b", "/b/agents/keep.md") {
		t.Error("recovery dropped another target's left decision")
	}
}

// A journal written before left decisions were journaled leaves the ledger's
// left set untouched.
func TestRecoveryOfAJournalWithoutLeftKeepsLedgerLeft(t *testing.T) {
	home := newHome(t)
	records := []TargetRecord{{Harness: "omp", Instance: "a", NativeRoot: "/a", Status: "converged"}}
	tx := publishedBlock(t, home, "op-old-left", "old", "omp:a", nil, records)
	ledger := &Ledger{Header: NewHeader(), Left: []LeftResource{{Target: "omp:a", Path: "/a/agents/mine.md"}}}

	recoverJournal(t, home, tx, ledger)

	if !ledger.IsLeft("omp:a", "/a/agents/mine.md") {
		t.Errorf("recovery of a journal without left cleared the ledger's decisions: %+v", ledger.Left)
	}
}
