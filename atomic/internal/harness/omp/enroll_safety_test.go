package omp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

const userExtension = "export default function userExtension() {}\n"

// A module the user wrote at the path Atomic publishes is their only copy, so
// enrollment must refuse rather than replace it.
func TestPreexistingUnownedExtensionConflicts(t *testing.T) {
	home, root, a := enrolledProfile(t)
	writeFile(t, ExtensionPath(root), userExtension)

	_, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}})
	if err == nil || !strings.Contains(err.Error(), ExtensionPath(root)) || !strings.Contains(err.Error(), "--leave-unowned") {
		t.Fatalf("enroll err = %v, want a refusal naming the extension and --leave-unowned", err)
	}
	if got := readFile(t, ExtensionPath(root)); got != userExtension {
		t.Errorf("the user's extension was overwritten:\n%s", got)
	}
}

func TestLeaveUnownedKeepsTheExtensionAcrossLaterVerbs(t *testing.T) {
	home, root, a := enrolledProfile(t)
	writeFile(t, ExtensionPath(root), userExtension)

	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, Decision: installstate.DecisionLeaveUnowned}); err != nil {
		t.Fatalf("enroll --leave-unowned: %v", err)
	}
	if _, ok := ownedRow(t, home, root, ExtensionPath(root)); ok {
		t.Error("--leave-unowned recorded ownership of the user's extension")
	}
	plan, err := a.Lifecycle(home).Project(harness.Target{Kind: harness.KindOMP, Instance: root, NativeRoot: root}, harness.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Converged || len(plan.Blockers) > 0 {
		t.Errorf("a later plan with no decision = converged %v blockers %v; the --leave-unowned choice must persist", plan.Converged, plan.Blockers)
	}
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("a later enroll with no decision refused the left extension: %v", err)
	}
	if got := readFile(t, ExtensionPath(root)); got != userExtension {
		t.Errorf("a later enroll overwrote the left extension:\n%s", got)
	}
}

func TestReplaceOwnsAnUnownedExtension(t *testing.T) {
	home, root, a := enrolledProfile(t)
	writeFile(t, ExtensionPath(root), userExtension)

	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, Decision: installstate.DecisionReplace}); err != nil {
		t.Fatalf("enroll --replace: %v", err)
	}
	if got := readFile(t, ExtensionPath(root)); got == userExtension {
		t.Error("--replace kept the user's bytes")
	}
	if _, ok := ownedRow(t, home, root, ExtensionPath(root)); !ok {
		t.Error("--replace recorded no ownership of the published extension")
	}
}

// An extension the user edited after Atomic wrote it blocks every decision,
// the same as an edited agent file.
func TestEditedExtensionBlocks(t *testing.T) {
	home, root, a := enrolledProfile(t)
	if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	writeFile(t, ExtensionPath(root), userExtension)
	a.Corpus = func() (*artifacts.Catalog, error) { return nextGenerationCorpus(t), nil }

	for _, decision := range []installstate.Decision{"", installstate.DecisionReplace, installstate.DecisionLeaveUnowned} {
		if _, err := a.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: root}, Decision: decision}); err == nil || !strings.Contains(err.Error(), "edited outside Atomic") {
			t.Errorf("enroll with decision %q err = %v, want an edited-outside-Atomic refusal", decision, err)
		}
	}
	if got := readFile(t, ExtensionPath(root)); got != userExtension {
		t.Errorf("the edited extension was overwritten:\n%s", got)
	}
}

// failLedgerCommit lets the publication land and fails the ledger write: the
// window between publication and Complete.
func failLedgerCommit(t *testing.T, home string) {
	t.Helper()
	for _, dir := range []string{config.JournalsDir(home), config.TransactionsDir(home)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	install := config.InstallDir(home)
	if err := os.Chmod(install, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(install, 0o755) })
}

func TestRecoveryRestoresAnInterruptedEnrollment(t *testing.T) {
	home := newHome(t)
	defRoot := filepath.Join(home, ".omp", "agent")
	workRoot := filepath.Join(home, ".omp", "profiles", "work", "agent")
	for _, root := range []string{defRoot, workRoot} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	first := newAdapter(t, map[string]string{"": defRoot, "work": workRoot})
	if _, err := first.Enroll(EnrollRequest{Home: home, Profile: Profile{Root: defRoot}}); err != nil {
		t.Fatalf("enroll the default profile: %v", err)
	}
	swapped := nextGenerationCorpus(t)
	next, err := BuildPackage(swapped, harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatal(err)
	}
	second := newAdapter(t, map[string]string{"": defRoot, "work": workRoot})
	second.Corpus = func() (*artifacts.Catalog, error) { return swapped, nil }

	failLedgerCommit(t, home)
	if _, err := second.Enroll(EnrollRequest{Home: home, Profile: Profile{Name: "work", Root: workRoot}, OperationID: "interrupted"}); err == nil {
		t.Fatal("enroll committed its ledger through a read-only install directory")
	}
	if err := os.Chmod(config.InstallDir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := installstate.RecoverJournals(home); err != nil {
		t.Fatalf("recover: %v", err)
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.FindTarget(string(harness.KindOMP), workRoot); !ok {
		t.Errorf("recovery left the published profile without a target record: %+v", ledger.Targets)
	}
	defKey := harness.Target{Kind: harness.KindOMP, Instance: defRoot}.Key()
	row, ok := ledger.Find(defKey, PackageResource(home))
	if !ok || row.Generation != next.Generation {
		t.Errorf("the sibling consumer's package row = %+v (found %v), want generation %s", row, ok, next.Generation)
	}
}
