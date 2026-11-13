package harness

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// ErrNotEnrolled reports a target the ledger does not record.
var ErrNotEnrolled = errors.New("harness: target is not enrolled")

// RemovalOptions tunes a removal. The zero value skips changed bytes and keeps
// their claim.
type RemovalOptions struct {
	// DiscardChanged releases Atomic's claim on every resource the default plan
	// skipped, after Confirm approves it by id.
	DiscardChanged bool
	// Confirm is asked once per skipped resource so a batch never discards bytes
	// the operator did not see named. Required with DiscardChanged.
	Confirm func(resource, reason string) (bool, error)
	// Approved is the discard set the caller printed before applying. A
	// resource the removal would discard outside it refuses the whole removal.
	Approved []string
}

// PlanTargetRemoval is the read-only removal plan for one enrolled target. A
// changed resource is skipped rather than aborting, so the rest still uninstalls.
func PlanTargetRemoval(home string, t Target, ledger *installstate.Ledger) (Removal, map[string]installstate.Row, error) {
	out, rows, err := planTargetRemoval(home, t.Key(), ledger)
	out.Target = t
	return out, rows, err
}

// PlanTargetRowRemoval is PlanTargetRemoval for a raw ledger key, the only
// identity a stale row whose key does not parse has.
func PlanTargetRowRemoval(home, key string, ledger *installstate.Ledger) (Removal, map[string]installstate.Row, error) {
	return planTargetRemoval(home, key, ledger)
}

func planTargetRemoval(home, key string, ledger *installstate.Ledger) (Removal, map[string]installstate.Row, error) {
	out := Removal{TargetKey: key}
	if home == "" {
		return out, nil, fmt.Errorf("harness: uninstall %s: no home", key)
	}
	if key == "" {
		return out, nil, fmt.Errorf("harness: uninstall: target key is empty")
	}

	if _, ok := ledgerTargetByKey(ledger, key); !ok {
		hasRow := false
		for _, row := range ledger.Rows {
			if row.Target == key {
				hasRow = true
				break
			}
		}
		if !hasRow {
			return out, nil, fmt.Errorf("harness: uninstall %s: %w", key, ErrNotEnrolled)
		}
	}

	type owned struct {
		row       installstate.Row
		consumers []string
	}
	byResource := map[string]*owned{}
	rows := map[string]installstate.Row{}
	var order []string
	for _, row := range ledger.Rows {
		if row.Target != key {
			continue
		}
		o, ok := byResource[row.Resource]
		if !ok {
			o = &owned{row: row}
			byResource[row.Resource] = o
			order = append(order, row.Resource)
		}
		rows[row.Resource] = row
		// Claude ids are root-relative, so the same id under another root is a
		// different file; only a row at the same physical path shares it.
		for _, other := range ledger.Rows {
			if other.Resource == row.Resource && other.Target != key && samePhysicalPath(other.Applied.Path, row.Applied.Path) {
				o.consumers = appendUnique(o.consumers, other.Target)
			}
		}
	}

	for _, id := range order {
		o := byResource[id]
		if o.row.Applied.Kind == managedfile.KindSymlink {
			out.Left = append(out.Left, id)
			delete(rows, id)
			continue
		}
		if len(o.consumers) > 0 {
			out.Retained = append(out.Retained, id)
			delete(rows, id)
			continue
		}
		if err := verifyOwned(o.row); err != nil {
			if errors.Is(err, ErrEvidenceConflict) {
				// Drift skips one resource so the rest of the target still uninstalls.
				out.Skipped = append(out.Skipped, id)
				out.NoteSkip(id, trimConflict(err))
				continue
			}
			return out, nil, err
		}
		out.Removed = append(out.Removed, id)
	}
	sort.Strings(out.Removed)
	sort.Strings(out.Retained)
	sort.Strings(out.Skipped)
	sort.Strings(out.Left)
	return out, rows, nil
}

// samePhysicalPath compares parent directories by file identity, so a symlinked
// root or a case-only difference on a case-insensitive volume is one path.
func samePhysicalPath(a, b string) bool {
	if filepath.Base(a) != filepath.Base(b) {
		return false
	}
	da, errA := os.Stat(filepath.Dir(a))
	db, errB := os.Stat(filepath.Dir(b))
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return os.SameFile(da, db)
}

