package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRoundTrip: set → persist → load → get returns the set value.
func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	if err := Set(cfg, "output.intensity", "lite"); err != nil {
		t.Fatalf("Set: %v", err)
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

	got, err := Get(loaded, "output.intensity")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "lite" {
		t.Errorf("got %q, want %q", got, "lite")
	}
}

// TestSetUnknownKey: Set returns error on unknown key and includes a suggestion for near-matches.
func TestSetUnknownKey(t *testing.T) {
	cfg := Default()
	err := Set(cfg, "outpot.intensity", "full") // typo: outpot
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	if !strings.Contains(err.Error(), "output.intensity") {
		t.Errorf("expected suggestion 'output.intensity' in error %q", err.Error())
	}
}

// TestSetUnknownKeyNoSuggestion: Set returns error for keys with no close match (no suggestion).
func TestSetUnknownKeyNoSuggestion(t *testing.T) {
	cfg := Default()
	err := Set(cfg, "zzz.completely_unknown", "x")
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	// Should not contain any suggestion
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("unexpected suggestion in error: %q", err.Error())
	}
}

// TestSetUnknownValue: Set returns error listing allowed values on bad enum.
func TestSetUnknownValue(t *testing.T) {
	cfg := Default()
	err := Set(cfg, "output.intensity", "bogus")
	if err == nil {
		t.Fatal("expected error for unknown value, got nil")
	}
	// Should mention allowed values
	if !strings.Contains(err.Error(), "lite") || !strings.Contains(err.Error(), "full") || !strings.Contains(err.Error(), "ultra") {
		t.Errorf("expected allowed values in error %q", err.Error())
	}
}

// TestLoadUnknownKeyWarn: unknown top-level section on Load returns a Warning but no error.
func TestLoadUnknownKeyWarn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	toml := "[output]\nintensity = \"full\"\n[unknown_section]\nfoo = \"bar\"\n"
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(warns) == 0 {
		t.Error("expected at least one warning for unknown key, got none")
	}
}

// TestLoadUnknownLeafKeyWarn: an unknown leaf key inside a known section produces
// a Warning mentioning the dotted path, and the valid key retains its default.
func TestLoadUnknownLeafKeyWarn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// [output] is a known section; foo is an unknown leaf key within it.
	tomlContent := "[output]\nfoo = \"bar\"\n"
	if err := os.WriteFile(path, []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(warns) == 0 {
		t.Error("expected at least one warning for unknown leaf key, got none")
	}
	// Warning must mention the dotted key.
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "output.foo") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning mentioning 'output.foo', got: %v", warns)
	}
	// Valid keys still resolve to defaults.
	got, err := Get(cfg, "output.intensity")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != intensityDefault {
		t.Errorf("got intensity %q, want default %q", got, intensityDefault)
	}
}

// TestLoadMissingFile: missing file returns empty Config with no warnings/error.
func TestLoadMissingFile(t *testing.T) {
	cfg, warns, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load of missing file should not error: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	// Should return default values
	got, err := Get(cfg, "output.intensity")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "full" {
		t.Errorf("got %q, want default %q", got, "full")
	}
}

// TestValidate: Validate rejects bad enum values.
func TestValidate(t *testing.T) {
	cfg := &Config{}
	cfg.Output.Intensity = "invalid"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected Validate to error on invalid intensity")
	}
}

// TestUnset: Unset reverts to built-in default.
func TestUnset(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "output.intensity", "lite"); err != nil {
		t.Fatal(err)
	}
	if err := Unset(cfg, "output.intensity"); err != nil {
		t.Fatal(err)
	}
	got, err := Get(cfg, "output.intensity")
	if err != nil {
		t.Fatal(err)
	}
	if got != "full" {
		t.Errorf("after Unset got %q, want default %q", got, "full")
	}
}

// TestUnsetUnknownKey: Unset returns error for unknown keys.
func TestUnsetUnknownKey(t *testing.T) {
	cfg := Default()
	err := Unset(cfg, "output.bogus")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

// TestWritePersistAtomic: WritePersist creates parent dir if absent and uses atomic write.
// Also asserts no tempfile residue remains after the call — the rename must have completed.
func TestWritePersistAtomic(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested")
	path := filepath.Join(nested, "config.toml")

	cfg := Default()
	if err := WritePersist(path, cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// No tempfile residue: after a successful WritePersist, the rename must have
	// completed and no *.toml.tmp files may remain in the parent directory.
	entries, err := os.ReadDir(nested)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".toml.tmp") {
			t.Errorf("tempfile residue found after WritePersist: %s", e.Name())
		}
	}
}

// TestResolvedDefaults: Resolved fills in defaults for empty Config.
func TestResolvedDefaults(t *testing.T) {
	cfg := Default()
	m := Resolved(cfg)
	if m["output.intensity"] != "full" {
		t.Errorf("expected default 'full', got %q", m["output.intensity"])
	}
}
