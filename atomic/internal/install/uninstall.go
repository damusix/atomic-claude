package install

import (
	"fmt"
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

// UninstallTarget removes one enrolled target's resources. A resource whose
// bytes drifted underneath Atomic is reported skipped and keeps its claim while
// the rest of the target uninstalls; resources another enrolled consumer depends
// on are retained and reported.
func (s Steps) UninstallTarget(key string) (harness.Removal, error) {
	target, err := harness.ParseKey(key)
	if err != nil {
		return harness.Removal{}, err
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
	plan, _, err := harness.PlanTargetRemoval(s.Home, target, ledger)
	if err != nil {
		return harness.Removal{}, err
	}
	ok, err := s.approve(fmt.Sprintf("Uninstall %s?", target.Key()), fmt.Sprintf("%d resource(s) removed, %d retained", len(plan.Removed), len(plan.Retained)))
	if err != nil {
		return harness.Removal{}, err
	}
	if !ok {
		return plan, fmt.Errorf("uninstall %s: declined", target.Key())
	}
	return harness.RemoveTargetResources(s.Home, target)
}

// dryRunTargetRemoval is the read-only target-removal plan. It opens no lock
// and recovers no journal: it previews unresolved journals in memory, and
// reports blocked_on_recovery rather than planning around bytes recovery could
// not reconcile. A simulable journal yields the advisory post-recovery plan,
// planned against the ledger a real recovery would leave behind.
func (s Steps) dryRunTargetRemoval(target harness.Target) (harness.Removal, error) {
	sims, ledger, blocked, err := installstate.SimulateRecoveries(s.Home)
	if err != nil {
		return harness.Removal{}, err
	}
	if blocked {
		return harness.Removal{Target: target, Recovery: sims, Blockers: []string{blockedOnRecovery}}, nil
	}
	plan, _, err := harness.PlanTargetRemoval(s.Home, target, ledger)
	if err != nil {
		return harness.Removal{}, err
	}
	plan.Recovery = sims
	return plan, nil
}

// UninstallAll removes every enrolled target, then removes the completed
// operational and adoption state. It preserves ~/.atomic/config.toml,
// profile.md, wikis.md, and backups, and retains unresolved journals plus the
// transaction backups, ledger rows, and state selections they reference until
// recovery completes.
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
	plans, _, err := s.planFullRemoval(ledger)
	if err != nil {
		return report, err
	}

	ok, err := s.approve("Uninstall Atomic from every enrolled target?", fmt.Sprintf("%d target(s); config, profile, wikis, and backups are preserved", len(plans)))
	if err != nil {
		return report, err
	}
	if !ok {
		return report, fmt.Errorf("uninstall: declined")
	}

	for _, plan := range plans {
		removal, err := harness.RemoveTargetResources(s.Home, plan.Target)
		if err != nil {
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
			if row, ok := ledger.Find(removal.Target.Key(), id); ok {
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

// dryRunUninstallAll is the read-only full-uninstall plan. It opens no lock and
// recovers no journal: it previews unresolved journals in memory and reports
// blocked_on_recovery rather than planning around bytes recovery could not
// reconcile. A simulable journal yields the advisory post-recovery plan, planned
// against the ledger a real recovery would leave behind.
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
	plans, retention, err := s.planFullRemoval(ledger)
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

// planFullRemoval is the read-only full-uninstall plan: every enrolled target in
// the supplied ledger view — the enrolled ledger on disk, or the dry run's
// simulated post-recovery ledger — and the operational state unresolved journals
// still own on disk. Every target is planned before any is removed, and the plan
// writes nothing.
func (s Steps) planFullRemoval(ledger *installstate.Ledger) ([]harness.Removal, installstate.Retention, error) {
	records := append([]installstate.TargetRecord(nil), ledger.Targets...)
	plans := make([]harness.Removal, 0, len(records))
	for _, record := range records {
		target := harness.Target{Kind: harness.Kind(record.Harness), Instance: record.Instance, NativeRoot: record.NativeRoot}
		plan, _, err := harness.PlanTargetRemoval(s.Home, target, ledger)
		if err != nil {
			return nil, installstate.Retention{}, err
		}
		plans = append(plans, plan)
	}
	retention, err := s.removalRetention()
	if err != nil {
		return nil, installstate.Retention{}, err
	}
	return plans, retention, nil
}

// lock acquires the one advisory lifecycle lock.
func (s Steps) lock(operationID string) (*installstate.Lock, error) {
	if s.OperationID != "" {
		operationID = s.OperationID
	}
	return installstate.AcquireLock(s.Home, installstate.WriterIdentity{OperationID: operationID})
}

// recoverRetaining reconciles unresolved journals oldest-first. Uninstall is the
// one lifecycle operation that proceeds past a conflict: recovery consumes what
// it can verify, the removal itself refuses any resource whose bytes changed,
// and the cleanup retains everything still unresolved for a later recovery.
func (s Steps) recoverRetaining() error {
	_, err := installstate.RecoverJournals(s.Home)
	return err
}

// Recover is the explicit `atomic harness recover` operation: it acquires the
// one advisory lifecycle lock and reconciles every unresolved journal
// oldest-first, rolling forward by default or restoring digest-verified
// transaction backups under rollback. A journal that cannot be reconciled to one
// safe result leaves its conflict in the returned actions; the journal survives
// so a later run can finish the job.
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

// PreviewRecovery is the read-only counterpart of Recover: it opens no lock and
// neutralizes every write seam, simulating each unresolved journal in memory so a
// dry run can report what a real recovery would do — rolling forward, or
// restoring transaction backups when rollback is set. blocked reports that a
// journal cannot be reconciled to one safe result.
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
