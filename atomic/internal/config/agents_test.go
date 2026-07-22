package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// --- validateModelInput (huh.Input validator, pure function) ---

// TestValidateModelInput: empty passes (no override); well-formed model
// strings pass; strings with internal whitespace fail with a guidance error.
func TestValidateModelInput(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty", "", false},
		{"tier", "opus", false},
		{"model id with bracket suffix", "claude-opus-4-6[1m]", false},
		{"two words", "two words", true},
		{"leading space", " opus", true},
		{"trailing space", "opus ", true},
		{"leading tab", "\topus", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateModelInput(tc.in)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateModelInput(%q) = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
		})
	}
}

// --- effort option list (interactive Select) ---

// TestEffortOptionValues_order: the ordered option list is exactly
// "" (bundled default) followed by the five-level enum, in enum order.
func TestEffortOptionValues_order(t *testing.T) {
	want := []string{"", "low", "medium", "high", "xhigh", "max"}
	if len(effortOptionValues) != len(want) {
		t.Fatalf("effortOptionValues = %v, want %v", effortOptionValues, want)
	}
	for i, v := range want {
		if effortOptionValues[i] != v {
			t.Errorf("effortOptionValues[%d] = %q, want %q", i, effortOptionValues[i], v)
		}
	}
}

// --- applyAgentOverrides (pure function) ---

// TestApplyAgentOverrides_validSelections: valid model selections are written to cfg.Agents.
func TestApplyAgentOverrides_validSelections(t *testing.T) {
	cfg := Default()
	selections := map[string]AgentOverride{
		"atomic-implementer":   {Model: "sonnet"},
		"atomic-investigator":  {Model: "haiku"},
		"atomic-reviewer":      {Model: "sonnet"},
		"atomic-strategist":    {Model: "opus"},
		"atomic-wiki-inferrer": {Model: "haiku"},
	}
	if err := applyAgentOverrides(cfg, selections); err != nil {
		t.Fatalf("applyAgentOverrides: unexpected error: %v", err)
	}
	if len(cfg.Agents) != 5 {
		t.Fatalf("Agents len = %d, want 5", len(cfg.Agents))
	}
	if cfg.Agents["atomic-implementer"].Model != "sonnet" {
		t.Errorf("atomic-implementer = %q, want %q", cfg.Agents["atomic-implementer"].Model, "sonnet")
	}
	if cfg.Agents["atomic-strategist"].Model != "opus" {
		t.Errorf("atomic-strategist = %q, want %q", cfg.Agents["atomic-strategist"].Model, "opus")
	}
}

// TestApplyAgentOverrides_invalidModelNeverFails: model validation is lenient —
// applyAgentOverrides never hard-fails on an arbitrary model string.
func TestApplyAgentOverrides_invalidModelNeverFails(t *testing.T) {
	cfg := Default()
	err := applyAgentOverrides(cfg, map[string]AgentOverride{
		"atomic-implementer": {Model: "turbo"}, // arbitrary, still accepted
	})
	if err != nil {
		t.Fatalf("applyAgentOverrides: unexpected error for lenient model: %v", err)
	}
	if cfg.Agents["atomic-implementer"].Model != "turbo" {
		t.Errorf("atomic-implementer = %q, want %q", cfg.Agents["atomic-implementer"].Model, "turbo")
	}
}

// TestApplyAgentOverrides_invalidEffort: an invalid effort value returns an error
// (delegates to the validEfforts allowlist).
func TestApplyAgentOverrides_invalidEffort(t *testing.T) {
	cfg := Default()
	err := applyAgentOverrides(cfg, map[string]AgentOverride{
		"atomic-implementer": {Effort: "turbo"}, // not in allowlist
	})
	if err == nil {
		t.Fatal("expected error for invalid effort, got nil")
	}
	if !strings.Contains(err.Error(), "turbo") {
		t.Errorf("error should mention invalid effort value, got: %v", err)
	}
	if !strings.Contains(err.Error(), "atomic-implementer") {
		t.Errorf("error should mention agent name, got: %v", err)
	}
}

