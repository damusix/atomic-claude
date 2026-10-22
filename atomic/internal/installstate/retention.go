package installstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func ledgerPathFor(home string) string { return config.LedgerPath(home) }

func transactionsRootFor(home string) string { return config.TransactionsDir(home) }

// Retention is the operational state a full uninstall must preserve because an
// unresolved journal still references it: the journal itself, the transaction
// backups it can still restore from, the state selections it depends on, the
// ledger rows it has yet to commit, and the targets those rows record.
type Retention struct {
	Journals   []string `json:"journals"`
	Backups    []string `json:"backups"`
	Selections []string `json:"selections,omitempty"`
	Rows       []Row    `json:"rows,omitempty"`
	// Targets holds the target keys unresolved journals still mutate.
	Targets []string `json:"targets,omitempty"`
}

// ComputeRetention reads the journals directory and returns everything an
// unresolved journal still owns. Completed journals contribute nothing, so
// their operational state is removable.
func ComputeRetention(journalsDir string, led *Ledger) (Retention, error) {
	paths, err := JournalPaths(journalsDir)
	if err != nil {
		return Retention{}, err
	}

	var ret Retention
	for _, path := range paths {
		j, err := LoadJournal(path)
		if err != nil {
			return Retention{}, err
		}
		if j.Completed {
			continue
		}
		ret.Journals = append(ret.Journals, path)
		ret.Selections = dedupe(append(ret.Selections, j.SelectionDependencies...))
		backups := append([]string{}, ret.Backups...)
		targets := make([]string, 0, len(j.Mutations))
		for _, m := range j.Mutations {
			backups = append(backups, m.Backup)
			targets = append(targets, m.Target)
		}
		for _, b := range j.Backups {
			backups = append(backups, b.Path)
		}
		ret.Backups = dedupe(backups)
		ret.Targets = dedupe(append(ret.Targets, targets...))
		if led == nil {
			continue
		}
		for _, row := range led.Rows {
			if !ret.KeepsRow(row) && journalReferencesRow(j, row) {
				ret.Rows = append(ret.Rows, row)
			}
		}
	}
	return ret, nil
}

// dedupe preserves order while dropping empty and repeated references, so one
// backup recorded in both the mutation and the backup list counts once.
func dedupe(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// Keeps reports whether path must survive a full uninstall: it is referenced
// directly, or it is an ancestor directory holding something referenced (a
// transaction directory holding a referenced backup).
func (r Retention) Keeps(path string) bool {
	for _, ref := range r.references() {
		if ref == path || within(path, ref) {
			return true
		}
	}
	return false
}

// KeepsRow reports whether a ledger row is referenced by an unresolved journal.
func (r Retention) KeepsRow(row Row) bool {
	for _, kept := range r.Rows {
		if kept.Target == row.Target && kept.Resource == row.Resource {
			return true
		}
	}
	return false
}

// KeepsTarget reports whether an unresolved journal still mutates the enrolled
// target. Cleanup also keeps a target a retained ownership row names, which it
// matches from the retained rows rather than from here.
func (r Retention) KeepsTarget(record TargetRecord) bool {
	key := record.Key()
	for _, ref := range r.Targets {
		if ref == key {
			return true
		}
	}
	return false
}

// rowsNameTarget reports whether any row still names record.
func rowsNameTarget(rows []Row, record TargetRecord) bool {
	key := record.Key()
	for _, row := range rows {
		if row.Target == key {
			return true
		}
	}
	return false
}

// keepsRow reports whether row is one of the rows a caller asked Cleanup to
// retain. Row identity is the target plus resource pair, the ledger's key.
func keepsRow(keep []Row, row Row) bool {
	for _, k := range keep {
		if k.Target == row.Target && k.Resource == row.Resource {
			return true
		}
	}
	return false
}

func (r Retention) references() []string {
	refs := make([]string, 0, len(r.Journals)+len(r.Backups)+len(r.Selections))
	refs = append(refs, r.Journals...)
	refs = append(refs, r.Backups...)
	refs = append(refs, r.Selections...)
	return refs
}

func journalReferencesRow(j *Journal, row Row) bool {
	for _, m := range j.Mutations {
		if m.Resource != row.Resource {
			continue
		}
		if m.Target == "" || m.Target == row.Target {
			return true
		}
	}
	return false
}

// CleanupResult records what Cleanup removed.
type CleanupResult struct {
	RemovedJournals     []string       `json:"removed_journals,omitempty"`
	RemovedTransactions []string       `json:"removed_transactions,omitempty"`
	RemovedRows         []Row          `json:"removed_rows,omitempty"`
	RemovedTargets      []TargetRecord `json:"removed_targets,omitempty"`
}

// CleanupOperation removes the operational state of one consumed operation: its
// completed journal and its transaction directory. It is the post-recovery
// cleanup, so it never touches the ledger or a selection dependency — a
// verified row is ownership authority, and dropping it here would let the next
// operation classify a healthy install as orphaned. Only the full-uninstall
// path (Cleanup) may remove a row. An unresolved journal is refused: recovery
// has not consumed it, and its transaction directory still holds the only
// pre-mutation copy.
//
// The transaction directory goes first and the journal last. Removing the
// journal first would strand the directory when its removal fails: with no
// journal to match it to, the orphan-preservation rule keeps it forever. This
// order leaves both present after an interruption, so the next cleanup retries.
func CleanupOperation(home, operationID string) (CleanupResult, error) {
	var result CleanupResult
	if err := config.ValidateSegment("operation id", operationID); err != nil {
		return result, err
	}

	journalPath := config.JournalPath(home, operationID)
	j, err := LoadJournal(journalPath)
	if err != nil {
		return result, err
	}
	if !j.Completed {
		return result, fmt.Errorf("installstate: journal %s is not completed; recovery must consume the operation before cleanup", journalPath)
	}

	dir := config.TransactionDir(home, operationID)
	exists, err := pathExists(dir)
	if err != nil {
		return result, err
	}
	if exists {
		if err := os.RemoveAll(dir); err != nil {
			return result, fmt.Errorf("installstate: remove transactions %s: %w", dir, err)
		}
		result.RemovedTransactions = append(result.RemovedTransactions, dir)
	}

	if err := os.Remove(journalPath); err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("installstate: remove completed journal %s: %w", journalPath, err)
	}
	result.RemovedJournals = append(result.RemovedJournals, journalPath)
	return result, nil
}

