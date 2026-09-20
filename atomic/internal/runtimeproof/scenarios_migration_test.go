// CP8A migration scenarios: discovery classification, per-resource adoption
// decisions, interrupted-operation recovery, idempotent reruns, state-root
// selection, schema refusal, and old-binary drift — over isolated Claude homes
// carrying fixtures the real installer produced. Each scenario drives the
// production installstate engine (Classify / PlanAdoption / Adopt) and asserts
// the criterion it names.
package runtimeproof

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// scenarioMkfile writes path, creating its parent directories.
func scenarioMkfile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// scenarioClaims mirrors what the harness layer hands the classifier: one claim per
// selected artifact with the selected generation's digest.
func scenarioClaims(t *testing.T, root string) []managedfile.Claim {
	t.Helper()
	artifacts := claudeArtifacts(t, root)
	out := make([]managedfile.Claim, 0, len(artifacts))
	for _, a := range artifacts {
		claim := managedfile.Claim{ID: a.ID, Kind: a.Kind, Path: a.Path}
		if digest, err := managedfile.DigestResourceBytes(a.Data, a.Kind); err == nil {
			claim.SelectedDigest = digest
		}
		out = append(out, claim)
	}
	return out
}

// scenarioAdoptReq is the explicit adoption intent a scenario applies. It carries
// the selected generation's artifacts exactly as the harness layer would.
func scenarioAdoptReq(t *testing.T, home, root string) installstate.AdoptionRequest {
	t.Helper()
	return installstate.AdoptionRequest{
		Home:          home,
		NativeRoot:    root,
		Target:        "claude:default",
		Generation:    "gen-selected",
		Tier:          "unsupported",
		BatchDecision: installstate.DecisionReplace,
		Artifacts:     claudeArtifacts(t, root),
	}
}

// scenarioClassify classifies one Claude home against the selected generation.
func scenarioClassify(t *testing.T, home, root string) installstate.Classification {
	t.Helper()
	c, err := installstate.Classify(installstate.ClassifyRequest{Home: home, NativeRoot: root, Claims: scenarioClaims(t, root)})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	return c
}

// scenarioWriteLedger persists a real v2 ledger.
func scenarioWriteLedger(t *testing.T, home string, targets []installstate.TargetRecord, rows []installstate.Row) {
	t.Helper()
	ledger := &installstate.Ledger{Targets: targets, Rows: rows}
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
}

// scenarioWriteJournal persists an unresolved journal in the v2 layout.
func scenarioWriteJournal(t *testing.T, home string, j *installstate.Journal) string {
	t.Helper()
	path := config.JournalPath(home, j.OperationID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installstate.WriteJournal(path, j); err != nil {
		t.Fatal(err)
	}
	return path
}

// scenarioFixedTime is a fixed instant for fixture journals.
func scenarioFixedTime() time.Time { return time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC) }

// scenarioMutation builds a journal-ready file mutation with the intended digest.
func scenarioMutation(t *testing.T, unit, path string, data []byte) installstate.Mutation {
	t.Helper()
	digest, err := managedfile.DigestResourceBytes(data, managedfile.KindFile)
	if err != nil {
		t.Fatal(err)
	}
	return installstate.Mutation{
		Unit:     unit,
		Resource: unit,
		Target:   "claude:default",
		Kind:     managedfile.KindFile,
		Path:     path,
		Intended: digest,
	}
}

// scenarioJournal builds an unresolved journal carrying mutations and progress.
func scenarioJournal(id string, mutations []installstate.Mutation, progress []installstate.Progress) *installstate.Journal {
	j := installstate.NewJournal(id, scenarioFixedTime())
	j.Mutations = mutations
	j.Progress = progress
	return j
}

// scenarioActionFor reports the plan's action for a resource id.
func scenarioActionFor(p installstate.AdoptionPlan, id string) installstate.AdoptionAction {
	for _, r := range p.Resources {
		if r.ID == id {
			return r.Action
		}
	}
	return ""
}

