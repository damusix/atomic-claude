package installstate

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Step names a durability boundary in the commit-unit sequence. Tests assert
// ordering against it; production ignores it.
type Step string

const (
	// StepJournaled is the fsynced journal write that precedes any mutation.
	StepJournaled Step = "journaled"
	// StepObserved is a current-byte observation of the resource.
	StepObserved Step = "observed"
	// StepBackedUp is a durable pre-mutation backup, or the journaled intent to
	// displace the current tree into a transaction backup.
	StepBackedUp Step = "backed-up"
	// StepPublished is the native mutation.
	StepPublished Step = "published"
	// StepVerified is effective content confirmed against the intended digest.
	StepVerified Step = "verified"
	// StepLedgerCommitted is the durable ledger commit for verified units.
	StepLedgerCommitted Step = "ledger-committed"
	// StepJournalCompleted is the terminal journal write.
	StepJournalCompleted Step = "journal-completed"
)

// Plan is one operation's complete intent: the observations it starts from and
// every native mutation it intends, in commit order.
type Plan struct {
	Observations          []managedfile.Observation
	Mutations             []Mutation
	SelectionDependencies []string
}

// Transaction drives one operation's durable ordering: journal and backups
// before mutation, verification before ledger commit. Every external effect is
// a function field so a test can inject stubs and record call order.
type Transaction struct {
	Home string
	ID   string

	JournalPath string
	LedgerPath  string
	Journal     *Journal
	Ledger      *Ledger
	Now         func() time.Time

	Observe        func(path string, kind managedfile.Kind) (managedfile.Observation, error)
	WriteJournalFn func(path string, j *Journal) error
	WriteLedgerFn  func(path string, l *Ledger) error
	BackupFn       func(src, dest, operationID string, now time.Time) (managedfile.BackupRecord, error)
	WriteFileFn    func(path string, data []byte, mode os.FileMode) error
	PublishDirFn   func(stageDir, destDir, backupDir string) (managedfile.Publication, error)
	OnStep         func(step Step, unit string)

	wrote bool
}

// NewTransaction prepares an operation: it loads the ledger and records the
// plan. Begin, or any mutation method, writes the journal durably before
// touching native state. An empty plan writes no journal and creates no
// transaction directories — a converged operation has nothing to recover.
func NewTransaction(home, operationID string, plan Plan) (*Transaction, error) {
	if err := config.ValidateSegment("operation id", operationID); err != nil {
		return nil, err
	}

	ledger, err := LoadLedger(config.LedgerPath(home))
	if err != nil {
		return nil, err
	}

	t := &Transaction{
		Home:           home,
		ID:             operationID,
		JournalPath:    config.JournalPath(home, operationID),
		LedgerPath:     config.LedgerPath(home),
		Ledger:         ledger,
		Now:            func() time.Time { return time.Now().UTC() },
		Observe:        managedfile.Observe,
		WriteJournalFn: WriteJournal,
		WriteLedgerFn:  func(path string, l *Ledger) error { return l.Save(path) },
		BackupFn:       managedfile.BackupFile,
		WriteFileFn:    managedfile.WriteFileAtomic,
		PublishDirFn:   managedfile.PublishDir,
	}
	t.Journal = NewJournal(operationID, t.Now())
	t.Journal.Observations = plan.Observations
	t.Journal.Mutations = plan.Mutations
	t.Journal.SelectionDependencies = plan.SelectionDependencies

	if len(plan.Mutations) == 0 {
		return t, nil
	}
	for _, dir := range []string{t.StageRoot(), t.BackupRoot()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("installstate: mkdir %s: %w", dir, err)
		}
	}
	return t, nil
}

// Begin writes and fsyncs the journal that precedes every mutation. Calling it
// is optional — the first mutation does the same — but it is what makes the
// journal-before-mutation ordering explicit.
func (t *Transaction) Begin() error {
	return t.writeJournal()
}

// StageRoot is the operation's staging directory.
func (t *Transaction) StageRoot() string {
	return config.TransactionStageDir(t.Home, t.ID)
}

// BackupRoot is the operation's transaction backup directory.
func (t *Transaction) BackupRoot() string {
	return config.TransactionBackupDir(t.Home, t.ID)
}

// StagePath is where one commit unit's projection is staged.
func (t *Transaction) StagePath(unit string) (string, error) {
	if err := config.ValidateSegment("commit unit", unit); err != nil {
		return "", err
	}
	return filepath.Join(t.StageRoot(), unit), nil
}

