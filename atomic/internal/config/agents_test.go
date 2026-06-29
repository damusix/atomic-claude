package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// --- applyAgentTiers (pure function) ---

// TestApplyAgentTiers_validSelections: valid tier selections are written to cfg.Agents.
func TestApplyAgentTiers_validSelections(t *testing.T) {
	cfg := Default()
	selections := map[string]string{
		"atomic-implementer":   "sonnet",
		"atomic-investigator":  "haiku",
		"atomic-reviewer":      "sonnet",
		"atomic-strategist":    "opus",
		"atomic-wiki-inferrer": "haiku",
	}
	if err := applyAgentTiers(cfg, selections); err != nil {
		t.Fatalf("applyAgentTiers: unexpected error: %v", err)
	}
	if len(cfg.Agents) != 5 {
		t.Fatalf("Agents len = %d, want 5", len(cfg.Agents))
	}
	if cfg.Agents["atomic-implementer"] != "sonnet" {
		t.Errorf("atomic-implementer = %q, want %q", cfg.Agents["atomic-implementer"], "sonnet")
	}
	if cfg.Agents["atomic-strategist"] != "opus" {
		t.Errorf("atomic-strategist = %q, want %q", cfg.Agents["atomic-strategist"], "opus")
	}
}

// TestApplyAgentTiers_invalidTier: an invalid tier value returns an error
// (delegates to validTiers allowlist).
func TestApplyAgentTiers_invalidTier(t *testing.T) {
	cfg := Default()
	err := applyAgentTiers(cfg, map[string]string{
		"atomic-implementer": "turbo", // not in allowlist
	})
	if err == nil {
		t.Fatal("expected error for invalid tier, got nil")
	}
	if !strings.Contains(err.Error(), "turbo") {
		t.Errorf("error should mention invalid tier value, got: %v", err)
	}
	if !strings.Contains(err.Error(), "atomic-implementer") {
		t.Errorf("error should mention agent name, got: %v", err)
	}
}

// TestApplyAgentTiers_emptySelectionRemovesEntry: selecting "" (bundled default)
// removes the agent's entry from cfg.Agents.
func TestApplyAgentTiers_emptySelectionRemovesEntry(t *testing.T) {
	cfg := Default()
	cfg.Agents = map[string]string{
		"atomic-implementer": "sonnet",
		"atomic-reviewer":    "haiku",
	}
	// Decline override for atomic-implementer.
	if err := applyAgentTiers(cfg, map[string]string{
		"atomic-implementer": "", // remove override
	}); err != nil {
		t.Fatalf("applyAgentTiers: %v", err)
	}
	if _, ok := cfg.Agents["atomic-implementer"]; ok {
		t.Error("atomic-implementer should be absent from cfg.Agents after empty selection")
	}
	// atomic-reviewer was not in selections → should remain untouched.
	if cfg.Agents["atomic-reviewer"] != "haiku" {
		t.Errorf("atomic-reviewer should still be %q, got %q", "haiku", cfg.Agents["atomic-reviewer"])
	}
}

// TestApplyAgentTiers_allEmptyNilsMap: when all agents select "" (bundled default)
// and cfg.Agents was nil/empty, the map remains nil (no empty [agents] TOML section).
func TestApplyAgentTiers_allEmptyNilsMap(t *testing.T) {
	cfg := Default()
	selections := map[string]string{
		"atomic-implementer":   "",
		"atomic-investigator":  "",
		"atomic-reviewer":      "",
		"atomic-strategist":    "",
		"atomic-wiki-inferrer": "",
	}
	if err := applyAgentTiers(cfg, selections); err != nil {
		t.Fatalf("applyAgentTiers: %v", err)
	}
	if cfg.Agents != nil {
		t.Errorf("cfg.Agents should be nil when all selections are empty, got %v", cfg.Agents)
	}
}

// TestApplyAgentTiers_clearAllExistingOverrides: selecting "" for every agent
// when overrides exist should result in nil Agents map.
func TestApplyAgentTiers_clearAllExistingOverrides(t *testing.T) {
	cfg := Default()
	cfg.Agents = map[string]string{
		"atomic-implementer": "haiku",
		"atomic-reviewer":    "opus",
	}
	selections := map[string]string{
		"atomic-implementer": "",
		"atomic-reviewer":    "",
	}
	if err := applyAgentTiers(cfg, selections); err != nil {
		t.Fatalf("applyAgentTiers: %v", err)
	}
	if cfg.Agents != nil {
		t.Errorf("cfg.Agents should be nil after clearing all overrides, got %v", cfg.Agents)
	}
}

