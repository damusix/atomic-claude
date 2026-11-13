package omp

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

func storeOMPOverride(t *testing.T, home string, ov config.AgentOverride) {
	t.Helper()
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	cfg.OMP.Agents = map[string]config.AgentOverride{fixtureAgent: ov}
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}
}

func enrolledProfile(t *testing.T) (home, root string, a *Adapter) {
	t.Helper()
	home = newHome(t)
	root = filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return home, root, newAdapter(t, map[string]string{"": root})
}

func fixtureAgentPath(root string) string {
	return filepath.Join(root, "agents", fixtureAgent+".md")
}

func wantOverlaid(t *testing.T, ov config.AgentOverride) []byte {
	t.Helper()
	cat := fixtureCorpus(t)
	for _, art := range cat.Artifacts {
		if art.Semantics.Name != fixtureAgent {
			continue
		}
		p, err := harness.OMPAgent(cat, art)
		if err != nil {
			t.Fatal(err)
		}
		data, err := harness.Overlay(harness.KindOMP, p.Bytes, ov)
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
	key := harness.Target{Kind: harness.KindOMP, Instance: root}.Key()
	row, ok := ledger.Find(key, fixtureAgentPath(root))
	if !ok {
		t.Fatalf("ledger has no row for %s", fixtureAgentPath(root))
	}
	return row
}

func TestEnrollWritesOverlaidAgentToProfileAgentRoot(t *testing.T) {
	home, root, a := enrolledProfile(t)
	ov := config.AgentOverride{Model: "@slow", Effort: "high"}
	storeOMPOverride(t, home, ov)

	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	got := readFile(t, fixtureAgentPath(root))
	if got != string(wantOverlaid(t, ov)) {
		t.Errorf("agent file is not the overlaid projection:\n%s", got)
	}
	if !strings.Contains(got, "thinking: high") || !strings.Contains(got, "model: '@slow'") {
		t.Errorf("agent file lacks OMP's model and thinking keys:\n%s", got)
	}
	row := agentRow(t, home, root)
	if row.Applied.Kind != managedfile.KindFile || row.Applied.Digest != managedfile.Digest([]byte(got)) {
		t.Errorf("agent row %+v does not claim the overlaid bytes on disk", row.Applied)
	}
}

func TestEnrollWithoutOverrideWritesNoModelOrThinking(t *testing.T) {
	home, root, a := enrolledProfile(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	got := readFile(t, fixtureAgentPath(root))
	if strings.Contains(got, "\nmodel:") || strings.Contains(got, "\nthinking:") {
		t.Errorf("an empty override wrote a key, so OMP would not inherit the session's value:\n%s", got)
	}
}

func TestPackageCarriesNoAgents(t *testing.T) {
	pkg, err := BuildPackage(fixtureCorpus(t), harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range pkg.Paths() {
		if strings.HasPrefix(path, "agents/") {
			t.Errorf("corpus store carries %s; OMP reads agents from the profile agent root", path)
		}
	}
}

func TestChangedOverrideConverges(t *testing.T) {
	home, root, a := enrolledProfile(t)
	storeOMPOverride(t, home, config.AgentOverride{Model: "@slow"})
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("first enroll: %v", err)
	}

	next := config.AgentOverride{Model: "@smol", Effort: "low"}
	storeOMPOverride(t, home, next)
	plan, err := a.Lifecycle(home).Project(harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}, harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Converged || len(plan.Blockers) > 0 {
		t.Fatalf("plan after an override change = converged %v blockers %v, want an unblocked pending change", plan.Converged, plan.Blockers)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
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

func TestHandEditedAgentConflictsAndKeepsItsBytes(t *testing.T) {
	home, root, a := enrolledProfile(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	edited := "---\nname: atomic-example\nmodel: mine\n---\nhand edit\n"
	writeFile(t, fixtureAgentPath(root), edited)
	storeOMPOverride(t, home, config.AgentOverride{Model: "@slow"})

	plan, err := a.Lifecycle(home).Project(harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}, harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !linesContain(plan.Blockers, fixtureAgentPath(root)+" was edited outside Atomic") {
		t.Errorf("blockers %v do not name the hand-edited agent", plan.Blockers)
	}
	for _, decision := range []installstate.Decision{"", installstate.DecisionReplace} {
		if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, Decision: decision}); err == nil {
			t.Fatalf("enroll with decision %q replaced a hand-edited agent file", decision)
		}
	}
	if got := readFile(t, fixtureAgentPath(root)); got != edited {
		t.Errorf("hand edit was overwritten:\n%s", got)
	}
}

const userAgent = "---\nname: atomic-example\n---\nthe user's own agent\n"

func TestPreexistingUnownedAgentConflicts(t *testing.T) {
	home, root, a := enrolledProfile(t)
	writeFile(t, fixtureAgentPath(root), userAgent)

	_, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}})
	if err == nil || !strings.Contains(err.Error(), "was not written by Atomic") || !strings.Contains(err.Error(), "--leave-unowned") {
		t.Fatalf("enroll err = %v, want a refusal naming --replace and --leave-unowned", err)
	}
	if got := readFile(t, fixtureAgentPath(root)); got != userAgent {
		t.Errorf("the user's agent was overwritten:\n%s", got)
	}
}

