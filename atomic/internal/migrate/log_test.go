package migrate_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/migrate"
)

func TestFormatLogSkipsEntriesWithNoSummary(t *testing.T) {
	registry := []migrate.Migration{
		{TargetVersion: "1.0.0", Scope: "repo"}, // no log fields — execution-only
		{TargetVersion: "1.1.0", Scope: "repo", Summary: "logged one", Date: "2026-08-01"},
	}
	out, err := migrate.FormatLog(registry, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "1.0.0") {
		t.Errorf("output named the execution-only entry: %q", out)
	}
	if !strings.Contains(out, "logged one") {
		t.Errorf("output missing the logged entry's summary: %q", out)
	}
}

func TestFormatLogNewestFirst(t *testing.T) {
	registry := []migrate.Migration{
		{TargetVersion: "1.0.0", Summary: "oldest", Date: "2026-01-01"},
		{TargetVersion: "1.2.0", Summary: "newest", Date: "2026-08-01"},
		{TargetVersion: "1.1.0", Summary: "middle", Date: "2026-04-01"},
	}
	out, err := migrate.FormatLog(registry, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	iNewest := strings.Index(out, "newest")
	iMiddle := strings.Index(out, "middle")
	iOldest := strings.Index(out, "oldest")
	if iNewest < 0 || iMiddle < 0 || iOldest < 0 {
		t.Fatalf("missing entries in output: %q", out)
	}
	if !(iNewest < iMiddle && iMiddle < iOldest) {
		t.Errorf("entries not newest-first: %q", out)
	}
}

func TestFormatLogFiltersBySinceVersion(t *testing.T) {
	registry := []migrate.Migration{
		{TargetVersion: "1.0.0", Summary: "before", Date: "2026-01-01"},
		{TargetVersion: "1.2.0", Summary: "after", Date: "2026-08-01"},
	}
	out, err := migrate.FormatLog(registry, "1.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "before") {
		t.Errorf("output kept an entry at or below the since version: %q", out)
	}
	if !strings.Contains(out, "after") {
		t.Errorf("output dropped an entry above the since version: %q", out)
	}
}

func TestFormatLogFiltersBySinceDate(t *testing.T) {
	registry := []migrate.Migration{
		{TargetVersion: "1.0.0", Summary: "before", Date: "2026-01-01"},
		{TargetVersion: "1.2.0", Summary: "after", Date: "2026-08-01"},
	}
	out, err := migrate.FormatLog(registry, "2026-06-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "before") {
		t.Errorf("output kept an entry dated before since: %q", out)
	}
	if !strings.Contains(out, "after") {
		t.Errorf("output dropped an entry dated after since: %q", out)
	}
}

func TestFormatLogNoMatchingEntriesIsEmpty(t *testing.T) {
	registry := []migrate.Migration{
		{TargetVersion: "1.0.0", Scope: "repo"},
	}
	out, err := migrate.FormatLog(registry, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty output for a registry with no log entries, got %q", out)
	}
}

// A since value that is neither a valid semver nor a valid YYYY-MM-DD date
// must error rather than silently return "no entries" — indistinguishable
// from a real empty answer — or fall back to a lexical compare that returns
// a plausible-looking wrong result.
func TestFormatLogRejectsMalformedSince(t *testing.T) {
	registry := []migrate.Migration{
		{TargetVersion: "1.2.0", Summary: "s", Date: "2026-08-01"},
	}
	for _, since := range []string{"banana", "2026-13-45"} {
		out, err := migrate.FormatLog(registry, since)
		if err == nil {
			t.Errorf("since=%q: got nil error, out=%q — want a validation error", since, out)
		}
	}
}

// "1.2" is neither a 3-component semver nor a date; it must not slip through
// as an ASCII-lucky lexical compare against Date and match everything.
func TestFormatLogRejectsPartialVersionSince(t *testing.T) {
	registry := []migrate.Migration{
		{TargetVersion: "1.2.0", Summary: "s", Date: "2026-08-01"},
	}
	_, err := migrate.FormatLog(registry, "1.2")
	if err == nil {
		t.Errorf("since=%q: got nil error — want a validation error, not a lexical-compare match", "1.2")
	}
}
