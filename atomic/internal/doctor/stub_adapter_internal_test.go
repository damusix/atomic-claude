package doctor

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/install"
)

// fakeAdapter is the deterministic harness adapter the multi-harness tests
// inject, so a category test never reaches a real harness binary or the
// developer's own configuration.
type fakeAdapter struct {
	kind       harness.Kind
	instances  []harness.Instance
	caps       harness.CapabilityMatrix
	plan       harness.Plan
	onConverge func(home string, target harness.Target)
}

func (f fakeAdapter) Kind() harness.Kind { return f.kind }

func (f fakeAdapter) Discover(string) ([]harness.Instance, error) { return f.instances, nil }

func (f fakeAdapter) Capabilities() harness.CapabilityMatrix {
	if f.caps.Harness != "" {
		return f.caps
	}
	if f.kind == harness.KindClaude {
		return harness.ClaudeCapabilities()
	}
	return harness.OMPCapabilities()
}

func (f fakeAdapter) Lifecycle(home string) harness.Lifecycle {
	return harness.Lifecycle{
		ProjectFn: func(t harness.Target, _ harness.PlanRequest) (harness.Plan, error) {
			p := f.plan
			p.Target = t
			return p, nil
		},
		ConvergeFn: func(t harness.Target, _ harness.Plan) (harness.Convergence, error) {
			if f.onConverge != nil {
				f.onConverge(home, t)
			}
			return harness.Convergence{Target: t, Status: harness.StatusConverged}, nil
		},
	}
}

// stepsWith builds read-only reporting steps over one fake adapter.
func stepsWith(home string, adapter fakeAdapter) install.Steps {
	return install.Steps{
		Home: home,
		Registry: func(string) (*harness.Registry, error) {
			reg, err := harness.NewRegistry(adapter)
			if err != nil {
				return nil, err
			}
			return reg, nil
		},
	}
}

// withStatusSteps swaps the reporting-steps seam for the duration of one test.
func withStatusSteps(t *testing.T, steps func(home string) install.Steps) {
	t.Helper()
	restore := statusStepsFn
	statusStepsFn = steps
	t.Cleanup(func() { statusStepsFn = restore })
}
