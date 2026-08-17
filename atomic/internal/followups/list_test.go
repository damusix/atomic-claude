package followups

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupListDir(t *testing.T) (string, time.Time) {
	t.Helper()
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	if _, err := Add(dir, AddOpts{
		ID: "risk-001", Title: "Risk one", Severity: "risk", Origin: "o", Today: today,
	}); err != nil {
		t.Fatalf("Add risk: %v", err)
	}
	staleEntry := `---
id: stale-nit
title: Stale nit
created: 2026-01-01
origin: |
  origin
severity: nit
review_by: 2026-03-01
status: open
---

Body.
`
	if err := os.WriteFile(filepath.Join(dir, "stale-nit.md"), []byte(staleEntry), 0o644); err != nil {
		t.Fatalf("write stale-nit: %v", err)
	}
	if _, err := Add(dir, AddOpts{
		ID: "q-001", Title: "Question one", Severity: "question", Origin: "o", Today: today,
	}); err != nil {
		t.Fatalf("Add question: %v", err)
	}

	return dir, today
}

func TestListEntries_All(t *testing.T) {
	dir, today := setupListDir(t)

	entries, err := ListEntries(dir, ListOpts{Today: today})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestListEntries_StaleOnly(t *testing.T) {
	dir, today := setupListDir(t)

	entries, err := ListEntries(dir, ListOpts{StaleOnly: true, Today: today})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 stale entry, got %d", len(entries))
	}
	if entries[0].ID != "stale-nit" {
		t.Errorf("stale entry id=%q, want %q", entries[0].ID, "stale-nit")
	}
}

func TestListEntries_EmptyDir(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	entries, err := ListEntries(dir, ListOpts{Today: today})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestFormatListHuman(t *testing.T) {
	dir, today := setupListDir(t)
	entries, _ := ListEntries(dir, ListOpts{Today: today})

	out := FormatListHuman(entries, today)
	if !strings.Contains(out, "risk-001") {
		t.Errorf("output should contain risk-001, got:\n%s", out)
	}
	if !strings.Contains(out, "risks") {
		t.Errorf("output should contain risks bucket header, got:\n%s", out)
	}
	if !strings.Contains(out, "stale") {
		t.Errorf("output should mark stale entry, got:\n%s", out)
	}
}

// A plan stays out of --stale even with review_by in the past.
func TestListEntries_PlanNotInStale(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	stalePlan := `---
id: old-plan
title: Old plan
created: 2026-01-01
origin: |
  origin
kind: plan
review_by: 2026-03-01
status: open
---

Body.
`
	if err := os.WriteFile(filepath.Join(dir, "old-plan.md"), []byte(stalePlan), 0o644); err != nil {
		t.Fatalf("write old-plan: %v", err)
	}

	staleFinding := `---
id: stale-finding
title: Stale finding
created: 2026-01-01
origin: |
  origin
severity: risk
review_by: 2026-03-01
status: open
---

Body.
`
	if err := os.WriteFile(filepath.Join(dir, "stale-finding.md"), []byte(staleFinding), 0o644); err != nil {
		t.Fatalf("write stale-finding: %v", err)
	}

	entries, err := ListEntries(dir, ListOpts{StaleOnly: true, Today: today})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 stale entry (the finding), got %d", len(entries))
	}
	if len(entries) == 1 && entries[0].ID != "stale-finding" {
		t.Errorf("stale entry id=%q, want 'stale-finding'", entries[0].ID)
	}
}

func TestFormatListJSON(t *testing.T) {
	dir, today := setupListDir(t)
	entries, _ := ListEntries(dir, ListOpts{Today: today})

	out, err := FormatListJSON(entries)
	if err != nil {
		t.Fatalf("FormatListJSON: %v", err)
	}

	var parsed []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, out)
	}
	if len(parsed) != 3 {
		t.Errorf("expected 3 JSON entries, got %d", len(parsed))
	}
	for _, m := range parsed {
		if _, ok := m["id"]; !ok {
			t.Errorf("JSON entry missing 'id' field: %v", m)
		}
	}
}

// Every --json entry carries a kind.
func TestFormatListJSON_KindField(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	if _, err := Add(dir, AddOpts{
		ID: "f-001", Title: "Finding entry", Severity: "risk", Origin: "o", Today: today,
	}); err != nil {
		t.Fatalf("Add finding: %v", err)
	}

	planEntry := `---
id: p-001
title: Plan entry
created: 2026-05-22
origin: |
  o
kind: plan
review_by: 2026-12-31
status: open
---

Body.
`
	if err := os.WriteFile(filepath.Join(dir, "p-001.md"), []byte(planEntry), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	entries, err := ListEntries(dir, ListOpts{Today: today})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}

	out, err := FormatListJSON(entries)
	if err != nil {
		t.Fatalf("FormatListJSON: %v", err)
	}

	var parsed []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, out)
	}

	byID := make(map[string]map[string]interface{}, len(parsed))
	for _, m := range parsed {
		id, _ := m["id"].(string)
		byID[id] = m
	}

	finding, ok := byID["f-001"]
	if !ok {
		t.Fatal("f-001 not found in JSON output")
	}
	if kind, _ := finding["kind"].(string); kind != "finding" {
		t.Errorf("f-001 kind=%q, want %q", kind, "finding")
	}

	plan, ok := byID["p-001"]
	if !ok {
		t.Fatal("p-001 not found in JSON output")
	}
	if kind, _ := plan["kind"].(string); kind != "plan" {
		t.Errorf("p-001 kind=%q, want %q", kind, "plan")
	}
}
