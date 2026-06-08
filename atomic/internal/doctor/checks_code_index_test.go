package doctor_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// makeIndexDB creates the minimal directory structure and a zero-byte DB file
// at the canonical path (<root>/.claude/.atomic-index/atomic.db) with the
// given mtime. It does NOT open SQLite — the doctor check only stat-inspects
// the file, never reads it.
func makeIndexDB(t *testing.T, root string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(root, ".claude", ".atomic-index")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("makeIndexDB mkdir: %v", err)
	}
	dbPath := filepath.Join(dir, "atomic.db")
	if err := os.WriteFile(dbPath, []byte{}, 0o644); err != nil {
		t.Fatalf("makeIndexDB write: %v", err)
	}
	if err := os.Chtimes(dbPath, mtime, mtime); err != nil {
		t.Fatalf("makeIndexDB chtimes: %v", err)
	}
}

// TestCheckCodeIndexAbsent is the key spec assertion: absence of the DB must
// produce PASS (informational), never WARN.
//
// The index is opt-in; millions of repos that never run `atomic code index`
// must not see a persistent WARN on every `atomic doctor` run.
func TestCheckCodeIndexAbsent(t *testing.T) {
	root := t.TempDir()
	// No index directory, no DB file.
	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS when index absent (must never be WARN)", r.Severity)
	}
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

// TestCheckCodeIndexFresh verifies PASS when the DB exists and is within the
// staleness threshold.
func TestCheckCodeIndexFresh(t *testing.T) {
	root := t.TempDir()
	// mtime = 2 days ago, threshold = 7
	mtime := time.Now().Add(-2 * 24 * time.Hour)
	makeIndexDB(t, root, mtime)

	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (fresh index, detail: %s)", r.Severity, r.Detail)
	}
}

// TestCheckCodeIndexStale verifies WARN when the DB exists but is older than
// the staleness threshold.
func TestCheckCodeIndexStale(t *testing.T) {
	root := t.TempDir()
	// mtime = 10 days ago, threshold = 7
	mtime := time.Now().Add(-10 * 24 * time.Hour)
	makeIndexDB(t, root, mtime)

	r := doctor.RunCheckCodeIndexWith(root, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (stale index)", r.Severity)
	}
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

// TestCheckCodeIndexNeverFail asserts the check never produces FAIL regardless
// of how broken the environment is. The code index is opt-in and optional; it
// must never be a hard installation requirement.
func TestCheckCodeIndexNeverFail(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{
			name:  "absent",
			setup: func(t *testing.T, root string) {},
		},
		{
			name: "stale",
			setup: func(t *testing.T, root string) {
				mtime := time.Now().Add(-30 * 24 * time.Hour)
				makeIndexDB(t, root, mtime)
			},
		},
		{
			name: "fresh",
			setup: func(t *testing.T, root string) {
				mtime := time.Now().Add(-1 * 24 * time.Hour)
				makeIndexDB(t, root, mtime)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)
			r := doctor.RunCheckCodeIndexWith(root, 7)
			if r.Severity == doctor.FAIL {
				t.Errorf("severity = FAIL, want PASS or WARN (code index check must never FAIL)")
			}
		})
	}
}

// TestCheckCodeIndexStaleDaysRespected verifies the threshold is honoured.
// A 3-day-old DB is PASS at threshold=7 but WARN at threshold=2.
func TestCheckCodeIndexStaleDaysRespected(t *testing.T) {
	root := t.TempDir()
	mtime := time.Now().Add(-3 * 24 * time.Hour)
	makeIndexDB(t, root, mtime)

	r7 := doctor.RunCheckCodeIndexWith(root, 7)
	if r7.Severity != doctor.PASS {
		t.Errorf("threshold=7: severity = %v, want PASS", r7.Severity)
	}

	r2 := doctor.RunCheckCodeIndexWith(root, 2)
	if r2.Severity != doctor.WARN {
		t.Errorf("threshold=2: severity = %v, want WARN", r2.Severity)
	}
}
