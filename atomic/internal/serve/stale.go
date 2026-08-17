// The single parser for wiki.Stale's output, which both the nav and status
// paths delegate to. Its line grammar:
//
//	DRIFT <verb> <path>          — membership drift
//	STALE <kind> <path> [(fp)]   — content drift; kind = repo | concern | summary
//	STALE bucket <name>          — bucket has a non-empty diff
package serve

import (
	"path/filepath"
	"strings"
)

// staleSets is the raw output of parseStaleLines.
type staleSets struct {
	// Members is keyed by both base name and raw path, so a caller can look up
	// by either form.
	Members map[string]bool

	Buckets map[string]bool

	// Concerns stays separate from Members: a stale concern does not make its
	// member stale, and nav must not badge it as if it did.
	Concerns map[string]bool
}

// parseStaleLines silently skips malformed lines — a staleness check must
// never crash its caller.
func parseStaleLines(output string) staleSets {
	sets := staleSets{
		Members:  map[string]bool{},
		Buckets:  map[string]bool{},
		Concerns: map[string]bool{},
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		prefix, kind := parts[0], parts[1]

		switch {
		case prefix == "STALE" && kind == "bucket":
			sets.Buckets[parts[2]] = true

		case prefix == "STALE" && kind == "concern":
			rawPath := parts[len(parts)-1]
			// Drop a trailing fingerprint like "(alpha@abc123)".
			if idx := strings.Index(rawPath, "("); idx != -1 {
				rawPath = strings.TrimSpace(rawPath[:idx])
			}
			base := stripMDExt(filepath.Base(rawPath))
			sets.Concerns[base] = true

		case prefix == "STALE" || prefix == "DRIFT":
			rawPath := parts[len(parts)-1]
			if idx := strings.Index(rawPath, "("); idx != -1 {
				rawPath = strings.TrimSpace(rawPath[:idx])
			}
			base := filepath.Base(rawPath)
			sets.Members[base] = true
			// Both forms, so nav resolves whether it keys by name or by path.
			sets.Members[rawPath] = true
		}
	}

	return sets
}
