package followups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdd_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	path, err := Add(dir, AddOpts{
		ID:       "my-finding-001",
		Title:    "Something broke in auth",
		Severity: "risk",
		Origin:   "Found during code review 2026-05-22.",
		Today:    today,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if path == "" {
		t.Fatal("Add returned empty path")
	}

	// File must exist and parse cleanly.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	e, err := ParseEntry(string(raw))
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	if e.ID != "my-finding-001" {
		t.Errorf("id=%q, want %q", e.ID, "my-finding-001")
	}
	if e.Severity != SeverityRisk {
		t.Errorf("severity=%q, want %q", e.Severity, SeverityRisk)
	}
	if e.Created != "2026-05-22" {
		t.Errorf("created=%q, want %q", e.Created, "2026-05-22")
	}
	expectedReviewBy := "2026-07-21" // 2026-05-22 + 60 days
	if e.ReviewBy != expectedReviewBy {
		t.Errorf("review_by=%q, want %q", e.ReviewBy, expectedReviewBy)
	}
	if e.Status != StatusOpen {
		t.Errorf("status=%q, want open", e.Status)
	}
}

func TestAdd_WithBody(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	path, err := Add(dir, AddOpts{
		ID:       "with-body-001",
		Title:    "Has body content",
		Severity: "nit",
		Origin:   "origin text",
		Body:     "This is the body content.\nWith multiple lines.\n",
		Today:    today,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	e, err := ParseEntry(string(raw))
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	if !strings.Contains(e.Body, "body content") {
		t.Errorf("body=%q, want it to contain body content", e.Body)
	}
}

func TestAdd_WithFile(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	path, err := Add(dir, AddOpts{
		ID:       "with-file-001",
		Title:    "Has file ref",
		Severity: "question",
		Origin:   "origin",
		File:     "atomic/internal/foo.go:42",
		Today:    today,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	e, err := ParseEntry(string(raw))
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	if e.File != "atomic/internal/foo.go:42" {
		t.Errorf("file=%q, want %q", e.File, "atomic/internal/foo.go:42")
	}
}

func TestAdd_ValidationErrors(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		opts    AddOpts
		wantErr string
	}{
		{
			name:    "empty id",
			opts:    AddOpts{Title: "t", Severity: "risk", Origin: "o", Today: today},
			wantErr: "id",
		},
		{
			name:    "invalid id characters",
			opts:    AddOpts{ID: "Has Spaces", Title: "t", Severity: "risk", Origin: "o", Today: today},
			wantErr: "id",
		},
		{
			name:    "empty title",
			opts:    AddOpts{ID: "ok-id", Severity: "risk", Origin: "o", Today: today},
			wantErr: "title",
		},
		{
			name:    "invalid severity",
			opts:    AddOpts{ID: "ok-id", Title: "t", Severity: "bad", Origin: "o", Today: today},
			wantErr: "severity",
		},
		{
			name:    "empty origin",
			opts:    AddOpts{ID: "ok-id", Title: "t", Severity: "risk", Today: today},
			wantErr: "origin",
		},
		{
			name:    "duplicate id",
			opts:    AddOpts{ID: "my-finding-001", Title: "t", Severity: "nit", Origin: "o", Today: today},
			wantErr: "already exists",
		},
	}

	// Pre-create one entry for duplicate test.
	if _, err := Add(dir, AddOpts{
		ID: "my-finding-001", Title: "existing", Severity: "risk", Origin: "o", Today: today,
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	for _, tc := range cases {
		tc.opts.Dir = dir
		_, err := Add(dir, tc.opts)
		if err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), tc.wantErr) {
			t.Errorf("%s: error=%q, want it to contain %q", tc.name, err.Error(), tc.wantErr)
		}
	}
}
