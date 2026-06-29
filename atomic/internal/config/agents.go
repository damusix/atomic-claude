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

// tierOptionValues is the ordered list of selectable tier values.
// The empty string represents "Use bundled default (no override)".
var tierOptionValues = []string{"", "haiku", "sonnet", "opus", "fable"}

// tierOptionLabels maps a tier value to its human-readable label.
var tierOptionLabels = map[string]string{
	"":       "Use bundled default (no override)",
	"haiku":  "haiku (fast, cost-efficient)",
	"sonnet": "sonnet (balanced)",
	"opus":   "opus (most capable)",
	"fable":  "fable (future tier — forward-reserved)",
}

// applyAgentTiers merges selections into cfg.Agents.
// A selection value of "" removes the agent's entry from [agents] (no override).
// A non-empty value is validated against validTiers before writing.
// Pure function: no I/O, no TTY interaction.
func applyAgentTiers(cfg *Config, selections map[string]string) error {
	for agentName, tier := range selections {
		if tier == "" {
			// "leave unchanged / use bundled default" — remove any existing override.
			if cfg.Agents != nil {
				delete(cfg.Agents, agentName)
			}
			continue
		}
		if !validTiers[tier] {
			return fmt.Errorf("config: agents.%s: invalid tier %q; must be one of: haiku, sonnet, opus, fable", agentName, tier)
		}
		if cfg.Agents == nil {
			cfg.Agents = make(map[string]string)
		}
		cfg.Agents[agentName] = tier
	}
	// Nil out empty map so TOML omits the [agents] table when no overrides remain.
	if len(cfg.Agents) == 0 {
		cfg.Agents = nil
	}
	return nil
}

// ErrNonInteractiveAgents is returned by AgentTierSelector when the terminal
// is not interactive. Distinct from prompt.ErrNonInteractive so callers in
// cli.go can avoid importing internal/prompt.
var ErrNonInteractiveAgents = errors.New("atomic config agents: non-interactive terminal")

// ErrAgentsAborted is returned when the user aborts the huh form (Ctrl+C).
var ErrAgentsAborted = errors.New("atomic config agents: user aborted")

// defaultAgentTierSelector presents a huh-backed multi-select form — one
// Select field per agent — and returns the chosen tier per agent.
// "" in the result means "use bundled default / no override".
// Returns ErrNonInteractiveAgents when stdin or stdout is not a TTY.
func defaultAgentTierSelector(cfg *Config) (map[string]string, error) {
	if !isAgentsTTY() {
		return nil, ErrNonInteractiveAgents
	}

	// Build one huh.Select per agent, pre-populating the current value.
	results := make(map[string]*string, len(agentOrder))
	for _, agent := range agentOrder {
		v := cfg.Agents[agent] // "" when absent (no override)
		s := v
		results[agent] = &s
	}

	var fields []huh.Field
	for _, agent := range agentOrder {
		agent := agent // capture
		ptr := results[agent]

		var opts []huh.Option[string]
		for _, v := range tierOptionValues {
			opts = append(opts, huh.NewOption(tierOptionLabels[v], v))
		}

		current := *ptr
		title := fmt.Sprintf("Model tier for %s", agent)
		if current != "" {
			title = fmt.Sprintf("Model tier for %s (current: %s)", agent, current)
		}

		fields = append(fields, huh.NewSelect[string]().
			Title(title).
			Options(opts...).
			Value(ptr),
		)
	}

	form := huh.NewForm(huh.NewGroup(fields...))
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, ErrAgentsAborted
		}
		return nil, fmt.Errorf("agents tier form: %w", err)
	}

	selections := make(map[string]string, len(agentOrder))
	for _, agent := range agentOrder {
		selections[agent] = *results[agent]
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

// AgentTierSelector is the injectable seam for the interactive tier selection.
// Production code uses defaultAgentTierSelector (huh-backed).
// Tests override this to return crafted selections without spawning a TTY.
// Signature: func(cfg *Config) (map[string]string, error)
var AgentTierSelector = defaultAgentTierSelector
