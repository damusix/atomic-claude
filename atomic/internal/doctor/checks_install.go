package doctor

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
)

// checkInstall implements category 1: install integrity.
//
// Resolves ~/.claude/ via claudeinstall.ResolveTarget, then calls
// claudeinstall.Diff to compare each embedded artifact against the on-disk
// state. Maps results to doctor severity:
//   - All DiffMatch  → PASS
//   - Any DiffAbsent → FAIL
//   - Any DiffDiffer → WARN
//
// If the target directory does not exist → SKIP.
func checkInstall(opts Opts) Result {
	target, err := claudeinstall.ResolveTarget("~/.claude")
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve target: %v", err)}
	}
	return RunCheckInstall(target)
}

// RunCheckInstall runs the install check against an explicit target directory.
// Exported for testing; production callers use checkInstall which resolves
// the target via claudeinstall.ResolveTarget.
func RunCheckInstall(target string) Result {
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return Result{Severity: SKIP, Detail: "atomic-claude not installed"}
	}

	rows, err := claudeinstall.Diff(target)
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("diff failed: %v", err)}
	}

	total := len(rows)
	var missing, drifted int
	var findings []string
	for _, r := range rows {
		switch r.Status {
		case claudeinstall.DiffAbsent:
			missing++
			findings = append(findings, "missing: "+r.Artifact.Target)
		case claudeinstall.DiffDiffer:
			drifted++
			findings = append(findings, "drifted: "+r.Artifact.Target)
		}
	}

	matched := total - missing - drifted

	switch {
	case missing > 0:
		return Result{
			Severity:    FAIL,
			Detail:      fmt.Sprintf("%d/%d files match bundle (%d missing, %d drifted)", matched, total, missing, drifted),
			Findings:    findings,
			Remediation: "atomic claude update",
		}
	case drifted > 0:
		return Result{
			Severity:    WARN,
			Detail:      fmt.Sprintf("%d/%d files match bundle (%d drifted)", matched, total, drifted),
			Findings:    findings,
			Remediation: "atomic claude update",
		}
	default:
		return Result{
			Severity: PASS,
			Detail:   fmt.Sprintf("%d/%d files match bundle", total, total),
		}
	}
}