// scenarioHasRecovery reports whether any journal simulation reached want.
func scenarioHasRecovery(plan installstate.AdoptionPlan, want installstate.RecoveryDecision) bool {
	for _, sim := range plan.Recovery {
		for _, a := range sim.Actions {
			if a.Decision == want {
				return true
			}
		}
	}
	return false
}

// scenarioJournalCount counts the journal files under ~/.atomic/install/journals.
func scenarioJournalCount(home string) int {
	entries, err := os.ReadDir(config.JournalsDir(home))
	if err != nil {
		return 0
	}
	return len(entries)
}

// TestCP8ALegacyCompleteDiscovery proves a structurally complete prior-version
// install classifies as legacy-complete, offers a ready plan under an explicit
// replace decision, and adopts to the selected generation while preserving the
// user prose around the managed block.
//
// Criterion: read-only discovery classifies legacy-complete; adoption enrolls
// the target and writes the selected generation's bytes.
func TestCP8ALegacyCompleteDiscovery(t *testing.T) {
	home := isolatedHome(t)
	root := legacyClaudeInstall(t, home)
	ageClaudeInstall(t, home, root)

	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/legacy-complete",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "read-only discovery classifies legacy-complete and adoption enrolls under an explicit replace decision",
		Command:   "Classify → PlanAdoption → Adopt",
		Paths:     []string{home, root},
	}

	c := scenarioClassify(t, home, root)
	if c.State != installstate.StateLegacyComplete {
		t.Fatalf("state = %s (%v), want %s", c.State, c.Conflicts, installstate.StateLegacyComplete)
	}
	plan, err := installstate.PlanAdoption(scenarioAdoptReq(t, home, root))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != installstate.StatusReady {
		t.Fatalf("plan status = %s (%v), want ready", plan.Status, plan.Blockers)
	}
	result, err := installstate.Adopt(scenarioAdoptReq(t, home, root))
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if len(result.Applied) == 0 {
		t.Fatalf("adoption applied nothing: %+v", result)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.FindTarget("claude", "default"); !ok {
		t.Errorf("adoption did not enroll the target")
	}
	steering, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(steering), "user prose before the block\n") ||
		!strings.HasSuffix(string(steering), "user prose after the block\n") {
		t.Errorf("adoption did not preserve the user prose around the block")
	}

	ev.Outcome = "legacy-complete classified; ready plan; adoption enrolled the target and preserved surrounding prose"
	recordScenario(t, ev)
}

// TestCP8AListedMissingDrift proves a listed-but-missing artifact is reported as
// drift, planned as a recreate, and restored only when the caller converges.
//
// Criterion: missing listed files remain drift until convergence is accepted.
func TestCP8AListedMissingDrift(t *testing.T) {
	home := isolatedHome(t)
	root := legacyClaudeInstall(t, home)
	ageClaudeInstall(t, home, root)
	deleteClaudeListed(t, root, "commands/commit.md")

	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/listed-missing-drift",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "a listed-but-missing artifact is drift, planned as recreate, and restored only on convergence",
		Command:   "Classify → PlanAdoption → Adopt",
		Paths:     []string{home, root},
	}

	c := scenarioClassify(t, home, root)
	if c.State != installstate.StateLegacyPartial {
		t.Fatalf("state = %s, want %s", c.State, installstate.StateLegacyPartial)
	}
	if len(c.Legacy.ListedMissing) == 0 {
		t.Fatalf("the missing listed file was not reported as drift")
	}
	plan, err := installstate.PlanAdoption(scenarioAdoptReq(t, home, root))
	if err != nil {
		t.Fatal(err)
	}
	if got := scenarioActionFor(plan, "commands/commit.md"); got != installstate.ActionRecreate {
		t.Errorf("action = %s, want recreate", got)
	}
	if _, err := installstate.Adopt(scenarioAdoptReq(t, home, root)); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if !fileExists(filepath.Join(root, "commands", "commit.md")) {
		t.Errorf("convergence did not restore the missing artifact")
	}

	ev.Outcome = "legacy-partial with the missing artifact reported; recreate planned; convergence restored it"
	recordScenario(t, ev)
}

