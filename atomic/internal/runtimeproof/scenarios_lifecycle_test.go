// Lifecycle scenarios over isolated Claude and OMP homes, driven through the
// production install engine and the real harness adapters.
package runtimeproof

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embeddedcorpus"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// scenarioSteps wires the production install engine with consent pre-approved
// and a fixed clock, so a scenario drives the same ordering a CLI verb does.
func scenarioSteps(home string) install.Steps {
	steps := install.DefaultSteps(home)
	steps.AssumeYes = true
	steps.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	return steps
}

// scenarioStepsWith replaces the adapter set, so a scenario can exercise a
// profile layout the production resolver does not enumerate.
func scenarioStepsWith(home string, adapters ...harness.Adapter) install.Steps {
	steps := scenarioSteps(home)
	steps.Registry = func(string) (*harness.Registry, error) { return harness.NewRegistry(adapters...) }
	return steps
}

// deterministicOMP computes profile roots from the home, so a multi-profile
// scenario needs no installed OMP binary or ambient profile environment.
func deterministicOMP(named ...string) *omp.Adapter {
	a := omp.New()
	a.ConfigPath = func(home, profile string) (string, error) {
		if profile == "" {
			return filepath.Join(home, filepath.FromSlash(omp.DefaultAgentDir)), nil
		}
		return filepath.Join(home, ".omp", "profiles", profile, "agent"), nil
	}
	// This resolver answers every name, so an ambient selector would add a
	// profile the scenario never asked for.
	a.AmbientProfile = nil
	if len(named) > 0 {
		names := append([]string(nil), named...)
		a.ProfileNames = func(string) []string { return names }
	}
	return a
}

// enrolledTarget reports whether the ledger records kind:instance.
func enrolledTarget(t *testing.T, home string, kind harness.Kind, instance string) bool {
	t.Helper()
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	_, ok := ledger.FindTarget(string(kind), instance)
	return ok
}

// Install, repair, and uninstall must act only on enrolled targets, and Claude's
// global contract lands in ~/.claude/CLAUDE.md without a ~/.claude/AGENTS.md.
func TestClaudeOnlyLifecycle(t *testing.T) {
	home := isolatedHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	steps := scenarioSteps(home)
	root := filepath.Join(home, ".claude")

	ev := ScenarioEvidence{
		Scenario:  "cp8a/lifecycle/claude-only",
		Group:     "lifecycle",
		Engine:    "install + harness/claude",
		Criterion: "install/repair/uninstall act on enrolled Claude targets; global steering is a CLAUDE.md block with no global AGENTS.md",
		Command:   "install.Steps.Converge (claude, enroll) → Converge (repair) + unenrolled-repair refusal",
		Paths:     []string{home, root},
	}

	report, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}, Enroll: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(report) != 1 || report[0].Status != harness.StatusConverged || len(report[0].Blockers) > 0 {
		t.Fatalf("install reports = %+v, want one converged Claude target", report)
	}
	steering, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read global steering: %v", err)
	}
	if !strings.Contains(string(steering), "<atomic>") {
		t.Errorf("global steering carries no Atomic block")
	}
	if fileExists(filepath.Join(root, "AGENTS.md")) {
		t.Errorf("the Claude adapter created a global AGENTS.md loader")
	}
	if !enrolledTarget(t, home, harness.KindClaude, root) {
		t.Fatalf("the Claude target was not enrolled")
	}
	if !fileExists(filepath.Join(root, "commands", "commit.md")) {
		t.Fatalf("the artifact tree was not projected")
	}

	// Settings ownership is in the ledger, so a cleared file is drift repair
	// restores; an unenrolled home refuses rather than writing.
	settings := filepath.Join(root, "settings.json")
	if err := os.WriteFile(settings, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repair, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(repair) != 1 || len(repair[0].Blockers) > 0 {
		t.Fatalf("repair reports = %+v, want one unblocked target", repair)
	}
	restored, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(restored), "atomic hooks session-start") {
		t.Errorf("repair did not restore the SessionStart registration")
	}
	fresh := isolatedHome(t)
	if _, err := scenarioSteps(fresh).Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}}); err == nil {
		t.Errorf("repair of an unenrolled target succeeded")
	}

	ev.Outcome = "claude target converged; global block present; no global AGENTS.md; removed SessionStart registration restored by repair; unenrolled repair refused"
	recordScenario(t, ev)
}

