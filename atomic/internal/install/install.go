// Package install owns the command-level ordering of the multi-harness
// lifecycle; each harness adapter keeps its own lock, recovery, and ledger commit.
package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
)

// Steps carries the CLI's ordering seams and the collaborators a test replaces;
// production wiring is DefaultSteps.
type Steps struct {
	Home      string
	DryRun    bool
	AssumeYes bool
	// DiscardChanged releases Atomic's claim on a resource uninstall skipped
	// because its bytes no longer verify, after per-resource confirmation.
	DiscardChanged bool
	// BatchDecision is the caller's replace-or-leave-unowned choice for
	// resources the selected generation cannot prove ownership of.
	BatchDecision installstate.Decision
	// SkipHooks converges without registering the session-start hook.
	SkipHooks   bool
	Registry    func(home string) (*harness.Registry, error)
	Confirm     func(title, desc string, def bool) (bool, error)
	Now         func() time.Time
	OperationID string
	// Warn receives the error for a harness discovery could not resolve;
	// that harness is treated as not installed.
	Warn func(error)
	// Notice receives one-line messages for the user, such as the first-install
	// profile nudge; nil discards them.
	Notice io.Writer
}

// DefaultSteps wires the production collaborators. This package is the one
// place that names the concrete harnesses.
func DefaultSteps(home string) Steps {
	return Steps{
		Home:     home,
		Registry: DefaultRegistry,
		Confirm:  prompt.Confirm,
		Now:      func() time.Time { return time.Now().UTC() },
		Notice:   os.Stderr,
	}
}

