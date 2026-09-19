// CP8B mixed-home scenarios: the three-target proof over one isolated home
// where Claude, OMP, and Codex are all enrolled through the production install
// engine.
//
// The mixed home is where the shared-concern claims live: `--all` enrolls every
// discovered target, each physical resource keeps one ownership record with its
// consumers separate from mere visibility, update/uninstall/doctor --fix
// serialize on the one advisory lifecycle lock, an unresolved journal is
// recovered before any mutation, every unenrolled target refuses repair, and
// doctor categories 15-24 report each harness independently.
package runtimeproof

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// mixedHome prepares one isolated home where all three harnesses are
// discoverable: the Claude configuration root is home-derived, OMP resolves its
// default profile under the isolated home, and CODEX_HOME points at an isolated
// root. It returns the home and the Codex native root.
func mixedHome(t *testing.T) (home, codexRoot string) {
	t.Helper()
	home = isolatedHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv(omp.ProfileEnv, "")
	codexRoot = codexHomeRoot(t, home)
	return home, codexRoot
}

// mixedTargetKeys returns the three enrolled target keys a mixed home carries.
func mixedTargetKeys(home, codexRoot string) map[harness.Kind]string {
	return map[harness.Kind]string{
		harness.KindClaude: (harness.Target{Kind: harness.KindClaude, Instance: filepath.Join(home, ".claude")}).Key(),
		harness.KindOMP:    (harness.Target{Kind: harness.KindOMP, Instance: filepath.Join(home, filepath.FromSlash(omp.DefaultAgentDir))}).Key(),
		harness.KindCodex:  (harness.Target{Kind: harness.KindCodex, Instance: codexRoot}).Key(),
	}
}

// enrollMixed enrolls all three discovered targets through the production
// engine and returns the reports.
func enrollMixed(t *testing.T, home string) []install.ConvergeReport {
	t.Helper()
	reports, err := scenarioSteps(home).Converge(install.ConvergeRequest{Selection: install.Selection{All: true}, Enroll: true})
	if err != nil {
		t.Fatalf("install --all: %v", err)
	}
	return reports
}

// TestCP8BMixedInstallAllEnrollsThree proves `--all` enrolls every discovered
// target across the three harnesses and each one's native output lands, with no
// harness's artifacts written into another's root.
//
// Criterion: install operates only on explicitly enrolled target instances;
// `--all` enrolls each discovered harness instance exactly once.
func TestCP8BMixedInstallAllEnrollsThree(t *testing.T) {
	home, codexRoot := mixedHome(t)
	steps := scenarioSteps(home)

	ev := ScenarioEvidence{
		Scenario:  "cp8b/mixed/install-all-enrolls-three",
		Group:     "mixed",
		Engine:    "install (all three adapters)",
		Criterion: "install --all enrolls each discovered Claude, OMP, and Codex instance exactly once",
		Command:   "install.Steps.Converge (--all, enroll)",
		Paths:     []string{home, codexRoot},
	}

	reports := enrollMixed(t, home)
	if len(reports) != 3 {
		t.Fatalf("reports = %+v, want three targets", reports)
	}
	seen := map[harness.Kind]bool{}
	for _, report := range reports {
		seen[report.Target.Kind] = true
		if report.Status != harness.StatusConverged || len(report.Blockers) > 0 {
			t.Errorf("target %s = %s (%v), want converged with no blockers", report.Target.Key(), report.Status, report.Blockers)
		}
	}
	for _, kind := range []harness.Kind{harness.KindClaude, harness.KindOMP, harness.KindCodex} {
		if !seen[kind] {
			t.Errorf("--all did not enroll the %s target", kind)
		}
	}

	keys := mixedTargetKeys(home, codexRoot)
	for kind, key := range keys {
		target, err := harness.ParseKey(key)
		if err != nil {
			t.Fatalf("parse key %s: %v", key, err)
		}
		if !enrolledTarget(t, home, kind, target.Instance) {
			t.Errorf("the %s target %s was not enrolled", kind, key)
		}
	}
	if !fileExists(filepath.Join(home, ".claude", "commands", "commit.md")) {
		t.Errorf("the Claude artifact tree was not projected")
	}
	if !fileExists(filepath.Join(config.PackageRoot(home, "omp"), "extensions", "atomic.ts")) {
		t.Errorf("the OMP shared package was not published")
	}
	if !fileExists(filepath.Join(codex.PackageResource(home), filepath.FromSlash(codex.MarketplaceManifestPath))) {
		t.Errorf("the Codex plugin package was not published")
	}
	// No harness's global contract leaked into another harness's root.
	for _, foreign := range []string{
		filepath.Join(codexRoot, "CLAUDE.md"),
		filepath.Join(codexRoot, "AGENTS.md"),
		filepath.Join(home, ".claude", "config.toml"),
	} {
		if fileExists(foreign) {
			t.Errorf("a mixed install wrote %s", foreign)
		}
	}
	if steps.DryRun {
		t.Errorf("the scenario mutates; DryRun must be false")
	}

	ev.Outcome = "three targets enrolled and converged; each harness's native output published in its own root"
	recordScenario(t, ev)
}

