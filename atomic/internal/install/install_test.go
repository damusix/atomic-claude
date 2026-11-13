package install

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// fakeAdapter is a lifecycle fixture over the resources a test declares; it
// removes them through the shared uninstall path.
type fakeAdapter struct {
	kind      harness.Kind
	instances []harness.Instance
	// files maps a resource ID to its native absolute path and content.
	files    map[string]fakeFile
	blockers []string
	// projectErr makes the projection fail, so a caller's error handling is
	// observable.
	projectErr        error
	applied, unproven []string
	planUnproven      []string
	notices           []string
	removed           func(harness.Target, harness.Removal) (harness.Removal, error)
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
			if f.projectErr != nil {
				return harness.Plan{}, f.projectErr
			}
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
			plan.Notices = f.notices
			plan.Unproven = f.planUnproven
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
			return harness.Convergence{Target: t, Status: harness.StatusConverged, Applied: f.applied, Itemized: f.applied != nil, Unproven: f.unproven}, ledger.Save(ledgerPath(home))
		},
		VerifyFn: func(t harness.Target) ([]harness.Assessment, error) { return nil, nil },
		RemoveFn: func(t harness.Target) (harness.Removal, error) {
			return harness.RemoveTargetResources(home, t)
		},
		RemovedFn: f.removed,
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

// An adapter that itemizes its writes and wrote nothing must not read as
// applied, and what it could not deliver must reach the report.
func TestConvergeReportsOnlyItemizedWrites(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "codex-home")
	files := map[string]fakeFile{"steering": {path: filepath.Join(root, "AGENTS.md"), content: []byte("<atomic>\nx\n</atomic>\n"), kind: managedfile.KindBlock}}
	adapter := &fakeAdapter{kind: harness.KindCodex, instances: []harness.Instance{{Kind: harness.KindCodex, ID: root, NativeRoot: root, Home: home, Exists: true}}, files: files,
		applied: []string{}, unproven: []string{"native plugin registration: no codex; fix: install it"}}

	reports, err := testSteps(t, home, adapter).Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindCodex}, Enroll: true})
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if len(reports) != 1 || reports[0].Applied {
		t.Fatalf("reports = %+v, want one target not reported applied", reports)
	}
	if !slices.Contains(reports[0].Unproven, adapter.unproven[0]) {
		t.Errorf("unproven = %v, want the convergence gap", reports[0].Unproven)
	}
}

// The plan and the convergence can both observe the same gap; the report names
// it once.
func TestConvergeReportsEachGapOnce(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "codex-home")
	gap := "native plugin registration: config.toml does not parse; fix: repair it"
	adapter := &fakeAdapter{kind: harness.KindCodex, instances: []harness.Instance{{Kind: harness.KindCodex, ID: root, NativeRoot: root, Home: home, Exists: true}},
		files:        map[string]fakeFile{"steering": {path: filepath.Join(root, "AGENTS.md"), content: []byte("<atomic>\nx\n</atomic>\n"), kind: managedfile.KindBlock}},
		planUnproven: []string{gap}, unproven: []string{gap}}
	reports, err := testSteps(t, home, adapter).Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindCodex}, Enroll: true})
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Unproven) != 1 {
		t.Errorf("reports = %+v, want the gap named once", reports)
	}
}

// A mutation outside the ledger, such as Codex editing its own config.toml,
// is part of what the user consents to.
func TestConsentNamesTheAdapterNotices(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "codex-home")
	notice := "runs `codex plugin add`, which edits " + filepath.Join(root, "config.toml")
	adapter := &fakeAdapter{kind: harness.KindCodex, instances: []harness.Instance{{Kind: harness.KindCodex, ID: root, NativeRoot: root, Home: home, Exists: true}},
		files: map[string]fakeFile{"steering": {path: filepath.Join(root, "AGENTS.md"), content: []byte("<atomic>\nx\n</atomic>\n"), kind: managedfile.KindBlock}}, notices: []string{notice}}
	steps := testSteps(t, home, adapter)
	var shown []string
	steps.AssumeYes = false
	steps.Confirm = func(title, desc string, _ bool) (bool, error) {
		shown = append(shown, desc)
		return true, nil
	}
	if _, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindCodex}, Enroll: true}); err != nil {
		t.Fatalf("converge: %v", err)
	}
	if len(shown) == 0 || !strings.Contains(strings.Join(shown, "\n"), notice) {
		t.Errorf("consent showed %q, want the notice %q", shown, notice)
	}
}

