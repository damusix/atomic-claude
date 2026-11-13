package codex

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// fakeRegister records the request and writes what Codex was observed to
// write, so the lifecycle is testable without a codex binary.
func fakeRegister(t *testing.T, calls *[]RegisterRequest) func(RegisterRequest) (RegisterResult, error) {
	return func(req RegisterRequest) (RegisterResult, error) {
		*calls = append(*calls, req)
		writeFile(t, ConfigPath(req.Root), "[marketplaces."+MarketplaceName+"]\nsource_type = \"local\"\nsource = \""+PackageResource(req.Home)+"\"\n\n[plugins.\""+PluginID()+"\"]\nenabled = true\n")
		if err := os.RemoveAll(CachePath(req.Root)); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(CachePath(req.Root)), 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("cp", "-R", filepath.Join(PackageResource(req.Home), PluginRoot), CachePath(req.Root)).CombinedOutput(); err != nil {
			t.Fatalf("copy plugin: %v %s", err, out)
		}
		target := harness.Target{Kind: harness.KindCodex, Instance: req.Root, NativeRoot: req.Root}
		if err := recordCacheOwnership(req.Home, target, CachePath(req.Root)); err != nil {
			t.Fatal(err)
		}
		return RegisterResult{Target: target, Installed: true, Enabled: true, CachePath: CachePath(req.Root), Applied: []string{CachePath(req.Root)}}, nil
	}
}

func project(t *testing.T, a *Adapter, home, root string) harness.Plan {
	t.Helper()
	plan, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	return plan
}

// Registration is the step that makes Codex list the plugin's skills, so the
// plan must name it and convergence must perform it once, not on every run.
func TestConvergeRegistersThePublishedPluginOnce(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	var calls []RegisterRequest
	a := newAdapter(t, root)
	a.RegisterFn = fakeRegister(t, &calls)

	plan := project(t, a, home, root)
	if !slices.ContainsFunc(plan.Claims, func(c harness.Claim) bool { return c.Path == CachePath(root) }) {
		t.Fatalf("plan claims = %+v, want the registration's plugin cache so consent names it", plan.Claims)
	}
	c, err := a.Lifecycle(home).Converge(codexTarget(root), plan)
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if len(calls) != 1 || calls[0].Home != home || calls[0].Root != root {
		t.Fatalf("register calls = %+v, want one for this home", calls)
	}
	if !slices.Contains(c.Applied, CachePath(root)) || !slices.Contains(c.Applied, SteeringPath(root)) {
		t.Errorf("applied = %v, want the steering block and the plugin cache", c.Applied)
	}

	again := project(t, a, home, root)
	if !again.Converged {
		t.Errorf("a registered, current home plans unconverged: %+v", again)
	}
	c, err = a.Lifecycle(home).Converge(codexTarget(root), again)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Errorf("a current registration was re-run: %d calls", len(calls))
	}
	if !c.Itemized || len(c.Applied) != 0 {
		t.Errorf("applied = %#v itemized %v, want an itemized, empty list for a no-op converge", c.Applied, c.Itemized)
	}
}

// A registration that could not run is a gap the user must see with its fix,
// never a silent success.
func TestConvergeReportsUnperformedRegistration(t *testing.T) {
	for name, fn := range map[string]func(RegisterRequest) (RegisterResult, error){
		"no seam": nil,
		"no codex binary": func(RegisterRequest) (RegisterResult, error) {
			return RegisterResult{}, harness.ErrUnsupported
		},
		"codex refuses": func(RegisterRequest) (RegisterResult, error) {
			return RegisterResult{}, errors.New("marketplace 'atomic-claude' is already added from a different source")
		},
	} {
		t.Run(name, func(t *testing.T) {
			home := newHome(t)
			root := newRoot(t, home)
			a := newAdapter(t, root)
			a.RegisterFn = fn

			c, err := a.Lifecycle(home).Converge(codexTarget(root), project(t, a, home, root))
			if err != nil {
				t.Fatalf("converge: %v", err)
			}
			if slices.Contains(c.Applied, CachePath(root)) {
				t.Errorf("applied = %v names a registration that did not happen", c.Applied)
			}
			found := false
			for _, u := range c.Unproven {
				if strings.HasPrefix(u, registrationSurface+": ") && strings.Contains(u, "atomic install --harness codex") {
					found = true
				}
			}
			if !found {
				t.Errorf("unproven = %v, want the registration gap with its fix", c.Unproven)
			}
		})
	}
}

