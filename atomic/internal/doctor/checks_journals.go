package doctor

import (
	"fmt"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// checkJournals implements category 17: unfinished lifecycle journals, previewed
// read-only. SimulateRecoveries reconciles each journal in memory against the
// current native bytes, so the check reports whether a journal resolves to one
// safe result or carries a conflict, without opening the lock or writing.
func checkJournals(opts Opts) Result {
	home := installHome(opts)
	if home == "" {
		return unavailability(errNoHome)
	}

	sims, _, blocked, err := installstate.SimulateRecoveries(home)
	if err != nil {
		return Result{Severity: WARN, Detail: "could not preview unfinished journals: " + err.Error()}
	}
	if len(sims) == 0 {
		return Result{Severity: PASS, Detail: "no unfinished journals"}
	}

	var findings, problems []string
	for _, sim := range sims {
		name := filepath.Base(sim.Journal)
		findings = append(findings, fmt.Sprintf("%s: %d action(s), %d conflict(s)", name, len(sim.Actions), len(sim.Conflicts)))
		for _, conflict := range sim.Conflicts {
			problems = append(problems, fmt.Sprintf("%s: %s (%s)", name, conflict.Unit, conflict.Detail))
		}
	}

	if blocked {
		return resultFor("", problems, findings)
	}
	return Result{
		Severity: WARN,
		Detail:   fmt.Sprintf("%d unfinished journal(s), each recoverable", len(sims)),
		Findings: sortedFindings(findings),
	}
}
