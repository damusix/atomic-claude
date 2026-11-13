package doctor

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/install"
)

// checkTargets implements category 15. Enrollment and native registration are
// separate facts, so both are reported and their disagreement is the defect.
func checkTargets(opts Opts) Result {
	report, err := loadStatus(opts)
	if err != nil {
		return unavailability(err)
	}
	if len(report.Targets) == 0 && len(report.Instances) == 0 {
		return withDetectedLeg(Result{Severity: PASS, Detail: "no discovered instances or enrolled targets"}, opts.Harness, nil)
	}

	var findings, problems []string

	for _, row := range report.Instances {
		state := "unenrolled"
		if row.Enrolled {
			state = "enrolled"
		}
		findings = append(findings, fmt.Sprintf("discovered %s (%s, exists=%v)", row.Instance.Target().Key(), state, row.Instance.Exists))
	}

	for _, status := range report.Targets {
		target := status.Target
		state := string(status.Status)
		if state == "" {
			state = string(harness.StatusEnrolled)
		}
		findings = append(findings, fmt.Sprintf("enrolled %s (%s)", target.Key(), state))
		if target.NativeRoot != "" {
			if _, err := os.Stat(target.NativeRoot); err != nil {
				problems = append(problems, fmt.Sprintf("%s native root %s is not present", target.Key(), target.NativeRoot))
				continue
			}
			if !discoveredInstance(report.Instances, target) {
				problems = append(problems, fmt.Sprintf("%s is enrolled but discovery does not report it", target.Key()))
			}
		}
	}

	detail := fmt.Sprintf("%d enrolled target(s), %d discovered instance(s)", len(report.Targets), len(report.Instances))
	return withDetectedLeg(resultFor(detail, problems, findings), opts.Harness, report.Targets)
}

// withDetectedLeg adds the OMP and Codex active-harness leg: the session runs in
// a harness Atomic has not enrolled. Claude's leg is claudeScopeFor's.
func withDetectedLeg(r Result, kind harness.Kind, targets []harness.TargetStatus) Result {
	if kind != harness.KindOMP && kind != harness.KindCodex {
		return r
	}
	for _, status := range targets {
		if status.Target.Kind == kind {
			return r
		}
	}
	remediation := fmt.Sprintf("run `atomic install --harness %s`", kind)
	leg := fmt.Sprintf("detected %s, not enrolled; %s", kind, remediation)
	if r.Severity == PASS {
		return Result{Severity: WARN, Detail: leg, Findings: r.Findings, Remediation: remediation, ManualOnly: true}
	}
	r.Detail += "; " + leg
	return r
}

// discoveredInstance reports whether discovery observed the target. A project-keyed
// target names no single native root, so it is never expected.
func discoveredInstance(rows []install.InstanceRow, target harness.Target) bool {
	for _, row := range rows {
		if row.Instance.Kind == target.Kind && row.Instance.ID == target.Instance {
			return true
		}
	}
	return false
}
