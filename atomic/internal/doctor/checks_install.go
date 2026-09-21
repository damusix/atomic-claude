package doctor

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
)

// checkInstall implements category 1: install integrity, diffing every embedded
// artifact against each enrolled Claude target. A missing artifact FAILs, a
// drifted one WARNs, and a target directory that does not exist SKIPs. A machine
// whose ledger enrols no Claude target is skipped instead of being failed
// against an un-enrolled ~/.claude.
func checkInstall(opts Opts) Result {
	scope := claudeScopeFor(opts)
	if scope.Skip {
		return Result{Severity: SKIP, Detail: scope.Detail}
	}
	home, err := resolveHome()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve home dir: %v", err)}
	}
	results := make([]Result, 0, len(scope.Roots))
	for _, root := range scope.Roots {
		results = append(results, RunCheckInstall(root, home))
	}
	return combineClaudeRoots(scope, results)
}

// RunCheckInstall runs the install check against an explicit target directory
// and home dir. Exported for testing.
func RunCheckInstall(target, home string) Result {
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return Result{Severity: SKIP, Detail: "atomic-claude not installed"}
	}

	rows, err := claudeinstall.Diff(target, home)
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
