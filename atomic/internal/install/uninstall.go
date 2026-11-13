package install

import (
	"fmt"
	"slices"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// FullUninstallReport is what a full uninstall removed and what it kept.
type FullUninstallReport struct {
	Targets []harness.Removal          `json:"targets,omitempty"`
	Cleanup installstate.CleanupResult `json:"cleanup"`
	// Retention names the operational state preserved because an unresolved
	// journal still references it.
	Retention installstate.Retention `json:"retention"`
	// Recovery previews unresolved journals in memory. A real uninstall has
	// already reconciled them, so only a dry run populates it.
	Recovery []installstate.RecoverySimulation `json:"recovery,omitempty"`
	// Blockers names the observations that forbid a decidable plan.
	Blockers []string `json:"blockers,omitempty"`
}

// blockedOnRecovery is the report a dry run gives when an unresolved journal
// cannot be reconciled to one safe result: no plan can be offered yet.
const blockedOnRecovery = string(installstate.StatusBlockedOnRecovery) +
	": an unresolved journal cannot be recovered to one safe result without an unavailable observation or a later edit"

// UninstallTarget removes one enrolled target's resources. An unparseable key is
// removed by its raw ledger key, so an older writer's stale row stays removable.
func (s Steps) UninstallTarget(key string) (harness.Removal, error) {
	target, err := harness.ParseKey(key)
	if err != nil {
		return s.uninstallRawKey(key)
	}
	if s.DryRun {
		return s.dryRunTargetRemoval(target)
	}
	lock, err := s.lock("uninstall-" + safeSegment(key))
	if err != nil {
		return harness.Removal{}, err
	}
	defer lock.Release()
	if err := s.recoverRetaining(); err != nil {
		return harness.Removal{}, err
	}

	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return harness.Removal{}, err
	}
	plan, rows, err := harness.PlanTargetRemoval(s.Home, target, ledger)
	if err != nil {
		return harness.Removal{}, err
	}
	s.printDiscards(s.planDiscards(&plan, rows))
	ok, err := s.approve(fmt.Sprintf("Uninstall %s?", target.Key()), fmt.Sprintf("%d resource(s) removed, %d retained, %d left", len(plan.Removed), len(plan.Retained), len(plan.Left)))
	if err != nil {
		return harness.Removal{}, err
	}
	if !ok {
		return plan, fmt.Errorf("uninstall %s: declined", target.Key())
	}
	removal, err := harness.RemoveTargetResources(s.Home, target, s.removalOptions(plan.Skipped))
	if err != nil {
		return removal, err
	}
	return s.removed(target, removal)
}

// removed runs the adapter's post-removal step. Removal itself is ledger-driven,
// so a binary without the kind's adapter still uninstalls and skips this step.
func (s Steps) removed(t harness.Target, removal harness.Removal) (harness.Removal, error) {
	reg, err := s.registry()
	if err != nil {
		return removal, err
	}
	adapter, err := reg.Adapter(t.Kind)
	if err != nil {
		return removal, nil
	}
	return adapter.Lifecycle(s.Home).Removed(t, removal)
}

// uninstallRawKey removes the ledger rows an unparseable target key names,
// without requiring a binary that can spell the kind.
func (s Steps) uninstallRawKey(key string) (harness.Removal, error) {
	if key == "" {
		return harness.Removal{}, fmt.Errorf("harness: uninstall: target key is empty")
	}
	if s.DryRun {
		return s.dryRunRowRemoval(key)
	}
	lock, err := s.lock("uninstall-" + safeSegment(key))
	if err != nil {
		return harness.Removal{}, err
	}
	defer lock.Release()
	if err := s.recoverRetaining(); err != nil {
		return harness.Removal{}, err
	}
	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return harness.Removal{}, err
	}
	plan, rows, err := harness.PlanTargetRowRemoval(s.Home, key, ledger)
	if err != nil {
		return harness.Removal{}, err
	}
	s.printDiscards(s.planDiscards(&plan, rows))
	ok, err := s.approve(fmt.Sprintf("Uninstall %s?", key), fmt.Sprintf("%d resource(s) removed, %d retained, %d left", len(plan.Removed), len(plan.Retained), len(plan.Left)))
	if err != nil {
		return harness.Removal{}, err
	}
	if !ok {
		return plan, fmt.Errorf("uninstall %s: declined", key)
	}
	return harness.RemoveTargetRows(s.Home, key, s.removalOptions(plan.Skipped))
}

