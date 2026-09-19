package doctor

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// checkRules implements category 19: every enrolled target's rule surface —
// enforcement tier, owned rule resources with their recorded and observed
// digests, and the CP0 rule-delivery roles the projection cannot promise. An
// unproven role is an uncovered operation surface, reported rather than
// presented as parity.
func checkRules(opts Opts) Result {
	statuses, err := loadRules(opts)
	if err != nil {
		return unavailability(err)
	}
	if len(statuses) == 0 {
		return Result{Severity: PASS, Detail: "no enrolled targets carry a rule surface"}
	}

	var findings, problems []string
	uncovered := 0
	for _, status := range statuses {
		key := status.Target.Key()
		tier := status.Tier
		if tier == "" {
			tier = "unsupported"
		}
		findings = append(findings, fmt.Sprintf("%s: tier=%s, %d rule resource(s), %d uncovered role(s)",
			key, tier, len(status.Resources), len(status.Gaps)))
		for _, gap := range status.Gaps {
			uncovered++
			findings = append(findings, fmt.Sprintf("%s uncovered %s: %s", key, gap.Role, gap.Status))
		}
		for _, r := range status.Resources {
			findings = append(findings, fmt.Sprintf("%s %s: %s applied=%s observed=%s tier=%s",
				key, r.Resource, r.State, shortDigest(r.Applied), shortDigest(r.Observed), r.Tier))
			switch r.State {
			case harness.RowConflict:
				problems = append(problems, fmt.Sprintf("%s: rule resource %s conflicts", key, r.Resource))
			case harness.RowMissing:
				problems = append(problems, fmt.Sprintf("%s: rule resource %s is missing", key, r.Resource))
			}
		}
	}

	detail := fmt.Sprintf("%d target rule surface(s), %d uncovered role(s)", len(statuses), uncovered)
	return resultFor(detail, problems, findings)
}
