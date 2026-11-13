package harness

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// PlanRequest asks an adapter to compute the projection plan for one target.
type PlanRequest struct {
	// Generation identifies the rendered generation the plan publishes.
	Generation string
	// BatchDecision is the replace-or-leave-unowned choice for unproven
	// resources. Empty blocks the plan, because either guess can lose user bytes.
	BatchDecision installstate.Decision
	// SkipHooks leaves the session-start hook unregistered on an adapter that
	// would otherwise register it.
	SkipHooks bool
}

// Plan is one target's projection plan. A plan with any blocker must not be
// converged.
type Plan struct {
	Target     Target   `json:"target"`
	Generation string   `json:"generation,omitempty"`
	Claims     []Claim  `json:"claims,omitempty"`
	Blockers   []string `json:"blockers,omitempty"`
	// BatchDecision carries PlanRequest.BatchDecision.
	BatchDecision installstate.Decision `json:"batch_decision,omitempty"`
	// Converged reports that the target already holds this generation, so
	// convergence would write nothing.
	Converged bool `json:"converged,omitempty"`
	// Unproven names the native surfaces this projection cannot promise, each as
	// "surface: evidence", reported wherever the plan is shown.
	Unproven []string `json:"unproven,omitempty"`
	// Notices names native mutations convergence makes outside the ledger, so
	// consent covers them.
	Notices []string `json:"notices,omitempty"`
	// SkipHooks carries PlanRequest.SkipHooks into convergence.
	SkipHooks bool `json:"-"`
}

// Convergence is one target's convergence outcome.
type Convergence struct {
	Target Target `json:"target"`
	Status Status `json:"status"`
	// Applied names the native resources written; it is complete only when
	// Itemized is set.
	Applied  []string `json:"applied,omitempty"`
	Itemized bool     `json:"-"`
	// Unproven names surfaces convergence could not deliver, as "surface:
	// reason; fix: remediation".
	Unproven []string `json:"unproven,omitempty"`
}

// Removal is the outcome of removing one target's resources. Skipped resources
// keep their ledger rows so a later uninstall or confirmed discard can finish.
type Removal struct {
	Target Target `json:"target"`
	// TargetKey is the ledger key the removal matched. It is the only identity
	// for a raw key that does not parse into a Target, which leaves Target zero.
	TargetKey string   `json:"target_key,omitempty"`
	Removed   []string `json:"removed,omitempty"`
	Retained  []string `json:"retained,omitempty"`
	Skipped   []string `json:"skipped,omitempty"`
	// Left names tracked repository resources, such as the card link, whose
	// claim the removal released without deleting them.
	Left []string `json:"left,omitempty"`
	// SkipReasons names why each skipped resource was skipped, keyed by
	// resource id.
	SkipReasons map[string]string `json:"skip_reasons,omitempty"`
	// Discarded names the skipped resources a confirmed discard released.
	Discarded []string `json:"discarded,omitempty"`
	// Pruned names the directories a removal emptied and deleted.
	Pruned []string `json:"pruned,omitempty"`
	// Notices names native state the removal released but could not undo,
	// with the commands that would.
	Notices []string `json:"notices,omitempty"`
	// Recovery previews unresolved journals in memory. It is never a mutation.
	Recovery []installstate.RecoverySimulation `json:"recovery,omitempty"`
	// Blockers names the observations that forbid a decidable plan, including a
	// journal that cannot be simulated to one safe result.
	Blockers []string `json:"blockers,omitempty"`
}

// NoteSkip records why one resource was skipped, so a skip line can name the
// resolution instead of the bare id.
func (r *Removal) NoteSkip(id, reason string) {
	if r.SkipReasons == nil {
		r.SkipReasons = map[string]string{}
	}
	r.SkipReasons[id] = reason
}

// Lifecycle is the set of hook points a concrete adapter supplies. A nil hook
// yields ErrUnsupported rather than silently succeeding.
type Lifecycle struct {
	ProjectFn  func(Target, PlanRequest) (Plan, error)
	ConvergeFn func(Target, Plan) (Convergence, error)
	VerifyFn   func(Target) ([]Assessment, error)
	RemoveFn   func(Target) (Removal, error)
	// RemovedFn undoes native state the ledger cannot delete; a failure becomes
	// a Removal.Notices line and the removal still completes.
	RemovedFn func(Target, Removal) (Removal, error)
}

// Removed runs the adapter's post-removal step, a no-op when it has none.
func (l Lifecycle) Removed(t Target, r Removal) (Removal, error) {
	if l.RemovedFn == nil {
		return r, nil
	}
	return l.RemovedFn(t, r)
}

// Project computes the target's projection plan.
func (l Lifecycle) Project(t Target, req PlanRequest) (Plan, error) {
	if l.ProjectFn == nil {
		return Plan{}, fmt.Errorf("harness: project %s: %w", t.Key(), ErrUnsupported)
	}
	return l.ProjectFn(t, req)
}

// Converge applies a projection plan and reports where the target landed.
func (l Lifecycle) Converge(t Target, p Plan) (Convergence, error) {
	if l.ConvergeFn == nil {
		return Convergence{}, fmt.Errorf("harness: converge %s: %w", t.Key(), ErrUnsupported)
	}
	return l.ConvergeFn(t, p)
}

// Verify assesses the target's ownership evidence as currently observed.
func (l Lifecycle) Verify(t Target) ([]Assessment, error) {
	if l.VerifyFn == nil {
		return nil, fmt.Errorf("harness: verify %s: %w", t.Key(), ErrUnsupported)
	}
	return l.VerifyFn(t)
}

// Remove plans the removal of the target's resources, retaining any resource
// another enrolled consumer still depends on.
func (l Lifecycle) Remove(t Target) (Removal, error) {
	if l.RemoveFn == nil {
		return Removal{}, fmt.Errorf("harness: remove %s: %w", t.Key(), ErrUnsupported)
	}
	return l.RemoveFn(t)
}
