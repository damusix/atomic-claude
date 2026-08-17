package claudeinstall_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

func TestPruneDiff_StaleEntry(t *testing.T) {
	stored := []string{"agents/old-agent.md", "commands/commit.md"}
	current := map[string]bool{"commands/commit.md": true} // old-agent.md gone

	stale := claudeinstall.PruneDiff(stored, current)
	if len(stale) != 1 || stale[0] != "agents/old-agent.md" {
		t.Errorf("PruneDiff = %v, want [agents/old-agent.md]", stale)
	}
}

func TestPruneDiff_NothingStale(t *testing.T) {
	stored := []string{"agents/foo.md", "commands/bar.md"}
	current := map[string]bool{"agents/foo.md": true, "commands/bar.md": true}

	stale := claudeinstall.PruneDiff(stored, current)
	if len(stale) != 0 {
		t.Errorf("PruneDiff = %v, want empty", stale)
	}
}

func TestPruneDiff_EmptyStored(t *testing.T) {
	stale := claudeinstall.PruneDiff([]string{}, map[string]bool{"agents/foo.md": true})
	if len(stale) != 0 {
		t.Errorf("PruneDiff with empty stored = %v, want nil", stale)
	}
}

func TestPruneDiff_NilStored(t *testing.T) {
	stale := claudeinstall.PruneDiff(nil, map[string]bool{"agents/foo.md": true})
	if len(stale) != 0 {
		t.Errorf("PruneDiff with nil stored = %v, want nil", stale)
	}
}

// A user-added file absent from stored is never returned, regardless of what's on disk.
func TestPruneDiff_UserAddedFileNotReturned(t *testing.T) {
	stored := []string{"agents/atomic-foo.md"}
	current := map[string]bool{"agents/atomic-foo.md": true}

	stale := claudeinstall.PruneDiff(stored, current)
	for _, s := range stale {
		if s == "agents/mine.md" {
			t.Errorf("PruneDiff returned user-added file agents/mine.md — must never happen")
		}
	}
	if len(stale) != 0 {
		t.Errorf("PruneDiff = %v, want empty", stale)
	}
}

func TestPruneDiff_MultipleStale(t *testing.T) {
	stored := []string{"agents/a.md", "agents/b.md", "commands/c.md"}
	current := map[string]bool{} // everything gone

	stale := claudeinstall.PruneDiff(stored, current)
	if len(stale) != 3 {
		t.Errorf("PruneDiff = %v, want 3 stale entries", stale)
	}
}

func TestInstallWritesManifestToConfig(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	cfgPath := config.TOMLPath(target)
	cfg, warns, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected config warnings: %v", warns)
	}

	// Test binaries build as "dev", which must never reach Install.Version; a
	// real release build still records its version normally.
	if version.Version == "dev" {
		if cfg.Install.Version != "" {
			t.Errorf("Install.Version = %q, want empty for dev build", cfg.Install.Version)
		}
	} else if cfg.Install.Version != version.Version {
		t.Errorf("Install.Version = %q, want %q", cfg.Install.Version, version.Version)
	}

	if len(cfg.Install.Artifacts.Agents) == 0 {
		t.Error("Install.Artifacts.Agents is empty — expected at least one agent")
	}
	if len(cfg.Install.Artifacts.Commands) == 0 {
		t.Error("Install.Artifacts.Commands is empty — expected at least one command")
	}
	if len(cfg.Install.Artifacts.Skills) == 0 {
		t.Error("Install.Artifacts.Skills is empty — expected at least one skill")
	}
	if len(cfg.Install.Artifacts.OutputStyles) == 0 {
		t.Error("Install.Artifacts.OutputStyles is empty — expected at least one output style")
	}
	if len(cfg.Install.Artifacts.Rules) == 0 {
		t.Error("Install.Artifacts.Rules is empty — expected at least one rule")
	}

	found := false
	for _, a := range cfg.Install.Artifacts.Agents {
		if a == "agents/atomic-implementer.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("agents/atomic-implementer.md not in Install.Artifacts.Agents: %v", cfg.Install.Artifacts.Agents)
	}
}