// TestApplyAgentTiers_fableIsValid: "fable" (forward-reserved) is accepted.
func TestApplyAgentTiers_fableIsValid(t *testing.T) {
	cfg := Default()
	if err := applyAgentTiers(cfg, map[string]string{
		"atomic-strategist": "fable",
	}); err != nil {
		t.Errorf("applyAgentTiers: fable should be valid, got: %v", err)
	}
	if cfg.Agents["atomic-strategist"] != "fable" {
		t.Errorf("atomic-strategist = %q, want %q", cfg.Agents["atomic-strategist"], "fable")
	}
}

// TestApplyAgentTiers_roundTrip: apply → WritePersist → Load → Validate is clean.
func TestApplyAgentTiers_roundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	if err := applyAgentTiers(cfg, map[string]string{
		"atomic-implementer":  "sonnet",
		"atomic-investigator": "haiku",
		"atomic-reviewer":     "", // leave unchanged
	}); err != nil {
		t.Fatalf("applyAgentTiers: %v", err)
	}

	if err := WritePersist(path, cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}

	loaded, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if err := Validate(loaded); err != nil {
		t.Errorf("Validate: %v", err)
	}

	if loaded.Agents["atomic-implementer"] != "sonnet" {
		t.Errorf("atomic-implementer = %q, want %q", loaded.Agents["atomic-implementer"], "sonnet")
	}
	if loaded.Agents["atomic-investigator"] != "haiku" {
		t.Errorf("atomic-investigator = %q, want %q", loaded.Agents["atomic-investigator"], "haiku")
	}
	// atomic-reviewer was "" → should be absent from [agents].
	if _, ok := loaded.Agents["atomic-reviewer"]; ok {
		t.Error("atomic-reviewer should be absent (empty selection = no override)")
	}
}

// --- AgentTierSelector seam: CLI-level tests ---

// withAgentTierSelectorStub replaces the AgentTierSelector seam for the duration of f.
func withAgentTierSelectorStub(sel func(*Config) (map[string]string, error), f func()) {
	orig := AgentTierSelector
	AgentTierSelector = sel
	defer func() { AgentTierSelector = orig }()
	f()
}

// TestRunAgents_writesSelections: agents verb with a stubbed selector writes tiers,
// creates config.toml, and returns exit 0.
func TestRunAgents_writesSelections(t *testing.T) {
	home := t.TempDir()

	withAgentTierSelectorStub(func(_ *Config) (map[string]string, error) {
		return map[string]string{
			"atomic-implementer":   "sonnet",
			"atomic-investigator":  "haiku",
			"atomic-reviewer":      "",
			"atomic-strategist":    "opus",
			"atomic-wiki-inferrer": "haiku",
		}, nil
	}, func() {
		code, _, stderr := runCLI(t, home, "agents")
		if code != 0 {
			t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr)
		}
	})

	// Verify persisted values.
	cfg, _, err := Load(TOMLPath(home))
	if err != nil {
		t.Fatalf("Load after agents: %v", err)
	}
	if cfg.Agents["atomic-implementer"] != "sonnet" {
		t.Errorf("atomic-implementer = %q, want sonnet", cfg.Agents["atomic-implementer"])
	}
	if cfg.Agents["atomic-strategist"] != "opus" {
		t.Errorf("atomic-strategist = %q, want opus", cfg.Agents["atomic-strategist"])
	}
	// atomic-reviewer was "" → should be absent.
	if _, ok := cfg.Agents["atomic-reviewer"]; ok {
		t.Error("atomic-reviewer should be absent (empty selection = no override)")
	}
}

// TestRunAgents_nonInteractive: ErrNonInteractiveAgents exits 1 with guidance.
func TestRunAgents_nonInteractive(t *testing.T) {
	home := t.TempDir()

	withAgentTierSelectorStub(func(_ *Config) (map[string]string, error) {
		return nil, ErrNonInteractiveAgents
	}, func() {
		code, _, stderr := runCLI(t, home, "agents")
		if code != 1 {
			t.Fatalf("expected exit 1, got %d", code)
		}
		if !strings.Contains(stderr, "interactive terminal") {
			t.Errorf("expected non-interactive guidance in stderr, got: %q", stderr)
		}
	})
}

// TestRunAgents_aborted: ErrAgentsAborted exits 1 with "aborted" message.
func TestRunAgents_aborted(t *testing.T) {
	home := t.TempDir()

	withAgentTierSelectorStub(func(_ *Config) (map[string]string, error) {
		return nil, ErrAgentsAborted
	}, func() {
		code, _, stderr := runCLI(t, home, "agents")
		if code != 1 {
			t.Fatalf("expected exit 1, got %d", code)
		}
		if !strings.Contains(stderr, "aborted") {
			t.Errorf("expected 'aborted' in stderr, got: %q", stderr)
		}
	})
}

