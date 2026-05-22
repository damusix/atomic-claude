package followups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEntry_HappyPath(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "entries", "atomic-doctor-F-1.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	e, err := ParseEntry(string(raw))
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	if e.ID != "atomic-doctor-F-1" {
		t.Errorf("id: got %q, want %q", e.ID, "atomic-doctor-F-1")
	}
	if e.Title != "bundlemirror.Run double-reads files via path reconstruction" {
		t.Errorf("title: got %q", e.Title)
	}
	if e.Created != "2026-05-17" {
		t.Errorf("created: got %q", e.Created)
	}
	if !strings.Contains(e.Origin, "docs/spec/atomic-doctor.md") {
		t.Errorf("origin: got %q", e.Origin)
	}
	if e.Severity != SeverityRisk {
		t.Errorf("severity: got %q", e.Severity)
	}
	if e.ReviewBy != "2026-05-20" {
		t.Errorf("review_by: got %q", e.ReviewBy)
	}
	if e.Status != StatusOpen {
		t.Errorf("status: got %q", e.Status)
	}
	if e.File != "atomic/internal/bundlemirror/mirror.go:196-216" {
		t.Errorf("file: got %q", e.File)
	}
}

func TestParseEntry_NitNoFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "entries", "skill-routing-F-1.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	e, err := ParseEntry(string(raw))
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	if e.Severity != SeverityNit {
		t.Errorf("severity: got %q", e.Severity)
	}
	if e.File != "" {
		t.Errorf("file: expected empty, got %q", e.File)
	}
}

func TestParseEntry_Question(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "entries", "open-question-Q-1.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	e, err := ParseEntry(string(raw))
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	if e.Severity != SeverityQuestion {
		t.Errorf("severity: got %q", e.Severity)
	}
}

