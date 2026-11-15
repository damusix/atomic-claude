package config

import (
	"errors"
	"maps"
	"slices"
	"testing"
)

func withAgentTierSelector(t *testing.T, sel func(AgentForm, map[string]AgentOverride) (map[string]AgentOverride, error)) {
	t.Helper()
	orig := AgentTierSelector
	AgentTierSelector = sel
	t.Cleanup(func() { AgentTierSelector = orig })
}

func formFor(t *testing.T, kind string) AgentForm {
	t.Helper()
	form, err := AgentFormFor(kind)
	if err != nil {
		t.Fatal(err)
	}
	return form
}

// Each harness reads its own native model syntax and effort levels, so a form
// borrowed from another harness would offer values the harness rejects.
func TestAgentFormForEachHarness(t *testing.T) {
	tests := []struct {
		kind, placeholder, restart string
		efforts                    []string
	}{
		{"claude", "opus | claude-opus-4-8", "Restart Claude Code sessions to pick up the change.",
			[]string{"", "low", "medium", "high", "xhigh", "max"}},
		{"omp", "@slow | anthropic/claude-opus-4-8", "Restart omp sessions to pick up the change.",
			[]string{"", "auto", "inherit", "off", "minimal", "low", "medium", "high", "xhigh", "max"}},
		{"codex", "gpt-5.2-codex", "Restart Codex sessions to pick up the change.",
			[]string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			form, err := AgentFormFor(tt.kind)
			if err != nil {
				t.Fatal(err)
			}
			if form.Harness != tt.kind || form.ModelPlaceholder != tt.placeholder || form.RestartNote != tt.restart {
				t.Errorf("form = %+v", form)
			}
			if !slices.Equal(form.Efforts, tt.efforts) {
				t.Errorf("efforts = %v, want %v", form.Efforts, tt.efforts)
			}
		})
	}
	if _, err := AgentFormFor("pi"); err == nil {
		t.Error("AgentFormFor accepted a kind that stores no overrides")
	}
}

func TestEditAgentsWritesOnlyTheSelectedHarnessTable(t *testing.T) {
	home := t.TempDir()
	existing := Default()
	existing.Claude.Agents = map[string]AgentOverride{"atomic-reviewer": {Model: "opus"}}
	existing.OMP.Agents = map[string]AgentOverride{"atomic-implementer": {Model: "@slow"}}
	existing.Output.Signals.MaxDepth = 7
	if err := WritePersist(TOMLPath(home), existing); err != nil {
		t.Fatal(err)
	}

	var gotForm AgentForm
	var gotCurrent map[string]AgentOverride
	withAgentTierSelector(t, func(form AgentForm, current map[string]AgentOverride) (map[string]AgentOverride, error) {
		gotForm, gotCurrent = form, maps.Clone(current)
		return map[string]AgentOverride{
			"atomic-implementer": {},
			"atomic-reviewer":    {Model: "@smol", Effort: "minimal"},
		}, nil
	})
	if err := EditAgents(home, formFor(t, "omp")); err != nil {
		t.Fatal(err)
	}

	if gotForm.Harness != "omp" || gotCurrent["atomic-implementer"].Model != "@slow" {
		t.Errorf("form %q pre-filled with %v, want omp's own table", gotForm.Harness, gotCurrent)
	}
	cfg, _, err := Load(TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.OMP.Agents["atomic-implementer"]; ok {
		t.Error("a blank model and effort left the omp entry in place")
	}
	if got := cfg.OMP.Agents["atomic-reviewer"]; got != (AgentOverride{Model: "@smol", Effort: "minimal"}) {
		t.Errorf("omp atomic-reviewer = %+v", got)
	}
	if got := cfg.Claude.Agents["atomic-reviewer"].Model; got != "opus" {
		t.Errorf("editing omp changed the claude table: %q", got)
	}
	if cfg.Output.Signals.MaxDepth != 7 {
		t.Errorf("MaxDepth = %d, want 7 preserved", cfg.Output.Signals.MaxDepth)
	}
}

func TestEditAgentsBlankFormsDropTheTable(t *testing.T) {
	home := t.TempDir()
	existing := Default()
	existing.Codex.Agents = map[string]AgentOverride{"atomic-reviewer": {Model: "gpt-a"}}
	if err := WritePersist(TOMLPath(home), existing); err != nil {
		t.Fatal(err)
	}
	withAgentTierSelector(t, func(AgentForm, map[string]AgentOverride) (map[string]AgentOverride, error) {
		return map[string]AgentOverride{"atomic-reviewer": {}}, nil
	})
	if err := EditAgents(home, formFor(t, "codex")); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Codex.Agents != nil {
		t.Errorf("codex agents = %v, want the table omitted", cfg.Codex.Agents)
	}
}

// Claude Code rejects an effort outside its five levels, so the save refuses it.
func TestEditAgentsRefusesAClaudeEffortOutsideTheEnum(t *testing.T) {
	home := t.TempDir()
	withAgentTierSelector(t, func(AgentForm, map[string]AgentOverride) (map[string]AgentOverride, error) {
		return map[string]AgentOverride{"atomic-implementer": {Effort: "ultra"}}, nil
	})
	if err := EditAgents(home, formFor(t, "claude")); err == nil {
		t.Fatal("EditAgents saved a claude effort outside the enum")
	}
}

func TestEditAgentsPassesSelectorErrorsThrough(t *testing.T) {
	for _, want := range []error{ErrNonInteractiveAgents, ErrAgentsAborted} {
		withAgentTierSelector(t, func(AgentForm, map[string]AgentOverride) (map[string]AgentOverride, error) {
			return nil, want
		})
		if err := EditAgents(t.TempDir(), formFor(t, "claude")); !errors.Is(err, want) {
			t.Errorf("EditAgents = %v, want %v", err, want)
		}
	}
}

func TestDefaultAgentTierSelectorRefusesWithoutATerminal(t *testing.T) {
	orig := isAgentsTTY
	isAgentsTTY = func() bool { return false }
	t.Cleanup(func() { isAgentsTTY = orig })
	form, _ := AgentFormFor("omp")
	if _, err := defaultAgentTierSelector(form, nil); !errors.Is(err, ErrNonInteractiveAgents) {
		t.Errorf("err = %v, want ErrNonInteractiveAgents", err)
	}
}
