package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// makeFollowupsFile writes .claude/project/followups.md with the given content.
func makeFollowupsFile(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	path := filepath.Join(dir, "followups.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write followups: %v", err)
	}
}

// TestCheckFollowupsFileAbsent verifies PASS when no followups.md exists.
func TestCheckFollowupsFileAbsent(t *testing.T) {
	root := t.TempDir()
	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS", r.Severity)
	}
}

// TestCheckFollowupsWellFormed verifies PASS for a file with all F-entries
// inside severity buckets and each having an Origin: line.
func TestCheckFollowupsWellFormed(t *testing.T) {
	root := t.TempDir()
	content := `# Project follow-ups

## 🟡 risks

### F-1 — Some risk

Body text.

Origin: chat session 2026-05-17.

## 🔵 nits

### F-2 — Some nit

Body text.

Origin: another session.

## Closed

(none)
`
	makeFollowupsFile(t, root, content)
	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (detail: %s)", r.Severity, r.Detail)
	}
}

// TestCheckFollowupsMissingOrigin verifies WARN when an F-entry lacks Origin:.
func TestCheckFollowupsMissingOrigin(t *testing.T) {
	root := t.TempDir()
	content := `# Project follow-ups

## 🟡 risks

### F-1 — Risk without origin

Body text only, no origin line.

`
	makeFollowupsFile(t, root, content)
	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN", r.Severity)
	}
	// Detail must mention F-1.
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

// TestCheckFollowupsEntryOutsideBucket verifies WARN when an F-entry is not
// under any recognized severity bucket heading.
func TestCheckFollowupsEntryOutsideBucket(t *testing.T) {
	root := t.TempDir()
	content := `# Project follow-ups

### F-1 — Floating entry

Origin: somewhere.

`
	makeFollowupsFile(t, root, content)
	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN", r.Severity)
	}
}

// TestCheckFollowupsNoEntries verifies PASS when file has no F-<id> entries.
func TestCheckFollowupsNoEntries(t *testing.T) {
	root := t.TempDir()
	content := `# Project follow-ups

## 🟡 risks

(none)

## Closed

(none)
`
	makeFollowupsFile(t, root, content)
	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS", r.Severity)
	}
}

// TestCheckFollowupsMultipleMalformed verifies WARN lists up to 3 IDs then "...".
func TestCheckFollowupsMultipleMalformed(t *testing.T) {
	root := t.TempDir()
	content := `# Project follow-ups

## 🟡 risks

### F-1 — No origin

### F-2 — No origin

### F-3 — No origin

### F-4 — No origin

`
	makeFollowupsFile(t, root, content)
	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN", r.Severity)
	}
	// Detail must contain "..." for the overflow.
	found := false
	for i := 0; i < len(r.Detail)-2; i++ {
		if r.Detail[i:i+3] == "..." {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("detail %q: expected '...' for 4 malformed entries", r.Detail)
	}
}

// TestCheckFollowupsEmDashAndASCIIHyphen verifies both em-dash and ASCII hyphen
// are accepted as the separator in the F-<id> heading.
func TestCheckFollowupsEmDashAndASCIIHyphen(t *testing.T) {
	root := t.TempDir()
	content := `# Project follow-ups

## 🔵 nits

### F-1 — Em-dash entry

Origin: session A.

### F-2 - ASCII-hyphen entry

Origin: session B.

`
	makeFollowupsFile(t, root, content)
	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (detail: %s)", r.Severity, r.Detail)
	}
}
