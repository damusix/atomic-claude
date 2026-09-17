package harness

import "fmt"

// PlanRequest asks an adapter to compute the projection plan for one target.
type PlanRequest struct {
	// Generation identifies the rendered generation the plan publishes.
	Generation string
}

// Plan is one target's projection plan: the resources it would write and the
// ownership claims those bytes establish.
type Plan struct {
	Target Target  `json:"target"`
	Claims []Claim `json:"claims,omitempty"`
}

// Convergence is one target's convergence outcome.
type Convergence struct {
	Target Target `json:"target"`
	Status Status `json:"status"`
}

// Removal is the outcome of removing one target's resources. Retained names
// resources kept because another enrolled consumer still depends on them.
type Removal struct {
	Target   Target   `json:"target"`
	Removed  []string `json:"removed,omitempty"`
	Retained []string `json:"retained,omitempty"`
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
