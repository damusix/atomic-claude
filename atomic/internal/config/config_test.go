package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallRoundTrip: config with [install] version + artifact lists survives WritePersist→Load.
func TestInstallRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	cfg.Install.Version = "1.2.3"
	cfg.Install.Artifacts.Agents = []string{"atomic-implementer.md", "atomic-reviewer.md"}
	cfg.Install.Artifacts.Commands = []string{"commit.md", "autopilot.md"}
	cfg.Install.Artifacts.Skills = []string{"atomic-tdd"}
	cfg.Install.Artifacts.OutputStyles = []string{"atomic.md"}
	cfg.Install.Artifacts.Rules = []string{"typescript/style.md"}

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

	if loaded.Install.Version != "1.2.3" {
		t.Errorf("Install.Version = %q, want %q", loaded.Install.Version, "1.2.3")
	}
	if len(loaded.Install.Artifacts.Agents) != 2 || loaded.Install.Artifacts.Agents[0] != "atomic-implementer.md" {
		t.Errorf("Install.Artifacts.Agents = %v, want [atomic-implementer.md atomic-reviewer.md]", loaded.Install.Artifacts.Agents)
	}
	if len(loaded.Install.Artifacts.Commands) != 2 || loaded.Install.Artifacts.Commands[0] != "commit.md" {
		t.Errorf("Install.Artifacts.Commands = %v, want [commit.md autopilot.md]", loaded.Install.Artifacts.Commands)
	}
	if len(loaded.Install.Artifacts.Skills) != 1 || loaded.Install.Artifacts.Skills[0] != "atomic-tdd" {
		t.Errorf("Install.Artifacts.Skills = %v, want [atomic-tdd]", loaded.Install.Artifacts.Skills)
	}
	if len(loaded.Install.Artifacts.OutputStyles) != 1 || loaded.Install.Artifacts.OutputStyles[0] != "atomic.md" {
		t.Errorf("Install.Artifacts.OutputStyles = %v, want [atomic.md]", loaded.Install.Artifacts.OutputStyles)
	}
	if len(loaded.Install.Artifacts.Rules) != 1 || loaded.Install.Artifacts.Rules[0] != "typescript/style.md" {
		t.Errorf("Install.Artifacts.Rules = %v, want [typescript/style.md]", loaded.Install.Artifacts.Rules)
	}
}

// TestInstallVersionInvalid: Validate rejects a non-semver install.version.
func TestInstallVersionInvalid(t *testing.T) {
	cfg := Default()
	cfg.Install.Version = "not-a-semver"
	if err := Validate(cfg); err == nil {
		t.Fatal("Validate should error on invalid install.version, got nil")
	}
}

// TestInstallVersionValidVariants: empty version (pre-framework) and standard semver forms pass.
func TestInstallVersionValidVariants(t *testing.T) {
	cases := []string{
		"", // pre-framework install — no [install] table
		"1.0.0",
		"v1.2.3",
		"0.1.0-alpha",
		"2.10.0+build.1",
	}
	for _, v := range cases {
		cfg := Default()
		cfg.Install.Version = v
		if err := Validate(cfg); err != nil {
			t.Errorf("Validate with install.version=%q: unexpected error: %v", v, err)
		}
	}
}

// TestInstallAbsent: Load of a TOML without [install] produces zero-value Install, no warnings, valid.
func TestInstallAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := "[output.signals]\nmax_depth = 3\n[update]\nrun_doctor = true\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings for config without [install]: %v", warns)
	}
	if cfg.Install.Version != "" {
		t.Errorf("Install.Version = %q, want empty (absent)", cfg.Install.Version)
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate on config without [install] should not error: %v", err)
	}
}

