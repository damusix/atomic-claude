package doctor

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// checkConflicts implements category 22: owned resources whose native bytes
// match neither the recorded generation nor the selected projection. A later
// derivative edit, a malformed managed block, or a conflicting project card all
// land here; none is overwritten by a repair.
func checkConflicts(opts Opts) Result {
	report, err := loadStatus(opts)
	if err != nil {
		return unavailability(err)
	}
	if len(report.Targets) == 0 {
		return Result{Severity: PASS, Detail: "no enrolled targets to check for conflicts"}
	}

	var findings, problems []string
	for _, status := range report.Targets {
		key := status.Target.Key()
		for _, r := range status.Resources {
			findings = append(findings, fmt.Sprintf("%s %s: %s", key, r.Resource, describeConflict(r)))
			if r.State == harness.RowConflict {
				problems = append(problems, fmt.Sprintf("%s: %s carries bytes Atomic did not record (observed=%s, recorded=%s)",
					key, r.Resource, shortDigest(r.Observed), shortDigest(r.Applied)))
			}
		}
		for _, blocker := range status.Blockers {
			problems = append(problems, fmt.Sprintf("%s: %s", key, blocker))
		}
	}

	detail := fmt.Sprintf("%d target(s) inspected for conflicts", len(report.Targets))
	return resultFor(detail, problems, findings)
}

// describeConflict renders one resource's conflict verdict, naming the
// managed-block conflict when the observation carries one.
func describeConflict(r harness.ResourceStatus) string {
	if r.Conflict != "" {
		return fmt.Sprintf("%s (%s)", r.State, r.Conflict)
	}
	return string(r.State)
}