// DefaultRegistry builds the adapter set this binary ships. The home argument
// keeps the seam's shape; an adapter resolves its instances per call.
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
	// EnrolledOnly resolves from the ledger instead of discovery, so repair and
	// rules sync never touch an unenrolled instance.
	EnrolledOnly bool
	// Detect selects every installed harness when Kind is empty.
	Detect bool
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
	if sel.Kind == "" && sel.Detect {
		return s.detectInstalled(reg, sel.Instance)
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

// detectInstalled returns each harness whose native root exists, narrowed by
// selector. With nothing found and no selector it falls back to Claude.
func (s Steps) detectInstalled(reg *harness.Registry, selector string) ([]harness.Instance, error) {
	var found []harness.Instance
	var fallback *harness.Instance
	for _, kind := range []harness.Kind{harness.KindClaude, harness.KindOMP, harness.KindCodex} {
		adapter, err := reg.Adapter(kind)
		if err != nil {
			continue
		}
		var candidates []harness.Instance
		// OMP discovery lists the default profile first; Installed narrows it
		// to the instance the user runs.
		if o, ok := adapter.(*omp.Adapter); ok {
			candidates, err = o.Installed(s.Home)
		} else {
			candidates, err = adapter.Discover(s.Home)
		}
		if errors.Is(err, harness.ErrUnsupported) {
			continue
		}
		if errors.Is(err, harness.ErrNotInstalled) {
			if s.Warn != nil {
				s.Warn(err)
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			continue
		}
		// Discovery lists the configured root first; the others are candidates
		// the user has not chosen.
		inst := candidates[0]
		if kind == harness.KindClaude {
			fallback = &inst
		}
		if !inst.Exists {
			continue
		}
		if selector != "" {
			if inst, err = harness.Select([]harness.Instance{inst}, harness.Selector{Kind: kind, Instance: selector}); err != nil {
				if errors.Is(err, harness.ErrNoInstance) {
					continue
				}
				return nil, err
			}
		}
		found = append(found, inst)
	}
	if len(found) > 0 {
		return found, nil
	}
	if selector != "" {
		return nil, fmt.Errorf("no installed harness instance matches %q: %w", selector, harness.ErrNoInstance)
	}
	if fallback == nil {
		return nil, fmt.Errorf("no harness installed and no Claude adapter registered: %w", harness.ErrNoInstance)
	}
	return []harness.Instance{*fallback}, nil
}

// ConvergeRequest is one mutating convergence: install, enroll, repair, or the
// enrolled-target half of rules sync.
type ConvergeRequest struct {
	Selection Selection
	// Enroll records the target's enrollment in the same operation; repair
	// leaves it false.
	Enroll bool
	// Instances, when set, are the already-resolved targets, so a caller that
	// printed the selection converges exactly what it showed.
	Instances []harness.Instance
	// AdoptLegacy adopts a selected, unenrolled Claude root that carries legacy
	// install evidence before converging it. Only `atomic install` sets it.
	AdoptLegacy bool
}

// adoptionPlans plans, once per instance, the adoption of each unenrolled
// Claude root carrying legacy install evidence, keyed by position in instances.
func (s Steps) adoptionPlans(ledger *installstate.Ledger, instances []harness.Instance) (map[int]installstate.AdoptionPlan, error) {
	plans := map[int]installstate.AdoptionPlan{}
	for i, inst := range instances {
		target := inst.Target()
		if inst.Kind != harness.KindClaude || enrolled(ledger, target) {
			continue
		}
		plan, err := claude.PlanMigration(claude.MigrationRequest{
			Home:                s.Home,
			NativeRoot:          inst.NativeRoot,
			Target:              target.Key(),
			Generation:          claude.Generation(),
			Tier:                string(claude.Tier),
			BatchDecision:       s.adoptBatchDecision(),
			AcknowledgeSnapshot: true,
		})
		if err != nil {
			return nil, err
		}
		// Backup stamps and a steering block count, unlike update's
		// [install]-record trigger.
		if plan.Classification.Legacy.Present() {
			plans[i] = plan
		}
	}
	return plans, nil
}

func blockedAdoptionReports(instances []harness.Instance, adoptions map[int]installstate.AdoptionPlan) []ConvergeReport {
	var reports []ConvergeReport
	for i, inst := range instances {
		plan, ok := adoptions[i]
		if !ok || !adoptionBlocked(plan) {
			continue
		}
		reports = append(reports, ConvergeReport{
			Target:          inst.Target(),
			Status:          harness.StatusConflicted,
			Blockers:        withResolution(plan.Blockers),
			AdoptionBlocked: true,
		})
	}
	return reports
}

func adoptionBlocked(plan installstate.AdoptionPlan) bool {
	return plan.Status == installstate.StatusBlocked || plan.Status == installstate.StatusBlockedOnRecovery
}

func withResolution(blockers []string) []string {
	out := append([]string(nil), blockers...)
	if len(out) > 0 {
		out[len(out)-1] += "; resolve it and re-run `atomic install`"
	}
	return out
}

// ConvergeReport is one target's convergence outcome. Blockers are the
// observations that forbade mutation; Applied reports native bytes written.
type ConvergeReport struct {
	Target     harness.Target `json:"target"`
	Status     harness.Status `json:"status"`
	Generation string         `json:"generation,omitempty"`
	Applied    bool           `json:"applied"`
	Blockers   []string       `json:"blockers,omitempty"`
	// AdoptionBlocked marks a refused legacy adoption, which install exits 1 on
	// even when it prints JSON.
	AdoptionBlocked bool `json:"-"`
	// Declined marks an adoption the user refused at its prompt.
	Declined bool `json:"-"`
	// Unproven names the native surfaces the projection cannot promise, so a
	// converged target never reads as a fully delivered one.
	Unproven []string `json:"unproven,omitempty"`
}

// Converge projects, refuses a blocked plan, obtains consent, then converges
// through the adapter, which re-observes under its lock so a moved plan refuses.
func (s Steps) Converge(req ConvergeRequest) ([]ConvergeReport, error) {
	instances := req.Instances
	if instances == nil {
		var err error
		if instances, err = s.Resolve(req.Selection); err != nil {
			return nil, err
		}
	}
	reg, err := s.registry()
	if err != nil {
		return nil, err
	}
	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return nil, err
	}

	var adoptions map[int]installstate.AdoptionPlan
	if req.AdoptLegacy {
		if adoptions, err = s.adoptionPlans(ledger, instances); err != nil {
			return nil, err
		}
	}
	// A blocked adoption stops the whole install before consent, so a refusal
	// names its file and no other harness converges.
	if blocked := blockedAdoptionReports(instances, adoptions); len(blocked) > 0 {
		return blocked, nil
	}
	var reports []ConvergeReport
	for i, inst := range instances {
		target := inst.Target()
		report := ConvergeReport{Target: target}

		if !req.Enroll {
			if !enrolled(ledger, target) {
				return reports, fmt.Errorf("%s is not enrolled; run `atomic harness enroll %s --instance %s` first",
					target.Key(), target.Kind, inst.NativeRoot)
			}
		}

		_, adopting := adoptions[i]
		if adopting {
			if s.DryRun {
				report.Status = harness.StatusEnrolled
				reports = append(reports, report)
				continue
			}
			blockers, declined, err := s.adoptClaudeRoot(inst.NativeRoot)
			if err != nil {
				return reports, err
			}
			if declined {
				report.Status = harness.StatusStale
				report.Blockers = []string{"declined"}
				reports = append(reports, report)
				continue
			}
			if err := s.seedUserState(req, ledger, inst, adopting); err != nil {
				return reports, err
			}
			if len(blockers) > 0 {
				report.Status = harness.StatusConflicted
				report.Blockers = withResolution(blockers)
				report.AdoptionBlocked = true
				reports = append(reports, report)
				continue
			}
			report.Applied = true
		}

		adapter, err := reg.Adapter(inst.Kind)
		if err != nil {
			return reports, err
		}
		plan, err := adapter.Lifecycle(s.Home).Project(target, harness.PlanRequest{BatchDecision: s.BatchDecision, SkipHooks: s.SkipHooks})
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
		report.Unproven = append(report.Unproven, plan.Unproven...)
		if len(plan.Blockers) > 0 {
			report.Status = harness.StatusConflicted
			report.Blockers = plan.Blockers
			reports = append(reports, report)
			continue
		}
		if plan.Converged {
			// An already-current target still converges: the adapter's no-op path
			// is where the enrollment record is written.
			if s.DryRun {
				report.Status = harness.StatusConverged
				reports = append(reports, report)
				continue
			}
			if err := s.seedUserState(req, ledger, inst, adopting); err != nil {
				return reports, err
			}
			convergence, err := adapter.Lifecycle(s.Home).Converge(target, plan)
			report.Status = convergence.Status
			report.Unproven = appendNew(report.Unproven, convergence.Unproven)
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

		if err := s.seedUserState(req, ledger, inst, adopting); err != nil {
			return reports, err
		}
		convergence, err := adapter.Lifecycle(s.Home).Converge(target, plan)
		report.Status = convergence.Status
		report.Applied = err == nil && (!convergence.Itemized || len(convergence.Applied) > 0)
		report.Unproven = appendNew(report.Unproven, convergence.Unproven)
		if err != nil {
			return reports, err
		}
		reports = append(reports, report)
	}
	return reports, nil
}

// seedUserState runs just before inst is first written, so a refused install
// never freezes the write-once snapshot of a Claude root's pre-install bytes.
func (s Steps) seedUserState(req ConvergeRequest, ledger *installstate.Ledger, inst harness.Instance, adopting bool) error {
	if !req.Enroll {
		return nil
	}
	var claudeRoot string
	if inst.Kind == harness.KindClaude && !adopting && !enrolled(ledger, inst.Target()) {
		claudeRoot = inst.NativeRoot
	}
	notice := s.Notice
	if notice == nil {
		notice = io.Discard
	}
	return claudeinstall.SeedUserState(s.Home, claudeRoot, notice)
}

// ConvergeEnrolled converges every enrolled target. Update runs it, so unlike
// repair it must not fail an unenrolled home.
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

// Adopt migrates one verified legacy Claude installation, the explicit
// `harness adopt claude` flow, using the same planner a dry run reports.
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
			ok, err = s.approve(fmt.Sprintf("Adopt %s?", target.Key()), adoptApprovalDesc(plan, s.BatchDecision))
		}
		if err != nil {
			return reports, err
		}
		if !ok {
			report.Status = harness.StatusStale
			report.Blockers = []string{"declined"}
			report.Declined = true
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
			if errors.Is(err, installstate.ErrAdoptionRefused) {
				report.Status = harness.StatusConflicted
				report.Blockers = migrated.Adoption.Plan.Blockers
				reports = append(reports, report)
			}
			return reports, err
		}
		if err := claude.ApplyOwnedSettings(s.Home, target, claude.Generation(), s.SkipHooks); err != nil {
			return reports, err
		}
		report.Status = harness.StatusConverged
		report.Applied = len(migrated.Adoption.Applied) > 0
		reports = append(reports, report)
	}
	return reports, nil
}

