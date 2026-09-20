package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// fakeAdapter is a lifecycle fixture: it projects the resources a test declares,
// converges them by writing their bytes and recording the ownership rows, and
// removes them through the shared uninstall path.
type fakeAdapter struct {
	kind      harness.Kind
	instances []harness.Instance
	// files maps a resource ID to its native absolute path and content.
	files    map[string]fakeFile
	blockers []string
}

type fakeFile struct {
	path    string
	content []byte
	kind    managedfile.Kind
}

func (f *fakeAdapter) Kind() harness.Kind { return f.kind }

func (f *fakeAdapter) Discover(string) ([]harness.Instance, error) { return f.instances, nil }

func (f *fakeAdapter) Capabilities() harness.CapabilityMatrix { return harness.OMPCapabilities() }

func (f *fakeAdapter) Lifecycle(home string) harness.Lifecycle {
	return harness.Lifecycle{
		ProjectFn: func(t harness.Target, req harness.PlanRequest) (harness.Plan, error) {
			plan := harness.Plan{Target: t, Generation: "gen-1", Blockers: f.blockers}
			converged := true
			for id, file := range f.files {
				digest, err := managedfile.DigestResourceBytes(file.content, file.kind)
				if err != nil {
					return harness.Plan{}, err
				}
				plan.Claims = append(plan.Claims, harness.Claim{ID: id, Kind: file.kind, Path: file.path, SelectedDigest: digest})
				obs, err := managedfile.Observe(file.path, file.kind)
				if err != nil {
					return harness.Plan{}, err
				}
				if obs.Digest != digest {
					converged = false
				}
			}
			plan.Converged = converged && len(plan.Blockers) == 0
			return plan, nil
		},
		ConvergeFn: func(t harness.Target, p harness.Plan) (harness.Convergence, error) {
			ledger, err := installstate.LoadLedger(ledgerPath(home))
			if err != nil {
				return harness.Convergence{}, err
			}
			for id, file := range f.files {
				digest, err := managedfile.DigestResourceBytes(file.content, file.kind)
				if err != nil {
					return harness.Convergence{}, err
				}
				if err := managedfile.WriteFileAtomic(file.path, file.content, 0o644); err != nil {
					return harness.Convergence{}, err
				}
				ledger.Upsert(installstate.Row{
					Target:     t.Key(),
					Resource:   id,
					Consumer:   t.Key(),
					Generation: p.Generation,
					Tier:       "unsupported",
					Applied:    installstate.AppliedValue{Path: file.path, Kind: file.kind, Digest: digest},
				})
			}
			ledger.UpsertTarget(installstate.TargetRecord{Harness: string(t.Kind), Instance: t.Instance, NativeRoot: t.NativeRoot, Status: "converged"})
			return harness.Convergence{Target: t, Status: harness.StatusConverged}, ledger.Save(ledgerPath(home))
		},
		VerifyFn: func(t harness.Target) ([]harness.Assessment, error) { return nil, nil },
		RemoveFn: func(t harness.Target) (harness.Removal, error) {
			return harness.RemoveTargetResources(home, t)
		},
	}
}

func testSteps(t *testing.T, home string, adapters ...harness.Adapter) Steps {
	t.Helper()
	steps := DefaultSteps(home)
	steps.AssumeYes = true
	steps.Registry = func(string) (*harness.Registry, error) { return harness.NewRegistry(adapters...) }
	steps.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	return steps
}

// TestConvergeInstallEnrollsTarget proves the whole mutating path: the plan is
// applied, the bytes land, and the ledger records both ownership and enrollment.
func TestConvergeInstallEnrollsTarget(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	files := map[string]fakeFile{
		"steering": {path: filepath.Join(root, "AGENTS.md"), content: []byte("<atomic>\ncontract\n</atomic>\n"), kind: managedfile.KindBlock},
		"package":  {path: filepath.Join(home, ".atomic", "packages", "omp", "atomic", "extensions", "atomic.ts"), content: []byte("module\n"), kind: managedfile.KindFile},
	}
	adapter := &fakeAdapter{kind: harness.KindOMP, instances: []harness.Instance{{Kind: harness.KindOMP, ID: root, NativeRoot: root, Home: home, Exists: true}}, files: files}
	steps := testSteps(t, home, adapter)

	reports, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindOMP}, Enroll: true})
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if len(reports) != 1 || !reports[0].Applied || reports[0].Status != harness.StatusConverged {
		t.Fatalf("reports = %+v", reports)
	}
	if _, err := os.Stat(files["steering"].path); err != nil {
		t.Errorf("steering not written: %v", err)
	}
	ledger, err := installstate.LoadLedger(ledgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Rows) != 2 {
		t.Errorf("rows = %d, want 2", len(ledger.Rows))
	}
	if _, ok := ledger.FindTarget(string(harness.KindOMP), root); !ok {
		t.Error("target was not enrolled")
	}
}

