package doctor

import (
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
)

// checkResources implements category 16. Visibility is not a dependency, so
// consumers and merely-visible instances are reported separately.
func checkResources(opts Opts) Result {
	report, err := loadStatus(opts)
	if err != nil {
		return unavailability(err)
	}
	if len(report.Shared) == 0 {
		return Result{Severity: PASS, Detail: "no ledger-owned resources"}
	}

	ledger, ledgerErr := loadLedger(opts)
	enrolledKeys := map[string]bool{}
	if ledgerErr == nil {
		ompEnrolled := false
		for _, record := range ledger.Targets {
			enrolledKeys[record.Key()] = true
			ompEnrolled = ompEnrolled || record.Harness == string(harness.KindOMP)
		}
		// Project card rows name a per-repository target the ledger never
		// enrolls; any enrolled OMP target consumes them.
		for _, row := range ledger.Rows {
			if ompEnrolled && row.Target == omp.ProjectRuleTarget(row.Consumer).Key() {
				enrolledKeys[row.Target] = true
			}
		}
	}

	var findings, problems []string
	for _, r := range report.Shared {
		findings = append(findings, fmt.Sprintf("%s: owner=%s consumers=%s visible-to=%s",
			r.ID, orNone(r.Owner), joinOrNone(r.Consumers), joinOrNone(r.VisibleTo)))
		if len(r.Consumers) == 0 {
			problems = append(problems, fmt.Sprintf("%s is owned but has no consumer", r.ID))
			continue
		}
		if ledgerErr == nil {
			for _, consumer := range r.Consumers {
				if !enrolledKeys[consumer] {
					problems = append(problems, fmt.Sprintf("%s names consumer %s, which the ledger does not enroll", r.ID, consumer))
				}
			}
		}
	}

	detail := fmt.Sprintf("%d shared resource(s)", len(report.Shared))
	return resultFor(detail, problems, findings)
}

// orNone renders an optional identity as a placeholder rather than an empty
// field, so a finding line never reads as truncated.
func orNone(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}

// joinOrNone renders a list for a finding line.
func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ",")
}
