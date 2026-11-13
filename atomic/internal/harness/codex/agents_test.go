package codex

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

const fixtureAgent = "atomic-example"

func storeCodexOverride(t *testing.T, home string, ov config.AgentOverride) {
	t.Helper()
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Codex.Agents = map[string]config.AgentOverride{fixtureAgent: ov}
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}
}

func enrolledHome(t *testing.T) (home, root string, a *Adapter) {
	t.Helper()
	home = newHome(t)
	root = newRoot(t, home)
	return home, root, newAdapter(t, root)
}

func fixtureAgentPath(root string) string {
	return filepath.Join(root, "agents", fixtureAgent+".toml")
}

func codexTarget(root string) harness.Target {
	return harness.Target{Kind: harness.KindCodex, Instance: root, NativeRoot: root}
}

func wantOverlaid(t *testing.T, ov config.AgentOverride) []byte {
	t.Helper()
	cat := fixtureCorpus(t)
	for _, art := range cat.Artifacts {
		if art.Semantics.Name != fixtureAgent {
			continue
		}
		p, err := harness.CodexAgent(cat, art)
		if err != nil {
			t.Fatal(err)
		}
		data, err := harness.Overlay(harness.KindCodex, p.Bytes, ov)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	t.Fatalf("fixture corpus has no %s agent", fixtureAgent)
	return nil
}

func agentRow(t *testing.T, home, root string) installstate.Row {
	t.Helper()
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	row, ok := ledger.Find(codexTarget(root).Key(), fixtureAgentPath(root))
	if !ok {
		t.Fatalf("ledger has no row for %s", fixtureAgentPath(root))
	}
	return row
}

func linesContain(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

func TestEnrollWritesOverlaidAgentToCodexHome(t *testing.T) {
	home, root, a := enrolledHome(t)
	ov := config.AgentOverride{Model: "gpt-5.2-codex", Effort: "high"}
	storeCodexOverride(t, home, ov)

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	got := readFile(t, fixtureAgentPath(root))
	if got != string(wantOverlaid(t, ov)) {
		t.Errorf("agent file is not the overlaid projection:\n%s", got)
	}
	if !strings.Contains(got, "model = 'gpt-5.2-codex'") || !strings.Contains(got, "model_reasoning_effort = 'high'") {
		t.Errorf("agent file lacks Codex's model and model_reasoning_effort keys:\n%s", got)
	}
	row := agentRow(t, home, root)
	if row.Applied.Kind != managedfile.KindFile || row.Applied.Digest != managedfile.Digest([]byte(got)) {
		t.Errorf("agent row %+v does not claim the overlaid bytes on disk", row.Applied)
	}
}

func TestEnrollWithoutOverrideWritesNoModelOrEffort(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	got := readFile(t, fixtureAgentPath(root))
	if strings.Contains(got, "model =") || strings.Contains(got, "model_reasoning_effort") {
		t.Errorf("an empty override wrote a key, so Codex would not inherit the session's value:\n%s", got)
	}
}

func TestDeliveredCodexAgentRoundTripsToCanonicalBody(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	doc, err := harness.DecodeCodexAgent([]byte(readFile(t, fixtureAgentPath(root))))
	if err != nil {
		t.Fatal(err)
	}
	cat := fixtureCorpus(t)
	var agent artifacts.Artifact
	for _, art := range cat.OfKind(artifacts.KindAgent) {
		agent = art
	}
	body, err := artifacts.AgentBody(agent)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != fixtureAgent || doc.DeveloperInstructions != string(body) {
		t.Errorf("delivered agent = name %q, instructions %q; want %q and the canonical body", doc.Name, doc.DeveloperInstructions, fixtureAgent)
	}
	proj, err := harness.CodexAgent(cat, agent)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(proj.Unsupported, "skills") {
		t.Errorf("agent projection does not report the dropped skills dependency: %+v", proj.Unsupported)
	}
}

func TestChangedCodexOverrideConverges(t *testing.T) {
	home, root, a := enrolledHome(t)
	storeCodexOverride(t, home, config.AgentOverride{Model: "gpt-a"})
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("first enroll: %v", err)
	}

	next := config.AgentOverride{Model: "gpt-b", Effort: "low"}
	storeCodexOverride(t, home, next)
	plan, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Converged || len(plan.Blockers) > 0 {
		t.Fatalf("plan after an override change = converged %v blockers %v, want an unblocked pending change", plan.Converged, plan.Blockers)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("converge: %v", err)
	}
	want := wantOverlaid(t, next)
	if got := readFile(t, fixtureAgentPath(root)); got != string(want) {
		t.Errorf("agent file did not converge to the new override:\n%s", got)
	}
	if row := agentRow(t, home, root); row.Applied.Digest != managedfile.Digest(want) {
		t.Errorf("row digest %s does not record the converged bytes", row.Applied.Digest)
	}
}

func TestHandEditedCodexAgentConflictsAndKeepsItsBytes(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	edited := "name = 'atomic-example'\nmodel = 'mine'\n"
	writeFile(t, fixtureAgentPath(root), edited)
	storeCodexOverride(t, home, config.AgentOverride{Model: "gpt-a"})

	plan, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !linesContain(plan.Blockers, fixtureAgentPath(root)+" was edited outside Atomic") {
		t.Errorf("blockers %v do not name the hand-edited agent", plan.Blockers)
	}
	for _, decision := range []installstate.Decision{"", installstate.DecisionReplace} {
		if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, Decision: decision}); err == nil {
			t.Fatalf("enroll with decision %q replaced a hand-edited agent file", decision)
		}
	}
	if got := readFile(t, fixtureAgentPath(root)); got != edited {
		t.Errorf("hand edit was overwritten:\n%s", got)
	}
}

