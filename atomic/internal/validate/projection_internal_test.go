package validate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

func auditArtifact(id, source string) artifacts.Artifact {
	return artifacts.Artifact{ID: id, Kind: artifacts.KindAgent, Source: source}
}

func newProjGate(items ...artifacts.Artifact) *projGate {
	g := &projGate{cat: &artifacts.Catalog{Artifacts: items}, ids: map[string]bool{}}
	for _, a := range items {
		g.ids[a.ID] = true
	}
	return g
}

func gateFindingsWithRule(g *projGate, rule string) bool {
	for _, f := range g.findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func exactRender(p artifacts.Projection) func(artifacts.Artifact) (artifacts.Projection, error) {
	return func(artifacts.Artifact) (artifacts.Projection, error) { return p, nil }
}

// P0: an adapter that cannot render at all names the artifact and reason.
func TestProjGate_RenderFailureIsP0(t *testing.T) {
	a := auditArtifact("agent:agents/x.md", "agents/x.md")
	g := newProjGate(a)
	g.audit(a, artifacts.TargetClaude, func(artifacts.Artifact) (artifacts.Projection, error) {
		return artifacts.Projection{}, errors.New("boom")
	}, nil)
	if !gateFindingsWithRule(g, ruleRenderFailure) {
		t.Fatalf("expected P0 finding, got %+v", g.findings)
	}
}

// P2: a projection that loses its canonical identity fails.
func TestProjGate_IdentityDriftIsP2(t *testing.T) {
	a := auditArtifact("agent:agents/x.md", "agents/x.md")
	g := newProjGate(a)
	body := []byte("body\n")
	g.audit(a, artifacts.TargetClaude, exactRender(artifacts.Projection{
		Artifact: "agent:agents/other.md",
		Bytes:    body,
		Digest:   artifacts.ProjectionDigest(body),
	}), nil)
	if !gateFindingsWithRule(g, ruleIdentity) {
		t.Fatalf("expected P2 finding, got %+v", g.findings)
	}
}

// P4: a digest that does not describe the projected bytes fails.
func TestProjGate_DigestMismatchIsP4(t *testing.T) {
	a := auditArtifact("agent:agents/x.md", "agents/x.md")
	g := newProjGate(a)
	g.audit(a, artifacts.TargetClaude, exactRender(artifacts.Projection{
		Artifact: a.ID,
		Bytes:    []byte("body\n"),
		Digest:   "deadbeef",
	}), nil)
	if !gateFindingsWithRule(g, ruleDeterminism) {
		t.Fatalf("expected P4 finding, got %+v", g.findings)
	}
}

// P4: two renders of one artifact that differ fail.
func TestProjGate_NonDeterministicRenderIsP4(t *testing.T) {
	a := auditArtifact("agent:agents/x.md", "agents/x.md")
	g := newProjGate(a)
	calls := 0
	g.audit(a, artifacts.TargetClaude, func(artifacts.Artifact) (artifacts.Projection, error) {
		calls++
		body := []byte("first\n")
		if calls > 1 {
			body = []byte("second\n")
		}
		return artifacts.Projection{Artifact: a.ID, Bytes: body, Digest: artifacts.ProjectionDigest(body)}, nil
	}, nil)
	if !gateFindingsWithRule(g, ruleDeterminism) {
		t.Fatalf("expected P4 finding, got %+v", g.findings)
	}
}

// P5: a tier stronger than the CP0 record proves is an overclaim.
func TestProjGate_TierOverclaimIsP5(t *testing.T) {
	a := auditArtifact("agent:agents/x.md", "agents/x.md")
	g := newProjGate(a)
	body := []byte("body\n")
	g.audit(a, artifacts.TargetClaude, exactRender(artifacts.Projection{
		Artifact:    a.ID,
		Bytes:       body,
		Digest:      artifacts.ProjectionDigest(body),
		Enforcement: artifacts.EnforcementNativeScope,
	}), nil)
	if !gateFindingsWithRule(g, ruleTier) {
		t.Fatalf("expected P5 finding, got %+v", g.findings)
	}
}

// P5: hook-required stands on two CP0 roles, so a target proving only one of
// them — OMP proves pre-operation targets but not context return — cannot claim
// it. Proving both passes; a native-scope overclaim (Claude proves no
// static-scope row) still fails.
func TestProjGate_TierRequiresEveryBackingRole(t *testing.T) {
	a := auditArtifact("rule:rules/atomic/x.md", "rules/atomic/x.md")
	body := []byte("body\n")
	hookRequired := artifacts.Projection{
		Artifact:    a.ID,
		Bytes:       body,
		Digest:      artifacts.ProjectionDigest(body),
		Enforcement: artifacts.EnforcementHookRequired,
	}

	overclaim := newProjGate(a)
	overclaim.audit(a, artifacts.TargetOMP, exactRender(hookRequired), nil)
	if !gateFindingsWithRule(overclaim, ruleTier) {
		t.Fatalf("OMP proves pre-operation but not context return; expected P5, got %+v", overclaim.findings)
	}

	// OMP's shipped rows plus its never-exercised context-return row: with
	// both backing roles proven the claim holds.
	omp := harness.OMPCapabilities()
	proven := newProjGate(a)
	proven.supports = func(_ artifacts.Target, role harness.Role) bool {
		return omp.Supports(role) || role == harness.RoleContextReturn
	}
	proven.audit(a, artifacts.TargetOMP, exactRender(hookRequired), nil)
	if gateFindingsWithRule(proven, ruleTier) {
		t.Fatalf("hook-required with every backing role proven should pass P5: %+v", proven.findings)
	}

	// The default predicate still reads the shipped matrix: Claude's
	// static-scope row is unproven, so native scope fails.
	native := newProjGate(a)
	native.audit(a, artifacts.TargetClaude, exactRender(artifacts.Projection{
		Artifact:    a.ID,
		Bytes:       body,
		Digest:      artifacts.ProjectionDigest(body),
		Enforcement: artifacts.EnforcementNativeScope,
	}), nil)
	if !gateFindingsWithRule(native, ruleTier) {
		t.Fatalf("Claude proves no static-scope row; expected P5, got %+v", native.findings)
	}
}

// P3: a projected native document that carries a user-policy key fails.
func TestProjGate_MetadataLeakIsP3(t *testing.T) {
	a := auditArtifact("agent:agents/x.md", "agents/x.md")
	g := newProjGate(a)
	body := []byte("---\nname: atomic-x\nmodel: opus\n---\nbody\n")
	g.audit(a, artifacts.TargetOMP, exactRender(artifacts.Projection{
		Artifact: a.ID,
		Bytes:    body,
		Digest:   artifacts.ProjectionDigest(body),
	}), nil)
	if !gateFindingsWithRule(g, ruleMetadata) {
		t.Fatalf("expected P3 finding, got %+v", g.findings)
	}
}

// P6: an unclassified wire token in another target's bytes fails, while one the
// adapter already reports does not.
func TestProjGate_WireTokenClassification(t *testing.T) {
	a := auditArtifact("agent:agents/x.md", "agents/x.md")
	body := []byte("Then run `Bash` directly.\n")

	unreported := newProjGate(a)
	unreported.audit(a, artifacts.TargetOMP, exactRender(artifacts.Projection{
		Artifact: a.ID,
		Bytes:    body,
		Digest:   artifacts.ProjectionDigest(body),
	}), nil)
	if !gateFindingsWithRule(unreported, ruleWireToken) {
		t.Fatalf("expected P6 finding, got %+v", unreported.findings)
	}

	reported := newProjGate(a)
	reported.audit(a, artifacts.TargetOMP, exactRender(artifacts.Projection{
		Artifact: a.ID,
		Bytes:    body,
		Digest:   artifacts.ProjectionDigest(body),
	}), map[string]bool{"harness tool name": true})
	if gateFindingsWithRule(reported, ruleWireToken) {
		t.Fatalf("reported wire token should not be a leak: %+v", reported.findings)
	}
}

// P1/P2: an unresolved dependency fails, and a source digest that no longer
// matches the authored bytes fails.
func TestProjGate_CorpusIdentityAndDependencies(t *testing.T) {
	root := t.TempDir()
	srcPath := filepath.Join(root, "context", "agents", "atomic-x.md")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data := []byte("---\nname: atomic-x\ndescription: x\n---\nbody\n")
	if err := os.WriteFile(srcPath, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	dep := artifacts.Artifact{
		ID: "agent:agents/atomic-x.md", Kind: artifacts.KindAgent, Source: "agents/atomic-x.md",
		SourceDigest: managedfile.Digest(data),
		Semantics:    artifacts.Semantics{Requires: []string{"skill:skills/nope/SKILL.md"}},
	}
	g := newProjGate(dep)
	g.root = root
	g.checkCorpus()
	if !gateFindingsWithRule(g, ruleDependency) {
		t.Fatalf("expected P1 finding, got %+v", g.findings)
	}

	drift := dep
	drift.SourceDigest = "stale"
	g2 := newProjGate(drift)
	g2.root = root
	g2.checkCorpus()
	if !gateFindingsWithRule(g2, ruleIdentity) {
		t.Fatalf("expected P2 finding, got %+v", g2.findings)
	}
}