// RemoveTargetResources applies the verified removal plan and commits the
// ledger. The caller must hold the lifecycle lock.
func RemoveTargetResources(home string, t Target, opts ...RemovalOptions) (Removal, error) {
	return removeTargetResources(home, t.Key(), t, opts...)
}

// RemoveTargetRows is RemoveTargetResources for a raw ledger key, the only
// identity a stale row whose key does not parse has.
func RemoveTargetRows(home, key string, opts ...RemovalOptions) (Removal, error) {
	return removeTargetResources(home, key, Target{}, opts...)
}

func removeTargetResources(home, key string, t Target, opts ...RemovalOptions) (Removal, error) {
	var opt RemovalOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return Removal{Target: t, TargetKey: key}, err
	}
	// The target a key parses into carries identity only, so the native root the
	// prune is bounded by comes from the enrolled record.
	if t.NativeRoot == "" {
		if record, ok := ledgerTargetByKey(ledger, key); ok {
			t.NativeRoot = record.NativeRoot
		}
	}
	out, owned, err := planTargetRemoval(home, key, ledger)
	if err != nil {
		return out, err
	}
	out.Target = t

	discard := map[string]bool{}
	if opt.DiscardChanged {
		if opt.Confirm == nil {
			return out, fmt.Errorf("harness: uninstall %s: discarding changed resources needs a per-resource confirmation", key)
		}
		for _, id := range out.Skipped {
			if !contains(opt.Approved, id) {
				return out, fmt.Errorf("harness: uninstall %s: %s changed after the discard plan was printed; nothing was discarded, re-run to review the new plan", key, id)
			}
		}
		for _, id := range out.Skipped {
			ok, err := opt.Confirm(id, out.SkipReasons[id])
			if err != nil {
				return out, err
			}
			if ok {
				discard[id] = true
			}
		}
	}

	// Settings rows go last: their outputStyle member is dropped only once the
	// style file it names, another row in this plan, is gone.
	toRemove := make([]string, 0, len(out.Removed)+len(discard))
	toRemove = append(toRemove, out.Removed...)
	for _, id := range out.Skipped {
		if discard[id] && !contains(out.Removed, id) {
			toRemove = append(toRemove, id)
		}
	}
	ordered := make([]string, 0, len(toRemove))
	for _, id := range toRemove {
		if owned[id].Applied.Kind != managedfile.KindSettings {
			ordered = append(ordered, id)
		}
	}
	for _, id := range toRemove {
		if owned[id].Applied.Kind == managedfile.KindSettings {
			ordered = append(ordered, id)
		}
	}

	// Seeded so the reconciliation below never drops a row whose bytes still
	// hold Atomic content.
	skipped := map[string]bool{}
	for _, id := range out.Skipped {
		skipped[id] = true
	}
	var discarded []string
	var pruned []string
	keyUnit, err := installstate.UnitFor(key)
	if err != nil {
		return out, err
	}
	backupDir := filepath.Join(config.BackupStampDir(home, "discard-"+config.BackupStamp(time.Now())), keyUnit)
	keptCopy := false
	failed := func(err error) (Removal, error) {
		if keptCopy {
			err = fmt.Errorf("%w; recovery copies of the resources already discarded are under %s", err, backupDir)
		}
		return out, err
	}
	for _, id := range ordered {
		row := owned[id]
		if discard[id] {
			cleared, backedUp, err := discardResource(row, backupDir)
			if err != nil {
				return failed(err)
			}
			keptCopy = keptCopy || backedUp
			delete(skipped, id)
			discarded = append(discarded, id)
			if !cleared {
				out.NoteSkip(id, "Atomic's bytes could not be removed, so its claim was released with the bytes left in place")
			}
			pruned = append(pruned, PruneEmptyContainers(row.Applied.Path, t.NativeRoot)...)
			continue
		}
		removed, err := removeResource(row)
		if err != nil {
			return failed(err)
		}
		if !removed {
			skipped[id] = true
			out.NoteSkip(id, "Atomic could not write the resource")
			continue
		}
		pruned = append(pruned, PruneEmptyContainers(row.Applied.Path, t.NativeRoot)...)
	}
	if keptCopy {
		out.Notices = append(out.Notices, "recovery copies of the discarded resources are kept under "+backupDir)
	}
	sort.Strings(discarded)
	sort.Strings(pruned)
	out.Discarded = discarded
	out.Pruned = pruned
	// An uncleared resource still holds Atomic's bytes, so it keeps its row and
	// enrollment; a discarded one releases its claim either way.
	out.Skipped = out.Skipped[:0:0]
	for id := range skipped {
		out.Skipped = append(out.Skipped, id)
	}
	sort.Strings(out.Skipped)
	if len(skipped) > 0 {
		keptRemoved := out.Removed[:0:0]
		for _, id := range out.Removed {
			if !skipped[id] {
				keptRemoved = append(keptRemoved, id)
			}
		}
		out.Removed = keptRemoved
	}

	kept := ledger.Rows[:0:0]
	for _, row := range ledger.Rows {
		if row.Target == key && !skipped[row.Resource] {
			continue
		}
		kept = append(kept, row)
	}
	ledger.Rows = kept
	keptTargets := ledger.Targets[:0:0]
	for _, record := range ledger.Targets {
		if record.Key() == key && len(out.Skipped) == 0 {
			continue
		}
		keptTargets = append(keptTargets, record)
	}
	ledger.Targets = keptTargets
	if len(out.Skipped) == 0 {
		ledger.ForgetLeft(key)
	}
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		return out, err
	}
	return out, nil
}