// TestInstallNoUnknownKeyWarnings: [install] and [install.artifacts.*] do not produce unknown-key warnings.
func TestInstallNoUnknownKeyWarnings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `[install]
version = "1.0.0"
[install.artifacts]
agents = ["atomic-implementer.md"]
commands = ["commit.md"]
skills = ["atomic-tdd"]
output-styles = ["atomic.md"]
rules = ["typescript/style.md"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, w := range warns {
		if strings.Contains(w.Message, "install") {
			t.Errorf("unexpected warning for [install] key: %q", w.Message)
		}
	}
}

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
	if m["update.check"] != "true" {
		t.Errorf("Resolved(&Config{}) update.check = %q, want \"true\"", m["update.check"])
	}
	if m["update.stage"] != "true" {
		t.Errorf("Resolved(&Config{}) update.stage = %q, want \"true\"", m["update.stage"])
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

// --- [claude.agents] table tests (CP2/CP7) ---

// TestAgentsRoundTrip: config with [claude.agents] (model overrides for all 5
// known agents) survives WritePersist→Load without structural warnings or
// Validate error.
func TestAgentsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{
		"atomic-implementer":   {Model: "sonnet"},
		"atomic-investigator":  {Model: "haiku"},
		"atomic-reviewer":      {Model: "sonnet"},
		"atomic-strategist":    {Model: "opus"},
		"atomic-wiki-inferrer": {Model: "sonnet"},
	}

	if err := WritePersist(path, cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}

	loaded, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected structural warnings: %v", warns)
	}
	if err := Validate(loaded); err != nil {
		t.Errorf("Validate: unexpected error: %v", err)
	}

	if len(loaded.Claude.Agents) != 5 {
		t.Errorf("Agents len = %d, want 5", len(loaded.Claude.Agents))
	}
	if loaded.Claude.Agents["atomic-implementer"].Model != "sonnet" {
		t.Errorf("atomic-implementer = %q, want %q", loaded.Claude.Agents["atomic-implementer"].Model, "sonnet")
	}
	if loaded.Claude.Agents["atomic-investigator"].Model != "haiku" {
		t.Errorf("atomic-investigator = %q, want %q", loaded.Claude.Agents["atomic-investigator"].Model, "haiku")
	}
	if loaded.Claude.Agents["atomic-strategist"].Model != "opus" {
		t.Errorf("atomic-strategist = %q, want %q", loaded.Claude.Agents["atomic-strategist"].Model, "opus")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "[claude.agents.atomic-implementer]") {
		t.Errorf("expected nested [claude.agents.<name>] table header in written output, got:\n%s", raw)
	}
}

// TestAgentsScalarUnderClaudeAgentsIsDecodeError: nested tables are the only
// accepted shape — a scalar value under [claude.agents] is a plain decode
// error (no silent accept, no back-compat seam).
func TestAgentsScalarUnderClaudeAgentsIsDecodeError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	scalar := "[claude.agents]\natomic-implementer = \"opus\"\n"
	if err := os.WriteFile(path, []byte(scalar), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Load(path); err == nil {
		t.Fatal("Load should error on a scalar value under [claude.agents], got nil")
	}
}

// TestAgentsStaleTopLevelBlockIsUnknownKeyWarning: a stale top-level [agents]
// block (left by a pre-rename build) is no longer a recognized section — it
// produces an unknown-key warning and is ignored, not loaded as an override.
func TestAgentsStaleTopLevelBlockIsUnknownKeyWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	stale := "[agents.atomic-implementer]\nmodel = \"opus\"\n"
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Claude.Agents) != 0 {
		t.Errorf("cfg.Claude.Agents should be empty for a stale top-level [agents] block, got %v", cfg.Claude.Agents)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, `unknown key "agents"`) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an unknown-key warning for stale top-level [agents], got: %v", warns)
	}
}

// TestAgentsInvalidEffort: Validate returns an error when an agents effort value
// is outside the allowlist {low, medium, high, xhigh, max}.
func TestAgentsInvalidEffort(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{
		"atomic-implementer": {Effort: "turbo"}, // invalid
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate should error on invalid effort, got nil")
	}
	if !strings.Contains(err.Error(), "atomic-implementer") {
		t.Errorf("error should mention agent name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "turbo") {
		t.Errorf("error should mention the invalid effort value, got: %v", err)
	}
}

// TestAgentsValidEffort: Validate accepts every enum value with no error.
func TestAgentsValidEffort(t *testing.T) {
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		cfg := Default()
		cfg.Claude.Agents = map[string]AgentOverride{
			"atomic-implementer": {Effort: effort},
		}
		if err := Validate(cfg); err != nil {
			t.Errorf("Validate(effort=%q): unexpected error: %v", effort, err)
		}
	}
}

// TestAgentsArbitraryModelNoError: model validation is lenient — an arbitrary
// well-formed model id never fails Validate or produces an AgentWarnings entry.
func TestAgentsArbitraryModelNoError(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{
		"atomic-implementer": {Model: "claude-opus-4-6[1m]"},
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate: unexpected error for well-formed model: %v", err)
	}
	for _, w := range AgentWarnings(cfg) {
		if strings.Contains(w.Message, "questionable value") {
			t.Errorf("unexpected malformed-model warning for well-formed model: %v", w)
		}
	}
}

// TestAgentsMalformedModelWarns: a model with internal whitespace produces a
// warning from AgentWarnings, not a Validate error.
func TestAgentsMalformedModelWarns(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{
		"atomic-implementer": {Model: "claude opus"},
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate should not error on malformed model, got: %v", err)
	}
	warns := AgentWarnings(cfg)
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "questionable value") && strings.Contains(w.Message, "atomic-implementer") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a malformed-model warning, got: %v", warns)
	}
}

// TestAgentsUnknownKeyIsWarn: an agent name not in the known-agent set produces a
// Warning from AgentWarnings (non-fatal), not a Validate error (FAIL).
func TestAgentsUnknownKeyIsWarn(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{
		"made-up-agent": {Model: "haiku"}, // unknown key, but well-formed model
	}

	// Validate must succeed (unknown key is a WARNING, not a FAIL).
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate should not error on unknown agent key, got: %v", err)
	}

	// AgentWarnings must return at least one warning mentioning the unknown key.
	warns := AgentWarnings(cfg)
	if len(warns) == 0 {
		t.Fatal("AgentWarnings should return a warning for unknown agent key, got none")
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Message, "made-up-agent") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("warning should mention 'made-up-agent', got: %v", warns)
	}
}

// TestAgentsAbsent: no [claude.agents] table → no structural warnings, Validate returns nil,
// AgentWarnings returns empty.
func TestAgentsAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := "[output.signals]\nmax_depth = 3\n[update]\nrun_doctor = true\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate on config without [claude.agents]: %v", err)
	}
	agentWarns := AgentWarnings(cfg)
	if len(agentWarns) != 0 {
		t.Errorf("AgentWarnings on config without [claude.agents] = %v, want empty", agentWarns)
	}
}

// TestAgentsNoStructuralWarningsFromLoad: [claude.agents.<name>] with valid
// known-agent keys does not produce structural unknown-key warnings from Load
// — [claude] is opaque, so its children are never structurally checked.
func TestAgentsNoStructuralWarningsFromLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `[claude.agents.atomic-implementer]
model = "sonnet"