// TestResolveCodexUsesConfiguredHome proves the generic selection resolves the
// Codex kind through the adapter's relocation variable: a configured root
// resolves, and an unset one is an explicit refusal rather than a guessed
// default directory.
func TestResolveCodexUsesConfiguredHome(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "codex-home")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	steps := DefaultSteps(home)

	t.Setenv("CODEX_HOME", root)
	instances, err := steps.Resolve(Selection{Kind: harness.KindCodex})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(instances) != 1 || instances[0].NativeRoot != root {
		t.Fatalf("instances = %+v, want the configured CODEX_HOME", instances)
	}

	t.Setenv("CODEX_HOME", "")
	if _, err := steps.Resolve(Selection{Kind: harness.KindCodex}); !errors.Is(err, harness.ErrUnsupported) {
		t.Errorf("resolve error = %v, want the adapter's ErrUnsupported refusal", err)
	}
}

// TestResolveRefusesAllCombinedWithNamedHarness proves --all can never silently
// drop a harness the caller named explicitly (the CODEX_HOME refusal must
// surface instead of quietly enrolling the other harnesses).
func TestResolveRefusesAllCombinedWithNamedHarness(t *testing.T) {
	home := t.TempDir()
	steps := DefaultSteps(home)
	if _, err := steps.Resolve(Selection{All: true, Kind: harness.KindCodex}); err == nil {
		t.Fatal("resolve accepted --all with a named harness")
	}
	if _, err := steps.Resolve(Selection{All: true, Instance: "x"}); err == nil {
		t.Fatal("resolve accepted --all with a named instance")
	}
	instances, err := steps.Resolve(Selection{All: true})
	if err != nil {
		t.Fatalf("plain --all must still resolve: %v", err)
	}
	if len(instances) == 0 {
		t.Fatal("plain --all resolved no instances")
	}
}

// TestConvergeEnrolledNoEnrollmentIsNoop proves an unenrolled home converges
// nothing and reports no error, which is what update relies on.
func TestConvergeEnrolledNoEnrollmentIsNoop(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	adapter := &fakeAdapter{kind: harness.KindOMP, instances: []harness.Instance{{Kind: harness.KindOMP, ID: root, NativeRoot: root, Home: home, Exists: true}}}
	steps := testSteps(t, home, adapter)

	reports, err := steps.ConvergeEnrolled()
	if err != nil {
		t.Fatalf("converge enrolled: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("reports = %+v, want none without enrollment", reports)
	}
}

// TestConvergeEnrolledReconvergesEnrolledTarget proves update convergence acts
// on the ledger's enrolled targets, not on discovery.
func TestConvergeEnrolledReconvergesEnrolledTarget(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	adapter := &fakeAdapter{kind: harness.KindOMP, instances: []harness.Instance{{Kind: harness.KindOMP, ID: root, NativeRoot: root, Home: home, Exists: true}}, files: map[string]fakeFile{
		"steering": {path: filepath.Join(root, "AGENTS.md"), content: []byte("<atomic>\ncontract\n</atomic>\n"), kind: managedfile.KindBlock},
	}}
	steps := testSteps(t, home, adapter)

	if _, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindOMP}, Enroll: true}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	reports, err := steps.ConvergeEnrolled()
	if err != nil {
		t.Fatalf("converge enrolled: %v", err)
	}
	if len(reports) != 1 || reports[0].Status != harness.StatusConverged {
		t.Fatalf("reports = %+v, want the enrolled target reconverged", reports)
	}
	if reports[0].Target.Key() != adapter.instances[0].Target().Key() {
		t.Errorf("target = %s, want %s", reports[0].Target.Key(), adapter.instances[0].Target().Key())
	}
}

