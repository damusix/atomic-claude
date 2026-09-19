package install

import (
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// RulesTargetStatus is one enrolled target's rule surface: the enforcement tier
// its rows carry, the CP0 surfaces the projection cannot promise, and every
// owned rule resource compared against the selected generation.
type RulesTargetStatus struct {
	Target    harness.Target           `json:"target"`
	Tier      string                   `json:"tier,omitempty"`
	Gaps      []harness.Capability     `json:"gaps,omitempty"`
	Resources []harness.ResourceStatus `json:"resources,omitempty"`
}

// RulesStatus reports per-target rule tier, digest, coverage, and conflict state
// without writing anything. An operation without a proven native scoped-rule
// surface reports `unsupported` rather than implying parity.
func (s Steps) RulesStatus() ([]RulesTargetStatus, error) {
	reg, err := s.registry()
	if err != nil {
		return nil, err
	}
	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return nil, err
	}

	var out []RulesTargetStatus
	for _, record := range ledger.Targets {
		target := harness.Target{Kind: harness.Kind(record.Harness), Instance: record.Instance, NativeRoot: record.NativeRoot}
		status := RulesTargetStatus{Target: target}
		desired := map[string]string{}
		if adapter, err := reg.Adapter(target.Kind); err == nil {
			status.Gaps = harness.RuleGaps(adapter.Capabilities())
			if plan, err := adapter.Lifecycle(s.Home).Project(target, harness.PlanRequest{}); err == nil {
				desired = harness.DesiredDigests(plan)
			}
		}
		for _, row := range ledger.Rows {
			if row.Target != target.Key() || !isRuleResource(row.Resource, row.Applied.Path) {
				continue
			}
			resource, err := harness.RowStatus(row, desired[row.Resource])
			if err != nil {
				return nil, err
			}
			status.Resources = append(status.Resources, resource)
			if row.Tier != "" {
				status.Tier = row.Tier
			}
		}
		sort.Slice(status.Resources, func(i, j int) bool { return status.Resources[i].Resource < status.Resources[j].Resource })
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target.Key() < out[j].Target.Key() })
	return out, nil
}

// RulesSync converges the rule and steering projections of already-enrolled
// targets. It never enrolls a newly discovered instance.
func (s Steps) RulesSync() ([]ConvergeReport, error) {
	return s.Converge(ConvergeRequest{Selection: Selection{EnrolledOnly: true}})
}

// isRuleResource reports whether an owned resource is part of a target's rule
// surface. Rule generations live under a `rules` path segment: Claude's shipped
// rules and repository wiki cards, and the OMP package and project rule trees.
func isRuleResource(id, path string) bool {
	for _, part := range strings.FieldsFunc(id+"/"+path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == "rules" {
			return true
		}
	}
	return false
}
