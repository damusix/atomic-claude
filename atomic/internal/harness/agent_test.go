package harness

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

var update = flag.Bool("update", false, "regenerate golden testdata fixtures")

// repoRoot is the checkout that carries the canonical corpus this test projects.
const repoRoot = "../../.."

// representativeAgent is the agent whose three native projections are golden:
// it declares skill dependencies and carries the richest contract.
const representativeAgent = "agent:agents/atomic-reviewer.md"

func loadCorpus(t *testing.T) *artifacts.Catalog {
	t.Helper()
	cat, err := artifacts.Load(repoRoot)
	if err != nil {
		t.Fatalf("artifacts.Load: %v", err)
	}
	for _, a := range cat.OfKind(artifacts.KindAgent) {
		if err := cat.ValidateAgent(a); err != nil {
			t.Fatalf("agent dependency validation: %v", err)
		}
	}
	return cat
}

func getAgent(t *testing.T, cat *artifacts.Catalog, id string) artifacts.Artifact {
	t.Helper()
	a, ok := cat.Get(id)
	if !ok {
		t.Fatalf("agent %s missing from the corpus", id)
	}
	return a
}

func checkAgentGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "agent", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run go test ./internal/harness -update)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("golden %s mismatch:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// Every canonical agent projects into all three target documents, with the
// native path, delivery class, and digest each projection owes its caller.
func TestAgent_AllCanonicalAgentsProject(t *testing.T) {
	cat := loadCorpus(t)

	agents := cat.OfKind(artifacts.KindAgent)
	if len(agents) != 8 {
		t.Fatalf("canonical agent count = %d, want 8", len(agents))
	}

	for _, a := range agents {
		t.Run(a.Semantics.Name, func(t *testing.T) {
			claude, err := ClaudeAgent(cat, a)
			if err != nil {
				t.Fatalf("ClaudeAgent: %v", err)
			}
			if claude.Target != artifacts.TargetClaude || claude.Path != "agents/"+filepath.Base(a.Source) {
				t.Errorf("Claude projection = %s %s, want agents/%s", claude.Target, claude.Path, filepath.Base(a.Source))
			}
			if claude.Delivery != artifacts.DeliveryDirect {
				t.Errorf("Claude delivery = %q, want %q", claude.Delivery, artifacts.DeliveryDirect)
			}
			if string(claude.Bytes) != string(a.Body) {
				t.Error("Claude projection is not the canonical bytes")
			}
			if claude.Enforcement != artifacts.EnforcementUnsupported {
				t.Errorf("Claude enforcement = %q, want %q", claude.Enforcement, artifacts.EnforcementUnsupported)
			}
			if len(claude.Unsupported) != 0 {
				t.Errorf("Claude projection reports unsupported fields %v", claude.Unsupported)
			}

			omp, err := OMPAgent(cat, a)
			if err != nil {
				t.Fatalf("OMPAgent: %v", err)
			}
			if omp.Target != artifacts.TargetOMP || omp.Path != "agents/"+filepath.Base(a.Source) {
				t.Errorf("OMP projection = %s %s", omp.Target, omp.Path)
			}
			body, err := artifacts.AgentBody(a)
			if err != nil {
				t.Fatalf("AgentBody: %v", err)
			}
			if !strings.HasSuffix(string(omp.Bytes), string(body)) {
				t.Error("OMP projection did not preserve the canonical instruction body")
			}
			ompMeta, _, err := frontmatter.Parse(string(omp.Bytes))
			if err != nil {
				t.Fatalf("OMP projection is not parseable frontmatter: %v", err)
			}
			if ompMeta["name"] != a.Semantics.Name {
				t.Errorf("OMP projection name = %v, want %q", ompMeta["name"], a.Semantics.Name)
			}
			if _, ok := ompMeta["skills"]; ok {
				t.Error("OMP projection carries an unproven skills field")
			} else if len(a.Semantics.Requires) > 0 && !slices.Contains(omp.Unsupported, "skills") {
				t.Errorf("OMP projection dropped skills without reporting it: %v", omp.Unsupported)
			}

			claudeMeta, _, err := frontmatter.Parse(string(claude.Bytes))
			if err != nil {
				t.Fatalf("Claude projection is not parseable frontmatter: %v", err)
			}
			if claudeMeta["name"] != a.Semantics.Name {
				t.Errorf("Claude projection name = %v, want %q", claudeMeta["name"], a.Semantics.Name)
			}

			codex, err := CodexAgent(cat, a)
			if err != nil {
				t.Fatalf("CodexAgent: %v", err)
			}
			if codex.Target != artifacts.TargetCodex || codex.Path != "agents/"+a.Semantics.Name+".toml" {
				t.Errorf("Codex projection = %s %s", codex.Target, codex.Path)
			}

			for _, p := range []artifacts.Projection{claude, omp, codex} {
				if p.Digest != artifacts.ProjectionDigest(p.Bytes) {
					t.Errorf("%s %s digest = %q, want the digest of its bytes", p.Target, p.Path, p.Digest)
				}
				if p.Artifact != a.ID {
					t.Errorf("%s projection names artifact %q, want %q", p.Target, p.Artifact, a.ID)
				}
			}
		})
	}
}

// The Codex TOML boundary: developer_instructions decodes back to the canonical
// instruction body byte-for-byte, and re-encoding the decoded document is
// stable.
func TestAgent_CodexDeveloperInstructionsRoundTrip(t *testing.T) {
	cat := loadCorpus(t)

	for _, a := range cat.OfKind(artifacts.KindAgent) {
		t.Run(a.Semantics.Name, func(t *testing.T) {
			proj, err := CodexAgent(cat, a)
			if err != nil {
				t.Fatalf("CodexAgent: %v", err)
			}
			doc, err := DecodeCodexAgent(proj.Bytes)
			if err != nil {
				t.Fatalf("DecodeCodexAgent: %v", err)
			}
			body, err := artifacts.AgentBody(a)
			if err != nil {
				t.Fatalf("AgentBody: %v", err)
			}
			if doc.DeveloperInstructions != string(body) {
				t.Errorf("developer_instructions did not decode to the canonical body:\n--- got ---\n%s\n--- want ---\n%s", doc.DeveloperInstructions, body)
			}
			if doc.Name != a.Semantics.Name || doc.Description != a.Semantics.Description {
				t.Errorf("decoded metadata = %q/%q, want %q/%q", doc.Name, doc.Description, a.Semantics.Name, a.Semantics.Description)
			}

			decoded, err := CodexAgent(cat, a)
			if err != nil {
				t.Fatalf("CodexAgent (second): %v", err)
			}
			if string(decoded.Bytes) != string(proj.Bytes) {
				t.Error("Codex projection is not stable across runs")
			}
		})
	}
}

// OMP has no agent-metadata row beyond name and description, so a declared
// skills dependency degrades to unsupported instead of shipping as an implied
// native claim. Claude, which consumes the same frontmatter natively, keeps it.
func TestAgent_OMPDegradesUnprovenSkillMetadata(t *testing.T) {
	cat := loadCorpus(t)
	a := getAgent(t, cat, representativeAgent)

	omp, err := OMPAgent(cat, a)
	if err != nil {
		t.Fatalf("OMPAgent: %v", err)
	}
	ompMeta, ompBody, err := frontmatter.Parse(string(omp.Bytes))
	if err != nil {
		t.Fatalf("parse OMP projection: %v", err)
	}
	if ompMeta["name"] != "atomic-reviewer" {
		t.Errorf("OMP projection name = %v, want atomic-reviewer", ompMeta["name"])
	}
	if _, ok := ompMeta["skills"]; ok {
		t.Errorf("OMP projection carries an unproven skills field: %v", ompMeta)
	}
	if len(omp.Unsupported) != 1 || omp.Unsupported[0] != "skills" {
		t.Errorf("OMP unsupported fields = %v, want [skills]", omp.Unsupported)
	}
	canonicalBody, err := artifacts.AgentBody(a)
	if err != nil {
		t.Fatalf("AgentBody: %v", err)
	}
	if strings.Compare(ompBody, string(canonicalBody)) != 0 {
		t.Error("OMP projection did not preserve the canonical instruction body byte-for-byte")
	}

	claude, err := ClaudeAgent(cat, a)
	if err != nil {
		t.Fatalf("ClaudeAgent: %v", err)
	}
	claudeMeta, _, err := frontmatter.Parse(string(claude.Bytes))
	if err != nil {
		t.Fatalf("parse Claude projection: %v", err)
	}
	if _, ok := claudeMeta["skills"]; !ok {
		t.Error("Claude projection dropped the native skills frontmatter")
	}
}

// A dependency that names an artifact the corpus does not carry fails the
// projection and the error names the missing identity.
func TestAgent_UnknownRequiresFails(t *testing.T) {
	cat := loadCorpus(t)
	a := getAgent(t, cat, representativeAgent)
	missing := "skill:skills/atomic-missing/SKILL.md"

	broken := a
	broken.Semantics.Requires = append(append([]string{}, a.Semantics.Requires...), missing)

	_, err := ClaudeAgent(cat, broken)
	if err == nil {
		t.Fatal("projecting an agent with an unknown requirement succeeded")
	}
	for _, want := range []string{a.ID, missing} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}

	for name, project := range map[string]func(*artifacts.Catalog, artifacts.Artifact) (artifacts.Projection, error){
		"omp":   OMPAgent,
		"codex": CodexAgent,
	} {
		if _, err := project(cat, broken); err == nil {
			t.Errorf("%s projection accepted an unknown requirement", name)
		}
	}
}

