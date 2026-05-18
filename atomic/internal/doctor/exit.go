package doctor

// ExitCode returns the process exit code implied by the results slice:
//
//	0  — all results are PASS, WARN, or SKIP (no FAIL)
//	1  — at least one result is FAIL
func ExitCode(results []Result) int {
	for _, r := range results {
		if r.Severity == FAIL {
			return 1
		}
	}
	return 0
}
