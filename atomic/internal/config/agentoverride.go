package config

import (
	"fmt"
	"maps"
	"slices"
	"unicode"
)

// AgentOverride is one `[<harness>.agents.<name>]` table; a scalar does not decode.
// An empty field means the harness inherits the session's value.
type AgentOverride struct {
	Model  string `toml:"model,omitempty"`
	Effort string `toml:"effort,omitempty"`
}

type agentsSection struct {
	Agents map[string]AgentOverride `toml:"agents,omitempty"`
}

// AgentHarnesses lists the harness kinds that store agent overrides, in the
// order doctor reports them.
var AgentHarnesses = []string{"claude", "omp", "codex"}

// AgentOverrides returns the overrides stored for one harness kind, nil for a
// kind outside AgentHarnesses.
func (c *Config) AgentOverrides(kind string) map[string]AgentOverride {
	if s := c.agentSection(kind); s != nil {
		return s.Agents
	}
	return nil
}

func (c *Config) agentSection(kind string) *agentsSection {
	switch kind {
	case "claude":
		return &c.Claude
	case "omp":
		return &c.OMP
	case "codex":
		return &c.Codex
	}
	return nil
}

// validEfforts is the Claude Code subagent effort enum: exactly these five string
// levels, no integers. The runtime downgrades gracefully per model when a level
// is not supported.
var validEfforts = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
	"xhigh":  true,
	"max":    true,
}

// ValidateClaudeAgentOverrides fails on a Claude effort outside validEfforts.
// A model and an unknown agent name only warn, through AgentWarnings.
func ValidateClaudeAgentOverrides(agents map[string]AgentOverride) error {
	for _, agentName := range slices.Sorted(maps.Keys(agents)) {
		if effort := agents[agentName].Effort; effort != "" && !validEfforts[effort] {
			return fmt.Errorf("config: claude.agents.%s.effort: invalid effort %q; must be one of: low, medium, high, xhigh, max", agentName, effort)
		}
	}
	return nil
}

// checkAgentTableShapes refuses a non-table `<harness>.agents` or agent entry
// before the typed decode, whose error would name only Go types.
func checkAgentTableShapes(raw map[string]any) error {
	for _, kind := range AgentHarnesses {
		section, ok := raw[kind].(map[string]any)
		if !ok {
			continue
		}
		agents, present := section["agents"]
		if !present {
			continue
		}
		table, ok := agents.(map[string]any)
		if !ok {
			return fmt.Errorf("config: %s.agents: each [%s.agents.<name>] must be a table with model and effort", kind, kind)
		}
		for _, name := range slices.Sorted(maps.Keys(table)) {
			if _, ok := table[name].(map[string]any); !ok {
				return fmt.Errorf("config: %s.agents.%s must be a table with model and effort", kind, name)
			}
		}
	}
	return nil
}

// advisoryEfforts are the OMP and Codex levels known at release. Either harness
// may add one, so a value outside its set warns instead of failing.
var advisoryEfforts = map[string][]string{
	"omp":   {"auto", "inherit", "off", "minimal", "low", "medium", "high", "xhigh", "max"},
	"codex": {"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"},
}

// effortOptionValues is the ordered allowlist, empty "no override" first. Reused
// by the interactive effort Select.
var effortOptionValues = []string{"", "low", "medium", "high", "xhigh", "max"}

// validModelFormat requires no internal whitespace and no ASCII control
// characters, so identifiers like "claude-opus-4-6[1m]" pass. An empty string is
// the caller's concern and treated as well-formed, so an untouched interactive
// Input still validates cleanly.
func validModelFormat(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
