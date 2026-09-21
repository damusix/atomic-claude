package harness

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// PlanRequest asks an adapter to compute the projection plan for one target.
type PlanRequest struct {
	// Generation identifies the rendered generation the plan publishes.
	Generation string
	// BatchDecision is the caller's replace-or-leave-unowned choice for
	// resources the selected generation cannot prove ownership of. Empty leaves
	// such a plan blocked, because guessing would either overwrite unknown bytes
	// or strand a resource the user expected Atomic to adopt.
	BatchDecision installstate.Decision
}

// Plan is one target's projection plan: the resources it would write, the
// ownership claims those bytes establish, and every observation that forbids
// applying it. Blockers are reported, never worked around: a plan with any
// blocker must not be converged.
type Plan struct {
	Target     Target   `json:"target"`
	Generation string   `json:"generation,omitempty"`
	Claims     []Claim  `json:"claims,omitempty"`
	Blockers   []string `json:"blockers,omitempty"`
	// BatchDecision is the caller's choice for resources the selected
	// generation cannot prove ownership of. It is empty until the caller
	// decides, which is what leaves such a plan blocked.
	BatchDecision installstate.Decision `json:"batch_decision,omitempty"`
	// Converged reports that the target already holds this generation, so
	// convergence would write nothing.
	Converged bool `json:"converged,omitempty"`
	// Unproven names the native surfaces this projection cannot promise, each as
	// "surface: evidence". A surface listed here is reported as unsupported
	// wherever the plan is shown; nothing about it is applied silently.
	Unproven []string `json:"unproven,omitempty"`
}

// Convergence is one target's convergence outcome.
type Convergence struct {
	Target Target `json:"target"`
	Status Status `json:"status"`
}

// Removal is the outcome of removing one target's resources. Retained names
// resources kept because another enrolled consumer still depends on them.
// Skipped names resources removal could not clear — a read-only settings file —
// whose ledger rows and enrollment are kept so a later uninstall can finish the
// job.
//
// A dry run populates Recovery with the unresolved journals a real removal
// would reconcile first, and Blockers with anything that forbids a decidable
// plan. An applied removal leaves both empty: recovery has already run.
type Removal struct {
	Target   Target   `json:"target"`
	Removed  []string `json:"removed,omitempty"`
	Retained []string `json:"retained,omitempty"`
	Skipped  []string `json:"skipped,omitempty"`
	// Recovery previews unresolved journals in memory. It is never a mutation.
	Recovery []installstate.RecoverySimulation `json:"recovery,omitempty"`
	// Blockers names the observations that forbid a decidable plan, including a
	// journal that cannot be simulated to one safe result.
	Blockers []string `json:"blockers,omitempty"`
}

// Lifecycle is the set of hook points a concrete adapter supplies: projection,
// convergence, verification, and removal. A nil hook is the honest report that
// this adapter owns no behavior for that surface yet, so it yields
// ErrUnsupported rather than silently succeeding.
type Lifecycle struct {
	ProjectFn  func(Target, PlanRequest) (Plan, error)
	ConvergeFn func(Target, Plan) (Convergence, error)
	VerifyFn   func(Target) ([]Assessment, error)
	RemoveFn   func(Target) (Removal, error)
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