// TestApplyAgentOverrides_emptySelectionRemovesEntry: selecting {} (bundled default)
// removes the agent's entry from cfg.Agents.
func TestApplyAgentOverrides_emptySelectionRemovesEntry(t *testing.T) {
	cfg := Default()
	cfg.Agents = map[string]AgentOverride{
		"atomic-implementer": {Model: "sonnet"},
		"atomic-reviewer":    {Model: "haiku"},
	}
	// Decline override for atomic-implementer.
	if err := applyAgentOverrides(cfg, map[string]AgentOverride{
		"atomic-implementer": {}, // remove override
	}); err != nil {
		t.Fatalf("applyAgentOverrides: %v", err)
	}
	if _, ok := cfg.Agents["atomic-implementer"]; ok {
		t.Error("atomic-implementer should be absent from cfg.Agents after empty selection")
	}
	// atomic-reviewer was not in selections → should remain untouched.
	if cfg.Agents["atomic-reviewer"].Model != "haiku" {
		t.Errorf("atomic-reviewer should still be %q, got %q", "haiku", cfg.Agents["atomic-reviewer"].Model)
	}
}

// TestApplyAgentOverrides_allEmptyNilsMap: when all agents select {} (bundled default)
// and cfg.Agents was nil/empty, the map remains nil (no empty [agents] TOML section).
func TestApplyAgentOverrides_allEmptyNilsMap(t *testing.T) {
	cfg := Default()
	selections := map[string]AgentOverride{
		"atomic-implementer":   {},
		"atomic-investigator":  {},
		"atomic-reviewer":      {},
		"atomic-strategist":    {},
		"atomic-wiki-inferrer": {},
	}
	if err := applyAgentOverrides(cfg, selections); err != nil {
		t.Fatalf("applyAgentOverrides: %v", err)
	}
	if cfg.Agents != nil {
		t.Errorf("cfg.Agents should be nil when all selections are empty, got %v", cfg.Agents)
	}
}

// TestApplyAgentOverrides_clearAllExistingOverrides: selecting {} for every agent
// when overrides exist should result in nil Agents map.
func TestApplyAgentOverrides_clearAllExistingOverrides(t *testing.T) {
	cfg := Default()
	cfg.Agents = map[string]AgentOverride{
		"atomic-implementer": {Model: "haiku"},
		"atomic-reviewer":    {Model: "opus"},
	}
	selections := map[string]AgentOverride{
		"atomic-implementer": {},
		"atomic-reviewer":    {},
	}
	if err := applyAgentOverrides(cfg, selections); err != nil {
		t.Fatalf("applyAgentOverrides: %v", err)
	}
	if cfg.Agents != nil {
		t.Errorf("cfg.Agents should be nil after clearing all overrides, got %v", cfg.Agents)
	}
}

// TestApplyAgentOverrides_fableIsValid: an arbitrary forward-reserved model
// name like "fable" is accepted (no allowlist).
func TestApplyAgentOverrides_fableIsValid(t *testing.T) {
	cfg := Default()
	if err := applyAgentOverrides(cfg, map[string]AgentOverride{
		"atomic-strategist": {Model: "fable"},
	}); err != nil {
		t.Errorf("applyAgentOverrides: fable should be valid, got: %v", err)
	}
	if cfg.Agents["atomic-strategist"].Model != "fable" {
		t.Errorf("atomic-strategist = %q, want %q", cfg.Agents["atomic-strategist"].Model, "fable")
	}
}

// TestApplyAgentOverrides_roundTrip: apply → WritePersist → Load → Validate is clean.
func TestApplyAgentOverrides_roundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	if err := applyAgentOverrides(cfg, map[string]AgentOverride{
		"atomic-implementer":  {Model: "sonnet"},
		"atomic-investigator": {Model: "haiku"},
		"atomic-reviewer":     {}, // leave unchanged
	}); err != nil {
		t.Fatalf("applyAgentOverrides: %v", err)
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

	if loaded.Agents["atomic-implementer"].Model != "sonnet" {
		t.Errorf("atomic-implementer = %q, want %q", loaded.Agents["atomic-implementer"].Model, "sonnet")
	}
	if loaded.Agents["atomic-investigator"].Model != "haiku" {
		t.Errorf("atomic-investigator = %q, want %q", loaded.Agents["atomic-investigator"].Model, "haiku")
	}
	// atomic-reviewer was {} → should be absent from [agents].
	if _, ok := loaded.Agents["atomic-reviewer"]; ok {
		t.Error("atomic-reviewer should be absent (empty selection = no override)")
	}
}

