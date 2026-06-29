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

// TestRenderAgentsSection: Render includes the [agents] section when overrides are set.
func TestRenderAgentsSection(t *testing.T) {
	cfg := Default()
	cfg.Agents = map[string]string{
		"atomic-implementer":  "sonnet",
		"atomic-investigator": "haiku",
	}
	out := Render(cfg)
	if !strings.Contains(out, "## [agents]") {
		t.Errorf("expected '## [agents]' section in render, got:\n%s", out)
	}
	if !strings.Contains(out, "agents.atomic-implementer") {
		t.Errorf("expected 'agents.atomic-implementer' in render, got:\n%s", out)
	}
	if !strings.Contains(out, "sonnet") {
		t.Errorf("expected 'sonnet' tier in render, got:\n%s", out)
	}
	if !strings.Contains(out, "agents.atomic-investigator") {
		t.Errorf("expected 'agents.atomic-investigator' in render, got:\n%s", out)
	}
	if !strings.Contains(out, "haiku") {
		t.Errorf("expected 'haiku' tier in render, got:\n%s", out)
	}
}

// TestRenderAgentsSectionAbsent: Render omits the [agents] section when no overrides are set.
func TestRenderAgentsSectionAbsent(t *testing.T) {
	cfg := Default()
	out := Render(cfg)
	if strings.Contains(out, "## [agents]") {
		t.Errorf("expected no '## [agents]' section when no overrides set, got:\n%s", out)
	}
}

// TestRenderAgentsInRenderedFileOnly: agents.* appear in Render output (config.resolved.md)
// but NOT in Resolved (the user-settable list). Render includes machine-written
// sections so sessions reading the file see the full active configuration.
func TestRenderAgentsInRenderedFileOnly(t *testing.T) {
	cfg := Default()
	cfg.Agents = map[string]string{"atomic-implementer": "opus"}

	// Render (config.resolved.md) must include the agents entry.
	rendered := Render(cfg)
	if !strings.Contains(rendered, "agents.atomic-implementer") {
		t.Errorf("Render: expected 'agents.atomic-implementer' in rendered output, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "opus") {
		t.Errorf("Render: expected 'opus' tier in rendered output, got:\n%s", rendered)
	}

	// Resolved (atomic config list) must NOT include agents — machine-written section.
	m := Resolved(cfg)
	for k := range m {
		if strings.HasPrefix(k, "agents") {
			t.Errorf("Resolved: unexpected agents key %q — agents is machine-written, must not appear in config list", k)
		}
	}
}
