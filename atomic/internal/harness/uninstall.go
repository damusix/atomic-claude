package harness

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// ErrNotEnrolled reports a target the ledger does not record.
var ErrNotEnrolled = errors.New("harness: target is not enrolled")

// RemovalOptions tunes a removal beyond the plan's default refusal to touch
// bytes that no longer verify. The zero value is the default plan: changed
// bytes are skipped and keep their claim.
type RemovalOptions struct {
	// DiscardChanged releases Atomic's claim on every resource the default plan
	// skipped, after Confirm approves that resource by id. A file or tree is
	// deleted; a managed block is stripped, or left whole with its claim
	// released when its tags no longer parse; a settings file Atomic cannot
	// write keeps the user's bytes while its registration claim is dropped.
	DiscardChanged bool
	// Confirm is asked once per skipped resource before anything is discarded,
	// so a batch never removes bytes the operator did not see named. It is
	// required when DiscardChanged is set.
	Confirm func(resource, reason string) (bool, error)
}

// PlanTargetRemoval is the read-only removal plan for one enrolled target
// against a ledger view: the enrolled ledger on disk for a real removal, or the
// dry run's simulated post-recovery ledger. Every removable resource's bytes are
// verified against the digest the ledger recorded writing; a resource that
// changed underneath Atomic — ordinary drift on a resource Atomic owns, or a
// malformed block — is reported skipped and keeps its claim rather than
// aborting the removal, so the rest of the target still uninstalls.
func PlanTargetRemoval(home string, t Target, ledger *installstate.Ledger) (Removal, map[string]installstate.Row, error) {
	out, rows, err := planTargetRemoval(home, t.Key(), ledger)
	out.Target = t
	return out, rows, err
}

// PlanTargetRowRemoval is PlanTargetRemoval for a raw ledger target key. A key
// that does not parse into a target still names rows — a stale record an older
// writer left — and the removal must be clearable by the key it is recorded
// under, which is the only identity such a row has.
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
		for _, other := range ledger.Rows {
			if other.Resource == row.Resource && other.Target != key {
				o.consumers = appendUnique(o.consumers, other.Target)
			}
		}
	}

	for _, id := range order {
		o := byResource[id]
		if len(o.consumers) > 0 {
			out.Retained = append(out.Retained, id)
			delete(rows, id)
			continue
		}
		if err := verifyOwned(o.row); err != nil {
			if errors.Is(err, ErrEvidenceConflict) {
				// Ordinary drift on an owned resource is not a reason to abort
				// the whole removal: the resource is skipped and keeps its
				// claim, so the rest of the target still uninstalls and a later
				// repair (or a confirmed discard) can finish the job.
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
	return out, rows, nil
}

// RemoveTargetResources applies the verified removal plan: it deletes only the
// resources one enrolled target solely consumes, retains every resource another
// enrolled consumer still depends on — visibility by an unenrolled instance is
// not a dependency and never blocks removal — and then commits the ledger.
//
// A resource the plan skipped keeps its claim unless opts approve discarding it,
// in which case its bytes are removed (or its claim released) after a
// per-resource confirmation.
//
// The caller serializes this against other lifecycle operations by holding the
// advisory lock at ~/.atomic/install/operation.lock.
func RemoveTargetResources(home string, t Target, opts ...RemovalOptions) (Removal, error) {
	return removeTargetResources(home, t.Key(), t, opts...)
}

// RemoveTargetRows removes the ledger rows a raw target key names, for a key
// that does not parse into a target — a stale record an older writer wrote,
// which nothing else can name. It verifies and removes bytes exactly as
// RemoveTargetResources does; only the identity it matches differs.
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
			ok, err := opt.Confirm(id, out.SkipReasons[id])
			if err != nil {
				return out, err
			}
			if ok {
				discard[id] = true
			}
		}
	}

	// Every removable resource verified before the first deletion, so a conflict
	// in the last resource cannot leave the earlier ones half-removed. Settings
	// rows go last: their outputStyle member is dropped only once the style file
	// it names is gone, and that file is another row in this same plan.
	toRemove := make([]string, 0, len(out.Removed)+len(discard))
	toRemove = append(toRemove, out.Removed...)
	for id := range discard {
		if !contains(out.Removed, id) {
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

	// Resources the plan already skipped keep their claim for a later removal;
	// they are seeded here so the occupancy reconciliation below never drops a
	// row whose bytes still hold Atomic content.
	skipped := map[string]bool{}
	for _, id := range out.Skipped {
		skipped[id] = true
	}
	var discarded []string
	var pruned []string
	for _, id := range ordered {
		row := owned[id]
		if discard[id] {
			cleared, err := discardResource(row)
			if err != nil {
				return out, err
			}
			delete(skipped, id)
			discarded = append(discarded, id)
			if !cleared {
				out.NoteSkip(id, "Atomic's bytes could not be removed, so its claim was released with the bytes left in place")
			}
			pruned = append(pruned, pruneEmptyContainer(row.Applied.Path, t.NativeRoot)...)
			continue
		}
		removed, err := removeResource(row)
		if err != nil {
			return out, err
		}
		if !removed {
			skipped[id] = true
			out.NoteSkip(id, "Atomic could not write the resource")
			continue
		}
		pruned = append(pruned, pruneEmptyContainer(row.Applied.Path, t.NativeRoot)...)
	}
	sort.Strings(discarded)
	sort.Strings(pruned)
	out.Discarded = discarded
	out.Pruned = pruned
	// A resource removal could not clear still holds Atomic's bytes, so its
	// claim is not spent: report it skipped, drop it from Removed, and keep its
	// ledger row and enrollment for a later uninstall. A discarded resource's
	// claim is released whether or not its bytes could be removed.
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
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		return out, err
	}
	return out, nil
}