// ErrInstallRecordUnreadable marks a config.toml the legacy [install] record
// could not be read from: a warning the user fixes in config.toml.
var ErrInstallRecordUnreadable = errors.New("read legacy install record")

// AdoptLegacyClaude adopts an unenrolled, unadopted legacy Claude root. An
// enrolled root with an unmarked record only gets the marker, which a failed write left behind.
func (s Steps) AdoptLegacyClaude(announce func(root string)) (string, []string, error) {
	root := claude.DefaultConfigDir(s.Home)
	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return root, nil, nil
	}
	if _, ok := ledger.FindTarget(string(harness.KindClaude), root); ok {
		if rec, err := claudeinstall.ReadInstallRecord(s.Home); err == nil && rec.Present() && !rec.Adopted {
			return root, nil, installstate.MarkLegacyAdopted(s.Home)
		}
		return root, nil, nil
	}
	rec, err := claudeinstall.ReadInstallRecord(s.Home)
	if err != nil {
		return root, nil, fmt.Errorf("%w: %w", ErrInstallRecordUnreadable, err)
	}
	if !rec.Present() || rec.Adopted {
		return root, nil, nil
	}
	if announce != nil {
		announce(root)
	}
	s.AssumeYes = true
	blockers, _, err := s.adoptClaudeRoot(root)
	return root, blockers, err
}