// TestRunAgents_invalidTierFromSelector: when the selector somehow returns an invalid
// tier, applyAgentTiers catches it and agents exits 1.
func TestRunAgents_invalidTierFromSelector(t *testing.T) {
	home := t.TempDir()

	withAgentTierSelectorStub(func(_ *Config) (map[string]string, error) {
		return map[string]string{"atomic-implementer": "turbo"}, nil
	}, func() {
		code, _, stderr := runCLI(t, home, "agents")
		if code != 1 {
			t.Fatalf("expected exit 1, got %d; stderr: %s", code, stderr)
		}
		if !strings.Contains(stderr, "turbo") {
			t.Errorf("expected 'turbo' in stderr, got: %q", stderr)
		}
	})
}

// TestRunAgents_allDefault: all empty selections produce nil Agents (no [agents] section).
func TestRunAgents_allDefault(t *testing.T) {
	home := t.TempDir()

	withAgentTierSelectorStub(func(_ *Config) (map[string]string, error) {
		return map[string]string{
			"atomic-implementer":   "",
			"atomic-investigator":  "",
			"atomic-reviewer":      "",
			"atomic-strategist":    "",
			"atomic-wiki-inferrer": "",
		}, nil
	}, func() {
		code, _, _ := runCLI(t, home, "agents")
		if code != 0 {
			t.Fatalf("expected exit 0, got %d", code)
		}
	})

	cfg, _, err := Load(TOMLPath(home))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Agents) != 0 {
		t.Errorf("expected no agent overrides, got %v", cfg.Agents)
	}
}

// TestRunAgents_selectorReceivesExistingConfig: AgentTierSelector receives the
// current cfg so it can pre-populate selections from existing overrides.
func TestRunAgents_selectorReceivesExistingConfig(t *testing.T) {
	home := t.TempDir()

	// Pre-write a config with an existing override.
	existing := Default()
	existing.Agents = map[string]string{"atomic-implementer": "haiku"}
	if err := WritePersist(TOMLPath(home), existing); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}

	var seenTier string
	withAgentTierSelectorStub(func(cfg *Config) (map[string]string, error) {
		seenTier = cfg.Agents["atomic-implementer"]
		return map[string]string{}, nil
	}, func() {
		runCLI(t, home, "agents")
	})

	if seenTier != "haiku" {
		t.Errorf("selector received atomic-implementer tier %q, want %q", seenTier, "haiku")
	}
}

// TestRunAgents_preservesOtherConfigSections: agents verb does not clobber
// existing [output] or [update] settings.
func TestRunAgents_preservesOtherConfigSections(t *testing.T) {
	home := t.TempDir()

	// Pre-write config with non-default values.
	existing := Default()
	existing.Output.Signals.MaxDepth = 7
	if err := WritePersist(TOMLPath(home), existing); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}

	withAgentTierSelectorStub(func(_ *Config) (map[string]string, error) {
		return map[string]string{"atomic-implementer": "sonnet"}, nil
	}, func() {
		if code, _, _ := runCLI(t, home, "agents"); code != 0 {
			t.Fatal("expected exit 0")
		}
	})

	cfg, _, err := Load(TOMLPath(home))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Output.Signals.MaxDepth != 7 {
		t.Errorf("MaxDepth = %d, want 7 (should be preserved)", cfg.Output.Signals.MaxDepth)
	}
	if cfg.Agents["atomic-implementer"] != "sonnet" {
		t.Errorf("atomic-implementer = %q, want sonnet", cfg.Agents["atomic-implementer"])
	}
}

// TestDefaultAgentTierSelector_nonInteractive: the default selector returns
// ErrNonInteractiveAgents when not attached to a TTY (CI / test environment).
// This test verifies the no-panic contract: it must not hang or crash.
func TestDefaultAgentTierSelector_nonInteractive(t *testing.T) {
	// In test environments stdin/stdout are not TTYs, so defaultAgentTierSelector
	// should return ErrNonInteractiveAgents immediately without hanging.
	cfg := Default()
	_, err := defaultAgentTierSelector(cfg)
	if !errors.Is(err, ErrNonInteractiveAgents) {
		// In a CI environment this is the expected path.
		// If somehow a TTY is present (rare in CI), this test is a no-op.
		t.Logf("defaultAgentTierSelector returned %v (not ErrNonInteractiveAgents — may be running on a TTY)", err)
	}
}
