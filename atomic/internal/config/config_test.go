package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestInstallVersionInvalid(t *testing.T) {
	cfg := Default()
	cfg.Install.Version = "not-a-semver"
	if err := Validate(cfg); err == nil {
		t.Fatal("Validate should error on invalid install.version, got nil")
	}
}

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

func TestSetUnknownKeyNoSuggestion(t *testing.T) {
	cfg := Default()
	err := Set(cfg, "zzz.completely_unknown", "x")
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("unexpected suggestion in error: %q", err.Error())
	}
}

func TestSetUnknownValue(t *testing.T) {
	cfg := Default()
	err := Set(cfg, "output.signals.max_depth", "bogus")
	if err == nil {
		t.Fatal("expected error for invalid value, got nil")
	}
	if !strings.Contains(err.Error(), "positive integer") {
		t.Errorf("expected 'positive integer' in error %q", err.Error())
	}
}

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

func TestLoadUnknownLeafKeyWarn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

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
	got, err := Get(cfg, "output.signals.max_depth")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "3" {
		t.Errorf("got max_depth %q, want default %q", got, "3")
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, warns, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load of missing file should not error: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	got, err := Get(cfg, "update.run_doctor")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "true" {
		t.Errorf("got %q, want default %q", got, "true")
	}
}

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

func TestUnsetUnknownKey(t *testing.T) {
	cfg := Default()
	err := Unset(cfg, "output.bogus")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

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

	// No tempfile residue: the rename must have completed.
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

func TestResolvedDefaults(t *testing.T) {
	cfg := Default()
	m := Resolved(cfg)
	if m["output.signals.max_depth"] != "3" {
		t.Errorf("expected default '3', got %q", m["output.signals.max_depth"])
	}
}

func TestUpdateRunDoctorDefault(t *testing.T) {
	cfg := Default()
	if !cfg.Update.RunDoctor {
		t.Error("Default() should set Update.RunDoctor = true")
	}
}

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

// The render.go backfill path: a literal zero-value Config must resolve both
// keys to their defaults before Default() is ever called. Its zero max_depth is
// also the sentinel that triggers the run_doctor default.
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

func TestSetUpdateRunDoctorFalse(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "update.run_doctor", "false"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if cfg.Update.RunDoctor {
		t.Error("Set false should set RunDoctor = false")
	}
}

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

func TestSignalsMaxDepthDefault(t *testing.T) {
	cfg := Default()
	if cfg.Output.Signals.MaxDepth != 3 {
		t.Errorf("Default() Output.Signals.MaxDepth = %d, want 3", cfg.Output.Signals.MaxDepth)
	}
}

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

// --- [claude.agents] table tests ---

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

// Nested tables are the only accepted shape: a scalar under [claude.agents] is a
// plain decode error, with no silent accept and no back-compat seam.
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

// A stale top-level [agents] block from a pre-rename build is no longer a
// recognized section: it warns and is ignored, never loaded as an override.
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

// Model validation is lenient: an arbitrary well-formed id never fails Validate
// and never produces an AgentWarnings entry.
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

// A model with internal whitespace warns rather than failing Validate.
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

// An agent name outside the known set is a warning, not a Validate failure.
func TestAgentsUnknownKeyIsWarn(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{
		"made-up-agent": {Model: "haiku"}, // unknown key, but well-formed model
	}

	if err := Validate(cfg); err != nil {
		t.Errorf("Validate should not error on unknown agent key, got: %v", err)
	}

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

// [claude] is opaque, so its children are never structurally checked by Load.
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

// Model validation has no allowlist, so a forward-reserved name passes.
func TestAgentsFableIsValid(t *testing.T) {
	cfg := Default()
	cfg.Claude.Agents = map[string]AgentOverride{
		"atomic-implementer": {Model: "fable"},
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate should accept 'fable' (lenient model validation), got: %v", err)
	}
}

// The install manifest takes precedence over the static known-agent set.
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

// [claude.agents] is machine-written and not user-settable, so Resolved omits it.
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

// --- harness.dir ---

func TestHarnessDirDefault(t *testing.T) {
	cfg := Default()
	if cfg.Harness.Dir != ".claude" {
		t.Errorf("Default() Harness.Dir = %q, want \".claude\"", cfg.Harness.Dir)
	}
}

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

func TestHarnessDirSetInvalidVariants(t *testing.T) {
	for _, v := range []string{"foo/bar", ".", "..", ""} {
		cfg := Default()
		if err := Set(cfg, "harness.dir", v); err == nil {
			t.Errorf("Set(harness.dir, %q): expected error, got nil", v)
		}
	}
}

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

func TestHarnessDirValidateRejectsBadValue(t *testing.T) {
	cfg := Default()
	cfg.Harness.Dir = "foo/bar"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected Validate to error on harness.dir containing '/'")
	}
}

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

// --- [repl] idle_timeout ---

// Empty means unset, per this key's "empty means unset" contract.
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

func TestReplIdleTimeoutSetInvalidVariants(t *testing.T) {
	for _, v := range []string{"bogus", "0s", "-5m", "0"} {
		cfg := Default()
		if err := Set(cfg, "repl.idle_timeout", v); err == nil {
			t.Errorf("Set(repl.idle_timeout, %q): expected error, got nil", v)
		}
	}
}

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

// An unset idle_timeout must not leave `idle_timeout = ""` on disk.
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

func TestReplIdleTimeoutValidateAcceptsAbsent(t *testing.T) {
	cfg := Default()
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate on config without repl.idle_timeout: %v", err)
	}
}

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

// --- [update] check / stage ---

func TestUpdateCheckDefault(t *testing.T) {
	cfg := Default()
	if !cfg.Update.Check {
		t.Error("Default() should set Update.Check = true")
	}
}

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

func TestSetUpdateCheckFalse(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "update.check", "false"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if cfg.Update.Check {
		t.Error("Set false should set Check = false")
	}
}

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

func TestUpdateStageDefault(t *testing.T) {
	cfg := Default()
	if !cfg.Update.Stage {
		t.Error("Default() should set Update.Stage = true")
	}
}

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

func TestSetUpdateStageFalse(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "update.stage", "false"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if cfg.Update.Stage {
		t.Error("Set false should set Stage = false")
	}
}

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
