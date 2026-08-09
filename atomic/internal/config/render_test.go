package config

import (
	"strings"
	"testing"
)

// TestRenderByteStable: Render produces identical bytes for the same input (run twice).
func TestRenderByteStable(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "output.signals.max_depth", "5"); err != nil {
		t.Fatal(err)
	}
	a := Render(cfg)
	b := Render(cfg)
	if a != b {
		t.Errorf("Render not byte-stable:\nfirst:  %q\nsecond: %q", a, b)
	}
}

// TestRenderEmptyConfig: empty Config (zero value) renders a present file with a header.
func TestRenderEmptyConfig(t *testing.T) {
	cfg := &Config{}
	out := Render(cfg)
	if !strings.HasPrefix(out, "# Atomic resolved config") {
		t.Errorf("expected header, got: %q", out)
	}
}

// TestRenderSectionOrder: [output] appears before [update] (alphabetical key sort).
func TestRenderSectionOrder(t *testing.T) {
	cfg := Default()
	out := Render(cfg)
	outputIdx := strings.Index(out, "## [output]")
	updateIdx := strings.Index(out, "## [update]")
	if outputIdx < 0 {
		t.Error("expected '## [output]' in render")
	}
	if updateIdx < 0 {
		t.Error("expected '## [update]' in render")
	}
	if outputIdx > updateIdx {
		t.Errorf("[output] should appear before [update]; output=%d update=%d", outputIdx, updateIdx)
	}
}

// TestRenderUpdateSection: Render includes update.run_doctor with its value.
func TestRenderUpdateSection(t *testing.T) {
	cfg := Default()
	out := Render(cfg)
	if !strings.Contains(out, "update.run_doctor") {
		t.Errorf("expected 'update.run_doctor' in render, got: %q", out)
	}
	if !strings.Contains(out, "true") {
		t.Errorf("expected 'true' (default) in render, got: %q", out)
	}
}

// TestRenderSignalsMaxDepth: Render includes output.signals.max_depth.
func TestRenderSignalsMaxDepth(t *testing.T) {
	cfg := Default()
	out := Render(cfg)
	if !strings.Contains(out, "output.signals.max_depth") {
		t.Errorf("expected 'output.signals.max_depth' in render, got:\n%s", out)
	}
	if !strings.Contains(out, "3") {
		t.Errorf("expected default value '3' in render, got:\n%s", out)
	}
}

// TestRenderSignalsMaxDepthSetValue: Render reflects non-default max_depth.
func TestRenderSignalsMaxDepthSetValue(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "output.signals.max_depth", "5"); err != nil {
		t.Fatal(err)
	}
	out := Render(cfg)
	if !strings.Contains(out, "5") {
		t.Errorf("expected '5' in render after Set, got:\n%s", out)
	}
}

// TestRenderUpdateSectionFalse: Render shows false after Set false.
func TestRenderUpdateSectionFalse(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "update.run_doctor", "false"); err != nil {
		t.Fatal(err)
	}
	out := Render(cfg)
	if !strings.Contains(out, "false") {
		t.Errorf("expected 'false' in render after Set, got: %q", out)
	}
}

// TestRenderUpdateCheckSection: Render includes update.check with its value.
func TestRenderUpdateCheckSection(t *testing.T) {
	cfg := Default()
	out := Render(cfg)
	if !strings.Contains(out, "update.check") {
		t.Errorf("expected 'update.check' in render, got: %q", out)
	}
}

// TestRenderUpdateCheckSectionFalse: Render shows false after Set false.
func TestRenderUpdateCheckSectionFalse(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "update.check", "false"); err != nil {
		t.Fatal(err)
	}
	out := Render(cfg)
	if !strings.Contains(out, "update.check` = `false") {
		t.Errorf("expected 'update.check` = `false' in render after Set, got: %q", out)
	}
}

// TestRenderUpdateStageSection: Render includes update.stage with its value.
func TestRenderUpdateStageSection(t *testing.T) {
	cfg := Default()
	out := Render(cfg)
	if !strings.Contains(out, "update.stage") {
		t.Errorf("expected 'update.stage' in render, got: %q", out)
	}
}

// TestRenderUpdateStageSectionFalse: Render shows false after Set false.
func TestRenderUpdateStageSectionFalse(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "update.stage", "false"); err != nil {
		t.Fatal(err)
	}
	out := Render(cfg)
	if !strings.Contains(out, "update.stage` = `false") {
		t.Errorf("expected 'update.stage` = `false' in render after Set, got: %q", out)
	}
}