// Enrolling one OMP profile must give it one native AGENTS.md and create no
// Claude target as a side effect.
func TestOMPOnlyLifecycle(t *testing.T) {
	home := isolatedHome(t)
	steps := scenarioSteps(home)
	root := filepath.Join(home, filepath.FromSlash(omp.DefaultAgentDir))

	ev := ScenarioEvidence{
		Scenario:  "cp8a/lifecycle/omp-only",
		Group:     "lifecycle",
		Engine:    "install + harness/omp",
		Criterion: "enrollment targets only the selected OMP profile and writes its package and steering",
		Command:   "install.Steps.Converge (omp, enroll)",
		Paths:     []string{home, root},
	}

	reports, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindOMP}, Enroll: true})
	if err != nil {
		t.Fatalf("enroll omp: %v", err)
	}
	if len(reports) != 1 || reports[0].Status != harness.StatusConverged || len(reports[0].Blockers) > 0 {
		t.Fatalf("enroll reports = %+v, want one converged OMP target", reports)
	}
	if !fileExists(filepath.Join(config.PackageRoot(home, "omp"), "extensions", "atomic.ts")) {
		t.Errorf("the shared OMP package was not published")
	}
	steering, err := os.ReadFile(omp.SteeringPath(root))
	if err != nil {
		t.Fatalf("read profile steering: %v", err)
	}
	if !strings.Contains(string(steering), "<atomic>") {
		t.Errorf("profile steering carries no Atomic block")
	}
	if !enrolledTarget(t, home, harness.KindOMP, root) {
		t.Errorf("the OMP target was not enrolled")
	}
	if enrolledTarget(t, home, harness.KindClaude, filepath.Join(home, ".claude")) {
		t.Errorf("an OMP-only enroll enrolled a Claude target")
	}

	ev.Outcome = "omp target converged; shared package and profile steering published; no Claude target enrolled"
	recordScenario(t, ev)
}

// A named OMP profile resolves its own root through `omp config path`, so this
// skips loudly without the OMP binary.
func TestOMPNamedProfile(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp8a/lifecycle/omp-named-profile",
		Group:     "lifecycle",
		Engine:    "harness/omp",
		Criterion: "a named OMP profile resolves to its own agent root and enrolls there without touching the default profile",
		Command:   "omp.DefaultConfigPath(home, \"work\") → omp.Adapter.Enroll",
	}

	if _, err := exec.LookPath("omp"); err != nil {
		recordScenarioSkip(t, ev, "omp binary unavailable: named-profile resolution has no recorded default root")
		return
	}
	home := isolatedHome(t)
	t.Setenv(omp.ProfileEnv, "")
	root, err := omp.DefaultConfigPath(home, "work")
	if err != nil {
		t.Fatalf("resolve named profile root: %v", err)
	}
	want := filepath.Join(home, ".omp", "profiles", "work", "agent")
	if root != want {
		t.Fatalf("named profile root = %s, want %s", root, want)
	}
	ev.Paths = []string{home, root}

	adapter := omp.New()
	result, err := adapter.Enroll(omp.EnrollRequest{Home: home, Profile: omp.Profile{Name: "work", Root: root}})
	if err != nil {
		t.Fatalf("enroll named profile: %v", err)
	}
	if result.Status != harness.StatusConverged || len(result.Applied) == 0 {
		t.Fatalf("enroll result = %+v, want an applied converged profile", result)
	}
	if !fileExists(omp.SteeringPath(root)) {
		t.Errorf("named profile steering was not written")
	}
	defRoot := filepath.Join(home, filepath.FromSlash(omp.DefaultAgentDir))
	if fileExists(omp.SteeringPath(defRoot)) {
		t.Errorf("the named-profile enroll wrote the default profile's steering")
	}

	ev.Outcome = "named profile root resolved via omp config path; steering written only under the named root"
	recordScenario(t, ev)
}

