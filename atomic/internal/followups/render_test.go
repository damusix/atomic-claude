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

func TestRender_PlansSectionFirst(t *testing.T) {
	entries := []Entry{
		{ID: "r-1", Title: "A risk", Created: "2026-05-17", Kind: KindFinding, Severity: SeverityRisk, ReviewBy: "2026-07-16", Status: StatusOpen},
		{ID: "p-1", Title: "A plan", Created: "2026-05-17", Kind: KindPlan, ReviewBy: "2026-07-16", Status: StatusOpen},
	}
	out := Render(entries, testToday)

	plansIdx := strings.Index(out, "📋 plans")
	riskIdx := strings.Index(out, "🟡 risks")

	if plansIdx < 0 {
		t.Fatalf("plans section missing:\n%s", out)
	}
	if riskIdx < 0 {
		t.Fatalf("risks section missing:\n%s", out)
	}
	if plansIdx >= riskIdx {
		t.Errorf("plans section should come before risks; plans=%d risks=%d", plansIdx, riskIdx)
	}
}

func TestRender_PlanExcludedFromSeverityBuckets(t *testing.T) {
	entries := []Entry{
		{ID: "p-1", Title: "A plan entry", Created: "2026-05-17", Kind: KindPlan, ReviewBy: "2026-07-16", Status: StatusOpen},
	}
	out := Render(entries, testToday)

	// Plans section must show it.
	if !strings.Contains(out, "📋 plans (1)") {
		t.Errorf("expected plans section count 1:\n%s", out)
	}
	// Severity buckets must all be empty.
	if !strings.Contains(out, "🟡 risks (0)") {
		t.Errorf("expected risks bucket empty:\n%s", out)
	}
	if !strings.Contains(out, "🔵 nits (0)") {
		t.Errorf("expected nits bucket empty:\n%s", out)
	}
	if !strings.Contains(out, "❓ questions (0)") {
		t.Errorf("expected questions bucket empty:\n%s", out)
	}
}

func TestRender_PlanWithFileShowsLink(t *testing.T) {
	entries := []Entry{
		{ID: "spec-p-1", Title: "Write spec for X", Created: "2026-05-17", Kind: KindPlan,
			File: "docs/spec/x.md", ReviewBy: "2026-07-16", Status: StatusOpen},
	}
	out := Render(entries, testToday)

	if !strings.Contains(out, "→ docs/spec/x.md") {
		t.Errorf("expected file link in plan row:\n%s", out)
	}
}

func TestRender_PlanWithoutFileNoArrow(t *testing.T) {
	entries := []Entry{
		{ID: "nofile-p-1", Title: "No file plan", Created: "2026-05-17", Kind: KindPlan,
			ReviewBy: "2026-07-16", Status: StatusOpen},
	}
	out := Render(entries, testToday)

	if strings.Contains(out, "→") {
		t.Errorf("expected no arrow for plan without file:\n%s", out)
	}
	if !strings.Contains(out, "No file plan") {
		t.Errorf("expected title in plan row:\n%s", out)
	}
}

func TestRender_FindingEntriesUnchanged(t *testing.T) {
	// Explicitly-typed finding entries must render in severity buckets unchanged.
	entries := []Entry{
		{ID: "r-1", Title: "A risk finding", Created: "2026-05-17", Kind: KindFinding,
			Severity: SeverityRisk, ReviewBy: "2026-07-16", Status: StatusOpen},
	}
	out := Render(entries, testToday)

	if !strings.Contains(out, "🟡 risks (1)") {
		t.Errorf("expected finding in risks bucket:\n%s", out)
	}
	if strings.Contains(out, "📋 plans (1)") {
		t.Errorf("finding should not appear in plans bucket:\n%s", out)
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

// CP2: plan staleness exemption tests.

func TestIsStale_PlanNeverStale(t *testing.T) {
	// A plan entry with review_by far in the past must not be stale.
	yesterday := testToday.AddDate(0, 0, -30).Format("2006-01-02")
	e := Entry{ID: "p-1", Kind: KindPlan, ReviewBy: yesterday}
	if isStale(e, testToday) {
		t.Errorf("plan entry with old review_by should not be stale")
	}
}

func TestIsStale_FindingStillStale(t *testing.T) {
	// A finding entry with review_by in the past must still be stale.
	yesterday := testToday.AddDate(0, 0, -1).Format("2006-01-02")
	e := Entry{ID: "f-1", Kind: KindFinding, Severity: SeverityRisk, ReviewBy: yesterday}
	if !isStale(e, testToday) {
		t.Errorf("finding entry with old review_by should be stale")
	}
}

func TestRender_PlanNotCountedAsStale(t *testing.T) {
	// A plan with an old review_by must NOT add to the stale-count in the header.
	yesterday := testToday.AddDate(0, 0, -1).Format("2006-01-02")
	entries := []Entry{
		{ID: "p-1", Title: "Old plan", Created: "2026-05-17", Kind: KindPlan,
			ReviewBy: yesterday, Status: StatusOpen},
		{ID: "f-1", Title: "Fresh risk", Created: "2026-05-17", Kind: KindFinding,
			Severity: SeverityRisk, ReviewBy: "2026-12-01", Status: StatusOpen},
	}
	out := Render(entries, testToday)
	if !strings.Contains(out, "Stale: 0") {
		t.Errorf("plan with old review_by should not count as stale; output:\n%s", out)
	}
}
