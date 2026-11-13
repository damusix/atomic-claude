package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// checkInstall implements category 1: every embedded artifact diffed against each
// enrolled Claude target. A ledger with no Claude target skips rather than fails.
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

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("read ledger: %v", err)}
	}
	key := harness.Target{Kind: harness.KindClaude, Instance: target}.Key()

	total := len(rows)
	var missing, drifted int
	var findings []string
	for _, r := range rows {
		switch {
		case r.Status == claudeinstall.DiffAbsent:
			missing++
			findings = append(findings, "missing: "+r.Artifact.Target)
		case r.Status == claudeinstall.DiffDiffer && ledger.IsLeft(key, filepath.Join(target, filepath.FromSlash(r.Artifact.Target))):
		case r.Status == claudeinstall.DiffDiffer:
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
