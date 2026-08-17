package config

import (
	"testing"

	"github.com/pelletier/go-toml/v2"
)

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
