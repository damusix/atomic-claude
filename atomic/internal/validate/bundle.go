package validate

import (
	"fmt"
	"io"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/manifestcheck"
)

// RunBundleCheckAt runs the bundle parity check against an explicit repoRoot.
func RunBundleCheckAt(repoRoot string, w io.Writer) int {
	return runBundleAt(repoRoot, false, false, w)
}

// bundleFindings converts manifestcheck drift into Findings. exit is 0 or 2;
// the caller derives the final exit from the summary.
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

	// A synthetic overflow entry keeps the cap visible in both output modes.
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

// runBundleCollect returns findings without printing, so runWholeRepo can
// aggregate before emitting its own block.
func runBundleCollect(repoRoot string) ([]Finding, summary, int) {
	findings, exit := bundleFindings(repoRoot)
	if exit != 0 {
		return nil, summary{}, exit
	}
	return findings, summarize(findings), 0
}

// runBundleImpl discovers repoRoot from cwd and delegates to runBundleAt.
func runBundleImpl(jsonOut, suggest bool, w io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(w, "atomic validate bundle: cannot get working directory: %v\n", err)
		return 2
	}

	// An empty repoRoot is fine: runBundleAt skips outside the dev repo.
	repoRoot := findRepoRoot(cwd)
	return runBundleAt(repoRoot, jsonOut, suggest, w)
}

// runBundleAt checks bundle parity via manifestcheck.Compare. Parity only has
// meaning in the atomic-claude dev repo, which owns the embedded source
// snapshot; elsewhere the check skips cleanly with exit 0.
func runBundleAt(repoRoot string, jsonOut, suggest bool, w io.Writer) int {
	if !repoDev(repoRoot) {
		if jsonOut {
			printJSON(w, nil, summary{})
		} else {
			printHeader(w, "bundle", "manifest parity")
			fmt.Fprintln(w, "SKIP — not in atomic-claude repo (no embedded source to compare)")
		}
		return 0
	}

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