// Uninstall undoes the registration only when Atomic made it, so a user's own
// registration of the same names is never removed.
func TestTargetUninstallUnregistersOnlyAnAtomicRegistration(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	var calls []RegisterRequest
	var removed []RegisterRequest
	a := newAdapter(t, root)
	a.RegisterFn = fakeRegister(t, &calls)
	a.UnregisterFn = func(req RegisterRequest) error {
		removed = append(removed, req)
		return nil
	}
	if _, err := a.Lifecycle(home).Converge(codexTarget(root), project(t, a, home, root)); err != nil {
		t.Fatal(err)
	}
	removal, err := a.Lifecycle(home).Remove(codexTarget(root))
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("Remove unregistered; only the post-removal step may")
	}
	if removal, err = a.Lifecycle(home).Removed(codexTarget(root), removal); err != nil {
		t.Fatalf("removed: %v", err)
	}
	if len(removed) != 1 || removed[0].Root != root {
		t.Fatalf("unregister calls = %+v, want one for this home", removed)
	}
	if !slices.Contains(removal.Removed, CachePath(root)) {
		t.Errorf("removed = %v, want the plugin cache", removal.Removed)
	}

	home2 := newHome(t)
	root2 := newRoot(t, home2)
	userConfig := "[marketplaces." + MarketplaceName + "]\nsource_type = \"local\"\nsource = \"/elsewhere\"\n\n[plugins.\"" + PluginID() + "\"]\nenabled = true\n"
	writeFile(t, ConfigPath(root2), userConfig)
	writeFile(t, filepath.Join(CachePath(root2), "marker"), "user\n")
	b := newAdapter(t, root2)
	b.UnregisterFn = a.UnregisterFn
	if _, err := b.Enroll(EnrollRequest{Home: home2, Root: root2}); err != nil {
		t.Fatal(err)
	}
	removal2, err := b.Lifecycle(home2).Remove(codexTarget(root2))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Lifecycle(home2).Removed(codexTarget(root2), removal2); err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Errorf("unregister ran for a home Atomic never registered")
	}
	if got := readFile(t, ConfigPath(root2)); got != userConfig {
		t.Errorf("config.toml = %q, want the user's registration untouched", got)
	}
	if got := readFile(t, filepath.Join(CachePath(root2), "marker")); got != "user\n" {
		t.Errorf("the user's plugin cache was touched: %q", got)
	}
}

// A kept enrollment would let `atomic update` re-converge a home the user
// uninstalled, so a failed unregister names the manual commands instead.
func TestFailedUnregisterEndsTheEnrollmentWithTheManualFix(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	var calls []RegisterRequest
	a := newAdapter(t, root)
	a.RegisterFn = fakeRegister(t, &calls)
	a.UnregisterFn = func(RegisterRequest) error { return errors.New("codex: plugin remove: exit status 1") }
	if _, err := a.Lifecycle(home).Converge(codexTarget(root), project(t, a, home, root)); err != nil {
		t.Fatal(err)
	}

	removal, err := a.Lifecycle(home).Remove(codexTarget(root))
	if err != nil {
		t.Fatal(err)
	}
	if removal, err = a.Lifecycle(home).Removed(codexTarget(root), removal); err != nil {
		t.Fatalf("removed: %v", err)
	}
	if len(removal.Skipped) != 0 || !slices.Contains(removal.Removed, CachePath(root)) {
		t.Errorf("removal = %+v, want the cache removed and nothing kept", removal)
	}
	joined := strings.Join(removal.Notices, "\n")
	for _, want := range []string{"plugin remove: exit status 1", "codex plugin remove " + PluginID(), "codex plugin marketplace remove " + MarketplaceName} {
		if !strings.Contains(joined, want) {
			t.Errorf("notices = %q, want %q", joined, want)
		}
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.FindTarget(string(harness.KindCodex), root); ok {
		t.Error("the uninstalled home is still enrolled, so update would re-converge it")
	}
	if _, ok := ledger.Find(codexTarget(root).Key(), CachePath(root)); ok {
		t.Error("the cache row survived the uninstall")
	}
}

// A registration the user already removed leaves nothing to undo, even when
// the codex binary is gone.
func TestUninstallClearsWhenCodexHoldsNoRegistration(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	var calls []RegisterRequest
	a := newAdapter(t, root)
	a.RegisterFn = fakeRegister(t, &calls)
	a.UnregisterFn = DefaultUnregister
	if _, err := a.Lifecycle(home).Converge(codexTarget(root), project(t, a, home, root)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ConfigPath(root)); err != nil {
		t.Fatal(err)
	}
	t.Setenv(BinaryEnv, "")
	t.Setenv("PATH", t.TempDir())

	removal, err := a.Lifecycle(home).Remove(codexTarget(root))
	if err != nil {
		t.Fatal(err)
	}
	if removal, err = a.Lifecycle(home).Removed(codexTarget(root), removal); err != nil {
		t.Fatal(err)
	}
	if len(removal.Skipped) != 0 || len(removal.Notices) != 0 {
		t.Errorf("removal = %+v, want a clean uninstall", removal)
	}
}

// Codex's own registry is the source of truth for registration; an unreadable
// one is reported with its parse error, never answered by re-registering.
func TestConvergeReportsAnUnreadableCodexConfig(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	var calls []RegisterRequest
	a := newAdapter(t, root)
	a.RegisterFn = fakeRegister(t, &calls)
	writeFile(t, ConfigPath(root), "[marketplaces\nbroken")

	c, err := a.Lifecycle(home).Converge(codexTarget(root), project(t, a, home, root))
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("register ran over an unreadable config.toml")
	}
	if !slices.ContainsFunc(c.Unproven, func(u string) bool {
		return strings.HasPrefix(u, registrationSurface+": ") && strings.Contains(u, "config.toml")
	}) {
		t.Errorf("unproven = %v, want the registration gap naming the parse failure", c.Unproven)
	}
}