// ledgerTargetByKey matches a raw key literally, so a key that does not parse
// still resolves to its record.
func ledgerTargetByKey(ledger *installstate.Ledger, key string) (installstate.TargetRecord, bool) {
	for _, record := range ledger.Targets {
		if record.Key() == key {
			return record, true
		}
	}
	return installstate.TargetRecord{}, false
}

// verifyOwned proves a resource still holds exactly what Atomic recorded
// writing. Missing bytes pass: only the ledger row is left to remove.
func verifyOwned(row installstate.Row) error {
	if row.Applied.Digest == "" {
		return fmt.Errorf("%w: %s carries no recorded digest, so its bytes cannot be verified", ErrEvidenceConflict, row.Resource)
	}
	obs, err := installstate.ObserveApplied(row.Applied)
	if err != nil {
		return err
	}
	if obs.Conflict != managedfile.ConflictNone {
		return fmt.Errorf("%w: %s carries an ambiguous managed block", ErrEvidenceConflict, row.Applied.Path)
	}
	if !obs.Exists() {
		return nil
	}
	if _, ok := obs.Verify(row.Applied.Digest); !ok {
		return fmt.Errorf("%w: %s changed after Atomic wrote it (observed %s, recorded %s); resolve it before uninstalling",
			ErrEvidenceConflict, row.Applied.Path, obs.Digest, row.Applied.Digest)
	}
	return nil
}

// removeResource deletes one verified resource. removed is false for a
// read-only settings file, so the caller keeps the claim the bytes still support.
func removeResource(row installstate.Row) (removed bool, err error) {
	switch row.Applied.Kind {
	case managedfile.KindTree:
		if err := os.RemoveAll(row.Applied.Path); err != nil {
			return false, fmt.Errorf("harness: remove tree %s: %w", row.Applied.Path, err)
		}
		return true, nil
	case managedfile.KindFile:
		if err := os.Remove(row.Applied.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("harness: remove %s: %w", row.Applied.Path, err)
		}
		return true, nil
	case managedfile.KindBlock:
		if err := removeBlock(row.Applied.Path); err != nil {
			return false, err
		}
		return true, nil
	case managedfile.KindSettings:
		// Runs after the artifact rows, since Claude's outputStyle member drops
		// only once its style file is gone; a read-only file reports skipped.
		skipped, err := hooks.RemoveOwnedMembers(row.Applied.Path)
		if err != nil {
			return false, fmt.Errorf("harness: remove settings ownership at %s: %w", row.Applied.Path, err)
		}
		return !skipped, nil
	default:
		return false, fmt.Errorf("harness: remove %s: unknown resource kind %q", row.Resource, row.Applied.Kind)
	}
}

