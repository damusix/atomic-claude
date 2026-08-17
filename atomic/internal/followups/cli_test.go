package followups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// cliTestRepo returns the repo root and its followups dir, clock already fixed.
func cliTestRepo(t *testing.T) (root, dir string, today time.Time) {
	t.Helper()
	tmp := t.TempDir()
	dir = filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today = time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	return tmp, dir, today
}

func TestCLIPath(t *testing.T) {
	root, _, _ := cliTestRepo(t)
	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{"path"}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 0 {
		t.Errorf("exit code=%d, want 0; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "followups") {
		t.Errorf("path output=%q, want it to contain 'followups'", out.String())
	}
}

// Run threads repoRoot through config.FollowupsDir, so a ".pi" harness moves
// followups off the .claude default.
func TestCLIPath_UnderNonDefaultHarnessDir(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	root := t.TempDir()
	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{"path"}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 0 {
		t.Errorf("exit code=%d, want 0; stderr=%s", code, errOut.String())
	}
	want := filepath.Join(root, ".pi", "project", "followups")
	if got := strings.TrimSpace(out.String()); got != want {
		t.Errorf("path output=%q, want %q", got, want)
	}
}

func TestCLIRender(t *testing.T) {
	root, dir, today := cliTestRepo(t)
	if _, err := Add(dir, AddOpts{ID: "r-001", Title: "Render test", Severity: "risk", Origin: "o", Today: today}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{"render"}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 0 {
		t.Errorf("exit code=%d, want 0; stderr=%s", code, errOut.String())
	}

	indexPath := filepath.Join(dir, "INDEX.md")
	if _, err := os.Stat(indexPath); err != nil {
		t.Errorf("INDEX.md not created after render: %v", err)
	}
}

func TestCLIAdd(t *testing.T) {
	root, dir, _ := cliTestRepo(t)
	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{
		"add",
		"--id", "new-entry",
		"--title", "New entry title",
		"--severity", "nit",
		"--origin", "From a test",
	}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 0 {
		t.Errorf("exit code=%d, want 0; stderr=%s", code, errOut.String())
	}

	if _, err := os.Stat(filepath.Join(dir, "new-entry.md")); err != nil {
		t.Errorf("entry file not created: %v", err)
	}
	if !strings.Contains(out.String(), "new-entry") {
		t.Errorf("stdout=%q, want it to contain 'new-entry'", out.String())
	}
}

func TestCLIAdd_ValidationFails(t *testing.T) {
	root, _, _ := cliTestRepo(t)
	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{"add", "--id", "ok-id"}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 1 {
		t.Errorf("exit code=%d, want 1", code)
	}
}

func TestCLIAdd_KindPlan(t *testing.T) {
	root, dir, _ := cliTestRepo(t)
	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{
		"add",
		"--id", "plan-entry",
		"--title", "A deferred plan",
		"--kind", "plan",
		"--origin", "Deferred during review",
	}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 0 {
		t.Errorf("exit code=%d, want 0; stderr=%s", code, errOut.String())
	}

	raw, err := os.ReadFile(filepath.Join(dir, "plan-entry.md"))
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	e, err := ParseEntry(string(raw))
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	if e.Kind != KindPlan {
		t.Errorf("kind=%q, want %q", e.Kind, KindPlan)
	}
	if e.Severity != "" {
		t.Errorf("severity=%q, want empty for plan", e.Severity)
	}
	if !strings.Contains(string(raw), "kind: plan") {
		t.Errorf("expected 'kind: plan' in frontmatter:\n%s", raw)
	}
}

func TestCLIAdd_KindPlan_SeverityStillOptional(t *testing.T) {
	root, _, _ := cliTestRepo(t)
	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{
		"add",
		"--id", "plan-with-sev",
		"--title", "plan with severity",
		"--kind", "plan",
		"--severity", "nit",
		"--origin", "o",
	}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 0 {
		t.Errorf("exit code=%d, want 0; stderr=%s", code, errOut.String())
	}
}

func TestCLIAdd_InvalidKind(t *testing.T) {
	root, _, _ := cliTestRepo(t)
	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{
		"add",
		"--id", "bad-kind-entry",
		"--title", "t",
		"--kind", "badvalue",
		"--severity", "nit",
		"--origin", "o",
	}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 1 {
		t.Errorf("exit code=%d, want 1", code)
	}
	if !strings.Contains(strings.ToLower(errOut.String()), "kind") {
		t.Errorf("stderr=%q, expected mention of 'kind'", errOut.String())
	}
}

// A bad --kind must report itself, not a missing --severity.
func TestCLIAdd_InvalidKindWithoutSeverity(t *testing.T) {
	root, _, _ := cliTestRepo(t)
	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{
		"add",
		"--id", "bad-kind-no-sev",
		"--title", "t",
		"--kind", "badvalue",
		"--origin", "o",
	}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 1 {
		t.Errorf("exit code=%d, want 1", code)
	}
	stderr := errOut.String()
	if !strings.Contains(strings.ToLower(stderr), "kind") {
		t.Errorf("stderr=%q, expected mention of 'kind'", stderr)
	}
	// Swapping the validation order would regress this.
	if strings.Contains(stderr, "missing required flag --severity") {
		t.Errorf("stderr reports missing-severity instead of invalid-kind: %q", stderr)
	}
}

func TestCLIAdd_FindingStillRequiresSeverity(t *testing.T) {
	root, _, _ := cliTestRepo(t)
	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{
		"add",
		"--id", "finding-no-sev",
		"--title", "t",
		"--kind", "finding",
		"--origin", "o",
	}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 1 {
		t.Errorf("exit code=%d, want 1 (severity required for finding)", code)
	}
}

func TestCLIList(t *testing.T) {
	root, dir, today := cliTestRepo(t)
	if _, err := Add(dir, AddOpts{ID: "list-r", Title: "List risk", Severity: "risk", Origin: "o", Today: today}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{"list"}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 0 {
		t.Errorf("exit code=%d, want 0; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "list-r") {
		t.Errorf("list output=%q, want it to contain 'list-r'", out.String())
	}
}

func TestCLIList_JSON(t *testing.T) {
	root, dir, today := cliTestRepo(t)
	if _, err := Add(dir, AddOpts{ID: "json-r", Title: "JSON risk", Severity: "risk", Origin: "o", Today: today}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{"list", "--json"}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 0 {
		t.Errorf("exit code=%d, want 0; stderr=%s", code, errOut.String())
	}
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "[") {
		t.Errorf("expected JSON array, got: %s", out.String())
	}
}

func TestCLIClose(t *testing.T) {
	root, dir, today := cliTestRepo(t)
	if _, err := Add(dir, AddOpts{ID: "to-close", Title: "To close", Severity: "risk", Origin: "o", Today: today}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{"close", "to-close"}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 0 {
		t.Errorf("exit code=%d, want 0; stderr=%s", code, errOut.String())
	}

	if _, err := os.Stat(filepath.Join(dir, "to-close.md")); err == nil {
		t.Error("expected to-close.md deleted, still exists")
	}
}

func TestCLIClose_MissingID(t *testing.T) {
	root, _, _ := cliTestRepo(t)
	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{"close", "no-such-id"}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 1 {
		t.Errorf("exit code=%d, want 1", code)
	}
}

func TestCLIUnknownVerb(t *testing.T) {
	root, _, _ := cliTestRepo(t)
	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{"bogus"}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 2 {
		t.Errorf("exit code=%d, want 2", code)
	}
}

func TestCLINoArgs(t *testing.T) {
	root, _, _ := cliTestRepo(t)
	var out strings.Builder
	var errOut strings.Builder
	code := Run([]string{}, root, &out, &errOut, nowFixed(2026, 5, 22))
	if code != 2 {
		t.Errorf("exit code=%d, want 2", code)
	}
}

func nowFixed(year, month, day int) func() time.Time {
	return func() time.Time {
		return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	}
}