// One shared OMP package has one ownership record; an unenrolled profile that
// sees it is visibility, never a consumer.
func TestOMPSharedVisibility(t *testing.T) {
	home := isolatedHome(t)
	adapter := deterministicOMP("work", "other")
	defRoot := filepath.Join(home, filepath.FromSlash(omp.DefaultAgentDir))
	workRoot := filepath.Join(home, ".omp", "profiles", "work", "agent")
	otherRoot := filepath.Join(home, ".omp", "profiles", "other", "agent")
	if err := os.MkdirAll(otherRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	ev := ScenarioEvidence{
		Scenario:  "cp8a/lifecycle/omp-shared-visibility",
		Group:     "lifecycle",
		Engine:    "harness/omp",
		Criterion: "one physical shared package has one ownership record; consumers and mere visibility stay separate",
		Command:   "omp.Adapter.Enroll (default, work) → Resources",
		Paths:     []string{home, defRoot, workRoot, otherRoot},
	}

	for _, p := range []omp.Profile{{Root: defRoot}, {Name: "work", Root: workRoot}} {
		if _, err := adapter.Enroll(omp.EnrollRequest{Home: home, Profile: p}); err != nil {
			t.Fatalf("enroll %q: %v", p.Name, err)
		}
	}
	resources, err := adapter.Resources(home)
	if err != nil {
		t.Fatalf("resources: %v", err)
	}
	var pkg *harness.Resource
	for i := range resources {
		if resources[i].ID == omp.PackageResource(home) {
			pkg = &resources[i]
		}
	}
	if pkg == nil {
		t.Fatalf("resources = %+v, want the shared package record", resources)
	}
	if len(pkg.Consumers) != 2 {
		t.Errorf("package consumers = %v, want the two enrolled profiles", pkg.Consumers)
	}
	otherKey := (harness.Target{Kind: harness.KindOMP, Instance: otherRoot}).Key()
	if !contains(pkg.VisibleTo, otherKey) {
		t.Errorf("package visible-to = %v, want the unenrolled profile %s", pkg.VisibleTo, otherKey)
	}
	if contains(pkg.Consumers, otherKey) {
		t.Errorf("an unenrolled profile was counted as a consumer")
	}

	ev.Outcome = "shared package has 2 consumers and reports the unenrolled profile as visibility only"
	recordScenario(t, ev)
}

// A consumer's recorded generation is stale state, not a requirement, so a
// shared-package swap must converge every profile instead of deadlocking.
func TestOMPSharedGenerationConverges(t *testing.T) {
	home := isolatedHome(t)
	first := deterministicOMP("work")
	defRoot := filepath.Join(home, filepath.FromSlash(omp.DefaultAgentDir))
	workRoot := filepath.Join(home, ".omp", "profiles", "work", "agent")
	defKey := (harness.Target{Kind: harness.KindOMP, Instance: defRoot}).Key()
	workKey := (harness.Target{Kind: harness.KindOMP, Instance: workRoot}).Key()

	ev := ScenarioEvidence{
		Scenario: "cp8a/lifecycle/omp-shared-generation-converges",
		Group:    "lifecycle",
		Engine:   "harness/omp",
		Criterion: "a new shared-package generation converges every enrolled consumer in one operation " +
			"instead of refusing on a consumer's older recorded generation",
		Command: "Enroll (default, work) at generation one → swap the selected corpus → ConvergeEnrolled",
		Paths:   []string{home, defRoot, workRoot},
	}

	pkgRoot := omp.PackageResource(home)
	for _, p := range []omp.Profile{{Root: defRoot}, {Name: "work", Root: workRoot}} {
		if _, err := first.Enroll(omp.EnrollRequest{Home: home, Profile: p}); err != nil {
			t.Fatalf("enroll %q: %v", p.Name, err)
		}
	}
	older := treeDigest(t, pkgRoot)
	if older == "" {
		t.Fatalf("the first generation published no package tree")
	}

	// The swap: the same profiles stay enrolled while the selected binary's
	// corpus moves a whole-file artifact, which changes the package generation.
	swapped := nextGenerationCatalog(t)
	next, err := omp.BuildPackage(swapped, harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatalf("build the swapped package: %v", err)
	}
	nextDigest, err := next.TreeDigest()
	if err != nil {
		t.Fatalf("digest the swapped package: %v", err)
	}
	second := deterministicOMP("work")
	second.Corpus = func() (*artifacts.Catalog, error) { return swapped, nil }
	steps := scenarioStepsWith(home, second)
	reports, err := steps.ConvergeEnrolled()
	if err != nil {
		t.Fatalf("the update path refused after a generation swap: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("converged %d target(s), want both enrolled profiles", len(reports))
	}
	for _, report := range reports {
		if report.Status != harness.StatusConverged || len(report.Blockers) > 0 {
			t.Errorf("report %s = %s %v, want a converged target", report.Target.Key(), report.Status, report.Blockers)
		}
	}

	newer := treeDigest(t, pkgRoot)
	if newer == older {
		t.Fatalf("the package tree did not change across the generation swap")
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{defKey, workKey} {
		row, ok := ledger.Find(key, pkgRoot)
		if !ok {
			t.Fatalf("no package row for %s after convergence", key)
		}
		if row.Applied.Digest != nextDigest {
			t.Errorf("%s records package digest %s, want the converged tree %s", key, row.Applied.Digest, nextDigest)
		}
		if row.Generation != next.Generation {
			t.Errorf("%s records generation %s, want the converged %s", key, row.Generation, next.Generation)
		}
		if _, ok := ledger.FindTarget("omp", strings.TrimPrefix(key, "omp:")); !ok {
			t.Errorf("%s is not enrolled after convergence", key)
		}
	}
	ev.Outcome = "both enrolled profiles converged on the new shared package generation in one update pass"
	recordScenario(t, ev)
}

// nextGenerationCatalog loads the shipped corpus and moves one whole-file
// artifact, so the selected generation differs from the one already enrolled.
func nextGenerationCatalog(t *testing.T) *artifacts.Catalog {
	t.Helper()
	base, err := embeddedcorpus.Load()
	if err != nil {
		t.Fatalf("load embedded corpus: %v", err)
	}
	swapped := &artifacts.Catalog{Artifacts: append([]artifacts.Artifact(nil), base.Artifacts...)}
	for i := range swapped.Artifacts {
		if swapped.Artifacts[i].Kind != artifacts.KindRule {
			continue
		}
		body := append([]byte(nil), swapped.Artifacts[i].Body...)
		swapped.Artifacts[i].Body = append(body, []byte("\nNew generation.\n")...)
		return swapped
	}
	t.Fatal("the shipped corpus carries no rule to move")
	return nil
}

// A target uninstall must keep a shared resource while another consumer exists
// and remove it with the last one.
func TestTargetUninstallSharedRetained(t *testing.T) {
	home := isolatedHome(t)
	adapter := deterministicOMP("work")
	defRoot := filepath.Join(home, filepath.FromSlash(omp.DefaultAgentDir))
	workRoot := filepath.Join(home, ".omp", "profiles", "work", "agent")
	steps := scenarioStepsWith(home, adapter)

	ev := ScenarioEvidence{
		Scenario:  "cp8a/lifecycle/target-uninstall-shared-retained",
		Group:     "lifecycle",
		Engine:    "install + harness/omp",
		Criterion: "a target uninstall retains a shared resource while another enrolled consumer exists and removes it with the last",
		Command:   "Converge (default, work) → UninstallTarget(default) → UninstallTarget(work)",
		Paths:     []string{home, defRoot, workRoot},
	}

	if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindOMP, Instance: defRoot}, Enroll: true}); err != nil {
		t.Fatalf("enroll default: %v", err)
	}
	if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindOMP, Instance: workRoot}, Enroll: true}); err != nil {
		t.Fatalf("enroll work: %v", err)
	}

	defKey := (harness.Target{Kind: harness.KindOMP, Instance: defRoot}).Key()
	removal, err := steps.UninstallTarget(defKey)
	if err != nil {
		t.Fatalf("uninstall default: %v", err)
	}
	if len(removal.Retained) == 0 {
		t.Fatalf("removal = %+v, want the shared package retained", removal)
	}
	if !fileExists(omp.SteeringPath(workRoot)) {
		t.Errorf("the other consumer's steering was removed")
	}
	if !fileExists(filepath.Join(config.PackageRoot(home, "omp"), "extensions", "atomic.ts")) {
		t.Errorf("the shared package was removed while a consumer remained")
	}

	workKey := (harness.Target{Kind: harness.KindOMP, Instance: workRoot}).Key()
	if _, err := steps.UninstallTarget(workKey); err != nil {
		t.Fatalf("uninstall work: %v", err)
	}
	if fileExists(config.PackageRoot(home, "omp")) {
		t.Errorf("the shared package survived the last consumer's removal")
	}

	ev.Outcome = "first target uninstall retained the shared package; last consumer removed it"
	recordScenario(t, ev)
}

