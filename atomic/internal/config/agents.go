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

// effortOptionLabels maps an effort option value to its label in the
// interactive Select. Terse and factual — no per-agent editorializing.
var effortOptionLabels = map[string]string{
	"":       "(bundled default)",
	"low":    "low",
	"medium": "medium",
	"high":   "high",
	"xhigh":  "xhigh",
	"max":    "max",
}

// applyAgentOverrides merges selections into cfg.Claude.Agents.
// A selection with both Model and Effort empty removes the agent's entry
// from [claude.agents] (no override). Effort is validated against the strict
// enum; model is never a hard failure (lenient — see AgentWarnings for the
// non-fatal malformed-model check). Pure function: no I/O, no TTY interaction.
func applyAgentOverrides(cfg *Config, selections map[string]AgentOverride) error {
	for agentName, ov := range selections {
		if ov.Model == "" && ov.Effort == "" {
			// "leave unchanged / use bundled default" — remove any existing override.
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
	// Nil out empty map so TOML omits the [claude.agents] table when no overrides remain.
	if len(cfg.Claude.Agents) == 0 {
		cfg.Claude.Agents = nil
	}
	return nil
}

// ErrNonInteractiveAgents is returned by AgentTierSelector when the terminal
// is not interactive. Distinct from prompt.ErrNonInteractive so callers in
// cli.go can avoid importing internal/prompt.
var ErrNonInteractiveAgents = errors.New("atomic config agents: non-interactive terminal")

// ErrAgentsAborted is returned when the user aborts the huh form (Ctrl+C).
var ErrAgentsAborted = errors.New("atomic config agents: user aborted")

// validateModelInput is the huh.Input validator for the model field. Empty
// is always valid (no override); a non-empty value must satisfy
// validModelFormat. Named and exported at package scope so it is
// unit-testable without spawning a TTY.
func validateModelInput(s string) error {
	if s == "" || validModelFormat(s) {
		return nil
	}
	return fmt.Errorf("no spaces; use a tier (opus) or a model id (claude-opus-4-8)")
}

// defaultAgentTierSelector presents a huh-backed form — a free-text model
// Input plus an effort Select per agent — and returns the chosen
// AgentOverride per agent. Both fields empty means "use bundled default /
// no override". Returns ErrNonInteractiveAgents when stdin or stdout is not
// a TTY.
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

// isAgentsTTY reports whether both stdin and stdout are connected to a terminal.
// Extracted as a variable for testing.
var isAgentsTTY = func() bool {
	return charmterm.IsTerminal(os.Stdin.Fd()) &&
		charmterm.IsTerminal(os.Stdout.Fd())
}

// DefaultAgentTierSelector is the production AgentTierSelector implementation.
// Exported so tests can restore it after overriding AgentTierSelector.
var DefaultAgentTierSelector = defaultAgentTierSelector

// AgentTierSelector is the injectable seam for the interactive agent override
// selection. Production code uses defaultAgentTierSelector (huh-backed).
// Tests override this to return crafted selections without spawning a TTY.
// Signature: func(cfg *Config) (map[string]AgentOverride, error)
var AgentTierSelector = defaultAgentTierSelector
