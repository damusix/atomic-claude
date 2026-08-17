package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// --- validateModelInput (huh.Input validator, pure function) ---

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

// The ordered option list is exactly "" followed by the five-level enum.
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
	if len(cfg.Claude.Agents) != 5 {
		t.Fatalf("Agents len = %d, want 5", len(cfg.Claude.Agents))
	}
	if cfg.Claude.Agents["atomic-implementer"].Model != "sonnet" {
		t.Errorf("atomic-implementer = %q, want %q", cfg.Claude.Agents["atomic-implementer"].Model, "sonnet")
	}
	if cfg.Claude.Agents["atomic-strategist"].Model != "opus" {
		t.Errorf("atomic-strategist = %q, want %q", cfg.Claude.Agents["atomic-strategist"].Model, "opus")
	}
}

// Model validation is lenient: applyAgentOverrides never hard-fails on an
// arbitrary model string.
func TestApplyAgentOverrides_invalidModelNeverFails(t *testing.T) {
	cfg := Default()
	err := applyAgentOverrides(cfg, map[string]AgentOverride{
		"atomic-implementer": {Model: "turbo"}, // arbitrary, still accepted
	})
	if err != nil {
		t.Fatalf("applyAgentOverrides: unexpected error for lenient model: %v", err)
	}
	if cfg.Claude.Agents["atomic-implementer"].Model != "turbo" {
		t.Errorf("atomic-implementer = %q, want %q", cfg.Claude.Agents["atomic-implementer"].Model, "turbo")
	}
}

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

func TestApplyAgentOverrides_emptySelectionRemovesEntry(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{
		"atomic-implementer": {Model: "sonnet"},
		"atomic-reviewer":    {Model: "haiku"},
	}
	if err := applyAgentOverrides(cfg, map[string]AgentOverride{
		"atomic-implementer": {}, // remove override
	}); err != nil {
		t.Fatalf("applyAgentOverrides: %v", err)
	}
	if _, ok := cfg.Claude.Agents["atomic-implementer"]; ok {
		t.Error("atomic-implementer should be absent from cfg.Claude.Agents after empty selection")
	}
	// atomic-reviewer was not in selections, so it must remain untouched.
	if cfg.Claude.Agents["atomic-reviewer"].Model != "haiku" {
		t.Errorf("atomic-reviewer should still be %q, got %q", "haiku", cfg.Claude.Agents["atomic-reviewer"].Model)
	}
}

// An all-empty selection over an already-empty map leaves it nil, so TOML emits
// no [claude.agents] section at all.
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
	if cfg.Claude.Agents != nil {
		t.Errorf("cfg.Claude.Agents should be nil when all selections are empty, got %v", cfg.Claude.Agents)
	}
}

func TestApplyAgentOverrides_clearAllExistingOverrides(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{
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
	if cfg.Claude.Agents != nil {
		t.Errorf("cfg.Claude.Agents should be nil after clearing all overrides, got %v", cfg.Claude.Agents)
	}
}

// An arbitrary forward-reserved model name is accepted — there is no allowlist.
func TestApplyAgentOverrides_fableIsValid(t *testing.T) {
	cfg := Default()
	if err := applyAgentOverrides(cfg, map[string]AgentOverride{
		"atomic-strategist": {Model: "fable"},
	}); err != nil {
		t.Errorf("applyAgentOverrides: fable should be valid, got: %v", err)
	}
	if cfg.Claude.Agents["atomic-strategist"].Model != "fable" {
		t.Errorf("atomic-strategist = %q, want %q", cfg.Claude.Agents["atomic-strategist"].Model, "fable")
	}
}

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

	if loaded.Claude.Agents["atomic-implementer"].Model != "sonnet" {
		t.Errorf("atomic-implementer = %q, want %q", loaded.Claude.Agents["atomic-implementer"].Model, "sonnet")
	}
	if loaded.Claude.Agents["atomic-investigator"].Model != "haiku" {
		t.Errorf("atomic-investigator = %q, want %q", loaded.Claude.Agents["atomic-investigator"].Model, "haiku")
	}
	// atomic-reviewer was {}, so it must be absent.
	if _, ok := loaded.Claude.Agents["atomic-reviewer"]; ok {
		t.Error("atomic-reviewer should be absent (empty selection = no override)")
	}
}