const userAgent = "name = 'atomic-example'\ndeveloper_instructions = 'the user'\n"

func TestPreexistingUnownedCodexAgentConflicts(t *testing.T) {
	home, root, a := enrolledHome(t)
	writeFile(t, fixtureAgentPath(root), userAgent)

	_, err := a.Enroll(EnrollRequest{Home: home, Root: root})
	if err == nil || !strings.Contains(err.Error(), "was not written by Atomic") || !strings.Contains(err.Error(), "--leave-unowned") {
		t.Fatalf("enroll err = %v, want a refusal naming --replace and --leave-unowned", err)
	}
	if got := readFile(t, fixtureAgentPath(root)); got != userAgent {
		t.Errorf("the user's agent was overwritten:\n%s", got)
	}
}

func TestReplaceOwnsAnUnownedCodexAgent(t *testing.T) {
	home, root, a := enrolledHome(t)
	writeFile(t, fixtureAgentPath(root), userAgent)

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, Decision: installstate.DecisionReplace}); err != nil {
		t.Fatalf("enroll --replace: %v", err)
	}
	want := wantOverlaid(t, config.AgentOverride{})
	if got := readFile(t, fixtureAgentPath(root)); got != string(want) {
		t.Errorf("--replace kept the user's bytes:\n%s", got)
	}
	if row := agentRow(t, home, root); row.Applied.Digest != managedfile.Digest(want) {
		t.Errorf("--replace recorded %s, want the written bytes", row.Applied.Digest)
	}
}

func TestLeaveUnownedKeepsTheCodexAgentAndEnrollsTheRest(t *testing.T) {
	home, root, a := enrolledHome(t)
	writeFile(t, fixtureAgentPath(root), userAgent)

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, Decision: installstate.DecisionLeaveUnowned}); err != nil {
		t.Fatalf("enroll --leave-unowned: %v", err)
	}
	if got := readFile(t, fixtureAgentPath(root)); got != userAgent {
		t.Errorf("--leave-unowned overwrote the user's agent:\n%s", got)
	}
	if !exists(PackageResource(home)) {
		t.Error("--leave-unowned skipped the rest of the enrollment")
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find(codexTarget(root).Key(), fixtureAgentPath(root)); ok {
		t.Error("--leave-unowned recorded ownership of the user's agent")
	}
	plan, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Converged || len(plan.Blockers) > 0 {
		t.Errorf("a later plan with no decision = converged %v blockers %v; the --leave-unowned choice must persist", plan.Converged, plan.Blockers)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("a later enroll with no decision refused the left agent: %v", err)
	}
	if got := readFile(t, fixtureAgentPath(root)); got != userAgent {
		t.Errorf("a later enroll overwrote the left agent:\n%s", got)
	}
}

func TestReplaceTakesOverALeftCodexAgent(t *testing.T) {
	home, root, a := enrolledHome(t)
	writeFile(t, fixtureAgentPath(root), userAgent)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, Decision: installstate.DecisionLeaveUnowned}); err != nil {
		t.Fatalf("enroll --leave-unowned: %v", err)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, Decision: installstate.DecisionReplace}); err != nil {
		t.Fatalf("enroll --replace: %v", err)
	}
	if got := readFile(t, fixtureAgentPath(root)); got != string(wantOverlaid(t, config.AgentOverride{})) {
		t.Errorf("--replace kept the left agent's bytes:\n%s", got)
	}
	if leftRecorded(t, home, root) {
		t.Error("--replace kept the leave-unowned record")
	}
}

func TestVanishedLeftCodexAgentIsWrittenAndForgotten(t *testing.T) {
	home, root, a := enrolledHome(t)
	writeFile(t, fixtureAgentPath(root), userAgent)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, Decision: installstate.DecisionLeaveUnowned}); err != nil {
		t.Fatalf("enroll --leave-unowned: %v", err)
	}
	if err := os.Remove(fixtureAgentPath(root)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if !exists(fixtureAgentPath(root)) {
		t.Error("enroll did not write the agent once the left file was gone")
	}
	if leftRecorded(t, home, root) {
		t.Error("the leave-unowned record outlived its file")
	}
}