// TestCP8BMixedSharedOwnership proves the mixed home keeps one ownership record
// per physical resource: each enrolled target is a consumer of its own
// resources, no record mixes harness kinds, and an unenrolled instance that can
// see a shared package is reported as visibility only.
//
// Criterion: one physical resource has one ownership record even when several
// targets can discover it; consumers and mere visibility stay separate.
func TestCP8BMixedSharedOwnership(t *testing.T) {
	home, codexRoot := mixedHome(t)
	enrollMixed(t, home)
	steps := scenarioSteps(home)

	ev := ScenarioEvidence{
		Scenario:  "cp8b/mixed/shared-ownership",
		Group:     "mixed",
		Engine:    "install + harness",
		Criterion: "one ownership record per physical resource; three enrolled consumers kept separate from unenrolled visibility",
		Command:   "Converge (--all, enroll) → install.Steps.Status + harness.Resources",
		Paths:     []string{home, codexRoot},
	}

	report, err := steps.Status(install.Selection{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	consumers := map[string]bool{}
	for _, resource := range report.Shared {
		kinds := map[harness.Kind]bool{}
		for _, consumer := range resource.Consumers {
			consumers[consumer] = true
			target, err := harness.ParseKey(consumer)
			if err != nil {
				t.Fatalf("parse consumer %s: %v", consumer, err)
			}
			kinds[target.Kind] = true
		}
		if len(kinds) > 1 {
			t.Errorf("resource %s mixes harness kinds %v", resource.ID, kinds)
		}
	}
	if len(consumers) != 3 {
		t.Errorf("distinct consumers = %v, want the three enrolled target keys", consumers)
	}
	keys := mixedTargetKeys(home, codexRoot)
	for _, key := range keys {
		if !consumers[key] {
			t.Errorf("the resource report omits consumer %s", key)
		}
	}

	// Visibility is separate: an unenrolled Codex home that can discover the
	// shared package is reported visible-to, never a consumer.
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	unenrolled := filepath.Join(home, "codex-unenrolled")
	if err := os.MkdirAll(unenrolled, 0o755); err != nil {
		t.Fatal(err)
	}
	var merged []harness.Instance
	for kind, key := range keys {
		target, _ := harness.ParseKey(key)
		merged = append(merged, harness.Instance{Kind: kind, ID: target.Instance, NativeRoot: target.Instance, Home: home, Exists: true})
	}
	merged = append(merged, harness.Instance{Kind: harness.KindCodex, ID: unenrolled, NativeRoot: unenrolled, Home: home, Exists: true})
	resources := harness.Resources(ledger, merged, harness.SharedRoots(home))
	var pkg *harness.Resource
	for i := range resources {
		if resources[i].ID == codex.PackageResource(home) {
			pkg = &resources[i]
		}
	}
	if pkg == nil {
		t.Fatalf("no Codex package record in %+v", resources)
	}
	unenrolledKey := (harness.Target{Kind: harness.KindCodex, Instance: unenrolled}).Key()
	if !contains(pkg.VisibleTo, unenrolledKey) {
		t.Errorf("Codex package visible-to = %v, want the unenrolled home", pkg.VisibleTo)
	}
	if contains(pkg.Consumers, unenrolledKey) {
		t.Errorf("an unenrolled home was counted as a consumer")
	}

	ev.Outcome = "three distinct consumers across one ownership record per resource; an unenrolled Codex home is visibility only"
	recordScenario(t, ev)
}

// TestCP8BMixedConcurrentLifecycle proves update convergence, uninstall, and
// `doctor --fix` each serialize on the one advisory lifecycle lock across a
// three-target home: every operation blocks while the lock is held and
// completes once it is released.
//
// Criterion: install, update, repair, and uninstall acquire one global advisory
// lifecycle lock; a ledger-managed doctor --fix acquires it too.
func TestCP8BMixedConcurrentLifecycle(t *testing.T) {
	home, codexRoot := mixedHome(t)

	ev := ScenarioEvidence{
		Scenario:  "cp8b/mixed/concurrent-lifecycle",
		Group:     "mixed",
		Engine:    "install + doctor + installstate",
		Criterion: "update convergence, uninstall, and doctor --fix serialize on the one advisory lifecycle lock over three targets",
		Command:   "hold AcquireLock → run each operation → release",
		Paths:     []string{home, codexRoot},
	}

	enrollMixed(t, home)

	blockThenRunMixed(t, "update convergence", home, func() error {
		_, err := scenarioSteps(home).ConvergeEnrolled()
		return err
	})
	blockThenRunMixed(t, "uninstall", home, func() error {
		_, err := scenarioSteps(home).UninstallAll()
		return err
	})

	// doctor --fix on the mixed home: rebuild the enrolled state, then hold the
	// lock around the ledger-managed repair. The production repairer resolves
	// the home from HOME, so the isolated home must be the process home too.
	enrollMixed(t, home)
	t.Setenv("HOME", home)
	results := []doctor.Result{{
		Index:    21,
		Name:     "staleness",
		Severity: doctor.WARN,
		Detail:   "materialized generation is behind the selected projection",
	}}
	var summary doctor.RepairSummary
	blockThenRunMixed(t, "doctor fix", home, func() error {
		summary = doctor.Repair(results, doctor.Opts{Home: home}, yesPrompter{}, io.Discard)
		return nil
	})
	if summary.Applied != 1 {
		t.Errorf("doctor fix summary = %+v, want one applied repair after the lock was released", summary)
	}

	ev.Outcome = "update convergence, uninstall, and doctor --fix each blocked while the lock was held and completed after release"
	recordScenario(t, ev)
}

// blockThenRunMixed holds the home's lifecycle lock, starts op, asserts it has
// not completed while the lock is held, releases, and asserts op finishes.
func blockThenRunMixed(t *testing.T, label, home string, op func() error) {
	t.Helper()
	lock, err := installstate.AcquireLock(home, installstate.WriterIdentity{OperationID: "cp8b-mixed-concurrent"})
	if err != nil {
		t.Fatalf("%s: acquire lock: %v", label, err)
	}
	done := make(chan error, 1)
	go func() { done <- op() }()
	select {
	case err := <-done:
		_ = lock.Release()
		t.Fatalf("%s: operation completed while the lifecycle lock was held (err=%v)", label, err)
	case <-time.After(250 * time.Millisecond):
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("%s: release lock: %v", label, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: operation after lock release: %v", label, err)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("%s: operation did not proceed after the lock was released", label)
	}
}

// TestCP8BMixedInterruptionRecovery proves an unresolved journal over a
// three-target home is reconciled before any enrolled convergence mutates
// state, with the completed journal left as operational history.
//
// Criterion: a lifecycle operation recovers unresolved journals oldest-first
// before planning any mutation.
func TestCP8BMixedInterruptionRecovery(t *testing.T) {
	home, codexRoot := mixedHome(t)
	enrollMixed(t, home)

	ev := ScenarioEvidence{
		Scenario:  "cp8b/mixed/interruption-recovery",
		Group:     "mixed",
		Engine:    "install + installstate",
		Criterion: "an unresolved journal is recovered before the enrolled targets reconverge",
		Command:   "Converge (--all, enroll) → applied-unrecorded journal → ConvergeEnrolled",
		Paths:     []string{home, codexRoot},
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	var row *installstate.Row
	for i := range ledger.Rows {
		if ledger.Rows[i].Applied.Kind == managedfile.KindFile && fileExists(ledger.Rows[i].Applied.Path) {
			row = &ledger.Rows[i]
			break
		}
	}
	if row == nil {
		t.Fatalf("no file ownership row to build the interrupted operation from")
	}
	data := mustRead(t, row.Applied.Path)
	m := cp8aMutation(t, "cp8b-mixed-recovery", row.Applied.Path, data)
	m.Target = row.Target
	journalPath := cp8aWriteJournal(t, home, cp8aJournal("cp8b-mixed-recovery", []installstate.Mutation{m},
		[]installstate.Progress{{Unit: m.Unit, State: installstate.StateApplied, At: cp8aFixedTime()}}))

	reports, err := scenarioSteps(home).ConvergeEnrolled()
	if err != nil {
		t.Fatalf("converge enrolled: %v", err)
	}
	if len(reports) != 3 {
		t.Fatalf("reports = %+v, want the three enrolled targets converged", reports)
	}
	if j, err := installstate.LoadJournal(journalPath); err == nil {
		if !j.Completed {
			t.Errorf("the unresolved journal was neither removed nor marked completed")
		}
	}

	ev.Outcome = "applied-unrecorded journal recovered and marked completed while all three targets reconverged"
	recordScenario(t, ev)
}

// TestCP8BMixedUnenrolledRepairRefusals proves every harness's repair refuses an
// unenrolled target and is never an enrollment path: on a home where nothing is
// enrolled, each kind's convergence refuses and the ledger stays empty.
//
// Criterion: repair operates only on explicitly enrolled target instances;
// discovery never enrolls.
func TestCP8BMixedUnenrolledRepairRefusals(t *testing.T) {
	home, codexRoot := mixedHome(t)
	steps := scenarioSteps(home)

	ev := ScenarioEvidence{
		Scenario:  "cp8b/mixed/unenrolled-repair-refusals",
		Group:     "mixed",
		Engine:    "install",
		Criterion: "each harness's repair refuses an unenrolled target without enrolling it",
		Command:   "Converge (kind, repair, unenrolled) for claude|omp|codex",
		Paths:     []string{home, codexRoot},
	}

	for _, kind := range []harness.Kind{harness.KindClaude, harness.KindOMP, harness.KindCodex} {
		if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: kind}}); err == nil {
			t.Errorf("repair of an unenrolled %s target succeeded", kind)
		}
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Targets) != 0 {
		t.Errorf("a refused repair enrolled targets: %+v", ledger.Targets)
	}

	ev.Outcome = "all three repair attempts refused; the ledger recorded no enrollment"
	recordScenario(t, ev)
}

// TestCP8BMixedDoctorCategories proves doctor categories 15-24 each report the
// mixed home independently: every category returns a result, none fails, the
// target/capability/rule/trust rows name each harness, and the Codex category
// reports its home.
//
// Criterion: doctor reports target instances, shared resources, unfinished
// journals, capability gaps, staleness, disablement, conflicts, shadowing, and
// Codex surfaces independently; multi-harness categories append with stable
// names and indices.
func TestCP8BMixedDoctorCategories(t *testing.T) {
	home, codexRoot := mixedHome(t)
	enrollMixed(t, home)

	ev := ScenarioEvidence{
		Scenario:  "cp8b/mixed/doctor-categories-15-24",
		Group:     "mixed",
		Engine:    "doctor",
		Criterion: "categories 15-24 each report the mixed home; no category fails; multi-harness rows name each harness",
		Command:   "doctor.RunWith(--only 15..24)",
		Paths:     []string{home, codexRoot},
	}

	only := []int{15, 16, 17, 18, 19, 20, 21, 22, 23, 24}
	results, err := doctor.RunWith(doctor.Opts{
		Home:     home,
		Only:     only,
		RepoRoot: isolatedHome(t),
	}, false)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if len(results) != len(only) {
		t.Fatalf("doctor returned %d categories, want %d", len(results), len(only))
	}
	for i, result := range results {
		if result.Index != only[i] {
			t.Errorf("category %d = index %d, want %d", i, result.Index, only[i])
		}
		if result.Detail == "" {
			t.Errorf("category %d (%s) reported no detail", result.Index, result.Name)
		}
		if result.Severity == doctor.FAIL {
			t.Errorf("category %d (%s) failed: %s", result.Index, result.Name, result.Detail)
		}
	}

	byIndex := map[int]doctor.Result{}
	for _, result := range results {
		byIndex[result.Index] = result
	}
	targets := byIndex[15]
	if !strings.Contains(targets.Detail, "3 enrolled target(s)") && !strings.Contains(strings.Join(targets.Findings, "\n"), "3 enrolled") {
		t.Errorf("targets category = %+v, want three enrolled targets reported", targets)
	}
	for _, kind := range []string{"claude", "omp", "codex"} {
		if !containsSubstring(byIndex[18].Findings, kind) {
			t.Errorf("capabilities category findings omit the %s harness", kind)
		}
		if !containsSubstring(byIndex[20].Findings, kind) {
			t.Errorf("trust category findings omit the %s harness", kind)
		}
	}
	keys := mixedTargetKeys(home, codexRoot)
	for kind, key := range keys {
		if !containsSubstring(byIndex[19].Findings, key) {
			t.Errorf("rules category findings omit the %s target %s", kind, key)
		}
	}
	if !strings.Contains(byIndex[24].Detail, "Codex home") {
		t.Errorf("codex category = %q, want its Codex home summary", byIndex[24].Detail)
	}
	if !containsSubstring(byIndex[24].Findings, codexRoot) {
		t.Errorf("codex category findings omit the enrolled root %s", codexRoot)
	}

	ev.Outcome = "categories 15-24 each reported; targets counted three; capability/trust/rules rows named each harness; codex category reported its home"
	recordScenario(t, ev)
}

// containsSubstring reports whether any line contains want.
func containsSubstring(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}
