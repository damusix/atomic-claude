package harness

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// ErrNotEnrolled reports a target the ledger does not record.
var ErrNotEnrolled = errors.New("harness: target is not enrolled")

// PlanTargetRemoval is the read-only removal plan for one enrolled target
// against a ledger view: the enrolled ledger on disk for a real removal, or the
// dry run's simulated post-recovery ledger. Every removable resource's bytes are
// verified against the digest the ledger recorded writing; a resource that
// changed underneath Atomic refuses the plan, so nothing is half-removed around
// a conflict the user must resolve.
func PlanTargetRemoval(home string, t Target, ledger *installstate.Ledger) (Removal, map[string]installstate.Row, error) {
	out := Removal{Target: t}
	if home == "" {
		return out, nil, fmt.Errorf("harness: uninstall %s: no home", t.Key())
	}
	if t.Instance == "" {
		return out, nil, fmt.Errorf("harness: uninstall: instance identity is empty")
	}

	if _, ok := ledger.FindTarget(string(t.Kind), t.Instance); !ok {
		hasRow := false
		for _, row := range ledger.Rows {
			if row.Target == t.Key() {
				hasRow = true
				break
			}
		}
		if !hasRow {
			return out, nil, fmt.Errorf("harness: uninstall %s: %w", t.Key(), ErrNotEnrolled)
		}
	}

	type owned struct {
		row       installstate.Row
		consumers []string
	}
	byResource := map[string]*owned{}
	var order []string
	for _, row := range ledger.Rows {
		if row.Target != t.Key() {
			continue
		}
		o, ok := byResource[row.Resource]
		if !ok {
			o = &owned{row: row}
			byResource[row.Resource] = o
			order = append(order, row.Resource)
		}
		for _, other := range ledger.Rows {
			if other.Resource == row.Resource && other.Target != t.Key() {
				o.consumers = appendUnique(o.consumers, other.Target)
			}
		}
	}

	removable := map[string]installstate.Row{}
	for _, id := range order {
		o := byResource[id]
		if len(o.consumers) > 0 {
			out.Retained = append(out.Retained, id)
			continue
		}
		if err := verifyOwned(o.row); err != nil {
			return out, nil, err
		}
		out.Removed = append(out.Removed, id)
		removable[id] = o.row
	}
	sort.Strings(out.Removed)
	sort.Strings(out.Retained)
	return out, removable, nil
}

// RemoveTargetResources applies the verified removal plan: it deletes only the
// resources one enrolled target solely consumes, retains every resource another
// enrolled consumer still depends on — visibility by an unenrolled instance is
// not a dependency and never blocks removal — and then commits the ledger.
//
// The caller serializes this against other lifecycle operations by holding the
// advisory lock at ~/.atomic/install/operation.lock.
func RemoveTargetResources(home string, t Target) (Removal, error) {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return Removal{Target: t}, err
	}
	out, removable, err := PlanTargetRemoval(home, t, ledger)
	if err != nil {
		return out, err
	}

	// Every removable resource verified before the first deletion, so a conflict
	// in the last resource cannot leave the earlier ones half-removed.
	for _, id := range out.Removed {
		if err := removeResource(removable[id]); err != nil {
			return out, err
		}
	}

	kept := ledger.Rows[:0:0]
	for _, row := range ledger.Rows {
		if row.Target == t.Key() {
			continue
		}
		kept = append(kept, row)
	}
	ledger.Rows = kept
	keptTargets := ledger.Targets[:0:0]
	for _, record := range ledger.Targets {
		if record.Key() == t.Key() {
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

// verifyOwned proves a resource still holds exactly what Atomic recorded
// writing. Missing bytes are removable: the resource is already gone, and the
// ledger row is the stale claim.
func verifyOwned(row installstate.Row) error {
	if row.Applied.Digest == "" {
		return fmt.Errorf("%w: %s carries no recorded digest, so its bytes cannot be verified", ErrEvidenceConflict, row.Resource)
	}
	obs, err := managedfile.Observe(row.Applied.Path, row.Applied.Kind)
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
func removeResource(row installstate.Row) error {
	switch row.Applied.Kind {
	case managedfile.KindTree:
		if err := os.RemoveAll(row.Applied.Path); err != nil {
			return fmt.Errorf("harness: remove tree %s: %w", row.Applied.Path, err)
		}
		return nil
	case managedfile.KindFile:
		if err := os.Remove(row.Applied.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("harness: remove %s: %w", row.Applied.Path, err)
		}
		return nil
	case managedfile.KindBlock:
		return removeBlock(row.Applied.Path)
	default:
		return fmt.Errorf("harness: remove %s: unknown resource kind %q", row.Resource, row.Applied.Kind)
	}
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
