package config

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
)

func bundledAgents() []string {
	var names []string
	for _, a := range embedded.Manifest() {
		if a.Kind == "agent" {
			names = append(names, strings.TrimSuffix(path.Base(a.Target), ".md"))
		}
	}
	return names
}

// AgentForm is one harness's `atomic config agents` form.
type AgentForm struct {
	Harness          string
	ModelPlaceholder string
	// Efforts are the effort choices, the empty no-override choice first.
	Efforts     []string
	RestartNote string
}

// AgentFormFor returns the form of a kind in AgentHarnesses.
func AgentFormFor(kind string) (AgentForm, error) {
	form := AgentForm{Harness: kind}
	switch kind {
	case "claude":
		form.ModelPlaceholder = "opus | claude-opus-4-8"
		form.Efforts = effortOptionValues
		form.RestartNote = "Restart Claude Code sessions to pick up the change."
	case "omp":
		form.ModelPlaceholder = "@slow | anthropic/claude-opus-4-8"
		form.RestartNote = "Restart omp sessions to pick up the change."
	case "codex":
		form.ModelPlaceholder = "gpt-5.2-codex"
		form.RestartNote = "Restart Codex sessions to pick up the change."
	default:
		return AgentForm{}, fmt.Errorf("config: %q stores no agent overrides; want one of %s", kind, strings.Join(AgentHarnesses, ", "))
	}
	if form.Efforts == nil {
		form.Efforts = append([]string{""}, advisoryEfforts[kind]...)
	}
	return form, nil
}

// EditAgents opens form pre-filled from its harness's `[<kind>.agents.<name>]`
// tables and saves the answers to config.toml.
func EditAgents(home string, form AgentForm) error {
	cfg, _, err := Load(TOMLPath(home))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	selections, err := AgentTierSelector(form, cfg.AgentOverrides(form.Harness))
	if err != nil {
		return err
	}
	if err := applyAgentOverrides(cfg, form.Harness, selections); err != nil {
		return err
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// applyAgentOverrides merges selections into kind's table; both fields empty removes
// the entry. Only Claude's effort set is strict; the others warn via AgentWarnings.
func applyAgentOverrides(cfg *Config, kind string, selections map[string]AgentOverride) error {
	section := cfg.agentSection(kind)
	if section == nil {
		return fmt.Errorf("config: %q stores no agent overrides", kind)
	}
	for agentName, ov := range selections {
		if ov.Model == "" && ov.Effort == "" {
			delete(section.Agents, agentName)
			continue
		}
		if kind == "claude" {
			if err := ValidateClaudeAgentOverrides(map[string]AgentOverride{agentName: ov}); err != nil {
				return err
			}
		}
		if section.Agents == nil {
			section.Agents = make(map[string]AgentOverride)
		}
		section.Agents[agentName] = ov
	}
	if len(section.Agents) == 0 {
		section.Agents = nil
	}
	return nil
}

// ErrNonInteractiveAgents is returned when the terminal is not interactive.
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
	return fmt.Errorf("no spaces or control characters")
}

// defaultAgentTierSelector presents a model Input plus an effort Select per agent
// and returns the chosen AgentOverride. Both fields empty means no override.
// Returns ErrNonInteractiveAgents when stdin or stdout is not a TTY.
func defaultAgentTierSelector(form AgentForm, current map[string]AgentOverride) (map[string]AgentOverride, error) {
	if !isAgentsTTY() {
		return nil, ErrNonInteractiveAgents
	}

	agents := bundledAgents()
	models := make(map[string]*string, len(agents))
	efforts := make(map[string]*string, len(agents))
	for _, agent := range agents {
		m := current[agent].Model
		e := current[agent].Effort
		models[agent] = &m
		efforts[agent] = &e
	}

	var effortOpts []huh.Option[string]
	for _, v := range form.Efforts {
		label := v
		if v == "" {
			label = "(no override)"
		}
		effortOpts = append(effortOpts, huh.NewOption(label, v))
	}

	var fields []huh.Field
	for _, agent := range agents {
		fields = append(fields, huh.NewInput().
			Title(fmt.Sprintf("%s model for %s (blank = no override)", form.Harness, agent)).
			Placeholder(form.ModelPlaceholder).
			Value(models[agent]).
			Validate(validateModelInput),
		)
		fields = append(fields, huh.NewSelect[string]().
			Title(fmt.Sprintf("%s effort for %s", form.Harness, agent)).
			Options(effortOpts...).
			Value(efforts[agent]),
		)
	}

	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, ErrAgentsAborted
		}
		return nil, fmt.Errorf("agents tier form: %w", err)
	}

	selections := make(map[string]AgentOverride, len(agents))
	for _, agent := range agents {
		selections[agent] = AgentOverride{Model: *models[agent], Effort: *efforts[agent]}
	}
	return selections, nil
}

// isAgentsTTY is a variable so tests can override it.
var isAgentsTTY = prompt.IsInteractive

// DefaultAgentTierSelector is the production implementation, exported so tests
// can restore it after overriding AgentTierSelector.
var DefaultAgentTierSelector = defaultAgentTierSelector

// AgentTierSelector is the injectable seam for interactive override selection.
// Tests override it to return crafted selections without spawning a TTY.
var AgentTierSelector = defaultAgentTierSelector
