package installstate

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// RecoveryDecision is the digest-driven outcome for one commit unit.
type RecoveryDecision string

const (
	// DecisionSkipCommitted leaves a completed generation alone.
	DecisionSkipCommitted RecoveryDecision = "skip-committed"
	// DecisionCommitApplied records applied bytes that verify against the
	// intended digest, without re-applying anything.
	DecisionCommitApplied RecoveryDecision = "commit-applied"
	// DecisionDiscardStaging removes a proven-unused staging projection.
	DecisionDiscardStaging RecoveryDecision = "discard-staging"
	// DecisionRestoreBackup writes digest-unchanged applied bytes back from a
	// transaction backup.
	DecisionRestoreBackup RecoveryDecision = "restore-backup"
	// DecisionNoop is a unit the operation never reached.
	DecisionNoop RecoveryDecision = "noop"
	// DecisionConflict is a unit whose native bytes no longer match what the
	// journal describes; recovery never overwrites it.
	DecisionConflict RecoveryDecision = "conflict"
)

// RecoveryAction is one decision about one commit unit.
type RecoveryAction struct {
	Unit     string           `json:"unit"`
	Decision RecoveryDecision `json:"decision"`
	Path     string           `json:"path,omitempty"`
	Detail   string           `json:"detail,omitempty"`
}

// RecoveryResult is the complete reconciliation of one journal.
type RecoveryResult struct {
	Actions   []RecoveryAction `json:"actions"`
	Conflicts []RecoveryAction `json:"conflicts,omitempty"`
}

// Recovery reconciles one unresolved journal against current native bytes. It
// is digest-driven throughout: applied bytes are committed only after they
// verify, staging is discarded only with journal proof, backups are restored
// only when no later edit changed the bytes, and anything else is a conflict.
type Recovery struct {
	Journal    *Journal
	Ledger     *Ledger
	LedgerPath string
	// JournalPath is where reconciled decisions are persisted; empty skips the
	// rewrite and leaves the journal as loaded.
	JournalPath string
	// TransactionsRoot confines every staging path recovery may remove; a stage
	// path outside it is a conflict, not a cleanup.
	TransactionsRoot string

	// Observe re-reads native state. For file kinds it must return the
	// resource's current bytes: rollback compares whole-file digests, which a
	// block-scoped digest cannot prove.
	Observe         func(path string, kind managedfile.Kind) (managedfile.Observation, error)
	Exists          func(path string) (bool, error)
	RemoveStaging   func(path string) error
	RestoreBackupFn func(rec managedfile.BackupRecord, kind managedfile.Kind) error
	SaveLedger      func(path string, l *Ledger) error
	WriteJournalFn  func(path string, j *Journal) error

	ledgerChanged bool
	journalDirty  bool
}

// NewRecovery wires a recovery onto one journal with production seams.
func NewRecovery(home string, j *Journal, ledger *Ledger) *Recovery {
	return &Recovery{
		Journal:          j,
		Ledger:           ledger,
		LedgerPath:       ledgerPathFor(home),
		JournalPath:      config.JournalPath(home, j.OperationID),
		TransactionsRoot: transactionsRootFor(home),
		Observe:          managedfile.Observe,
		Exists:           pathExists,
		RemoveStaging:    os.RemoveAll,
		RestoreBackupFn:  restoreBackup,
		SaveLedger:       func(path string, l *Ledger) error { return l.Save(path) },
		WriteJournalFn:   WriteJournal,
	}
}