// A selection carrying both fields writes both into cfg.Claude.Agents.
func TestApplyAgentOverrides_modelAndEffortRoundTrip(t *testing.T) {
	cfg := Default()
	if err := applyAgentOverrides(cfg, map[string]AgentOverride{
		"atomic-implementer": {Model: "claude-opus-4-8", Effort: "high"},
	}); err != nil {
		t.Fatalf("applyAgentOverrides: %v", err)
	}
	got := cfg.Claude.Agents["atomic-implementer"]
	want := AgentOverride{Model: "claude-opus-4-8", Effort: "high"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// --- AgentTierSelector seam: CLI-level tests ---

// withAgentTierSelectorStub replaces the AgentTierSelector seam for f.
func withAgentTierSelectorStub(sel func(*Config) (map[string]AgentOverride, error), f func()) {
	orig := AgentTierSelector
	AgentTierSelector = sel
	defer func() { AgentTierSelector = orig }()
	f()
}

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

	cfg, _, err := Load(TOMLPath(home))
	if err != nil {
		t.Fatalf("Load after agents: %v", err)
	}
	if cfg.Claude.Agents["atomic-implementer"].Model != "sonnet" {
		t.Errorf("atomic-implementer = %q, want sonnet", cfg.Claude.Agents["atomic-implementer"].Model)
	}
	if cfg.Claude.Agents["atomic-strategist"].Model != "opus" {
		t.Errorf("atomic-strategist = %q, want opus", cfg.Claude.Agents["atomic-strategist"].Model)
	}
	// atomic-reviewer was {}, so it must be absent.
	if _, ok := cfg.Claude.Agents["atomic-reviewer"]; ok {
		t.Error("atomic-reviewer should be absent (empty selection = no override)")
	}
}

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

// A selector that somehow returns an invalid effort is caught by
// applyAgentOverrides, and the verb exits 1.
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
	if len(cfg.Claude.Agents) != 0 {
		t.Errorf("expected no agent overrides, got %v", cfg.Claude.Agents)
	}
}

// The selector receives the current cfg so it can pre-populate from existing
// overrides.
func TestRunAgents_selectorReceivesExistingConfig(t *testing.T) {
	home := t.TempDir()

	existing := Default()
	existing.Claude.Agents = map[string]AgentOverride{"atomic-implementer": {Model: "haiku"}}
	if err := WritePersist(TOMLPath(home), existing); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}

	var seenModel string
	withAgentTierSelectorStub(func(cfg *Config) (map[string]AgentOverride, error) {
		seenModel = cfg.Claude.Agents["atomic-implementer"].Model
		return map[string]AgentOverride{}, nil
	}, func() {
		runCLI(t, home, "agents")
	})

	if seenModel != "haiku" {
		t.Errorf("selector received atomic-implementer model %q, want %q", seenModel, "haiku")
	}
}

// The agents verb must not clobber existing [output] or [update] settings.
func TestRunAgents_preservesOtherConfigSections(t *testing.T) {
	home := t.TempDir()

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
	if cfg.Claude.Agents["atomic-implementer"].Model != "sonnet" {
		t.Errorf("atomic-implementer = %q, want sonnet", cfg.Claude.Agents["atomic-implementer"].Model)
	}
}

// withApplyAgentsHookStub swaps ApplyAgentsHook for f, restoring the original.
func withApplyAgentsHookStub(hook func(home string) ([]string, int, error), f func()) {
	orig := ApplyAgentsHook
	ApplyAgentsHook = hook
	defer func() { ApplyAgentsHook = orig }()
	f()
}