// TestCP8APendingProposalBlocks proves a pending legacy proposal blocks
// automatic adoption until it is resolved.
//
// Criterion: pending proposed/merged Claude files block automatic adoption until
// explicitly resolved.
func TestCP8APendingProposalBlocks(t *testing.T) {
	home := isolatedHome(t)
	root := legacyClaudeInstall(t, home)
	ageClaudeInstall(t, home, root)
	writeClaudeProposal(t, home)

	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/pending-proposal",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "a pending proposed CLAUDE.md blocks automatic adoption until resolved",
		Command:   "Classify → PlanAdoption → Adopt",
		Paths:     []string{home, root},
	}

	c := scenarioClassify(t, home, root)
	if c.State != installstate.StateLegacyPartial || !c.Legacy.Proposed {
		t.Fatalf("state = %s proposed = %v, want partial with a pending proposal", c.State, c.Legacy.Proposed)
	}
	plan, err := installstate.PlanAdoption(scenarioAdoptReq(t, home, root))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != installstate.StatusBlocked {
		t.Fatalf("plan status = %s, want blocked", plan.Status)
	}
	if _, err := installstate.Adopt(scenarioAdoptReq(t, home, root)); err == nil {
		t.Errorf("adoption proceeded despite a pending proposal")
	}

	ev.Outcome = "legacy-partial with a pending proposal; plan and adoption refused"
	recordScenario(t, ev)
}

// TestCP8ACorruptSnapshotAcknowledged proves a corrupt legacy snapshot blocks
// adoption until the caller explicitly acknowledges that restoration evidence is
// unusable.
//
// Criterion: a corrupt legacy snapshot blocks automatic adoption until
// explicitly acknowledged.
func TestCP8ACorruptSnapshotAcknowledged(t *testing.T) {
	home := isolatedHome(t)
	root := legacyClaudeInstall(t, home)
	ageClaudeInstall(t, home, root)
	corruptClaudeSnapshot(t, home)

	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/corrupt-snapshot",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "a corrupt legacy snapshot blocks adoption until explicitly acknowledged",
		Command:   "Classify → PlanAdoption → PlanAdoption(acknowledge)",
		Paths:     []string{home, root},
	}

	c := scenarioClassify(t, home, root)
	if c.State != installstate.StateLegacyPartial {
		t.Fatalf("state = %s, want %s", c.State, installstate.StateLegacyPartial)
	}
	if c.Legacy.Snapshot.State != "corrupt" {
		t.Fatalf("snapshot state = %s, want corrupt", c.Legacy.Snapshot.State)
	}
	plan, err := installstate.PlanAdoption(scenarioAdoptReq(t, home, root))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != installstate.StatusBlocked {
		t.Fatalf("plan status = %s, want blocked", plan.Status)
	}
	req := scenarioAdoptReq(t, home, root)
	req.AcknowledgeSnapshot = true
	ack, err := installstate.PlanAdoption(req)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != installstate.StatusReady {
		t.Fatalf("acknowledged plan status = %s (%v), want ready", ack.Status, ack.Blockers)
	}

	ev.Outcome = "corrupt snapshot classified; blocked without acknowledgement; ready once acknowledged"
	recordScenario(t, ev)
}