// A user who removed the plugin from Codex meant it: install reports the gap
// with the way back and never re-adds the plugin unless asked to replace.
func TestUserRemovedRegistrationHolds(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	var calls []RegisterRequest
	a := newAdapter(t, root)
	a.RegisterFn = fakeRegister(t, &calls)
	if _, err := a.Lifecycle(home).Converge(codexTarget(root), project(t, a, home, root)); err != nil {
		t.Fatal(err)
	}
	// `codex plugin remove` drops the plugin entry and the cache and keeps the
	// marketplace.
	writeFile(t, ConfigPath(root), "[marketplaces."+MarketplaceName+"]\nsource_type = \"local\"\n")
	if err := os.RemoveAll(CacheRoot(root)); err != nil {
		t.Fatal(err)
	}

	plan := project(t, a, home, root)
	if !plan.Converged {
		t.Errorf("plan after a user removal = %+v, want converged with the removal standing", plan)
	}
	if !slices.ContainsFunc(plan.Unproven, func(u string) bool { return strings.Contains(u, "--replace") }) {
		t.Errorf("plan unproven = %v, want the user-removal gap with its fix", plan.Unproven)
	}
	if _, err := a.Lifecycle(home).Converge(codexTarget(root), plan); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("register calls = %d, want the user's removal to hold", len(calls))
	}

	replace, err := a.Lifecycle(home).Project(codexTarget(root), harness.PlanRequest{BatchDecision: installstate.DecisionReplace})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Lifecycle(home).Converge(codexTarget(root), replace); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Errorf("register calls = %d, want --replace to re-register", len(calls))
	}
}

// The registration fix must match why Codex lacks the plugin; a generic
// "install codex-cli" sends a user with a disabled plugin the wrong way.
func TestRegistrationRemediationFollowsTheObservedState(t *testing.T) {
	const surface = "native marketplace and plugin registration"
	t.Run("codex absent", func(t *testing.T) {
		home := newHome(t)
		root := newRoot(t, home)
		t.Setenv(BinaryEnv, "")
		t.Setenv("PATH", t.TempDir())
		if fix := Remediation(home, root, surface); !strings.Contains(fix, BinaryEnv) {
			t.Errorf("fix = %q, want the codex-cli install step", fix)
		}
	})
	t.Run("plugin disabled", func(t *testing.T) {
		home := newHome(t)
		root := newRoot(t, home)
		writeFile(t, ConfigPath(root), "[plugins.\""+PluginID()+"\"]\nenabled = false\n")
		want := "codex plugin add " + PluginID()
		if fix := Remediation(home, root, surface); !strings.Contains(fix, want) {
			t.Errorf("fix = %q, want %q, which codex-cli 0.147.0 re-enables with", fix, want)
		}
		if fix := Remediation(home, root, "plugin-hook disablement"); !strings.Contains(fix, want) {
			t.Errorf("disablement fix = %q, want the same re-enable step", fix)
		}
	})
	t.Run("config unreadable", func(t *testing.T) {
		home := newHome(t)
		root := newRoot(t, home)
		writeFile(t, ConfigPath(root), "[plugins\nbroken")
		if fix := Remediation(home, root, surface); !strings.Contains(fix, ConfigPath(root)) {
			t.Errorf("fix = %q, want the file to repair", fix)
		}
	})
	t.Run("removed by the user", func(t *testing.T) {
		home := newHome(t)
		root := newRoot(t, home)
		var calls []RegisterRequest
		a := newAdapter(t, root)
		a.RegisterFn = fakeRegister(t, &calls)
		if _, err := a.Lifecycle(home).Converge(codexTarget(root), project(t, a, home, root)); err != nil {
			t.Fatal(err)
		}
		writeFile(t, ConfigPath(root), "[marketplaces."+MarketplaceName+"]\nsource_type = \"local\"\n")
		if err := os.RemoveAll(CacheRoot(root)); err != nil {
			t.Fatal(err)
		}
		if fix := Remediation(home, root, surface); !strings.Contains(fix, "--replace") {
			t.Errorf("fix = %q, want the replace step", fix)
		}
	})
}

