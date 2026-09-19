// CP8A lifecycle scenarios: install, enroll, repair, uninstall, and update over
// isolated Claude and OMP homes, driven through the production install engine
// and the real harness adapters. Each scenario asserts what a consumer observes
// — an enrolled target, native bytes on disk, a refused plan, a preserved
// authority — and records the criterion it proves.
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

	"github.com/damusix/atomic-claude/atomic/internal/config"
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
// profile layout the production resolver does not enumerate — while still
// driving the real install engine over it.
func scenarioStepsWith(home string, adapters ...harness.Adapter) install.Steps {
	steps := scenarioSteps(home)
	steps.Registry = func(string) (*harness.Registry, error) { return harness.NewRegistry(adapters...) }
	return steps
}

// deterministicOMP returns an OMP adapter whose profile roots are computed from
// the home, so a multi-profile scenario is hermetic: it does not depend on an
// installed OMP binary or the ambient profile environment. The real resolver is
// exercised separately by the named-profile scenario.
func deterministicOMP(named ...string) *omp.Adapter {
	a := omp.New()
	a.ConfigPath = func(home, profile string) (string, error) {
		if profile == "" {
			return filepath.Join(home, filepath.FromSlash(omp.DefaultAgentDir)), nil
		}
		return filepath.Join(home, ".omp", "profiles", profile, "agent"), nil
	}
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

// TestCP8AClaudeOnlyLifecycle proves the Claude-only install/repair/uninstall
// path over an isolated home: a single Claude target enrolls, the global
// projection lands as a managed block with no global loader, a re-converge
// repairs drift, and a target uninstall removes the owned tree.
//
// Criterion: install, repair, and uninstall operate only on explicitly enrolled
// target instances; the Claude adapter renders the global contract into
// ~/.claude/CLAUDE.md without creating ~/.claude/AGENTS.md.
func TestCP8AClaudeOnlyLifecycle(t *testing.T) {
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

	// The adapter owns the SessionStart registration outside the ledger, so
	// clearing it is drift repair must restore; the same call for an unenrolled
	// home refuses rather than writing.
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

// TestCP8AOMPOnlyLifecycle proves the OMP-only enrollment path over an isolated
// home: one profile enrolls, the shared package and the profile steering block
// land, and no Claude target is created as a side effect.
//
// Criterion: install/enroll operate only on explicitly enrolled target
// instances; each enrolled OMP profile receives one native AGENTS.md.
func TestCP8AOMPOnlyLifecycle(t *testing.T) {
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

// TestCP8AOMPNamedProfile proves a named OMP profile resolves to its own
// isolated agent root through the CP0-proven `omp config path` selector and
// enrolls there, leaving the default profile untouched. A machine without the
// OMP binary has no recorded default root for a named profile, which is a loud
// skip.
//
// Criterion: each enrolled OMP profile receives one native AGENTS.md at its own
// resolved root.
func TestCP8AOMPNamedProfile(t *testing.T) {
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

// TestCP8AOMPSharedVisibility proves one physical shared package carries one
// ownership record with every enrolled consumer, while an unenrolled profile
// that can discover it is reported as visibility only — never a consumer.
//
// Criterion: one physical resource has one ownership record even when several
// targets or unenrolled instances can discover it; OMP shared-package
// visibility and consumer enrollment remain separate and visible.
func TestCP8AOMPSharedVisibility(t *testing.T) {
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

// TestCP8AOMPIncompatibleGeneration proves convergence refuses before any
// mutation when two enrolled profiles require different generations of one
// shared package, reporting both targets and leaving the package bytes intact.
//
// Criterion: if two enrolled OMP profiles require different generations of one
// shared package, convergence refuses before mutation and reports both targets
// without changing the package.
func TestCP8AOMPIncompatibleGeneration(t *testing.T) {
	home := isolatedHome(t)
	adapter := deterministicOMP("work")
	defRoot := filepath.Join(home, filepath.FromSlash(omp.DefaultAgentDir))
	workRoot := filepath.Join(home, ".omp", "profiles", "work", "agent")

	ev := ScenarioEvidence{
		Scenario:  "cp8a/lifecycle/omp-incompatible-generation",
		Group:     "lifecycle",
		Engine:    "harness/omp",
		Criterion: "two consumers requiring different shared-package generations refuse before mutation and report both targets",
		Command:   "Enroll (default) → rewrite recorded generation → Enroll (work)",
		Paths:     []string{home, defRoot, workRoot},
	}

	if _, err := adapter.Enroll(omp.EnrollRequest{Home: home, Profile: omp.Profile{Root: defRoot}}); err != nil {
		t.Fatalf("enroll default: %v", err)
	}
	pkgRoot := omp.PackageResource(home)
	before := treeDigest(t, pkgRoot)

	// Record a second, incompatible generation for the shared package, as an
	// older binary would have left behind.
	ledgerPath := config.LedgerPath(home)
	ledger, err := installstate.LoadLedger(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for i := range ledger.Rows {
		if ledger.Rows[i].Resource == pkgRoot {
			ledger.Rows[i].Generation = "incompatible-generation"
			changed = true
		}
	}
	if !changed {
		t.Fatalf("no package ownership row to age")
	}
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatal(err)
	}

	_, err = adapter.Enroll(omp.EnrollRequest{Home: home, Profile: omp.Profile{Name: "work", Root: workRoot}})
	if err == nil {
		t.Fatalf("enroll succeeded despite an incompatible recorded generation")
	}
	if !strings.Contains(err.Error(), "refuses before package mutation") {
		t.Errorf("refusal error = %q, want the compatibility refusal", err)
	}
	defKey := (harness.Target{Kind: harness.KindOMP, Instance: defRoot}).Key()
	if !strings.Contains(err.Error(), defKey) || !strings.Contains(err.Error(), "incompatible-generation") {
		t.Errorf("refusal error = %q, want it to name both the conflicting target and its generation", err)
	}
	if after := treeDigest(t, pkgRoot); after != before {
		t.Errorf("the package changed despite the refusal")
	}
	if fileExists(omp.SteeringPath(workRoot)) {
		t.Errorf("the refused enroll wrote the second profile's steering")
	}

	ev.Outcome = "enroll refused before mutation; both targets reported; package bytes unchanged"
	recordScenario(t, ev)
}

// TestCP8ATargetUninstallSharedRetained proves a target uninstall removes the
// resources only that target owns while retaining one another enrolled consumer
// still depends on, and removes it once the last consumer is gone.
//
// Criterion: a target uninstall removes only unchanged resources owned for that
// target and leaves shared resources while another consumer exists.
func TestCP8ATargetUninstallSharedRetained(t *testing.T) {
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

// TestCP8AFullUninstallPreservesAuthorities proves a full uninstall removes
// every enrolled target's artifacts while preserving the mutable authorities
// and the recovery state nothing unresolved still owns.
//
// Criterion: full uninstall after the final target preserves config.toml,
// profile.md, wikis.md, and backups and removes completed operational state.
func TestCP8AFullUninstallPreservesAuthorities(t *testing.T) {
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

// TestCP8AUpdateAlreadyCurrentConverges proves an up-to-date binary's update
// path still converges every enrolled target through the same engine
// `atomic update` uses after binary selection, and is a no-op on an unenrolled
// home.
//
// Criterion: an already-current normal update skips download but still converges
// enrolled targets.
func TestCP8AUpdateAlreadyCurrentConverges(t *testing.T) {
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

// TestCP8AGlobalInstallProjection proves the Claude global projection maps the
// authored global contract directly into ~/.claude/CLAUDE.md and never creates a
// user-level ~/.claude/AGENTS.md loader.
//
// Criterion: the Claude adapter renders context/AGENTS.md into ~/.claude/CLAUDE.md
// without creating or referencing ~/.claude/AGENTS.md.
func TestCP8AGlobalInstallProjection(t *testing.T) {
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

	artifacts, err := claude.GlobalArtifacts(root)
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

// TestCP8AUninstallRecovery proves a full uninstall reconciles an unresolved
// journal oldest-first before removing targets, consuming it as completed
// operational state.
//
// Criterion: uninstall acquires the one advisory lifecycle lock and recovers
// unresolved journals oldest-first before mutation.
func TestCP8AUninstallRecovery(t *testing.T) {
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
	cp8aMkfile(t, row.Applied.Path, string(mustRead(t, row.Applied.Path)))
	m := installstate.Mutation{
		Unit: "cp8a-recovery", Resource: row.Resource, Target: row.Target,
		Kind: row.Applied.Kind, Path: row.Applied.Path, Intended: row.Applied.Digest,
		PriorObserved: true, BackupSum: "unused",
	}
	journalPath := cp8aWriteJournal(t, home, cp8aJournal("cp8a-uninstall-recovery", []installstate.Mutation{m},
		[]installstate.Progress{{Unit: m.Unit, State: installstate.StateApplied, At: cp8aFixedTime()}}))
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

// so the replacement-convergence scenario drives the real CLI without a second
// build per scenario.
var builtAtomicBinary struct {
	once sync.Once
	path string
	err  error
}

// atomicBinary builds the real CLI once per package run, so the
// replacement-convergence scenario drives the real CLI without a second build
// per scenario. ATOMIC_RUNTIMEPROOF_ATOMIC_BIN overrides the built binary, which
// is how a run proves the scenario fails when the replacement converges nothing.
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

// TestCP8AUpdateReplacementConverges proves the re-exec'd replacement binary —
// the process that owns post-swap convergence — converges an enrolled target
// from its own embedded generation, through the real CLI over an isolated home.
//
// Criterion: a downloaded update re-executes the replacement binary before
// target convergence and applies only that binary's embedded generation.
func TestCP8AUpdateReplacementConverges(t *testing.T) {
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

	// Perturb the state enrollment just wrote, so only real convergence can
	// restore it. The enrolled rows already record the current generation and the
	// adapter owns the SessionStart registration, so without this the assertions
	// below would pass whether or not the replacement ran.
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