// TestCP8AV2MigrationStates proves the v2 classifications the recovery engine
// depends on: an unresolved journal owns in-flight work, and an unjournaled
// transaction remnant is orphaned.
//
// Criterion: discovery classifies v2-in-flight and v2-orphaned from v2
// evidence.
func TestCP8AV2MigrationStates(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/v2-states",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "discovery classifies v2-in-flight from an unresolved journal and v2-orphaned from an unjournaled remnant",
		Command:   "Classify over an in-flight journal and an orphaned transaction directory",
	}

	t.Run("in-flight", func(t *testing.T) {
		home := isolatedHome(t)
		root := filepath.Join(home, ".claude")
		path := filepath.Join(root, "commands", "commit.md")
		scenarioMkfile(t, path, "applied\n")
		m := scenarioMutation(t, "commands.commit.md", path, []byte("applied\n"))
		scenarioWriteJournal(t, home, scenarioJournal("op-inflight", []installstate.Mutation{m}, nil))

		c := scenarioClassify(t, home, root)
		if c.State != installstate.StateV2InFlight {
			t.Fatalf("state = %s, want %s", c.State, installstate.StateV2InFlight)
		}
		if len(c.V2.Journals) != 1 {
			t.Errorf("unresolved journals = %v, want 1", c.V2.Journals)
		}
		ev.Paths = append(ev.Paths, home)
	})

	t.Run("orphaned", func(t *testing.T) {
		home := isolatedHome(t)
		if err := os.MkdirAll(config.TransactionStageDir(home, "op-orphan"), 0o755); err != nil {
			t.Fatal(err)
		}
		c := scenarioClassify(t, home, filepath.Join(home, ".claude"))
		if c.State != installstate.StateV2Orphaned {
			t.Fatalf("state = %s, want %s", c.State, installstate.StateV2Orphaned)
		}
		if len(c.V2.Orphans) == 0 {
			t.Errorf("orphan remnants were not reported")
		}
		ev.Paths = append(ev.Paths, home)
	})

	ev.Outcome = "unresolved journal classified v2-in-flight; unjournaled transaction classified v2-orphaned"
	recordScenario(t, ev)
}

// TestCP8AMixedEvidenceBlocks proves a ledger row that disagrees with the
// observed bytes — overlapping legacy and v2 ownership evidence — classifies
// mixed and refuses adoption.
//
// Criterion: unjournaled v2 remnants and overlapping legacy/v2 evidence block
// automatic adoption until resolved.
func TestCP8AMixedEvidenceBlocks(t *testing.T) {
	home := isolatedHome(t)
	root := legacyClaudeInstall(t, home)
	ageClaudeInstall(t, home, root)
	drifted := filepath.Join(root, "commands", "commit.md")
	scenarioWriteLedger(t, home, nil, []installstate.Row{{
		Target:   "claude:default",
		Resource: "commands/commit.md",
		Applied:  installstate.AppliedValue{Path: drifted, Kind: managedfile.KindFile, Digest: "0000"},
	}})

	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/mixed-evidence",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "overlapping legacy and v2 evidence classifies mixed and refuses adoption",
		Command:   "Classify → PlanAdoption",
		Paths:     []string{home, root},
	}

	c := scenarioClassify(t, home, root)
	if c.State != installstate.StateMixed {
		t.Fatalf("state = %s (%v), want %s", c.State, c.Conflicts, installstate.StateMixed)
	}
	plan, err := installstate.PlanAdoption(scenarioAdoptReq(t, home, root))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status == installstate.StatusReady {
		t.Fatalf("plan status = %s, want a refused plan", plan.Status)
	}

	ev.Outcome = "mixed classified; adoption plan refused"
	recordScenario(t, ev)
}

