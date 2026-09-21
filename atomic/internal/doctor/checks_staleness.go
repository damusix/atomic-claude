package doctor

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// checkStaleness implements category 21: materialized resources whose recorded
// generation is behind the selected binary's projection, resources whose native
// bytes hold the desired generation without a matching ledger record, and owned
// resources that are not on disk at all. RowMissing is counted here for every
// owned resource, not rules only: a deleted owned skill or agent is exactly the
// drift this category exists to surface, and no other category counts it.
func checkStaleness(opts Opts) Result {
	report, err := loadStatus(opts)
	if err != nil {
		return unavailability(err)
	}
	if len(report.Targets) == 0 {
		return Result{Severity: PASS, Detail: "no enrolled targets to compare against a generation"}
	}

	var findings, problems []string
	total := 0
	for _, status := range report.Targets {
		key := status.Target.Key()
		for _, r := range status.Resources {
			total++
			if r.State == harness.RowStale {
				problems = append(problems, fmt.Sprintf("%s: %s materialized generation %s is behind the selected projection",
					key, r.Resource, shortDigest(r.Generation)))
			}
			if r.State == harness.RowMissing {
				problems = append(problems, fmt.Sprintf("%s: owned resource %s is not on disk", key, r.Resource))
			}
			if r.State == harness.RowUnverifiable {
				problems = append(problems, fmt.Sprintf("%s: %s carries no applied digest to compare", key, r.Resource))
			}
			findings = append(findings, fmt.Sprintf("%s %s: %s generation=%s applied=%s observed=%s",
				key, r.Resource, r.State, shortDigest(r.Generation), shortDigest(r.Applied), shortDigest(r.Observed)))
		}
	}

	detail := fmt.Sprintf("%d resource(s) at the selected generation", total)
	return resultFor(detail, problems, findings)
}
