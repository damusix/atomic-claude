package followups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var closedTestTime = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

func TestAppendClosed_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLOSED.md")

	err := AppendClosed(path, "atomic-doctor-F-2", "gitToplevel called 3× per run", "design promoted to spec", closedTestTime)
	if err != nil {
		t.Fatalf("AppendClosed: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(raw)

	// Must contain the id
	if !strings.Contains(content, "atomic-doctor-F-2") {
		t.Errorf("missing id in output:\n%s", content)
	}
	// Must contain quoted title
	if !strings.Contains(content, `"gitToplevel called 3× per run"`) {
		t.Errorf("missing quoted title in output:\n%s", content)
	}
	// Must contain the marker
	if !strings.Contains(content, "design promoted to spec") {
		t.Errorf("missing marker in output:\n%s", content)
	}
	// Must contain the date
	if !strings.Contains(content, "2026-05-21") {
		t.Errorf("missing date in output:\n%s", content)
	}
	// Must be a single line per entry (no internal newlines in the line)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	entryLines := 0
	for _, l := range lines {
		if strings.Contains(l, "atomic-doctor-F-2") {
			entryLines++
		}
	}
	if entryLines != 1 {
		t.Errorf("expected 1 line with the id, got %d:\n%s", entryLines, content)
	}
}

func TestAppendClosed_Format(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLOSED.md")

	err := AppendClosed(path, "my-feature-F-1", "some title", "shipped in v2", closedTestTime)
	if err != nil {
		t.Fatalf("AppendClosed: %v", err)
	}

	raw, _ := os.ReadFile(path)
	content := strings.TrimRight(string(raw), "\n")

	// Expected format: - YYYY-MM-DD <id> — "<title>" — <marker>
	expected := `- 2026-05-21 my-feature-F-1 — "some title" — shipped in v2`
	if content != expected {
		t.Errorf("format mismatch:\ngot:  %q\nwant: %q", content, expected)
	}
}

func TestAppendClosed_MultilineMarkerCollapsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLOSED.md")

	// Marker with embedded newlines should be collapsed to spaces
	marker := "line one\nline two\nline three"
	err := AppendClosed(path, "my-F-1", "title", marker, closedTestTime)
	if err != nil {
		t.Fatalf("AppendClosed: %v", err)
	}

	raw, _ := os.ReadFile(path)
	content := string(raw)

	// Must not contain raw newlines inside the marker
	line := strings.TrimRight(content, "\n")
	if strings.Contains(line, "\n") {
		t.Errorf("entry line contains embedded newlines:\n%s", content)
	}
	if !strings.Contains(line, "line one line two line three") {
		t.Errorf("collapsed marker missing:\n%s", content)
	}
}

func TestAppendClosed_TitleEscaping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLOSED.md")

	// Title with embedded double-quote should be escaped
	err := AppendClosed(path, "my-F-1", `title with "quotes" inside`, "done", closedTestTime)
	if err != nil {
		t.Fatalf("AppendClosed: %v", err)
	}

	raw, _ := os.ReadFile(path)
	content := string(raw)

	if !strings.Contains(content, `\"quotes\"`) {
		t.Errorf("quotes not escaped in title:\n%s", content)
	}
}

func TestAppendClosed_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLOSED.md")

	// First close
	err := AppendClosed(path, "dedup-F-1", "some title", "first marker", closedTestTime)
	if err != nil {
		t.Fatalf("first AppendClosed: %v", err)
	}

	// Second close with same id on same date → must not double-append
	err = AppendClosed(path, "dedup-F-1", "some title", "first marker", closedTestTime)
	if err != nil {
		t.Fatalf("second AppendClosed: %v", err)
	}

	raw, _ := os.ReadFile(path)
	content := string(raw)

	// Count occurrences of the id
	count := strings.Count(content, "dedup-F-1")
	if count != 1 {
		t.Errorf("expected 1 occurrence of id, got %d:\n%s", count, content)
	}
}

func TestAppendClosed_MultipleEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLOSED.md")

	err := AppendClosed(path, "alpha-F-1", "Alpha finding", "done", closedTestTime)
	if err != nil {
		t.Fatalf("AppendClosed 1: %v", err)
	}
	err = AppendClosed(path, "beta-F-2", "Beta finding", "resolved", closedTestTime)
	if err != nil {
		t.Fatalf("AppendClosed 2: %v", err)
	}

	raw, _ := os.ReadFile(path)
	content := string(raw)

	if !strings.Contains(content, "alpha-F-1") {
		t.Errorf("missing alpha-F-1:\n%s", content)
	}
	if !strings.Contains(content, "beta-F-2") {
		t.Errorf("missing beta-F-2:\n%s", content)
	}
}

func TestParseClosedLine_RoundTrip(t *testing.T) {
	cases := []struct {
		id     string
		title  string
		marker string
	}{
		{"my-feature-F-1", "simple title", "shipped in v2"},
		{"atomic-F-2", "title with \"quotes\"", "resolved by PR #42"},
		{"task-F-3", "backslash \\test", "done"},
	}

	for _, tc := range cases {
		line := FormatClosedLine(tc.id, tc.title, tc.marker, closedTestTime)
		gotID, gotTitle, gotMarker, gotDate, err := ParseClosedLine(line)
		if err != nil {
			t.Errorf("ParseClosedLine(%q): %v", line, err)
			continue
		}
		if gotID != tc.id {
			t.Errorf("id round-trip: got %q, want %q (line: %q)", gotID, tc.id, line)
		}
		if gotTitle != tc.title {
			t.Errorf("title round-trip: got %q, want %q (line: %q)", gotTitle, tc.title, line)
		}
		if gotMarker != tc.marker {
			t.Errorf("marker round-trip: got %q, want %q (line: %q)", gotMarker, tc.marker, line)
		}
		if gotDate != "2026-05-21" {
			t.Errorf("date round-trip: got %q (line: %q)", gotDate, line)
		}
	}
}

func TestParseClosedLine_InvalidFormat(t *testing.T) {
	bad := []string{
		"",
		"not a closed line",
		"- 2026-05-21 only-id-no-rest",
	}
	for _, line := range bad {
		_, _, _, _, err := ParseClosedLine(line)
		if err == nil {
			t.Errorf("expected error for %q", line)
		}
	}
}
