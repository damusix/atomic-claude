package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// legacyClaudeHome is a home whose config.toml carries a legacy [install]
// record, so the Claude root counts as an unadopted legacy install.
func legacyClaudeHome(t *testing.T) (home, root string, adapter *claude.Adapter) {
	t.Helper()
	home = t.TempDir()
	root = filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Install.Version = "1.0.0"
	if err := config.WritePersist(config.TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}
	adapter = claude.New()
	adapter.ConfigDir = func(string) string { return root }
	return home, root, adapter
}

// A declined adoption is the user's refusal: nothing is enrolled and no seeded
// user state appears, since seeding waits for the approval.
func TestDeclinedAdoptionEnrollsAndSeedsNothing(t *testing.T) {
	home, root, adapter := legacyClaudeHome(t)
	steps := testSteps(t, home, adapter)
	steps.AssumeYes = false
	steps.Confirm = func(string, string, bool) (bool, error) { return false, nil }

	reports, err := steps.Converge(ConvergeRequest{
		Instances:   []harness.Instance{{Kind: harness.KindClaude, ID: root, NativeRoot: root, Home: home, Exists: true}},
		Enroll:      true,
		AdoptLegacy: true,
	})
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if len(reports) != 1 || reports[0].Status != harness.StatusStale || !slicesEqual(reports[0].Blockers, []string{"declined"}) {
		t.Fatalf("reports = %+v, want one stale declined target", reports)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Targets) != 0 || len(ledger.Rows) != 0 {
		t.Errorf("a declined adoption enrolled something: %+v %+v", ledger.Targets, ledger.Rows)
	}
	if _, err := os.Stat(config.ProfilePath(home)); !os.IsNotExist(err) {
		t.Errorf("a declined adoption seeded a profile (stat err = %v)", err)
	}
}

// Declining one target's adoption leaves every other target to its own prompt.
func TestDeclinedAdoptionStillPromptsTheNextTarget(t *testing.T) {
	home, root, adapter := legacyClaudeHome(t)
	ompRoot := filepath.Join(home, ".omp", "agent")
	omp := &fakeAdapter{
		kind:      harness.KindOMP,
		instances: []harness.Instance{{Kind: harness.KindOMP, ID: ompRoot, NativeRoot: ompRoot, Home: home, Exists: true}},
		files: map[string]fakeFile{
			"steering": {path: filepath.Join(ompRoot, "AGENTS.md"), content: []byte("<atomic>\nx\n</atomic>\n"), kind: managedfile.KindBlock},
		},
	}
	steps := testSteps(t, home, adapter, omp)
	steps.AssumeYes = false
	var titles []string
	steps.Confirm = func(title, _ string, _ bool) (bool, error) {
		titles = append(titles, title)
		return !strings.HasPrefix(title, "Adopt "), nil
	}

	reports, err := steps.Converge(ConvergeRequest{
		Instances: []harness.Instance{
			{Kind: harness.KindClaude, ID: root, NativeRoot: root, Home: home, Exists: true},
			{Kind: harness.KindOMP, ID: ompRoot, NativeRoot: ompRoot, Home: home, Exists: true},
		},
		Enroll:      true,
		AdoptLegacy: true,
	})
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if len(reports) != 2 || reports[0].Status != harness.StatusStale || reports[1].Status != harness.StatusConverged {
		t.Fatalf("reports = %+v, want the Claude adoption declined and OMP converged", reports)
	}
	if len(titles) != 2 || !strings.HasPrefix(titles[1], "Converge omp:") {
		t.Errorf("prompts = %q, want the adoption prompt then OMP's own", titles)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.FindTarget(string(harness.KindClaude), root); ok {
		t.Error("the declined Claude root was enrolled")
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
