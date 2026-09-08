package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

func installStyleFileAt(t *testing.T, claudeDir string) {
	t.Helper()
	stylePath := filepath.Join(claudeDir, hooks.OutputStyleRelPath)
	if err := os.MkdirAll(filepath.Dir(stylePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stylePath, []byte("# Atomic"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSeedFlag(t *testing.T, home string, enabled bool) {
	t.Helper()
	cfg := config.Default()
	value := "false"
	if enabled {
		value = "true"
	}
	if err := config.Set(cfg, "output_style.seed", value); err != nil {
		t.Fatal(err)
	}
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultOutputStyleRepair_SeedFalse_NoWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installStyleFileAt(t, filepath.Join(home, ".claude"))
	writeSeedFlag(t, home, false)

	var out strings.Builder
	if err := defaultOutputStyleRepair(&out); err != errNonFixable {
		t.Fatalf("defaultOutputStyleRepair err = %v, want errNonFixable", err)
	}

	value, present, err := hooks.ReadOutputStyle(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("ReadOutputStyle: %v", err)
	}
	if present {
		t.Errorf("outputStyle written despite seed=false: %q", value)
	}
	if !strings.Contains(out.String(), "no-op") {
		t.Errorf("expected the no-op reason in output, got:\n%s", out.String())
	}
}

// stubPrompter never confirms; it exists so the non-fixable path below can
// prove Confirm is never even reached — repairPlan filters it out first.
type stubPrompter struct{}

func (stubPrompter) Confirm(string) Decision { return DecisionYes }
func (stubPrompter) Indexed([]string) int    { return 0 }

func TestRepair_OutputStyleSeedFalse_NoApplyNoFixedClaim(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	installStyleFileAt(t, claudeDir)
	writeSeedFlag(t, home, false)

	check := RunCheckOutputStyleWith(claudeDir, home, "")
	if check.Severity != WARN {
		t.Fatalf("check severity = %v, want WARN; detail: %q", check.Severity, check.Detail)
	}

	rp := DefaultRepairer()
	var out strings.Builder
	summary := rp.Repair(
		[]Result{{Index: 14, Name: "output-style", Severity: check.Severity, Detail: check.Detail}},
		Opts{Fix: true}, stubPrompter{}, &out,
	)

	if summary.Applied != 0 {
		t.Errorf("Applied = %d, want 0", summary.Applied)
	}
	if summary.NonFixable != 1 {
		t.Errorf("NonFixable = %d, want 1", summary.NonFixable)
	}
	if strings.Contains(out.String(), "✓ fixed") {
		t.Errorf("output claims success despite seed=false, got:\n%s", out.String())
	}

	value, present, err := hooks.ReadOutputStyle(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatalf("ReadOutputStyle: %v", err)
	}
	if present {
		t.Errorf("outputStyle written despite seed=false: %q", value)
	}
}

func TestDefaultOutputStyleRepair_SeedTrue_KeyAbsent_Writes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installStyleFileAt(t, filepath.Join(home, ".claude"))
	writeSeedFlag(t, home, true)

	var out strings.Builder
	if err := defaultOutputStyleRepair(&out); err != nil {
		t.Fatalf("defaultOutputStyleRepair: %v", err)
	}

	value, present, err := hooks.ReadOutputStyle(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("ReadOutputStyle: %v", err)
	}
	if !present || value != hooks.OutputStyleName {
		t.Errorf("outputStyle = %q, present=%v; want %q, true", value, present, hooks.OutputStyleName)
	}
}