// adoptBatchDecision is the caller's replace-or-leave-unowned choice, else
// replace: automatic adoption always keeps a recovery copy of what it replaces.
func (s Steps) adoptBatchDecision() installstate.Decision {
	if s.BatchDecision == "" {
		return installstate.DecisionReplace
	}
	return s.BatchDecision
}

// adoptClaudeRoot is the adoption path update and install share; consent is
// Adopt's own approval. A refusal returns blockers; declined, a refused approval.
func (s Steps) adoptClaudeRoot(root string) (blockers []string, declined bool, err error) {
	s.BatchDecision = s.adoptBatchDecision()
	reports, err := s.Adopt(AdoptRequest{
		Selection:           Selection{Kind: harness.KindClaude, Instance: root},
		AcknowledgeSnapshot: true,
	})
	for _, r := range reports {
		if r.Declined {
			declined = true
			continue
		}
		blockers = append(blockers, r.Blockers...)
	}
	if errors.Is(err, installstate.ErrAdoptionRefused) && len(blockers) > 0 {
		return blockers, declined, nil
	}
	return blockers, declined, err
}

// adoptApprovalDesc is the consent prompt for adopting one legacy root: what
// the batch decision replaces and where the recovery copies go.
func adoptApprovalDesc(plan installstate.AdoptionPlan, decision installstate.Decision) string {
	desc := fmt.Sprintf("classification: %s", plan.Classification.State)
	if decision == installstate.DecisionReplace {
		desc += "; older-version artifacts are replaced, recovery copies kept under ~/.atomic/backups/"
	}
	return desc
}

// approve obtains explicit consent for a mutating plan. A non-interactive caller
// is refused, not defaulted into mutation.
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

// TargetEnrolled reports whether the ledger enrolls target; the legacy Claude
// verbs use it to choose between the install engine and the legacy writer.
func TargetEnrolled(home string, target harness.Target) (bool, error) {
	ledger, err := installstate.LoadLedger(ledgerPath(home))
	if err != nil {
		return false, err
	}
	return enrolled(ledger, target), nil
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

func appendNew(list, more []string) []string {
	for _, s := range more {
		if !slices.Contains(list, s) {
			list = append(list, s)
		}
	}
	return list
}

// planSummary is the one-line description a consent prompt shows: what the plan
// would write, never the bytes themselves.
func planSummary(plan harness.Plan) string {
	summary := fmt.Sprintf("%d resource(s), generation %s", len(plan.Claims), short(plan.Generation))
	for _, notice := range plan.Notices {
		summary += "; " + notice
	}
	return summary
}

// short truncates a generation digest for display.
func short(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}
