// Package config manages atomic's TOML-backed configuration under ~/.atomic/:
// lenient load, strict write validation, get/set/unset, atomic file writes, and
// a markdown render of resolved values.
package config

import (
	"os"
	"path/filepath"
	"sort"
)

// Dir returns <home>/.atomic — the root of atomic-owned state.
func Dir(home string) string {
	return filepath.Join(home, ".atomic")
}

// TOMLPath returns the path to the user config file.
func TOMLPath(home string) string {
	return filepath.Join(Dir(home), "config.toml")
}

// BackupDir returns the directory where claudeinstall writes pre-write backups.
func BackupDir(home string) string {
	return filepath.Join(Dir(home), "backups")
}

// ProposedCLAUDEMD returns the path where claudeinstall writes a diverged
// CLAUDE.md for the user to review and merge.
func ProposedCLAUDEMD(home string) string {
	return filepath.Join(Dir(home), "proposed", "CLAUDE.md")
}

// PreInstallDir returns the directory where claudeinstall writes a write-once
// snapshot of every file it will touch, captured before the first Apply() call.
func PreInstallDir(home string) string {
	return filepath.Join(Dir(home), "pre-install")
}

// ProfilePath returns the user profile file. It is @-referenced from CLAUDE.md so
// every session loads it: created idempotently at install time, written
// opportunistically by Claude.
func ProfilePath(home string) string {
	return filepath.Join(Dir(home), "profile.md")
}

// ProfileRelPath returns profile.md's home-relative path with forward slashes,
// matching how pre-install manifests store it. Compare against manifest entries
// through this, never a hardcoded string.
func ProfileRelPath() string {
	return ".atomic/profile.md"
}

// StatePath returns ~/.atomic/state.json — the machine-managed source of truth
// for update-check cadence, staged downloads, and swap-lock coordination.
func StatePath(home string) string {
	return filepath.Join(Dir(home), "state.json")
}

// InstallDir returns ~/.atomic/install — the root of lifecycle operation state:
// the advisory operation lock, the enrollment ledger, unresolved journals, and
// per-operation transaction staging.
func InstallDir(home string) string {
	return filepath.Join(Dir(home), "install")
}

// OperationLockPath returns ~/.atomic/install/operation.lock — the advisory
// file every mutating lifecycle operation flocks. The inert path may persist;
// it carries no ownership meaning of its own.
func OperationLockPath(home string) string {
	return filepath.Join(InstallDir(home), "operation.lock")
}

// LedgerPath returns ~/.atomic/install/ledger.json — enrollment, physical
// resource, consumer, generation, tier, and last-applied authority.
func LedgerPath(home string) string {
	return filepath.Join(InstallDir(home), "ledger.json")
}

// JournalsDir returns ~/.atomic/install/journals — one JSON journal per
// in-flight operation.
func JournalsDir(home string) string {
	return filepath.Join(InstallDir(home), "journals")
}

// JournalPath returns ~/.atomic/install/journals/<operation-id>.json.
func JournalPath(home, operationID string) string {
	return filepath.Join(JournalsDir(home), operationID+".json")
}

// TransactionsDir returns ~/.atomic/install/transactions — per-operation
// staging and transaction backups.
func TransactionsDir(home string) string {
	return filepath.Join(InstallDir(home), "transactions")
}

// TransactionDir returns ~/.atomic/install/transactions/<operation-id>.
func TransactionDir(home, operationID string) string {
	return filepath.Join(TransactionsDir(home), operationID)
}

// TransactionStageDir returns the per-operation staging directory, published
// into place only after its bytes validate.
func TransactionStageDir(home, operationID string) string {
	return filepath.Join(TransactionDir(home, operationID), "stage")
}

// TransactionBackupDir returns the per-operation backup directory holding the
// pre-mutation copies an unresolved journal still owns.
func TransactionBackupDir(home, operationID string) string {
	return filepath.Join(TransactionDir(home, operationID), "backup")
}

// BackupStampDir returns ~/.atomic/backups/<stamp> — one timestamped backup set.
func BackupStampDir(home, stamp string) string {
	return filepath.Join(BackupDir(home), stamp)
}

// BackupStamps lists the timestamped backup sets under ~/.atomic/backups,
// oldest-first by name. A missing backups root is an empty list, not an error.
func BackupStamps(home string) ([]string, error) {
	entries, err := os.ReadDir(BackupDir(home))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stamps []string
	for _, e := range entries {
		if e.IsDir() {
			stamps = append(stamps, e.Name())
		}
	}
	sort.Strings(stamps)
	return stamps, nil
}

// RemoveBackupStamp removes one timestamped backup set. A missing set is a
// no-op so cleanup stays replayable.
func RemoveBackupStamp(home, stamp string) error {
	return os.RemoveAll(BackupStampDir(home, stamp))
}
