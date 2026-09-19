package artifacts

import (
	"strings"
	"testing"
)

func loadFixture(t *testing.T) *Catalog {
	t.Helper()
	cat, err := Load("testdata/repo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cat
}

// The corpus carries one artifact per authored file, with partials already
// expanded for the kinds that compose them.
func TestLoad_EnumeratesCorpus(t *testing.T) {
	cat := loadFixture(t)

	want := []string{
		"agent:agents/atomic-example.md",
		"command:commands/example.md",
		"output-style:output-styles/atomic.md",
		"rule:rules/typescript/style.md",
		"skill:skills/atomic-example/SKILL.md",
		"steering:AGENTS.md",
	}
	got := make([]string, 0, len(cat.Artifacts))
	for _, a := range cat.Artifacts {
		got = append(got, a.ID)
		if a.Source == "CLAUDE.md" {
			t.Errorf("artifact %s is still sourced from a context/CLAUDE.md", a.ID)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("artifacts = %v, want %v", got, want)
	}
}

func TestLoad_ExpandsExactlyOnce(t *testing.T) {
	cat := loadFixture(t)

	agent, ok := cat.Get("agent:agents/atomic-example.md")
	if !ok {
		t.Fatal("agent artifact missing")
	}
	if strings.Contains(string(agent.Body), "{{") {
		t.Errorf("agent body still holds a template directive:\n%s", agent.Body)
	}
	if !strings.Contains(string(agent.Body), "Replace this block with the shared body.") {
		t.Errorf("agent body did not expand the shared partial:\n%s", agent.Body)
	}

	command, ok := cat.Get("command:commands/example.md")
	if !ok {
		t.Fatal("command artifact missing")
	}
	if !strings.Contains(string(command.Body), "Replace this block with the shared body.") {
		t.Errorf("command body did not expand the shared partial:\n%s", command.Body)
	}

	rule, ok := cat.Get("rule:rules/typescript/style.md")
	if !ok {
		t.Fatal("rule artifact missing")
	}
	if !strings.HasPrefix(string(rule.Body), "---\n") {
		t.Errorf("rule body must be copied byte-for-byte; got:\n%s", rule.Body)
	}
}

func TestLoad_SemanticsAndDependencies(t *testing.T) {
	cat := loadFixture(t)

	agent, ok := cat.Get("agent:agents/atomic-example.md")
	if !ok {
		t.Fatal("agent artifact missing")
	}
	if agent.Semantics.Name != "atomic-example" {
		t.Errorf("Name = %q, want %q", agent.Semantics.Name, "atomic-example")
	}
	if !strings.Contains(agent.Semantics.Description, "Fixture agent") {
		t.Errorf("Description = %q, want it to carry the frontmatter description", agent.Semantics.Description)
	}
	want := "skill:skills/atomic-example/SKILL.md"
	if len(agent.Semantics.Requires) != 1 || agent.Semantics.Requires[0] != want {
		t.Errorf("Requires = %v, want [%s]", agent.Semantics.Requires, want)
	}

	if err := cat.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestLoad_Deterministic(t *testing.T) {
	first := loadFixture(t)
	second := loadFixture(t)

	if len(first.Artifacts) != len(second.Artifacts) {
		t.Fatalf("artifact counts differ: %d vs %d", len(first.Artifacts), len(second.Artifacts))
	}
	for i := range first.Artifacts {
		a, b := first.Artifacts[i], second.Artifacts[i]
		if a.ID != b.ID || a.SourceDigest != b.SourceDigest {
			t.Errorf("artifact %d differs: %+v vs %+v", i, a, b)
		}
		if string(a.Body) != string(b.Body) {
			t.Errorf("artifact %s body differs between loads", a.ID)
		}
	}
}

func TestValidate_RejectsUnknownDependency(t *testing.T) {
	cat := &Catalog{
		Artifacts: []Artifact{
			{ID: "agent:agents/atomic-a.md", Kind: KindAgent, Source: "agents/atomic-a.md",
				Semantics: Semantics{Requires: []string{SkillID("atomic-missing")}}},
		},
	}
	err := cat.Validate()
	if err == nil {
		t.Fatal("Validate accepted a dependency the corpus does not carry")
	}
	if !strings.Contains(err.Error(), "atomic-missing") {
		t.Errorf("error %q does not name the missing artifact", err)
	}
}

func TestValidate_RejectsDuplicateIdentity(t *testing.T) {
	dup := Artifact{ID: "agent:agents/atomic-a.md", Kind: KindAgent, Source: "agents/atomic-a.md"}
	cat := &Catalog{Artifacts: []Artifact{dup, dup}}
	if err := cat.Validate(); err == nil {
		t.Fatal("Validate accepted a duplicate identity")
	}
}

// NewCatalog is the entry point for a corpus already carried as bytes — the
// embedded bundle — so it indexes it and parses its semantics from those bytes.
func TestNewCatalog_IndexesRenderedArtifacts(t *testing.T) {
	cat, err := NewCatalog([]Artifact{
		{ID: "command:commands/b.md", Kind: KindCommand, Source: "commands/b.md", Body: []byte("---\ndescription: B\n---\n\nBody.\n")},
		{ID: "command:commands/a.md", Kind: KindCommand, Source: "commands/a.md", Body: []byte("---\ndescription: A\n---\n\nBody.\n")},
	})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	if got := []string{cat.Artifacts[0].Source, cat.Artifacts[1].Source}; got[0] != "commands/a.md" || got[1] != "commands/b.md" {
		t.Errorf("corpus order = %v, want kind-then-source order", got)
	}
	a, ok := cat.Get("command:commands/a.md")
	if !ok || a.Semantics.Description != "A" {
		t.Errorf("Get = %+v, %v; want parsed semantics for the rendered bytes", a, ok)
	}
}

func TestNewCatalog_RefusesAnIdentityItCannotRecord(t *testing.T) {
	if _, err := NewCatalog([]Artifact{{ID: "command:commands/a.md", Kind: KindCommand, Source: "commands/a.md"}}); err != nil {
		t.Fatalf("unique identity refused: %v", err)
	}
	if _, err := NewCatalog([]Artifact{{Kind: KindCommand, Source: "commands/a.md"}}); err == nil {
		t.Error("NewCatalog accepted an artifact with no identity")
	}
	dup := Artifact{ID: "command:commands/a.md", Kind: KindCommand, Source: "commands/a.md"}
	if _, err := NewCatalog([]Artifact{dup, dup}); err == nil {
		t.Error("NewCatalog accepted a duplicate identity")
	}
}
