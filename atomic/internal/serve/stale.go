// stale.go — shared wiki.Stale output parser.
//
// parseStaleLines is the single parser for the text emitted by wiki.Stale.
// Both computeStaleness (nav.go) and productionWikiStale (health.go) delegate
// here; they map the raw sets into their own return types.
//
// Line grammar (from atomic/internal/wiki/stale.go):
//
//	DRIFT <verb> <path>          — membership drift (added/removed/status)
//	STALE <kind> <path> [(fp)]   — artifact content drift; kind = repo | concern | summary
//	STALE bucket <name>          — bucket has a non-empty diff
package serve

import (
	"path/filepath"
	"strings"
)

// staleSets is the raw output of parseStaleLines.
type staleSets struct {
	// Members holds repo/summary/drift paths that are stale.
	// Keyed by both filepath.Base(path) and the raw path so callers can look
	// up by either form.
	Members map[string]bool

	// Buckets holds bucket names that have a non-empty diff ("STALE bucket <name>").
	Buckets map[string]bool

	// Concerns holds concern base-names that are stale ("STALE concern <path>").
	// Kept separate from Members — a stale concern does NOT mean a repo member
	// is stale, and nav should not show a member-stale badge for a stale concern.
	Concerns map[string]bool
}

// parseStaleLines parses the text output of wiki.Stale into raw sets.
// Unknown or malformed lines are silently skipped (consistent with the
// graceful-degradation policy: staleness checks must never crash callers).
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
			// "STALE bucket <name>"
			sets.Buckets[parts[2]] = true

		case prefix == "STALE" && kind == "concern":
			// "STALE concern wiki/concerns/foo.md [(fingerprint)]"
			rawPath := parts[len(parts)-1]
			// Strip parenthetical suffix e.g. "(alpha@abc123)".
			if idx := strings.Index(rawPath, "("); idx != -1 {
				rawPath = strings.TrimSpace(rawPath[:idx])
			}
			base := stripMDExt(filepath.Base(rawPath))
			sets.Concerns[base] = true

		case prefix == "STALE" || prefix == "DRIFT":
			// STALE repo/summary or DRIFT added/removed/status — repo member paths.
			rawPath := parts[len(parts)-1]
			if idx := strings.Index(rawPath, "("); idx != -1 {
				rawPath = strings.TrimSpace(rawPath[:idx])
			}
			base := filepath.Base(rawPath)
			sets.Members[base] = true
			// Also index by the raw path so nav's lookup works whether it keys
			// by name or by relative path.
			sets.Members[rawPath] = true
		}
	}

	return sets
}
