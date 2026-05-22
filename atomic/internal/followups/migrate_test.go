package followups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixturePath returns the path to the testdata/migrate/followups.md fixture.
func fixturePath() string {
	return filepath.Join("testdata", "migrate", "followups.md")
}

func TestMigrateParseBlocks(t *testing.T) {
	raw, err := os.ReadFile(fixturePath())
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	blocks, err := parseLegacyBlocks(string(raw))
	if err != nil {
		t.Fatalf("parseLegacyBlocks: %v", err)
	}

	// The fixture has:
	// risks: atomic-doctor-F-1..F-4, install-output-style-F-1, F-2, F-5 = 7 open risks
	// nits: F-1 (bare), F-2 (closed), F-3 (bare), F-4 (bare) = 4 nits
	// questions: (none header only)
	if len(blocks) == 0 {
		t.Fatal("expected at least one block, got 0")
	}

	// Should have found at least 10 blocks (7 risks + 3 open nits + the closed nit)
	if len(blocks) < 10 {
		t.Errorf("expected >= 10 blocks, got %d", len(blocks))
	}
}

func TestMigrateIDFromHeader(t *testing.T) {
	cases := []struct {
		header     string
		bucket     string
		wantID     string
		wantClosed bool
	}{
		{
			// Prefixed ids are normalized to lowercase kebab-case.
			header:     "atomic-doctor-F-1 — `bundlemirror.Run` double-reads files via path reconstruction",
			bucket:     "🟡 risks",
			wantID:     "atomic-doctor-f-1",
			wantClosed: false,
		},
		{
			// Bare F-N synthesizes bucket-slug + first-4-words + F-N (all lowercase).
			header:     "F-1 — Encode skill trigger boundary in atomic-tdd and atomic-debug descriptions",
			bucket:     "🔵 nits",
			wantID:     "nits-encode-skill-trigger-boundary-f-1",
			wantClosed: false,
		},
		{
			// Closed marker detected; title first 4 words: "Design and decide on".
			header:     "F-2 — Design and decide on `atomic doctor` CLI subcommand *(closed 2026-05-17 — dbe2a53)*",
			bucket:     "🔵 nits",
			wantID:     "nits-design-and-decide-on-f-2",
			wantClosed: true,
		},
		{
			header:     "install-output-style-F-1 — `prompt.Confirm` default-value plumbing is untested",
			bucket:     "🟡 risks",
			wantID:     "install-output-style-f-1",
			wantClosed: false,
		},
	}

	for _, tc := range cases {
		id, closed := idFromHeader(tc.header, tc.bucket)
		if id != tc.wantID {
			t.Errorf("header=%q bucket=%q: id=%q, want %q", tc.header, tc.bucket, id, tc.wantID)
		}
		if closed != tc.wantClosed {
			t.Errorf("header=%q: closed=%v, want %v", tc.header, closed, tc.wantClosed)
		}
	}
}

func TestMigrateSeverityFromBucket(t *testing.T) {
	cases := []struct {
		bucket string
		want   Severity
	}{
		{"## 🟡 risks", SeverityRisk},
		{"## 🔵 nits", SeverityNit},
		{"## ❓ questions", SeverityQuestion},
		{"🟡 risks", SeverityRisk},
		{"🔵 nits", SeverityNit},
		{"❓ questions", SeverityQuestion},
	}
	for _, tc := range cases {
		got := severityFromBucket(tc.bucket)
		if got != tc.want {
			t.Errorf("bucket=%q: got %q, want %q", tc.bucket, got, tc.want)
		}
	}
}

func TestMigrateExtractOriginDate(t *testing.T) {
	cases := []struct {
		origin string
		want   string // YYYY-MM-DD or "" (today)
	}{
		{
			origin: "docs/spec/atomic-doctor.md, iter 3 reviewer (CP-2). Deferred to project followups at Phase 3 finalize 2026-05-17.",
			want:   "2026-05-17",
		},
		{
			origin: "chat session 2026-05-21 audit review.",
			want:   "2026-05-21",
		},
		{
			origin: "no date here",
			want:   "",
		},
	}
	for _, tc := range cases {
		got := extractOriginDate(tc.origin)
		if got != tc.want {
			t.Errorf("origin=%q: got %q, want %q", tc.origin, got, tc.want)
		}
	}
}