// A full uninstall must keep config.toml, profile.md, wikis.md, and backups, and
// remove completed operational state.
func TestFullUninstallPreservesAuthorities(t *testing.T) {
	home := isolatedHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	steps := scenarioSteps(home)
	root := filepath.Join(home, ".claude")

	ev := ScenarioEvidence{
		Scenario:  "cp8a/lifecycle/full-uninstall-preserves-authorities",
		Group:     "lifecycle",
		Engine:    "install",
		Criterion: "full uninstall preserves mutable authorities and removes the enrolled artifacts",
		Command:   "Converge (claude, enroll) → seed authorities → UninstallAll",
		Paths:     []string{home, root},
	}

	if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}, Enroll: true}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	authorities := map[string]string{
		filepath.Join(home, ".atomic", "config.toml"): "state.dir=\".claude\"\n",
		filepath.Join(home, ".atomic", "profile.md"):  "# profile\n",
		filepath.Join(home, ".atomic", "wikis.md"):    "# wikis\n",
	}
	for path, content := range authorities {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := steps.UninstallAll(); err != nil {
		t.Fatalf("full uninstall: %v", err)
	}
	for path, content := range authorities {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("authority %s was removed: %v", path, err)
			continue
		}
		if string(got) != content {
			t.Errorf("authority %s changed: %q", path, got)
		}
	}
	if fileExists(filepath.Join(root, "commands", "commit.md")) {
		t.Errorf("enrolled artifacts survived the full uninstall")
	}

	ev.Outcome = "artifacts removed; config.toml, profile.md, and wikis.md preserved byte-for-byte"
	recordScenario(t, ev)
}