func leftRecorded(t *testing.T, home, root string) bool {
	t.Helper()
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	return ledger.IsLeft(codexTarget(root).Key(), fixtureAgentPath(root))
}

// renamedAgentCorpus is the fixture corpus with its one agent shipped under a
// new name, so the previous generation's agent file is no longer projected.
func renamedAgentCorpus(t *testing.T) *artifacts.Catalog {
	t.Helper()
	base := fixtureCorpus(t)
	renamed := &artifacts.Catalog{Artifacts: append([]artifacts.Artifact(nil), base.Artifacts...)}
	for i, art := range renamed.Artifacts {
		if art.Kind == artifacts.KindAgent {
			art.Source = "agents/atomic-renamed.md"
			art.ID = "agent:" + art.Source
			art.Semantics.Name = "atomic-renamed"
			renamed.Artifacts[i] = art
		}
	}
	return renamed
}

func TestRemovedCodexAgentFileIsRetired(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	renamed := renamedAgentCorpus(t)
	a.Corpus = func() (*artifacts.Catalog, error) { return renamed, nil }

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll the renamed corpus: %v", err)
	}
	if exists(fixtureAgentPath(root)) {
		t.Error("the removed agent's file is still on disk")
	}
	if !exists(filepath.Join(root, "agents", "atomic-renamed.toml")) {
		t.Error("the renamed agent was not written")
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find(codexTarget(root).Key(), fixtureAgentPath(root)); ok {
		t.Error("the removed agent's ledger row survived")
	}
}

func TestRetiredCodexAgentIsReportedAsRetired(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	renamed := renamedAgentCorpus(t)
	a.Corpus = func() (*artifacts.Catalog, error) { return renamed, nil }

	result, err := a.Enroll(EnrollRequest{Home: home, Root: root})
	if err != nil {
		t.Fatalf("enroll the renamed corpus: %v", err)
	}
	if !slices.Contains(result.Retired, fixtureAgentPath(root)) || slices.Contains(result.Applied, fixtureAgentPath(root)) {
		t.Errorf("retired %v applied %v; the removed agent belongs under retired only", result.Retired, result.Applied)
	}
}

// A crash between deleting a retired file and committing the ledger leaves the
// row without its file.
func TestRetiredCodexRowWhoseFileIsGoneIsDropped(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if err := os.Remove(fixtureAgentPath(root)); err != nil {
		t.Fatal(err)
	}
	renamed := renamedAgentCorpus(t)
	a.Corpus = func() (*artifacts.Catalog, error) { return renamed, nil }

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find(codexTarget(root).Key(), fixtureAgentPath(root)); ok {
		t.Error("the row of the already-deleted agent file survived")
	}
}

func TestRemovedButEditedCodexAgentFileBlocks(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	edited := "name = 'atomic-example'\ndeveloper_instructions = 'kept'\n"
	writeFile(t, fixtureAgentPath(root), edited)
	renamed := renamedAgentCorpus(t)
	a.Corpus = func() (*artifacts.Catalog, error) { return renamed, nil }

	plan, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !linesContain(plan.Blockers, fixtureAgentPath(root)) {
		t.Errorf("blockers %v do not name the edited file of the removed agent", plan.Blockers)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err == nil {
		t.Fatal("enroll deleted an edited file of a removed agent")
	}
	if got := readFile(t, fixtureAgentPath(root)); got != edited {
		t.Errorf("the edited file was changed:\n%s", got)
	}
}

func TestPluginCarriesNoAgents(t *testing.T) {
	plugin, err := BuildPlugin(fixtureCorpus(t), harness.CodexCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range plugin.Paths() {
		if strings.Contains(path, "/agents/") {
			t.Errorf("plugin carries %s; Codex reads agents from $CODEX_HOME/agents", path)
		}
	}
}

func TestCodexTargetUninstallRemovesAgentFiles(t *testing.T) {
	home, root, a := enrolledHome(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if !exists(fixtureAgentPath(root)) {
		t.Fatal("enroll wrote no agent file")
	}
	if _, err := a.Lifecycle(home).Remove(codexTarget(root)); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if exists(fixtureAgentPath(root)) {
		t.Error("target uninstall left the agent file behind")
	}
}

func TestCodexAgentDeliveryReadsUnsupported(t *testing.T) {
	home, root, a := enrolledHome(t)
	result, err := a.Enroll(EnrollRequest{Home: home, Root: root})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	found := false
	for _, gap := range result.Unproven {
		found = found || gap.Surface == "agent delivery"
	}
	if !found {
		t.Errorf("enrollment unproven %+v does not report agent delivery", result.Unproven)
	}
	plan, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !linesContain(plan.Unproven, "agent delivery: ") {
		t.Errorf("plan unproven %v does not report agent delivery", plan.Unproven)
	}
}
