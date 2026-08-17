package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

func TestCheckInstall_pass(t *testing.T) {
	target := t.TempDir()

	for _, a := range embedded.Manifest() {
		installArtifact(t, target, a)
	}

	r := doctor.RunCheckInstall(target, target)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
}

// Nothing is missing here, so a content mismatch alone must stay at WARN.
func TestCheckInstall_warn_drift(t *testing.T) {
	target := t.TempDir()

	manifest := embedded.Manifest()
	for i, a := range manifest {
		if i == 0 {
			writeFile(t, filepath.Join(target, filepath.FromSlash(a.Target)), []byte("drift"))
		} else {
			installArtifact(t, target, a)
		}
	}

	r := doctor.RunCheckInstall(target, target)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("Detail is empty, want drift description")
	}
}

func TestCheckInstall_fail_missing(t *testing.T) {
	target := t.TempDir()

	manifest := embedded.Manifest()
	for i, a := range manifest {
		if i == 0 {
			continue
		}
		installArtifact(t, target, a)
	}

	r := doctor.RunCheckInstall(target, target)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}
}

func TestCheckInstall_skip_missing_dir(t *testing.T) {
	target := filepath.Join(t.TempDir(), "nonexistent")

	r := doctor.RunCheckInstall(target, target)
	if r.Severity != doctor.SKIP {
		t.Errorf("severity = %q, want SKIP; detail: %s", r.Severity, r.Detail)
	}
}

// The check compares manifest entries against disk, so atomic-owned state
// under .atomic/ has to stay invisible to it.
func TestCheckInstall_atomic_subtree_not_flagged(t *testing.T) {
	target := t.TempDir()

	for _, a := range embedded.Manifest() {
		installArtifact(t, target, a)
	}

	// Files claudeinstall creates that are not manifest entries.
	atomicFiles := []string{
		filepath.Join(target, ".atomic", "config.resolved.md"),
		filepath.Join(target, ".atomic", "config.toml"),
		filepath.Join(target, ".atomic", "backups", "2026-01-01T00-00-00Z", "CLAUDE.md"),
		filepath.Join(target, ".atomic", "proposed", "CLAUDE.md"),
	}
	for _, f := range atomicFiles {
		writeFile(t, f, []byte("# atomic-owned state"))
	}

	r := doctor.RunCheckInstall(target, target)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s (atomic subtree must not be flagged)", r.Severity, r.Detail)
	}
}

// TestCheckInstall_findings_drift: a drifted artifact must appear in Findings
// with prefix "drifted: " and Remediation must be set.
func TestCheckInstall_findings_drift(t *testing.T) {
	target := t.TempDir()

	manifest := embedded.Manifest()
	var driftedPath string
	for i, a := range manifest {
		if i == 0 {
			driftedPath = a.Target
			writeFile(t, filepath.Join(target, filepath.FromSlash(a.Target)), []byte("drift"))
		} else {
			installArtifact(t, target, a)
		}
	}

	r := doctor.RunCheckInstall(target, target)
	if r.Severity != doctor.WARN {
		t.Fatalf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}

	want := "drifted: " + driftedPath
	found := false
	for _, f := range r.Findings {
		if f == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Findings = %v; want entry %q", r.Findings, want)
	}
	if r.Remediation != "atomic claude update" {
		t.Errorf("Remediation = %q, want %q", r.Remediation, "atomic claude update")
	}
}

func TestCheckInstall_findings_missing(t *testing.T) {
	target := t.TempDir()

	manifest := embedded.Manifest()
	var missingPath string
	for i, a := range manifest {
		if i == 0 {
			missingPath = a.Target
			continue // do not install
		}
		installArtifact(t, target, a)
	}

	r := doctor.RunCheckInstall(target, target)
	if r.Severity != doctor.FAIL {
		t.Fatalf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}

	want := "missing: " + missingPath
	found := false
	for _, f := range r.Findings {
		if f == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Findings = %v; want entry %q", r.Findings, want)
	}
	if r.Remediation != "atomic claude update" {
		t.Errorf("Remediation = %q, want %q", r.Remediation, "atomic claude update")
	}
}

func TestCheckInstall_pass_no_findings(t *testing.T) {
	target := t.TempDir()
	for _, a := range embedded.Manifest() {
		installArtifact(t, target, a)
	}

	r := doctor.RunCheckInstall(target, target)
	if r.Severity != doctor.PASS {
		t.Fatalf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
	if len(r.Findings) != 0 {
		t.Errorf("Findings = %v, want empty", r.Findings)
	}
	if r.Remediation != "" {
		t.Errorf("Remediation = %q, want empty", r.Remediation)
	}
}

// claudeinstall.Diff compares an installed agent against the bundle content
// patched with its configured overrides, so config-to-disk drift is caught
// and repaired without a dedicated agent-aware check. Locks that contract.
func TestCheckInstall_agentOverrideDrift_detectAndRepair(t *testing.T) {
	target := t.TempDir()
	suppressClaudeinstallSeams(t)

	if _, err := claudeinstall.Install(target, target, false, claudeinstall.RealClock); err != nil {
		t.Fatalf("initial Install: %v", err)
	}
	if r := doctor.RunCheckInstall(target, target); r.Severity != doctor.PASS {
		t.Fatalf("severity after clean install = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}

	// The on-disk agent still carries un-patched bundle frontmatter.
	writeAgentOverride(t, target, "atomic-implementer", config.AgentOverride{Effort: "high"})

	r := doctor.RunCheckInstall(target, target)
	if r.Severity != doctor.WARN {
		t.Fatalf("severity after config drift = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	wantFinding := "drifted: agents/atomic-implementer.md"
	found := false
	for _, f := range r.Findings {
		if f == wantFinding {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Findings = %v; want entry %q", r.Findings, wantFinding)
	}

	// The same Install call `atomic doctor --fix` runs for this category.
	if _, err := claudeinstall.Install(target, target, false, claudeinstall.RealClock); err != nil {
		t.Fatalf("repair Install: %v", err)
	}
	if r := doctor.RunCheckInstall(target, target); r.Severity != doctor.PASS {
		t.Fatalf("severity after repair = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
}

// --- helpers ---

// writeAgentOverride writes a config.toml carrying one agent override.
func writeAgentOverride(t *testing.T, home, agentName string, ov config.AgentOverride) {
	t.Helper()
	cfg := config.Default()
	cfg.Claude.Agents = map[string]config.AgentOverride{agentName: ov}
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatalf("write override config: %v", err)
	}
}

// suppressClaudeinstallSeams no-ops the TTY-gated seams so Install can run
// unattended in a test binary.
func suppressClaudeinstallSeams(t *testing.T) {
	t.Helper()
	claudeinstall.ProfileRefresh = func(_, _ string, _ int) (bool, error) { return false, nil }
	claudeinstall.PruneConfirm = func(_ []string) (bool, error) { return false, nil }
	t.Cleanup(func() {
		claudeinstall.ProfileRefresh = claudeinstall.DefaultProfileRefresh
		claudeinstall.PruneConfirm = claudeinstall.DefaultPruneConfirm
	})
}

func installArtifact(t *testing.T, target string, a embedded.Artifact) {
	t.Helper()
	data, err := embedded.FS.ReadFile(a.Source)
	if err != nil {
		t.Fatalf("read embedded %s: %v", a.Source, err)
	}
	writeFile(t, filepath.Join(target, filepath.FromSlash(a.Target)), data)
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