// removalOptions carries the discard choice, the per-resource confirmation, and
// the printed discard set the removal may not exceed.
func (s Steps) removalOptions(approved []string) harness.RemovalOptions {
	return harness.RemovalOptions{
		DiscardChanged: s.DiscardChanged,
		Confirm: func(resource, reason string) (bool, error) {
			return s.approve(fmt.Sprintf("Discard changed resource %s?", resource), reason)
		},
		Approved: approved,
	}
}

func (s Steps) planDiscards(plan *harness.Removal, rows map[string]installstate.Row) []string {
	if !s.DiscardChanged {
		return nil
	}
	var lines []string
	for _, id := range plan.Skipped {
		lines = append(lines, fmt.Sprintf("discards %s at %s (changed after Atomic wrote it); a recovery copy is kept under ~/.atomic/backups/", id, rows[id].Applied.Path))
	}
	plan.Notices = append(plan.Notices, lines...)
	return lines
}

// printDiscards shows the discard plan before anything is applied: under --yes
// it is the plan the caller approved.
func (s Steps) printDiscards(lines []string) {
	if s.Notice == nil {
		return
	}
	for _, line := range lines {
		fmt.Fprintf(s.Notice, "notice\t%s\n", line)
	}
}

// dryRunRowRemoval is dryRunTargetRemoval for a raw ledger key.
func (s Steps) dryRunRowRemoval(key string) (harness.Removal, error) {
	sims, ledger, blocked, err := installstate.SimulateRecoveries(s.Home)
	if err != nil {
		return harness.Removal{}, err
	}
	if blocked {
		return harness.Removal{TargetKey: key, Recovery: sims, Blockers: []string{blockedOnRecovery}}, nil
	}
	plan, rows, err := harness.PlanTargetRowRemoval(s.Home, key, ledger)
	if err != nil {
		return harness.Removal{}, err
	}
	s.planDiscards(&plan, rows)
	plan.Recovery = sims
	return plan, nil
}

// dryRunTargetRemoval is the read-only target-removal plan, made against the
// ledger a simulated recovery would leave behind.
func (s Steps) dryRunTargetRemoval(target harness.Target) (harness.Removal, error) {
	sims, ledger, blocked, err := installstate.SimulateRecoveries(s.Home)
	if err != nil {
		return harness.Removal{}, err
	}
	if blocked {
		return harness.Removal{Target: target, TargetKey: target.Key(), Recovery: sims, Blockers: []string{blockedOnRecovery}}, nil
	}
	plan, rows, err := harness.PlanTargetRemoval(s.Home, target, ledger)
	if err != nil {
		return harness.Removal{}, err
	}
	s.planDiscards(&plan, rows)
	plan.Recovery = sims
	return plan, nil
}

// UninstallAll removes every enrolled target and completed state, keeping user
// files and anything an unresolved journal still references.
func (s Steps) UninstallAll() (FullUninstallReport, error) {
	var report FullUninstallReport
	if s.DryRun {
		return s.dryRunUninstallAll()
	}
	lock, err := s.lock("uninstall-all")
	if err != nil {
		return report, err
	}
	defer lock.Release()
	if err := s.recoverRetaining(); err != nil {
		return report, err
	}

	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return report, err
	}
	plans, _, discards, err := s.planFullRemoval(ledger)
	if err != nil {
		return report, err
	}
	s.printDiscards(discards)

	ok, err := s.approve("Uninstall Atomic from every enrolled target?", fmt.Sprintf("%d target(s); config, profile, wikis, and backups are preserved", len(plans)))
	if err != nil {
		return report, err
	}
	if !ok {
		return report, fmt.Errorf("uninstall: declined")
	}
	if err := s.checkDiscardPlans(plans); err != nil {
		return report, err
	}

	for _, plan := range plans {
		removal, err := harness.RemoveTargetResources(s.Home, plan.Target, s.removalOptions(plan.Skipped))
		if err != nil {
			return report, err
		}
		if removal, err = s.removed(plan.Target, removal); err != nil {
			return report, err
		}
		report.Targets = append(report.Targets, removal)
	}

	// The removals committed their own ledger writes, so a full uninstall must
	// re-read before cleanup prunes the rows and targets it just removed.
	ledger, err = installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return report, err
	}
	// A resource a removal could not clear — a read-only settings file — is not
	// completed state: keep its row so a later uninstall can finish the job.
	var uncleared []installstate.Row
	for _, removal := range report.Targets {
		for _, id := range removal.Skipped {
			if row, ok := ledger.Find(removal.TargetKey, id); ok {
				uncleared = append(uncleared, row)
			}
		}
	}
	report.Cleanup, err = installstate.Cleanup(s.Home, ledger, s.now(), uncleared...)
	if err != nil {
		return report, err
	}
	if len(report.Cleanup.RemovedRows) > 0 || len(report.Cleanup.RemovedTargets) > 0 {
		if err := ledger.Save(ledgerPath(s.Home)); err != nil {
			return report, err
		}
	}
	report.Retention, err = installstate.ComputeRetention(journalsDir(s.Home), ledger)
	return report, err
}

