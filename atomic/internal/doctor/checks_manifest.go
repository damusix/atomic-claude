package doctor

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/manifestcheck"
)

// checkManifest implements category 5: manifest parity. Outside the
// atomic-claude repo it SKIPs; inside, drift between the generated and
// committed bundle FAILs.
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

// RunCheckManifest resolves the repo root from cwd, then checks manifest parity.
//
// Deprecated: prefer RunCheckManifestWith(root), which avoids a redundant git
// subprocess when the toplevel is already resolved. This shim goes through the
// injectable gitToplevelFn, so a test that swaps it still intercepts the call.
func RunCheckManifest(cwd string) Result {
	return RunCheckManifestWith(gitToplevelFn(cwd))
}

// RunCheckManifestWith runs the manifest parity check against an explicit repo
// root. Exported for testing.
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