// Writing the un-ldflagged "dev" default into Install.Version fails Validate's
// semver check and permanently reds `atomic doctor` for every dev-build install.
func TestInstallDoesNotRecordDevVersion(t *testing.T) {
	if version.Version != "dev" {
		t.Skipf("test binary built with -ldflags version=%q; dev-version behavior not exercised", version.Version)
	}

	target := t.TempDir()
	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	cfg, _, err := config.Load(config.TOMLPath(target))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if err := config.Validate(cfg); err != nil {
		t.Errorf("config.Validate after dev-build install: %v", err)
	}
	if cfg.Install.Version != "" {
		t.Errorf("Install.Version = %q, want empty (dev builds do not record a version)", cfg.Install.Version)
	}
}

func TestInstallManifestRoundTrip(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("second Install: %v", err)
	}

	cfgPath := config.TOMLPath(target)
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load after second install: %v", err)
	}
	if version.Version == "dev" {
		if cfg.Install.Version != "" {
			t.Errorf("Install.Version after second install = %q, want empty for dev build", cfg.Install.Version)
		}
	} else if cfg.Install.Version != version.Version {
		t.Errorf("Install.Version after second install = %q, want %q", cfg.Install.Version, version.Version)
	}
	if len(cfg.Install.Artifacts.Agents) == 0 {
		t.Error("Install.Artifacts.Agents empty after second install")
	}
}

func TestInstallDryRunDoesNotWriteManifest(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, target, true, fixedClock); err != nil {
		t.Fatalf("Install dry-run: %v", err)
	}

	cfgPath := config.TOMLPath(target)
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Errorf("dry-run wrote config.toml — must not write on dry-run")
	}
}

func TestPruneRemovesStaleFile(t *testing.T) {
	target := t.TempDir()

	claudeinstall.PruneConfirm = func(stale []string) (bool, error) { return true, nil }
	t.Cleanup(func() { claudeinstall.PruneConfirm = claudeinstall.DefaultPruneConfirm })

	staleTarget := "agents/old-agent-not-in-bundle.md"
	stalePath := filepath.Join(target, staleTarget)
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(stalePath, []byte("old content"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	cfgPath := config.TOMLPath(target)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir .atomic: %v", err)
	}
	cfg := config.Default()
	cfg.Install.Version = "0.0.1"
	cfg.Install.Artifacts.Agents = []string{staleTarget}
	if err := config.WritePersist(cfgPath, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("stale file still exists after prune: %s", stalePath)
	}
}

func TestPruneSkipsWhenConfirmDeclines(t *testing.T) {
	target := t.TempDir()

	claudeinstall.PruneConfirm = func(stale []string) (bool, error) { return false, nil }
	t.Cleanup(func() { claudeinstall.PruneConfirm = claudeinstall.DefaultPruneConfirm })

	staleTarget := "agents/old-agent-not-in-bundle.md"
	stalePath := filepath.Join(target, staleTarget)
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(stalePath, []byte("old content"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	cfgPath := config.TOMLPath(target)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir .atomic: %v", err)
	}
	cfg := config.Default()
	cfg.Install.Version = "0.0.1"
	cfg.Install.Artifacts.Agents = []string{staleTarget}
	if err := config.WritePersist(cfgPath, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if _, err := os.Stat(stalePath); err != nil {
		t.Errorf("stale file removed despite declined confirm: %v", err)
	}
}

func TestPruneNotCalledOnFreshInstall(t *testing.T) {
	target := t.TempDir()

	confirmCalled := false
	claudeinstall.PruneConfirm = func(stale []string) (bool, error) {
		confirmCalled = true
		return false, nil
	}
	t.Cleanup(func() { claudeinstall.PruneConfirm = claudeinstall.DefaultPruneConfirm })

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if confirmCalled {
		t.Error("PruneConfirm called on fresh install with no prior manifest — should be a no-op")
	}
}

func TestPruneConfirmReceivesStaleList(t *testing.T) {
	target := t.TempDir()

	var gotStale []string
	claudeinstall.PruneConfirm = func(stale []string) (bool, error) {
		gotStale = stale
		return false, nil // decline so no deletion
	}
	t.Cleanup(func() { claudeinstall.PruneConfirm = claudeinstall.DefaultPruneConfirm })

	staleTarget := "agents/old-agent-not-in-bundle.md"

	cfgPath := config.TOMLPath(target)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir .atomic: %v", err)
	}
	cfg := config.Default()
	cfg.Install.Version = "0.0.1"
	cfg.Install.Artifacts.Agents = []string{staleTarget}
	if err := config.WritePersist(cfgPath, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if len(gotStale) != 1 || gotStale[0] != staleTarget {
		t.Errorf("PruneConfirm received stale = %v, want [%s]", gotStale, staleTarget)
	}
}

func TestBuildUninstallPlan_InstallArtifacts_ScopesDelete(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	// Both agents have existed=false (atomic created them per the snapshot).
	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "agents/atomic-foo.md", "sha256": "", "existed": false},
		{"path": "agents/user-custom.md", "sha256": "", "existed": false},
	})

	installedTargets := map[string]bool{
		"agents/atomic-foo.md": true,
	}

	plan, err := claudeinstall.BuildUninstallPlanWithManifest(targetDir, targetDir, map[string]string{}, installedTargets)
	if err != nil {
		t.Fatalf("BuildUninstallPlanWithManifest: %v", err)
	}

	if len(plan.Delete) != 1 || plan.Delete[0] != "agents/atomic-foo.md" {
		t.Errorf("Delete = %v, want [agents/atomic-foo.md]", plan.Delete)
	}
	for _, p := range plan.Delete {
		if p == "agents/user-custom.md" {
			t.Error("Delete contains agents/user-custom.md — user-added files must never be removed")
		}
	}
}