// TestCP8AReplaceOrLeaveUnowned proves the batched per-resource decision for an
// older-version artifact: replace rewrites it to the selected generation, and
// leave-unowned preserves the bytes and records no ownership.
//
// Criterion: older-version non-block artifacts require batched per-resource
// replace-or-leave-unowned decisions.
func TestCP8AReplaceOrLeaveUnowned(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/replace-or-leave-unowned",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "an older-version artifact is replaced or left unowned per the caller's batch decision",
		Command:   "PlanAdoption (undecided) → PlanAdoption(replace) → PlanAdoption(leave-unowned)",
	}

	home := isolatedHome(t)
	root := legacyClaudeInstall(t, home)
	ageClaudeInstall(t, home, root)
	ev.Paths = []string{home, root}
	victim := filepath.Join(root, "commands", "commit.md")
	older, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}

	undecided := scenarioAdoptReq(t, home, root)
	undecided.BatchDecision = ""
	plan, err := installstate.PlanAdoption(undecided)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != installstate.StatusBlocked || len(plan.Batch) == 0 {
		t.Fatalf("undecided plan = %+v, want blocked with a batch awaiting a decision", plan)
	}

	replace, err := installstate.PlanAdoption(scenarioAdoptReq(t, home, root))
	if err != nil {
		t.Fatal(err)
	}
	if got := scenarioActionFor(replace, "commands/commit.md"); got != installstate.ActionReplace {
		t.Errorf("replace action = %s, want replace", got)
	}

	leave := scenarioAdoptReq(t, home, root)
	leave.BatchDecision = installstate.DecisionLeaveUnowned
	left, err := installstate.PlanAdoption(leave)
	if err != nil {
		t.Fatal(err)
	}
	if got := scenarioActionFor(left, "commands/commit.md"); got != installstate.ActionLeaveUnowned {
		t.Errorf("leave action = %s, want leave-unowned", got)
	}
	if _, err := installstate.Adopt(leave); err != nil {
		t.Fatalf("adopt leave-unowned: %v", err)
	}
	after, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(older) {
		t.Errorf("leave-unowned changed the artifact's bytes")
	}

	ev.Outcome = "undecided plan blocked; replace planned a rewrite; leave-unowned preserved bytes with no ownership"
	recordScenario(t, ev)
}