// Recover rolls the operation forward: verified and already-applied bytes are
// ledger-committed, proven-unused staging is discarded, and a unit whose native
// bytes moved on is reported as a conflict. It never restores a backup — that
// is Rollback's explicit decision.
func (r *Recovery) Recover() (RecoveryResult, error) {
	var result RecoveryResult

	for _, m := range r.Journal.Mutations {
		decision, detail, err := r.forwardDecision(m)
		if err != nil {
			return result, err
		}
		switch decision {
		case DecisionCommitApplied:
			r.Ledger.Upsert(m.LedgerRow())
			r.ledgerChanged = true
			r.Journal.Record(m.Unit, StateCommitted, m.Intended, time.Now().UTC())
			r.journalDirty = true
			result.add(m, decision, detail)
		case DecisionDiscardStaging:
			action, err := r.discardStaging(m)
			if err != nil {
				return result, err
			}
			result.record(action)
		default:
			result.add(m, decision, detail)
		}
	}

	if r.ledgerChanged && r.LedgerPath != "" {
		save := r.SaveLedger
		if save == nil {
			save = func(path string, l *Ledger) error { return l.Save(path) }
		}
		if err := save(r.LedgerPath, r.Ledger); err != nil {
			return result, err
		}
	}
	if r.journalDirty && r.JournalPath != "" {
		// The applied decisions are durable before anything may consume the
		// journal, so a second recovery run reaches the same conclusions.
		if err := r.writeJournal(r.JournalPath); err != nil {
			return result, err
		}
	}
	return result, nil
}

// forwardDecision judges one incomplete unit against current native bytes. It
// changes nothing; Recover applies the ledger and journal effects the decision
// implies.
//
// The distinction that keeps recovery non-destructive is whether a native
// mutation could already have landed. Every publication journals the
// pre-mutation observation before it writes, so a unit carrying no such
// observation was never published and its staging is provably unused. Once it
// carries one, the decision is digest-driven: matching intended bytes are
// recorded as applied, matching pre-mutation bytes prove the write never
// landed, and anything else is a conflict rather than a discard — a discard
// would let cleanup delete the transaction's only pre-mutation backup while the
// resource holds Atomic bytes no ledger row accounts for.
func (r *Recovery) forwardDecision(m Mutation) (RecoveryDecision, string, error) {
	switch r.Journal.State(m.Unit) {
	case StateCommitted:
		return DecisionSkipCommitted, "completed generation is never implicitly rolled back", nil

	case StateVerified, StateApplied:
		obs, err := r.Observe(m.Path, m.Kind)
		if err != nil {
			return "", "", err
		}
		switch {
		case m.Intended != "" && obs.Digest == m.Intended:
			return DecisionCommitApplied, "applied bytes verified against the intended digest", nil
		case m.BackupSum != "" && wholeDigest(obs) == m.BackupSum:
			return DecisionNoop, "native bytes already hold the pre-mutation backup", nil
		default:
			return DecisionConflict, fmt.Sprintf("native bytes at %s (digest %s) match neither the intended digest %s nor the backup digest %s", m.Path, digestOrAbsent(obs), m.Intended, m.BackupSum), nil
		}

	default:
		if m.Stage == "" {
			return DecisionNoop, "no staged projection", nil
		}
		if !m.PriorObserved {
			return DecisionDiscardStaging, "journal proves the staged projection was never applied", nil
		}
		obs, err := r.Observe(m.Path, m.Kind)
		if err != nil {
			return "", "", err
		}
		switch {
		case m.Intended != "" && obs.Digest == m.Intended:
			return DecisionCommitApplied, "the native write landed before the journal recorded it as applied", nil
		case m.BackupSum != "" && wholeDigest(obs) == m.BackupSum:
			return DecisionDiscardStaging, "native bytes still hold the pre-mutation backup, so the staged projection was never applied", nil
		case m.Prior != "" && obs.Digest == m.Prior:
			return DecisionDiscardStaging, "native bytes still hold the observed pre-mutation digest, so the staged projection was never applied", nil
		case !obs.Exists() && m.BackupSum == "":
			return DecisionDiscardStaging, "the resource is still absent and the transaction captured no pre-mutation copy", nil
		default:
			return DecisionConflict, fmt.Sprintf("native bytes at %s (digest %s) match neither the intended digest %s nor the pre-mutation digest %s, and the transaction holds a pre-mutation copy; refusing to discard the staged projection", m.Path, digestOrAbsent(obs), m.Intended, m.Prior), nil
		}
	}
}

