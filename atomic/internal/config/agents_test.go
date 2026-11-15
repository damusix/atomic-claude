package config

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

// The agents form must follow the shipped agents/ directory, so an agent added
// to the bundle shows up without editing a Go list.
func TestBundledAgentsMatchesEmbeddedAgentsDir(t *testing.T) {
	entries, err := fs.ReadDir(embedded.FS, "bundle/agents")
	if err != nil {
		t.Fatalf("read embedded agents dir: %v", err)
	}
	var want []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			want = append(want, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	got := bundledAgents()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("bundledAgents() = %v, want %v", got, want)
	}
}

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
	if err := applyAgentOverrides(cfg, "claude", selections); err != nil {
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
	err := applyAgentOverrides(cfg, "claude", map[string]AgentOverride{
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
	err := applyAgentOverrides(cfg, "claude", map[string]AgentOverride{
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
	if err := applyAgentOverrides(cfg, "claude", map[string]AgentOverride{
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
	if err := applyAgentOverrides(cfg, "claude", selections); err != nil {
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
	if err := applyAgentOverrides(cfg, "claude", selections); err != nil {
		t.Fatalf("applyAgentOverrides: %v", err)
	}
	if cfg.Claude.Agents != nil {
		t.Errorf("cfg.Claude.Agents should be nil after clearing all overrides, got %v", cfg.Claude.Agents)
	}
}

// An arbitrary forward-reserved model name is accepted — there is no allowlist.
func TestApplyAgentOverrides_fableIsValid(t *testing.T) {
	cfg := Default()
	if err := applyAgentOverrides(cfg, "claude", map[string]AgentOverride{
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
	if err := applyAgentOverrides(cfg, "claude", map[string]AgentOverride{
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
	if err := applyAgentOverrides(cfg, "claude", map[string]AgentOverride{
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
