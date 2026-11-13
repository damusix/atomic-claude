package install

import (
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// RulesTargetStatus is one enrolled target's rule surface against the selected
// generation.
type RulesTargetStatus struct {
	Target    harness.Target           `json:"target"`
	Tier      string                   `json:"tier,omitempty"`
	Gaps      []harness.Capability     `json:"gaps,omitempty"`
	Resources []harness.ResourceStatus `json:"resources,omitempty"`
	// Unproven names the native package and runtime surfaces this target's
	// projection cannot promise, each as "surface: evidence".
	Unproven []string `json:"unproven,omitempty"`
}

// RulesStatus reports per-target rule state without writing. A target without a
// proven scoped-rule surface reports `unsupported` rather than implying parity.
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
		adapter, adapterErr := reg.Adapter(target.Kind)
		if adapterErr == nil {
			status.Gaps = harness.RuleGaps(adapter.Capabilities())
			if plan, err := adapter.Lifecycle(s.Home).Project(target, harness.PlanRequest{}); err == nil {
				desired = harness.DesiredDigests(plan)
				status.Unproven = append(status.Unproven, plan.Unproven...)
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
		if adapterErr == nil && status.Tier == "" && target.Kind == harness.KindCodex {
			status.Tier = harness.WikiCardTier(adapter.Capabilities())
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
// surface: every rule generation lives under a `rules` path segment.
func isRuleResource(id, path string) bool {
	for _, part := range strings.FieldsFunc(id+"/"+path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == "rules" {
			return true
		}
	}
	return false
}