// User model policy owns model, effort, and tool restrictions, so a canonical
// agent that declares one is refused rather than projected.
func TestAgent_RefusesUserPolicyMetadata(t *testing.T) {
	cat := loadCorpus(t)
	a := getAgent(t, cat, representativeAgent)

	for _, key := range userPolicyKeys {
		policy := a
		policy.Body = []byte("---\nname: atomic-reviewer\ndescription: fixture\n" + key + ": value\n---\nBody.\n")

		for name, project := range map[string]func(*artifacts.Catalog, artifacts.Artifact) (artifacts.Projection, error){
			"claude": ClaudeAgent,
			"omp":    OMPAgent,
			"codex":  CodexAgent,
		} {
			_, err := project(cat, policy)
			if err == nil {
				t.Errorf("%s projection accepted a canonical %s declaration", name, key)
				continue
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("%s error %q does not name %q", name, err, key)
			}
		}
	}
}

// The same corpus renders the same bytes and digests every run.
func TestAgent_Deterministic(t *testing.T) {
	first := loadCorpus(t)
	second := loadCorpus(t)

	for _, a := range first.OfKind(artifacts.KindAgent) {
		other, ok := second.Get(a.ID)
		if !ok {
			t.Fatalf("second corpus is missing %s", a.ID)
		}
		for name, project := range map[string]func(*artifacts.Catalog, artifacts.Artifact) (artifacts.Projection, error){
			"claude": ClaudeAgent,
			"omp":    OMPAgent,
			"codex":  CodexAgent,
		} {
			one, err := project(first, a)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			two, err := project(second, other)
			if err != nil {
				t.Fatalf("%s (second): %v", name, err)
			}
			if one.Digest != two.Digest || string(one.Bytes) != string(two.Bytes) {
				t.Errorf("%s projection of %s is not stable across runs", name, a.ID)
			}
		}
	}
}