func TestReplaceOwnsAnUnownedAgent(t *testing.T) {
	home, root, a := enrolledProfile(t)
	writeFile(t, fixtureAgentPath(root), userAgent)

	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, Decision: installstate.DecisionReplace}); err != nil {
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

func TestLeaveUnownedKeepsTheAgentAndEnrollsTheRest(t *testing.T) {
	home, root, a := enrolledProfile(t)
	writeFile(t, fixtureAgentPath(root), userAgent)

	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, Decision: installstate.DecisionLeaveUnowned}); err != nil {
		t.Fatalf("enroll --leave-unowned: %v", err)
	}
	if got := readFile(t, fixtureAgentPath(root)); got != userAgent {
		t.Errorf("--leave-unowned overwrote the user's agent:\n%s", got)
	}
	if !exists(ExtensionPath(root)) {
		t.Error("--leave-unowned skipped the rest of the enrollment")
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find(harness.Target{Kind: harness.KindOMP, Instance: root}.Key(), fixtureAgentPath(root)); ok {
		t.Error("--leave-unowned recorded ownership of the user's agent")
	}
	plan, err := a.Lifecycle(home).Project(harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}, harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Converged || len(plan.Blockers) > 0 {
		t.Errorf("a later plan with no decision = converged %v blockers %v; the --leave-unowned choice must persist", plan.Converged, plan.Blockers)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("a later enroll with no decision refused the left agent: %v", err)
	}
	if got := readFile(t, fixtureAgentPath(root)); got != userAgent {
		t.Errorf("a later enroll overwrote the left agent:\n%s", got)
	}
}

func TestTargetUninstallForgetsALeftAgent(t *testing.T) {
	home, root, a := enrolledProfile(t)
	writeFile(t, fixtureAgentPath(root), userAgent)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, Decision: installstate.DecisionLeaveUnowned}); err != nil {
		t.Fatalf("enroll --leave-unowned: %v", err)
	}
	if _, err := a.Lifecycle(home).Remove(harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err == nil {
		t.Error("a re-enroll after uninstall honored the uninstalled target's leave decision")
	}
}

func TestReplaceTakesOverALeftAgent(t *testing.T) {
	home, root, a := enrolledProfile(t)
	writeFile(t, fixtureAgentPath(root), userAgent)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, Decision: installstate.DecisionLeaveUnowned}); err != nil {
		t.Fatalf("enroll --leave-unowned: %v", err)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, Decision: installstate.DecisionReplace}); err != nil {
		t.Fatalf("enroll --replace: %v", err)
	}
	if got := readFile(t, fixtureAgentPath(root)); got != string(wantOverlaid(t, config.AgentOverride{})) {
		t.Errorf("--replace kept the left agent's bytes:\n%s", got)
	}
	if leftRecorded(t, home, root) {
		t.Error("--replace kept the leave-unowned record")
	}
}