// TestRenderHarnessDir: Render includes harness.dir with its default value.
func TestRenderHarnessDir(t *testing.T) {
	cfg := Default()
	out := Render(cfg)
	if !strings.Contains(out, "harness.dir") {
		t.Errorf("expected 'harness.dir' in render, got:\n%s", out)
	}
	if !strings.Contains(out, ".claude") {
		t.Errorf("expected default '.claude' in render, got:\n%s", out)
	}
}

// TestRenderHarnessDirSetValue: Render reflects a non-default harness.dir.
func TestRenderHarnessDirSetValue(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "harness.dir", ".pi"); err != nil {
		t.Fatal(err)
	}
	out := Render(cfg)
	if !strings.Contains(out, ".pi") {
		t.Errorf("expected '.pi' in render after Set, got:\n%s", out)
	}
}

// TestRenderAgentsSection: Render includes the [claude] section when overrides are set.
func TestRenderAgentsSection(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{
		"atomic-implementer":  {Model: "sonnet"},
		"atomic-investigator": {Model: "haiku"},
	}
	out := Render(cfg)
	if !strings.Contains(out, "## [claude]") {
		t.Errorf("expected '## [claude]' section in render, got:\n%s", out)
	}
	if !strings.Contains(out, "claude.agents.atomic-implementer.model") {
		t.Errorf("expected 'claude.agents.atomic-implementer.model' in render, got:\n%s", out)
	}
	if !strings.Contains(out, "sonnet") {
		t.Errorf("expected 'sonnet' model in render, got:\n%s", out)
	}
	if !strings.Contains(out, "claude.agents.atomic-investigator.model") {
		t.Errorf("expected 'claude.agents.atomic-investigator.model' in render, got:\n%s", out)
	}
	if !strings.Contains(out, "haiku") {
		t.Errorf("expected 'haiku' model in render, got:\n%s", out)
	}
}

// TestRenderAgentsSectionEffort: Render emits the .effort dotted key alongside
// (or independently of) .model, per agent.
func TestRenderAgentsSectionEffort(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{
		"atomic-implementer":  {Model: "opus", Effort: "high"},
		"atomic-investigator": {Effort: "low"}, // effort-only, no model
	}
	out := Render(cfg)
	if !strings.Contains(out, "claude.agents.atomic-implementer.model") || !strings.Contains(out, "claude.agents.atomic-implementer.effort") {
		t.Errorf("expected both .model and .effort keys for atomic-implementer, got:\n%s", out)
	}
	if !strings.Contains(out, "claude.agents.atomic-investigator.effort") {
		t.Errorf("expected '.effort' key for atomic-investigator, got:\n%s", out)
	}
	if strings.Contains(out, "claude.agents.atomic-investigator.model") {
		t.Errorf("expected no '.model' key for effort-only atomic-investigator, got:\n%s", out)
	}
}

// TestRenderAgentsSectionAbsent: Render omits the [claude] section when no overrides are set.
func TestRenderAgentsSectionAbsent(t *testing.T) {
	cfg := Default()
	out := Render(cfg)
	if strings.Contains(out, "## [claude]") {
		t.Errorf("expected no '## [claude]' section when no overrides set, got:\n%s", out)
	}
}

// TestRenderAgentsInRenderedFileOnly: claude.agents.* appear in Render output
// (config.resolved.md) but NOT in Resolved (the user-settable list). Render
// includes machine-written sections so sessions reading the file see the full
// active configuration.
func TestRenderAgentsInRenderedFileOnly(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{"atomic-implementer": {Model: "opus"}}

	// Render (config.resolved.md) must include the agents entry.
	rendered := Render(cfg)
	if !strings.Contains(rendered, "claude.agents.atomic-implementer.model") {
		t.Errorf("Render: expected 'claude.agents.atomic-implementer.model' in rendered output, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "opus") {
		t.Errorf("Render: expected 'opus' model in rendered output, got:\n%s", rendered)
	}

	// Resolved (atomic config list) must NOT include agents — machine-written section.
	m := Resolved(cfg)
	for k := range m {
		if strings.HasPrefix(k, "claude.agents") {
			t.Errorf("Resolved: unexpected claude.agents key %q — agents is machine-written, must not appear in config list", k)
		}
	}
}
