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

func TestAdd_KindPlan_SeverityOptional(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	path, err := Add(dir, AddOpts{
		ID:     "spec-plan-001",
		Title:  "Write spec for X",
		Kind:   "plan",
		Origin: "Deferred during review.",
		Today:  today,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// File must parse cleanly.
	e, err := ParseEntry(string(raw))
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	if e.Kind != KindPlan {
		t.Errorf("kind=%q, want %q", e.Kind, KindPlan)
	}
	// Frontmatter must contain kind: plan.
	if !strings.Contains(string(raw), "kind: plan") {
		t.Errorf("expected 'kind: plan' in frontmatter:\n%s", raw)
	}
	// Severity must not be set.
	if e.Severity != "" {
		t.Errorf("severity=%q, want empty for plan", e.Severity)
	}
}

func TestAdd_KindPlan_WithFile(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	path, err := Add(dir, AddOpts{
		ID:     "spec-plan-002",
		Title:  "Write spec for Y",
		Kind:   "plan",
		Origin: "Deferred during review.",
		File:   "docs/spec/y.md",
		Today:  today,
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
	if e.File != "docs/spec/y.md" {
		t.Errorf("file=%q, want %q", e.File, "docs/spec/y.md")
	}
}

func TestAdd_KindFinding_SeverityRequired(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	// Omit severity with kind=finding → must fail.
	_, err := Add(dir, AddOpts{
		ID:     "finding-no-sev",
		Title:  "missing severity",
		Kind:   "finding",
		Origin: "o",
		Today:  today,
	})
	if err == nil {
		t.Error("expected error when severity omitted for finding, got nil")
	}
}

func TestAdd_InvalidKind(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	_, err := Add(dir, AddOpts{
		ID:       "bad-kind",
		Title:    "t",
		Kind:     "invalid",
		Severity: "risk",
		Origin:   "o",
		Today:    today,
	})
	if err == nil {
		t.Error("expected error for invalid kind, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "kind") {
		t.Errorf("error=%q, want it to mention 'kind'", err.Error())
	}
}

func TestAdd_DefaultKindIsFinding(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	// Omit Kind field → should default to finding.
	path, err := Add(dir, AddOpts{
		ID:       "default-kind-001",
		Title:    "default kind test",
		Severity: "nit",
		Origin:   "o",
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
	if e.Kind != KindFinding {
		t.Errorf("kind=%q, want %q (default)", e.Kind, KindFinding)
	}
}

// CP2 F-1: missing severity error must not double-wrap.
func TestAdd_MissingSeverityErrorSingleWrapped(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	_, err := Add(dir, AddOpts{
		ID:     "finding-no-sev-f1",
		Title:  "t",
		Kind:   "finding",
		Origin: "o",
		Today:  today,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	// Must not contain the double-prefix "followups add: followups: missing".
	if strings.Contains(msg, "followups add: followups:") {
		t.Errorf("error is double-wrapped: %q", msg)
	}
	// Must still say something about severity.
	if !strings.Contains(strings.ToLower(msg), "severity") {
		t.Errorf("error should mention severity: %q", msg)
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
