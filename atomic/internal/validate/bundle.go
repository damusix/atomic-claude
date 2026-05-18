package validate

import (
	"fmt"
	"io"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/manifestcheck"
)

// RunBundleCheckAt runs the bundle parity check against an explicit repoRoot
// and writes output to w. Returns 0 (OK), 1 (FAIL — drift found), or
// 2 (internal error). Exported for tests and future atomic doctor.
func RunBundleCheckAt(repoRoot string, w io.Writer) int {
	return runBundleAt(repoRoot, false, false, w)
}

// bundleFindings runs manifestcheck.Compare and converts its Result into
// Findings (Rule="bundle", Severity="FAIL"). Returns (findings, exit) where
// exit is 0 (clean) or 2 (internal error from Compare). The caller decides
// final exit by inspecting the summary.
func bundleFindings(repoRoot string) ([]Finding, int) {
	result, err := manifestcheck.Compare(repoRoot, embedded.Manifest())
	if err != nil {
		return nil, 2
	}

	var findings []Finding
	for _, t := range result.Missing {
		findings = append(findings, Finding{
			Severity: "FAIL",
			Rule:     "bundle",
			Path:     t,
			Line:     0,
			Message:  "removed: not present in working tree",
		})
	}
	for _, t := range result.Extra {
		findings = append(findings, Finding{
			Severity: "FAIL",
			Rule:     "bundle",
			Path:     t,
			Line:     0,
			Message:  "added: not present in committed manifest",
		})
	}
	for _, d := range result.Drifted {
		findings = append(findings, Finding{
			Severity: "FAIL",
			Rule:     "bundle",
			Path:     d.Target,
			Line:     0,
			Message:  fmt.Sprintf("changed: sha256 %s != %s", d.GeneratedSHA, d.CommittedSHA),
		})
	}

	// Cap visible findings at 5; emit synthetic overflow so the cap is visible
	// in both human and JSON output.
	if len(findings) > 5 {
		overflow := len(findings) - 5
		findings = append(findings[:5], Finding{
			Severity: "FAIL",
			Rule:     "bundle",
			Path:     "",
			Line:     0,
			Message:  fmt.Sprintf("%d more diffs not shown", overflow),
		})
	}
	return findings, 0
}

// runBundleCollect runs the bundle parity check and returns findings + summary
// without printing anything. Used by runWholeRepo to aggregate findings before
// printing a unified header+block. Returns (findings, summary, exitCode)
// where exitCode is 0 (ok) or 2 (internal error).
func runBundleCollect(repoRoot string) ([]Finding, summary, int) {
	findings, exit := bundleFindings(repoRoot)
	if exit != 0 {
		return nil, summary{}, exit
	}
	return findings, summarize(findings), 0
}

// runBundleImpl discovers repoRoot from cwd and delegates to runBundleAt.
// Called from the validate dispatch when no explicit root is available.
func runBundleImpl(jsonOut, suggest bool, w io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(w, "atomic validate bundle: cannot get working directory: %v\n", err)
		return 2
	}

	repoRoot := findRepoRoot(cwd)
	if repoRoot == "" {
		fmt.Fprintf(w, "atomic validate bundle: no .git found from %s\n", cwd)
		return 2
	}

	return runBundleAt(repoRoot, jsonOut, suggest, w)
}

// runBundleAt performs the actual bundle parity check against repoRoot via
// manifestcheck.Compare (shared with atomic doctor). Drift entries are
// converted to Findings with Rule="bundle" and Severity="FAIL", emitted via
// the unified formatter so all three subcommands share one output contract.
func runBundleAt(repoRoot string, jsonOut, suggest bool, w io.Writer) int {
	findings, exit := bundleFindings(repoRoot)
	if exit != 0 {
		fmt.Fprintf(w, "atomic validate bundle: internal error\n")
		return exit
	}

	s := summarize(findings)

	if jsonOut {
		printJSON(w, findings, s)
	} else {
		printHeader(w, "bundle", "manifest parity")
		printHuman(w, findings, s, suggest)
	}

	return exitCode(s)
}
