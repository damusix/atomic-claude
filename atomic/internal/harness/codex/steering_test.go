package codex

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Codex reads $CODEX_HOME/AGENTS.md into every session, so the user's own
// guidance there must survive enrollment and uninstall byte for byte.
func TestEnrollWritesTheGlobalSteeringBlock(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	writeFile(t, SteeringPath(root), "# mine\n")
	a := newAdapter(t, root)

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, OperationID: "steer"}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	got := readFile(t, SteeringPath(root))
	block, err := SteeringBlock(fixtureCorpus(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "# mine\n") || !strings.Contains(got, string(block)) {
		t.Fatalf("AGENTS.md = %q, want the user's line and the Atomic block", got)
	}
	for _, want := range []string{"## Principles", "You respond in atomic style."} {
		if !strings.Contains(string(block), want) {
			t.Errorf("block lacks %q: the steering body and the output style both belong in it", want)
		}
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	row, ok := ledger.Find(codexTarget(root).Key(), SteeringPath(root))
	if !ok || row.Applied.Kind != managedfile.KindBlock {
		t.Fatalf("steering row = %+v ok=%v, want an owned block row", row, ok)
	}

	if _, err := a.Lifecycle(home).Remove(codexTarget(root)); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := readFile(t, SteeringPath(root)); got != "# mine\n" {
		t.Errorf("after uninstall AGENTS.md = %q, want only the user's line", got)
	}
}

// Most Codex users already keep their own $CODEX_HOME/AGENTS.md, so a file
// that has never held an Atomic block must plan an append, never a blocker.
func TestPlanAppendsToAUserSteeringFileWithoutABlock(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	user := "# my codex rules\n\nalways run the linter\n"
	writeFile(t, SteeringPath(root), user)
	a := newAdapter(t, root)

	plan, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if len(plan.Blockers) > 0 || plan.Converged {
		t.Fatalf("plan = blockers %v converged %v, want an unblocked append", plan.Blockers, plan.Converged)
	}
	if _, err := a.Lifecycle(home).Converge(codexTarget(root), plan); err != nil {
		t.Fatalf("converge: %v", err)
	}
	got := readFile(t, SteeringPath(root))
	block, err := SteeringBlock(fixtureCorpus(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, user) || !strings.HasSuffix(got, string(block)) {
		t.Errorf("AGENTS.md = %q, want the user's bytes unchanged and the block appended", got)
	}
	again, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil || !again.Converged {
		t.Errorf("after converge plan = %+v (%v), want converged", again, err)
	}
}

// No Codex capability row proves a tool, so naming one would steer the model
// toward a call nothing shows Codex has.
func TestSteeringBlockNamesNoUnprovenTool(t *testing.T) {
	block, err := SteeringBlock(fixtureCorpus(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(block), "## Native tools") {
		t.Errorf("Codex steering carries a native-tool paragraph no capability row proves:\n%s", block)
	}
}

func TestClaimsIncludeTheSteeringBlock(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	claims, err := newAdapter(t, root).Claims(home, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range claims {
		if c.Path == SteeringPath(root) && c.Kind == managedfile.KindBlock {
			return
		}
	}
	t.Fatalf("claims = %+v, want a block claim on %s", claims, SteeringPath(root))
}

func TestGlobalSteeringIsAProvenCodexRole(t *testing.T) {
	if !harness.CodexCapabilities().Supports(harness.RoleGlobalSteering) {
		t.Fatal("the global AGENTS.md row is not supported; the adapter must not write the block")
	}
}
