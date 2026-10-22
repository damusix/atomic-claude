package installstate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

const blockDoc = "user prose\n<atomic>\nold body\n</atomic>\ntrailing prose\n"

// newHome points HOME at a temp dir and returns the resolved home, so the test
// exercises the production path helpers rather than a hardcoded layout.
func newHome(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func writeBlockFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceBlockBody(old, body string) string {
	out, err := managedfile.ReplaceBlock([]byte(old), []byte("<atomic>\n"+body+"\n</atomic>\n"))
	if err != nil {
		panic(err)
	}
	return string(out)
}

func fileMutation(t *testing.T, unit, path, intended string) Mutation {
	t.Helper()
	digest, err := managedfile.DigestResourceBytes([]byte(intended), managedfile.KindBlock)
	if err != nil {
		t.Fatal(err)
	}
	return Mutation{
		Unit:       unit,
		Resource:   unit,
		Target:     "claude:default",
		Kind:       managedfile.KindBlock,
		Path:       path,
		Intended:   digest,
		Generation: "gen-1",
		Tier:       "native",
	}
}

func TestHeaderValidateRefusesIncompatibleSchemas(t *testing.T) {
	if err := NewHeader().Validate(); err != nil {
		t.Fatalf("current header rejected: %v", err)
	}

	newer := NewHeader()
	newer.SchemaVersion = SchemaVersion + 1
	if err := newer.Validate(); err == nil || !strings.Contains(err.Error(), "newer than supported") {
		t.Errorf("newer schema error = %v", err)
	}

	unreadable := NewHeader()
	unreadable.MinimumReader = SchemaVersion + 1
	if err := unreadable.Validate(); err == nil || !strings.Contains(err.Error(), "requires reader schema") {
		t.Errorf("minimum-reader error = %v", err)
	}

	if err := (Header{}).Validate(); err == nil {
		t.Error("unstamped header accepted")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "journal.json")
	for name, body := range map[string]string{
		"newer schema": `{"schema_version":99,"writer_version":"v","minimum_reader_version":1,"operation_id":"op","mutations":[]}`,
		"newer reader": `{"schema_version":1,"writer_version":"v","minimum_reader_version":99,"operation_id":"op","mutations":[]}`,
		"unstamped":    `{"operation_id":"op","mutations":[]}`,
		"undecodable":  `not json`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadJournal(path); err == nil {
			t.Errorf("%s: LoadJournal accepted incompatible state", name)
		}
	}
}

func TestAcquireLockSerializesAndRecordsWriter(t *testing.T) {
	home := newHome(t)
	lock, err := AcquireLock(home, WriterIdentity{OperationID: "op-1", WriterVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	if lock.Path() != config.OperationLockPath(home) {
		t.Errorf("lock path = %s", lock.Path())
	}
	identity, err := ReadLockIdentity(config.OperationLockPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if identity.OperationID != "op-1" || identity.WriterVersion != "test" || identity.PID != os.Getpid() {
		t.Errorf("writer identity = %+v", identity)
	}

	// A second open of the same path must not acquire while the first holds it.
	probe, err := os.OpenFile(config.OperationLockPath(home), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		t.Fatal("advisory lock is not exclusive")
	}

	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	syscall.Flock(int(probe.Fd()), syscall.LOCK_UN)

	if _, err := AcquireLock(home, WriterIdentity{OperationID: "../escape"}); err == nil {
		t.Error("unsafe operation id accepted")
	}
}

func TestLedgerRoundTripAndUpsert(t *testing.T) {
	home := newHome(t)
	path := config.LedgerPath(home)

	ledger, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Rows) != 0 {
		t.Fatalf("missing ledger loaded %d rows", len(ledger.Rows))
	}

	row := Row{
		Target:     "claude:default",
		Resource:   "global-claude",
		Consumer:   "claude:default",
		Generation: "gen-1",
		Tier:       "native",
		Applied:    AppliedValue{Path: "/tmp/CLAUDE.md", Kind: managedfile.KindBlock, Digest: "abc"},
	}
	if !ledger.Upsert(row) {
		t.Error("first upsert reported no change")
	}
	if ledger.Upsert(row) {
		t.Error("identical upsert reported a change")
	}
	row.Generation = "gen-2"
	if !ledger.Upsert(row) {
		t.Error("changed upsert reported no change")
	}
	if len(ledger.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(ledger.Rows))
	}
	if err := ledger.Save(path); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := reloaded.Find("claude:default", "global-claude")
	if !ok || found.Generation != "gen-2" || reloaded.Header.WriterVersion == "" {
		t.Errorf("reloaded row = %+v (ok=%v)", found, ok)
	}

	if err := os.WriteFile(path, []byte(`{"schema_version":99,"minimum_reader_version":1,"rows":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLedger(path); err == nil {
		t.Error("ledger with a newer schema was accepted")
	}
}

func TestLedgerTargetRecordsSurviveRowWrites(t *testing.T) {
	home := newHome(t)
	path := config.LedgerPath(home)

	ledger, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	record := TargetRecord{
		Harness:    "claude",
		Instance:   "/home/u/.claude",
		NativeRoot: "/home/u/.claude",
		Status:     "enrolled",
	}
	if !ledger.UpsertTarget(record) {
		t.Error("first target upsert reported no change")
	}
	if ledger.UpsertTarget(record) {
		t.Error("identical target upsert reported a change")
	}
	record.Status = "stale"
	if !ledger.UpsertTarget(record) {
		t.Error("changed target upsert reported no change")
	}
	if len(ledger.Targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(ledger.Targets))
	}
	if err := ledger.Save(path); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := reloaded.FindTarget("claude", "/home/u/.claude")
	if !ok || found.Status != "stale" || found.NativeRoot != "/home/u/.claude" {
		t.Errorf("reloaded target = %+v (ok=%v)", found, ok)
	}
	if _, ok := reloaded.FindTarget("omp", "/home/u/.omp/agent"); ok {
		t.Error("unregistered target reported as enrolled")
	}

	// A row write must not disturb target enrollment.
	if !reloaded.Upsert(Row{Target: "claude:/home/u/.claude", Resource: "CLAUDE.md"}) {
		t.Error("row upsert reported no change")
	}
	if err := reloaded.Save(path); err != nil {
		t.Fatal(err)
	}
	after, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.FindTarget("claude", "/home/u/.claude"); !ok {
		t.Error("target record lost after a row write")
	}
}

func TestJournalProgressIsAppendOnlyAndOrdered(t *testing.T) {
	j := NewJournal("op-1", time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC))
	if got := j.State("unit-a"); got != StatePlanned {
		t.Errorf("default state = %s, want %s", got, StatePlanned)
	}
	j.Record("unit-a", StateStaged, "d1", time.Now())
	j.Record("unit-a", StateApplied, "d2", time.Now())
	j.Record("unit-a", StateVerified, "d3", time.Now())
	if got := j.State("unit-a"); got != StateVerified {
		t.Errorf("state = %s, want %s", got, StateVerified)
	}
	latest, ok := j.Latest("unit-a")
	if !ok || latest.Digest != "d3" {
		t.Errorf("latest = %+v (ok=%v)", latest, ok)
	}
	if len(j.Progress) != 3 {
		t.Errorf("progress entries = %d, want 3", len(j.Progress))
	}
}

func TestJournalPathsAreOldestFirst(t *testing.T) {
	dir := t.TempDir()
	for _, spec := range []struct {
		id      string
		created time.Time
	}{
		{"second", time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC)},
		{"first", time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)},
	} {
		j := NewJournal(spec.id, spec.created)
		if err := WriteJournal(filepath.Join(dir, spec.id+".json"), j); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := JournalPaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || filepath.Base(paths[0]) != "first.json" || filepath.Base(paths[1]) != "second.json" {
		t.Errorf("journal order = %v", paths)
	}
}

// TestCommitUnitDurabilityOrder proves the write ordering the recovery contract
// depends on: the journal is durable before any native mutation, the backup is
// durable before publication, and the ledger commits only after verification.
func TestCommitUnitDurabilityOrder(t *testing.T) {
	home := newHome(t)
	target := filepath.Join(home, ".claude", "CLAUDE.md")
	writeBlockFile(t, target, blockDoc)

	intended := replaceBlockBody(blockDoc, "new body")
	plan := Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, intended)}}

	tx, err := NewTransaction(home, "op-order", plan)
	if err != nil {
		t.Fatal(err)
	}
	var steps []Step
	tx.OnStep = func(step Step, unit string) { steps = append(steps, step) }
	if err := tx.Begin(); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadJournal(tx.JournalPath); err != nil {
		t.Fatalf("journal not durable at transaction start: %v", err)
	}

	stage, err := tx.StageFile("global-claude", []byte(intended), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if stage == "" {
		t.Fatal("no stage path")
	}

	var journalAtPublish *Journal
	tx.WriteFileFn = func(path string, data []byte, mode os.FileMode) error {
		onDisk, err := LoadJournal(tx.JournalPath)
		if err != nil {
			t.Fatalf("journal unreadable at publication: %v", err)
		}
		journalAtPublish = onDisk
		return managedfile.WriteFileAtomic(path, data, mode)
	}
	if err := tx.PublishFile("global-claude"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Complete(); err != nil {
		t.Fatal(err)
	}

	if journalAtPublish == nil {
		t.Fatal("publication ran without a durable journal")
	}
	m, ok := journalAtPublish.Mutation("global-claude")
	if !ok || m.Stage != stage || m.Backup == "" || m.BackupSum == "" {
		t.Fatalf("journal at publication = %+v", m)
	}
	if journalAtPublish.State("global-claude") != StateStaged {
		t.Errorf("journal state at publication = %s, want %s", journalAtPublish.State("global-claude"), StateStaged)
	}
	if data, err := os.ReadFile(m.Backup); err != nil || string(data) != blockDoc {
		t.Errorf("durable backup = %q (%v)", data, err)
	}

	want := []Step{StepJournaled, StepObserved, StepBackedUp, StepPublished, StepVerified, StepLedgerCommitted, StepJournalCompleted}
	if len(steps) != len(want) {
		t.Fatalf("steps = %v, want %v", steps, want)
	}
	for i := range want {
		if steps[i] != want[i] {
			t.Fatalf("steps = %v, want %v", steps, want)
		}
	}

	if got, _ := os.ReadFile(target); string(got) != intended {
		t.Errorf("native content = %q, want %q", got, intended)
	}
	ledger, err := LoadLedger(tx.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find("claude:default", "global-claude"); !ok {
		t.Error("verified unit was not ledger-committed")
	}
	journal, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.Completed {
		t.Error("journal was not completed")
	}
}

func TestInterruptionBetweenAdjacentCommitBoundaries(t *testing.T) {
	type outcome struct {
		wantDecision     RecoveryDecision
		wantNative       string
		wantLedgerRow    bool
		wantStagedBefore bool
		wantStagingAfter bool
	}

	tests := []struct {
		name             string
		wantInterruptErr bool
		interrupt        func(t *testing.T, tx *Transaction, intended string) error
		want             outcome
	}{
		{
			name: "after journal, before staging",
			interrupt: func(t *testing.T, tx *Transaction, intended string) error {
				return tx.Begin()
			},
			want: outcome{wantDecision: DecisionNoop, wantNative: blockDoc, wantLedgerRow: false, wantStagedBefore: false, wantStagingAfter: false},
		},
		{
			name: "after staging, before publication",
			interrupt: func(t *testing.T, tx *Transaction, intended string) error {
				if err := tx.Begin(); err != nil {
					return err
				}
				_, err := tx.StageFile("global-claude", []byte(intended), 0o644)
				return err
			},
			want: outcome{wantDecision: DecisionDiscardStaging, wantNative: blockDoc, wantLedgerRow: false, wantStagedBefore: true, wantStagingAfter: false},
		},
		{
			name:             "between publication and verification",
			wantInterruptErr: true,
			interrupt: func(t *testing.T, tx *Transaction, intended string) error {
				if err := tx.Begin(); err != nil {
					return err
				}
				if _, err := tx.StageFile("global-claude", []byte(intended), 0o644); err != nil {
					return err
				}
				calls := 0
				real := tx.Observe
				tx.Observe = func(path string, kind managedfile.Kind) (managedfile.Observation, error) {
					calls++
					if calls == 2 {
						return managedfile.Observation{}, errors.New("interrupted before verification")
					}
					return real(path, kind)
				}
				return tx.PublishFile("global-claude")
			},
			want: outcome{wantDecision: DecisionCommitApplied, wantNative: "applied", wantLedgerRow: true, wantStagedBefore: true, wantStagingAfter: true},
		},
		{
			name: "between verification and ledger commit",
			interrupt: func(t *testing.T, tx *Transaction, intended string) error {
				if err := tx.Begin(); err != nil {
					return err
				}
				if _, err := tx.StageFile("global-claude", []byte(intended), 0o644); err != nil {
					return err
				}
				return tx.PublishFile("global-claude")
			},
			want: outcome{wantDecision: DecisionCommitApplied, wantNative: "applied", wantLedgerRow: true, wantStagedBefore: true, wantStagingAfter: true},
		},
		{
			name:             "between ledger commit and journal completion",
			wantInterruptErr: true,
			interrupt: func(t *testing.T, tx *Transaction, intended string) error {
				if err := tx.Begin(); err != nil {
					return err
				}
				if _, err := tx.StageFile("global-claude", []byte(intended), 0o644); err != nil {
					return err
				}
				if err := tx.PublishFile("global-claude"); err != nil {
					return err
				}
				real := tx.WriteJournalFn
				tx.WriteJournalFn = func(path string, j *Journal) error {
					if j.Completed {
						return errors.New("interrupted before journal completion")
					}
					return real(path, j)
				}
				return tx.Complete()
			},
			want: outcome{wantDecision: DecisionCommitApplied, wantNative: "applied", wantLedgerRow: true, wantStagedBefore: true, wantStagingAfter: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := newHome(t)
			target := filepath.Join(home, ".claude", "CLAUDE.md")
			writeBlockFile(t, target, blockDoc)
			intended := replaceBlockBody(blockDoc, "new body")
			selection := filepath.Join(home, ".atomic", "proj-1", "state-location.json")
			if err := os.MkdirAll(filepath.Dir(selection), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(selection, []byte("{}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			plan := Plan{
				Mutations:             []Mutation{fileMutation(t, "global-claude", target, intended)},
				SelectionDependencies: []string{selection},
			}

			tx, err := NewTransaction(home, "op-interrupt", plan)
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.interrupt(t, tx, intended); (err != nil) != tt.wantInterruptErr {
				t.Fatalf("interrupt error = %v, wantInterruptErr = %v", err, tt.wantInterruptErr)
			}

			journal, err := LoadJournal(tx.JournalPath)
			if err != nil {
				t.Fatalf("journal after interruption: %v", err)
			}
			ledger, err := LoadLedger(tx.LedgerPath)
			if err != nil {
				t.Fatal(err)
			}

			stage, err := tx.StagePath("global-claude")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(stage); tt.want.wantStagedBefore && err != nil {
				t.Fatalf("staging missing before recovery: %v", err)
			} else if !tt.want.wantStagedBefore && !os.IsNotExist(err) {
				t.Fatalf("staging unexpectedly present before recovery: %v", err)
			}

			rec := NewRecovery(home, journal, ledger)
			result, err := rec.Recover()
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Conflicts) != 0 {
				t.Fatalf("unexpected conflicts: %+v", result.Conflicts)
			}
			if len(result.Actions) != 1 || result.Actions[0].Decision != tt.want.wantDecision {
				t.Fatalf("actions = %+v, want %s", result.Actions, tt.want.wantDecision)
			}

			if got, _ := os.ReadFile(target); tt.want.wantNative == "applied" {
				if string(got) != intended {
					t.Errorf("native content = %q, want applied bytes", got)
				}
			} else if string(got) != tt.want.wantNative {
				t.Errorf("native content = %q, want %q", got, tt.want.wantNative)
			}

			recovered, err := LoadLedger(tx.LedgerPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := recovered.Find("claude:default", "global-claude"); ok != tt.want.wantLedgerRow {
				t.Errorf("ledger row present = %v, want %v", ok, tt.want.wantLedgerRow)
			}

			if _, err := os.Stat(stage); tt.want.wantStagingAfter && err != nil {
				t.Errorf("staging removed by recovery: %v", err)
			} else if !tt.want.wantStagingAfter && !os.IsNotExist(err) {
				t.Errorf("staging survived recovery: %v", err)
			}

			// Recovery consumes what it reconciled, then the scoped cleanup may
			// drop that operation's operational state — never a verified ledger
			// row or a selection dependency it depends on.
			if err := rec.ConsumeJournal(tx.JournalPath, result); err != nil {
				t.Fatal(err)
			}
			cleanup, err := CleanupOperation(home, "op-interrupt")
			if err != nil {
				t.Fatal(err)
			}
			if len(cleanup.RemovedRows) != 0 {
				t.Errorf("scoped cleanup removed ledger rows: %+v", cleanup.RemovedRows)
			}
			if _, err := os.Stat(tx.JournalPath); !os.IsNotExist(err) {
				t.Errorf("completed journal survived cleanup: %v", err)
			}
			if _, err := os.Stat(config.TransactionDir(home, "op-interrupt")); !os.IsNotExist(err) {
				t.Errorf("consumed transaction directory survived cleanup: %v", err)
			}
			if _, err := os.Stat(selection); err != nil {
				t.Errorf("scoped cleanup removed the referenced selection record: %v", err)
			}
			onDisk, err := LoadLedger(tx.LedgerPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := onDisk.Find("claude:default", "global-claude"); ok != tt.want.wantLedgerRow {
				t.Errorf("scoped cleanup changed the ledger row: present = %v, want %v", ok, tt.want.wantLedgerRow)
			}
		})
	}
}

// TestCleanupOperationRemovesTransactionsBeforeJournal pins the removal order:
// when the transaction directory cannot be removed, the completed journal
// survives so the next cleanup can retry. Removing the journal first would
// strand the directory as an orphan no later cleanup claims.
func TestCleanupOperationRemovesTransactionsBeforeJournal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits are not enforced")
	}
	home := newHome(t)
	target := filepath.Join(home, ".claude", "CLAUDE.md")
	writeBlockFile(t, target, blockDoc)
	intended := replaceBlockBody(blockDoc, "new body")

	tx, err := NewTransaction(home, "op-cleanup-order", Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, intended)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Begin(); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile("global-claude", []byte(intended), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishFile("global-claude"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Complete(); err != nil {
		t.Fatal(err)
	}

	// A non-writable subdirectory holding a file makes os.RemoveAll fail
	// partway, standing in for an interruption between the two removals.
	blocked := filepath.Join(config.TransactionDir(home, "op-cleanup-order"), "blocked")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "inner"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0o500); err != nil {
		t.Fatal(err)
	}

	if _, err := CleanupOperation(home, "op-cleanup-order"); err == nil {
		t.Fatal("cleanup succeeded despite an unremovable transaction directory")
	}
	if _, err := os.Stat(tx.JournalPath); err != nil {
		t.Fatalf("failed cleanup removed the completed journal: %v", err)
	}
	if _, err := os.Stat(config.TransactionDir(home, "op-cleanup-order")); err != nil {
		t.Fatalf("failed cleanup removed the transaction directory: %v", err)
	}

	if err := os.Chmod(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	cleanup, err := CleanupOperation(home, "op-cleanup-order")
	if err != nil {
		t.Fatalf("retry once the transaction directory is removable: %v", err)
	}
	if len(cleanup.RemovedTransactions) != 1 || len(cleanup.RemovedJournals) != 1 {
		t.Fatalf("cleanup result = %+v, want the transaction directory and the journal removed", cleanup)
	}
	if _, err := os.Stat(config.TransactionDir(home, "op-cleanup-order")); !os.IsNotExist(err) {
		t.Errorf("transaction directory survived the retry: %v", err)
	}
	if _, err := os.Stat(tx.JournalPath); !os.IsNotExist(err) {
		t.Errorf("completed journal survived the retry: %v", err)
	}
}

// Full uninstall must not remove a completed journal while its transaction
// directory still exists: if the directory removal then fails, no journal
// remains to match it, and the orphan-preservation rule keeps it forever.
func TestCleanupRemovesCompletedTransactionsBeforeJournals(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits are not enforced")
	}
	home := newHome(t)

	ids := []string{"op-a-fine", "op-z-blocked"}
	txs := make(map[string]*Transaction, len(ids))
	for _, id := range ids {
		target := filepath.Join(home, ".claude", id+".md")
		writeBlockFile(t, target, blockDoc)
		intended := replaceBlockBody(blockDoc, id+" body")
		tx, err := NewTransaction(home, id, Plan{Mutations: []Mutation{fileMutation(t, "res-"+id, target, intended)}})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Begin(); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.StageFile("res-"+id, []byte(intended), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := tx.PublishFile("res-" + id); err != nil {
			t.Fatal(err)
		}
		if err := tx.Complete(); err != nil {
			t.Fatal(err)
		}
		txs[id] = tx
	}

	// A non-writable subdirectory holding a file makes os.RemoveAll fail
	// partway, standing in for a directory removal that cannot complete.
	blocked := filepath.Join(config.TransactionDir(home, "op-z-blocked"), "blocked")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "inner"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0o500); err != nil {
		t.Fatal(err)
	}

	if _, err := Cleanup(home, nil, time.Now().UTC()); err == nil {
		t.Fatal("cleanup succeeded despite an unremovable transaction directory")
	}
	// Every transaction directory that survived the interrupted cleanup still
	// has the journal that owns it, so the next cleanup can match and retry it.
	for _, id := range ids {
		if _, err := os.Stat(txs[id].JournalPath); err != nil {
			t.Errorf("failed cleanup removed the journal for %s: %v", id, err)
		}
	}
	if _, err := os.Stat(config.TransactionDir(home, "op-z-blocked")); err != nil {
		t.Errorf("failed cleanup removed the blocked transaction directory: %v", err)
	}

	if err := os.Chmod(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	cleanup, err := Cleanup(home, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("retry once the transaction directory is removable: %v", err)
	}
	if len(cleanup.RemovedJournals) != len(ids) || len(cleanup.RemovedTransactions) != 1 {
		t.Fatalf("cleanup result = %+v, want both journals and the blocked transaction directory removed", cleanup)
	}
	for _, id := range ids {
		if _, err := os.Stat(config.TransactionDir(home, id)); !os.IsNotExist(err) {
			t.Errorf("transaction directory %s survived the retry: %v", id, err)
		}
		if _, err := os.Stat(txs[id].JournalPath); !os.IsNotExist(err) {
			t.Errorf("completed journal %s survived the retry: %v", id, err)
		}
	}
}

func TestRecoveryNeverOverwritesALaterEdit(t *testing.T) {
	home := newHome(t)
	target := filepath.Join(home, ".claude", "CLAUDE.md")
	writeBlockFile(t, target, blockDoc)
	intended := replaceBlockBody(blockDoc, "new body")

	tx, err := NewTransaction(home, "op-conflict", Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, intended)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Begin(); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile("global-claude", []byte(intended), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishFile("global-claude"); err != nil {
		t.Fatal(err)
	}

	userEdit := replaceBlockBody(blockDoc, "user changed this after atomic wrote")
	writeBlockFile(t, target, userEdit)

	journal, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := LoadLedger(tx.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	rec := NewRecovery(home, journal, ledger)

	forward, err := rec.Recover()
	if err != nil {
		t.Fatal(err)
	}
	if len(forward.Conflicts) != 1 || forward.Conflicts[0].Decision != DecisionConflict {
		t.Fatalf("roll-forward result = %+v, want one conflict", forward)
	}
	back, err := rec.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Conflicts) != 1 {
		t.Fatalf("rollback result = %+v, want one conflict", back)
	}

	if got, _ := os.ReadFile(target); string(got) != userEdit {
		t.Errorf("recovery overwrote the later edit: %q", got)
	}
	if _, ok := ledger.Find("claude:default", "global-claude"); ok {
		t.Error("conflicting unit was ledger-committed")
	}
	if err := rec.ConsumeJournal(tx.JournalPath, forward); err == nil {
		t.Error("journal with conflicts was consumed")
	}
}

func TestTreeRollbackRestoresDisplacedTree(t *testing.T) {
	home := newHome(t)
	dest := filepath.Join(home, ".atomic", "packages", "omp", "atomic")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "old.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tx, intended := newTreeTransaction(t, home, "op-tree", dest)
	if _, err := tx.StageTree("omp-package", renderTree); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishTree("omp-package"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "commands", "help.md")); err != nil || string(got) != "new\n" {
		t.Fatalf("published tree = %q (%v)", got, err)
	}
	_ = intended

	journal, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := journal.Mutation("omp-package")
	if m.Backup == "" {
		t.Fatal("journal did not record the transaction backup before publication")
	}
	if got, err := os.ReadFile(filepath.Join(m.Backup, "old.md")); err != nil || string(got) != "old\n" {
		t.Fatalf("displaced tree backup = %q (%v)", got, err)
	}

	// A crash between the displacement and the staged move leaves the
	// destination absent with the displaced tree still in the transaction backup.
	if err := os.RemoveAll(dest); err != nil {
		t.Fatal(err)
	}

	ledger, err := LoadLedger(tx.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	rec := NewRecovery(home, journal, ledger)
	result, err := rec.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Conflicts) != 0 {
		t.Fatalf("rollback conflicts = %+v", result.Conflicts)
	}
	if len(result.Actions) != 1 || result.Actions[0].Decision != DecisionRestoreBackup {
		t.Fatalf("rollback actions = %+v, want %s", result.Actions, DecisionRestoreBackup)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "old.md")); err != nil || string(got) != "old\n" {
		t.Errorf("rolled-back tree = %q (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "commands", "help.md")); !os.IsNotExist(err) {
		t.Errorf("rollback left applied bytes in place: %v", err)
	}
	if _, err := os.Stat(m.Backup); !os.IsNotExist(err) {
		t.Errorf("backup survived restoration: %v", err)
	}
}

// TestTreeRollbackRefusesTamperedBackup proves the tree restore path verifies
// the recorded backup digest on the backup before it clears or moves the
// destination, exactly as the file path refuses a tampered backup.
func TestTreeRollbackRefusesTamperedBackup(t *testing.T) {
	home := newHome(t)
	dest := filepath.Join(home, ".atomic", "packages", "omp", "atomic")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "old.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tx, _ := newTreeTransaction(t, home, "op-tree-tamper", dest)
	if _, err := tx.StageTree("omp-package", renderTree); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishTree("omp-package"); err != nil {
		t.Fatal(err)
	}

	journal, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := journal.Mutation("omp-package")
	published, err := os.ReadFile(filepath.Join(dest, "commands", "help.md"))
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with the displaced tree: the journal's recorded digest no longer
	// describes what the transaction backup holds.
	if err := os.WriteFile(filepath.Join(m.Backup, "old.md"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ledger, err := LoadLedger(tx.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewRecovery(home, journal, ledger).Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Decision != DecisionConflict {
		t.Fatalf("rollback = %+v, want one conflict for the tampered tree backup", result)
	}
	if !strings.Contains(result.Conflicts[0].Detail, "changed after capture") {
		t.Errorf("conflict detail = %q", result.Conflicts[0].Detail)
	}
	// The destination was never cleared or replaced, and the tampered backup
	// was never moved over it.
	if got, err := os.ReadFile(filepath.Join(dest, "commands", "help.md")); err != nil || string(got) != string(published) {
		t.Errorf("destination after refused rollback = %q (%v)", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(m.Backup, "old.md")); err != nil || string(got) != "tampered\n" {
		t.Errorf("tampered backup was moved: %q (%v)", got, err)
	}
}

func TestTreeRecoveryCommitsAppliedTreeAndSkipsCompleted(t *testing.T) {
	home := newHome(t)
	dest := filepath.Join(home, ".atomic", "packages", "omp", "atomic")

	tx, _ := newTreeTransaction(t, home, "op-tree-forward", dest)
	if _, err := tx.StageTree("omp-package", renderTree); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishTree("omp-package"); err != nil {
		t.Fatal(err)
	}

	journal, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := LoadLedger(tx.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	rec := NewRecovery(home, journal, ledger)
	result, err := rec.Recover()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Conflicts) != 0 || result.Actions[0].Decision != DecisionCommitApplied {
		t.Fatalf("recovery result = %+v", result)
	}
	if _, ok := ledger.Find("omp:default", "omp-package"); !ok {
		t.Error("applied tree was not ledger-committed")
	}
	if got, err := os.ReadFile(filepath.Join(dest, "commands", "help.md")); err != nil || string(got) != "new\n" {
		t.Errorf("published tree = %q (%v)", got, err)
	}

	// A completed generation is never implicitly rolled back.
	if err := rec.ConsumeJournal(tx.JournalPath, result); err != nil {
		t.Fatal(err)
	}
	completed, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	rollback, err := NewRecovery(home, completed, ledger).Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if rollback.Actions[0].Decision != DecisionSkipCommitted {
		t.Errorf("rollback of a completed generation = %+v", rollback.Actions)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "commands", "help.md")); err != nil || string(got) != "new\n" {
		t.Errorf("completed generation was rolled back: %q (%v)", got, err)
	}
}

func newTreeTransaction(t *testing.T, home, operationID, dest string) (*Transaction, string) {
	t.Helper()
	scratch := filepath.Join(t.TempDir(), "atomic")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := renderTree(scratch); err != nil {
		t.Fatal(err)
	}
	intended, _, err := managedfile.TreeDigest(scratch)
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan{Mutations: []Mutation{{
		Unit:       "omp-package",
		Resource:   "omp-package",
		Target:     "omp:default",
		Kind:       managedfile.KindTree,
		Path:       dest,
		Intended:   intended,
		Generation: "gen-1",
		Tier:       "native",
	}}}
	tx, err := NewTransaction(home, operationID, plan)
	if err != nil {
		t.Fatal(err)
	}
	return tx, intended
}

func renderTree(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "commands"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "commands", "help.md"), []byte("new\n"), 0o644)
}

func TestRetentionAndCleanupPreserveUnresolvedOperationalState(t *testing.T) {
	home := newHome(t)

	// One unresolved operation still owning a backup, a selection, and a row.
	unresolvedTarget := filepath.Join(home, ".claude", "CLAUDE.md")
	writeBlockFile(t, unresolvedTarget, blockDoc)
	unresolved := replaceBlockBody(blockDoc, "unresolved body")
	selection := filepath.Join(home, ".atomic", "proj-1", "state-location.json")
	if err := os.MkdirAll(filepath.Dir(selection), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selection, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uTx, err := NewTransaction(home, "op-unresolved", Plan{
		Mutations:             []Mutation{fileMutation(t, "global-claude", unresolvedTarget, unresolved)},
		SelectionDependencies: []string{selection},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := uTx.Begin(); err != nil {
		t.Fatal(err)
	}
	if _, err := uTx.StageFile("global-claude", []byte(unresolved), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := uTx.PublishFile("global-claude"); err != nil {
		t.Fatal(err)
	}
	uJournal, err := LoadJournal(uTx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	uMutation, _ := uJournal.Mutation("global-claude")

	// One completed operation whose operational state is removable.
	completedTarget := filepath.Join(home, ".claude", "other.md")
	writeBlockFile(t, completedTarget, blockDoc)
	completed := replaceBlockBody(blockDoc, "completed body")
	cTx, err := NewTransaction(home, "op-complete", Plan{Mutations: []Mutation{fileMutation(t, "other-resource", completedTarget, completed)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := cTx.Begin(); err != nil {
		t.Fatal(err)
	}
	if _, err := cTx.StageFile("other-resource", []byte(completed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cTx.PublishFile("other-resource"); err != nil {
		t.Fatal(err)
	}
	if err := cTx.Complete(); err != nil {
		t.Fatal(err)
	}

	// An orphaned transaction directory has no journal and is never auto-removed.
	orphan := config.TransactionDir(home, "op-orphan")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}

	ledger, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	referencedRow := Row{Target: "claude:default", Resource: "global-claude", Generation: "gen-0", Applied: AppliedValue{Path: unresolvedTarget, Kind: managedfile.KindBlock, Digest: "previous"}}
	unreferencedRow := Row{Target: "omp:default", Resource: "omp-package", Generation: "gen-0"}
	ledger.Upsert(referencedRow)
	ledger.Upsert(unreferencedRow)
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	retention, err := ComputeRetention(config.JournalsDir(home), ledger)
	if err != nil {
		t.Fatal(err)
	}
	if len(retention.Journals) != 1 || !strings.HasSuffix(retention.Journals[0], "op-unresolved.json") {
		t.Errorf("retained journals = %v", retention.Journals)
	}
	if len(retention.Backups) != 1 || retention.Backups[0] != uMutation.Backup {
		t.Errorf("retained backups = %v, want %s", retention.Backups, uMutation.Backup)
	}
	if len(retention.Selections) != 1 || retention.Selections[0] != selection {
		t.Errorf("retained selections = %v", retention.Selections)
	}
	if len(retention.Rows) != 1 || retention.Rows[0] != referencedRow {
		t.Errorf("retained rows = %+v", retention.Rows)
	}
	if !retention.Keeps(config.TransactionDir(home, "op-unresolved")) {
		t.Error("unresolved transaction directory is not retained")
	}
	if retention.Keeps(config.TransactionDir(home, "op-complete")) {
		t.Error("completed transaction directory is retained")
	}

	result, err := Cleanup(home, ledger, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RemovedJournals) != 1 || !strings.HasSuffix(result.RemovedJournals[0], "op-complete.json") {
		t.Errorf("removed journals = %v", result.RemovedJournals)
	}
	if len(result.RemovedTransactions) != 1 || result.RemovedTransactions[0] != config.TransactionDir(home, "op-complete") {
		t.Errorf("removed transactions = %v", result.RemovedTransactions)
	}
	// The unreferenced row and the completed operation's row are both removable.
	if len(result.RemovedRows) != 2 {
		t.Errorf("removed rows = %+v, want the unreferenced row and the completed operation's row", result.RemovedRows)
	}
	removedCompleted := false
	for _, removed := range result.RemovedRows {
		if removed.Target == referencedRow.Target && removed.Resource == referencedRow.Resource {
			t.Errorf("referenced row was removed: %+v", removed)
		}
		if removed.Resource == "other-resource" {
			removedCompleted = true
		}
	}
	if !removedCompleted {
		t.Errorf("completed operation's row was not removed: %+v", result.RemovedRows)
	}

	if _, err := os.Stat(uTx.JournalPath); err != nil {
		t.Errorf("unresolved journal was removed: %v", err)
	}
	if _, err := os.Stat(uMutation.Backup); err != nil {
		t.Errorf("referenced transaction backup was removed: %v", err)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("orphaned transaction directory was auto-removed: %v", err)
	}
	if _, err := os.Stat(config.TransactionDir(home, "op-complete")); !os.IsNotExist(err) {
		t.Errorf("completed transaction directory survived: %v", err)
	}
	if len(ledger.Rows) != 1 || ledger.Rows[0] != referencedRow {
		t.Errorf("ledger rows after cleanup = %+v", ledger.Rows)
	}
	if _, err := os.Stat(selection); err != nil {
		t.Errorf("full-uninstall cleanup removed the referenced selection record: %v", err)
	}
	onDisk, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(onDisk.Rows) != 1 || onDisk.Rows[0] != referencedRow {
		t.Errorf("ledger on disk after cleanup = %+v, want only the referenced row", onDisk.Rows)
	}
}

func TestCleanupDropsTargetsNothingReferences(t *testing.T) {
	home := newHome(t)

	// One unresolved operation names a target directly, and mutates a second
	// resource with no target at all. The untargeted mutation keeps every
	// ownership row for its resource, so a retained row — not the journal — is
	// what keeps that target enrolled.
	namedTarget := TargetRecord{Harness: "claude", Instance: "default", NativeRoot: filepath.Join(home, ".claude"), Status: "enrolled"}
	rowKeptTarget := TargetRecord{Harness: "claude", Instance: "secondary", NativeRoot: filepath.Join(home, ".claude-alt"), Status: "enrolled"}
	unresolved := NewJournal("op-unresolved", time.Now().UTC())
	unresolved.Mutations = []Mutation{
		{Unit: "named", Resource: "named-resource", Target: namedTarget.Key(), Kind: managedfile.KindBlock, Path: filepath.Join(home, ".claude", "CLAUDE.md"), Intended: "sha256:named"},
		{Unit: "untargeted", Resource: "untargeted-resource", Kind: managedfile.KindBlock, Path: filepath.Join(home, ".claude-alt", "CLAUDE.md"), Intended: "sha256:untargeted"},
	}
	if err := WriteJournal(config.JournalPath(home, "op-unresolved"), unresolved); err != nil {
		t.Fatal(err)
	}

	// A completed operation owns nothing: its journal is consumed state, so its
	// row and target record are removable.
	consumedTarget := TargetRecord{Harness: "omp", Instance: "done", NativeRoot: filepath.Join(home, ".omp", "agent"), Status: "enrolled"}
	completed := NewJournal("op-complete", time.Now().UTC())
	completed.Completed = true
	completed.Mutations = []Mutation{
		{Unit: "done", Resource: "done-resource", Target: consumedTarget.Key(), Kind: managedfile.KindBlock, Path: filepath.Join(home, ".omp", "agent", "AGENTS.md"), Intended: "sha256:done"},
	}
	if err := WriteJournal(config.JournalPath(home, "op-complete"), completed); err != nil {
		t.Fatal(err)
	}

	ledger, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []TargetRecord{namedTarget, rowKeptTarget, consumedTarget} {
		ledger.UpsertTarget(record)
	}
	ledger.Upsert(Row{Target: namedTarget.Key(), Resource: "named-resource"})
	ledger.Upsert(Row{Target: rowKeptTarget.Key(), Resource: "untargeted-resource"})
	ledger.Upsert(Row{Target: consumedTarget.Key(), Resource: "done-resource"})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	retention, err := ComputeRetention(config.JournalsDir(home), ledger)
	if err != nil {
		t.Fatal(err)
	}
	if len(retention.Targets) != 1 || retention.Targets[0] != namedTarget.Key() {
		t.Errorf("retained targets = %v, want only %s", retention.Targets, namedTarget.Key())
	}
	if !retention.KeepsTarget(namedTarget) {
		t.Error("target named by an unresolved journal is not retained")
	}
	if retention.KeepsTarget(rowKeptTarget) {
		t.Error("target kept only by an ownership row reports a journal reference")
	}

	result, err := Cleanup(home, ledger, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RemovedTargets) != 1 || result.RemovedTargets[0] != consumedTarget {
		t.Errorf("removed targets = %+v, want only %+v", result.RemovedTargets, consumedTarget)
	}
	if _, ok := ledger.FindTarget(consumedTarget.Harness, consumedTarget.Instance); ok {
		t.Error("the completed operation's target record survived cleanup")
	}
	for _, record := range []TargetRecord{namedTarget, rowKeptTarget} {
		if _, ok := ledger.FindTarget(record.Harness, record.Instance); !ok {
			t.Errorf("target %s was dropped while still referenced", record.Key())
		}
	}

	onDisk, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(onDisk.Targets) != 2 {
		t.Errorf("targets on disk after cleanup = %+v, want the two referenced targets", onDisk.Targets)
	}
	if _, ok := onDisk.FindTarget(consumedTarget.Harness, consumedTarget.Instance); ok {
		t.Error("the completed operation's target record survived on disk")
	}
	for _, record := range []TargetRecord{namedTarget, rowKeptTarget} {
		if _, ok := onDisk.FindTarget(record.Harness, record.Instance); !ok {
			t.Errorf("target %s was dropped from disk while still referenced", record.Key())
		}
	}
}

// TestCleanupKeepsRowsACallerCouldNotClear proves Cleanup retains a row a
// removal could not clear — a resource still on disk behind a read-only file —
// and the target record that row names, while still pruning everything else.
func TestCleanupKeepsRowsACallerCouldNotClear(t *testing.T) {
	home := newHome(t)

	skippedTarget := TargetRecord{Harness: "claude", Instance: "default", NativeRoot: filepath.Join(home, ".claude"), Status: "converged"}
	clearedTarget := TargetRecord{Harness: "claude", Instance: "read-only", NativeRoot: filepath.Join(home, ".claude-ro"), Status: "converged"}
	ledger, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ledger.UpsertTarget(skippedTarget)
	ledger.UpsertTarget(clearedTarget)
	kept := Row{Target: skippedTarget.Key(), Resource: "settings.json", Applied: AppliedValue{Path: filepath.Join(skippedTarget.NativeRoot, "settings.json"), Kind: managedfile.KindSettings, Digest: "sha256:kept"}}
	cleared := Row{Target: clearedTarget.Key(), Resource: "global-claude"}
	ledger.Upsert(kept)
	ledger.Upsert(cleared)
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	result, err := Cleanup(home, ledger, time.Now().UTC(), kept)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RemovedRows) != 1 || result.RemovedRows[0].Resource != "global-claude" {
		t.Errorf("removed rows = %+v, want only the cleared resource", result.RemovedRows)
	}
	if _, ok := ledger.Find(skippedTarget.Key(), "settings.json"); !ok {
		t.Error("the kept row was pruned")
	}
	if _, ok := ledger.FindTarget(skippedTarget.Harness, skippedTarget.Instance); !ok {
		t.Error("the target a kept row names was dropped")
	}
	if _, ok := ledger.FindTarget(clearedTarget.Harness, clearedTarget.Instance); ok {
		t.Error("a target nothing references survived cleanup")
	}
	onDisk, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(onDisk.Rows) != 1 || onDisk.Rows[0].Resource != "settings.json" {
		t.Errorf("ledger on disk after cleanup = %+v, want only the kept row", onDisk.Rows)
	}
}

func TestEndToEndOperationUnderRealHome(t *testing.T) {
	home := newHome(t)

	lock, err := AcquireLock(home, WriterIdentity{OperationID: "op-e2e", WriterVersion: "test-writer"})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	target := filepath.Join(home, ".claude", "CLAUDE.md")
	writeBlockFile(t, target, blockDoc)
	intended := replaceBlockBody(blockDoc, "e2e body")

	tx, err := NewTransaction(home, "op-e2e", Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, intended)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Begin(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.JournalPath(home, "op-e2e")); err != nil {
		t.Fatalf("journal not durable before mutation: %v", err)
	}
	if _, err := tx.StageFile("global-claude", []byte(intended), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishFile("global-claude"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Complete(); err != nil {
		t.Fatal(err)
	}

	if got, err := os.ReadFile(target); err != nil || string(got) != intended {
		t.Fatalf("native file = %q (%v)", got, err)
	}
	journal, err := LoadJournal(config.JournalPath(home, "op-e2e"))
	if err != nil {
		t.Fatal(err)
	}
	if !journal.Completed {
		t.Error("journal not marked completed")
	}
	ledger, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	row, ok := ledger.Find("claude:default", "global-claude")
	if !ok {
		t.Fatal("ledger row missing")
	}
	wantDigest, err := managedfile.DigestResourceBytes([]byte(intended), managedfile.KindBlock)
	if err != nil {
		t.Fatal(err)
	}
	if row.Applied.Digest != wantDigest || row.Applied.Kind != managedfile.KindBlock || row.Applied.Path != target {
		t.Errorf("applied value = %+v", row.Applied)
	}
	if row.Generation != "gen-1" || row.Tier != "native" {
		t.Errorf("row = %+v", row)
	}
	if got, err := os.ReadFile(filepath.Join(tx.BackupRoot(), "global-claude")); err != nil || string(got) != blockDoc {
		t.Errorf("transaction backup = %q (%v)", got, err)
	}

	retention, err := ComputeRetention(config.JournalsDir(home), ledger)
	if err != nil {
		t.Fatal(err)
	}
	if len(retention.Journals) != 0 || len(retention.Backups) != 0 {
		t.Errorf("completed operation retained state: %+v", retention)
	}

	// Backups under ~/.atomic/backups are user recovery history: full-uninstall
	// cleanup must leave them, and the inert lock path, alone.
	stamp := config.BackupStampDir(home, "20260916-120000")
	if err := os.MkdirAll(stamp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stamp, "CLAUDE.md"), []byte(blockDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Cleanup(home, ledger, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RemovedJournals) != 1 || len(result.RemovedTransactions) != 1 || len(result.RemovedRows) != 1 {
		t.Fatalf("cleanup result = %+v", result)
	}
	if _, err := os.Stat(config.JournalPath(home, "op-e2e")); !os.IsNotExist(err) {
		t.Errorf("completed journal survived cleanup: %v", err)
	}
	if _, err := os.Stat(config.TransactionDir(home, "op-e2e")); !os.IsNotExist(err) {
		t.Errorf("completed transaction directory survived cleanup: %v", err)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Errorf("user backup stamp was removed by cleanup: %v", err)
	}
	if _, err := os.Stat(config.OperationLockPath(home)); err != nil {
		t.Errorf("inert lock path removed: %v", err)
	}
	final, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Rows) != 0 {
		t.Errorf("unreferenced ledger rows survived cleanup: %+v", final.Rows)
	}
}

// TestInterruptionBetweenNativeWriteAndAppliedRecord covers the one window the
// journal cannot see: the native bytes are published but the process died
// before the journal recorded the unit as applied. Recovery must recognize the
// published bytes and commit them — discarding the staging would let cleanup
// delete the transaction's only pre-mutation copy while the resource holds
// Atomic bytes no ledger row accounts for.
func TestInterruptionBetweenNativeWriteAndAppliedRecord(t *testing.T) {
	kill := errors.New("killed between the native write and the applied record")

	t.Run("managed file", func(t *testing.T) {
		home := newHome(t)
		target := filepath.Join(home, ".claude", "CLAUDE.md")
		writeBlockFile(t, target, blockDoc)
		intended := replaceBlockBody(blockDoc, "new body")

		tx, err := NewTransaction(home, "op-window-file", Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, intended)}})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Begin(); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.StageFile("global-claude", []byte(intended), 0o644); err != nil {
			t.Fatal(err)
		}
		tx.WriteFileFn = func(path string, data []byte, mode os.FileMode) error {
			if err := managedfile.WriteFileAtomic(path, data, mode); err != nil {
				return err
			}
			return kill
		}
		if err := tx.PublishFile("global-claude"); !errors.Is(err, kill) {
			t.Fatalf("publish error = %v, want the injected kill", err)
		}

		journal, err := LoadJournal(tx.JournalPath)
		if err != nil {
			t.Fatal(err)
		}
		m, ok := journal.Mutation("global-claude")
		if !ok {
			t.Fatal("journal lost the mutation")
		}
		if state := journal.State("global-claude"); state != StateStaged {
			t.Fatalf("journal state = %s, want %s (the window under test)", state, StateStaged)
		}
		if m.AppliedSum != "" {
			t.Fatalf("applied digest was recorded before the interruption: %s", m.AppliedSum)
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != intended {
			t.Fatalf("native content after the kill = %q (%v), want the published bytes", got, err)
		}

		ledger, err := LoadLedger(tx.LedgerPath)
		if err != nil {
			t.Fatal(err)
		}
		rec := NewRecovery(home, journal, ledger)
		result, err := rec.Recover()
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Conflicts) != 0 {
			t.Fatalf("recovery reported conflicts for published bytes: %+v", result.Conflicts)
		}
		if len(result.Actions) != 1 || result.Actions[0].Decision != DecisionCommitApplied {
			t.Fatalf("recovery actions = %+v, want %s", result.Actions, DecisionCommitApplied)
		}
		if _, ok := ledger.Find("claude:default", "global-claude"); !ok {
			t.Error("published bytes were not ledger-committed")
		}

		if data, err := os.ReadFile(m.Backup); err != nil || string(data) != blockDoc {
			t.Fatalf("transaction backup = %q (%v); recovery discarded the only pre-mutation copy", data, err)
		}
		if _, err := os.Stat(m.Stage); err != nil {
			t.Errorf("recovery discarded the staging of a unit it committed: %v", err)
		}
		if got, _ := os.ReadFile(target); string(got) != intended {
			t.Errorf("native content = %q, want the published bytes", got)
		}

		if err := rec.ConsumeJournal(tx.JournalPath, result); err != nil {
			t.Fatal(err)
		}
		if _, err := CleanupOperation(home, "op-window-file"); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != intended {
			t.Errorf("cleanup disturbed the published bytes: %q (%v)", got, err)
		}
		onDisk, err := LoadLedger(tx.LedgerPath)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := onDisk.Find("claude:default", "global-claude"); !ok {
			t.Error("scoped cleanup dropped the verified ledger row")
		}
	})

	t.Run("generated tree", func(t *testing.T) {
		home := newHome(t)
		dest := filepath.Join(home, ".atomic", "packages", "omp", "atomic")
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dest, "old.md"), []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		tx, intended := newTreeTransaction(t, home, "op-window-tree", dest)
		if _, err := tx.StageTree("omp-package", renderTree); err != nil {
			t.Fatal(err)
		}
		tx.PublishDirFn = func(stageDir, destDir, backupDir string) (managedfile.Publication, error) {
			pub, err := managedfile.PublishDir(stageDir, destDir, backupDir)
			if err != nil {
				return pub, err
			}
			return pub, kill
		}
		if err := tx.PublishTree("omp-package"); !errors.Is(err, kill) {
			t.Fatalf("publish error = %v, want the injected kill", err)
		}

		journal, err := LoadJournal(tx.JournalPath)
		if err != nil {
			t.Fatal(err)
		}
		m, _ := journal.Mutation("omp-package")
		if state := journal.State("omp-package"); state != StateStaged {
			t.Fatalf("journal state = %s, want %s (the window under test)", state, StateStaged)
		}
		if m.Intended != intended || m.AppliedSum != "" {
			t.Fatalf("journal mutation = %+v, want the intended digest and no applied digest", m)
		}
		if m.Backup == "" {
			t.Fatal("journal did not record the transaction backup before the displacement")
		}
		if got, err := os.ReadFile(filepath.Join(dest, "commands", "help.md")); err != nil || string(got) != "new\n" {
			t.Fatalf("published tree = %q (%v)", got, err)
		}
		if _, err := os.Stat(m.Stage); !os.IsNotExist(err) {
			t.Fatalf("the staged tree survived the staged rename: %v", err)
		}

		ledger, err := LoadLedger(tx.LedgerPath)
		if err != nil {
			t.Fatal(err)
		}
		rec := NewRecovery(home, journal, ledger)
		result, err := rec.Recover()
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Conflicts) != 0 {
			t.Fatalf("recovery reported conflicts for a published tree: %+v", result.Conflicts)
		}
		if len(result.Actions) != 1 || result.Actions[0].Decision != DecisionCommitApplied {
			t.Fatalf("recovery actions = %+v, want %s", result.Actions, DecisionCommitApplied)
		}
		if got, err := os.ReadFile(filepath.Join(m.Backup, "old.md")); err != nil || string(got) != "old\n" {
			t.Fatalf("displaced tree = %q (%v); recovery discarded the only pre-mutation copy", got, err)
		}
		if _, ok := ledger.Find("omp:default", "omp-package"); !ok {
			t.Error("published tree was not ledger-committed")
		}
		if got, err := os.ReadFile(filepath.Join(dest, "commands", "help.md")); err != nil || string(got) != "new\n" {
			t.Errorf("native tree = %q (%v), want the published tree", got, err)
		}

		if err := rec.ConsumeJournal(tx.JournalPath, result); err != nil {
			t.Fatal(err)
		}
		if _, err := CleanupOperation(home, "op-window-tree"); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(filepath.Join(dest, "commands", "help.md")); err != nil || string(got) != "new\n" {
			t.Errorf("cleanup disturbed the published tree: %q (%v)", got, err)
		}
		onDisk, err := LoadLedger(tx.LedgerPath)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := onDisk.Find("omp:default", "omp-package"); !ok {
			t.Error("scoped cleanup dropped the verified ledger row")
		}
	})

	t.Run("later edit while a pre-mutation copy exists", func(t *testing.T) {
		home := newHome(t)
		target := filepath.Join(home, ".claude", "CLAUDE.md")
		writeBlockFile(t, target, blockDoc)
		intended := replaceBlockBody(blockDoc, "new body")

		tx, err := NewTransaction(home, "op-later-edit", Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, intended)}})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Begin(); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.StageFile("global-claude", []byte(intended), 0o644); err != nil {
			t.Fatal(err)
		}
		tx.WriteFileFn = func(path string, data []byte, mode os.FileMode) error {
			return errors.New("publication never reached the native write")
		}
		if err := tx.PublishFile("global-claude"); err == nil {
			t.Fatal("publication reported success without writing")
		}

		edited := replaceBlockBody(blockDoc, "user edited after the plan")
		writeBlockFile(t, target, edited)

		journal, err := LoadJournal(tx.JournalPath)
		if err != nil {
			t.Fatal(err)
		}
		m, _ := journal.Mutation("global-claude")
		ledger, err := LoadLedger(tx.LedgerPath)
		if err != nil {
			t.Fatal(err)
		}
		rec := NewRecovery(home, journal, ledger)
		result, err := rec.Recover()
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Conflicts) != 1 || result.Conflicts[0].Decision != DecisionConflict {
			t.Fatalf("recovery = %+v, want one conflict", result)
		}
		if got, _ := os.ReadFile(target); string(got) != edited {
			t.Errorf("recovery overwrote the later edit: %q", got)
		}
		if data, err := os.ReadFile(m.Backup); err != nil || string(data) != blockDoc {
			t.Fatalf("pre-mutation copy = %q (%v); recovery discarded the only one", data, err)
		}
		if err := rec.ConsumeJournal(tx.JournalPath, result); err == nil {
			t.Error("recovery consumed a journal it could not reconcile")
		}

		// A scoped cleanup refuses the unresolved journal outright, and the
		// full-uninstall path keeps its transaction directory, so the only
		// pre-mutation copy survives both.
		if _, err := CleanupOperation(home, "op-later-edit"); err == nil {
			t.Error("scoped cleanup consumed an unresolved journal")
		}
		if _, err := Cleanup(home, ledger, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(m.Backup); err != nil {
			t.Errorf("cleanup removed the transaction holding the only pre-mutation copy: %v", err)
		}
		if _, err := os.Stat(tx.JournalPath); err != nil {
			t.Errorf("cleanup removed the unresolved journal: %v", err)
		}
	})
}

// TestPublishFileSplicesUserBytesAroundTheBlock proves ownership stays scoped
// to the managed block: prose the user changed between plan and apply survives
// publication instead of being replaced by the plan-time copy.
func TestPublishFileSplicesUserBytesAroundTheBlock(t *testing.T) {
	home := newHome(t)
	target := filepath.Join(home, ".claude", "CLAUDE.md")
	writeBlockFile(t, target, blockDoc)
	intended := replaceBlockBody(blockDoc, "new body")

	tx, err := NewTransaction(home, "op-splice", Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, intended)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Begin(); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile("global-claude", []byte(intended), 0o644); err != nil {
		t.Fatal(err)
	}

	block, err := managedfile.ManagedBlock([]byte(blockDoc))
	if err != nil {
		t.Fatal(err)
	}
	userEdited := "user prose changed\n" + string(block) + "trailing prose changed\n"
	writeBlockFile(t, target, userEdited)

	if err := tx.PublishFile("global-claude"); err != nil {
		t.Fatal(err)
	}
	want := "user prose changed\n<atomic>\nnew body\n</atomic>\ntrailing prose changed\n"
	if got, err := os.ReadFile(target); err != nil || string(got) != want {
		t.Fatalf("published content = %q (%v), want the user's prose around the new block", got, err)
	}

	journal, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := journal.Mutation("global-claude")
	if m.AppliedSum != managedfile.Digest([]byte(want)) {
		t.Errorf("applied digest = %s, want the published whole file's %s", m.AppliedSum, managedfile.Digest([]byte(want)))
	}
	if data, err := os.ReadFile(m.Backup); err != nil || string(data) != userEdited {
		t.Errorf("transaction backup = %q (%v), want the immediately preceding native bytes", data, err)
	}

	if err := tx.Complete(); err != nil {
		t.Fatal(err)
	}
	completed, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !completed.Completed {
		t.Error("journal was not completed after a verified publication")
	}
	ledger, err := LoadLedger(tx.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if row, ok := ledger.Find("claude:default", "global-claude"); !ok || row.Applied.Digest != m.Intended {
		t.Errorf("ledger row = %+v (ok=%v), want ownership of the managed block only", row, ok)
	}
}

// TestPublishFileFailsLoudOnMalformedBlock proves a resource whose block became
// unparseable between plan and apply is never clobbered by the plan-time bytes.
func TestPublishFileFailsLoudOnMalformedBlock(t *testing.T) {
	home := newHome(t)
	target := filepath.Join(home, ".claude", "CLAUDE.md")
	writeBlockFile(t, target, blockDoc)
	intended := replaceBlockBody(blockDoc, "new body")

	tx, err := NewTransaction(home, "op-malformed", Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, intended)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Begin(); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile("global-claude", []byte(intended), 0o644); err != nil {
		t.Fatal(err)
	}

	malformed := "<atomic>open only\n"
	writeBlockFile(t, target, malformed)

	err = tx.PublishFile("global-claude")
	if err == nil || !strings.Contains(err.Error(), "lost its parseable") {
		t.Fatalf("publish error = %v, want a loud malformed-block failure", err)
	}
	if got, _ := os.ReadFile(target); string(got) != malformed {
		t.Errorf("malformed file was clobbered: %q", got)
	}

	journal, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	if state := journal.State("global-claude"); state != StateStaged {
		t.Fatalf("journal state = %s, want %s", state, StateStaged)
	}
	ledger, err := LoadLedger(tx.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewRecovery(home, journal, ledger).Recover()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Conflicts) != 0 {
		t.Fatalf("conflicts = %+v, want none for a file that holds no Atomic bytes", result.Conflicts)
	}
	if len(result.Actions) != 1 || result.Actions[0].Decision != DecisionDiscardStaging {
		t.Fatalf("recovery actions = %+v, want %s", result.Actions, DecisionDiscardStaging)
	}
	if got, _ := os.ReadFile(target); string(got) != malformed {
		t.Errorf("recovery touched the malformed file: %q", got)
	}
}

// TestRollbackNeverOverwritesOutsideBlockEditsOrResurrectsDeletions pins the
// rollback predicate to the whole resource.
func TestRollbackNeverOverwritesOutsideBlockEditsOrResurrectsDeletions(t *testing.T) {
	t.Run("unchanged block file restores", func(t *testing.T) {
		home := newHome(t)
		target := filepath.Join(home, ".claude", "CLAUDE.md")
		writeBlockFile(t, target, blockDoc)
		intended := replaceBlockBody(blockDoc, "new body")

		tx, err := NewTransaction(home, "op-rollback-file", Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, intended)}})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Begin(); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.StageFile("global-claude", []byte(intended), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := tx.PublishFile("global-claude"); err != nil {
			t.Fatal(err)
		}

		journal, err := LoadJournal(tx.JournalPath)
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := LoadLedger(tx.LedgerPath)
		if err != nil {
			t.Fatal(err)
		}
		result, err := NewRecovery(home, journal, ledger).Rollback()
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Conflicts) != 0 || result.Actions[0].Decision != DecisionRestoreBackup {
			t.Fatalf("rollback = %+v, want a restore", result)
		}
		if got, _ := os.ReadFile(target); string(got) != blockDoc {
			t.Errorf("restored content = %q, want %q", got, blockDoc)
		}
	})

	t.Run("outside-block edit", func(t *testing.T) {
		home := newHome(t)
		target := filepath.Join(home, ".claude", "CLAUDE.md")
		writeBlockFile(t, target, blockDoc)
		intended := replaceBlockBody(blockDoc, "new body")

		tx, err := NewTransaction(home, "op-outside", Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, intended)}})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Begin(); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.StageFile("global-claude", []byte(intended), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := tx.PublishFile("global-claude"); err != nil {
			t.Fatal(err)
		}

		published, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		edited := strings.Replace(string(published), "trailing prose", "trailing prose edited", 1)
		writeBlockFile(t, target, edited)

		obs, err := managedfile.Observe(target, managedfile.KindBlock)
		if err != nil {
			t.Fatal(err)
		}
		publishedDigest, err := managedfile.DigestResourceBytes([]byte(intended), managedfile.KindBlock)
		if err != nil {
			t.Fatal(err)
		}
		if obs.Digest != publishedDigest {
			t.Fatalf("the edit changed the managed block: %s want %s", obs.Digest, publishedDigest)
		}

		journal, err := LoadJournal(tx.JournalPath)
		if err != nil {
			t.Fatal(err)
		}
		m, _ := journal.Mutation("global-claude")
		ledger, err := LoadLedger(tx.LedgerPath)
		if err != nil {
			t.Fatal(err)
		}
		rec := NewRecovery(home, journal, ledger)

		back, err := rec.Rollback()
		if err != nil {
			t.Fatal(err)
		}
		if len(back.Conflicts) != 1 || back.Conflicts[0].Decision != DecisionConflict {
			t.Fatalf("rollback = %+v, want one conflict", back)
		}
		if _, err := os.Stat(m.Backup); err != nil {
			t.Errorf("conflicting rollback removed the pre-mutation copy: %v", err)
		}
		if got, _ := os.ReadFile(target); string(got) != edited {
			t.Errorf("rollback overwrote the user's prose: %q", got)
		}

		// The block is exactly what Atomic published, so roll-forward commits
		// ownership of the block and leaves the user's prose alone.
		forward, err := rec.Recover()
		if err != nil {
			t.Fatal(err)
		}
		if len(forward.Conflicts) != 0 || forward.Actions[0].Decision != DecisionCommitApplied {
			t.Fatalf("forward recovery = %+v, want commit-applied", forward)
		}
		if got, _ := os.ReadFile(target); string(got) != edited {
			t.Errorf("forward recovery overwrote the user's prose: %q", got)
		}
	})

	t.Run("deleted whole-file resource", func(t *testing.T) {
		home := newHome(t)
		target := filepath.Join(home, ".atomic", "harness", "settings.json")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("old\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		tx, err := NewTransaction(home, "op-deleted", Plan{Mutations: []Mutation{{
			Unit:       "harness-settings",
			Resource:   "harness-settings",
			Target:     "omp:default",
			Kind:       managedfile.KindFile,
			Path:       target,
			Intended:   managedfile.Digest([]byte("new\n")),
			Generation: "gen-1",
			Tier:       "native",
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Begin(); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.StageFile("harness-settings", []byte("new\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := tx.PublishFile("harness-settings"); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}

		journal, err := LoadJournal(tx.JournalPath)
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := LoadLedger(tx.LedgerPath)
		if err != nil {
			t.Fatal(err)
		}
		rec := NewRecovery(home, journal, ledger)

		back, err := rec.Rollback()
		if err != nil {
			t.Fatal(err)
		}
		if len(back.Conflicts) != 1 || back.Conflicts[0].Decision != DecisionConflict {
			t.Fatalf("rollback = %+v, want one conflict for the user's deletion", back)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("rollback resurrected a deleted resource: %v", err)
		}

		forward, err := rec.Recover()
		if err != nil {
			t.Fatal(err)
		}
		if len(forward.Conflicts) != 1 || forward.Conflicts[0].Decision != DecisionConflict {
			t.Fatalf("forward recovery = %+v, want one conflict for the user's deletion", forward)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("forward recovery resurrected a deleted resource: %v", err)
		}
	})
}

// interruptTreePublication drives a generated-tree replacement up to the crash
// window PublishDir always leaves when it dies between the two renames: the
// current tree is displaced into the transaction backup and the staged tree has
// not yet moved into place. It returns the loaded journal, ledger, and the
// backup path.
func interruptTreePublication(t *testing.T, home, dest string) (*Journal, *Ledger, string) {
	t.Helper()
	tx, _ := newTreeTransaction(t, home, "op-tree-window", dest)
	if _, err := tx.StageTree("omp-package", renderTree); err != nil {
		t.Fatal(err)
	}
	kill := errors.New("killed between the two renames")
	tx.PublishDirFn = func(stageDir, destDir, backupDir string) (managedfile.Publication, error) {
		backup := filepath.Join(backupDir, filepath.Base(destDir))
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return managedfile.Publication{}, err
		}
		if err := os.Rename(destDir, backup); err != nil {
			return managedfile.Publication{}, err
		}
		return managedfile.Publication{}, kill
	}
	if err := tx.PublishTree("omp-package"); !errors.Is(err, kill) {
		t.Fatalf("publish error = %v, want the injected kill", err)
	}
	journal, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	if state := journal.State("omp-package"); state != StateStaged {
		t.Fatalf("journal state = %s, want %s (the window under test)", state, StateStaged)
	}
	m, _ := journal.Mutation("omp-package")
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("destination survived the crash window: %v", err)
	}
	ledger, err := LoadLedger(tx.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	return journal, ledger, m.Backup
}

// TestRecoveryFinishesInterruptedTreePublication proves the crash window where
// the staged tree is still intact is reconciled by finishing the rename: the
// published tree lands, the ledger records it, and the displaced tree stays in
// the transaction backup.
func TestRecoveryFinishesInterruptedTreePublication(t *testing.T) {
	home := newHome(t)
	dest := filepath.Join(home, ".atomic", "packages", "omp", "atomic")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "old.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	journal, ledger, backup := interruptTreePublication(t, home, dest)
	rec := NewRecovery(home, journal, ledger)
	result, err := rec.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(result.Conflicts) != 0 {
		t.Fatalf("recovery reported conflicts for a still-staged tree: %+v", result.Conflicts)
	}
	if len(result.Actions) != 1 || result.Actions[0].Decision != DecisionCompletePublication {
		t.Fatalf("recovery actions = %+v, want %s", result.Actions, DecisionCompletePublication)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "commands", "help.md")); err != nil || string(got) != "new\n" {
		t.Fatalf("published tree = %q (%v), want the staged tree finished into place", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(backup, "old.md")); err != nil || string(got) != "old\n" {
		t.Fatalf("displaced tree = %q (%v); recovery discarded the only pre-mutation copy", got, err)
	}
	if _, ok := ledger.Find("omp:default", "omp-package"); !ok {
		t.Error("finished publication was not ledger-committed")
	}
}

// TestRecoveryDoesNotResurrectLedgerRecordedTreeDeletion proves the other case
// an absent generated-tree destination can mean: the publication already
// committed — the ledger records its intended bytes — so the destination was
// deleted afterwards. Recovery must not finish the rename and recreate a tree
// the user removed.
func TestRecoveryDoesNotResurrectLedgerRecordedTreeDeletion(t *testing.T) {
	home := newHome(t)
	dest := filepath.Join(home, ".atomic", "packages", "omp", "atomic")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "old.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	journal, ledger, _ := interruptTreePublication(t, home, dest)
	m, _ := journal.Mutation("omp-package")
	// The publication committed, then the user deleted the tree.
	ledger.Upsert(Row{
		Target:     m.Target,
		Resource:   m.Resource,
		Generation: m.Generation,
		Applied:    AppliedValue{Path: dest, Kind: managedfile.KindTree, Digest: m.Intended},
	})

	rec := NewRecovery(home, journal, ledger)
	result, err := rec.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Decision != DecisionConflict {
		t.Fatalf("recovery = %+v, want one conflict for the user's deletion", result)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("recovery resurrected a tree the user deleted: %v", err)
	}
}

// TestRecoveryRestoresInterruptedTreeBackup proves the other half of the crash
// window: when the staged tree is gone, the digest-verified backup is restored
// instead of reporting a conflict.
func TestRecoveryRestoresInterruptedTreeBackup(t *testing.T) {
	home := newHome(t)
	dest := filepath.Join(home, ".atomic", "packages", "omp", "atomic")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "old.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	journal, ledger, backup := interruptTreePublication(t, home, dest)
	m, _ := journal.Mutation("omp-package")
	if err := os.RemoveAll(m.Stage); err != nil {
		t.Fatal(err)
	}

	rec := NewRecovery(home, journal, ledger)
	result, err := rec.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(result.Conflicts) != 0 {
		t.Fatalf("recovery reported conflicts for a digest-verified backup: %+v", result.Conflicts)
	}
	if len(result.Actions) != 1 || result.Actions[0].Decision != DecisionRestoreBackup {
		t.Fatalf("recovery actions = %+v, want %s", result.Actions, DecisionRestoreBackup)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "old.md")); err != nil || string(got) != "old\n" {
		t.Fatalf("restored tree = %q (%v), want the displaced tree back", got, err)
	}
	if _, ok := ledger.Find("omp:default", "omp-package"); ok {
		t.Error("a restored (undone) publication was ledger-committed")
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Errorf("restore left the backup in place: %v", err)
	}
}

// TestRecoveryTreatsNeverWrittenBlockAsUnapplied proves a block unit whose file
// still carries no Atomic tags never landed: the journaled write is discarded
// rather than reported as a conflict, even though the user's prose changed.
func TestRecoveryTreatsNeverWrittenBlockAsUnapplied(t *testing.T) {
	home := newHome(t)
	target := filepath.Join(home, ".claude", "CLAUDE.md")
	writeBlockFile(t, target, "user prose with no atomic block\n")

	tx, err := NewTransaction(home, "op-never-written", Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, blockDoc)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile("global-claude", []byte(blockDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	// beginPublish journals the pre-mutation observation without writing.
	if _, _, err := tx.beginPublish("global-claude"); err != nil {
		t.Fatal(err)
	}
	// The user edits their own prose between the crash and recovery.
	writeBlockFile(t, target, "user edited their prose, still no block\n")

	journal, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := LoadLedger(tx.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	rec := NewRecovery(home, journal, ledger)
	result, err := rec.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(result.Conflicts) != 0 {
		t.Fatalf("a never-written block reported a conflict: %+v", result.Conflicts)
	}
	if len(result.Actions) != 1 || result.Actions[0].Decision != DecisionDiscardStaging {
		t.Fatalf("recovery actions = %+v, want %s", result.Actions, DecisionDiscardStaging)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "user edited their prose, still no block\n" {
		t.Errorf("recovery touched the user's prose: %q (%v)", got, err)
	}
}

// TestRollbackJournalsRestoresAppliedBackup proves the rollback entry point is
// reachable and preserves user bytes: an applied-but-unverified publication is
// undone from its digest-verified backup, and the journal is consumed.
func TestRollbackJournalsRestoresAppliedBackup(t *testing.T) {
	home := newHome(t)
	target := filepath.Join(home, ".claude", "commands", "commit.md")
	writeBlockFile(t, target, "original bytes\n")

	staged := "user prose\n<atomic>\nnew body\n</atomic>\n"
	tx, err := NewTransaction(home, "op-rollback", Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, staged)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile("global-claude", []byte(staged), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishFile("global-claude"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	applied, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !managedfile.HasBlock([]byte(applied)) {
		t.Fatalf("native content = %q, want the published block", applied)
	}

	actions, err := RollbackJournals(home)
	if err != nil {
		t.Fatalf("RollbackJournals: %v", err)
	}
	if !hasDecisionIn(actions, DecisionRestoreBackup) {
		t.Fatalf("actions = %+v, want a restore", actions)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "original bytes\n" {
		t.Errorf("rollback did not restore the user's bytes: %q (%v)", got, err)
	}
	if _, err := os.Stat(config.JournalPath(home, "op-rollback")); !os.IsNotExist(err) {
		t.Errorf("rollback left the consumed journal and its transaction tree behind (stat err = %v)", err)
	}
}

// crashedTreeTransaction drives a real generated-tree publication into the crash
// window between PublishDir's two renames: the tree already at the destination is
// displaced into the transaction backup, and the process dies before the staged
// tree is moved in. corruptStage changes the staged tree afterwards, which is the
// shape whose only resolution is the digest-verified backup. It returns the
// transaction and the mutation as the journal recorded it.
func crashedTreeTransaction(t *testing.T, home, operationID, dest string, corruptStage bool) (*Transaction, Mutation) {
	t.Helper()
	scratch := filepath.Join(t.TempDir(), "atomic")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := renderTree(scratch); err != nil {
		t.Fatal(err)
	}
	intended, _, err := managedfile.TreeDigest(scratch)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "old.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tx, err := NewTransaction(home, operationID, Plan{Mutations: []Mutation{{
		Unit:       "omp-package",
		Resource:   "omp-package",
		Target:     "omp:default",
		Kind:       managedfile.KindTree,
		Path:       dest,
		Intended:   intended,
		Generation: "gen-1",
		Tier:       "native",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	stage, err := tx.StageTree("omp-package", renderTree)
	if err != nil {
		t.Fatal(err)
	}
	if corruptStage {
		if err := os.WriteFile(filepath.Join(stage, "commands", "help.md"), []byte("changed under the crash\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tx.PublishDirFn = func(_, destDir, backupDir string) (managedfile.Publication, error) {
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return managedfile.Publication{}, err
		}
		if err := os.Rename(destDir, filepath.Join(backupDir, filepath.Base(destDir))); err != nil {
			return managedfile.Publication{}, err
		}
		return managedfile.Publication{}, errors.New("simulated crash between the two renames")
	}
	if err := tx.PublishTree("omp-package"); err == nil {
		t.Fatal("the crash stub published the tree")
	}
	journal, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	mutation, ok := journal.Mutation("omp-package")
	if !ok {
		t.Fatal("the crashed publication left no mutation")
	}
	return tx, mutation
}

// homeFingerprint digests every path under home, so a read-only assertion
// compares the whole tree and not only the files a test happened to name.
func homeFingerprint(t *testing.T, home string) string {
	t.Helper()
	digest, _, err := managedfile.TreeDigest(home)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// TestSimulateRecoveriesNeutralizesTreeWriteSeams proves the in-memory preview
// resolves an interrupted generated-tree publication without performing it: the
// complete-publication and restore-backup decisions are both reported, while the
// destination, the staged tree, and the transaction backup keep their
// pre-preview bytes.
func TestSimulateRecoveriesNeutralizesTreeWriteSeams(t *testing.T) {
	home := newHome(t)
	publishDest := config.PackageRoot(home, "omp")
	publishTx, publishMutation := crashedTreeTransaction(t, home, "op-tree-publish", publishDest, false)
	restoreDest := config.PackageRoot(home, "cards")
	restoreTx, restoreMutation := crashedTreeTransaction(t, home, "op-tree-restore", restoreDest, true)

	before := homeFingerprint(t, home)
	sims, _, blocked, err := SimulateRecoveries(home)
	if err != nil {
		t.Fatalf("SimulateRecoveries: %v", err)
	}
	if blocked {
		t.Fatalf("an interrupted tree publication reported a conflict: %+v", sims)
	}

	for _, tc := range []struct {
		name   string
		path   string
		want   RecoveryDecision
		stage  string
		backup string
		dest   string
	}{
		{"publish", publishTx.JournalPath, DecisionCompletePublication, publishMutation.Stage, publishMutation.Backup, publishDest},
		{"restore", restoreTx.JournalPath, DecisionRestoreBackup, restoreMutation.Stage, restoreMutation.Backup, restoreDest},
	} {
		var actions []RecoveryAction
		for _, sim := range sims {
			if sim.Journal == tc.path {
				actions = sim.Actions
			}
		}
		if !hasDecisionIn(actions, tc.want) {
			t.Errorf("%s journal actions = %+v, want %s", tc.name, actions, tc.want)
		}
		if _, err := os.Stat(tc.dest); !os.IsNotExist(err) {
			t.Errorf("%s: the preview performed the tree publication (stat %s = %v)", tc.name, tc.dest, err)
		}
		if _, err := os.Stat(tc.stage); err != nil {
			t.Errorf("%s: the preview consumed the staged tree: %v", tc.name, err)
		}
		if _, err := os.Stat(tc.backup); err != nil {
			t.Errorf("%s: the preview consumed the transaction backup: %v", tc.name, err)
		}
	}
	if after := homeFingerprint(t, home); after != before {
		t.Errorf("the preview changed the filesystem:\nbefore %s\nafter  %s", before, after)
	}
}

// TestSimulateRollbacksPreviewsRestoreWithoutWriting proves the rollback preview
// reports the restore a real --rollback run would make while leaving the native
// bytes, the transaction backup, and the journal untouched.
func TestSimulateRollbacksPreviewsRestoreWithoutWriting(t *testing.T) {
	home := newHome(t)
	target := filepath.Join(home, ".claude", "commands", "commit.md")
	writeBlockFile(t, target, "original bytes\n")
	staged := "user prose\n<atomic>\nnew body\n</atomic>\n"
	tx, err := NewTransaction(home, "op-preview-rollback", Plan{Mutations: []Mutation{fileMutation(t, "global-claude", target, staged)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile("global-claude", []byte(staged), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishFile("global-claude"); err != nil {
		t.Fatal(err)
	}
	applied, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	before := homeFingerprint(t, home)

	sims, blocked, err := SimulateRollbacks(home)
	if err != nil {
		t.Fatalf("SimulateRollbacks: %v", err)
	}
	if blocked {
		t.Fatalf("a restorable journal reported a conflict: %+v", sims)
	}
	if len(sims) != 1 || !hasDecisionIn(sims[0].Actions, DecisionRestoreBackup) {
		t.Fatalf("preview = %+v, want one restore decision", sims)
	}
	if after, err := os.ReadFile(target); err != nil || string(after) != string(applied) {
		t.Errorf("the rollback preview changed the native bytes: %q (%v)", after, err)
	}
	if after := homeFingerprint(t, home); after != before {
		t.Errorf("the rollback preview changed the filesystem:\nbefore %s\nafter  %s", before, after)
	}
	journal, err := LoadJournal(tx.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	if journal.Completed {
		t.Error("the rollback preview consumed the journal")
	}
}
