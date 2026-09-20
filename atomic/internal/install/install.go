// Package install orchestrates the generic multi-harness lifecycle the atomic
// CLI exposes: discovery, explicit enrollment, adoption, repair, status, diff,
// rules, and target or full uninstall. It owns the command-level ordering —
// discover, plan, approve, converge, report — while each harness adapter keeps
// its own lock, recovery, staging, and ledger commit.
//
// Discovery never enrolls. Convergence happens through the adapter, which
// acquires the one advisory lifecycle lock and recovers unresolved journals
// before it mutates; uninstall acquires that lock here. A dry run opens no lock
// and writes nothing, so the filesystem is byte-identical when it returns.
package install

import (
	"errors"
	"fmt"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
)

// Steps carries the CLI's ordering seams: where atomic state lives, whether the
// caller pre-approved the plan, and the collaborators a test replaces.
// Production wiring is DefaultSteps.
type Steps struct {
	Home      string
	DryRun    bool
	AssumeYes bool
	// BatchDecision is the caller's replace-or-leave-unowned choice for
	// resources the selected generation cannot prove ownership of.
	BatchDecision installstate.Decision
	Registry      func(home string) (*harness.Registry, error)
	Confirm       func(title, desc string, def bool) (bool, error)
	Now           func() time.Time
	OperationID   string
}

// DefaultSteps wires the production collaborators. Registry is the composition
// root: the adapter set is a build-time decision, so this package is the one
// place that names the concrete harnesses.
func DefaultSteps(home string) Steps {
	return Steps{
		Home:     home,
		Registry: DefaultRegistry,
		Confirm:  prompt.Confirm,
		Now:      func() time.Time { return time.Now().UTC() },
	}
}

// DefaultRegistry builds the adapter set this binary ships. The home argument
// keeps the seam's shape; an adapter resolves its instances per call.
//
// Codex is registered like any other harness, but its native root is the
// CODEX_HOME variable CP0 observed: with the variable unset the adapter refuses
// discovery (ErrUnsupported), so a broad walk reports the harnesses it can
// resolve and `--harness codex` reports the refusal by name.
func DefaultRegistry(string) (*harness.Registry, error) {
	return harness.NewRegistry(claude.New(), omp.New(), codex.New())
}

// Selection names which discovered instances a verb operates on.
type Selection struct {
	// Kind, when set, restricts the operation to one harness.
	Kind harness.Kind
	// Instance names one native root or instance ID. Empty means "the only
	// match", which an ambiguous set refuses rather than guesses at.
	Instance string
	// All selects every discovered instance of every kind.
	All bool
	// EnrolledOnly resolves from the ledger's enrolled targets instead of
	// discovery, so repair and rules sync never touch an instance the user has
	// not enrolled.
	EnrolledOnly bool
}

// Resolve discovers instances and returns the ones sel names, refusing an
// ambiguous selection instead of writing into the wrong native root.
func (s Steps) Resolve(sel Selection) ([]harness.Instance, error) {
	reg, err := s.registry()
	if err != nil {
		return nil, err
	}
	if sel.EnrolledOnly {
		return s.enrolledInstances(sel)
	}
	if sel.All {
		if sel.Kind != "" || sel.Instance != "" {
			return nil, fmt.Errorf("--all cannot be combined with --harness or --instance: a harness you name explicitly must never be silently dropped")
		}
		return reg.Discover(s.Home)
	}
	if sel.Kind == "" {
		return nil, fmt.Errorf("select a harness with --harness <claude|omp|codex> or pass --all, then retry")
	}
	inst, err := reg.Select(s.Home, harness.Selector{Kind: sel.Kind, Instance: sel.Instance})
	if err != nil {
		return nil, err
	}
	return []harness.Instance{inst}, nil
}

// ConvergeRequest is one mutating convergence: install, enroll, repair, or the
// enrolled-target half of rules sync.
type ConvergeRequest struct {
	Selection Selection
	// Enroll records the target's enrollment in the same operation. It is true
	// for install and enroll, and false for repair, which operates on
	// already-enrolled targets only.
	Enroll bool
}

// ConvergeReport is one target's convergence outcome. Blockers are the
// observations that forbade mutation; Applied reports native bytes written.
type ConvergeReport struct {
	Target     harness.Target `json:"target"`
	Status     harness.Status `json:"status"`
	Generation string         `json:"generation,omitempty"`
	Applied    bool           `json:"applied"`
	Blockers   []string       `json:"blockers,omitempty"`
}