// ledgerTargetByKey returns the enrolled target record a raw key names, matched
// literally so a key that does not parse still resolves to the record it came
// from.
func ledgerTargetByKey(ledger *installstate.Ledger, key string) (installstate.TargetRecord, bool) {
	for _, record := range ledger.Targets {
		if record.Key() == key {
			return record, true
		}
	}
	return installstate.TargetRecord{}, false
}

// verifyOwned proves a resource still holds exactly what Atomic recorded
// writing. Missing bytes are removable: the resource is already gone, and the
// ledger row is the stale claim.
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

// removeResource deletes one verified resource. A managed block removes only
// its own bytes: the user's prose outside the block survives, and the file is
// deleted only when nothing but whitespace remains.
//
// removed reports whether the resource was actually cleared. A read-only
// settings file the removal cannot write reports false, so the caller keeps the
// claim the bytes still support instead of dropping it.
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
		// The owned members are the SessionStart registration and the
		// outputStyle seed. hooks.UninstallInDir removes the registration and
		// drops outputStyle only once the style file it names is gone, so this
		// runs after the artifact rows (see RemoveTargetResources). A read-only
		// settings file is reported skipped rather than clobbered.
		skipped, err := hooks.UninstallInDir(filepath.Dir(row.Applied.Path))
		if err != nil {
			return false, fmt.Errorf("harness: remove settings ownership at %s: %w", row.Applied.Path, err)
		}
		return !skipped, nil
	default:
		return false, fmt.Errorf("harness: remove %s: unknown resource kind %q", row.Resource, row.Applied.Kind)
	}
}

// discardResource clears a resource the default plan refused because its bytes
// no longer verify. A file or tree is deleted; a managed block is stripped, and
// a block whose tags no longer parse is left whole rather than deleting prose
// Atomic cannot isolate. A settings file Atomic cannot write keeps the user's
// bytes.
//
// cleared reports whether the bytes are gone. false is a released claim, not a
// failure: the operator confirmed the discard knowing Atomic would give up the
// resource.
func discardResource(row installstate.Row) (cleared bool, err error) {
	switch row.Applied.Kind {
	case managedfile.KindTree:
		if err := os.RemoveAll(row.Applied.Path); err != nil {
			return false, fmt.Errorf("harness: discard tree %s: %w", row.Applied.Path, err)
		}
		return true, nil
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
		skipped, err := hooks.UninstallInDir(filepath.Dir(row.Applied.Path))
		if err != nil {
			return false, fmt.Errorf("harness: discard settings ownership at %s: %w", row.Applied.Path, err)
		}
		return !skipped, nil
	default:
		return false, fmt.Errorf("harness: discard %s: unknown resource kind %q", row.Resource, row.Applied.Kind)
	}
}

// pruneEmptyContainer removes the directory a removed file resource emptied: the
// native directory Atomic created to hold it, bounded by root. A directory that
// still holds anything, and the target root itself, always survives; a tree
// resource already removes its own root. It returns the removed directory, if
// any, so the removal can report what it cleaned up.
func pruneEmptyContainer(path, root string) []string {
	if root == "" || path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == root || !strictDescendant(root, dir) {
		return nil
	}
	// A symlink is never Atomic's container: removing one would drop the user's
	// link rather than an empty directory Atomic created.
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return nil
	}
	if err := os.Remove(dir); err != nil {
		return nil
	}
	return []string{dir}
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

// removeBlock strips the single Atomic block from path, preserving every byte
// outside it. A file left with only whitespace is removed.
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
	if strings.TrimSpace(string(rest)) == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("harness: remove %s: %w", path, err)
		}
		return nil
	}
	mode := fs.FileMode(0o644)
	if fi, statErr := os.Stat(path); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := managedfile.WriteFileAtomic(path, rest, mode); err != nil {
		return fmt.Errorf("harness: rewrite %s: %w", path, err)
	}
	return nil
}
