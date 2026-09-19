package omp

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Lifecycle binds the OMP projection, convergence, verification, and removal
// seams for one home. Converge runs the profile enrollment engine, which owns
// the lifecycle lock, oldest-first recovery, staging, verification-before-ledger,
// and the shared-generation refusal; this file only translates the plan.
func (a *Adapter) Lifecycle(home string) harness.Lifecycle {
	return harness.Lifecycle{
		ProjectFn: func(t harness.Target, req harness.PlanRequest) (harness.Plan, error) {
			return a.project(home, t)
		},
		ConvergeFn: func(t harness.Target, p harness.Plan) (harness.Convergence, error) {
			return a.converge(home, t)
		},
		VerifyFn: func(t harness.Target) ([]harness.Assessment, error) {
			return a.assess(home, t)
		},
		RemoveFn: func(t harness.Target) (harness.Removal, error) {
			return harness.RemoveTargetResources(home, t)
		},
	}
}

// project builds the read-only plan for one OMP profile: the shared package and
// profile steering claims, the generation the selected binary publishes, and
// every blocker — an incompatible shared generation or an ambiguous block — that
// forbids mutation.
func (a *Adapter) project(home string, t harness.Target) (harness.Plan, error) {
	if t.NativeRoot == "" {
		return harness.Plan{}, fmt.Errorf("omp: project %s: %w", t.Key(), harness.ErrUnsupported)
	}
	cat, err := a.corpus()
	if err != nil {
		return harness.Plan{}, err
	}
	pkg, err := BuildPackage(cat, a.Capabilities())
	if err != nil {
		return harness.Plan{}, err
	}
	block, err := SteeringBlock(cat)
	if err != nil {
		return harness.Plan{}, err
	}
	treeDigest, err := pkg.TreeDigest()
	if err != nil {
		return harness.Plan{}, err
	}
	blockDigest, err := SteeringDigest(block)
	if err != nil {
		return harness.Plan{}, err
	}

	plan := harness.Plan{Target: t, Generation: pkg.Generation, Converged: true}
	desired := map[string]string{
		PackageResource(home):          treeDigest,
		SteeringResource(t.NativeRoot): blockDigest,
	}
	claims, err := a.Claims(home, t.NativeRoot)
	if err != nil {
		return harness.Plan{}, err
	}
	for i := range claims {
		if digest, ok := desired[claims[i].ID]; ok {
			claims[i].SelectedDigest = digest
		}
		plan.Claims = append(plan.Claims, claims[i])
	}

	for _, claim := range plan.Claims {
		obs, err := managedfile.Observe(claim.Path, claim.Kind)
		if err != nil {
			return harness.Plan{}, err
		}
		if obs.Conflict != managedfile.ConflictNone {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s carries an ambiguous %s block", claim.Path, managedfile.BlockOpen))
			plan.Converged = false
			continue
		}
		if want := desired[claim.ID]; want == "" || obs.Digest != want {
			plan.Converged = false
		}
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return harness.Plan{}, err
	}
	if err := ensureCompatible(ledger, t.Key(), PackageResource(home), pkg.Generation); err != nil {
		plan.Blockers = append(plan.Blockers, err.Error())
	}
	return plan, nil
}

// converge applies the OMP plan through the enrollment engine.
func (a *Adapter) converge(home string, t harness.Target) (harness.Convergence, error) {
	result, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: t.NativeRoot}})
	if err != nil {
		return harness.Convergence{Target: t}, err
	}
	return harness.Convergence{Target: t, Status: result.Status}, nil
}

// assess reports the ownership verdict for every OMP claim, read-only.
func (a *Adapter) assess(home string, t harness.Target) ([]harness.Assessment, error) {
	if t.NativeRoot == "" {
		return nil, fmt.Errorf("omp: assess %s: %w", t.Key(), harness.ErrUnsupported)
	}
	claims, err := a.Claims(home, t.NativeRoot)
	if err != nil {
		return nil, err
	}
	return harness.AssessAll(claims)
}
