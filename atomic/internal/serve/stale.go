// The single parser for wiki.Stale's output, which both the nav and status
// paths delegate to. Its line grammar:
//
//	DRIFT added|removed <path>
//	DRIFT status <path> <old>→<new>
package serve

import "strings"

// staleSets is the raw output of parseStaleLines.
type staleSets struct {
	// Members is keyed by member path as the scan records it.
	Members map[string]bool
}

// parseStaleLines silently skips malformed lines — a staleness check must
// never crash its caller.
func parseStaleLines(output string) staleSets {
	sets := staleSets{Members: map[string]bool{}}

	for _, line := range strings.Split(output, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 3 || parts[0] != "DRIFT" {
			continue
		}
		sets.Members[parts[2]] = true
	}

	return sets
}