// Converge executes the ordering one mutating verb follows per target: project
// read-only, refuse a blocked plan, honor an already-converged target as a
// no-op, obtain explicit consent, then converge through the adapter.
//
// The adapter acquires the lifecycle lock and recovers unresolved journals
// itself; the read-only projection above it is re-observed under that lock, so
// a plan that moved on between projection and convergence refuses rather than
// writing around the change.
func (s Steps) Converge(req ConvergeRequest) ([]ConvergeReport, error) {
	instances, err := s.Resolve(req.Selection)
	if err != nil {
		return nil, err
	}
	reg, err := s.registry()
	if err != nil {
		return nil, err
	}
	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return nil, err
	}

	var reports []ConvergeReport
	for _, inst := range instances {
		target := inst.Target()
		report := ConvergeReport{Target: target}

		if !req.Enroll {
			if !enrolled(ledger, target) {
				return reports, fmt.Errorf("%s is not enrolled; run `atomic harness enroll %s --instance %s` first",
					target.Key(), target.Kind, inst.NativeRoot)
			}
		}

		adapter, err := reg.Adapter(inst.Kind)
		if err != nil {
			return reports, err
		}
		plan, err := adapter.Lifecycle(s.Home).Project(target, harness.PlanRequest{BatchDecision: s.BatchDecision})
		if err != nil {
			if errors.Is(err, harness.ErrUnsupported) {
				report.Status = harness.StatusStale
				report.Blockers = []string{err.Error()}
				reports = append(reports, report)
				continue
			}
			return reports, err
		}
		report.Generation = plan.Generation
		if len(plan.Blockers) > 0 {
			report.Status = harness.StatusConflicted
			report.Blockers = plan.Blockers
			reports = append(reports, report)
			continue
		}
		if plan.Converged {
			// An already-current target still converges: the adapter's no-op path
			// is where the enrollment record is written, and consent to enroll was
			// given by naming the target.
			if s.DryRun {
				report.Status = harness.StatusConverged
				reports = append(reports, report)
				continue
			}
			convergence, err := adapter.Lifecycle(s.Home).Converge(target, plan)
			report.Status = convergence.Status
			if err != nil {
				return reports, err
			}
			reports = append(reports, report)
			continue
		}

		if s.DryRun {
			report.Status = harness.StatusEnrolled
			reports = append(reports, report)
			continue
		}

		ok, err := s.approve(fmt.Sprintf("Converge %s?", target.Key()), planSummary(plan))
		if err != nil {
			return reports, err
		}
		if !ok {
			report.Status = harness.StatusStale
			report.Blockers = []string{"declined"}
			reports = append(reports, report)
			continue
		}

		convergence, err := adapter.Lifecycle(s.Home).Converge(target, plan)
		report.Status = convergence.Status
		report.Applied = err == nil
		if err != nil {
			return reports, err
		}
		reports = append(reports, report)
	}
	return reports, nil
}

// ConvergeEnrolled converges every enrolled target, and is a no-op when none is
// enrolled. Update runs it after binary selection; unlike repair it must not
// fail an unenrolled home.
func (s Steps) ConvergeEnrolled() ([]ConvergeReport, error) {
	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return nil, err
	}
	if len(ledger.Targets) == 0 {
		return nil, nil
	}
	return s.Converge(ConvergeRequest{Selection: Selection{EnrolledOnly: true}})
}

// AdoptRequest is the explicit legacy adoption of one Claude target.
type AdoptRequest struct {
	Selection Selection
	// AcknowledgeSnapshot accepts an absent or corrupt legacy snapshot, whose
	// historical restoration evidence cannot be verified.
	AcknowledgeSnapshot bool
}