// An already-current update skips the download but must still converge enrolled
// targets.
func TestUpdateAlreadyCurrentConverges(t *testing.T) {
	home := isolatedHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	steps := scenarioSteps(home)

	ev := ScenarioEvidence{
		Scenario:  "cp8a/lifecycle/update-already-current-converges",
		Group:     "lifecycle",
		Engine:    "install (update convergence seam)",
		Criterion: "an already-current update still converges enrolled targets and is a no-op with none",
		Command:   "Converge (claude, enroll) → ConvergeEnrolled",
		Paths:     []string{home, filepath.Join(home, ".claude")},
	}

	if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}, Enroll: true}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	reports, err := steps.ConvergeEnrolled()
	if err != nil {
		t.Fatalf("converge enrolled: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Blockers) > 0 {
		t.Fatalf("reports = %+v, want one converged target", reports)
	}
	if reports[0].Generation != claude.Generation() {
		t.Errorf("generation = %q, want the running binary's %q", reports[0].Generation, claude.Generation())
	}

	empty := isolatedHome(t)
	if reports, err := scenarioSteps(empty).ConvergeEnrolled(); err != nil || len(reports) != 0 {
		t.Errorf("unenrolled home converge = %v, %v; want an empty no-op", reports, err)
	}

	ev.Outcome = "enrolled target reconverged at the binary's generation; unenrolled home was a no-op"
	recordScenario(t, ev)
}

