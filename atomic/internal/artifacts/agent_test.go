package artifacts

import (
	"strings"
	"testing"
)

// A canonical agent body is the instruction text with its portable frontmatter
// removed, so a target that carries the metadata natively round-trips the body
// byte-for-byte.
func TestAgentBody_StripsPortableFrontmatter(t *testing.T) {
	cat := loadFixture(t)
	agent, ok := cat.Get("agent:agents/atomic-example.md")
	if !ok {
		t.Fatal("agent artifact missing")
	}

	body, err := AgentBody(agent)
	if err != nil {
		t.Fatalf("AgentBody: %v", err)
	}
	got := string(body)
	if strings.HasPrefix(got, "---") || strings.Contains(got, "name: atomic-example") {
		t.Errorf("body still carries frontmatter:\n%s", got)
	}
	if !strings.HasPrefix(got, "# atomic-example\n") {
		t.Errorf("body lost the instruction text:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Errorf("body is not normalized to one trailing newline: %q", got)
	}
}

func TestAgentBody_RejectsNonAgent(t *testing.T) {
	cat := loadFixture(t)
	command, ok := cat.Get("command:commands/example.md")
	if !ok {
		t.Fatal("command artifact missing")
	}
	if _, err := AgentBody(command); err == nil {
		t.Error("AgentBody accepted a non-agent artifact")
	}
}

// A dependency that names an artifact the corpus does not carry fails, and the
// error names both the agent and the missing identity.
func TestValidateAgent_RejectsUnknownDependency(t *testing.T) {
	cat := loadFixture(t)
	agent, ok := cat.Get("agent:agents/atomic-example.md")
	if !ok {
		t.Fatal("agent artifact missing")
	}
	if err := cat.ValidateAgent(agent); err != nil {
		t.Fatalf("valid agent rejected: %v", err)
	}

	missing := "skill:skills/atomic-missing/SKILL.md"
	broken := agent
	broken.Semantics.Requires = append(append([]string{}, agent.Semantics.Requires...), missing)

	err := cat.ValidateAgent(broken)
	if err == nil {
		t.Fatal("unknown dependency accepted")
	}
	for _, want := range []string{agent.ID, missing} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestValidateAgent_RejectsNonAgent(t *testing.T) {
	cat := loadFixture(t)
	style, ok := cat.Get("output-style:output-styles/atomic.md")
	if !ok {
		t.Fatal("output-style artifact missing")
	}
	if err := cat.ValidateAgent(style); err == nil {
		t.Error("ValidateAgent accepted a non-agent artifact")
	}
}

// A catalog assembled directly, rather than by Load, still resolves a
// dependency by identity.
func TestValidateAgent_HandBuiltCatalogResolves(t *testing.T) {
	skill := Artifact{ID: SkillID("atomic-example"), Kind: KindSkill, Source: "skills/atomic-example/SKILL.md"}
	agent := Artifact{
		ID:        "agent:agents/atomic-example.md",
		Kind:      KindAgent,
		Source:    "agents/atomic-example.md",
		Semantics: Semantics{Name: "atomic-example", Requires: []string{skill.ID}},
	}
	cat := &Catalog{Artifacts: []Artifact{skill, agent}}

	if err := cat.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
	if err := cat.ValidateAgent(agent); err != nil {
		t.Errorf("ValidateAgent: %v", err)
	}
}