[claude.agents.atomic-investigator]
model = "haiku"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, w := range warns {
		if strings.Contains(w.Message, "atomic-implementer") || strings.Contains(w.Message, "claude") {
			t.Errorf("unexpected structural warning for [claude.agents] key: %q", w.Message)
		}
	}
}

// TestAgentsFableIsValid: an arbitrary forward-reserved model name like
// "fable" passes Validate — model validation has no allowlist.
func TestAgentsFableIsValid(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{
		"atomic-implementer": {Model: "fable"},
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate should accept 'fable' (lenient model validation), got: %v", err)
	}
}

// TestAgentsKnownAgentNoWarnWithManifest: when install.artifacts.agents lists the agent,
// AgentWarnings should not warn about it (manifest takes precedence over static set).
func TestAgentsKnownAgentNoWarnWithManifest(t *testing.T) {
	cfg := Default()
	cfg.Install.Artifacts.Agents = []string{"custom-agent.md", "atomic-implementer.md"}
	cfg.Claude.Agents = map[string]AgentOverride{
		"custom-agent":       {Model: "haiku"}, // in manifest → known
		"atomic-implementer": {Model: "sonnet"},
	}
	warns := AgentWarnings(cfg)
	if len(warns) != 0 {
		t.Errorf("AgentWarnings with manifest: expected no warnings, got %v", warns)
	}
}

// TestAgentsNotInConfigList: [claude.agents] keys do not appear in Resolved()
// (machine-written section, not user-settable via `atomic config set`).
func TestAgentsNotInConfigList(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{"atomic-implementer": {Model: "haiku"}}
	m := Resolved(cfg)
	for k := range m {
		if strings.HasPrefix(k, "claude.agents") {
			t.Errorf("Resolved() contains claude.agents key %q — agents is machine-written, must not appear in config list", k)
		}
	}
}

// --- harness.dir (CP2: configurable-state-paths) ---