// Claude's global contract renders straight into ~/.claude/CLAUDE.md, with no
// user-level ~/.claude/AGENTS.md loader.
func TestGlobalInstallProjection(t *testing.T) {
	home := isolatedHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	root := filepath.Join(home, ".claude")
	steps := scenarioSteps(home)

	ev := ScenarioEvidence{
		Scenario:  "cp8a/lifecycle/global-install-projection",
		Group:     "lifecycle",
		Engine:    "harness/claude + install",
		Criterion: "the global contract projects straight into ~/.claude/CLAUDE.md and no global ~/.claude/AGENTS.md is created",
		Command:   "install.Steps.Converge (claude, enroll)",
		Paths:     []string{home, root},
	}

	artifacts, err := claude.GlobalArtifacts(home, root)
	if err != nil {
		t.Fatalf("global artifacts: %v", err)
	}
	sawSteering := false
	for _, a := range artifacts {
		if a.ID == "AGENTS.md" {
			t.Fatalf("the global projection maps an artifact to the forbidden global loader")
		}
		if a.ID == "CLAUDE.md" {
			sawSteering = true
			if a.Kind != managedfile.KindBlock {
				t.Errorf("global steering kind = %s, want a managed block", a.Kind)
			}
		}
	}
	if !sawSteering {
		t.Fatalf("the global projection carries no CLAUDE.md steering artifact")
	}

	if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}, Enroll: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !fileExists(filepath.Join(root, "CLAUDE.md")) {
		t.Errorf("global steering was not written to CLAUDE.md")
	}
	if fileExists(claude.GlobalLoaderPath(root)) {
		t.Errorf("a global AGENTS.md loader was created")
	}

	ev.Outcome = "global contract projected into CLAUDE.md as a managed block; no global AGENTS.md created"
	recordScenario(t, ev)
}

// Uninstall must recover unresolved journals oldest-first before it removes
// anything.
func TestUninstallRecovery(t *testing.T) {
	home := isolatedHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	root := filepath.Join(home, ".claude")
	steps := scenarioSteps(home)

	ev := ScenarioEvidence{
		Scenario:  "cp8a/lifecycle/uninstall-recovery",
		Group:     "lifecycle",
		Engine:    "install + installstate",
		Criterion: "uninstall recovers an unresolved journal before removing the enrolled target",
		Command:   "Converge (claude, enroll) → unresolved journal → UninstallAll",
		Paths:     []string{home, root},
	}

	if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}, Enroll: true}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Rows) == 0 {
		t.Fatalf("no ownership rows to build the interrupted operation from")
	}
	row := ledger.Rows[0]
	// An applied-but-unrecorded write: the native bytes already match the
	// intended digest, so recovery commits them rather than discarding.
	scenarioMkfile(t, row.Applied.Path, string(mustRead(t, row.Applied.Path)))
	m := installstate.Mutation{
		Unit: "cp8a-recovery", Resource: row.Resource, Target: row.Target,
		Kind: row.Applied.Kind, Path: row.Applied.Path, Intended: row.Applied.Digest,
		PriorObserved: true, BackupSum: "unused",
	}
	journalPath := scenarioWriteJournal(t, home, scenarioJournal("cp8a-uninstall-recovery", []installstate.Mutation{m},
		[]installstate.Progress{{Unit: m.Unit, State: installstate.StateApplied, At: scenarioFixedTime()}}))
	if !fileExists(journalPath) {
		t.Fatalf("the unresolved journal was not written")
	}

	if _, err := steps.UninstallAll(); err != nil {
		t.Fatalf("full uninstall: %v", err)
	}
	if fileExists(journalPath) {
		t.Errorf("the recovered journal was not consumed by the uninstall")
	}
	if fileExists(filepath.Join(root, "commands", "commit.md")) {
		t.Errorf("the enrolled artifacts survived the full uninstall")
	}

	ev.Outcome = "uninstall recovered the unresolved journal and then removed the enrolled artifacts"
	recordScenario(t, ev)
}