// TestDryRunIsReadOnly proves a dry run leaves the filesystem byte-identical: no
// directory, journal, backup, or native write appears.
func TestDryRunIsReadOnly(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	adapter := &fakeAdapter{kind: harness.KindOMP, instances: []harness.Instance{{Kind: harness.KindOMP, ID: root, NativeRoot: root, Home: home, Exists: true}}, files: map[string]fakeFile{
		"steering": {path: filepath.Join(root, "AGENTS.md"), content: []byte("<atomic>\ncontract\n</atomic>\n"), kind: managedfile.KindBlock},
	}}
	steps := testSteps(t, home, adapter)
	steps.DryRun = true

	before := treeDigest(t, home)
	if _, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindOMP}, Enroll: true}); err != nil {
		t.Fatalf("dry-run converge: %v", err)
	}
	if after := treeDigest(t, home); after != before {
		t.Errorf("dry run changed the filesystem:\nbefore %s\nafter  %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(home, ".atomic")); !os.IsNotExist(err) {
		t.Errorf(".atomic was created by a dry run (stat err = %v)", err)
	}
}

// TestBlockedPlanRefuses proves an observation that forbids mutation is reported
// and nothing is written.
func TestBlockedPlanRefuses(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	adapter := &fakeAdapter{kind: harness.KindOMP, instances: []harness.Instance{{Kind: harness.KindOMP, ID: root, NativeRoot: root, Home: home, Exists: true}}, blockers: []string{"ambiguous block"}, files: map[string]fakeFile{
		"steering": {path: filepath.Join(root, "AGENTS.md"), content: []byte("<atomic>\ncontract\n</atomic>\n"), kind: managedfile.KindBlock},
	}}
	steps := testSteps(t, home, adapter)

	reports, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindOMP}, Enroll: true})
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if len(reports) != 1 || reports[0].Status != harness.StatusConflicted || len(reports[0].Blockers) == 0 {
		t.Fatalf("reports = %+v, want a blocked target", reports)
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("a blocked plan wrote native bytes")
	}
}

// TestRepairRefusesUnenrolledTarget proves repair operates only on explicitly
// enrolled targets.
func TestRepairRefusesUnenrolledTarget(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	adapter := &fakeAdapter{kind: harness.KindOMP, instances: []harness.Instance{{Kind: harness.KindOMP, ID: root, NativeRoot: root, Home: home, Exists: true}}, files: map[string]fakeFile{
		"steering": {path: filepath.Join(root, "AGENTS.md"), content: []byte("<atomic>\ncontract\n</atomic>\n"), kind: managedfile.KindBlock},
	}}
	steps := testSteps(t, home, adapter)

	if _, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindOMP}}); err == nil {
		t.Error("repair of an unenrolled target succeeded")
	}
	// The same selection as an explicit enrollment succeeds.
	if _, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindOMP}, Enroll: true}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
}

