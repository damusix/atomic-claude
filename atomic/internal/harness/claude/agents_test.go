package claude

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

const overriddenAgent = "agents/atomic-implementer.md"

func storeClaudeOverride(t *testing.T, home string, ov config.AgentOverride) {
	t.Helper()
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Claude.Agents = map[string]config.AgentOverride{"atomic-implementer": ov}
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}
}

func embeddedBytes(t *testing.T, target string) []byte {
	t.Helper()
	for _, a := range embedded.Manifest() {
		if a.Target == target {
			data, err := fs.ReadFile(embedded.FS, a.Source)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
	}
	t.Fatalf("no embedded artifact %s", target)
	return nil
}

func artifactData(t *testing.T, artifacts []installstate.Artifact, id string) []byte {
	t.Helper()
	for _, a := range artifacts {
		if a.ID == id {
			return a.Data
		}
	}
	t.Fatalf("no artifact %s", id)
	return nil
}

func agentMeta(t *testing.T, data []byte) map[string]any {
	t.Helper()
	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

// The home's stored [claude.agents] override reaches exactly its agent; every
// other artifact keeps the embedded bytes.
func TestGlobalArtifactsOverlayStoredClaudeOverrides(t *testing.T) {
	home := t.TempDir()
	storeClaudeOverride(t, home, config.AgentOverride{Model: "opus", Effort: "high"})
	artifacts, err := GlobalArtifacts(home, filepath.Join(home, ".claude"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range artifacts {
		if a.ID == steeringTarget {
			continue
		}
		want := embeddedBytes(t, a.ID)
		if a.ID == overriddenAgent {
			meta := agentMeta(t, a.Data)
			if meta["model"] != "opus" || meta["effort"] != "high" {
				t.Errorf("overridden agent frontmatter = %v", meta)
			}
			continue
		}
		if string(a.Data) != string(want) {
			t.Errorf("%s changed without an override", a.ID)
		}
	}
}

// The claim digests the overlaid bytes, so ledger drift and doctor agree
// with what convergence writes.
func TestSelectionClaimsDigestTheOverlaidBytes(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	storeClaudeOverride(t, home, config.AgentOverride{Effort: "max"})
	artifacts, err := GlobalArtifacts(home, root)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := SelectionClaims(home, root)
	if err != nil {
		t.Fatal(err)
	}
	want := managedfile.Digest(artifactData(t, artifacts, overriddenAgent))
	for _, c := range claims {
		if c.ID == overriddenAgent && c.SelectedDigest != want {
			t.Errorf("claim digest = %s, want the overlaid bytes' %s", c.SelectedDigest, want)
		}
	}
}

// A config.toml that cannot be read must not converge as though no override
// were stored.
func TestGlobalArtifactsRefuseAnUnparseableConfig(t *testing.T) {
	cases := []struct {
		name, body string
		agentsHint bool
	}{
		{"scalar agent", "[claude.agents]\natomic-implementer = \"opus\"\n", true},
		{"syntax error elsewhere", "[update\ncheck = true\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			writeFile(t, config.TOMLPath(home), tc.body)
			_, err := GlobalArtifacts(home, filepath.Join(home, ".claude"))
			if err == nil {
				t.Fatal("GlobalArtifacts ignored an undecodable config")
			}
			if got := strings.Contains(err.Error(), "must be a table with model and effort"); got != tc.agentsHint {
				t.Errorf("error %q: agents hint = %v, want %v", err, got, tc.agentsHint)
			}
		})
	}
}

// The adapter reads only [claude.agents]; an invalid value elsewhere is
// doctor's to report and must not stop the agents projecting.
func TestGlobalArtifactsIgnoreAnInvalidUnrelatedValue(t *testing.T) {
	home := t.TempDir()
	writeFile(t, config.TOMLPath(home), "[update]\nchannel = \"nightly\"\n")
	if _, err := GlobalArtifacts(home, filepath.Join(home, ".claude")); err != nil {
		t.Errorf("an invalid update.channel blocked the projection: %v", err)
	}
}

// Claude rejects an effort outside its enum, so an invalid one is never
// written into an agent file.
func TestGlobalArtifactsRefuseAnInvalidClaudeEffort(t *testing.T) {
	home := t.TempDir()
	storeClaudeOverride(t, home, config.AgentOverride{Effort: "ultra"})
	if _, err := GlobalArtifacts(home, filepath.Join(home, ".claude")); err == nil {
		t.Error("an invalid Claude effort was overlaid")
	}
}

// A stored override never licenses overwriting a hand edit: the edited agent
// still blocks convergence and keeps its bytes.
func TestConvergeKeepsAHandEditedAgentUnderAnOverride(t *testing.T) {
	home := newHome(t)
	t.Setenv(ConfigDirEnv, "")
	root := filepath.Join(home, ".claude")
	writeFile(t, filepath.Join(root, "settings.json"), "{}\n")
	target := convergeDefaultClaudeTarget(t, home, root)
	agentPath := filepath.Join(root, filepath.FromSlash(overriddenAgent))
	writeFile(t, agentPath, "hand edit\n")

	storeClaudeOverride(t, home, config.AgentOverride{Model: "haiku"})
	life := New().Lifecycle(home)
	plan, err := life.Project(target, harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) == 0 {
		t.Fatal("a hand-edited agent planned without a blocker")
	}
	if _, err := life.Converge(target, plan); err == nil {
		t.Error("converge overwrote past a hand-edit blocker")
	}
	if got, _ := os.ReadFile(agentPath); string(got) != "hand edit\n" {
		t.Errorf("hand edit lost:\n%s", got)
	}
}

// An agent ReapplyAgents patched in place with the stored override stays owned
// though the ledger still holds the embedded digest.
func TestEnrolledAgentPatchedInPlaceWithTheSameOverrideIsOwned(t *testing.T) {
	home := newHome(t)
	t.Setenv(ConfigDirEnv, "")
	root := filepath.Join(home, ".claude")
	writeFile(t, filepath.Join(root, "settings.json"), "{}\n")
	target := convergeDefaultClaudeTarget(t, home, root)

	storeClaudeOverride(t, home, config.AgentOverride{Model: "opus", Effort: "high"})
	changed, _, err := claudeinstall.ReapplyAgents(root, home)
	if err != nil || len(changed) != 1 {
		t.Fatalf("ReapplyAgents changed %v, err %v", changed, err)
	}
	for _, row := range ledgerRows(t, home) {
		if row.Resource == overriddenAgent && row.Applied.Digest != managedfile.Digest(embeddedBytes(t, overriddenAgent)) {
			t.Fatal("fixture: the ledger should still hold the embedded digest")
		}
	}

	assessments, err := New().Lifecycle(home).Verify(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range assessments {
		if a.Claim.ID == overriddenAgent && (!a.Owned || a.Evidence != managedfile.EvidenceSelection) {
			t.Errorf("patched agent assessed %s owned=%v, want selection-owned", a.Evidence, a.Owned)
		}
	}
}

// A legacy install patched with the stored override holds the overlaid bytes,
// so adoption owns the agent instead of asking for an older-version decision.
func TestMigrateAdoptsAnAgentPatchedWithTheSameOverride(t *testing.T) {
	home := t.TempDir()
	storeClaudeOverride(t, home, config.AgentOverride{Model: "sonnet", Effort: "low"})
	root := installLegacy(t, home)

	req := migrationRequest(home, root)
	req.AcknowledgeSnapshot = true
	plan, err := PlanMigration(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Batch) > 0 || len(plan.Blockers) > 0 {
		t.Fatalf("batch %v blockers %v, want none", plan.Batch, plan.Blockers)
	}
	if _, err := Migrate(req); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(overriddenAgent)))
	if err != nil {
		t.Fatal(err)
	}
	if meta := agentMeta(t, onDisk); meta["model"] != "sonnet" || meta["effort"] != "low" {
		t.Errorf("adopted agent frontmatter = %v", meta)
	}
	for _, row := range ledgerRows(t, home) {
		if row.Resource == overriddenAgent && row.Applied.Digest != managedfile.Digest(onDisk) {
			t.Errorf("row digest %s does not match the adopted bytes", row.Applied.Digest)
		}
	}
}

// A changed override on an enrolled target converges, and the claim and ledger
// digest the written bytes, so the next assessment reads owned, not drift.
func TestConvergeWritesAChangedOverrideAndClaimsItsDigest(t *testing.T) {
	home := newHome(t)
	t.Setenv(ConfigDirEnv, "")
	root := filepath.Join(home, ".claude")
	writeFile(t, filepath.Join(root, "settings.json"), "{}\n")
	target := convergeDefaultClaudeTarget(t, home, root)
	agentPath := filepath.Join(root, filepath.FromSlash(overriddenAgent))
	if got, _ := os.ReadFile(agentPath); string(got) != string(embeddedBytes(t, overriddenAgent)) {
		t.Fatal("first convergence did not write the embedded agent")
	}

	storeClaudeOverride(t, home, config.AgentOverride{Model: "haiku"})
	life := New().Lifecycle(home)
	plan, err := life.Project(target, harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Converged {
		t.Fatal("a changed override planned as converged")
	}
	if _, err := life.Converge(target, plan); err != nil {
		t.Fatalf("converge: %v", err)
	}

	onDisk, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	if agentMeta(t, onDisk)["model"] != "haiku" {
		t.Fatalf("converged agent lacks the override:\n%s", onDisk)
	}
	for _, c := range plan.Claims {
		if c.ID == overriddenAgent && c.SelectedDigest != managedfile.Digest(onDisk) {
			t.Errorf("claim digest %s does not match the written bytes", c.SelectedDigest)
		}
	}
	for _, row := range ledgerRows(t, home) {
		if row.Resource == overriddenAgent && row.Applied.Digest != managedfile.Digest(onDisk) {
			t.Errorf("ledger digest %s does not match the written bytes", row.Applied.Digest)
		}
	}
	assessments, err := life.Verify(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range assessments {
		if a.Claim.ID == overriddenAgent && !a.Owned {
			t.Errorf("overlaid agent assessed %+v, want owned", a)
		}
	}
}
