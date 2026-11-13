// Codex guidance and rule runtime scenarios. Only registration has a retained
// observation; a row with no decidable evidence skips loudly, never passes silently.
package runtimeproof

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/embeddedcorpus"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
)

// codexAdapter returns the production Codex adapter so every row drives the
// shipped generation, not a hand-built stub.
func codexAdapter() *codex.Adapter { return codex.New() }

// codexHomeRoot prepares an isolated CODEX_HOME and points the adapter's root
// discovery at it, so no row can read or write the developer's real Codex home.
func codexHomeRoot(t *testing.T, root string) string {
	t.Helper()
	codexRoot := filepath.Join(root, "codex-home")
	if err := os.MkdirAll(codexRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(codex.HomeEnv, codexRoot)
	return codexRoot
}

// codexRuntimeSurfaces gathers every surface string and its evidence the Codex
// adapter reports for one home.
func codexRuntimeSurfaces(t *testing.T, root string) (map[string]string, map[string]harness.CapabilityStatus) {
	t.Helper()
	cat, err := embeddedcorpus.Load()
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	plugin, err := codex.BuildPlugin(cat, harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("build plugin: %v", err)
	}
	a := codexAdapter()

	surfaces := map[string]string{}
	statuses := map[string]harness.CapabilityStatus{}
	add := func(surface, evidence string, status harness.CapabilityStatus) {
		surfaces[surface] = evidence
		statuses[surface] = status
	}

	for _, gap := range plugin.Unproven {
		add(gap.Surface, gap.Evidence, harness.StatusUnsupported)
	}
	for _, row := range a.RuntimeState(root) {
		add(row.Surface, row.Evidence, row.Status)
	}
	if row := a.RegistrationStateRow(root); row.Surface != "" {
		add(row.Surface, row.Evidence, row.Status)
	}
	return surfaces, statuses
}

// surfaceMatching returns the surface whose name contains want, so an assertion
// names the row by its human identity without pinning its exact wording.
func surfaceMatching(surfaces map[string]string, want string) (string, bool) {
	for surface := range surfaces {
		if strings.Contains(surface, want) {
			return surface, true
		}
	}
	return "", false
}

// Codex 0.147.0 proved no guidance or rule delivery, so each gap must be reported
// with its evidence and never read as a deliberate no-op.
func TestCodexGuidanceAndRuleDeliveryUncovered(t *testing.T) {
	root := isolatedHome(t)
	codexRoot := codexHomeRoot(t, root)
	cat, err := embeddedcorpus.Load()
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	plugin, err := codex.BuildPlugin(cat, harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("build plugin: %v", err)
	}

	ev := ScenarioEvidence{
		Scenario:  "cp6b/codex/guidance-and-rule-delivery-uncovered",
		Group:     "codex",
		Engine:    "harness/codex",
		Criterion: "Codex registers no hook, names every withheld rule-delivery role, and reports every runtime surface unsupported with evidence",
		Command:   "codex.BuildPlugin(embedded corpus) → Adapter.RuntimeState",
		Paths:     []string{codexRoot},
	}

	// An unproven rule-delivery role is reported as withheld, never registered.
	if plugin.Hooks.ConfigFile != "" {
		t.Errorf("plugin publishes a hook config file %q although no hook role is proven", plugin.Hooks.ConfigFile)
	}
	if len(plugin.Hooks.Registered) != 0 {
		t.Errorf("plugin registered events %+v although no hook role is proven", plugin.Hooks.Registered)
	}
	withheld := map[harness.Role]bool{}
	for _, c := range plugin.Hooks.Withheld {
		withheld[c.Role] = true
	}
	for _, role := range []harness.Role{
		harness.RoleStaticScope,
		harness.RoleSessionBaseline,
		harness.RolePreOperationTargets,
		harness.RoleContextReturn,
		harness.RoleDeterministicDeny,
	} {
		if !withheld[role] {
			t.Errorf("withheld hook roles omit %s; the unproven surface must be reported", role)
		}
	}

	// A gap with no capability row carries a synthesized limitation instead of
	// evidence, which is still a reported reason.
	gapStatus := map[harness.Role]harness.CapabilityStatus{}
	for _, gap := range plugin.Gaps {
		gapStatus[gap.Role] = gap.Status
		if gap.Status == harness.StatusSupported {
			t.Errorf("rule/skill gap %s reports supported; Codex 0.147.0 proved no hook event", gap.Role)
		}
		if gap.Evidence == "" && gap.Limitation == "" {
			t.Errorf("gap %s carries neither evidence nor a limitation", gap.Role)
		}
	}
	for _, role := range []harness.Role{
		harness.RoleStaticScope,
		harness.RoleSessionBaseline,
		harness.RolePreOperationTargets,
		harness.RoleContextReturn,
		harness.RoleDeterministicDeny,
	} {
		if gapStatus[role] != harness.StatusUnsupported {
			t.Errorf("rule-delivery role %s = %q, want unsupported", role, gapStatus[role])
		}
	}
	for _, gap := range plugin.Unproven {
		if gap.Surface == "" || gap.Evidence == "" {
			t.Errorf("unproven surface %+v is missing its identity or evidence", gap)
		}
	}

	// The read-only runtime report owns the dimensions no probe could observe.
	surfaces, statuses := codexRuntimeSurfaces(t, codexRoot)
	for _, want := range []string{
		"steering @ imports",
		"command delivery",
		"plugin-hook trust",
		"deliberate hook disablement",
		"mediated tool coverage",
		"hosted-tool and specialized-tool bypasses",
		"instruction and payload limits",
		"additional-context spill",
		"child-session rule delivery",
		"last successful runtime proof",
	} {
		surface, ok := surfaceMatching(surfaces, want)
		if !ok {
			t.Errorf("no runtime surface names %q", want)
			continue
		}
		if surfaces[surface] == "" {
			t.Errorf("surface %q carries no evidence", surface)
		}
		if statuses[surface] != harness.StatusUnsupported {
			t.Errorf("surface %q reports %s; no Codex runtime row is proven", surface, statuses[surface])
		}
	}
	// Registration is the one observed row, and it is unsupported until Codex's
	// own registry records the plugin.
	if surface, ok := surfaceMatching(surfaces, "registration"); !ok {
		t.Error("no surface reports the native registration observation")
	} else if statuses[surface] != harness.StatusUnsupported {
		t.Errorf("registration reports %s for an empty home, want unsupported", statuses[surface])
	}

	ev.Outcome = "no hook registered; every withheld role named; every runtime surface unsupported with evidence"
	recordScenario(t, ev)
}

// codexRuntimeUncoveredDimensions is the Codex runtime surface set the matrix
// must account for, with the evidence that fixes each dimension.
var codexRuntimeUncoveredDimensions = []struct {
	Dimension string
	Evidence  string
}{
	{"guidance discovery and override precedence", "codex-runtime (absence), codex.hook-absence: no completed request or instruction audit"},
	{"plugin-hook trust and changed-definition re-review", "codex-runtime (absence): no hook review or execution occurred"},
	{"deliberate hook disablement and config precedence", "codex.hook-absence: not exercised, no replay target"},
	{"context return", "codex.hook-absence; replay target codex-runtime (absence): the documented additionalContext result was never exercised"},
	{"deterministic deny", "codex.hook-absence: the documented permissionDecision deny result was never exercised"},
	{"additional-context spill and consumer resolution", "codex-runtime (absence): the documented 2,500-token threshold was not boundary-tested"},
	{"mediated tool coverage", "codex.hook-absence; replay target codex-runtime (absence): the documented PreToolUse payload and local-tool inventory are not runtime proof"},
	{"hosted-tool and specialized-tool bypasses", "codex.hook-absence: not exercised, no replay target"},
	{"reload", "codex.hook-absence: not exercised, no replay target"},
	{"resume", "codex.hook-absence: not exercised, no replay target"},
	{"child-session rule delivery", "codex.hook-absence: SubagentStart and child context were not exercised, no replay target"},
	{"dedupe", "codex.hook-absence: not exercised, no replay target"},
	{"multiple-hook ordering and concurrency", "codex.hook-absence: not exercised, no replay target"},
	{"failure, timeout, and exit-code semantics", "codex-runtime: the model was rejected before any hook event, so hook failure semantics have no observation"},
	{"plugin-hook removal and cleanup", "codex.hook-absence: hook teardown not exercised; plugin unregistration is observed by TestCodexNativeKindDelivery"},
}

// The account rejected the model before any Codex hook event ran, so each hook
// dimension stays uncovered and skips loudly instead of passing on absence.
func TestCodexRuntimeHookRowsUncovered(t *testing.T) {
	_, binaryErr := exec.LookPath("codex")
	ev := ScenarioEvidence{
		Scenario:  "cp6b/codex/runtime-hook-rows-uncovered",
		Group:     "codex",
		Engine:    "harness/codex + CP0 codex-runtime",
		Criterion: "every Codex hook and guidance runtime dimension not observed by CP0 is reported uncovered with its evidence",
		Command:   "CP0 codex-runtime replay rows (no isolated launch: the model was rejected before any hook event)",
	}

	named := make([]string, 0, len(codexRuntimeUncoveredDimensions))
	for _, row := range codexRuntimeUncoveredDimensions {
		named = append(named, row.Dimension+" ["+row.Evidence+"]")
	}
	reason := "CP0 codex-runtime reached thread creation and the account rejected the configured model before any hook event, so these dimensions stay uncovered (whether another credential or model resolves the rejection is unverified): " +
		strings.Join(named, "; ")
	if binaryErr != nil {
		reason += "; the codex binary is also unavailable to the isolated runner"
	}
	recordScenarioSkip(t, ev, reason)
}

// codexWorktree prepares a real linked Git worktree beside its main checkout and
// returns both.
func codexWorktree(t *testing.T, root string) (main, worktree string) {
	t.Helper()
	main = filepath.Join(root, "main")
	worktree = filepath.Join(root, "linked")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTree(t, main, map[string]string{"README.md": "main checkout\n"})
	guidanceGit(t, main, "init", "-q", "-b", "main")
	guidanceGit(t, main, "add", "-A")
	guidanceGit(t, main, "commit", "-q", "-m", "seed")
	guidanceGit(t, main, "worktree", "add", "-q", "-b", "linked-branch", worktree)
	return main, worktree
}

// codexDirEntries lists a directory's immediate entry names, so a row can prove
// a working directory gained nothing.
func codexDirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// Registration from a linked worktree must land in the observed
// `<CODEX_HOME>/plugins/cache/<marketplace>/<plugin>/<version>` layout, never the cwd.
func TestCodexRegistrationReplayFromWorktree(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp6b/codex/registration-replay-from-worktree",
		Group:     "codex",
		Engine:    "harness/codex + codex CLI",
		Criterion: "native registration from a linked worktree cwd installs into the enrolled CODEX_HOME and leaves the cwd untouched",
		Command:   "codex.DefaultRegister(marketplace add → plugin add → plugin list) with cwd = linked worktree",
	}
	if testing.Short() {
		recordScenarioSkip(t, ev, "short mode: the real codex registration is skipped")
		return
	}
	if _, err := exec.LookPath("codex"); err != nil {
		recordScenarioSkip(t, ev, "codex binary unavailable: the native registration surface cannot be observed")
		return
	}

	versionOut, err := exec.Command("codex", "--version").Output()
	if err != nil {
		t.Fatalf("codex --version: %v", err)
	}
	ev.Command = "codex.DefaultRegister(marketplace add → plugin add → plugin list) with cwd = linked worktree; binary " + strings.TrimSpace(string(versionOut))

	root := isolatedHome(t)
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	codexRoot := codexHomeRoot(t, root)
	main, worktree := codexWorktree(t, root)
	mainEntries := codexDirEntries(t, main)
	ev.Paths = []string{home, codexRoot, main, worktree}

	a := codexAdapter()
	if _, err := a.Enroll(codex.EnrollRequest{Home: home, Root: codexRoot, OperationID: "cp6b-enroll"}); err != nil {
		t.Fatalf("publish plugin: %v", err)
	}
	t.Chdir(worktree)
	result, err := a.Register(codex.RegisterRequest{Home: home, Root: codexRoot, OperationID: "cp6b-register"})
	if err != nil {
		t.Fatalf("register from worktree cwd: %v", err)
	}
	if result.PluginID != codex.PluginID() {
		t.Errorf("plugin id = %q, want %q", result.PluginID, codex.PluginID())
	}
	if !result.Installed || !result.Enabled {
		t.Fatalf("registration reported installed=%v enabled=%v", result.Installed, result.Enabled)
	}
	if result.CachePath != codex.CachePath(codexRoot) {
		t.Fatalf("cache path %s != the CP0-observed layout %s", result.CachePath, codex.CachePath(codexRoot))
	}
	if info, err := os.Stat(result.CachePath); err != nil || !info.IsDir() {
		t.Fatalf("cache path %s was not materialized under the enrolled CODEX_HOME: %v", result.CachePath, err)
	}
	if !strings.HasPrefix(result.CachePath, codexRoot) {
		t.Errorf("cache path %s is outside the enrolled CODEX_HOME %s", result.CachePath, codexRoot)
	}
	if !result.State.Marketplace || !result.State.Plugin || !result.State.Enabled {
		t.Errorf("observed registry state = %+v, want the recorded marketplace and enabled plugin", result.State)
	}

	// The worktree and the main checkout gained no plugin artifact: registration
	// targets the home, never the invoked working directory.
	for _, dir := range []string{main, worktree} {
		entries := codexDirEntries(t, dir)
		for _, forbidden := range []string{"plugins", ".agents", ".codex"} {
			if contains(entries, forbidden) {
				t.Errorf("registration from the worktree wrote %s into %s: %v", forbidden, dir, entries)
			}
		}
	}
	if got := codexDirEntries(t, main); strings.Join(got, ",") != strings.Join(mainEntries, ",") {
		t.Errorf("the main checkout changed during a worktree registration: %v → %v", mainEntries, got)
	}

	ev.Outcome = "plugin installed and enabled under the enrolled CODEX_HOME cache layout; worktree and main checkout unchanged"
	recordScenario(t, ev)
}