// TestHarnessDirDefault: Default() sets harness.dir = ".claude".
func TestHarnessDirDefault(t *testing.T) {
	cfg := Default()
	if cfg.Harness.Dir != ".claude" {
		t.Errorf("Default() Harness.Dir = %q, want \".claude\"", cfg.Harness.Dir)
	}
}

// TestHarnessDirAbsent: absent harness.dir in TOML → default ".claude".
func TestHarnessDirAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := "[output.signals]\nmax_depth = 3\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if cfg.Harness.Dir != ".claude" {
		t.Errorf("absent harness.dir should default to \".claude\", got %q", cfg.Harness.Dir)
	}
}

// TestHarnessDirExplicit: explicit harness.dir in TOML overrides the default.
func TestHarnessDirExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := "[harness]\ndir = \".pi\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if cfg.Harness.Dir != ".pi" {
		t.Errorf("Harness.Dir = %q, want \".pi\"", cfg.Harness.Dir)
	}
}

// TestHarnessDirGetSet: Get and Set work for harness.dir.
func TestHarnessDirGetSet(t *testing.T) {
	cfg := Default()
	v, err := Get(cfg, "harness.dir")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != ".claude" {
		t.Errorf("default Get = %q, want \".claude\"", v)
	}

	if err := Set(cfg, "harness.dir", ".pi"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err = Get(cfg, "harness.dir")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if v != ".pi" {
		t.Errorf("after Set .pi, Get = %q, want \".pi\"", v)
	}
}

// TestHarnessDirSetValidVariants: legal values (.pi, pi, .claude) all pass Set.
func TestHarnessDirSetValidVariants(t *testing.T) {
	for _, v := range []string{".pi", "pi", ".claude"} {
		cfg := Default()
		if err := Set(cfg, "harness.dir", v); err != nil {
			t.Errorf("Set(harness.dir, %q): unexpected error: %v", v, err)
		}
		if cfg.Harness.Dir != v {
			t.Errorf("Harness.Dir after Set(%q) = %q, want %q", v, cfg.Harness.Dir, v)
		}
	}
}

// TestHarnessDirSetInvalidVariants: illegal values (foo/bar, ., .., empty) are rejected.
func TestHarnessDirSetInvalidVariants(t *testing.T) {
	for _, v := range []string{"foo/bar", ".", "..", ""} {
		cfg := Default()
		if err := Set(cfg, "harness.dir", v); err == nil {
			t.Errorf("Set(harness.dir, %q): expected error, got nil", v)
		}
	}
}

// TestHarnessDirUnset: Unset reverts harness.dir to the built-in default.
func TestHarnessDirUnset(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "harness.dir", ".pi"); err != nil {
		t.Fatal(err)
	}
	if err := Unset(cfg, "harness.dir"); err != nil {
		t.Fatal(err)
	}
	got, err := Get(cfg, "harness.dir")
	if err != nil {
		t.Fatal(err)
	}
	if got != ".claude" {
		t.Errorf("after Unset got %q, want default \".claude\"", got)
	}
}

// TestHarnessDirRoundTrip: set → persist → load → get returns the set value.
func TestHarnessDirRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	if err := Set(cfg, "harness.dir", ".pi"); err != nil {
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
	got, err := Get(loaded, "harness.dir")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != ".pi" {
		t.Errorf("got %q, want \".pi\"", got)
	}
}

// TestHarnessDirValidateRejectsBadValue: Validate rejects a hand-corrupted value.
func TestHarnessDirValidateRejectsBadValue(t *testing.T) {
	cfg := Default()
	cfg.Harness.Dir = "foo/bar"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected Validate to error on harness.dir containing '/'")
	}
}

// TestHarnessDirNoUnknownKeyWarning: harness.dir in TOML does not produce a
// structural unknown-key warning.
func TestHarnessDirNoUnknownKeyWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := "[harness]\ndir = \".pi\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, w := range warns {
		if strings.Contains(w.Message, "harness") {
			t.Errorf("unexpected warning for known key: %q", w.Message)
		}
	}
}

// TestHarnessDirUnknownKeyTypoSuggestion: Set typo on harness.dir suggests the correct key.
func TestHarnessDirUnknownKeyTypoSuggestion(t *testing.T) {
	cfg := Default()
	err := Set(cfg, "harness.di", ".pi") // typo: harness.di
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	if !strings.Contains(err.Error(), "harness.dir") {
		t.Errorf("expected suggestion 'harness.dir' in error %q", err.Error())
	}
}

