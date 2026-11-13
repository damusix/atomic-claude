// Migration scenarios over isolated Claude homes carrying fixtures the real
// installer produced, driven through the production installstate engine.
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
	c, err := installstate.Classify(installstate.ClassifyRequest{Home: home, NativeRoot: root, Target: "claude:default", Claims: scenarioClaims(t, root)})
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

// A complete prior-version install must classify legacy-complete and adopt to
// the selected generation without touching user prose around the block.
func TestLegacyCompleteDiscovery(t *testing.T) {
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

// A listed-but-missing artifact is drift, restored only when the caller converges.
func TestListedMissingDrift(t *testing.T) {
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

// A pending legacy proposal must block automatic adoption until resolved.
func TestPendingProposalBlocks(t *testing.T) {
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

// A corrupt legacy snapshot must block adoption until the caller acknowledges
// the restoration evidence is unusable.
func TestCorruptSnapshotAcknowledged(t *testing.T) {
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

// Recovery depends on an unresolved journal reading as in-flight and an
// unjournaled transaction remnant as orphaned.
func TestV2MigrationStates(t *testing.T) {
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

// A row/bytes disagreement is per-resource drift, not a mixed install, and needs
// an explicit replace-or-leave-unowned decision.
func TestDriftedRowIsPerResourceDrift(t *testing.T) {
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
		Scenario:  "cp8a/migration/drifted-row-drift",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "a row/bytes disagreement is per-resource drift and needs a decision, not mixed",
		Command:   "Classify → PlanAdoption",
		Paths:     []string{home, root},
	}

	c := scenarioClassify(t, home, root)
	if c.State == installstate.StateMixed {
		t.Fatalf("state = %s (%v), want a decidable state — a row/bytes disagreement is per-resource drift", c.State, c.Conflicts)
	}
	reported := false
	for _, d := range c.Drift {
		if strings.Contains(d, drifted) {
			reported = true
		}
	}
	if !reported {
		t.Errorf("drift = %v, want the changed resource reported", c.Drift)
	}

	// With no batch decision the changed resource stays on the explicit path.
	undecided := scenarioAdoptReq(t, home, root)
	undecided.BatchDecision = ""
	plan, err := installstate.PlanAdoption(undecided)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status == installstate.StatusReady {
		t.Fatalf("plan status = %s, want a refused plan without a decision", plan.Status)
	}
	if _, err := installstate.Adopt(undecided); err == nil {
		t.Fatal("adoption succeeded without a decision for a drifted resource")
	}

	ev.Outcome = "drifted row classified as per-resource drift and kept on the decision path"
	recordScenario(t, ev)
}

// An older-version non-block artifact needs a per-resource decision: replace
// rewrites it, leave-unowned keeps the bytes and records no ownership.
func TestReplaceOrLeaveUnowned(t *testing.T) {
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

// An adoption interrupted between native write and ledger record must resume by
// committing the applied bytes only after digest verification.
func TestInterruptedAdoptionResumes(t *testing.T) {
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
	journalPath := config.JournalPath(home, "op-interrupted")
	if !fileExists(journalPath) {
		t.Fatalf("the interrupted transaction wrote no journal to recover")
	}

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
	// The journal existed when Adopt ran, so its absence means recovery
	// consumed it.
	if fileExists(journalPath) {
		t.Errorf("the recovered journal survived adoption; it was not consumed")
	}
	// The recovery effect: the applied bytes it committed are on disk at the
	// digest the journal verified, so the ledger row and the artifact agree.
	obs, err := managedfile.Observe(victim.Path, victim.Kind)
	if err != nil {
		t.Fatalf("observe recovered artifact: %v", err)
	}
	if obs.Digest != digest {
		t.Errorf("recovered bytes digest = %q, want the verified applied digest %q", obs.Digest, digest)
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

// Re-adopting a converged target must create no journal or backup.
func TestAdoptionIsIdempotent(t *testing.T) {
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

// A ledger from a newer schema must be refused rather than guessed at.
func TestNewerSchemaRefusal(t *testing.T) {
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

// An old Claude-only binary is an unsupported writer after migration, so its
// writes must surface as per-resource drift, never be silently adopted.
func TestOldBinaryDrift(t *testing.T) {
	home := isolatedHome(t)
	root := legacyClaudeInstall(t, home)
	ageClaudeInstall(t, home, root)

	ev := ScenarioEvidence{
		Scenario:  "cp8a/migration/old-binary-drift",
		Group:     "migration",
		Engine:    "installstate",
		Criterion: "an old binary's later write surfaces as drift/conflict after migration",
		Command:   "Adopt → old-binary rewrite → Classify → replace",
		Paths:     []string{home, root},
	}

	if _, err := installstate.Adopt(scenarioAdoptReq(t, home, root)); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if c := scenarioClassify(t, home, root); c.State != installstate.StateV2Clean {
		t.Fatalf("post-adoption state = %s (%v), want v2-clean", c.State, c.Conflicts)
	}

	driftedPath := filepath.Join(root, "commands", "commit.md")
	scenarioMkfile(t, driftedPath, "old binary wrote this\n")
	drifted := scenarioClassify(t, home, root)
	if drifted.State == installstate.StateMixed {
		t.Fatalf("drifted state = %s (%v), want a decidable state — drift is per resource", drifted.State, drifted.Conflicts)
	}
	reported := false
	for _, d := range drifted.Drift {
		if strings.Contains(d, driftedPath) {
			reported = true
		}
	}
	if !reported {
		t.Errorf("old-binary drift = %v, want the changed resource reported", drifted.Drift)
	}

	// The replace decision the request already carries repairs the drift.
	if _, err := installstate.Adopt(scenarioAdoptReq(t, home, root)); err != nil {
		t.Fatalf("replace-decided repair of old-binary drift: %v", err)
	}
	for _, a := range claudeArtifacts(t, root) {
		if a.ID != "commands/commit.md" {
			continue
		}
		got, err := os.ReadFile(driftedPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(a.Data) {
			t.Errorf("repair did not restore the selected bytes: %q", got)
		}
	}

	ev.Outcome = "old-binary rewrite classified as per-resource drift and repaired under replace"
	recordScenario(t, ev)
}
