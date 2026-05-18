package validate

import (
	"fmt"
	"io"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/manifestcheck"
)

// RunBundleCheckAt runs the bundle parity check against an explicit repoRoot
// and writes human-readable output to w. Returns 0 (OK), 1 (FAIL — drift found),
// or 2 (internal error). Exported for tests and future atomic doctor.
func RunBundleCheckAt(repoRoot string, w io.Writer) int {
	return runBundleAt(repoRoot, w)
}

// runBundleImpl discovers repoRoot from cwd and delegates to runBundleAt.
func runBundleImpl(w io.Writer) int {
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

	return runBundleAt(repoRoot, w)
}

// runBundleAt performs the actual bundle parity check against repoRoot using
// manifestcheck.Compare (shared with atomic doctor).
func runBundleAt(repoRoot string, w io.Writer) int {
	result, err := manifestcheck.Compare(repoRoot, embedded.Manifest())
	if err != nil {
		fmt.Fprintf(w, "atomic validate bundle: %v\n", err)
		return 2
	}

	if result.OK {
		fmt.Fprintf(w, "atomic validate bundle — OK\n")
		return 0
	}

	fmt.Fprintf(w, "atomic validate bundle — FAIL\n")

	type line struct {
		kind, target, detail string
	}
	var lines []line
	for _, t := range result.Missing {
		lines = append(lines, line{"removed", t, ""})
	}
	for _, t := range result.Extra {
		lines = append(lines, line{"added", t, ""})
	}
	for _, d := range result.Drifted {
		lines = append(lines, line{"changed", d.Target, fmt.Sprintf("sha256: %s != %s", d.GeneratedSHA, d.CommittedSHA)})
	}

	shown := lines
	if len(shown) > 5 {
		shown = shown[:5]
	}
	for _, l := range shown {
		if l.detail != "" {
			fmt.Fprintf(w, "  [%s] %s  %s\n", l.kind, l.target, l.detail)
		} else {
			fmt.Fprintf(w, "  [%s] %s\n", l.kind, l.target)
		}
	}
	if len(lines) > 5 {
		fmt.Fprintf(w, "  ... and %d more\n", len(lines)-5)
	}
	return 1
}