// A post-removal step that could not undo native state reports it as a notice;
// every target still uninstalls and nothing of the failed one stays enrolled.
func TestUninstallAllReportsAPostRemovalNotice(t *testing.T) {
	home := t.TempDir()
	codexRoot := filepath.Join(home, "codex-home")
	ompRoot := filepath.Join(home, ".omp", "agent")
	cache := filepath.Join(codexRoot, "plugins", "cache")
	notice := "Codex may still register atomic@atomic-claude; run `codex plugin remove atomic@atomic-claude`"
	codexAdapter := &fakeAdapter{kind: harness.KindCodex, instances: []harness.Instance{{Kind: harness.KindCodex, ID: codexRoot, NativeRoot: codexRoot, Home: home, Exists: true}},
		files: map[string]fakeFile{cache: {path: filepath.Join(cache, "plugin.json"), content: []byte("{}\n"), kind: managedfile.KindFile}}}
	codexAdapter.removed = func(_ harness.Target, r harness.Removal) (harness.Removal, error) {
		r.Notices = append(r.Notices, notice)
		return r, nil
	}
	ompAdapter := &fakeAdapter{kind: harness.KindOMP, instances: []harness.Instance{{Kind: harness.KindOMP, ID: ompRoot, NativeRoot: ompRoot, Home: home, Exists: true}},
		files: map[string]fakeFile{"steering": {path: filepath.Join(ompRoot, "AGENTS.md"), content: []byte("<atomic>\nx\n</atomic>\n"), kind: managedfile.KindBlock}}}
	steps := testSteps(t, home, codexAdapter, ompAdapter)
	for _, kind := range []harness.Kind{harness.KindCodex, harness.KindOMP} {
		if _, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: kind}, Enroll: true}); err != nil {
			t.Fatalf("converge %s: %v", kind, err)
		}
	}

	report, err := steps.UninstallAll()
	if err != nil {
		t.Fatalf("uninstall all: %v", err)
	}
	if len(report.Targets) != 2 {
		t.Fatalf("targets = %+v, want both removals reported", report.Targets)
	}
	if !slices.ContainsFunc(report.Targets, func(r harness.Removal) bool { return slices.Contains(r.Notices, notice) }) {
		t.Errorf("targets = %+v, want the Codex notice reported", report.Targets)
	}
	ledger, err := installstate.LoadLedger(ledgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	codexKey := harness.Target{Kind: harness.KindCodex, Instance: codexRoot}.Key()
	if _, ok := ledger.Find(codexKey, cache); ok {
		t.Error("the Codex row survived the uninstall")
	}
	for kind, root := range map[harness.Kind]string{harness.KindCodex: codexRoot, harness.KindOMP: ompRoot} {
		if _, ok := ledger.FindTarget(string(kind), root); ok {
			t.Errorf("the %s target was not uninstalled", kind)
		}
	}
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

// TestResolveCodexUsesConfiguredHome proves an unset CODEX_HOME is an explicit
// refusal rather than a guessed default directory.
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
// drop a harness the caller named explicitly.
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

// TestUninstallRetainsSharedResource proves a resource two targets consume
// survives the first removal and goes with the last.
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

// TestUninstallSkipsChangedResource proves a changed resource is skipped rather
// than aborting the whole removal.
func TestUninstallSkipsChangedResource(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	path := filepath.Join(root, "AGENTS.md")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user prose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	intact := filepath.Join(root, "commands", "help.md")
	if err := os.MkdirAll(filepath.Dir(intact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(intact, []byte("owned bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	intactDigest, err := managedfile.DigestResourceBytes([]byte("owned bytes\n"), managedfile.KindFile)
	if err != nil {
		t.Fatal(err)
	}
	seedLedger(t, home,
		[]installstate.TargetRecord{{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"}},
		[]installstate.Row{
			{Target: "omp:" + root, Resource: "steering", Consumer: "omp:" + root, Applied: installstate.AppliedValue{Path: path, Kind: managedfile.KindFile, Digest: "0000"}},
			{Target: "omp:" + root, Resource: "commands/help.md", Consumer: "omp:" + root, Applied: installstate.AppliedValue{Path: intact, Kind: managedfile.KindFile, Digest: intactDigest}},
		})

	steps := testSteps(t, home)
	removal, err := steps.UninstallTarget("omp:" + root)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(removal.Skipped) != 1 || removal.Skipped[0] != "steering" {
		t.Fatalf("skipped = %v, want [steering]", removal.Skipped)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "user prose\n" {
		t.Errorf("changed resource was not preserved: %q, %v", data, err)
	}
	if _, err := os.Stat(intact); !os.IsNotExist(err) {
		t.Errorf("intact owned resource survived the removal: %v", err)
	}
}

// TestUninstallWithoutDiscardKeepsChangedClaim proves the default removal keeps
// a skipped resource's claim, so a later uninstall can finish the job.
func TestUninstallWithoutDiscardKeepsChangedClaim(t *testing.T) {
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
		[]installstate.Row{{Target: "omp:" + root, Resource: "steering", Consumer: "omp:" + root,
			Applied: installstate.AppliedValue{Path: path, Kind: managedfile.KindFile, Digest: "0000"}}})

	removal, err := testSteps(t, home).UninstallTarget("omp:" + root)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(removal.Skipped) != 1 || removal.SkipReasons["steering"] == "" {
		t.Fatalf("skipped = %v (reasons %v), want steering skipped with a reason", removal.Skipped, removal.SkipReasons)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "user prose\n" {
		t.Errorf("changed resource was not preserved: %q, %v", data, err)
	}
	ledger, err := installstate.LoadLedger(ledgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find("omp:"+root, "steering"); !ok {
		t.Error("the default removal dropped the skipped resource's row")
	}
	if _, ok := ledger.FindTarget("omp", root); !ok {
		t.Error("the default removal dropped the enrollment")
	}
}

// TestUninstallDiscardChangedClearsTarget proves the explicit discard releases a
// claim the default removal keeps, and a declined confirmation touches nothing.
func TestUninstallDiscardChangedClearsTarget(t *testing.T) {
	newFixture := func(t *testing.T) (home, root, path string) {
		t.Helper()
		home = t.TempDir()
		root = filepath.Join(home, ".omp", "agent")
		path = filepath.Join(root, "AGENTS.md")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("user prose\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		seedLedger(t, home,
			[]installstate.TargetRecord{{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"}},
			[]installstate.Row{{Target: "omp:" + root, Resource: "steering", Consumer: "omp:" + root,
				Applied: installstate.AppliedValue{Path: path, Kind: managedfile.KindFile, Digest: "0000"}}})
		return home, root, path
	}

	t.Run("declined confirmation leaves the claim", func(t *testing.T) {
		home, root, path := newFixture(t)
		steps := testSteps(t, home)
		steps.AssumeYes = false
		steps.DiscardChanged = true
		steps.Confirm = func(title, _ string, _ bool) (bool, error) {
			return !strings.HasPrefix(title, "Discard changed resource"), nil
		}

		removal, err := steps.UninstallTarget("omp:" + root)
		if err != nil {
			t.Fatalf("uninstall: %v", err)
		}
		if len(removal.Discarded) != 0 || len(removal.Skipped) != 1 {
			t.Fatalf("removal = %+v, want the discard declined", removal)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("a declined discard removed the resource: %v", err)
		}
		ledger, err := installstate.LoadLedger(ledgerPath(home))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := ledger.Find("omp:"+root, "steering"); !ok {
			t.Error("a declined discard dropped the claim")
		}
	})

	t.Run("approved discard removes the resource and the claim", func(t *testing.T) {
		home, root, path := newFixture(t)
		steps := testSteps(t, home)
		steps.DiscardChanged = true
		var printed strings.Builder
		steps.Notice = writerFunc(func(p []byte) {
			if _, err := os.Stat(path); err != nil {
				t.Errorf("the plan was printed after the discard: %v", err)
			}
			printed.Write(p)
		})

		removal, err := steps.UninstallTarget("omp:" + root)
		if err != nil {
			t.Fatalf("uninstall: %v", err)
		}
		if len(removal.Discarded) != 1 || removal.Discarded[0] != "steering" {
			t.Fatalf("discarded = %v, want [steering]", removal.Discarded)
		}
		if !strings.Contains(printed.String(), "steering") || !strings.Contains(printed.String(), path) {
			t.Errorf("printed plan = %q, want steering and %s named", printed.String(), path)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("discarded resource survived (stat err = %v)", err)
		}
		ledger, err := installstate.LoadLedger(ledgerPath(home))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := ledger.Find("omp:"+root, "steering"); ok {
			t.Error("the discarded resource kept its row")
		}
		if _, ok := ledger.FindTarget("omp", root); ok {
			t.Error("the discarded resource kept the enrollment")
		}
	})
}

type writerFunc func([]byte)

func (f writerFunc) Write(p []byte) (int, error) {
	f(p)
	return len(p), nil
}

// The printed plan is what --yes approved, so a resource that drifted after it
// was printed must stop the whole discard rather than vanish unnamed.
func TestUninstallDiscardRefusesUnprintedResource(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	steering := filepath.Join(root, "AGENTS.md")
	help := filepath.Join(root, "commands", "help.md")
	if err := os.MkdirAll(filepath.Dir(help), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(steering, []byte("user prose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(help, []byte("owned bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	helpDigest, err := managedfile.DigestResourceBytes([]byte("owned bytes\n"), managedfile.KindFile)
	if err != nil {
		t.Fatal(err)
	}
	key := "omp:" + root
	seedLedger(t, home,
		[]installstate.TargetRecord{{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"}},
		[]installstate.Row{
			{Target: key, Resource: "steering", Consumer: key, Applied: installstate.AppliedValue{Path: steering, Kind: managedfile.KindFile, Digest: "0000"}},
			{Target: key, Resource: "commands/help.md", Consumer: key, Applied: installstate.AppliedValue{Path: help, Kind: managedfile.KindFile, Digest: helpDigest}},
		})

	steps := testSteps(t, home)
	steps.DiscardChanged = true
	steps.Notice = writerFunc(func([]byte) {
		if err := os.WriteFile(help, []byte("user edit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	if _, err := steps.UninstallTarget(key); err == nil || !strings.Contains(err.Error(), "commands/help.md") {
		t.Fatalf("uninstall err = %v, want a refusal naming commands/help.md", err)
	}
	for path, want := range map[string]string{steering: "user prose\n", help: "user edit\n"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q untouched", path, got, err, want)
		}
	}
	ledger, err := installstate.LoadLedger(ledgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Rows) != 2 {
		t.Errorf("ledger rows = %+v, want both claims kept", ledger.Rows)
	}
}

// fileRow writes content at path and returns the ledger row for it; a changed
// row records a digest the bytes no longer match.
func fileRow(t *testing.T, key, id, path, content string, changed bool) installstate.Row {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	digest := "0000"
	if !changed {
		var err error
		if digest, err = managedfile.DigestResourceBytes([]byte(content), managedfile.KindFile); err != nil {
			t.Fatal(err)
		}
	}
	return installstate.Row{Target: key, Resource: id, Consumer: key, Applied: installstate.AppliedValue{Path: path, Kind: managedfile.KindFile, Digest: digest}}
}

// assertUntouched fails when any file's bytes or any ledger row changed.
func assertUntouched(t *testing.T, home string, files map[string]string, rows int) {
	t.Helper()
	for path, want := range files {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q untouched", path, got, err, want)
		}
	}
	ledger, err := installstate.LoadLedger(ledgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Rows) != rows {
		t.Errorf("ledger rows = %+v, want all %d claims kept", ledger.Rows, rows)
	}
}

// A full uninstall applies target by target, so drift in a later target must be
// caught before the first target loses anything.
func TestUninstallAllDiscardRefusesUnprintedResource(t *testing.T) {
	home := t.TempDir()
	rootA := filepath.Join(home, ".omp", "a")
	rootB := filepath.Join(home, ".omp", "b")
	keyA, keyB := "omp:"+rootA, "omp:"+rootB
	steeringA, steeringB := filepath.Join(rootA, "AGENTS.md"), filepath.Join(rootB, "AGENTS.md")
	help := filepath.Join(rootB, "commands", "help.md")
	seedLedger(t, home,
		[]installstate.TargetRecord{
			{Harness: "omp", Instance: rootA, NativeRoot: rootA, Status: "converged"},
			{Harness: "omp", Instance: rootB, NativeRoot: rootB, Status: "converged"},
		},
		[]installstate.Row{
			fileRow(t, keyA, "steering-a", steeringA, "prose a\n", true),
			fileRow(t, keyB, "steering-b", steeringB, "prose b\n", true),
			fileRow(t, keyB, "commands/help.md", help, "owned bytes\n", false),
		})

	steps := testSteps(t, home)
	steps.DiscardChanged = true
	steps.Notice = writerFunc(func([]byte) {
		if err := os.WriteFile(help, []byte("user edit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	if _, err := steps.UninstallAll(); err == nil || !strings.Contains(err.Error(), "commands/help.md") {
		t.Fatalf("uninstall --all err = %v, want a refusal naming commands/help.md", err)
	}
	assertUntouched(t, home, map[string]string{steeringA: "prose a\n", steeringB: "prose b\n", help: "user edit\n"}, 3)
}

// Two Claude roots record the same root-relative id. Removing the first root
// must not turn the second root's copy into a discard nobody printed.
func TestUninstallAllDiscardChangedAcrossClaudeRootsWithOneId(t *testing.T) {
	home := t.TempDir()
	rootA, rootB := filepath.Join(home, ".claude"), filepath.Join(home, ".claude-work")
	keyA, keyB := "claude:"+rootA, "claude:"+rootB
	agentA := filepath.Join(rootA, "agents", "atomic-example.md")
	agentB := filepath.Join(rootB, "agents", "atomic-example.md")
	seedLedger(t, home,
		[]installstate.TargetRecord{
			{Harness: "claude", Instance: rootA, NativeRoot: rootA, Status: "converged"},
			{Harness: "claude", Instance: rootB, NativeRoot: rootB, Status: "converged"},
		},
		[]installstate.Row{
			fileRow(t, keyA, "agents/atomic-example.md", agentA, "owned\n", false),
			fileRow(t, keyB, "agents/atomic-example.md", agentB, "user edit\n", true),
		})

	steps := testSteps(t, home)
	steps.DiscardChanged = true
	report, err := steps.UninstallAll()
	if err != nil {
		t.Fatalf("uninstall --all --discard-changed: %v", err)
	}
	if len(report.Targets) != 2 {
		t.Fatalf("report targets = %+v, want both roots uninstalled", report.Targets)
	}
	for _, path := range []string{agentA, agentB} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s survived the uninstall (stat err = %v)", path, err)
		}
	}
	stamps, err := config.BackupStamps(home)
	if err != nil || len(stamps) != 1 {
		t.Fatalf("backup sets = %v (%v), want one discard set", stamps, err)
	}
	unit, err := installstate.UnitFor(keyB)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(config.BackupStampDir(home, stamps[0]), unit, "agents.2fatomic-example.md")
	if got, err := os.ReadFile(copyPath); err != nil || string(got) != "user edit\n" {
		t.Errorf("recovery copy = %q, %v; want the changed copy kept", got, err)
	}
}

// A raw ledger key takes its own removal path, which must hold the same guard.
func TestUninstallRawKeyDiscardRefusesUnprintedResource(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".pi", "agent")
	key := "pi:" + root
	steering := filepath.Join(root, "AGENTS.md")
	help := filepath.Join(root, "commands", "help.md")
	seedLedger(t, home, nil, []installstate.Row{
		fileRow(t, key, "steering", steering, "user prose\n", true),
		fileRow(t, key, "commands/help.md", help, "owned bytes\n", false),
	})

	steps := testSteps(t, home)
	steps.DiscardChanged = true
	steps.Notice = writerFunc(func([]byte) {
		if err := os.WriteFile(help, []byte("user edit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	if _, err := steps.UninstallTarget(key); err == nil || !strings.Contains(err.Error(), "commands/help.md") {
		t.Fatalf("uninstall err = %v, want a refusal naming commands/help.md", err)
	}
	assertUntouched(t, home, map[string]string{steering: "user prose\n", help: "user edit\n"}, 2)
}

// A discard must never lose the user's bytes, so a recovery copy that cannot be
// written stops the discard.
func TestUninstallDiscardStopsWhenBackupFails(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	key := "omp:" + root
	steering := filepath.Join(root, "AGENTS.md")
	seedLedger(t, home,
		[]installstate.TargetRecord{{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"}},
		[]installstate.Row{fileRow(t, key, "steering", steering, "user prose\n", true)})
	backups := config.BackupDir(home)
	if err := os.MkdirAll(backups, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(backups, 0o755) })

	steps := testSteps(t, home)
	steps.DiscardChanged = true
	if _, err := steps.UninstallTarget(key); err == nil {
		t.Fatal("uninstall succeeded with no writable recovery directory")
	}
	assertUntouched(t, home, map[string]string{steering: "user prose\n"}, 1)
}

// A discard that fails partway has already moved earlier resources aside, so
// the error must say where their copies are.
func TestUninstallDiscardFailureNamesRecoveryDir(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	key := "omp:" + root
	skills := filepath.Join(root, "skills")
	tree := filepath.Join(skills, "b")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "SKILL.md"), []byte("user skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedLedger(t, home,
		[]installstate.TargetRecord{{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"}},
		[]installstate.Row{
			fileRow(t, key, "a", filepath.Join(root, "a.md"), "user prose\n", true),
			{Target: key, Resource: "skills/b", Consumer: key, Applied: installstate.AppliedValue{Path: tree, Kind: managedfile.KindTree, Digest: "0000"}},
		})
	if err := os.Chmod(skills, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(skills, 0o755) })

	steps := testSteps(t, home)
	steps.DiscardChanged = true
	_, err := steps.UninstallTarget(key)
	if err == nil || !strings.Contains(err.Error(), "recovery copies of the resources already discarded are under "+config.BackupDir(home)) {
		t.Fatalf("uninstall err = %v, want the recovery directory named", err)
	}
}

// A dry run is how a scripted caller reads the plan --yes will approve.
func TestUninstallDryRunNamesDiscards(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	path := filepath.Join(root, "AGENTS.md")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user prose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := "omp:" + root
	seedLedger(t, home,
		[]installstate.TargetRecord{{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"}},
		[]installstate.Row{{Target: key, Resource: "steering", Consumer: key, Applied: installstate.AppliedValue{Path: path, Kind: managedfile.KindFile, Digest: "0000"}}})

	steps := testSteps(t, home)
	steps.DiscardChanged = true
	steps.DryRun = true
	plan, err := steps.UninstallTarget(key)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	want := "discards steering at " + path + " (changed after Atomic wrote it); a recovery copy is kept under ~/.atomic/backups/"
	if !slices.Contains(plan.Notices, want) {
		t.Errorf("notices = %v, want %q", plan.Notices, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("a dry run touched the resource: %v", err)
	}
}

// A discarded resource holds the user's edits, so its bytes must survive under
// ~/.atomic/backups, which a full uninstall preserves.
func TestUninstallDiscardKeepsRecoveryCopy(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	file := filepath.Join(root, "AGENTS.md")
	tree := filepath.Join(root, "skills", "atomic-tdd")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("user prose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "SKILL.md"), []byte("user skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	block := filepath.Join(root, "BLOCK.md")
	blockBytes := "user prose\n<atomic>\nedited\n</atomic>\n"
	if err := os.WriteFile(block, []byte(blockBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.InstallInDir(root); err != nil {
		t.Fatalf("register session-start hook: %v", err)
	}
	settings := filepath.Join(root, "settings.json")
	settingsBytes, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	key := "omp:" + root
	seedLedger(t, home,
		[]installstate.TargetRecord{{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"}},
		[]installstate.Row{
			{Target: key, Resource: "steering", Consumer: key, Applied: installstate.AppliedValue{Path: file, Kind: managedfile.KindFile, Digest: "0000"}},
			{Target: key, Resource: "skills/atomic-tdd", Consumer: key, Applied: installstate.AppliedValue{Path: tree, Kind: managedfile.KindTree, Digest: "0000"}},
			{Target: key, Resource: "block", Consumer: key, Applied: installstate.AppliedValue{Path: block, Kind: managedfile.KindBlock, Digest: "0000"}},
			{Target: key, Resource: "settings.json", Consumer: key, Applied: installstate.AppliedValue{Path: settings, Kind: managedfile.KindSettings, Digest: "0000"}},
		})

	steps := testSteps(t, home)
	steps.DiscardChanged = true
	removal, err := steps.UninstallTarget(key)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(removal.Discarded) != 4 {
		t.Fatalf("discarded = %v, want all four resources", removal.Discarded)
	}
	stamps, err := config.BackupStamps(home)
	if err != nil || len(stamps) != 1 || !strings.HasPrefix(stamps[0], "discard-") {
		t.Fatalf("backup sets = %v (%v), want one discard- set", stamps, err)
	}
	unit, err := installstate.UnitFor(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(config.BackupStampDir(home, stamps[0]), unit)
	for rel, want := range map[string]string{
		"steering": "user prose\n",
		filepath.Join("skills.2fatomic-tdd", "SKILL.md"): "user skill\n",
		"block":         blockBytes,
		"settings.json": string(settingsBytes),
	} {
		if got, err := os.ReadFile(filepath.Join(dir, rel)); err != nil || string(got) != want {
			t.Errorf("recovery copy %s = %q, %v; want %q", rel, got, err, want)
		}
	}
	if !slices.ContainsFunc(removal.Notices, func(n string) bool { return strings.Contains(n, dir) }) {
		t.Errorf("notices = %v, want the recovery directory named", removal.Notices)
	}
}

// TestUninstallPrunesEmptiedExtensionDirectory proves the extension directory
// goes once empty and survives while the user keeps anything in it.
func TestUninstallPrunesEmptiedExtensionDirectory(t *testing.T) {
	newFixture := func(t *testing.T) (home, root, ext string) {
		t.Helper()
		home = t.TempDir()
		root = filepath.Join(home, ".omp", "agent")
		ext = filepath.Join(root, "extensions", "atomic.ts")
		if err := os.MkdirAll(filepath.Dir(ext), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ext, []byte("module\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		digest, err := managedfile.DigestResourceBytes([]byte("module\n"), managedfile.KindFile)
		if err != nil {
			t.Fatal(err)
		}
		seedLedger(t, home,
			[]installstate.TargetRecord{{Harness: "omp", Instance: root, NativeRoot: root, Status: "converged"}},
			[]installstate.Row{{Target: "omp:" + root, Resource: ext, Consumer: "omp:" + root,
				Applied: installstate.AppliedValue{Path: ext, Kind: managedfile.KindFile, Digest: digest}}})
		return home, root, ext
	}

	t.Run("empty container is removed", func(t *testing.T) {
		home, root, ext := newFixture(t)
		removal, err := testSteps(t, home).UninstallTarget("omp:" + root)
		if err != nil {
			t.Fatalf("uninstall: %v", err)
		}
		if _, err := os.Stat(ext); !os.IsNotExist(err) {
			t.Fatalf("extension survived (stat err = %v)", err)
		}
		if _, err := os.Stat(filepath.Dir(ext)); !os.IsNotExist(err) {
			t.Errorf("emptied extensions directory survived (stat err = %v)", err)
		}
		if len(removal.Pruned) != 1 {
			t.Errorf("pruned = %v, want the emptied directory reported", removal.Pruned)
		}
	})

	t.Run("occupied container survives", func(t *testing.T) {
		home, root, ext := newFixture(t)
		other := filepath.Join(filepath.Dir(ext), "user.ts")
		if err := os.WriteFile(other, []byte("user module\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := testSteps(t, home).UninstallTarget("omp:" + root); err != nil {
			t.Fatalf("uninstall: %v", err)
		}
		if _, err := os.Stat(filepath.Dir(ext)); err != nil {
			t.Errorf("extensions directory holding a user file was removed: %v", err)
		}
		if _, err := os.Stat(other); err != nil {
			t.Errorf("the user's file was removed: %v", err)
		}
	})
}

// TestUninstallRawKeyClearsStaleRow proves a row whose target key does not parse
// is clearable by that key, its only identity.
func TestUninstallRawKeyClearsStaleRow(t *testing.T) {
	home := t.TempDir()
	missing := filepath.Join(home, ".omp", "agent", "extensions", "atomic.ts")
	seedLedger(t, home, nil,
		[]installstate.Row{{Target: ":stale", Resource: missing, Consumer: ":stale",
			Applied: installstate.AppliedValue{Path: missing, Kind: managedfile.KindFile, Digest: "0000"}}})

	removal, err := testSteps(t, home).UninstallTarget(":stale")
	if err != nil {
		t.Fatalf("raw-key uninstall: %v", err)
	}
	if len(removal.Removed) != 1 || removal.Removed[0] != missing {
		t.Fatalf("removed = %v, want the stale row's resource", removal.Removed)
	}
	ledger, err := installstate.LoadLedger(ledgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Find(":stale", missing); ok {
		t.Error("the raw-key removal left the stale row behind")
	}
}

// TestFullUninstallKeepsClaimWhenSettingsAreReadOnly proves a read-only settings
// file keeps its claim, so a later uninstall can still clear it.
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
// keeps user data and anything an unresolved journal still references.
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

	// Native bytes matching neither the intent nor the backup leave this journal
	// conflicted and unresolved.
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

// writeConflictingJournal records a journal whose native bytes match neither the
// intent nor the backup, so recovery cannot reach one safe result.
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

// TestUninstallDryRunLeavesFilesystemIdentical proves a dry run writes nothing,
// so the real run afterwards still has the journal to recover.
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
	if _, err := os.Stat(config.JournalPath(home, operationID)); !os.IsNotExist(err) {
		t.Errorf("the real run left the consumed journal behind (stat err = %v)", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("real uninstall left the owned resource (stat err = %v)", err)
	}
}

// TestStatusSurfacesProjectionFailure guards against an unprojectable target
// reading as current, the one verdict status must never invent.
func TestStatusSurfacesProjectionFailure(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	adapter := &fakeAdapter{
		kind:      harness.KindOMP,
		instances: []harness.Instance{{Kind: harness.KindOMP, ID: root, NativeRoot: root, Home: home, Exists: true}},
		files: map[string]fakeFile{
			"steering": {path: filepath.Join(root, "AGENTS.md"), content: []byte("<atomic>\ncontract\n</atomic>\n"), kind: managedfile.KindBlock},
		},
	}
	steps := testSteps(t, home, adapter)
	if _, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindOMP}, Enroll: true}); err != nil {
		t.Fatalf("converge: %v", err)
	}

	adapter.projectErr = errors.New("CODEX_HOME is unset")
	report, err := steps.Status(Selection{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(report.Targets) != 1 {
		t.Fatalf("targets = %+v, want the one enrolled target", report.Targets)
	}
	if blockers := strings.Join(report.Targets[0].Blockers, "; "); !strings.Contains(blockers, "CODEX_HOME is unset") {
		t.Errorf("target blockers = %q, want the projection failure", blockers)
	}
}

// A target with a conflicting resource must not read as converged: the recorded
// target status predates the edit that made the row conflict.
func TestStatusMovesTargetOffConvergedOnResourceConflict(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	steering := filepath.Join(root, "AGENTS.md")
	adapter := &fakeAdapter{
		kind:      harness.KindOMP,
		instances: []harness.Instance{{Kind: harness.KindOMP, ID: root, NativeRoot: root, Home: home, Exists: true}},
		files: map[string]fakeFile{
			"steering": {path: steering, content: []byte("<atomic>\ncontract\n</atomic>\n"), kind: managedfile.KindBlock},
		},
	}
	steps := testSteps(t, home, adapter)
	if _, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindOMP}, Enroll: true}); err != nil {
		t.Fatalf("converge: %v", err)
	}
	if err := os.WriteFile(steering, []byte("<atomic>\nuser edit\n</atomic>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := steps.Status(Selection{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(report.Targets) != 1 {
		t.Fatalf("targets = %+v, want the one enrolled target", report.Targets)
	}
	got := report.Targets[0]
	if len(got.Resources) == 0 || got.Resources[0].State != harness.RowConflict {
		t.Fatalf("resources = %+v, want a conflict row", got.Resources)
	}
	if got.Status != harness.StatusConflicted {
		t.Errorf("target status = %q, want %q", got.Status, harness.StatusConflicted)
	}
}

// A plan that reports blockers means the target cannot converge, so it must not
// read as converged either.
func TestStatusMovesTargetOffConvergedOnPlanBlockers(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	adapter := &fakeAdapter{
		kind:      harness.KindOMP,
		instances: []harness.Instance{{Kind: harness.KindOMP, ID: root, NativeRoot: root, Home: home, Exists: true}},
		files: map[string]fakeFile{
			"steering": {path: filepath.Join(root, "AGENTS.md"), content: []byte("<atomic>\ncontract\n</atomic>\n"), kind: managedfile.KindBlock},
		},
	}
	steps := testSteps(t, home, adapter)
	if _, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindOMP}, Enroll: true}); err != nil {
		t.Fatalf("converge: %v", err)
	}

	adapter.blockers = []string{"ambiguous block"}
	report, err := steps.Status(Selection{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := report.Targets[0].Status; got != harness.StatusConflicted {
		t.Errorf("target status = %q with plan blockers, want %q", got, harness.StatusConflicted)
	}

	adapter.blockers = nil
	adapter.projectErr = errors.New("CODEX_HOME is unset")
	report, err = steps.Status(Selection{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := report.Targets[0].Status; got != harness.StatusConflicted {
		t.Errorf("target status = %q when projection failed, want %q", got, harness.StatusConflicted)
	}
}

// TestUninstallAllDryRunReportsRecoveryPrerequisites proves a dry run reports
// the journal it would reconcile without recovering it.
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

// writeInterruptedInstall records an install interrupted between native write
// and ledger commit; withRecord picks which side of enrollment it stopped on.
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

// TestUninstallDryRunPlansAgainstSimulatedRecovery guards against a dry run of an
// interrupted install reporting a false ErrNotEnrolled or an incomplete plan.
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

// TestUninstallDryRunReportsBlockedOnRecovery proves an unreconcilable journal
// blocks the dry run instead of yielding a removal plan.
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

// claudeSteps wires the real Claude adapter against a fixed native root, without
// reading the process environment.
func claudeSteps(t *testing.T, home, root string) Steps {
	t.Helper()
	adapter := claude.New()
	adapter.ConfigDir = func(string) string { return root }
	return testSteps(t, home, adapter)
}

// TestConvergeEnrolledOverAChangedGeneration guards against Atomic's own earlier
// bytes reading as unowned, which blocked every update on a replace decision.
func TestConvergeEnrolledOverAChangedGeneration(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	steps := claudeSteps(t, home, root)

	reports, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindClaude}, Enroll: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Blockers) != 0 {
		t.Fatalf("install reports = %+v, want one unblocked target", reports)
	}

	// Simulate generation G1: every whole-file artifact holds older bytes, and
	// the ledger records those bytes as Atomic's own applied value.
	ledger, err := installstate.LoadLedger(ledgerPath(home))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	older := map[string][]byte{}
	for i := range ledger.Rows {
		row := &ledger.Rows[i]
		if row.Applied.Kind != managedfile.KindFile {
			continue
		}
		data := []byte("generation-one bytes for " + row.Resource + "\n")
		if err := os.WriteFile(row.Applied.Path, data, 0o644); err != nil {
			t.Fatalf("write older generation: %v", err)
		}
		row.Applied.Digest = managedfile.Digest(data)
		older[row.Applied.Path] = data
	}
	if len(older) == 0 {
		t.Fatal("no whole-file ledger rows to age; the fixture is empty")
	}
	if err := ledger.Save(ledgerPath(home)); err != nil {
		t.Fatalf("save ledger: %v", err)
	}

	// G2: the selected corpus differs from the G1 bytes on disk.
	reports, err = steps.ConvergeEnrolled()
	if err != nil {
		t.Fatalf("converge enrolled: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %+v, want the one enrolled target", reports)
	}
	if len(reports[0].Blockers) != 0 {
		t.Fatalf("a changed generation blocked convergence: %v", reports[0].Blockers)
	}
	if reports[0].Status != harness.StatusConverged || !reports[0].Applied {
		t.Fatalf("report = %+v, want an applied convergence", reports[0])
	}
	for path, g1 := range older {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(got) == string(g1) {
			t.Errorf("%s still holds the older generation's bytes", path)
		}
	}
}

// TestInstallIntoPrePopulatedClaudeHome guards against a user's own CLAUDE.md
// with no Atomic block reading as an ambiguous block.
func TestInstallIntoPrePopulatedClaudeHome(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	prose := "# My own guidance\n\nKeep it.\n"
	steering := filepath.Join(root, "CLAUDE.md")
	if err := os.WriteFile(steering, []byte(prose), 0o644); err != nil {
		t.Fatal(err)
	}
	steps := claudeSteps(t, home, root)

	reports, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindClaude}, Enroll: true})
	if err != nil {
		t.Fatalf("install over a pre-populated home: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Blockers) != 0 {
		t.Fatalf("install reports = %+v, want one unblocked target", reports)
	}
	got, err := os.ReadFile(steering)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), prose) {
		t.Errorf("install rewrote the user's prose:\n%s", got)
	}
	if !strings.Contains(string(got), managedfile.BlockOpen) {
		t.Errorf("install did not append an Atomic block:\n%s", got)
	}
}

// TestInstallIntoPrePopulatedOMPProfile is the OMP half of the pre-populated
// regression.
func TestInstallIntoPrePopulatedOMPProfile(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	prose := "# Profile guidance\n\nMine.\n"
	steering := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(steering, []byte(prose), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter := omp.New()
	adapter.ConfigPath = func(home, profile string) (string, error) { return root, nil }
	steps := testSteps(t, home, adapter)

	reports, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindOMP}, Enroll: true})
	if err != nil {
		t.Fatalf("install over a pre-populated profile: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Blockers) != 0 {
		t.Fatalf("install reports = %+v, want one unblocked target", reports)
	}
	got, err := os.ReadFile(steering)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), prose) {
		t.Errorf("install rewrote the profile's prose:\n%s", got)
	}
	if !strings.Contains(string(got), managedfile.BlockOpen) {
		t.Errorf("install did not append an Atomic block:\n%s", got)
	}
}

// TestInstallRefusesMalformedSteeringBlock proves unparseable Atomic tags still
// refuse rather than guess a boundary.
func TestInstallRefusesMalformedSteeringBlock(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	malformed := "prose\n" + managedfile.BlockOpen + "\nfirst\n" + managedfile.BlockClose + "\n" + managedfile.BlockOpen + "\nsecond\n" + managedfile.BlockClose + "\n"
	steering := filepath.Join(root, "CLAUDE.md")
	if err := os.WriteFile(steering, []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}
	steps := claudeSteps(t, home, root)

	reports, err := steps.Converge(ConvergeRequest{Selection: Selection{Kind: harness.KindClaude}, Enroll: true})
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Blockers) == 0 {
		t.Fatalf("reports = %+v, want a malformed-block refusal", reports)
	}
	if got, err := os.ReadFile(steering); err != nil || string(got) != malformed {
		t.Errorf("a malformed block was rewritten: %q, %v", got, err)
	}
}

// A Claude settings.json whose hooks a writer would have to coerce blocks that
// target only; the run still converges the next target.
func TestBadClaudeSettingsShapeBlocksOnlyClaude(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"hooks":[]}`
	settings := hooks.SettingsPathInDir(root)
	if err := os.WriteFile(settings, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	claudeAdapter := claude.New()
	claudeAdapter.ConfigDir = func(string) string { return root }
	ompRoot := filepath.Join(home, ".omp", "agent")
	other := &fakeAdapter{
		kind:      harness.KindOMP,
		instances: []harness.Instance{{Kind: harness.KindOMP, ID: ompRoot, NativeRoot: ompRoot, Home: home, Exists: true}},
		files:     map[string]fakeFile{"steering": {path: filepath.Join(ompRoot, "AGENTS.md"), content: []byte("<atomic>\nx\n</atomic>\n"), kind: managedfile.KindBlock}},
	}
	steps := testSteps(t, home, claudeAdapter, other)

	reports, err := steps.Converge(ConvergeRequest{Selection: Selection{All: true}, Enroll: true})
	if err != nil {
		t.Fatalf("converge aborted on one target's settings shape: %v", err)
	}
	var claudeReport, ompReport *ConvergeReport
	for i := range reports {
		switch reports[i].Target.Kind {
		case harness.KindClaude:
			claudeReport = &reports[i]
		case harness.KindOMP:
			ompReport = &reports[i]
		}
	}
	if claudeReport == nil || !strings.Contains(strings.Join(claudeReport.Blockers, "\n"), `"hooks" must be an object; found array`) {
		t.Errorf("claude report = %+v, want a shape blocker", claudeReport)
	}
	if ompReport == nil || ompReport.Status != harness.StatusConverged {
		t.Errorf("omp report = %+v, want converged", ompReport)
	}
	if got, _ := os.ReadFile(settings); string(got) != body {
		t.Errorf("settings.json changed to %s", got)
	}
}

// An enrolled --instance names a native root, so a trailing slash must select the
// same target rather than silently matching nothing.
func TestSelectionMatchesCleansInstancePaths(t *testing.T) {
	target := harness.Target{Kind: harness.KindClaude, Instance: "/home/user/.claude"}
	for _, instance := range []string{"/home/user/.claude", "/home/user/.claude/", "/home/user/./.claude"} {
		sel := Selection{Kind: harness.KindClaude, Instance: instance}
		if !sel.matches(target) {
			t.Errorf("selection %q did not match %q", instance, target.Instance)
		}
	}
	if (Selection{Kind: harness.KindClaude, Instance: "/home/user/.claude-work"}).matches(target) {
		t.Error("a different root matched")
	}
}
