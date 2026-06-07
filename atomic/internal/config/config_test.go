package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetUnknownKey: Set returns error on unknown key and includes a suggestion for near-matches.
func TestSetUnknownKey(t *testing.T) {
	cfg := Default()
	err := Set(cfg, "output.signals.max_dept", "5") // typo: max_dept
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	if !strings.Contains(err.Error(), "output.signals.max_depth") {
		t.Errorf("expected suggestion 'output.signals.max_depth' in error %q", err.Error())
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

// TestSetUnknownValue: Set returns error describing the expected type on a bad value.
func TestSetUnknownValue(t *testing.T) {
	cfg := Default()
	err := Set(cfg, "output.signals.max_depth", "bogus")
	if err == nil {
		t.Fatal("expected error for invalid value, got nil")
	}
	// Should describe the expected type.
	if !strings.Contains(err.Error(), "positive integer") {
		t.Errorf("expected 'positive integer' in error %q", err.Error())
	}
}

// TestLoadUnknownKeyWarn: unknown top-level section on Load returns a Warning but no error.
func TestLoadUnknownKeyWarn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	toml := "[output.signals]\nmax_depth = 3\n[unknown_section]\nfoo = \"bar\"\n"
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
	got, err := Get(cfg, "output.signals.max_depth")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "3" {
		t.Errorf("got max_depth %q, want default %q", got, "3")
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
	got, err := Get(cfg, "update.run_doctor")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "true" {
		t.Errorf("got %q, want default %q", got, "true")
	}
}

// TestUnset: Unset reverts to built-in default.
func TestUnset(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "output.signals.max_depth", "7"); err != nil {
		t.Fatal(err)
	}
	if err := Unset(cfg, "output.signals.max_depth"); err != nil {
		t.Fatal(err)
	}
	got, err := Get(cfg, "output.signals.max_depth")
	if err != nil {
		t.Fatal(err)
	}
	if got != "3" {
		t.Errorf("after Unset got %q, want default %q", got, "3")
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
	if m["output.signals.max_depth"] != "3" {
		t.Errorf("expected default '3', got %q", m["output.signals.max_depth"])
	}
}

// TestUpdateRunDoctorDefault: Default() sets update.run_doctor = true.
func TestUpdateRunDoctorDefault(t *testing.T) {
	cfg := Default()
	if !cfg.Update.RunDoctor {
		t.Error("Default() should set Update.RunDoctor = true")
	}
}

// TestUpdateRunDoctorAbsent: absent update.run_doctor in TOML → default true.
func TestUpdateRunDoctorAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	tomlContent := "[output.signals]\nmax_depth = 3\n"
	if err := os.WriteFile(path, []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if !cfg.Update.RunDoctor {
		t.Error("absent update.run_doctor should default to true")
	}
}

// TestUpdateRunDoctorExplicitFalse: explicit update.run_doctor = false round-trips correctly.
func TestUpdateRunDoctorExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	tomlContent := "[update]\nrun_doctor = false\n"
	if err := os.WriteFile(path, []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if cfg.Update.RunDoctor {
		t.Error("explicit run_doctor = false should be false, not true")
	}
}

// TestUpdateRunDoctorExplicitTrue: explicit update.run_doctor = true loads correctly.
func TestUpdateRunDoctorExplicitTrue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	tomlContent := "[update]\nrun_doctor = true\n"
	if err := os.WriteFile(path, []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Update.RunDoctor {
		t.Error("explicit run_doctor = true should be true")
	}
}

// TestUpdateRunDoctorRoundTrip: set false → persist → load → false.
func TestUpdateRunDoctorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	if err := Set(cfg, "update.run_doctor", "false"); err != nil {
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
	if loaded.Update.RunDoctor {
		t.Error("persisted run_doctor=false should load as false")
	}
}

// TestResolvedZeroValueConfig: Resolved(&Config{}) returns defaults for both keys.
// This is the render.go backfill path — a literal zero-value Config must
// produce "3" for max_depth and "true" for run_doctor even before Default() is called.
// The zero-value max_depth (0) is also the sentinel that triggers the run_doctor default.
func TestResolvedZeroValueConfig(t *testing.T) {
	m := Resolved(&Config{})
	if m["output.signals.max_depth"] != "3" {
		t.Errorf("Resolved(&Config{}) output.signals.max_depth = %q, want \"3\"", m["output.signals.max_depth"])
	}
	if m["update.run_doctor"] != "true" {
		t.Errorf("Resolved(&Config{}) update.run_doctor = %q, want \"true\"", m["update.run_doctor"])
	}
}

// TestUpdateRunDoctorTrueRoundTrip: set true → persist → load → still true.
func TestUpdateRunDoctorTrueRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	if err := Set(cfg, "update.run_doctor", "true"); err != nil {
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
	if !loaded.Update.RunDoctor {
		t.Error("persisted run_doctor=true should load as true")
	}
}

// TestSetUpdateRunDoctorBadValue: Set rejects values other than true/false.
func TestSetUpdateRunDoctorBadValue(t *testing.T) {
	cfg := Default()
	err := Set(cfg, "update.run_doctor", "yes")
	if err == nil {
		t.Fatal("expected error for invalid value 'yes'")
	}
	if !strings.Contains(err.Error(), "true") || !strings.Contains(err.Error(), "false") {
		t.Errorf("error should mention allowed values: %v", err)
	}
}

// TestSetUpdateRunDoctorTrue: Set("update.run_doctor", "true") works.
func TestSetUpdateRunDoctorTrue(t *testing.T) {
	cfg := Default()
	cfg.Update.RunDoctor = false
	if err := Set(cfg, "update.run_doctor", "true"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !cfg.Update.RunDoctor {
		t.Error("Set true should set RunDoctor = true")
	}
}

// TestSetUpdateRunDoctorFalse: Set("update.run_doctor", "false") works.
func TestSetUpdateRunDoctorFalse(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "update.run_doctor", "false"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if cfg.Update.RunDoctor {
		t.Error("Set false should set RunDoctor = false")
	}
}

// TestUnsetUpdateRunDoctor: Unset reverts update.run_doctor to default (true).
func TestUnsetUpdateRunDoctor(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "update.run_doctor", "false"); err != nil {
		t.Fatal(err)
	}
	if err := Unset(cfg, "update.run_doctor"); err != nil {
		t.Fatal(err)
	}
	if !cfg.Update.RunDoctor {
		t.Error("after Unset, update.run_doctor should be true (default)")
	}
}

// TestGetUpdateRunDoctor: Get returns "true"/"false" string for update.run_doctor.
func TestGetUpdateRunDoctor(t *testing.T) {
	cfg := Default()
	v, err := Get(cfg, "update.run_doctor")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "true" {
		t.Errorf("default update.run_doctor Get = %q, want \"true\"", v)
	}

	if err := Set(cfg, "update.run_doctor", "false"); err != nil {
		t.Fatal(err)
	}
	v, err = Get(cfg, "update.run_doctor")
	if err != nil {
		t.Fatalf("Get after Set false: %v", err)
	}
	if v != "false" {
		t.Errorf("after Set false, Get = %q, want \"false\"", v)
	}
}

// TestSignalsMaxDepthDefault: Default() sets output.signals.max_depth = 3.
func TestSignalsMaxDepthDefault(t *testing.T) {
	cfg := Default()
	if cfg.Output.Signals.MaxDepth != 3 {
		t.Errorf("Default() Output.Signals.MaxDepth = %d, want 3", cfg.Output.Signals.MaxDepth)
	}
}

// TestSignalsMaxDepthExplicit: explicit output.signals.max_depth in TOML overrides default.
func TestSignalsMaxDepthExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	tomlContent := "[output.signals]\nmax_depth = 5\n"
	if err := os.WriteFile(path, []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if cfg.Output.Signals.MaxDepth != 5 {
		t.Errorf("Output.Signals.MaxDepth = %d, want 5", cfg.Output.Signals.MaxDepth)
	}
}

// TestSignalsMaxDepthNonPositiveValidation: Validate rejects max_depth <= 0.
func TestSignalsMaxDepthNonPositiveValidation(t *testing.T) {
	cfg := Default()
	cfg.Output.Signals.MaxDepth = 0
	if err := Validate(cfg); err == nil {
		t.Fatal("expected Validate to error on max_depth = 0")
	}

	cfg2 := Default()
	cfg2.Output.Signals.MaxDepth = -1
	if err := Validate(cfg2); err == nil {
		t.Fatal("expected Validate to error on max_depth = -1")
	}
}

// TestSignalsMaxDepthGetSet: Get and Set work for output.signals.max_depth.
func TestSignalsMaxDepthGetSet(t *testing.T) {
	cfg := Default()
	v, err := Get(cfg, "output.signals.max_depth")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "3" {
		t.Errorf("default Get = %q, want \"3\"", v)
	}

	if err := Set(cfg, "output.signals.max_depth", "7"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err = Get(cfg, "output.signals.max_depth")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if v != "7" {
		t.Errorf("after Set 7, Get = %q, want \"7\"", v)
	}
}

// TestSignalsMaxDepthUnknownKeyNoFalsePositive: output.signals.max_depth does not
// emit an unknown-key warning when present in a valid TOML file.
func TestSignalsMaxDepthUnknownKeyNoFalsePositive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	tomlContent := "[output.signals]\nmax_depth = 5\n[update]\nrun_doctor = true\n"
	if err := os.WriteFile(path, []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	_, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, w := range warns {
		if strings.Contains(w.Message, "max_depth") || strings.Contains(w.Message, "signals") {
			t.Errorf("unexpected warning for known key: %q", w.Message)
		}
	}
}

// TestSignalsMaxDepthRoundTrip: set → persist → load → get returns the set value.
func TestSignalsMaxDepthRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	if err := Set(cfg, "output.signals.max_depth", "10"); err != nil {
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
	got, err := Get(loaded, "output.signals.max_depth")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "10" {
		t.Errorf("got %q, want \"10\"", got)
	}
}

// TestUpdateUnknownLeafKeyWarn: unknown key under [update] section emits a warning.
func TestUpdateUnknownLeafKeyWarn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	tomlContent := "[update]\nbogus = \"value\"\n"
	if err := os.WriteFile(path, []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	_, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "update.bogus") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning mentioning 'update.bogus', got: %v", warns)
	}
}