// Adopt migrates one verified legacy Claude installation through the CP3
// migration engine. It is the explicit `harness adopt claude` flow: the
// classification, per-resource decisions, and blockers come from the same
// planner a dry run reports.
func (s Steps) Adopt(req AdoptRequest) ([]ConvergeReport, error) {
	sel := req.Selection
	if sel.Kind == "" {
		sel.Kind = harness.KindClaude
	}
	instances, err := s.Resolve(sel)
	if err != nil {
		return nil, err
	}
	var reports []ConvergeReport
	for _, inst := range instances {
		if inst.Kind != harness.KindClaude {
			return reports, fmt.Errorf("adopt: %s is not a claude instance", inst.Kind)
		}
		target := inst.Target()
		plan, err := claude.PlanMigration(claude.MigrationRequest{
			Home:                s.Home,
			NativeRoot:          inst.NativeRoot,
			Target:              target.Key(),
			Generation:          claude.Generation(),
			Tier:                string(claude.Tier),
			BatchDecision:       s.BatchDecision,
			AcknowledgeSnapshot: req.AcknowledgeSnapshot,
		})
		if err != nil {
			return reports, err
		}
		report := ConvergeReport{Target: target, Generation: claude.Generation()}
		if plan.Status == installstate.StatusBlockedOnRecovery || plan.Status == installstate.StatusBlocked {
			report.Status = harness.StatusConflicted
			report.Blockers = append(report.Blockers, plan.Blockers...)
			reports = append(reports, report)
			continue
		}
		if plan.Status == installstate.StatusConverged && s.DryRun {
			report.Status = harness.StatusConverged
			reports = append(reports, report)
			continue
		}
		alreadyConverged := plan.Status == installstate.StatusConverged
		if s.DryRun {
			report.Status = harness.StatusEnrolled
			reports = append(reports, report)
			continue
		}
		ok := true
		if !alreadyConverged {
			ok, err = s.approve(fmt.Sprintf("Adopt %s?", target.Key()), fmt.Sprintf("classification: %s", plan.Classification.State))
		}
		if err != nil {
			return reports, err
		}
		if !ok {
			report.Status = harness.StatusStale
			report.Blockers = []string{"declined"}
			reports = append(reports, report)
			continue
		}
		migrated, err := claude.Migrate(claude.MigrationRequest{
			Home:                s.Home,
			NativeRoot:          inst.NativeRoot,
			Target:              target.Key(),
			Consumer:            target.Key(),
			Generation:          claude.Generation(),
			Tier:                string(claude.Tier),
			OperationID:         s.OperationID,
			BatchDecision:       s.BatchDecision,
			AcknowledgeSnapshot: req.AcknowledgeSnapshot,
			Now:                 s.Now,
		})
		if err != nil {
			return reports, err
		}
		if err := claude.ApplyOwnedSettings(s.Home, target, claude.Generation()); err != nil {
			return reports, err
		}
		report.Status = harness.StatusConverged
		report.Applied = len(migrated.Adoption.Applied) > 0
		reports = append(reports, report)
	}
	return reports, nil
}

// approve obtains explicit consent for a mutating plan. A non-interactive
// caller is refused rather than defaulted into mutation: --yes is user consent,
// not a substitute for a terminal.
func (s Steps) approve(title, desc string) (bool, error) {
	if s.AssumeYes {
		return true, nil
	}
	confirm := s.Confirm
	if confirm == nil {
		confirm = prompt.Confirm
	}
	ok, err := confirm(title, desc, false)
	switch {
	case errors.Is(err, prompt.ErrNonInteractive):
		return false, fmt.Errorf("refusing to mutate without explicit consent; re-run with --yes")
	case errors.Is(err, prompt.ErrAborted):
		return false, nil
	case err != nil:
		return false, err
	}
	return ok, nil
}

// registry resolves the adapter set through the step seam.
func (s Steps) registry() (*harness.Registry, error) {
	build := s.Registry
	if build == nil {
		build = DefaultRegistry
	}
	return build(s.Home)
}

// ledgerPath is the one ledger root every helper reads.
func ledgerPath(home string) string {
	return config.LedgerPath(home)
}

// enrolled reports whether the ledger records target.
func enrolled(ledger *installstate.Ledger, target harness.Target) bool {
	if _, ok := ledger.FindTarget(string(target.Kind), target.Instance); ok {
		return true
	}
	for _, row := range ledger.Rows {
		if row.Target == target.Key() {
			return true
		}
	}
	return false
}

// planSummary is the one-line description a consent prompt shows: what the plan
// would write, never the bytes themselves.
func planSummary(plan harness.Plan) string {
	return fmt.Sprintf("%d resource(s), generation %s", len(plan.Claims), short(plan.Generation))
}

// short truncates a generation digest for display.
func short(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}