func TestParseEntry_MissingRequiredField(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name: "missing id",
			content: `---
title: "test"
created: 2026-05-17
origin: some origin
severity: risk
review_by: 2026-07-16
status: open
---
body
`,
		},
		{
			name: "missing title",
			content: `---
id: test-F-1
created: 2026-05-17
origin: some origin
severity: risk
review_by: 2026-07-16
status: open
---
body
`,
		},
		{
			name: "missing severity",
			content: `---
id: test-F-1
title: "test"
created: 2026-05-17
origin: some origin
review_by: 2026-07-16
status: open
---
body
`,
		},
		{
			name: "missing status",
			content: `---
id: test-F-1
title: "test"
created: 2026-05-17
origin: some origin
severity: risk
review_by: 2026-07-16
---
body
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseEntry(tc.content)
			if err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestParseEntry_InvalidSeverity(t *testing.T) {
	content := `---
id: test-F-1
title: "test"
created: 2026-05-17
origin: some origin
severity: critical
review_by: 2026-07-16
status: open
---
body
`
	_, err := ParseEntry(content)
	if err == nil {
		t.Error("expected error for invalid severity")
	}
}

func TestParseEntry_InvalidStatus(t *testing.T) {
	content := `---
id: test-F-1
title: "test"
created: 2026-05-17
origin: some origin
severity: risk
review_by: 2026-07-16
status: pending
---
body
`
	_, err := ParseEntry(content)
	if err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestParseEntry_BlockScalarOrigin(t *testing.T) {
	// Block scalar origin (with | style) must round-trip without trailing newline noise
	content := `---
id: my-feature-F-1
title: "some title"
created: 2026-05-17
origin: |
  docs/spec/my-feature.md, iter 2 reviewer (CP-1). Deferred to project
  follow-ups at Phase 3 finalize 2026-05-17.
severity: nit
review_by: 2026-07-16
status: open
---

body text here
`
	e, err := ParseEntry(content)
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	// Origin should contain the multi-line content (trimmed)
	if !strings.Contains(e.Origin, "docs/spec/my-feature.md") {
		t.Errorf("origin missing expected text: %q", e.Origin)
	}
	if !strings.Contains(e.Origin, "iter 2 reviewer") {
		t.Errorf("origin missing second line: %q", e.Origin)
	}
}

func TestLoadEntries_Dir(t *testing.T) {
	entries, err := LoadEntries(filepath.Join("testdata", "entries"))
	if err != nil {
		t.Fatalf("LoadEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("got %d entries, want 3", len(entries))
	}
}

func TestLoadEntries_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	entries, err := LoadEntries(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestLoadEntries_MissingDir(t *testing.T) {
	entries, err := LoadEntries("/nonexistent/path/followups")
	if err == nil {
		t.Error("expected error for missing dir")
	}
	if entries != nil {
		t.Error("expected nil entries on error")
	}
}

// Fix 1: ISO date validation in ParseEntry

func TestParseEntry_InvalidCreatedDate(t *testing.T) {
	base := func(created string) string {
		return "---\nid: test-F-1\ntitle: \"test\"\ncreated: " + created + "\norigin: some origin\nseverity: risk\nreview_by: 2026-07-16\nstatus: open\n---\nbody\n"
	}

	cases := []struct {
		name    string
		created string
	}{
		{"not-a-date", "not-a-date"},
		{"wrong separator", "2026/05/17"},
		{"bad month/day", "2026-13-99"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseEntry(base(tc.created))
			if err == nil {
				t.Errorf("expected error for created=%q, got nil", tc.created)
			}
		})
	}
}

func TestParseEntry_InvalidReviewByDate(t *testing.T) {
	content := `---
id: test-F-1
title: "test"
created: 2026-05-17
origin: some origin
severity: risk
review_by: bogus
status: open
---
body
`
	_, err := ParseEntry(content)
	if err == nil {
		t.Error("expected error for review_by=bogus, got nil")
	}
}

// Fix 2: Missing required-field tests for created, origin, review_by

func TestParseEntry_MissingCreated(t *testing.T) {
	content := `---
id: test-F-1
title: "test"
origin: some origin
severity: risk
review_by: 2026-07-16
status: open
---
body
`
	_, err := ParseEntry(content)
	if err == nil {
		t.Error("expected error for missing created")
	}
}

func TestParseEntry_MissingOrigin(t *testing.T) {
	content := `---
id: test-F-1
title: "test"
created: 2026-05-17
severity: risk
review_by: 2026-07-16
status: open
---
body
`
	_, err := ParseEntry(content)
	if err == nil {
		t.Error("expected error for missing origin")
	}
}

func TestParseEntry_MissingReviewBy(t *testing.T) {
	content := `---
id: test-F-1
title: "test"
created: 2026-05-17
origin: some origin
severity: risk
status: open
---
body
`
	_, err := ParseEntry(content)
	if err == nil {
		t.Error("expected error for missing review_by")
	}
}

// Fix 3: LoadEntriesWithErrors

func TestLoadEntriesWithErrors_PartialResult(t *testing.T) {
	dir := t.TempDir()

	// Write 2 valid entry files
	valid1 := `---
id: test-F-1
title: "test one"
created: 2026-05-17
origin: some origin
severity: risk
review_by: 2026-07-16
status: open
---
body
`
	valid2 := `---
id: test-F-2
title: "test two"
created: 2026-05-17
origin: some origin
severity: nit
review_by: 2026-07-16
status: open
---
body
`
	malformed := "this is not valid frontmatter at all\n"

	if err := os.WriteFile(filepath.Join(dir, "valid1.md"), []byte(valid1), 0644); err != nil {
		t.Fatalf("write valid1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "valid2.md"), []byte(valid2), 0644); err != nil {
		t.Fatalf("write valid2: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte(malformed), 0644); err != nil {
		t.Fatalf("write bad: %v", err)
	}

	entries, errs, err := LoadEntriesWithErrors(dir)
	if err != nil {
		t.Fatalf("unexpected top-level error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2", len(entries))
	}
	if len(errs) != 1 {
		t.Errorf("got %d errs, want 1: %v", len(errs), errs)
	}
	if _, ok := errs["bad.md"]; !ok {
		t.Errorf("expected error keyed on bad.md, got keys: %v", errs)
	}
}

func TestLoadEntriesWithErrors_AllValid(t *testing.T) {
	dir := t.TempDir()
	valid := `---
id: test-F-1
title: "test"
created: 2026-05-17
origin: some origin
severity: risk
review_by: 2026-07-16
status: open
---
body
`
	if err := os.WriteFile(filepath.Join(dir, "valid.md"), []byte(valid), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	entries, errs, err := LoadEntriesWithErrors(dir)
	if err != nil {
		t.Fatalf("unexpected top-level error: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}
	if len(errs) != 0 {
		t.Errorf("expected empty errs, got: %v", errs)
	}
}

func TestLoadEntriesWithErrors_MissingDir(t *testing.T) {
	_, _, err := LoadEntriesWithErrors("/nonexistent/path/followups")
	if err == nil {
		t.Error("expected top-level error for missing dir")
	}
}