// --- [repl] idle_timeout (CP4: atomic-repl) ---

// TestReplIdleTimeoutAbsent: absent [repl] in TOML leaves the field empty
// (unset), matching the schema's "empty means unset" contract for this key.
func TestReplIdleTimeoutAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := "[output.signals]\nmax_depth = 3\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if cfg.Repl.IdleTimeout != "" {
		t.Errorf("absent repl.idle_timeout should be empty, got %q", cfg.Repl.IdleTimeout)
	}
}

// TestReplIdleTimeoutExplicit: explicit [repl] idle_timeout in TOML loads verbatim.
func TestReplIdleTimeoutExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := "[repl]\nidle_timeout = \"2h\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if cfg.Repl.IdleTimeout != "2h" {
		t.Errorf("Repl.IdleTimeout = %q, want \"2h\"", cfg.Repl.IdleTimeout)
	}
}

// TestReplIdleTimeoutGetSet: Get returns the built-in display default when
// unset, and the set value after Set.
func TestReplIdleTimeoutGetSet(t *testing.T) {
	cfg := Default()
	v, err := Get(cfg, "repl.idle_timeout")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "1h" {
		t.Errorf("default Get = %q, want \"1h\"", v)
	}

	if err := Set(cfg, "repl.idle_timeout", "45m"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err = Get(cfg, "repl.idle_timeout")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if v != "45m" {
		t.Errorf("after Set 45m, Get = %q, want \"45m\"", v)
	}
}

// TestReplIdleTimeoutSetInvalidVariants: Set rejects unparseable, zero, and
// negative durations.
func TestReplIdleTimeoutSetInvalidVariants(t *testing.T) {
	for _, v := range []string{"bogus", "0s", "-5m", "0"} {
		cfg := Default()
		if err := Set(cfg, "repl.idle_timeout", v); err == nil {
			t.Errorf("Set(repl.idle_timeout, %q): expected error, got nil", v)
		}
	}
}

// TestReplIdleTimeoutSetValidVariants: Set accepts every time.ParseDuration
// shape the spec names (hours, minutes, seconds).
func TestReplIdleTimeoutSetValidVariants(t *testing.T) {
	for _, v := range []string{"1h", "30m", "90s"} {
		cfg := Default()
		if err := Set(cfg, "repl.idle_timeout", v); err != nil {
			t.Errorf("Set(repl.idle_timeout, %q): unexpected error: %v", v, err)
		}
		if cfg.Repl.IdleTimeout != v {
			t.Errorf("Repl.IdleTimeout after Set(%q) = %q, want %q", v, cfg.Repl.IdleTimeout, v)
		}
	}
}

// TestReplIdleTimeoutUnset: Unset reverts repl.idle_timeout to empty (unset).
func TestReplIdleTimeoutUnset(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "repl.idle_timeout", "2h"); err != nil {
		t.Fatal(err)
	}
	if err := Unset(cfg, "repl.idle_timeout"); err != nil {
		t.Fatal(err)
	}
	got, err := Get(cfg, "repl.idle_timeout")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1h" {
		t.Errorf("after Unset got %q, want default \"1h\"", got)
	}
	if cfg.Repl.IdleTimeout != "" {
		t.Errorf("after Unset, Repl.IdleTimeout field = %q, want empty", cfg.Repl.IdleTimeout)
	}
}

// TestReplIdleTimeoutRoundTrip: set → persist → load → get returns the set value.
func TestReplIdleTimeoutRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	if err := Set(cfg, "repl.idle_timeout", "3h"); err != nil {
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
	got, err := Get(loaded, "repl.idle_timeout")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "3h" {
		t.Errorf("got %q, want \"3h\"", got)
	}
}

// TestReplIdleTimeoutRoundTrip_UnsetOmitsSection: an unset repl.idle_timeout
// round-trips without writing an empty [repl] table — WritePersist must not
// leave `idle_timeout = ""` on disk.
func TestReplIdleTimeoutRoundTrip_UnsetOmitsSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	if err := WritePersist(path, cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "[repl]") {
		t.Errorf("expected no [repl] table for an unset idle_timeout, got:\n%s", raw)
	}

	loaded, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if loaded.Repl.IdleTimeout != "" {
		t.Errorf("Repl.IdleTimeout = %q, want empty", loaded.Repl.IdleTimeout)
	}
}