// `uninstall --discard-changed` deletes a changed cache through the discard
// path; Codex must still be told, or config.toml points at a deleted package.
func TestDiscardedCacheIsUnregistered(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	var calls []RegisterRequest
	var removed int
	a := newAdapter(t, root)
	a.RegisterFn = fakeRegister(t, &calls)
	a.UnregisterFn = func(RegisterRequest) error { removed++; return nil }
	if _, err := a.Lifecycle(home).Converge(codexTarget(root), project(t, a, home, root)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(CachePath(root), "user-edit.txt"), "changed\n")

	removal, err := harness.RemoveTargetResources(home, codexTarget(root), harness.RemovalOptions{
		DiscardChanged: true,
		Confirm:        func(string, string) (bool, error) { return true, nil },
		Approved:       []string{CachePath(root)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(removal.Discarded, CachePath(root)) {
		t.Fatalf("removal = %+v, want the changed cache discarded", removal)
	}
	if _, err := a.Lifecycle(home).Removed(codexTarget(root), removal); err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("unregister calls = %d, want the discarded cache unregistered", removed)
	}
}

// Codex can re-copy the cache while the registration stays current; the row
// must follow, or uninstall reads the cache as changed and never unregisters.
func TestConvergeRefreshesTheCacheRowOfACurrentRegistration(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	var calls []RegisterRequest
	var removed int
	a := newAdapter(t, root)
	a.RegisterFn = fakeRegister(t, &calls)
	a.UnregisterFn = func(RegisterRequest) error { removed++; return nil }
	if _, err := a.Lifecycle(home).Converge(codexTarget(root), project(t, a, home, root)); err != nil {
		t.Fatal(err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	row, _ := ledger.Find(codexTarget(root).Key(), CachePath(root))
	row.Applied.Digest = "stale"
	ledger.Upsert(row)
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Lifecycle(home).Converge(codexTarget(root), project(t, a, home, root)); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("register calls = %d, want the current registration left alone", len(calls))
	}
	removal, err := a.Lifecycle(home).Remove(codexTarget(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Lifecycle(home).Removed(codexTarget(root), removal); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(removal.Removed, CachePath(root)) || removed != 1 {
		t.Errorf("removal = %+v unregister calls %d, want the cache removed and unregistered", removal, removed)
	}
}

// With no registration seam, the plan reports the same gap convergence does.
func TestPlanWithoutARegistrationSeamReportsTheGap(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	a := newAdapter(t, root)
	plan := project(t, a, home, root)
	c, err := a.Lifecycle(home).Converge(codexTarget(root), plan)
	if err != nil {
		t.Fatal(err)
	}
	want := registrationGap(harness.ErrUnsupported)
	if !slices.Contains(plan.Unproven, want) || !slices.Contains(c.Unproven, want) {
		t.Errorf("plan unproven %v, converge unproven %v, want both to carry %q", plan.Unproven, c.Unproven, want)
	}
}

// Plan and converge must agree on whether registration runs, or converge
// re-adds a plugin entry the plan never asked consent for.
func TestPlanAndConvergeAgreeOnAMissingPluginEntry(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	var calls []RegisterRequest
	a := newAdapter(t, root)
	a.RegisterFn = fakeRegister(t, &calls)
	if _, err := a.Lifecycle(home).Converge(codexTarget(root), project(t, a, home, root)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, ConfigPath(root), "[marketplaces."+MarketplaceName+"]\nsource_type = \"local\"\n")

	plan := project(t, a, home, root)
	if plan.Converged || len(plan.Notices) == 0 {
		t.Fatalf("plan = converged %v notices %v, want the re-registration announced", plan.Converged, plan.Notices)
	}
	if _, err := a.Lifecycle(home).Converge(codexTarget(root), plan); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Errorf("register calls = %d, want the announced re-registration", len(calls))
	}
}

// The registration edits Codex's config.toml outside the ledger, so the plan a
// user consents to must say so.
func TestPlanNamesTheCodexConfigMutation(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	var calls []RegisterRequest
	a := newAdapter(t, root)
	a.RegisterFn = fakeRegister(t, &calls)

	plan := project(t, a, home, root)
	joined := strings.Join(plan.Notices, "\n")
	for _, want := range []string{ConfigPath(root), "codex plugin marketplace add", "codex plugin add " + PluginID()} {
		if !strings.Contains(joined, want) {
			t.Errorf("notices = %q, want %q", joined, want)
		}
	}
	if _, err := a.Lifecycle(home).Converge(codexTarget(root), plan); err != nil {
		t.Fatal(err)
	}
	if again := project(t, a, home, root); len(again.Notices) != 0 {
		t.Errorf("a current registration still announces a mutation: %v", again.Notices)
	}
}
