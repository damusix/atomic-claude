package followups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// today used in tests: 2026-05-21 (matches expected_index.md fixture)
var testToday = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

func TestRender_GoldenIndex(t *testing.T) {
	entries, err := LoadEntries(filepath.Join("testdata", "entries"))
	if err != nil {
		t.Fatalf("LoadEntries: %v", err)
	}

	got := Render(entries, testToday)

	expectedBytes, err := os.ReadFile(filepath.Join("testdata", "expected_index.md"))
	if err != nil {
		t.Fatalf("read expected: %v", err)
	}
	want := string(expectedBytes)

	if got != want {
		t.Errorf("Render output mismatch.\nGot:\n%s\nWant:\n%s", got, want)
	}
}

func TestRender_SeverityBucketOrder(t *testing.T) {
	entries := []Entry{
		{ID: "q-1", Title: "A question", Created: "2026-05-17", Severity: SeverityQuestion, ReviewBy: "2026-07-16", Status: StatusOpen},
		{ID: "n-1", Title: "A nit", Created: "2026-05-17", Severity: SeverityNit, ReviewBy: "2026-07-16", Status: StatusOpen},
		{ID: "r-1", Title: "A risk", Created: "2026-05-17", Severity: SeverityRisk, ReviewBy: "2026-07-16", Status: StatusOpen},
	}
	out := Render(entries, testToday)

	riskIdx := strings.Index(out, "🟡 risks")
	nitIdx := strings.Index(out, "🔵 nits")
	questIdx := strings.Index(out, "❓ questions")

	if riskIdx < 0 || nitIdx < 0 || questIdx < 0 {
		t.Fatalf("missing bucket headers in output:\n%s", out)
	}
	if !(riskIdx < nitIdx && nitIdx < questIdx) {
		t.Errorf("bucket order wrong: risks=%d nits=%d questions=%d", riskIdx, nitIdx, questIdx)
	}
}

func TestRender_StaleMarking(t *testing.T) {
	// review_by is yesterday → stale
	yesterday := testToday.AddDate(0, 0, -1).Format("2006-01-02")
	entries := []Entry{
		{ID: "stale-F-1", Title: "Stale entry", Created: "2026-05-17", Severity: SeverityRisk,
			ReviewBy: yesterday, Status: StatusOpen},
	}
	out := Render(entries, testToday)
	if !strings.Contains(out, "**stale**") {
		t.Errorf("expected stale marker in output:\n%s", out)
	}
	if !strings.Contains(out, "Stale: 1") {
		t.Errorf("expected Stale: 1 in header:\n%s", out)
	}
}

func TestRender_NotStaleWhenReviewByIsToday(t *testing.T) {
	// review_by == today → not stale (stale = today > review_by)
	entries := []Entry{
		{ID: "fresh-F-1", Title: "Fresh entry", Created: "2026-05-21", Severity: SeverityRisk,
			ReviewBy: testToday.Format("2006-01-02"), Status: StatusOpen},
	}
	out := Render(entries, testToday)
	if strings.Contains(out, "**stale**") {
		t.Errorf("should not be stale when review_by == today:\n%s", out)
	}
	if !strings.Contains(out, "Stale: 0") {
		t.Errorf("expected Stale: 0:\n%s", out)
	}
}

func TestRender_AgeInDays(t *testing.T) {
	// created = 2026-05-10 → age = 11d when today = 2026-05-21
	entries := []Entry{
		{ID: "age-F-1", Title: "Old entry", Created: "2026-05-10", Severity: SeverityNit,
			ReviewBy: "2026-07-09", Status: StatusOpen},
	}
	out := Render(entries, testToday)
	if !strings.Contains(out, "11d") {
		t.Errorf("expected 11d age in output:\n%s", out)
	}
	// Must not contain weeks/months format
	if strings.Contains(out, "1w") || strings.Contains(out, "1m") {
		t.Errorf("age must stay in days only:\n%s", out)
	}
}

func TestRender_FutureCreatedRendersAsQuestionMark(t *testing.T) {
	// created in the future (clock skew or typo) must surface as `?d`
	// rather than silently rendering 0d. testToday is 2026-05-21.
	entries := []Entry{
		{ID: "future-F-1", Title: "Time traveler", Created: "2027-01-01", Severity: SeverityNit,
			ReviewBy: "2027-03-02", Status: StatusOpen},
	}
	out := Render(entries, testToday)
	if !strings.Contains(out, "?d") {
		t.Errorf("expected ?d age marker for future created date:\n%s", out)
	}
	if strings.Contains(out, "0d") {
		t.Errorf("future created date must not silently render as 0d:\n%s", out)
	}
}

func TestRender_UnparseableCreatedRendersAsQuestionMark(t *testing.T) {
	entries := []Entry{
		// Bypass ParseEntry validation by constructing directly with a bogus date.
		{ID: "bogus-F-1", Title: "Bad date", Created: "not-a-date", Severity: SeverityNit,
			ReviewBy: "2027-03-02", Status: StatusOpen},
	}
	out := Render(entries, testToday)
	if !strings.Contains(out, "?d") {
		t.Errorf("expected ?d age marker for unparseable created date:\n%s", out)
	}
}

func TestRender_EmptyBuckets(t *testing.T) {
	entries := []Entry{
		{ID: "r-1", Title: "Only a risk", Created: "2026-05-17", Severity: SeverityRisk,
			ReviewBy: "2026-07-16", Status: StatusOpen},
	}
	out := Render(entries, testToday)
	// All three buckets must appear even when empty
	if !strings.Contains(out, "🔵 nits (0)") {
		t.Errorf("expected empty nits bucket:\n%s", out)
	}
	if !strings.Contains(out, "❓ questions (0)") {
		t.Errorf("expected empty questions bucket:\n%s", out)
	}
	if !strings.Contains(out, "(none)") {
		t.Errorf("expected (none) for empty buckets:\n%s", out)
	}
}

func TestRender_HeaderCounts(t *testing.T) {
	// Stale entries must be included in header Open count
	yesterday := testToday.AddDate(0, 0, -1).Format("2006-01-02")
	entries := []Entry{
		{ID: "stale-r-1", Title: "Stale risk", Created: "2026-05-17", Severity: SeverityRisk,
			ReviewBy: yesterday, Status: StatusOpen},
		{ID: "fresh-n-1", Title: "Fresh nit", Created: "2026-05-20", Severity: SeverityNit,
			ReviewBy: "2026-07-20", Status: StatusOpen},
	}
	out := Render(entries, testToday)
	if !strings.Contains(out, "Open: 2") {
		t.Errorf("expected Open: 2 in header:\n%s", out)
	}
	if !strings.Contains(out, "Stale: 1") {
		t.Errorf("expected Stale: 1 in header:\n%s", out)
	}
}
