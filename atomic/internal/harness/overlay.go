package harness

import (
	"bytes"
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/pelletier/go-toml/v2"
)

type nativeKeys struct {
	Model  string
	Effort string
}

// markdownAgentKeys are the frontmatter keys the Markdown-agent harnesses read.
var markdownAgentKeys = map[Kind]nativeKeys{
	KindClaude: {Model: "model", Effort: "effort"},
	KindOMP:    {Model: "model", Effort: "thinking"},
}

// codexOverlaidDoc is a Codex agent document with the override keys appended
// after the projected ones.
type codexOverlaidDoc struct {
	CodexAgentDoc
	Model           string `toml:"model,omitempty"`
	ReasoningEffort string `toml:"model_reasoning_effort,omitempty"`
}

// Overlay writes ov under kind's native keys into a projected agent document; the result is
// both the written and the claimed bytes. An empty field writes no key, so no default is ever supplied.
func Overlay(kind Kind, projected []byte, ov config.AgentOverride) ([]byte, error) {
	keys, markdown := markdownAgentKeys[kind]
	if !markdown && kind != KindCodex {
		return nil, fmt.Errorf("harness: no agent override keys for %q", kind)
	}
	if ov.Model == "" && ov.Effort == "" {
		return projected, nil
	}
	if markdown {
		out, err := claudeinstall.OverlayFrontmatter(projected, keys.Model, keys.Effort, ov)
		if err != nil {
			return nil, fmt.Errorf("harness: overlay %s agent: %w", kind, err)
		}
		return out, nil
	}
	var doc CodexAgentDoc
	if err := toml.NewDecoder(bytes.NewReader(projected)).DisallowUnknownFields().Decode(&doc); err != nil {
		return nil, fmt.Errorf("harness: overlay codex agent: %w", err)
	}
	out, err := toml.Marshal(codexOverlaidDoc{CodexAgentDoc: doc, Model: ov.Model, ReasoningEffort: ov.Effort})
	if err != nil {
		return nil, fmt.Errorf("harness: overlay codex agent: %w", err)
	}
	return out, nil
}