// Cleanup is the full-uninstall path: it removes completed journals, the
// transaction directories of operations whose journal completed, ledger rows no
// unresolved journal references, and target records no retained row and no
// unresolved journal references. Unresolved journals, their referenced
// transaction backups, referenced ledger rows and their targets, and referenced
// selections survive. A transaction directory with no journal at all is
// orphaned v2 evidence and is never auto-deleted. Backups under ~/.atomic/backups
// are user recovery history and are untouched. Post-recovery cleanup is
// CleanupOperation, which never drops a ledger row or a target record.
//
// A completed operation's transaction directory is removed before its journal,
// so a failed directory removal cannot strand a journal-less orphan.
//
// keep names ledger rows a caller's removal could not clear — a resource still
// on disk behind a read-only file. They are not completed state: keeping them,
// and the target records they name, lets a later uninstall finish the job.
func Cleanup(home string, led *Ledger, now time.Time, keep ...Row) (CleanupResult, error) {
	var result CleanupResult

	journalsDir := config.JournalsDir(home)
	ret, err := ComputeRetention(journalsDir, led)
	if err != nil {
		return result, err
	}

	paths, err := JournalPaths(journalsDir)
	if err != nil {
		return result, err
	}
	// Collect the completed operations read-only first, then remove their
	// transaction directories, then their journals. A directory removal can
	// fail (a permission or a busy handle), and removing the journal first
	// would strand that directory: with no journal to match it to, the
	// orphan-preservation rule below keeps it forever. Journal last leaves
	// both present after an interruption, so the next cleanup retries.
	completed := make(map[string]bool)
	var completedJournals []string
	for _, path := range paths {
		j, err := LoadJournal(path)
		if err != nil {
			return result, err
		}
		if !j.Completed {
			continue
		}
		completed[strings.TrimSuffix(filepath.Base(path), ".json")] = true
		completedJournals = append(completedJournals, path)
	}

	entries, err := os.ReadDir(config.TransactionsDir(home))
	if err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("installstate: read transactions dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(config.TransactionsDir(home), e.Name())
		if ret.Keeps(dir) || !completed[e.Name()] {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return result, fmt.Errorf("installstate: remove transactions %s: %w", dir, err)
		}
		result.RemovedTransactions = append(result.RemovedTransactions, dir)
	}

	for _, path := range completedJournals {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return result, fmt.Errorf("installstate: remove completed journal %s: %w", path, err)
		}
		result.RemovedJournals = append(result.RemovedJournals, path)
	}

	if led == nil {
		return result, nil
	}
	kept := led.Rows[:0:0]
	for _, row := range led.Rows {
		if ret.KeepsRow(row) || keepsRow(keep, row) {
			kept = append(kept, row)
			continue
		}
		result.RemovedRows = append(result.RemovedRows, row)
	}
	if len(result.RemovedRows) > 0 {
		led.Rows = kept
	}

	// A target record is an enrollment claim. Once no retained row names it and
	// no unresolved journal still mutates it, its resources are gone and the
	// record would keep the ledger reporting an enrollment that no longer
	// exists.
	keptTargets := led.Targets[:0:0]
	for _, record := range led.Targets {
		if ret.KeepsTarget(record) || rowsNameTarget(kept, record) {
			keptTargets = append(keptTargets, record)
			continue
		}
		result.RemovedTargets = append(result.RemovedTargets, record)
	}
	if len(result.RemovedTargets) > 0 {
		led.Targets = keptTargets
	}

	if len(result.RemovedRows) > 0 || len(result.RemovedTargets) > 0 {
		if err := led.Save(config.LedgerPath(home)); err != nil {
			return result, err
		}
	}
	return result, nil
}

// within reports whether path is root itself or sits below it, without escaping
// through a parent reference.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if filepath.IsAbs(rel) {
		return false
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == ".." {
			return false
		}
	}
	return true
}