// TestReplIdleTimeoutValidateRejectsBadValue: Validate rejects an unparseable
// or non-positive repl.idle_timeout.
func TestReplIdleTimeoutValidateRejectsBadValue(t *testing.T) {
	for _, v := range []string{"bogus", "0s", "-1h"} {
		cfg := Default()
		cfg.Repl.IdleTimeout = v
		if err := Validate(cfg); err == nil {
			t.Errorf("Validate with repl.idle_timeout=%q: expected error, got nil", v)
		} else if !strings.Contains(err.Error(), v) {
			t.Errorf("Validate error %q does not name the offending value %q", err.Error(), v)
		}
	}
}

// TestReplIdleTimeoutValidateAcceptsAbsent: Validate does not error when
// repl.idle_timeout is unset (empty).
func TestReplIdleTimeoutValidateAcceptsAbsent(t *testing.T) {
	cfg := Default()
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate on config without repl.idle_timeout: %v", err)
	}
}

// TestReplIdleTimeoutNoUnknownKeyWarning: repl.idle_timeout in TOML does not
// produce a structural unknown-key warning.
func TestReplIdleTimeoutNoUnknownKeyWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := "[repl]\nidle_timeout = \"1h\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, w := range warns {
		if strings.Contains(w.Message, "repl") {
			t.Errorf("unexpected warning for known key: %q", w.Message)
		}
	}
}

// TestReplIdleTimeoutUnknownKeyTypoSuggestion: Set typo on repl.idle_timeout
// suggests the correct key.
func TestReplIdleTimeoutUnknownKeyTypoSuggestion(t *testing.T) {
	cfg := Default()
	err := Set(cfg, "repl.idle_timeot", "1h") // typo: idle_timeot
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	if !strings.Contains(err.Error(), "repl.idle_timeout") {
		t.Errorf("expected suggestion 'repl.idle_timeout' in error %q", err.Error())
	}
}

// --- [update] check / stage (selfupdate-state CP1) ---

// TestUpdateCheckDefault: Default() sets update.check = true.
func TestUpdateCheckDefault(t *testing.T) {
	cfg := Default()
	if !cfg.Update.Check {
		t.Error("Default() should set Update.Check = true")
	}
}

// TestUpdateCheckAbsent: absent update.check in TOML → default true.
func TestUpdateCheckAbsent(t *testing.T) {
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
	if !cfg.Update.Check {
		t.Error("absent update.check should default to true")
	}
}

// TestUpdateCheckExplicitFalse: explicit update.check = false round-trips correctly.
func TestUpdateCheckExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	tomlContent := "[update]\ncheck = false\n"
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
	if cfg.Update.Check {
		t.Error("explicit check = false should be false, not true")
	}
}

// TestUpdateCheckExplicitTrue: explicit update.check = true loads correctly.
func TestUpdateCheckExplicitTrue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	tomlContent := "[update]\ncheck = true\n"
	if err := os.WriteFile(path, []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Update.Check {
		t.Error("explicit check = true should be true")
	}
}

// TestUpdateCheckRoundTrip: set false → persist → load → false.
func TestUpdateCheckRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	if err := Set(cfg, "update.check", "false"); err != nil {
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
	if loaded.Update.Check {
		t.Error("persisted check=false should load as false")
	}
}

// TestSetUpdateCheckBadValue: Set rejects values other than true/false.
func TestSetUpdateCheckBadValue(t *testing.T) {
	cfg := Default()
	err := Set(cfg, "update.check", "yes")
	if err == nil {
		t.Fatal("expected error for invalid value 'yes'")
	}
	if !strings.Contains(err.Error(), "true") || !strings.Contains(err.Error(), "false") {
		t.Errorf("error should mention allowed values: %v", err)
	}
}

// TestSetUpdateCheckTrue: Set("update.check", "true") works.
func TestSetUpdateCheckTrue(t *testing.T) {
	cfg := Default()
	cfg.Update.Check = false
	if err := Set(cfg, "update.check", "true"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !cfg.Update.Check {
		t.Error("Set true should set Check = true")
	}
}

// TestSetUpdateCheckFalse: Set("update.check", "false") works.
func TestSetUpdateCheckFalse(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "update.check", "false"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if cfg.Update.Check {
		t.Error("Set false should set Check = false")
	}
}

// TestUnsetUpdateCheck: Unset reverts update.check to default (true).
func TestUnsetUpdateCheck(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "update.check", "false"); err != nil {
		t.Fatal(err)
	}
	if err := Unset(cfg, "update.check"); err != nil {
		t.Fatal(err)
	}
	if !cfg.Update.Check {
		t.Error("after Unset, update.check should be true (default)")
	}
}