// TestUninstallRetainsSharedResource proves one physical resource consumed by two
// enrolled targets survives the first target's removal and is removed with the
// last.
func TestUninstallRetainsSharedResource(t *testing.T) {
	home := t.TempDir()
	first := filepath.Join(home, ".omp", "agent")
	second := filepath.Join(home, ".omp", "work")
	shared := filepath.Join(home, ".atomic", "packages", "omp", "atomic")
	if err := os.MkdirAll(filepath.Join(shared, "extensions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "extensions", "atomic.ts"), []byte("module\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	treeDigest, _, err := managedfile.TreeDigest(shared)
	if err != nil {
		t.Fatal(err)
	}
	seedLedger(t, home,
		[]installstate.TargetRecord{
			{Harness: "omp", Instance: first, NativeRoot: first, Status: "converged"},
			{Harness: "omp", Instance: second, NativeRoot: second, Status: "converged"},
		},
		[]installstate.Row{
			{Target: "omp:" + first, Resource: shared, Consumer: "omp:" + first, Applied: installstate.AppliedValue{Path: shared, Kind: managedfile.KindTree, Digest: treeDigest}},
			{Target: "omp:" + second, Resource: shared, Consumer: "omp:" + second, Applied: installstate.AppliedValue{Path: shared, Kind: managedfile.KindTree, Digest: treeDigest}},
		})

	steps := testSteps(t, home)
	removal, err := steps.UninstallTarget("omp:" + first)
	if err != nil {
		t.Fatalf("uninstall first: %v", err)
	}
	if len(removal.Removed) != 0 || len(removal.Retained) != 1 {
		t.Fatalf("first removal = %+v, want the shared resource retained", removal)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Errorf("shared resource was deleted while another consumer existed: %v", err)
	}

	removal, err = steps.UninstallTarget("omp:" + second)
	if err != nil {
		t.Fatalf("uninstall second: %v", err)
	}
	if len(removal.Removed) != 1 {
		t.Fatalf("second removal = %+v, want the shared resource removed", removal)
	}
	if _, err := os.Stat(shared); !os.IsNotExist(err) {
		t.Errorf("shared resource survived its last consumer (stat err = %v)", err)
	}
}

// TestUninstallRefusesChangedResource proves a resource that changed underneath
// Atomic is preserved and refuses removal.
func TestUninstallRefusesChangedResource(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	path := filepath.Join(root, "AGENTS.md")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user prose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedLedger(t, home,
		[]installstate.TargetRecord{{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"}},
		[]installstate.Row{{Target: "omp:" + root, Resource: "steering", Consumer: "omp:" + root, Applied: installstate.AppliedValue{Path: path, Kind: managedfile.KindFile, Digest: "0000"}}})

	steps := testSteps(t, home)
	if _, err := steps.UninstallTarget("omp:" + root); err == nil {
		t.Fatal("uninstalling a changed resource succeeded")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "user prose\n" {
		t.Errorf("changed resource was not preserved: %q, %v", data, err)
	}
}

// TestFullUninstallKeepsClaimWhenSettingsAreReadOnly proves a full uninstall
// that cannot write a read-only settings file reports the resource skipped and
// keeps its ledger row and enrollment, so a later uninstall can still clear it.
func TestFullUninstallKeepsClaimWhenSettingsAreReadOnly(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.InstallInDir(root); err != nil {
		t.Fatalf("register session-start hook: %v", err)
	}
	owned, err := hooks.ObserveSettingsOwnedInDir(root)
	if err != nil {
		t.Fatalf("observe settings: %v", err)
	}
	seedLedger(t, home,
		[]installstate.TargetRecord{{Harness: "claude", Instance: root, NativeRoot: root, Status: "converged"}},
		[]installstate.Row{{
			Target: "claude:" + root, Resource: "settings.json", Consumer: "claude:" + root,
			Applied: installstate.AppliedValue{Path: owned.Path, Kind: managedfile.KindSettings, Digest: owned.Digest},
		}})

	settingsPath := filepath.Join(root, "settings.json")
	if err := os.Chmod(settingsPath, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(settingsPath, 0o644) })

	report, err := testSteps(t, home).UninstallAll()
	if err != nil {
		t.Fatalf("uninstall all: %v", err)
	}
	if len(report.Targets) != 1 || len(report.Targets[0].Skipped) != 1 || report.Targets[0].Skipped[0] != "settings.json" {
		t.Fatalf("targets = %+v, want the settings resource reported skipped", report.Targets)
	}
	kept, err := installstate.LoadLedger(ledgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := kept.Find("claude:"+root, "settings.json"); !ok {
		t.Error("the skipped removal dropped the ownership claim")
	}
	if _, ok := kept.FindTarget("claude", root); !ok {
		t.Error("the skipped removal dropped the enrollment")
	}
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("the read-only settings file was removed: %v", err)
	}
}

// TestFullUninstallPreservesDataAndRetainsUnresolvedWork proves a full uninstall
// removes completed operational state, preserves the mutable authorities and
// backups, and retains everything an unresolved journal still references.
func TestFullUninstallPreservesDataAndRetainsUnresolvedWork(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	path := filepath.Join(root, "AGENTS.md")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("<atomic>\ncontract\n</atomic>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := managedfile.DigestResourceBytes([]byte("<atomic>\ncontract\n</atomic>\n"), managedfile.KindBlock)
	if err != nil {
		t.Fatal(err)
	}
	seedLedger(t, home,
		[]installstate.TargetRecord{{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"}},
		[]installstate.Row{{
			Target: "omp:" + root, Resource: "steering", Consumer: "omp:" + root,
			Applied: installstate.AppliedValue{Path: path, Kind: managedfile.KindBlock, Digest: digest},
		}})

	// The mutable authorities a full uninstall must never touch.
	for name, content := range map[string]string{
		config.TOMLPath(home):                        "state.dir=\".claude\"\n",
		config.ProfilePath(home):                     "profile\n",
		config.WikisPath(home):                       "wikis\n",
		filepath.Join(config.BackupDir(home), "one"): "backup\n",
	} {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// An unresolved journal that owns a transaction backup and one ledger row.
	// Its native bytes match neither what it intended to write nor the backup, so
	// recovery reports a conflict and leaves it for a later resolution.
	operationID := "interrupted-op"
	otherPath := filepath.Join(home, ".omp", "other", "AGENTS.md")
	backup := filepath.Join(config.TransactionBackupDir(home, operationID), "steering")
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("prior bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := installstate.NewJournal(operationID, time.Unix(0, 0).UTC())
	journal.Mutations = []installstate.Mutation{{
		Unit: "other", Resource: "other-resource", Target: "omp:" + otherPath, Kind: managedfile.KindFile,
		Path: otherPath, Intended: "intended", Prior: "prior", PriorObserved: true,
		Backup: backup, BackupSum: managedfile.Digest([]byte("prior bytes\n")),
	}}
	journal.Record("other", installstate.StateApplied, "intended", time.Unix(0, 0).UTC())
	if err := installstate.WriteJournal(config.JournalPath(home, operationID), journal); err != nil {
		t.Fatal(err)
	}
	ledger, err := installstate.LoadLedger(ledgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	referenced := installstate.Row{
		Target: "omp:" + otherPath, Resource: "other-resource", Consumer: "omp:" + otherPath,
		Applied: installstate.AppliedValue{Path: otherPath, Kind: managedfile.KindFile, Digest: "recorded"},
	}
	// The row has no matching target record: it is ownership an unresolved
	// journal still references, not an enrollment the uninstall enumerates.
	ledger.Upsert(referenced)
	if err := ledger.Save(ledgerPath(home)); err != nil {
		t.Fatal(err)
	}

	steps := testSteps(t, home)
	report, err := steps.UninstallAll()
	if err != nil {
		t.Fatalf("uninstall all: %v", err)
	}
	if len(report.Retention.Journals) == 0 {
		t.Error("the unresolved journal was not retained")
	}
	if !report.Retention.KeepsRow(referenced) {
		t.Error("a ledger row an unresolved journal references was not retained")
	}
	kept, err := installstate.LoadLedger(ledgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := kept.Find("omp:"+otherPath, "other-resource"); !ok {
		t.Error("the referenced ledger row was removed from the ledger")
	}
	if _, err := os.Stat(config.JournalPath(home, operationID)); err != nil {
		t.Errorf("unresolved journal was removed: %v", err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("a referenced transaction backup was removed: %v", err)
	}
	for name := range map[string]string{
		config.TOMLPath(home):    "",
		config.ProfilePath(home): "",
		config.WikisPath(home):   "",
	} {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("full uninstall removed %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(config.BackupDir(home), "one")); err != nil {
		t.Errorf("full uninstall removed the backups directory contents: %v", err)
	}
}

// seedLedger writes a ledger fixture durably.
func seedLedger(t *testing.T, home string, targets []installstate.TargetRecord, rows []installstate.Row) {
	t.Helper()
	ledger, err := installstate.LoadLedger(ledgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ledger.Targets = targets
	ledger.Rows = rows
	if err := ledger.Save(ledgerPath(home)); err != nil {
		t.Fatal(err)
	}
}

// treeDigest digests every path under root, so a read-only assertion compares
// the whole tree and not only the files a test happened to name.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	digest, _, err := managedfile.TreeDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// enrolledOMPFixture seeds one enrolled OMP target whose steering block holds
// exactly the bytes its ledger row records, so a removal plan verifies.
func enrolledOMPFixture(t *testing.T, home string) (root, path string) {
	t.Helper()
	root = filepath.Join(home, ".omp", "agent")
	path = filepath.Join(root, "AGENTS.md")
	content := []byte("<atomic>\ncontract\n</atomic>\n")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := managedfile.DigestResourceBytes(content, managedfile.KindBlock)
	if err != nil {
		t.Fatal(err)
	}
	seedLedger(t, home,
		[]installstate.TargetRecord{{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"}},
		[]installstate.Row{{
			Target: "omp:" + root, Resource: "steering", Consumer: "omp:" + root,
			Applied: installstate.AppliedValue{Path: path, Kind: managedfile.KindBlock, Digest: digest},
		}})
	return root, path
}

// writeStagedJournal records an unresolved journal whose staged projection was
// never applied, which recovery resolves to one safe result (discard staging).
func writeStagedJournal(t *testing.T, home, root, path string) string {
	t.Helper()
	operationID := "staged-op"
	stage := filepath.Join(config.TransactionStageDir(home, operationID), "steering")
	if err := os.MkdirAll(filepath.Dir(stage), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stage, []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := installstate.NewJournal(operationID, time.Unix(0, 0).UTC())
	journal.Mutations = []installstate.Mutation{{
		Unit: "steering", Resource: "steering", Target: "omp:" + root, Kind: managedfile.KindBlock,
		Path: path, Stage: stage, Intended: "intended",
	}}
	if err := installstate.WriteJournal(config.JournalPath(home, operationID), journal); err != nil {
		t.Fatal(err)
	}
	return operationID
}

// writeConflictingJournal records an unresolved journal whose native bytes match
// neither the intended bytes nor the transaction backup, so recovery cannot
// reach one safe result in memory.
func writeConflictingJournal(t *testing.T, home string) string {
	t.Helper()
	operationID := "conflicting-op"
	otherPath := filepath.Join(home, ".omp", "other", "AGENTS.md")
	backup := filepath.Join(config.TransactionBackupDir(home, operationID), "other")
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("prior bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := installstate.NewJournal(operationID, time.Unix(0, 0).UTC())
	journal.Mutations = []installstate.Mutation{{
		Unit: "other", Resource: "other-resource", Target: "omp:" + otherPath, Kind: managedfile.KindFile,
		Path: otherPath, Intended: "intended", Prior: "prior", PriorObserved: true,
		Backup: backup, BackupSum: managedfile.Digest([]byte("prior bytes\n")),
	}}
	journal.Record("other", installstate.StateApplied, "intended", time.Unix(0, 0).UTC())
	if err := installstate.WriteJournal(config.JournalPath(home, operationID), journal); err != nil {
		t.Fatal(err)
	}
	return operationID
}

// TestUninstallDryRunLeavesFilesystemIdentical proves a target-uninstall dry run
// opens no lock, recovers no journal, and previews the unresolved journal in
// memory; the real run afterwards still recovers it and removes the target.
func TestUninstallDryRunLeavesFilesystemIdentical(t *testing.T) {
	home := t.TempDir()
	root, path := enrolledOMPFixture(t, home)
	operationID := writeStagedJournal(t, home, root, path)

	steps := testSteps(t, home)
	steps.DryRun = true
	before := treeDigest(t, home)
	removal, err := steps.UninstallTarget("omp:" + root)
	if err != nil {
		t.Fatalf("dry-run uninstall: %v", err)
	}
	if after := treeDigest(t, home); after != before {
		t.Errorf("dry-run uninstall changed the filesystem:\nbefore %s\nafter  %s", before, after)
	}
	if _, err := os.Stat(config.OperationLockPath(home)); !os.IsNotExist(err) {
		t.Errorf("dry-run uninstall opened the lifecycle lock (stat err = %v)", err)
	}
	if len(removal.Recovery) != 1 {
		t.Errorf("dry-run recovery preview = %+v, want the unresolved journal", removal.Recovery)
	}
	if len(removal.Blockers) != 0 {
		t.Errorf("a simulable journal reported blocked: %v", removal.Blockers)
	}
	if _, err := os.Stat(config.JournalPath(home, operationID)); err != nil {
		t.Errorf("dry-run consumed the unresolved journal: %v", err)
	}
	dryJournal, err := installstate.LoadJournal(config.JournalPath(home, operationID))
	if err != nil {
		t.Fatalf("load dry-run journal: %v", err)
	}
	if dryJournal.Completed {
		t.Error("dry-run marked the unresolved journal completed")
	}

	steps.DryRun = false
	if _, err := steps.UninstallTarget("omp:" + root); err != nil {
		t.Fatalf("real uninstall: %v", err)
	}
	recovered, err := installstate.LoadJournal(config.JournalPath(home, operationID))
	if err != nil {
		t.Fatalf("the real run consumed the journal: %v", err)
	}
	if !recovered.Completed {
		t.Error("real uninstall left the journal unresolved")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("real uninstall left the owned resource (stat err = %v)", err)
	}
}

// TestUninstallAllDryRunReportsRecoveryPrerequisites proves a full-uninstall dry
// run reports the journal it would reconcile and the state that journal retains,
// without opening a lock or recovering it.
func TestUninstallAllDryRunReportsRecoveryPrerequisites(t *testing.T) {
	home := t.TempDir()
	root, path := enrolledOMPFixture(t, home)
	operationID := writeStagedJournal(t, home, root, path)

	steps := testSteps(t, home)
	steps.DryRun = true
	before := treeDigest(t, home)
	report, err := steps.UninstallAll()
	if err != nil {
		t.Fatalf("dry-run uninstall all: %v", err)
	}
	if after := treeDigest(t, home); after != before {
		t.Errorf("dry-run uninstall all changed the filesystem:\nbefore %s\nafter  %s", before, after)
	}
	if _, err := os.Stat(config.OperationLockPath(home)); !os.IsNotExist(err) {
		t.Errorf("dry-run uninstall all opened the lifecycle lock (stat err = %v)", err)
	}
	if len(report.Recovery) != 1 {
		t.Errorf("dry-run recovery preview = %+v, want the unresolved journal", report.Recovery)
	}
	if len(report.Retention.Journals) != 1 {
		t.Errorf("retention = %+v, want the recovery prerequisite reported", report.Retention)
	}
	if len(report.Targets) != 1 {
		t.Errorf("targets = %+v, want the advisory plan for the simulable journal", report.Targets)
	}
	if _, err := os.Stat(config.JournalPath(home, operationID)); err != nil {
		t.Errorf("dry-run consumed the unresolved journal: %v", err)
	}
}

// TestUninstallAllDryRunCreatesNothing proves a full-uninstall dry run on a home
// with no Atomic state writes nothing at all, including the install directory.
func TestUninstallAllDryRunCreatesNothing(t *testing.T) {
	home := t.TempDir()
	steps := testSteps(t, home)
	steps.DryRun = true
	report, err := steps.UninstallAll()
	if err != nil {
		t.Fatalf("dry-run uninstall all: %v", err)
	}
	if len(report.Targets) != 0 || len(report.Recovery) != 0 || len(report.Blockers) != 0 {
		t.Errorf("a stateless dry run reported work: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(home, ".atomic")); !os.IsNotExist(err) {
		t.Errorf("dry-run created the install directory (stat err = %v)", err)
	}
}

// writeInterruptedInstall records an install interrupted after its native bytes
// landed but before the ledger row committed: the native block already holds the
// journal's intended digest and the unit is recorded applied. The enrollment
// record is written only when withRecord says so, so both interruption points —
// after enrollment and before it — are testable. It returns the target key.
func writeInterruptedInstall(t *testing.T, home string, withRecord bool) (key, path string) {
	t.Helper()
	root := filepath.Join(home, ".omp", "agent")
	path = filepath.Join(root, "AGENTS.md")
	content := []byte("<atomic>\ncontract\n</atomic>\n")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := managedfile.DigestResourceBytes(content, managedfile.KindBlock)
	if err != nil {
		t.Fatal(err)
	}
	key = "omp:" + root
	if withRecord {
		seedLedger(t, home,
			[]installstate.TargetRecord{{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"}},
			nil)
	}
	operationID := "interrupted-install-op"
	journal := installstate.NewJournal(operationID, time.Unix(0, 0).UTC())
	journal.Mutations = []installstate.Mutation{{
		Unit: "steering", Resource: "steering", Target: key, Consumer: key,
		Generation: "gen-1", Tier: "unsupported",
		Kind: managedfile.KindBlock, Path: path, Intended: digest,
	}}
	journal.Record("steering", installstate.StateApplied, digest, time.Unix(0, 0).UTC())
	if err := installstate.WriteJournal(config.JournalPath(home, operationID), journal); err != nil {
		t.Fatal(err)
	}
	return key, path
}

// TestUninstallDryRunPlansAgainstSimulatedRecovery proves a dry run of an
// interrupted install plans against the ledger recovery would leave behind: the
// applied-but-unrecorded resource appears in the advisory removal plan — a
// complete Removed list, never a false ErrNotEnrolled — the filesystem stays
// byte-identical, and the real run then recovers and removes it.
func TestUninstallDryRunPlansAgainstSimulatedRecovery(t *testing.T) {
	for _, withRecord := range []bool{true, false} {
		name := "without-enrollment-record"
		if withRecord {
			name = "with-enrollment-record"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			key, path := writeInterruptedInstall(t, home, withRecord)

			steps := testSteps(t, home)
			steps.DryRun = true
			before := treeDigest(t, home)
			removal, err := steps.UninstallTarget(key)
			if err != nil {
				t.Fatalf("dry-run uninstall: %v", err)
			}
			if after := treeDigest(t, home); after != before {
				t.Errorf("dry-run uninstall changed the filesystem:\nbefore %s\nafter  %s", before, after)
			}
			if len(removal.Blockers) != 0 {
				t.Errorf("a simulable journal reported blockers: %v", removal.Blockers)
			}
			if len(removal.Removed) != 1 || removal.Removed[0] != "steering" {
				t.Errorf("Removed = %v, want the applied-but-unrecorded resource", removal.Removed)
			}
			if len(removal.Recovery) != 1 {
				t.Errorf("recovery preview = %+v, want the unresolved journal", removal.Recovery)
			}

			if withRecord {
				report, err := steps.UninstallAll()
				if err != nil {
					t.Fatalf("dry-run uninstall all: %v", err)
				}
				if after := treeDigest(t, home); after != before {
					t.Errorf("dry-run uninstall all changed the filesystem:\nbefore %s\nafter  %s", before, after)
				}
				if len(report.Targets) != 1 || len(report.Targets[0].Removed) != 1 || report.Targets[0].Removed[0] != "steering" {
					t.Errorf("full dry run targets = %+v, want the applied resource in the advisory plan", report.Targets)
				}
			}

			steps.DryRun = false
			if _, err := steps.UninstallTarget(key); err != nil {
				t.Fatalf("real uninstall: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("real uninstall left the recovered resource (stat err = %v)", err)
			}
		})
	}
}

// TestUninstallDryRunReportsBlockedOnRecovery proves a journal that cannot be
// reconciled to one safe result is reported as blocked_on_recovery, not
// recovered, and no removal plan is offered.
func TestUninstallDryRunReportsBlockedOnRecovery(t *testing.T) {
	home := t.TempDir()
	root, _ := enrolledOMPFixture(t, home)
	operationID := writeConflictingJournal(t, home)

	steps := testSteps(t, home)
	steps.DryRun = true
	before := treeDigest(t, home)
	removal, err := steps.UninstallTarget("omp:" + root)
	if err != nil {
		t.Fatalf("dry-run uninstall: %v", err)
	}
	if after := treeDigest(t, home); after != before {
		t.Errorf("dry-run uninstall changed the filesystem:\nbefore %s\nafter  %s", before, after)
	}
	if len(removal.Blockers) == 0 || !strings.Contains(removal.Blockers[0], string(installstate.StatusBlockedOnRecovery)) {
		t.Fatalf("blockers = %v, want blocked_on_recovery", removal.Blockers)
	}
	if len(removal.Removed) != 0 || len(removal.Retained) != 0 {
		t.Errorf("a blocked dry run offered a removal plan: %+v", removal)
	}
	if len(removal.Recovery) != 1 {
		t.Errorf("recovery preview = %+v, want the conflicting journal", removal.Recovery)
	}
	if _, err := os.Stat(config.JournalPath(home, operationID)); err != nil {
		t.Errorf("dry-run consumed the unresolved journal: %v", err)
	}

	report, err := steps.UninstallAll()
	if err != nil {
		t.Fatalf("dry-run uninstall all: %v", err)
	}
	if len(report.Blockers) == 0 || !strings.Contains(report.Blockers[0], string(installstate.StatusBlockedOnRecovery)) {
		t.Fatalf("blockers = %v, want blocked_on_recovery", report.Blockers)
	}
	if len(report.Targets) != 0 {
		t.Errorf("a blocked full dry run offered a removal plan: %+v", report.Targets)
	}
	if len(report.Retention.Journals) != 1 {
		t.Errorf("retention = %+v, want the recovery prerequisite reported", report.Retention)
	}
	if after := treeDigest(t, home); after != before {
		t.Errorf("dry-run full uninstall changed the filesystem:\nbefore %s\nafter  %s", before, after)
	}
}
