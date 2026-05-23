package config

import (
	"strings"
	"testing"
)

// TestRenderByteStable: Render produces identical bytes for the same input (run twice).
func TestRenderByteStable(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "output.intensity", "ultra"); err != nil {
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

// TestRenderDefaultValues: Render includes the default value for output.intensity.
func TestRenderDefaultValues(t *testing.T) {
	cfg := Default()
	out := Render(cfg)
	if !strings.Contains(out, "output.intensity") {
		t.Errorf("expected 'output.intensity' in render, got: %q", out)
	}
	if !strings.Contains(out, "full") {
		t.Errorf("expected default value 'full' in render, got: %q", out)
	}
}

// TestRenderSetValue: Render reflects set value, not default.
func TestRenderSetValue(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "output.intensity", "lite"); err != nil {
		t.Fatal(err)
	}
	out := Render(cfg)
	if !strings.Contains(out, "lite") {
		t.Errorf("expected 'lite' in render after Set, got: %q", out)
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