func TestAgent_GoldenProjections(t *testing.T) {
	cat := loadCorpus(t)
	a := getAgent(t, cat, representativeAgent)

	claude, err := ClaudeAgent(cat, a)
	if err != nil {
		t.Fatalf("ClaudeAgent: %v", err)
	}
	checkAgentGolden(t, "claude/agents/atomic-reviewer.md", claude.Bytes)

	omp, err := OMPAgent(cat, a)
	if err != nil {
		t.Fatalf("OMPAgent: %v", err)
	}
	checkAgentGolden(t, "omp/agents/atomic-reviewer.md", omp.Bytes)

	codex, err := CodexAgent(cat, a)
	if err != nil {
		t.Fatalf("CodexAgent: %v", err)
	}
	checkAgentGolden(t, "codex/agents/atomic-reviewer.toml", codex.Bytes)

	doc, err := DecodeCodexAgent(codex.Bytes)
	if err != nil {
		t.Fatalf("DecodeCodexAgent: %v", err)
	}
	checkAgentGolden(t, "codex/agents/atomic-reviewer.body.md", []byte(doc.DeveloperInstructions))

	// The committed goldens are the projections: a digest taken from the golden
	// bytes must equal the digest the projection reported.
	for name, projection := range map[string]artifacts.Projection{
		"claude/agents/atomic-reviewer.md":  claude,
		"omp/agents/atomic-reviewer.md":     omp,
		"codex/agents/atomic-reviewer.toml": codex,
	} {
		golden, err := os.ReadFile(filepath.Join("testdata", "agent", name))
		if err != nil {
			t.Fatalf("read golden %s: %v", name, err)
		}
		if got := artifacts.ProjectionDigest(golden); got != projection.Digest {
			t.Errorf("golden %s digest = %q, projection digest = %q", name, got, projection.Digest)
		}
	}
}
