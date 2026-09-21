package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

func newRoot(t *testing.T, home string) string {
	t.Helper()
	root := filepath.Join(home, "codex-home")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestEnrollWritesPluginThroughTransaction drives one enrollment against a real
// HOME: the journal precedes the native write, the published marketplace tree
// matches its plan-time digest, and the ledger row records the generation and
// tier while the runtime surfaces stay reported as unproven.
func TestEnrollWritesPluginThroughTransaction(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	a := newAdapter(t, root)

	result, err := a.Enroll(EnrollRequest{Home: home, Root: root, OperationID: "enroll-test"})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if result.Status != harness.StatusConverged || result.JournalPath == "" || len(result.Applied) != 1 {
		t.Fatalf("enroll = %+v, want one applied plugin tree through a journal", result)
	}
	// The completed operation is consumed: its journal and transaction tree are
	// removed once the rows are committed, so nothing accumulates per converge.
	if _, err := os.Stat(result.JournalPath); !os.IsNotExist(err) {
		t.Errorf("completed enrollment left its journal behind (stat err = %v)", err)
	}
	if _, err := os.Stat(config.TransactionDir(home, "enroll-test")); !os.IsNotExist(err) {
		t.Errorf("completed enrollment left its transaction tree behind (stat err = %v)", err)
	}

	pkg, err := BuildPlugin(fixtureCorpus(t), harness.CodexCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := pkg.TreeDigest()
	if err != nil {
		t.Fatal(err)
	}
	gotDigest, _, err := managedfile.TreeDigest(config.PackageRoot(home, "codex"))
	if err != nil {
		t.Fatalf("tree digest: %v", err)
	}
	if gotDigest != wantDigest {
		t.Errorf("published tree digest %s != planned %s", gotDigest, wantDigest)
	}
	for _, path := range pkg.Paths() {
		if !exists(filepath.Join(config.PackageRoot(home, "codex"), filepath.FromSlash(path))) {
			t.Errorf("published plugin is missing %s", path)
		}
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	row, ok := ledger.Find(result.Target.Key(), PackageResource(home))
	if !ok {
		t.Fatalf("ledger has no row for %s", PackageResource(home))
	}
	if row.Generation != pkg.Generation || row.Tier != string(Tier) || row.Consumer != result.Target.Key() {
		t.Errorf("ledger row = %+v, want generation, tier, and consumer", row)
	}
	if rec, ok := ledger.FindTarget("codex", root); !ok || rec.Status != string(harness.StatusConverged) {
		t.Errorf("target record = %+v, want a converged enrollment", rec)
	}

	// The capability-disabled guarantees travel with the result: every runtime
	// surface is unsupported, and registration is the only observed row.
	if len(result.Surfaces) == 0 {
		t.Fatal("enroll result reports no runtime surfaces")
	}
	for _, surface := range result.Surfaces {
		if surface.Surface == "" || surface.Evidence == "" {
			t.Errorf("surface %+v is missing its identity or evidence", surface)
		}
		if strings.Contains(surface.Surface, "registration") {
			continue
		}
		if surface.Status != harness.StatusUnsupported {
			t.Errorf("surface %q reports %s; no Codex runtime row is proven", surface.Surface, surface.Status)
		}
	}
	if result.Registration.Plugin {
		t.Error("enrollment claimed native registration without running it")
	}
}

// TestEnrollRerunIsNoOp proves repeated same-generation enrollment writes
// nothing: no journal, no native bytes, and no ledger change.
func TestEnrollRerunIsNoOp(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	a := newAdapter(t, root)

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, OperationID: "first"}); err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	ledgerBefore := readFile(t, config.LedgerPath(home))
	treeBefore, _, err := managedfile.TreeDigest(config.PackageRoot(home, "codex"))
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Enroll(EnrollRequest{Home: home, Root: root, OperationID: "second"})
	if err != nil {
		t.Fatalf("second enroll: %v", err)
	}
	if result.JournalPath != "" || len(result.Applied) != 0 {
		t.Errorf("rerun applied = %+v, want a no-op", result)
	}
	if got := readFile(t, config.LedgerPath(home)); got != ledgerBefore {
		t.Error("rerun rewrote the ledger")
	}
	treeAfter, _, err := managedfile.TreeDigest(config.PackageRoot(home, "codex"))
	if err != nil {
		t.Fatal(err)
	}
	if treeBefore != treeAfter {
		t.Error("rerun rewrote the plugin tree")
	}
	if _, err := os.Stat(config.TransactionDir(home, "second")); !os.IsNotExist(err) {
		t.Errorf("no-op rerun created a transaction directory: %v", err)
	}
}

// TestEnrollRefusesIncompatibleSharedGeneration proves a generation claim the
// operation cannot move stops it: a row for a target the ledger does not enroll
// is a pin, and enrollment refuses before mutation, naming both targets. An
// *enrolled* home's older recorded generation is not a pin — it is what Atomic
// last wrote, and TestTwoHomesConvergeAcrossGenerationSwap proves the operation
// rewrites it.
func TestEnrollRefusesIncompatibleSharedGeneration(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	a := newAdapter(t, root)

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ledger.Upsert(installstate.Row{
		Target:     "codex:/other/home",
		Resource:   PackageResource(home),
		Consumer:   "codex:/other/home",
		Generation: "a-different-generation",
		Tier:       string(Tier),
		Applied:    installstate.AppliedValue{Path: PackageResource(home), Kind: managedfile.KindTree, Digest: "stale"},
	})
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	_, err = a.Enroll(EnrollRequest{Home: home, Root: root, OperationID: "refuse"})
	if err == nil {
		t.Fatal("enrollment converged a shared tree another target requires at another generation")
	}
	if !strings.Contains(err.Error(), "/other/home") || !strings.Contains(err.Error(), "generation") {
		t.Errorf("error %q does not name both targets and the generation conflict", err)
	}
	if exists(config.PackageRoot(home, "codex")) {
		t.Error("refused enrollment still published the plugin tree")
	}
	if _, err := os.Stat(config.JournalPath(home, "refuse")); !os.IsNotExist(err) {
		t.Errorf("refused enrollment wrote a journal: %v", err)
	}
}

// TestTwoHomesConvergeAcrossGenerationSwap proves a shared plugin-tree generation
// move converges every enrolled CODEX_HOME in one operation: the first home's row
// records the older generation, which is stale rather than a requirement, so the
// second home's enrollment must not refuse and both rows must record the new
// generation.
func TestTwoHomesConvergeAcrossGenerationSwap(t *testing.T) {
	home := newHome(t)
	firstRoot := newRoot(t, home)
	secondRoot := filepath.Join(home, "second-codex-home")
	if err := os.MkdirAll(secondRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	first := newAdapter(t, firstRoot)
	for _, root := range []string{firstRoot, secondRoot} {
		if _, err := first.Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
			t.Fatalf("enroll %s at generation one: %v", root, err)
		}
	}

	swapped := nextGenerationCorpus(t)
	next, err := BuildPlugin(swapped, first.Capabilities())
	if err != nil {
		t.Fatal(err)
	}
	nextDigest, err := next.TreeDigest()
	if err != nil {
		t.Fatal(err)
	}
	second := newAdapter(t, firstRoot)
	second.Corpus = func() (*artifacts.Catalog, error) { return swapped, nil }

	result, err := second.Enroll(EnrollRequest{Home: home, Root: secondRoot})
	if err != nil {
		t.Fatalf("the second enrolled home could not move to the new generation: %v", err)
	}
	if result.Generation != next.Generation {
		t.Fatalf("enrolled generation %s, want the selected %s", result.Generation, next.Generation)
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{firstRoot, secondRoot} {
		key := harness.Target{Kind: harness.KindCodex, Instance: root}.Key()
		row, ok := ledger.Find(key, PackageResource(home))
		if !ok {
			t.Fatalf("no plugin-tree row for %s", key)
		}
		if row.Generation != next.Generation || row.Applied.Digest != nextDigest {
			t.Errorf("%s records %s/%s, want the converged %s/%s", key, row.Generation, row.Applied.Digest, next.Generation, nextDigest)
		}
	}
}

// nextGenerationCorpus loads the fixture corpus with one whole-file artifact
// moved, so the selected generation differs from the enrolled one.
func nextGenerationCorpus(t *testing.T) *artifacts.Catalog {
	t.Helper()
	base := fixtureCorpus(t)
	swapped := &artifacts.Catalog{Artifacts: append([]artifacts.Artifact(nil), base.Artifacts...)}
	for i := range swapped.Artifacts {
		if swapped.Artifacts[i].Kind != artifacts.KindRule {
			continue
		}
		body := append([]byte(nil), swapped.Artifacts[i].Body...)
		swapped.Artifacts[i].Body = append(body, []byte("\nNew generation line.\n")...)
		return swapped
	}
	t.Fatal("the fixture corpus carries no rule to move")
	return nil
}

// TestClaimsCarryNoSteering proves the adapter claims only the shared plugin
// tree: Codex has no proven instruction surface, so no steering claim exists.
func TestClaimsCarryNoSteering(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	a := newAdapter(t, root)

	claims, err := a.Claims(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].Kind != managedfile.KindTree {
		t.Fatalf("claims = %+v, want the single plugin-tree claim", claims)
	}
}

// TestLifecycleBindsEnrollmentHooks proves convergence and verification are
// wired rather than reported unsupported.
func TestLifecycleBindsEnrollmentHooks(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	a := newAdapter(t, root)
	target := harness.Target{Kind: harness.KindCodex, Instance: root, NativeRoot: root}

	plan, err := a.Lifecycle(home).Project(target, harness.PlanRequest{})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if len(plan.Claims) != 1 || plan.Generation == "" {
		t.Errorf("plan = %+v, want the plugin-tree claim and a generation", plan)
	}
	convergence, err := a.Lifecycle(home).Converge(target, plan)
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if convergence.Status != harness.StatusConverged {
		t.Errorf("convergence status = %s", convergence.Status)
	}
	assessments, err := a.Lifecycle(home).Verify(target)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	for _, assessment := range assessments {
		if !assessment.Owned {
			t.Errorf("claim %s assessed %s, want owned", assessment.Claim.ID, assessment.Evidence)
		}
	}
}
