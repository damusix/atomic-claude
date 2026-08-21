package migrate

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

// FormatLog renders registry's log-carrying entries — those with a non-empty
// Summary — newest-first. since, when non-empty, keeps only entries after it:
// a valid semver compares against TargetVersion, a valid YYYY-MM-DD date
// compares lexically against Date (safe because Date is always YYYY-MM-DD).
// Anything else is neither, so it can only ever match by lexical accident —
// FormatLog rejects it as an error instead. Returns "" when nothing matches.
func FormatLog(registry []Migration, since string) (string, error) {
	if since != "" && !validSince(since) {
		return "", fmt.Errorf("invalid --show-log value %q: want a semver version or a YYYY-MM-DD date", since)
	}

	var entries []Migration
	for _, m := range registry {
		if m.Summary == "" {
			continue
		}
		if since != "" && !afterSince(m, since) {
			continue
		}
		entries = append(entries, m)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return selfupdate.CompareSemver(entries[i].TargetVersion, entries[j].TargetVersion) > 0
	})

	var b strings.Builder
	for _, m := range entries {
		fmt.Fprintf(&b, "%s (%s) — %s\n", m.TargetVersion, m.Date, m.Summary)
		if m.Instructions != "" {
			fmt.Fprintf(&b, "  %s\n", m.Instructions)
		}
	}
	return b.String(), nil
}

func validSince(since string) bool {
	if selfupdate.IsValidSemver(since) {
		return true
	}
	_, err := time.Parse("2006-01-02", since)
	return err == nil
}

func afterSince(m Migration, since string) bool {
	if selfupdate.IsValidSemver(since) {
		return selfupdate.CompareSemver(m.TargetVersion, since) > 0
	}
	return m.Date > since
}
