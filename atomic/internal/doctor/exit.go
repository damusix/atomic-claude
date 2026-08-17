package doctor

// ExitCode returns 1 when any result FAILs, else 0 — WARN and SKIP do not
// fail the run.
func ExitCode(results []Result) int {
	for _, r := range results {
		if r.Severity == FAIL {
			return 1
		}
	}
	return 0
}