// Rollback restores transaction backups for applied-but-incomplete units whose
// native bytes are still exactly what Atomic published. The comparison is on
// the whole resource, not the managed block, so an edit outside the block is a
// conflict rather than a restore that would discard the user's prose. An absent
// destination is a legitimate rollback target only for a generated tree, whose
// publication displaces the current tree before moving the staged one into
// place; for a file resource absence means the user deleted it, and rollback
// never resurrects a deletion.
func (r *Recovery) Rollback() (RecoveryResult, error) {
	var result RecoveryResult

	for _, m := range r.Journal.Mutations {
		if r.Journal.State(m.Unit) == StateCommitted {
			result.add(m, DecisionSkipCommitted, "completed generation is never implicitly rolled back")
			continue
		}
		if m.Backup == "" {
			result.add(m, DecisionNoop, "no transaction backup to restore")
			continue
		}
		obs, err := r.Observe(m.Path, m.Kind)
		if err != nil {
			return result, err
		}
		switch {
		case m.BackupSum != "" && wholeDigest(obs) == m.BackupSum:
			result.add(m, DecisionNoop, "backup bytes are already in place")
		case rollbackTarget(m, obs):
			rec, err := r.backupRecord(m)
			if err != nil {
				return result, err
			}
			if err := r.RestoreBackupFn(rec, m.Kind); err != nil {
				result.conflict(m, fmt.Sprintf("restore backup: %v", err))
				continue
			}
			result.add(m, DecisionRestoreBackup, "applied bytes were digest-unchanged; backup restored")
		default:
			result.conflict(m, fmt.Sprintf("native bytes at %s (digest %s) changed after the operation; refusing to overwrite", m.Path, digestOrAbsent(obs)))
		}
	}
	return result, nil
}

// rollbackTarget proves the resource still holds exactly the bytes Atomic
// published. A tree compares by its whole identity, which is its digest; an
// unapplied tree falls back to the intended digest, the only evidence an
// interruption before the applied record leaves behind. An absent destination
// is a target only for a generated tree, whose publication displaces the
// current tree before moving the staged one into place.
func rollbackTarget(m Mutation, obs managedfile.Observation) bool {
	if !obs.Exists() {
		return m.Kind == managedfile.KindTree
	}
	if m.AppliedSum != "" {
		return wholeDigest(obs) == m.AppliedSum
	}
	return m.Kind == managedfile.KindTree && m.Intended != "" && obs.Digest == m.Intended
}

// wholeDigest is a resource's whole-bytes digest in the space backup and
// applied digests are recorded in: the file's bytes for file and block
// resources, the tree digest for a generated tree, and nothing for an absent
// resource. A block resource's observation digest covers only the managed
// block, which cannot prove the rest of the file is unchanged.
func wholeDigest(obs managedfile.Observation) string {
	if !obs.Exists() {
		return ""
	}
	if obs.Kind == managedfile.KindTree {
		return obs.Digest
	}
	return managedfile.Digest(obs.Bytes)
}

// ConsumeJournal marks the reconciled journal completed, letting
// CleanupOperation remove the operational state recovery just handled — never
// the full-uninstall Cleanup, which also drops ledger rows. A recovery that
// found conflicts consumes nothing: the journal survives until the conflict is
// resolved.
func (r *Recovery) ConsumeJournal(journalPath string, result RecoveryResult) error {
	if len(result.Conflicts) > 0 {
		return fmt.Errorf("installstate: journal %s still has %d unresolved conflict(s)", journalPath, len(result.Conflicts))
	}
	r.Journal.Completed = true
	return r.writeJournal(journalPath)
}

func (r *Recovery) writeJournal(path string) error {
	if r.WriteJournalFn != nil {
		return r.WriteJournalFn(path, r.Journal)
	}
	return WriteJournal(path, r.Journal)
}

