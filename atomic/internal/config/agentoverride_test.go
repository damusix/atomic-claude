package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func loadString(t *testing.T, content string) (*Config, []Warning, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func setOverrides(cfg *Config, kind string, m map[string]AgentOverride) {
	cfg.agentSection(kind).Agents = m
}

// Every harness stores overrides in the same shape, so one AgentOverride
// decodes all three tables and none of them is a structural unknown key.
func TestAgentOverrideTablesDecodePerHarness(t *testing.T) {
	cfg, warns, err := loadString(t, `[claude.agents.atomic-implementer]
model = "opus"
effort = "high"

[omp.agents.atomic-implementer]
model = "anthropic/claude-opus-4-8:high"
effort = "auto"

[codex.agents.atomic-implementer]
model = "gpt-5.5"
effort = "ultra"
`)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("structural warnings: %v", warns)
	}
	want := map[string]AgentOverride{
		"claude": {Model: "opus", Effort: "high"},
		"omp":    {Model: "anthropic/claude-opus-4-8:high", Effort: "auto"},
		"codex":  {Model: "gpt-5.5", Effort: "ultra"},
	}
	for kind, ov := range want {
		if got := cfg.AgentOverrides(kind)["atomic-implementer"]; got != ov {
			t.Errorf("%s override = %+v, want %+v", kind, got, ov)
		}
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// Only the contents under `<kind>.agents` are free-form, so a misspelled table
// name warns instead of storing an override nothing reads.
func TestAgentOverrideTableTyposWarn(t *testing.T) {
	for body, key := range map[string]string{
		"[omp.agent.atomic-implementer]\nmodel = \"x\"\n": "omp.agent",
		"[codex]\nagnts = 1\n":                            "codex.agnts",
		"[claude]\nmodel = \"opus\"\n":                    "claude.model",
	} {
		_, warns, err := loadString(t, body)
		if err != nil {
			t.Fatalf("Load(%q): %v", body, err)
		}
		found := false
		for _, w := range warns {
			if w.Key == key {
				found = true
			}
		}
		if !found {
			t.Errorf("Load(%q) warnings = %v, want one for %s", body, warns, key)
		}
	}
}

// A scalar has no model/effort split to decode into, so it is refused rather
// than silently dropped, under every harness and at both nesting levels.
func TestAgentOverrideScalarIsDecodeErrorPerHarness(t *testing.T) {
	for _, kind := range []string{"claude", "omp", "codex"} {
		for _, body := range []string{
			"[" + kind + ".agents]\natomic-implementer = \"opus\"\n",
			"[" + kind + "]\nagents = \"opus\"\n",
		} {
			_, _, err := loadString(t, body)
			if err == nil {
				t.Errorf("Load(%q) = nil error, want a decode error", body)
			} else if !strings.Contains(err.Error(), kind+".agents") || !strings.Contains(err.Error(), "must be a table with model and effort") {
				t.Errorf("Load(%q) error %q does not name the table shape", body, err)
			}
		}
	}
}

// Only Claude rejects an unknown effort: OMP and Codex accept levels Atomic
// does not track yet, so theirs warn instead of blocking every verb.
func TestAgentOverrideEffortValidationPerHarness(t *testing.T) {
	cases := []struct {
		kind, effort string
		fail, warn   bool
	}{
		{"claude", "max", false, false},
		{"claude", "ultra", true, false},
		{"omp", "auto", false, false},
		{"omp", "inherit", false, false},
		{"omp", "ultra", false, true},
		{"codex", "none", false, false},
		{"codex", "ultra", false, false},
		{"codex", "auto", false, true},
	}
	for _, tc := range cases {
		cfg := Default()
		setOverrides(cfg, tc.kind, map[string]AgentOverride{"atomic-implementer": {Effort: tc.effort}})
		if err := Validate(cfg); (err != nil) != tc.fail {
			t.Errorf("%s effort %q: Validate err = %v, want fail=%v", tc.kind, tc.effort, err, tc.fail)
		}
		warned := false
		for _, w := range AgentWarnings(cfg) {
			if strings.Contains(w.Message, tc.kind+".agents.atomic-implementer.effort") {
				warned = true
			}
		}
		if warned != tc.warn {
			t.Errorf("%s effort %q: warned = %v, want %v", tc.kind, tc.effort, warned, tc.warn)
		}
	}
}

func TestAgentOverrideWarningsCoverEveryHarness(t *testing.T) {
	for _, kind := range []string{"omp", "codex"} {
		cfg := Default()
		setOverrides(cfg, kind, map[string]AgentOverride{
			"made-up-agent":      {Model: "x"},
			"atomic-implementer": {Model: "two words"},
		})
		var msgs []string
		for _, w := range AgentWarnings(cfg) {
			msgs = append(msgs, w.Message)
		}
		joined := strings.Join(msgs, "\n")
		if !strings.Contains(joined, kind+".agents.made-up-agent: unknown agent") {
			t.Errorf("%s: no unknown-agent warning in %q", kind, joined)
		}
		if !strings.Contains(joined, kind+".agents.atomic-implementer.model: questionable value") {
			t.Errorf("%s: no malformed-model warning in %q", kind, joined)
		}
	}
}

func TestAgentOverrideTablesSurviveWritePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	setOverrides(cfg, "omp", map[string]AgentOverride{"atomic-reviewer": {Model: "@slow"}})
	setOverrides(cfg, "codex", map[string]AgentOverride{"atomic-reviewer": {Effort: "high"}})
	if err := WritePersist(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentOverrides("omp")["atomic-reviewer"].Model != "@slow" || got.AgentOverrides("codex")["atomic-reviewer"].Effort != "high" {
		t.Errorf("round trip lost overrides: omp=%v codex=%v", got.AgentOverrides("omp"), got.AgentOverrides("codex"))
	}
}

// config list shows each set field under its harness's own dotted prefix, so
// one harness's overrides read as one contiguous group.
func TestResolvedListsAgentOverridesByHarness(t *testing.T) {
	cfg := Default()
	setOverrides(cfg, "claude", map[string]AgentOverride{"atomic-implementer": {Model: "opus"}})
	setOverrides(cfg, "codex", map[string]AgentOverride{"atomic-implementer": {Effort: "high"}})
	m := Resolved(cfg)
	if m["claude.agents.atomic-implementer.model"] != "opus" {
		t.Errorf("claude model = %q", m["claude.agents.atomic-implementer.model"])
	}
	if m["codex.agents.atomic-implementer.effort"] != "high" {
		t.Errorf("codex effort = %q", m["codex.agents.atomic-implementer.effort"])
	}
	for _, k := range []string{"claude.agents.atomic-implementer.effort", "codex.agents.atomic-implementer.model"} {
		if _, ok := m[k]; ok {
			t.Errorf("empty field %s listed", k)
		}
	}
}

func TestAgentOverride_UnmarshalNestedTable(t *testing.T) {
	var m map[string]AgentOverride
	input := "[atomic-implementer]\nmodel = \"opus\"\neffort = \"high\"\n"
	if err := toml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := m["atomic-implementer"]
	want := AgentOverride{Model: "opus", Effort: "high"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestAgentOverride_UnmarshalEffortOnly(t *testing.T) {
	var m map[string]AgentOverride
	input := "[atomic-implementer]\neffort = \"medium\"\n"
	if err := toml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := m["atomic-implementer"]
	want := AgentOverride{Effort: "medium"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Lenient: no internal whitespace or control characters, everything else passes.
func TestValidModelFormat(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", true},
		{"simple", "opus", true},
		{"dashed digits", "claude-opus-4-8", true},
		{"brackets suffix", "claude-opus-4-6[1m]", true},
		{"slash", "provider/model", true},
		{"dot", "claude.opus.4", true},
		{"internal space", "claude opus", false},
		{"tab", "claude\topus", false},
		{"newline", "claude\nopus", false},
		{"control char", "claude\x01opus", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validModelFormat(tc.in); got != tc.want {
				t.Errorf("validModelFormat(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidEfforts(t *testing.T) {
	for _, v := range []string{"low", "medium", "high", "xhigh", "max"} {
		if !validEfforts[v] {
			t.Errorf("validEfforts[%q] = false, want true", v)
		}
	}
	for _, v := range []string{"", "turbo", "1", "extreme"} {
		if validEfforts[v] {
			t.Errorf("validEfforts[%q] = true, want false", v)
		}
	}
}