func TestBuildUninstallPlan_NilInstalledTargets_NoScoping(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "agents/atomic-foo.md", "sha256": "", "existed": false},
		{"path": "agents/user-custom.md", "sha256": "", "existed": false},
	})

	// nil = no [install.artifacts] (pre-framework): existing snapshot-only behavior.
	plan, err := claudeinstall.BuildUninstallPlanWithManifest(targetDir, targetDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("BuildUninstallPlanWithManifest: %v", err)
	}

	if len(plan.Delete) != 2 {
		t.Errorf("Delete = %v, want both entries when installedTargets is nil", plan.Delete)
	}
}

func TestBuildUninstallPlan_ManifestScopedFromConfig(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "agents/atomic-foo.md", "sha256": "", "existed": false},
		{"path": "agents/user-custom.md", "sha256": "", "existed": false},
	})

	cfgPath := config.TOMLPath(targetDir)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir .atomic: %v", err)
	}
	cfg := config.Default()
	cfg.Install.Version = "1.0.0"
	cfg.Install.Artifacts.Agents = []string{"agents/atomic-foo.md"}
	if err := config.WritePersist(cfgPath, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	plan, err := claudeinstall.BuildUninstallPlan(targetDir, targetDir)
	if err != nil {
		t.Fatalf("BuildUninstallPlan: %v", err)
	}

	if len(plan.Delete) != 1 || plan.Delete[0] != "agents/atomic-foo.md" {
		t.Errorf("Delete = %v, want [agents/atomic-foo.md]", plan.Delete)
	}
}

func TestPruneAbortedIsDecline(t *testing.T) {
	target := t.TempDir()

	claudeinstall.PruneConfirm = func(stale []string) (bool, error) {
		return false, prompt.ErrAborted
	}
	t.Cleanup(func() { claudeinstall.PruneConfirm = claudeinstall.DefaultPruneConfirm })

	staleTarget := "agents/old-agent-not-in-bundle.md"
	stalePath := filepath.Join(target, staleTarget)
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(stalePath, []byte("old content"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	cfgPath := config.TOMLPath(target)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir .atomic: %v", err)
	}
	cfg := config.Default()
	cfg.Install.Version = "0.0.1"
	cfg.Install.Artifacts.Agents = []string{staleTarget}
	if err := config.WritePersist(cfgPath, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install returned error on aborted prune prompt: %v", err)
	}

	// Stale file must still exist — abort is a decline, not a delete.
	if _, err := os.Stat(stalePath); err != nil {
		t.Errorf("stale file was removed after aborted prompt — must be preserved: %v", err)
	}
}

func TestBuildUninstallPlan_NoConfigToml_FallsBackToUnscoped(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "agents/atomic-foo.md", "sha256": "", "existed": false},
	})

	plan, err := claudeinstall.BuildUninstallPlan(targetDir, targetDir)
	if err != nil {
		t.Fatalf("BuildUninstallPlan: %v", err)
	}

	// Unscoped: atomic-foo.md ends up in Delete.
	if len(plan.Delete) != 1 || plan.Delete[0] != "agents/atomic-foo.md" {
		t.Errorf("Delete = %v, want [agents/atomic-foo.md] for pre-framework unscoped path", plan.Delete)
	}
}
