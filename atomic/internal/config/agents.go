package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	charmterm "github.com/charmbracelet/x/term"
)

// agentOrder is the fixed display order for the 5 bundled atomic agents.
var agentOrder = []string{
	"atomic-implementer",
	"atomic-investigator",
	"atomic-reviewer",
	"atomic-strategist",
	"atomic-wiki-inferrer",
}

// effortOptionLabels maps an effort option value to its label in the Select.
var effortOptionLabels = map[string]string{
	"":       "(bundled default)",
	"low":    "low",
	"medium": "medium",
	"high":   "high",
	"xhigh":  "xhigh",
	"max":    "max",
}

// applyAgentOverrides merges selections into cfg.Claude.Agents. A selection with
// both fields empty removes the agent's entry. Effort is validated against the
// strict enum; model is lenient and never a hard failure. Pure: no I/O, no TTY.
func applyAgentOverrides(cfg *Config, selections map[string]AgentOverride) error {
	for agentName, ov := range selections {
		if ov.Model == "" && ov.Effort == "" {
			// Leave unchanged: remove any existing override.
			if cfg.Claude.Agents != nil {
				delete(cfg.Claude.Agents, agentName)
			}
			continue
		}
		if ov.Effort != "" && !validEfforts[ov.Effort] {
			return fmt.Errorf("config: claude.agents.%s.effort: invalid effort %q; must be one of: low, medium, high, xhigh, max", agentName, ov.Effort)
		}
		if cfg.Claude.Agents == nil {
			cfg.Claude.Agents = make(map[string]AgentOverride)
		}
		cfg.Claude.Agents[agentName] = ov
	}
	// Nil out the empty map so TOML omits [claude.agents] entirely.
	if len(cfg.Claude.Agents) == 0 {
		cfg.Claude.Agents = nil
	}
	return nil
}

// ErrNonInteractiveAgents is returned when the terminal is not interactive.
// Distinct from prompt.ErrNonInteractive so cli.go need not import that package.
var ErrNonInteractiveAgents = errors.New("atomic config agents: non-interactive terminal")

// ErrAgentsAborted is returned when the user aborts the huh form (Ctrl+C).
var ErrAgentsAborted = errors.New("atomic config agents: user aborted")

// validateModelInput is the model field's huh.Input validator. Empty is always
// valid; a non-empty value must satisfy validModelFormat. At package scope so it
// is unit-testable without spawning a TTY.
func validateModelInput(s string) error {
	if s == "" || validModelFormat(s) {
		return nil
	}
	return fmt.Errorf("no spaces; use a tier (opus) or a model id (claude-opus-4-8)")
}

// defaultAgentTierSelector presents a model Input plus an effort Select per agent
// and returns the chosen AgentOverride. Both fields empty means no override.
// Returns ErrNonInteractiveAgents when stdin or stdout is not a TTY.
func defaultAgentTierSelector(cfg *Config) (map[string]AgentOverride, error) {
	if !isAgentsTTY() {
		return nil, ErrNonInteractiveAgents
	}

	// One model + one effort pointer per agent, pre-populated from cfg.
	models := make(map[string]*string, len(agentOrder))
	efforts := make(map[string]*string, len(agentOrder))
	for _, agent := range agentOrder {
		m := cfg.Claude.Agents[agent].Model
		e := cfg.Claude.Agents[agent].Effort
		models[agent] = &m
		efforts[agent] = &e
	}

	var effortOpts []huh.Option[string]
	for _, v := range effortOptionValues {
		effortOpts = append(effortOpts, huh.NewOption(effortOptionLabels[v], v))
	}

	var fields []huh.Field
	for _, agent := range agentOrder {
		agent := agent // capture

		fields = append(fields, huh.NewInput().
			Title(fmt.Sprintf("Model for %s (blank = bundled default)", agent)).
			Placeholder("opus | claude-opus-4-8").
			Value(models[agent]).
			Validate(validateModelInput),
		)
		fields = append(fields, huh.NewSelect[string]().
			Title(fmt.Sprintf("Effort for %s", agent)).
			Options(effortOpts...).
			Value(efforts[agent]),
		)
	}

	form := huh.NewForm(huh.NewGroup(fields...))
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, ErrAgentsAborted
		}
		return nil, fmt.Errorf("agents tier form: %w", err)
	}

	selections := make(map[string]AgentOverride, len(agentOrder))
	for _, agent := range agentOrder {
		selections[agent] = AgentOverride{Model: *models[agent], Effort: *efforts[agent]}
	}
	return selections, nil
}

// isAgentsTTY reports whether stdin and stdout are both terminals. A variable so
// tests can override it.
var isAgentsTTY = func() bool {
	return charmterm.IsTerminal(os.Stdin.Fd()) &&
		charmterm.IsTerminal(os.Stdout.Fd())
}

// DefaultAgentTierSelector is the production implementation, exported so tests
// can restore it after overriding AgentTierSelector.
var DefaultAgentTierSelector = defaultAgentTierSelector

// AgentTierSelector is the injectable seam for interactive override selection.
// Tests override it to return crafted selections without spawning a TTY.
var AgentTierSelector = defaultAgentTierSelector
