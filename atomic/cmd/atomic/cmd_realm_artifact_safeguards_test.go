package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
)

func TestRealmCLIArtifactUnknownClaudeExecutableDoesNotActivate(t *testing.T) {
	bin, _ := realmInstructionTestPATH(t)
	git, err := os.Readlink(filepath.Join(bin, "git"))
	if err != nil {
		t.Fatal(err)
	}
	linkRealmInstructionTool(t, bin, "claude", git)
	f := newRealmArtifactCLIFixture(t)
	writeSteeringTestFile(t, filepath.Join(f.publisher, cliArtifactCommand), cliArtifactCommandText)
	publishRealmArtifactFixture(t, f, cliArtifactCommand)
	protected := seedRealmArtifactClaude(t, f)

	const native = ".claude/commands/realm-triage.md"
	preview := mustRunRealmCLI(t, f.joinerHome, "link", cliArtifactCommand, "--dry-run")
	assertRealmArtifactNativeStatus(t, preview, native, "unavailable", "unsupported", "unverified")
	mustRunRealmCLI(t, f.joinerHome, "link", cliArtifactCommand, "--yes", "--review", realmReviewDigest(t, preview))

	assertRealmCLIPathAbsent(t, f.joiner, native)
	assertRealmArtifactSourceStates(t, f.joinerHome, "linked", cliArtifactCommand)
	item, claude := realmArtifactClaudeStatus(t, f.joinerHome, cliArtifactCommand)
	if item.SourceAvailability != "available" || claude.Path != native ||
		(claude.State != "unavailable" && claude.State != "unsupported" && claude.State != "unverified") {
		t.Errorf("expected available source but no verified Claude activation: %+v, Claude: %+v", item, claude)
	}
	protected(t)
}

func TestRealmCLIArtifactClaudeAgentFrontmatterNameCollision(t *testing.T) {
	useRealmArtifactClaude(t)
	f := newRealmArtifactCLIFixture(t)
	writeSteeringTestFile(t, filepath.Join(f.publisher, cliArtifactAgent), cliArtifactAgentText)
	publishRealmArtifactFixture(t, f, cliArtifactAgent)
	protected := seedRealmArtifactClaude(t, f)

	const native = ".claude/agents/realm-reviewer.md"
	const personal = ".claude/agents/personal-review.md"
	personalPath := filepath.Join(f.joiner, personal)
	writeSteeringTestFile(t, personalPath, "---\nname: realm-reviewer\ndescription: Personal reviewer.\n---\n\nPERSONAL_AGENT_BODY_MARKER\n")
	before := realmArtifactLstat(t, personalPath)
	preserve := unchangedRealmCLITrees(t, personalPath)

	assertCollision := func(text string, code int) {
		t.Helper()
		if !strings.Contains(text, "realm-reviewer") || !realmArtifactMentions(text, "collision", "occupied", "already claims", "already exists") {
			t.Errorf("review did not identify the agent name collision: exit %d\n%s", code, text)
		}
	}
	preview, diag, code := runRealmCLI(t, f.joinerHome, "link", cliArtifactAgent, "--dry-run")
	assertCollision(preview+diag, code)
	if code == 0 {
		out, diag, code := runRealmCLI(t, f.joinerHome, "link", cliArtifactAgent, "--yes", "--review", realmReviewDigest(t, preview))
		if code == 0 {
			assertRealmArtifactSourceStates(t, f.joinerHome, "linked", cliArtifactAgent)
			_, claude := realmArtifactClaudeStatus(t, f.joinerHome, cliArtifactAgent)
			if claude.Path != native || claude.State != "collision" {
				t.Errorf("expected Claude agent name collision in status: %+v", claude)
			}
		} else {
			assertCollision(out+diag, code)
		}
	}
	assertRealmCLIPathAbsent(t, f.joiner, native)
	preserve(t)
	mustRunRealmCLI(t, f.joinerHome, "unlink")
	preserve(t)
	if !os.SameFile(before, realmArtifactLstat(t, personalPath)) {
		t.Error("link or unlink replaced the personal agent")
	}
	protected(t)
}

func TestRealmCLIArtifactMissingSkillDocumentRetainsReceipt(t *testing.T) {
	useRealmArtifactClaude(t)
	f := newRealmArtifactCLIFixture(t)
	writeSteeringTestFile(t, filepath.Join(f.publisher, cliArtifactDoc), cliArtifactSkillText)
	publishRealmArtifactFixture(t, f, cliArtifactSkill+"/")
	protected := seedRealmArtifactClaude(t, f)

	const native = ".claude/skills/realm-review"
	review := previewRealmReview(t, f.joinerHome, "link", cliArtifactSkill+"/")
	mustRunRealmCLI(t, f.joinerHome, "link", cliArtifactSkill+"/", "--yes", "--review", review)
	_, claude := realmArtifactClaudeStatus(t, f.joinerHome, cliArtifactSkill)
	if claude.Path != native || claude.State != "active" {
		t.Fatalf("expected an active Claude skill before source removal: %+v", claude)
	}
	receipt := unchangedRealmLinkWitness(t, f, cliArtifactSkill)
	clone := realmCLIManagedClone(t, f.joinerHome)
	nativeLink := filepath.Join(f.joiner, native)
	before := realmArtifactLstat(t, nativeLink)
	if err := os.Remove(filepath.Join(clone, cliArtifactDoc)); err != nil {
		t.Fatal(err)
	}

	unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)
	item, claude := realmArtifactClaudeStatus(t, f.joinerHome, cliArtifactSkill)
	if item.SourceAvailability != "unavailable" && !item.Drift {
		t.Errorf("missing SKILL.md did not report source unavailability or drift: %+v", item)
	}
	if claude.Path != native || (claude.State != "unavailable" && claude.State != "drifted") {
		t.Errorf("missing SKILL.md did not invalidate Claude activation: %+v", claude)
	}
	unchanged(t)
	receipt(t)
	if !os.SameFile(before, realmArtifactLstat(t, nativeLink)) {
		t.Error("status replaced the original native link")
	}
	mustRunRealmCLI(t, f.joinerHome, "unlink")
	assertRealmCLIPathAbsent(t, f.joiner, native)
	assertRealmCLIPathAbsent(t, f.joiner, cliArtifactSkill)
	protected(t)
}

func realmArtifactClaudeStatus(t *testing.T, home, path string) (realmshare.StatusPath, realmshare.ArtifactActivation) {
	t.Helper()
	raw := mustRunRealmCLI(t, home, "status", "--json")
	var status realmshare.StatusReport
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, raw)
	}
	for _, item := range status.Paths {
		if item.Path == path && item.Source == path {
			for _, activation := range item.Activations {
				if activation.Harness == "claude" {
					return item, activation
				}
			}
		}
	}
	t.Fatalf("status did not report Claude activation for %q:\n%s", path, raw)
	return realmshare.StatusPath{}, realmshare.ArtifactActivation{}
}