func TestMigrateExtractFileRef(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{
			body: "`atomic/internal/bundlemirror/mirror.go:196-216`\n\nSome text.",
			want: "atomic/internal/bundlemirror/mirror.go:196-216",
		},
		{
			body: "No file ref here, just text.",
			want: "",
		},
		{
			body: "`atomic/internal/doctor/fix.go:41-98`\n",
			want: "atomic/internal/doctor/fix.go:41-98",
		},
	}
	for _, tc := range cases {
		got := extractFileRef(tc.body)
		if got != tc.want {
			t.Errorf("body=%q: got %q, want %q", tc.body, got, tc.want)
		}
	}
}

func TestMigrateExtractOrigin(t *testing.T) {
	body := `Some content here.

Origin: docs/spec/atomic-doctor.md, iter 3 reviewer (CP-2). Deferred to project
followups at Phase 3 finalize 2026-05-17.

More content after.`

	got := extractOrigin(body)
	if !strings.Contains(got, "docs/spec/atomic-doctor.md") {
		t.Errorf("expected origin to contain source, got %q", got)
	}
}

func TestMigrateRun_Integration(t *testing.T) {
	// Integration test: run migration in a tmpdir, verify outputs.
	tmp := t.TempDir()

	// Copy fixture into tmp.
	raw, err := os.ReadFile(fixturePath())
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	claudeDir := filepath.Join(tmp, ".claude", "project")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	srcPath := filepath.Join(claudeDir, "followups.md")
	if err := os.WriteFile(srcPath, raw, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	if err := Migrate(tmp, today); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// followups.md should be deleted.
	if _, err := os.Stat(srcPath); err == nil {
		t.Error("expected followups.md to be deleted, still exists")
	}

	// followups/ dir should exist.
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("followups/ dir not created: %v", err)
	}

	// INDEX.md should exist.
	indexPath := filepath.Join(dir, "INDEX.md")
	if _, err := os.Stat(indexPath); err != nil {
		t.Errorf("INDEX.md not created: %v", err)
	}

	// CLOSED.md should have at least one entry (F-2 nit is closed).
	closedPath := filepath.Join(dir, "CLOSED.md")
	closedRaw, err := os.ReadFile(closedPath)
	if err != nil {
		t.Fatalf("CLOSED.md not created: %v", err)
	}
	// The closed nit (F-2) is synthesized to nits-design-and-decide-on-f-2.
	if !strings.Contains(string(closedRaw), "nits-design-and-decide-on-f-2") {
		t.Errorf("CLOSED.md should contain synthesized id for closed nit F-2, got:\n%s", string(closedRaw))
	}

	// Open entries for the risks bucket (ids normalized to lowercase).
	wantOpen := []string{
		"atomic-doctor-f-1.md",
		"atomic-doctor-f-2.md",
		"atomic-doctor-f-3.md",
		"atomic-doctor-f-4.md",
		"install-output-style-f-1.md",
		"install-output-style-f-2.md",
		"install-output-style-f-5.md",
	}
	for _, name := range wantOpen {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected entry file %s, not found", name)
		}
	}

	// Bare nit F-1 should be synthesized.
	entries, err := LoadEntries(dir)
	if err != nil {
		t.Fatalf("LoadEntries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries loaded after migration")
	}

	// All entries must parse cleanly (severity, review_by, etc.)
	for _, e := range entries {
		if e.Severity == "" {
			t.Errorf("entry %s has empty severity", e.ID)
		}
		if e.ReviewBy == "" {
			t.Errorf("entry %s has empty review_by", e.ID)
		}
		if e.Created == "" {
			t.Errorf("entry %s has empty created", e.ID)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	// Idempotent: if followups/ exists and followups.md absent → no-op exit 0.
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	// Should not error.
	if err := Migrate(tmp, today); err != nil {
		t.Fatalf("Migrate idempotent: %v", err)
	}
}

func TestMigrateErrorZeroEntries(t *testing.T) {
	// Zero entries parsed → exit 2 (ErrMigrateRefused).
	tmp := t.TempDir()
	claudeDir := filepath.Join(tmp, ".claude", "project")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write a file with no H3 sections.
	srcPath := filepath.Join(claudeDir, "followups.md")
	if err := os.WriteFile(srcPath, []byte("# Project follow-ups\n\nNo entries.\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	err := Migrate(tmp, today)
	if err == nil {
		t.Fatal("expected error for zero entries, got nil")
	}
	if !strings.Contains(err.Error(), "zero entries") {
		t.Errorf("expected 'zero entries' in error, got: %v", err)
	}
}