// StageFile writes a file mutation's intended bytes into the operation's
// staging area, proves they match the intended digest, and records the unit as
// staged.
func (t *Transaction) StageFile(unit string, data []byte, mode os.FileMode) (string, error) {
	m, ok := t.Journal.Mutation(unit)
	if !ok {
		return "", fmt.Errorf("installstate: no mutation %q in journal", unit)
	}
	stage, err := t.StagePath(unit)
	if err != nil {
		return "", err
	}
	staged, err := managedfile.DigestResourceBytes(data, m.Kind)
	if err != nil {
		return "", err
	}
	if m.Intended == "" || staged != m.Intended {
		return "", fmt.Errorf("installstate: staged bytes for %s digest %s do not match intended %s", unit, staged, m.Intended)
	}
	if err := os.MkdirAll(filepath.Dir(stage), 0o755); err != nil {
		return "", fmt.Errorf("installstate: mkdir %s: %w", filepath.Dir(stage), err)
	}
	if err := t.WriteFileFn(stage, data, mode); err != nil {
		return "", err
	}
	return stage, t.recordStaged(m, stage)
}

// StageTree renders a generated directory into the operation's staging area,
// proves the tree matches the intended digest, and records the unit as staged.
func (t *Transaction) StageTree(unit string, write func(dir string) error) (string, error) {
	m, ok := t.Journal.Mutation(unit)
	if !ok {
		return "", fmt.Errorf("installstate: no mutation %q in journal", unit)
	}
	stage, err := t.StagePath(unit)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return "", fmt.Errorf("installstate: mkdir %s: %w", stage, err)
	}
	if err := write(stage); err != nil {
		return "", err
	}
	digest, _, err := managedfile.TreeDigest(stage)
	if err != nil {
		return "", err
	}
	if m.Intended == "" || digest != m.Intended {
		return "", fmt.Errorf("installstate: staged tree for %s digest %s does not match intended %s", unit, digest, m.Intended)
	}
	return stage, t.recordStaged(m, stage)
}

func (t *Transaction) recordStaged(m Mutation, stage string) error {
	m.Stage = stage
	t.Journal.UpdateMutation(m)
	t.Journal.Record(m.Unit, StateStaged, m.Intended, t.Now())
	return t.writeJournal()
}

// PublishFile applies one staged file mutation: it observes current bytes,
// captures a durable backup when the resource exists, publishes the bytes the
// resource should hold, and verifies effective content. A block resource is
// spliced into the bytes observed immediately before publication, so
// user-owned prose around the block survives between plan and apply. The ledger
// commit waits for Complete, so a verified-but-unrecorded unit stays
// recoverable.
func (t *Transaction) PublishFile(unit string) error {
	m, obs, err := t.beginPublish(unit)
	if err != nil {
		return err
	}
	if m.Kind == managedfile.KindTree {
		return fmt.Errorf("installstate: %s is a generated tree; publish it with PublishTree", unit)
	}
	staged, err := os.ReadFile(m.Stage)
	if err != nil {
		return fmt.Errorf("installstate: read staged %s: %w", m.Stage, err)
	}
	data, err := filePublishBytes(m, obs, staged)
	if err != nil {
		return err
	}

	mode := os.FileMode(0o644)
	switch {
	case obs.Mode != 0:
		mode = os.FileMode(obs.Mode)
	default:
		// A resource with no native predecessor keeps the mode it was staged
		// with, so a projection never silently loses its permission bits.
		if fi, statErr := os.Stat(m.Stage); statErr == nil {
			mode = fi.Mode().Perm()
		}
	}

	if err := t.WriteFileFn(m.Path, data, mode); err != nil {
		return fmt.Errorf("installstate: publish %s: %w", unit, err)
	}
	m.AppliedSum = managedfile.Digest(data)
	t.Journal.UpdateMutation(m)
	t.Journal.Record(unit, StateApplied, m.Intended, t.Now())
	if err := t.writeJournal(); err != nil {
		return err
	}
	t.notify(StepPublished, unit)

	return t.verify(m)
}

// filePublishBytes derives the bytes a file mutation should publish. A block
// resource is spliced into the freshly observed bytes so only the managed
// block changes; a whole-file resource, or a resource that does not exist yet,
// publishes the staged bytes as-is. A block resource whose file no longer
// carries exactly one parseable block fails loud rather than being clobbered.
func filePublishBytes(m Mutation, obs managedfile.Observation, staged []byte) ([]byte, error) {
	if m.Kind != managedfile.KindBlock || !obs.Exists() {
		return staged, nil
	}
	if obs.Conflict == managedfile.ConflictMalformedBlock {
		return nil, fmt.Errorf("installstate: %s lost its parseable %s block between plan and apply", m.Path, managedfile.BlockOpen)
	}
	block, err := managedfile.ManagedBlock(staged)
	if err != nil {
		return nil, fmt.Errorf("installstate: staged %s carries no single %s block: %w", m.Unit, managedfile.BlockOpen, err)
	}
	spliced, err := managedfile.ReplaceBlock(obs.Bytes, block)
	if err != nil {
		return nil, fmt.Errorf("installstate: splice %s: %w", m.Path, err)
	}
	return spliced, nil
}

