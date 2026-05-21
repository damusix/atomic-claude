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
