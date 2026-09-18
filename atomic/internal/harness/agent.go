package harness

import (
	"fmt"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/pelletier/go-toml/v2"
)

// Agent projections render one canonical agent into a target's native document
// without touching the canonical corpus. They are offline: no enrollment,
// journal, or native write is involved, so the Codex TOML boundary is proven
// before any Codex runtime exists.
//
// The CP0 capability record gates every native claim: canonical metadata is
// carried only when the target's versioned record proves the surface. No
// tested harness has an agent-metadata row beyond the portable name/description
// pair, so a projection degrades unproven canonical fields to unsupported and
// reports them instead of copying them with an implied native claim. Claude is
// the exception: its native agent file uses exactly the canonical name,
// description, and skills keys, so the bytes ship unchanged with no CP0 row
// needed.

// userPolicyKeys are canonical agent metadata keys that select a model, a
// reasoning effort, or a tool surface. User model policy is authoritative, so
// no projection writes them into native metadata, and an agent that declares
// one is refused rather than projected.
var userPolicyKeys = []string{"model", "effort", "tools", "disallowedTools", "mcpServers"}

// ClaudeAgent renders a canonical agent into Claude Code's native agent file.
// The canonical frontmatter already carries only the Claude-native name,
// description, and skills keys, so the bytes ship unchanged; the projection
// records the native path, delivery class, and digest.
func ClaudeAgent(cat *artifacts.Catalog, a artifacts.Artifact) (artifacts.Projection, error) {
	if err := checkedAgent(cat, a); err != nil {
		return artifacts.Projection{}, err
	}
	return artifacts.Projection{
		Artifact:    a.ID,
		Target:      artifacts.TargetClaude,
		Path:        agentMarkdownPath(a),
		Bytes:       a.Body,
		Delivery:    artifacts.DeliveryDirect,
		Enforcement: artifacts.EnforcementUnsupported,
		Digest:      artifacts.ProjectionDigest(a.Body),
	}, nil
}

// OMPAgent renders a canonical agent into OMP's native agent Markdown: the
// canonical instruction body with only the frontmatter fields OMP's capability
// record proves. OMP 18.1.18 has no agent-metadata row beyond the portable
// name/description pair, so the canonical skills dependency list is not a
// proven native surface: it is dropped and reported unsupported rather than
// copied with an implied native claim. The instruction body itself is
// preserved byte-for-byte.
func OMPAgent(cat *artifacts.Catalog, a artifacts.Artifact) (artifacts.Projection, error) {
	if err := checkedAgent(cat, a); err != nil {
		return artifacts.Projection{}, err
	}
	body, err := artifacts.AgentBody(a)
	if err != nil {
		return artifacts.Projection{}, err
	}

	var unsupported []string
	fields := []frontmatter.KV{{Key: "name", Value: a.Semantics.Name}}
	if a.Semantics.Description != "" {
		fields = append(fields, frontmatter.KV{Key: "description", Value: a.Semantics.Description})
	}
	if len(a.Semantics.Requires) > 0 {
		unsupported = append(unsupported, "skills")
	}

	doc, err := frontmatter.EmitOrdered(fields, string(body))
	if err != nil {
		return artifacts.Projection{}, fmt.Errorf("harness: project %s for OMP: %w", a.ID, err)
	}

	return artifacts.Projection{
		Artifact:    a.ID,
		Target:      artifacts.TargetOMP,
		Path:        agentMarkdownPath(a),
		Bytes:       []byte(doc),
		Delivery:    artifacts.DeliveryDirect,
		Enforcement: artifacts.EnforcementUnsupported,
		Unsupported: unsupported,
		Digest:      artifacts.ProjectionDigest([]byte(doc)),
	}, nil
}

// CodexAgentDoc is Codex's standalone custom-agent document. It carries only
// the three keys name, description, and developer_instructions; model,
// reasoning effort, sandbox, and tool restrictions stay with the user's Codex
// configuration.
type CodexAgentDoc struct {
	Name                  string `toml:"name"`
	Description           string `toml:"description"`
	DeveloperInstructions string `toml:"developer_instructions"`
}

// CodexAgent renders a canonical agent into Codex's TOML custom-agent document.
// developer_instructions is the canonical instruction body, so decoding the
// document returns it byte-for-byte while the portable metadata travels as the
// native name and description keys. Codex has no agent-metadata row for skill
// preload, so a declared skills dependency is reported unsupported rather than
// emitted as configuration.
func CodexAgent(cat *artifacts.Catalog, a artifacts.Artifact) (artifacts.Projection, error) {
	if err := checkedAgent(cat, a); err != nil {
		return artifacts.Projection{}, err
	}
	body, err := artifacts.AgentBody(a)
	if err != nil {
		return artifacts.Projection{}, err
	}

	data, err := toml.Marshal(CodexAgentDoc{
		Name:                  a.Semantics.Name,
		Description:           a.Semantics.Description,
		DeveloperInstructions: string(body),
	})
	if err != nil {
		return artifacts.Projection{}, fmt.Errorf("harness: project %s for Codex: %w", a.ID, err)
	}

	var unsupported []string
	if len(a.Semantics.Requires) > 0 {
		unsupported = append(unsupported, "skills")
	}

	return artifacts.Projection{
		Artifact:    a.ID,
		Target:      artifacts.TargetCodex,
		Path:        "agents/" + a.Semantics.Name + ".toml",
		Bytes:       data,
		Delivery:    artifacts.DeliveryDirect,
		Enforcement: artifacts.EnforcementUnsupported,
		Unsupported: unsupported,
		Digest:      artifacts.ProjectionDigest(data),
	}, nil
}

// DecodeCodexAgent parses a projected Codex agent document back into its fields.
func DecodeCodexAgent(data []byte) (CodexAgentDoc, error) {
	var doc CodexAgentDoc
	if err := toml.Unmarshal(data, &doc); err != nil {
		return CodexAgentDoc{}, fmt.Errorf("harness: decode Codex agent document: %w", err)
	}
	return doc, nil
}

// checkedAgent validates a canonical agent before projection: its declared
// dependencies must resolve in the catalog, it must carry an identity, and it
// must not declare a model, effort, or tool restriction the user's own policy
// owns.
func checkedAgent(cat *artifacts.Catalog, a artifacts.Artifact) error {
	if err := cat.ValidateAgent(a); err != nil {
		return err
	}
	if a.Semantics.Name == "" {
		return fmt.Errorf("harness: agent %s carries no name", a.ID)
	}
	meta, _, err := frontmatter.Parse(string(a.Body))
	if err != nil {
		return fmt.Errorf("harness: agent %s frontmatter: %w", a.ID, err)
	}
	for _, key := range userPolicyKeys {
		if _, ok := meta[key]; ok {
			return fmt.Errorf("harness: agent %s declares %q, which user model policy owns; a projection never writes model, effort, or tool restrictions", a.ID, key)
		}
	}
	return nil
}

// agentMarkdownPath is the native path a canonical agent's Markdown document
// installs to, relative to the target root.
func agentMarkdownPath(a artifacts.Artifact) string {
	return "agents/" + filepath.Base(a.Source)
}