// PublishTree applies one staged generated-directory mutation. The journal
// records the transaction backup that will hold the displaced tree before the
// rename moves it, so an interruption mid-sequence is recoverable.
func (t *Transaction) PublishTree(unit string) error {
	m, _, err := t.beginPublish(unit)
	if err != nil {
		return err
	}
	if m.Kind != managedfile.KindTree {
		return fmt.Errorf("installstate: %s is not a generated tree; publish it with PublishFile", unit)
	}
	pub, err := t.PublishDirFn(m.Stage, m.Path, t.BackupRoot())
	if err != nil {
		return fmt.Errorf("installstate: publish tree %s: %w", unit, err)
	}
	m.AppliedSum = pub.Digest
	t.Journal.UpdateMutation(m)
	t.Journal.Record(unit, StateApplied, pub.Digest, t.Now())
	if err := t.writeJournal(); err != nil {
		return err
	}
	t.notify(StepPublished, unit)
	if pub.Digest != m.Intended {
		return fmt.Errorf("installstate: published tree %s digest %s does not match intended %s", unit, pub.Digest, m.Intended)
	}
	return t.verify(m)
}

// beginPublish validates the unit, observes current bytes, and journals the
// backup it is about to capture or displace, before any native mutation. It
// returns the updated mutation and the pre-mutation observation.
func (t *Transaction) beginPublish(unit string) (Mutation, managedfile.Observation, error) {
	m, ok := t.Journal.Mutation(unit)
	if !ok {
		return Mutation{}, managedfile.Observation{}, fmt.Errorf("installstate: no mutation %q in journal", unit)
	}
	if state := t.Journal.State(unit); state != StateStaged {
		return Mutation{}, managedfile.Observation{}, fmt.Errorf("installstate: %s is %s, not staged", unit, state)
	}
	if m.Stage == "" {
		return Mutation{}, managedfile.Observation{}, fmt.Errorf("installstate: %s has no staged projection", unit)
	}

	obs, err := t.Observe(m.Path, m.Kind)
	if err != nil {
		return Mutation{}, managedfile.Observation{}, err
	}
	t.Journal.Observations = append(t.Journal.Observations, obs)
	t.notify(StepObserved, unit)
	m.PriorObserved = true
	m.Prior = obs.Digest

	if obs.Exists() {
		if m.Kind == managedfile.KindTree {
			m.Backup = filepath.Join(t.BackupRoot(), filepath.Base(m.Path))
			m.BackupSum = obs.Digest
		} else {
			rec, backupErr := t.BackupFn(m.Path, filepath.Join(t.BackupRoot(), unit), t.ID, t.Now())
			if backupErr != nil {
				return Mutation{}, managedfile.Observation{}, backupErr
			}
			t.Journal.Backups = append(t.Journal.Backups, rec)
			m.Backup, m.BackupSum = rec.Path, rec.Digest
		}
	}
	t.Journal.UpdateMutation(m)
	if err := t.writeJournal(); err != nil {
		return Mutation{}, managedfile.Observation{}, err
	}
	t.notify(StepBackedUp, unit)
	return m, obs, nil
}

func (t *Transaction) verify(m Mutation) error {
	obs, err := t.Observe(m.Path, m.Kind)
	if err != nil {
		return err
	}
	if obs.Digest != m.Intended {
		return fmt.Errorf("installstate: %s effective content %s does not match intended %s", m.Path, obs.Digest, m.Intended)
	}
	t.Journal.Record(m.Unit, StateVerified, m.Intended, t.Now())
	if err := t.writeJournal(); err != nil {
		return err
	}
	t.notify(StepVerified, m.Unit)
	return nil
}

// Complete commits the ledger rows for every verified unit, then marks the
// journal completed. The ledger is durable first: an interruption between the
// two leaves an unresolved journal whose rows are already recorded, which
// recovery treats idempotently.
func (t *Transaction) Complete() error {
	for _, m := range t.Journal.Mutations {
		if t.Journal.State(m.Unit) != StateVerified {
			continue
		}
		t.Ledger.Upsert(m.LedgerRow())
		t.Journal.Record(m.Unit, StateCommitted, m.Intended, t.Now())
	}
	if err := t.WriteLedgerFn(t.LedgerPath, t.Ledger); err != nil {
		return err
	}
	t.notify(StepLedgerCommitted, "")

	if !t.wrote {
		return nil
	}
	t.Journal.Completed = true
	if err := t.writeJournal(); err != nil {
		return err
	}
	t.notify(StepJournalCompleted, "")
	return nil
}

func (t *Transaction) writeJournal() error {
	if len(t.Journal.Mutations) == 0 {
		return nil
	}
	first := !t.wrote
	if err := t.WriteJournalFn(t.JournalPath, t.Journal); err != nil {
		return err
	}
	t.wrote = true
	if first {
		t.notify(StepJournaled, "")
	}
	return nil
}

func (t *Transaction) notify(step Step, unit string) {
	if t.OnStep != nil {
		t.OnStep(step, unit)
	}
}
