package doctor

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/manifestcheck"
)

// checkManifest implements category 5: manifest parity.
//
// Repo-dev only: skips when IsRepoDev returns false. When repo-dev, calls
// manifestcheck.Compare against the committed embedded.Manifest() slice.
// Maps result:
//   - OK=true  → PASS
//   - OK=false → FAIL with count summary
func checkManifest(_ Opts) Result {
	cwd, err := os.Getwd()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("getwd: %v", err)}
	}
	return RunCheckManifest(cwd)
}

// RunCheckManifest runs the manifest parity check using cwd to determine the
// repo root. Exported for testing.
func RunCheckManifest(cwd string) Result {
	repoDev, err := IsRepoDev(cwd)
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("repo-dev detection: %v", err)}
	}
	if !repoDev {
		return Result{Severity: SKIP, Detail: "not in atomic-claude repo"}
	}

	root := gitToplevel(cwd)

	res, err := manifestcheck.Compare(root, embedded.Manifest())
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("manifest compare: %v", err)}
	}

	if res.OK {
		return Result{Severity: PASS, Detail: "generated == committed"}
	}

	return Result{
		Severity: FAIL,
		Detail: fmt.Sprintf(
			"%d missing, %d extra, %d drifted",
			len(res.Missing), len(res.Extra), len(res.Drifted),
		),
	}
}