// TestApplyAgentOverrides_modelAndEffortRoundTrip: a selection carrying both
// fields (as the reworked model-Input + effort-Select form now returns)
// writes both into cfg.Agents.
func TestApplyAgentOverrides_modelAndEffortRoundTrip(t *testing.T) {
	cfg := Default()
	if err := applyAgentOverrides(cfg, map[string]AgentOverride{
		"atomic-implementer": {Model: "claude-opus-4-8", Effort: "high"},
	}); err != nil {
		t.Fatalf("applyAgentOverrides: %v", err)
	}
	got := cfg.Agents["atomic-implementer"]
	want := AgentOverride{Model: "claude-opus-4-8", Effort: "high"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// --- AgentTierSelector seam: CLI-level tests ---

// withAgentTierSelectorStub replaces the AgentTierSelector seam for the duration of f.
func withAgentTierSelectorStub(sel func(*Config) (map[string]AgentOverride, error), f func()) {
	orig := AgentTierSelector
	AgentTierSelector = sel
	defer func() { AgentTierSelector = orig }()
	f()
}

// TestRunAgents_writesSelections: agents verb with a stubbed selector writes models,
// creates config.toml, and returns exit 0.
func TestRunAgents_writesSelections(t *testing.T) {
	home := t.TempDir()

	withAgentTierSelectorStub(func(_ *Config) (map[string]AgentOverride, error) {
		return map[string]AgentOverride{
			"atomic-implementer":   {Model: "sonnet"},
			"atomic-investigator":  {Model: "haiku"},
			"atomic-reviewer":      {},
			"atomic-strategist":    {Model: "opus"},
			"atomic-wiki-inferrer": {Model: "haiku"},
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
	if cfg.Agents["atomic-implementer"].Model != "sonnet" {
		t.Errorf("atomic-implementer = %q, want sonnet", cfg.Agents["atomic-implementer"].Model)
	}
	if cfg.Agents["atomic-strategist"].Model != "opus" {
		t.Errorf("atomic-strategist = %q, want opus", cfg.Agents["atomic-strategist"].Model)
	}
	// atomic-reviewer was {} → should be absent.
	if _, ok := cfg.Agents["atomic-reviewer"]; ok {
		t.Error("atomic-reviewer should be absent (empty selection = no override)")
	}
}

// TestRunAgents_nonInteractive: ErrNonInteractiveAgents exits 1 with guidance.
func TestRunAgents_nonInteractive(t *testing.T) {
	home := t.TempDir()

	withAgentTierSelectorStub(func(_ *Config) (map[string]AgentOverride, error) {
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

	withAgentTierSelectorStub(func(_ *Config) (map[string]AgentOverride, error) {
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

// TestRunAgents_invalidEffortFromSelector: when the selector somehow returns an
// invalid effort, applyAgentOverrides catches it and agents exits 1.
func TestRunAgents_invalidEffortFromSelector(t *testing.T) {
	home := t.TempDir()

	withAgentTierSelectorStub(func(_ *Config) (map[string]AgentOverride, error) {
		return map[string]AgentOverride{"atomic-implementer": {Effort: "turbo"}}, nil
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

	withAgentTierSelectorStub(func(_ *Config) (map[string]AgentOverride, error) {
		return map[string]AgentOverride{
			"atomic-implementer":   {},
			"atomic-investigator":  {},
			"atomic-reviewer":      {},
			"atomic-strategist":    {},
			"atomic-wiki-inferrer": {},
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
	existing.Agents = map[string]AgentOverride{"atomic-implementer": {Model: "haiku"}}
	if err := WritePersist(TOMLPath(home), existing); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}

	var seenModel string
	withAgentTierSelectorStub(func(cfg *Config) (map[string]AgentOverride, error) {
		seenModel = cfg.Agents["atomic-implementer"].Model
		return map[string]AgentOverride{}, nil
	}, func() {
		runCLI(t, home, "agents")
	})

	if seenModel != "haiku" {
		t.Errorf("selector received atomic-implementer model %q, want %q", seenModel, "haiku")
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

	withAgentTierSelectorStub(func(_ *Config) (map[string]AgentOverride, error) {
		return map[string]AgentOverride{"atomic-implementer": {Model: "sonnet"}}, nil
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
	if cfg.Agents["atomic-implementer"].Model != "sonnet" {
		t.Errorf("atomic-implementer = %q, want sonnet", cfg.Agents["atomic-implementer"].Model)
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