// discardResource clears a changed resource after keeping a copy in backupDir.
// cleared false is a released claim the operator confirmed, not a failure.
func discardResource(row installstate.Row, backupDir string) (cleared, backedUp bool, err error) {
	if _, err := os.Lstat(row.Applied.Path); errors.Is(err, fs.ErrNotExist) {
		return true, false, nil
	}
	unit, err := installstate.UnitFor(row.Resource)
	if err != nil {
		return false, false, err
	}
	dest := filepath.Join(backupDir, unit)
	if row.Applied.Kind == managedfile.KindTree {
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return false, false, err
		}
		if err := managedfile.MoveTree(row.Applied.Path, dest); err != nil {
			return false, false, fmt.Errorf("harness: discard tree %s: %w", row.Applied.Path, err)
		}
		return true, true, nil
	}
	if _, err := managedfile.BackupFile(row.Applied.Path, dest, "", time.Now()); err != nil {
		return false, false, err
	}
	cleared, err = clearResource(row)
	return cleared, true, err
}

func clearResource(row installstate.Row) (cleared bool, err error) {
	switch row.Applied.Kind {
	case managedfile.KindFile:
		if err := os.Remove(row.Applied.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("harness: discard %s: %w", row.Applied.Path, err)
		}
		return true, nil
	case managedfile.KindBlock:
		if err := removeBlock(row.Applied.Path); err != nil {
			if !errors.Is(err, ErrEvidenceConflict) {
				return false, err
			}
			return false, nil
		}
		return true, nil
	case managedfile.KindSettings:
		skipped, err := hooks.RemoveOwnedMembers(row.Applied.Path)
		if err != nil {
			return false, fmt.Errorf("harness: discard settings ownership at %s: %w", row.Applied.Path, err)
		}
		return !skipped, nil
	default:
		return false, fmt.Errorf("harness: discard %s: unknown resource kind %q", row.Resource, row.Applied.Kind)
	}
}

// PruneEmptyContainers removes the directories a removed file emptied, from its
// parent up to but never including root, and returns them.
func PruneEmptyContainers(path, root string) []string {
	if root == "" || path == "" {
		return nil
	}
	var pruned []string
	for dir := filepath.Dir(path); dir != root && strictDescendant(root, dir); dir = filepath.Dir(dir) {
		// A symlink is never Atomic's container: removing one would drop the
		// user's link rather than an empty directory Atomic created.
		if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
			break
		}
		if err := os.Remove(dir); err != nil {
			break
		}
		pruned = append(pruned, dir)
	}
	return pruned
}

// strictDescendant reports whether path sits below root without escaping
// through a parent reference.
func strictDescendant(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// trimConflict renders a verifyOwned conflict as the resolution a skip line
// shows, without the sentinel error's own prefix.
func trimConflict(err error) string {
	return strings.TrimPrefix(err.Error(), ErrEvidenceConflict.Error()+": ")
}

// removeBlock strips the Atomic block from path. A whitespace-only remainder is
// removed, unless reached through a symlink, which must survive.
func removeBlock(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("harness: read %s: %w", path, err)
	}
	start, end, err := managedfile.BlockBoundaries(data)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrEvidenceConflict, path, err)
	}
	rest := make([]byte, 0, len(data)-(end-start))
	rest = append(rest, data[:start]...)
	rest = append(rest, data[end:]...)
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("harness: stat %s: %w", path, err)
	}
	if strings.TrimSpace(string(rest)) == "" && fi.Mode()&fs.ModeSymlink == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("harness: remove %s: %w", path, err)
		}
		return nil
	}
	mode := fs.FileMode(0o644)
	if target, statErr := os.Stat(path); statErr == nil {
		mode = target.Mode().Perm()
	}
	if err := managedfile.WriteFileAtomic(path, rest, mode); err != nil {
		return fmt.Errorf("harness: rewrite %s: %w", path, err)
	}
	return nil
}