func TestVanishedLeftAgentIsWrittenAndForgotten(t *testing.T) {
	home, root, a := enrolledProfile(t)
	writeFile(t, fixtureAgentPath(root), userAgent)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, Decision: installstate.DecisionLeaveUnowned}); err != nil {
		t.Fatalf("enroll --leave-unowned: %v", err)
	}
	if err := os.Remove(fixtureAgentPath(root)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
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
	return ledger.IsLeft(harness.Target{Kind: harness.KindOMP, Instance: root}.Key(), fixtureAgentPath(root))
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

func TestRemovedAgentFileIsRetired(t *testing.T) {
	home, root, a := enrolledProfile(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	renamed := renamedAgentCorpus(t)
	a.Corpus = func() (*artifacts.Catalog, error) { return renamed, nil }

	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll the renamed corpus: %v", err)
	}
	if exists(fixtureAgentPath(root)) {
		t.Error("the removed agent's file is still on disk")
	}
	if !exists(filepath.Join(root, "agents", "atomic-renamed.md")) {
		t.Error("the renamed agent was not written")
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find(harness.Target{Kind: harness.KindOMP, Instance: root}.Key(), fixtureAgentPath(root)); ok {
		t.Error("the removed agent's ledger row survived")
	}
}

func TestRetiredAgentIsReportedAsRetired(t *testing.T) {
	home, root, a := enrolledProfile(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	renamed := renamedAgentCorpus(t)
	a.Corpus = func() (*artifacts.Catalog, error) { return renamed, nil }

	result, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}})
	if err != nil {
		t.Fatalf("enroll the renamed corpus: %v", err)
	}
	if !slices.Contains(result.Retired, fixtureAgentPath(root)) || slices.Contains(result.Applied, fixtureAgentPath(root)) {
		t.Errorf("retired %v applied %v; the removed agent belongs under retired only", result.Retired, result.Applied)
	}
}

// A crash between deleting a retired file and committing the ledger leaves the
// row without its file.
func TestRetiredRowWhoseFileIsGoneIsDropped(t *testing.T) {
	home, root, a := enrolledProfile(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if err := os.Remove(fixtureAgentPath(root)); err != nil {
		t.Fatal(err)
	}
	renamed := renamedAgentCorpus(t)
	a.Corpus = func() (*artifacts.Catalog, error) { return renamed, nil }

	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find(harness.Target{Kind: harness.KindOMP, Instance: root}.Key(), fixtureAgentPath(root)); ok {
		t.Error("the row of the already-deleted agent file survived")
	}
}

func TestRemovedButEditedAgentFileBlocks(t *testing.T) {
	home, root, a := enrolledProfile(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	edited := "---\nname: atomic-example\n---\nkept by the user\n"
	writeFile(t, fixtureAgentPath(root), edited)
	renamed := renamedAgentCorpus(t)
	a.Corpus = func() (*artifacts.Catalog, error) { return renamed, nil }

	plan, err := a.Lifecycle(home).Project(harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}, harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !linesContain(plan.Blockers, fixtureAgentPath(root)) {
		t.Errorf("blockers %v do not name the edited file of the removed agent", plan.Blockers)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err == nil {
		t.Fatal("enroll deleted an edited file of a removed agent")
	}
	if got := readFile(t, fixtureAgentPath(root)); got != edited {
		t.Errorf("the edited file was changed:\n%s", got)
	}
}

func TestTargetUninstallRemovesAgentFiles(t *testing.T) {
	home, root, a := enrolledProfile(t)
	storeOMPOverride(t, home, config.AgentOverride{Effort: "high"})
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if !exists(fixtureAgentPath(root)) {
		t.Fatal("enroll wrote no agent file")
	}
	if _, err := a.Lifecycle(home).Remove(harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if exists(fixtureAgentPath(root)) {
		t.Error("target uninstall left the agent file behind")
	}
}

func TestAgentDeliveryReadsUnsupported(t *testing.T) {
	home, root, a := enrolledProfile(t)
	result, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}})
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
	plan, err := a.Lifecycle(home).Project(harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}, harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !linesContain(plan.Unproven, "agent delivery: ") {
		t.Errorf("plan unproven %v does not report agent delivery", plan.Unproven)
	}
}

func linesContain(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}