// Enrolling from a non-Git directory, a worktree, and a checkout must publish one
// identical home-anchored generation and leave each directory untouched.
func TestCodexTargetingIsCwdIndependent(t *testing.T) {
	root := isolatedHome(t)
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	codexRoot := codexHomeRoot(t, root)
	main, worktree := codexWorktree(t, root)
	nonGit := filepath.Join(root, "non-git")
	if err := os.MkdirAll(nonGit, 0o755); err != nil {
		t.Fatal(err)
	}

	ev := ScenarioEvidence{
		Scenario:  "cp6b/codex/targeting-cwd-independent",
		Group:     "codex",
		Engine:    "harness/codex",
		Criterion: "publishing from a non-Git directory, a linked worktree, and a checkout produces one generation at the same home-anchored package path",
		Command:   "codex.Enroll with cwd = non-git | linked worktree | main checkout",
		Paths:     []string{nonGit, worktree, main, home},
	}

	a := codexAdapter()
	generations := make([]string, 0, 3)
	for _, cwd := range []struct{ dir, id string }{
		{nonGit, "cp6b-target-nongit"},
		{worktree, "cp6b-target-worktree"},
		{main, "cp6b-target-main"},
	} {
		before := codexDirEntries(t, cwd.dir)
		t.Chdir(cwd.dir)
		result, err := a.Enroll(codex.EnrollRequest{Home: home, Root: codexRoot, OperationID: cwd.id})
		if err != nil {
			t.Fatalf("enroll from %s: %v", cwd.dir, err)
		}
		if result.PackageRoot != codex.PackageResource(home) {
			t.Errorf("enroll from %s published to %s, want the home-anchored %s", cwd.dir, result.PackageRoot, codex.PackageResource(home))
		}
		if !strings.HasPrefix(result.PackageRoot, home) {
			t.Errorf("enroll from %s published outside the isolated home: %s", cwd.dir, result.PackageRoot)
		}
		generations = append(generations, result.Generation)
		if got := codexDirEntries(t, cwd.dir); strings.Join(got, ",") != strings.Join(before, ",") {
			t.Errorf("enrolling from %s changed the working directory: %v → %v", cwd.dir, before, got)
		}
	}
	for i := 1; i < len(generations); i++ {
		if generations[i] != generations[0] {
			t.Errorf("generation depends on cwd: %v", generations)
		}
	}
	// Enrollment publishes the package only; native registration is a separate
	// observable surface, so the CODEX_HOME stays untouched here.
	if _, err := os.Stat(filepath.Join(codexRoot, "plugins")); !os.IsNotExist(err) {
		t.Errorf("enrollment wrote a native plugin tree into CODEX_HOME: %v", err)
	}

	ev.Outcome = "one generation at the home-anchored package path from every cwd; no cwd gained a Codex artifact"
	recordScenario(t, ev)
}