// After a successful save the verb calls ApplyAgentsHook with the resolved home
// and reports the applied agents plus a restart note.
func TestRunAgents_appliesViaHook(t *testing.T) {
	home := t.TempDir()

	var gotHome string
	withAgentTierSelectorStub(func(_ *Config) (map[string]AgentOverride, error) {
		return map[string]AgentOverride{"atomic-implementer": {Effort: "high"}}, nil
	}, func() {
		withApplyAgentsHookStub(func(h string) ([]string, int, error) {
			gotHome = h
			return []string{"atomic-implementer"}, 1, nil
		}, func() {
			code, stdout, stderr := runCLI(t, home, "agents")
			if code != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr)
			}
			if !strings.Contains(stdout, "atomic-implementer") {
				t.Errorf("stdout missing applied agent name: %q", stdout)
			}
			if !strings.Contains(stdout, "Restart Claude Code sessions") {
				t.Errorf("stdout missing restart note: %q", stdout)
			}
		})
	})
	if gotHome != home {
		t.Errorf("ApplyAgentsHook called with home %q, want %q", gotHome, home)
	}
}

// A hook error is reported on stderr but the verb still exits 0 — the config
// write itself succeeded.
func TestRunAgents_hookErrorNonFatal(t *testing.T) {
	home := t.TempDir()

	withAgentTierSelectorStub(func(_ *Config) (map[string]AgentOverride, error) {
		return map[string]AgentOverride{"atomic-implementer": {Model: "opus"}}, nil
	}, func() {
		withApplyAgentsHookStub(func(_ string) ([]string, int, error) {
			return nil, 0, errors.New("boom")
		}, func() {
			code, _, stderr := runCLI(t, home, "agents")
			if code != 0 {
				t.Fatalf("expected exit 0 (config saved despite hook error), got %d", code)
			}
			if !strings.Contains(stderr, "boom") {
				t.Errorf("stderr missing hook error: %q", stderr)
			}
			if !strings.Contains(stderr, "atomic claude install") {
				t.Errorf("stderr missing fallback guidance: %q", stderr)
			}
		})
	})
}

func TestRunAgents_hookNoInstalledAgents(t *testing.T) {
	home := t.TempDir()

	withAgentTierSelectorStub(func(_ *Config) (map[string]AgentOverride, error) {
		return map[string]AgentOverride{"atomic-implementer": {Model: "opus"}}, nil
	}, func() {
		withApplyAgentsHookStub(func(_ string) ([]string, int, error) {
			return nil, 0, nil
		}, func() {
			code, stdout, _ := runCLI(t, home, "agents")
			if code != 0 {
				t.Fatalf("expected exit 0, got %d", code)
			}
			if !strings.Contains(stdout, "No installed agents found") {
				t.Errorf("stdout missing no-installed-agents message: %q", stdout)
			}
		})
	})
}

func TestRunAgents_hookAlreadyUpToDate(t *testing.T) {
	home := t.TempDir()

	withAgentTierSelectorStub(func(_ *Config) (map[string]AgentOverride, error) {
		return map[string]AgentOverride{"atomic-implementer": {Model: "opus"}}, nil
	}, func() {
		withApplyAgentsHookStub(func(_ string) ([]string, int, error) {
			return nil, 3, nil
		}, func() {
			code, stdout, _ := runCLI(t, home, "agents")
			if code != 0 {
				t.Fatalf("expected exit 0, got %d", code)
			}
			if !strings.Contains(stdout, "already up to date") {
				t.Errorf("stdout missing already-up-to-date message: %q", stdout)
			}
		})
	})
}

// The default selector must return ErrNonInteractiveAgents without hanging or
// crashing when not attached to a TTY.
func TestDefaultAgentTierSelector_nonInteractive(t *testing.T) {
	cfg := Default()
	_, err := defaultAgentTierSelector(cfg)
	if !errors.Is(err, ErrNonInteractiveAgents) {
		t.Logf("defaultAgentTierSelector returned %v (not ErrNonInteractiveAgents — may be running on a TTY)", err)
	}
}
