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
func checkManifest(opts Opts) Result {
	root := opts.RepoRoot
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Result{Severity: WARN, Detail: fmt.Sprintf("getwd: %v", err)}
		}
		root = gitToplevelFn(cwd)
	}
	return RunCheckManifestWith(root)
}

// RunCheckManifest runs the manifest parity check using cwd to determine the
// repo root. Exported for testing.
//
// Deprecated: prefer RunCheckManifestWith(root) which avoids a redundant git
// subprocess when the toplevel has already been resolved. Note: this shim
// calls gitToplevelFn (the injectable resolver variable), not the underlying
// gitToplevel function directly — a test that has swapped gitToplevelFn will
// intercept this call.
func RunCheckManifest(cwd string) Result {
	return RunCheckManifestWith(gitToplevelFn(cwd))
}

// RunCheckManifestWith runs the manifest parity check against an explicit repo root.
// Exported for testing; production callers use checkManifest.
func RunCheckManifestWith(root string) Result {
	repoDev, err := isRepoDevRoot(root)
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("repo-dev detection: %v", err)}
	}
	if !repoDev {
		return Result{Severity: SKIP, Detail: "not in atomic-claude repo"}
	}

	res, err := manifestcheck.Compare(root, embedded.Manifest())
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("manifest compare: %v", err)}
	}

	if res.OK {
		return Result{Severity: PASS, Detail: "generated == committed"}
	}

	var findings []string
	for _, path := range res.Missing {
		findings = append(findings, "missing: "+path)
	}
	for _, path := range res.Extra {
		findings = append(findings, "extra: "+path)
	}
	for _, d := range res.Drifted {
		findings = append(findings, "drifted: "+d.Target)
	}

	return Result{
		Severity: FAIL,
		Detail: fmt.Sprintf(
			"%d missing, %d extra, %d drifted",
			len(res.Missing), len(res.Extra), len(res.Drifted),
		),
		Findings:    findings,
		Remediation: "make -C atomic bundle",
	}
}