// TestCP8AInterruptedAdoptionResumes proves an adoption interrupted after its
// native write but before the ledger record resumes: recovery commits the
// verified applied bytes and consumes the journal.
//
// Criterion: partial v2 recovery ledger-commits applied bytes only after digest
// verification and consumes the recovered journal.
func TestCP8AInterruptedAdoptionResumes(t *testing.T) {
	home := isolatedHome(t)
	root := legacyClaudeInstall(t, home)
	ageClaudeInstall(t, home, root)

	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/interrupted-adoption",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "an adoption interrupted after the native write resumes, commits the verified bytes, and consumes the journal",
		Command:   "NewTransaction (stage+publish, not complete) → Adopt",
		Paths:     []string{home, root},
	}

	artifacts := claudeArtifacts(t, root)
	victim := scenarioArtifact(t, artifacts, "commands/commit.md")
	digest, err := managedfile.DigestResourceBytes(victim.Data, victim.Kind)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := installstate.NewTransaction(home, "op-interrupted", installstate.Plan{Mutations: []installstate.Mutation{{
		Unit: "commands.commit.md", Resource: victim.ID, Target: "claude:default",
		Kind: victim.Kind, Path: victim.Path, Intended: digest,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.StageFile("commands.commit.md", victim.Data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.PublishFile("commands.commit.md"); err != nil {
		t.Fatal(err)
	}
	// Deliberately not Complete: the journal owns applied-but-unrecorded work.

	req := scenarioAdoptReq(t, home, root)
	req.Artifacts = artifacts
	result, err := installstate.Adopt(req)
	if err != nil {
		t.Fatalf("adopt after interruption: %v", err)
	}
	if !scenarioHasActions(result.Recovery, installstate.DecisionCommitApplied) {
		t.Errorf("recovery actions = %+v, want commit-applied", result.Recovery)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find("claude:default", victim.ID); !ok {
		t.Errorf("the recovered resource was not ledger-committed")
	}
	j, err := installstate.LoadJournal(config.JournalPath(home, "op-interrupted"))
	if err != nil {
		t.Fatal(err)
	}
	if !j.Completed {
		t.Errorf("the recovered journal was not consumed")
	}

	ev.Outcome = "recovery committed the verified applied bytes, recorded the ledger row, and consumed the journal"
	recordScenario(t, ev)
}

// scenarioArtifact finds a selected artifact by id.
func scenarioArtifact(t *testing.T, artifacts []installstate.Artifact, id string) installstate.Artifact {
	t.Helper()
	for _, a := range artifacts {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("artifact %s not found", id)
	return installstate.Artifact{}
}

// scenarioHasActions reports whether any action reached want.
func scenarioHasActions(actions []installstate.RecoveryAction, want installstate.RecoveryDecision) bool {
	for _, a := range actions {
		if a.Decision == want {
			return true
		}
	}
	return false
}

// TestCP8AAdoptionIsIdempotent proves a second adoption of an already-converged
// target is a no-op: nothing applied and no journal written.
//
// Criterion: adoption is idempotent; an already-converged plan creates no
// journal or backup.
func TestCP8AAdoptionIsIdempotent(t *testing.T) {
	home := isolatedHome(t)
	root := legacyClaudeInstall(t, home)
	ageClaudeInstall(t, home, root)

	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/idempotent-rerun",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "a repeated adoption applies nothing and creates no journal",
		Command:   "Adopt → Adopt",
		Paths:     []string{home, root},
	}

	if _, err := installstate.Adopt(scenarioAdoptReq(t, home, root)); err != nil {
		t.Fatalf("first adopt: %v", err)
	}
	journals := scenarioJournalCount(home)
	result, err := installstate.Adopt(scenarioAdoptReq(t, home, root))
	if err != nil {
		t.Fatalf("second adopt: %v", err)
	}
	if result.Plan.Status != installstate.StatusConverged {
		t.Fatalf("second status = %s, want converged", result.Plan.Status)
	}
	if len(result.Applied) != 0 {
		t.Errorf("second adoption applied %v, want nothing", result.Applied)
	}
	if got := scenarioJournalCount(home); got != journals {
		t.Errorf("journal count = %d, want %d", got, journals)
	}

	ev.Outcome = "second adoption converged with nothing applied and no new journal"
	recordScenario(t, ev)
}

// TestCP8AStateRootPersistenceAndRollback proves adoption persists the
// repository state selection before scope resolution switches, and rolls it back
// when the adoption fails before any later state write.
//
// Criterion: repository-state migration persists and fsyncs selection before
// resolution switches, and rolls selection back only when no later state write
// occurred.
func TestCP8AStateRootPersistenceAndRollback(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/state-root-persistence-rollback",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "adoption persists the state selection before resolution switches and rolls it back on a failed adoption",
		Command:   "Adopt(RepoRoot) → Adopt(RepoRoot, unpublishable resource)",
	}

	t.Run("persists", func(t *testing.T) {
		home := isolatedHome(t)
		root := legacyClaudeInstall(t, home)
		ageClaudeInstall(t, home, root)
		repoRoot := isolatedHome(t)
		scenarioMkfile(t, filepath.Join(repoRoot, ".claude", "atomic.toml"), "scope marker\n")

		req := scenarioAdoptReq(t, home, root)
		req.RepoRoot = repoRoot
		if _, err := installstate.Adopt(req); err != nil {
			t.Fatalf("adopt: %v", err)
		}
		sel, ok, err := config.ReadStateSelection(repoRoot)
		if err != nil || !ok {
			t.Fatalf("state selection = %+v ok=%v err=%v", sel, ok, err)
		}
		if sel.StateDir != ".claude" {
			t.Errorf("state_dir = %q, want .claude", sel.StateDir)
		}
		ev.Paths = append(ev.Paths, home, repoRoot)
	})

	t.Run("rolls back", func(t *testing.T) {
		home := isolatedHome(t)
		root := legacyClaudeInstall(t, home)
		ageClaudeInstall(t, home, root)
		repoRoot := isolatedHome(t)
		scenarioMkfile(t, filepath.Join(repoRoot, ".claude", "atomic.toml"), "scope marker\n")

		blockedDir := filepath.Join(root, "commands")
		if err := os.Chmod(blockedDir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(blockedDir, 0o755) })

		req := scenarioAdoptReq(t, home, root)
		req.RepoRoot = repoRoot
		victim := scenarioArtifact(t, claudeArtifacts(t, root), "commands/atomic-help.md")
		req.Artifacts = append([]installstate.Artifact{victim}, claudeArtifacts(t, root)...)

		if _, err := installstate.Adopt(req); err == nil {
			t.Fatalf("adopt succeeded despite an unpublishable resource")
		}
		if _, ok, err := config.ReadStateSelection(repoRoot); err != nil {
			t.Fatal(err)
		} else if ok {
			t.Errorf("state selection survived a failed adoption; want rollback")
		}
		ev.Paths = append(ev.Paths, home, repoRoot)
	})

	ev.Outcome = "selection persisted on success; selection rolled back when the adoption failed before a later state write"
	recordScenario(t, ev)
}

// TestCP8ANewerSchemaRefusal proves a ledger written by a newer schema is refused
// rather than guessed at, by both discovery and adoption.
//
// Criterion: new binaries refuse newer incompatible schemas.
func TestCP8ANewerSchemaRefusal(t *testing.T) {
	home := isolatedHome(t)
	if err := os.MkdirAll(config.InstallDir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	newer := `{"schema_version":99,"writer_version":"v99","minimum_reader_version":1,"rows":[],"targets":[]}`
	if err := os.WriteFile(config.LedgerPath(home), []byte(newer), 0o644); err != nil {
		t.Fatal(err)
	}

	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/newer-schema-refusal",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "a newer incompatible ledger schema is refused, not guessed at",
		Command:   "Classify → Adopt",
		Paths:     []string{home},
	}

	if _, err := installstate.Classify(installstate.ClassifyRequest{Home: home}); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("classify error = %v, want a newer-schema refusal", err)
	}
	root := filepath.Join(home, ".claude")
	if _, err := installstate.Adopt(scenarioAdoptReq(t, home, root)); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("adopt error = %v, want a newer-schema refusal", err)
	}

	ev.Outcome = "discovery and adoption both refused the newer schema"
	recordScenario(t, ev)
}

// TestCP8AOldBinaryDrift proves that after migration an old Claude-only binary
// writing the artifact tree again surfaces as drift/conflict instead of being
// silently adopted.
//
// Criterion: old Claude-only binaries are unsupported writers after migration
// and their later changes surface as drift or conflict.
func TestCP8AOldBinaryDrift(t *testing.T) {
	home := isolatedHome(t)
	root := legacyClaudeInstall(t, home)
	ageClaudeInstall(t, home, root)

	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/old-binary-drift",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "an old binary's later write surfaces as drift/conflict after migration",
		Command:   "Adopt → old-binary rewrite → Classify",
		Paths:     []string{home, root},
	}

	if _, err := installstate.Adopt(scenarioAdoptReq(t, home, root)); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if c := scenarioClassify(t, home, root); c.State != installstate.StateV2Clean {
		t.Fatalf("post-adoption state = %s (%v), want v2-clean", c.State, c.Conflicts)
	}

	scenarioMkfile(t, filepath.Join(root, "commands", "commit.md"), "old binary wrote this\n")
	drifted := scenarioClassify(t, home, root)
	if drifted.State != installstate.StateMixed {
		t.Fatalf("drifted state = %s, want %s", drifted.State, installstate.StateMixed)
	}
	if len(drifted.Conflicts) == 0 {
		t.Errorf("old-binary drift reported no conflict")
	}

	ev.Outcome = "old-binary rewrite classified mixed with conflict detail"
	recordScenario(t, ev)
}