func (r *Recovery) discardStaging(m Mutation) (RecoveryAction, error) {
	if m.Stage == "" {
		return RecoveryAction{Unit: m.Unit, Decision: DecisionNoop, Path: m.Path, Detail: "no staged projection"}, nil
	}
	if r.TransactionsRoot != "" && !within(r.TransactionsRoot, m.Stage) {
		return RecoveryAction{Unit: m.Unit, Decision: DecisionConflict, Path: m.Stage, Detail: "staged path is outside the transaction root"}, nil
	}
	exists, err := r.Exists(m.Stage)
	if err != nil {
		return RecoveryAction{}, err
	}
	if !exists {
		return RecoveryAction{Unit: m.Unit, Decision: DecisionNoop, Path: m.Stage, Detail: "staging already removed"}, nil
	}
	if err := r.RemoveStaging(m.Stage); err != nil {
		return RecoveryAction{}, fmt.Errorf("installstate: discard staging %s: %w", m.Stage, err)
	}
	return RecoveryAction{Unit: m.Unit, Decision: DecisionDiscardStaging, Path: m.Stage, Detail: "journal proves the staged projection was never applied"}, nil
}

func (r *Recovery) backupRecord(m Mutation) (managedfile.BackupRecord, error) {
	if rec, ok := r.Journal.Backup(m.Unit); ok {
		return rec, nil
	}
	// A generated tree's displaced copy is recorded as a path plus digest, not
	// as a managedfile file backup.
	return managedfile.BackupRecord{
		Source:      m.Path,
		Path:        m.Backup,
		Digest:      m.BackupSum,
		OperationID: r.Journal.OperationID,
	}, nil
}

func (result *RecoveryResult) add(m Mutation, decision RecoveryDecision, detail string) {
	result.record(RecoveryAction{Unit: m.Unit, Decision: decision, Path: m.Path, Detail: detail})
}

// record appends one decision, mirroring a conflict into the conflict list so
// callers that surface conflicts separately never miss one.
func (result *RecoveryResult) record(action RecoveryAction) {
	result.Actions = append(result.Actions, action)
	if action.Decision == DecisionConflict {
		result.Conflicts = append(result.Conflicts, action)
	}
}

func (result *RecoveryResult) conflict(m Mutation, detail string) {
	result.record(RecoveryAction{Unit: m.Unit, Decision: DecisionConflict, Path: m.Path, Detail: detail})
}

func digestOrAbsent(obs managedfile.Observation) string {
	if !obs.Exists() {
		return "absent"
	}
	return obs.Digest
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// restoreBackup restores a file backup in place, or moves a displaced
// generated tree back over the tree that replaced it. A tree backup is
// digest-verified against the record before the destination is touched, so a
// tampered transaction backup is refused exactly as a tampered file backup is.
func restoreBackup(rec managedfile.BackupRecord, kind managedfile.Kind) error {
	if kind == managedfile.KindTree {
		digest, _, err := managedfile.TreeDigest(rec.Path)
		if err != nil {
			return fmt.Errorf("installstate: digest tree backup %s: %w", rec.Path, err)
		}
		if digest != rec.Digest {
			return fmt.Errorf("installstate: tree backup %s changed after capture", rec.Path)
		}
		if err := os.RemoveAll(rec.Source); err != nil {
			return fmt.Errorf("installstate: clear %s: %w", rec.Source, err)
		}
		if err := os.MkdirAll(filepath.Dir(rec.Source), 0o755); err != nil {
			return fmt.Errorf("installstate: mkdir %s: %w", filepath.Dir(rec.Source), err)
		}
		if err := managedfile.MoveTree(rec.Path, rec.Source); err != nil {
			return fmt.Errorf("installstate: restore tree %s: %w", rec.Source, err)
		}
		return nil
	}
	return managedfile.RestoreBackup(rec)
}