// dryRunUninstallAll is the read-only full-uninstall plan, made against the
// ledger a simulated recovery would leave behind.
func (s Steps) dryRunUninstallAll() (FullUninstallReport, error) {
	var report FullUninstallReport
	sims, ledger, blocked, err := installstate.SimulateRecoveries(s.Home)
	if err != nil {
		return report, err
	}
	report.Recovery = sims
	if blocked {
		report.Blockers = []string{blockedOnRecovery}
		report.Retention, err = s.removalRetention()
		return report, err
	}
	plans, retention, _, err := s.planFullRemoval(ledger)
	if err != nil {
		return report, err
	}
	report.Targets = plans
	report.Retention = retention
	return report, nil
}

// removalRetention is the operational state unresolved journals still own.
func (s Steps) removalRetention() (installstate.Retention, error) {
	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return installstate.Retention{}, err
	}
	return installstate.ComputeRetention(journalsDir(s.Home), ledger)
}

// planFullRemoval plans every enrolled target in the supplied ledger view before
// any is removed, writing nothing.
func (s Steps) planFullRemoval(ledger *installstate.Ledger) ([]harness.Removal, installstate.Retention, []string, error) {
	records := append([]installstate.TargetRecord(nil), ledger.Targets...)
	plans := make([]harness.Removal, 0, len(records))
	var discards []string
	for _, record := range records {
		target := harness.Target{Kind: harness.Kind(record.Harness), Instance: record.Instance, NativeRoot: record.NativeRoot}
		plan, rows, err := harness.PlanTargetRemoval(s.Home, target, ledger)
		if err != nil {
			return nil, installstate.Retention{}, nil, err
		}
		discards = append(discards, s.planDiscards(&plan, rows)...)
		plans = append(plans, plan)
	}
	retention, err := s.removalRetention()
	if err != nil {
		return nil, installstate.Retention{}, nil, err
	}
	return plans, retention, discards, nil
}

// checkDiscardPlans re-plans every target before the first removal, because a
// removal commits its target, so a later target's refusal would come too late.
func (s Steps) checkDiscardPlans(plans []harness.Removal) error {
	if !s.DiscardChanged {
		return nil
	}
	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return err
	}
	for _, printed := range plans {
		fresh, _, err := harness.PlanTargetRemoval(s.Home, printed.Target, ledger)
		if err != nil {
			return err
		}
		for _, id := range fresh.Skipped {
			if !slices.Contains(printed.Skipped, id) {
				return fmt.Errorf("uninstall %s: %s changed after the discard plan was printed; nothing was removed, re-run to review the new plan", printed.Target.Key(), id)
			}
		}
	}
	return nil
}

// lock acquires the one advisory lifecycle lock.
func (s Steps) lock(operationID string) (*installstate.Lock, error) {
	if s.OperationID != "" {
		operationID = s.OperationID
	}
	return installstate.AcquireLock(s.Home, installstate.WriterIdentity{OperationID: operationID})
}

// recoverRetaining reconciles unresolved journals. Uninstall alone proceeds past
// a conflict, because removal refuses changed bytes and cleanup retains the rest.
func (s Steps) recoverRetaining() error {
	_, err := installstate.RecoverJournals(s.Home)
	return err
}

// Recover is `atomic harness recover`: it reconciles every unresolved journal
// under the lifecycle lock, rolling forward or, with rollback, restoring backups.
func (s Steps) Recover(rollback bool) ([]installstate.RecoveryAction, error) {
	lock, err := s.lock("recover")
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	if rollback {
		return installstate.RollbackJournals(s.Home)
	}
	return installstate.RecoverJournals(s.Home)
}

// PreviewRecovery is the read-only counterpart of Recover, simulated in memory.
// blocked reports a journal that cannot be reconciled to one safe result.
func (s Steps) PreviewRecovery(rollback bool) ([]installstate.RecoverySimulation, bool, error) {
	if rollback {
		return installstate.SimulateRollbacks(s.Home)
	}
	sims, _, blocked, err := installstate.SimulateRecoveries(s.Home)
	return sims, blocked, err
}

// now is the step clock, defaulted so a zero Steps value still works.
func (s Steps) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now()
}

// safeSegment maps a target key to a single safe operation-id segment. The key
// already separates its parts with a colon, which an operation id rejects.
func safeSegment(key string) string {
	out := make([]byte, 0, len(key))
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
			out = append(out, c)
		default:
			out = append(out, '.')
		}
	}
	return string(out)
}