// TestGetUpdateCheck: Get returns "true"/"false" string for update.check.
func TestGetUpdateCheck(t *testing.T) {
	cfg := Default()
	v, err := Get(cfg, "update.check")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "true" {
		t.Errorf("default update.check Get = %q, want \"true\"", v)
	}

	if err := Set(cfg, "update.check", "false"); err != nil {
		t.Fatal(err)
	}
	v, err = Get(cfg, "update.check")
	if err != nil {
		t.Fatalf("Get after Set false: %v", err)
	}
	if v != "false" {
		t.Errorf("after Set false, Get = %q, want \"false\"", v)
	}
}

// TestUpdateStageDefault: Default() sets update.stage = true.
func TestUpdateStageDefault(t *testing.T) {
	cfg := Default()
	if !cfg.Update.Stage {
		t.Error("Default() should set Update.Stage = true")
	}
}

// TestUpdateStageAbsent: absent update.stage in TOML → default true.
func TestUpdateStageAbsent(t *testing.T) {
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
	if !cfg.Update.Stage {
		t.Error("absent update.stage should default to true")
	}
}

// TestUpdateStageExplicitFalse: explicit update.stage = false round-trips correctly.
func TestUpdateStageExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	tomlContent := "[update]\nstage = false\n"
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
	if cfg.Update.Stage {
		t.Error("explicit stage = false should be false, not true")
	}
}

// TestUpdateStageExplicitTrue: explicit update.stage = true loads correctly.
func TestUpdateStageExplicitTrue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	tomlContent := "[update]\nstage = true\n"
	if err := os.WriteFile(path, []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Update.Stage {
		t.Error("explicit stage = true should be true")
	}
}

// TestUpdateStageRoundTrip: set false → persist → load → false.
func TestUpdateStageRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	if err := Set(cfg, "update.stage", "false"); err != nil {
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
	if loaded.Update.Stage {
		t.Error("persisted stage=false should load as false")
	}
}

// TestSetUpdateStageBadValue: Set rejects values other than true/false.
func TestSetUpdateStageBadValue(t *testing.T) {
	cfg := Default()
	err := Set(cfg, "update.stage", "yes")
	if err == nil {
		t.Fatal("expected error for invalid value 'yes'")
	}
	if !strings.Contains(err.Error(), "true") || !strings.Contains(err.Error(), "false") {
		t.Errorf("error should mention allowed values: %v", err)
	}
}

// TestSetUpdateStageTrue: Set("update.stage", "true") works.
func TestSetUpdateStageTrue(t *testing.T) {
	cfg := Default()
	cfg.Update.Stage = false
	if err := Set(cfg, "update.stage", "true"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !cfg.Update.Stage {
		t.Error("Set true should set Stage = true")
	}
}

// TestSetUpdateStageFalse: Set("update.stage", "false") works.
func TestSetUpdateStageFalse(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "update.stage", "false"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if cfg.Update.Stage {
		t.Error("Set false should set Stage = false")
	}
}

// TestUnsetUpdateStage: Unset reverts update.stage to default (true).
func TestUnsetUpdateStage(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "update.stage", "false"); err != nil {
		t.Fatal(err)
	}
	if err := Unset(cfg, "update.stage"); err != nil {
		t.Fatal(err)
	}
	if !cfg.Update.Stage {
		t.Error("after Unset, update.stage should be true (default)")
	}
}

// TestGetUpdateStage: Get returns "true"/"false" string for update.stage.
func TestGetUpdateStage(t *testing.T) {
	cfg := Default()
	v, err := Get(cfg, "update.stage")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "true" {
		t.Errorf("default update.stage Get = %q, want \"true\"", v)
	}

	if err := Set(cfg, "update.stage", "false"); err != nil {
		t.Fatal(err)
	}
	v, err = Get(cfg, "update.stage")
	if err != nil {
		t.Fatalf("Get after Set false: %v", err)
	}
	if v != "false" {
		t.Errorf("after Set false, Get = %q, want \"false\"", v)
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