// mustRead reads path, failing the test on error.
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

var builtAtomicBinary struct {
	once sync.Once
	path string
	err  error
}

// atomicBinary builds the real CLI once per package run. ATOMIC_RUNTIMEPROOF_ATOMIC_BIN
// overrides it, to prove the scenario fails when the replacement converges nothing.
func atomicBinary(t *testing.T) (string, error) {
	t.Helper()
	if override := strings.TrimSpace(os.Getenv("ATOMIC_RUNTIMEPROOF_ATOMIC_BIN")); override != "" {
		return override, nil
	}
	builtAtomicBinary.once.Do(func() {
		dir, err := os.MkdirTemp("", "cp8a-atomic-bin")
		if err != nil {
			builtAtomicBinary.err = err
			return
		}
		path := filepath.Join(dir, "atomic")
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", path, "./cmd/atomic")
		cmd.Dir = moduleRoot()
		if out, err := cmd.CombinedOutput(); err != nil {
			builtAtomicBinary.err = errors.New(strings.TrimSpace(string(out)))
			return
		}
		builtAtomicBinary.path = path
	})
	return builtAtomicBinary.path, builtAtomicBinary.err
}

// moduleRoot walks up from the test's working directory to the Go module root,
// so the binary build does not assume a fixed checkout depth.
func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if fileExists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

// The re-executed replacement binary owns post-swap convergence and must apply
// only its own embedded generation.
func TestUpdateReplacementConverges(t *testing.T) {
	home := isolatedHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	steps := scenarioSteps(home)
	root := filepath.Join(home, ".claude")
	settings := filepath.Join(root, "settings.json")

	ev := ScenarioEvidence{
		Scenario:  "cp8a/lifecycle/update-replacement-converges",
		Group:     "lifecycle",
		Engine:    "cmd/atomic update (replacement child)",
		Criterion: "the replacement binary re-executes for target convergence and applies its own embedded generation",
		Command:   "atomic update --__target-converge --no-doctor against an isolated HOME",
		Paths:     []string{home, root},
	}

	if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindClaude}, Enroll: true}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	binary, err := atomicBinary(t)
	if err != nil {
		recordScenarioSkip(t, ev, "atomic binary could not be built: "+err.Error())
		return
	}

	// Without this perturbation the assertions below would pass whether or not
	// the replacement ran.
	targetKey := (harness.Target{Kind: harness.KindClaude, Instance: root}).Key()
	const perturbed = "cp8a-perturbed-generation"
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	perturbedRows := 0
	for i := range ledger.Rows {
		if ledger.Rows[i].Target == targetKey {
			ledger.Rows[i].Generation = perturbed
			perturbedRows++
		}
	}
	if perturbedRows == 0 {
		t.Fatalf("the enrollment recorded no ownership rows for %s to perturb", targetKey)
	}
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(binary, "update", "--__target-converge", "--no-doctor")
	// Update migrates the repository it runs in; this source tree must not be one.
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "HOME="+home, "CLAUDE_CONFIG_DIR=")
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Fatalf("replacement convergence exited non-zero: %v\n%s", runErr, out)
	}
	ledger, err = installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, r := range ledger.Rows {
		if r.Target != targetKey {
			continue
		}
		rows++
		if r.Generation != claude.Generation() {
			t.Errorf("recorded generation = %q, want the replacement's %q", r.Generation, claude.Generation())
		}
	}
	if rows == 0 {
		t.Fatalf("the replacement left no ownership rows for the enrolled target")
	}
	restored, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(restored), "atomic hooks session-start") {
		t.Errorf("the replacement's convergence did not restore the SessionStart registration it removed")
	}

	ev.Outcome = "replacement child converged the enrolled target, restored its embedded generation, and re-registered the cleared SessionStart hook"
	recordScenario(t, ev)
}
