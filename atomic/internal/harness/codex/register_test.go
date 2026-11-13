package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// TestObserveRegistrationReadsCodexRegistry proves the observer reports only
// what Codex's own file records and infers nothing.
func TestObserveRegistrationReadsCodexRegistry(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)

	state, err := ObserveRegistration(root)
	if err != nil {
		t.Fatalf("observe missing registry: %v", err)
	}
	if state.Marketplace || state.Plugin || state.Enabled || state.CachePresent {
		t.Errorf("state = %+v, want nothing observed for an absent registry", state)
	}

	writeFile(t, ConfigPath(root), "model = \"user-selected\"\n\n[marketplaces."+MarketplaceName+"]\nsource_type = \"local\"\n\n[plugins.\""+PluginID()+"\"]\nenabled = true\n")
	if err := os.MkdirAll(CachePath(root), 0o755); err != nil {
		t.Fatal(err)
	}
	state, err = ObserveRegistration(root)
	if err != nil {
		t.Fatalf("observe registry: %v", err)
	}
	if !state.Marketplace || !state.Plugin || !state.Enabled || !state.CachePresent {
		t.Errorf("state = %+v, want the recorded marketplace, plugin, enabled flag, and cache", state)
	}

	row := (&Adapter{}).RegistrationStateRow(root)
	if row.Status != harness.StatusSupported {
		t.Errorf("registration row = %+v, want supported for a fully observed registry", row)
	}
}

func TestObserveRegistrationRejectsMalformedRegistry(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	writeFile(t, ConfigPath(root), "this is not = = toml")
	if _, err := ObserveRegistration(root); err == nil {
		t.Error("malformed registry accepted")
	}
}

func TestParsePluginListReportsOnlyObservedPlugin(t *testing.T) {
	data := []byte(`{"installed":[{"pluginId":"other@m","installed":true,"enabled":true},{"pluginId":"` + PluginID() + `","installed":true,"enabled":false}],"available":[]}`)
	installed, enabled, err := parsePluginList(data, PluginID())
	if err != nil {
		t.Fatal(err)
	}
	if !installed || enabled {
		t.Errorf("installed=%v enabled=%v, want the entry's own flags", installed, enabled)
	}
	if _, _, err := parsePluginList([]byte("not json"), PluginID()); err == nil {
		t.Error("malformed plugin list accepted")
	}
}

// TestRegisterReplayAgainstIsolatedHome drives the real codex binary in an
// isolated CODEX_HOME, because the surface it proves is the native one.
func TestRegisterReplayAgainstIsolatedHome(t *testing.T) {
	if _, err := codexBinary(); err != nil {
		t.Skipf("skip codex plugin replay: %v", err)
	}

	home := newHome(t)
	root := newRoot(t, home)
	a := newAdapter(t, root)
	a.RegisterFn = DefaultRegister

	if _, err := a.Enroll(EnrollRequest{Home: home, Root: root, OperationID: "replay-enroll"}); err != nil {
		t.Fatalf("publish plugin: %v", err)
	}

	result, err := a.Register(RegisterRequest{Home: home, Root: root, OperationID: "replay-register"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if result.PluginID != PluginID() {
		t.Errorf("plugin id = %q, want %q", result.PluginID, PluginID())
	}
	if !result.Installed || !result.Enabled {
		t.Fatalf("registration reported installed=%v enabled=%v", result.Installed, result.Enabled)
	}
	if result.CachePath != CachePath(root) || !exists(result.CachePath) {
		t.Fatalf("cache path %s was not materialized", result.CachePath)
	}
	// Codex copies the plugin source directory into the versioned cache, so the
	// cache holds each plugin-relative path without the marketplace prefix.
	for _, path := range []string{PluginManifestPath, MatcherPath, RuleIndexPath} {
		rel := strings.TrimPrefix(path, PluginRoot+"/")
		if !exists(filepath.Join(result.CachePath, filepath.FromSlash(rel))) {
			t.Errorf("installed cache is missing %s", rel)
			entries := cacheEntries(t, result.CachePath)
			t.Logf("installed cache holds: %v", entries)
		}
	}
	if !result.State.Marketplace || !result.State.Plugin || !result.State.Enabled {
		t.Errorf("observed registry state = %+v, want the recorded marketplace and enabled plugin", result.State)
	}
}

// cacheEntries lists a cache directory's file paths relative to its root, so a
// layout mismatch reports what Codex actually materialized.
func cacheEntries(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Logf("walk cache: %v", err)
	}
	return out
}

// fakeCodex points BinaryEnv at a script that logs each invocation, answers
// `plugin list` with listJSON, and fails `plugin add` when addFails is set.
func fakeCodex(t *testing.T, listJSON string, addFails bool) (log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "calls.log")
	fail := "0"
	if addFails {
		fail = "1"
	}
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\n" +
		"case \"$*\" in\n" +
		"  \"plugin list --json\") echo '" + listJSON + "' ;;\n" +
		"  \"plugin add \"*) exit " + fail + " ;;\n" +
		"esac\n"
	bin := filepath.Join(dir, "codex")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(BinaryEnv, bin)
	return log
}

func publishedHome(t *testing.T) (home, root string) {
	t.Helper()
	home = newHome(t)
	root = newRoot(t, home)
	if _, err := newAdapter(t, root).Enroll(EnrollRequest{Home: home, Root: root}); err != nil {
		t.Fatal(err)
	}
	return home, root
}

func calls(t *testing.T, log string) string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// A user's deliberate disablement is refused before Codex is touched at all.
func TestRegisterRefusesADisabledPluginBeforeAnyMutation(t *testing.T) {
	home, root := publishedHome(t)
	log := fakeCodex(t, `{"installed":[{"pluginId":"`+PluginID()+`","installed":true,"enabled":false}]}`, false)

	if _, err := DefaultRegister(RegisterRequest{Home: home, Root: root}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("err = %v, want the disablement refused", err)
	}
	if got := calls(t, log); got != "plugin list --json\n" {
		t.Errorf("codex calls = %q, want only the read-only list", got)
	}
}

// A marketplace this call added is removed again when the plugin install
// fails, so a failed registration leaves Codex as it found it.
func TestRegisterRollsBackItsMarketplaceWhenPluginAddFails(t *testing.T) {
	t.Run("added by this call", func(t *testing.T) {
		home, root := publishedHome(t)
		log := fakeCodex(t, `{"installed":[]}`, true)
		if _, err := DefaultRegister(RegisterRequest{Home: home, Root: root}); err == nil {
			t.Fatal("a failed plugin add reported success")
		}
		if got := calls(t, log); !strings.HasSuffix(got, "plugin marketplace remove "+MarketplaceName+"\n") {
			t.Errorf("codex calls = %q, want the marketplace removed after the failed add", got)
		}
	})
	t.Run("already registered", func(t *testing.T) {
		home, root := publishedHome(t)
		writeFile(t, ConfigPath(root), "[marketplaces."+MarketplaceName+"]\nsource_type = \"local\"\n")
		log := fakeCodex(t, `{"installed":[]}`, true)
		if _, err := DefaultRegister(RegisterRequest{Home: home, Root: root}); err == nil {
			t.Fatal("a failed plugin add reported success")
		}
		if got := calls(t, log); strings.Contains(got, "marketplace remove") {
			t.Errorf("codex calls = %q, want a marketplace this call did not add left alone", got)
		}
	})
}

// After plugin add succeeds, a registration that cannot be verified is undone,
// or Codex would hold a plugin no ledger row records.
func TestRegisterRollsBackWhenTheCacheIsNotMaterialized(t *testing.T) {
	home, root := publishedHome(t)
	log := fakeCodex(t, `{"installed":[{"pluginId":"`+PluginID()+`","installed":true,"enabled":true}]}`, false)
	if _, err := DefaultRegister(RegisterRequest{Home: home, Root: root}); err == nil {
		t.Fatal("a registration with no cache reported success")
	}
	got := calls(t, log)
	if !strings.Contains(got, "plugin remove "+PluginID()) || !strings.HasSuffix(got, "plugin marketplace remove "+MarketplaceName+"\n") {
		t.Errorf("codex calls = %q, want the plugin and the marketplace this call added removed", got)
	}
}

// Codex exits 1 removing a marketplace it does not hold, so unregister runs
// only for the entries the registry records.
func TestUnregisterRemovesOnlyWhatCodexHolds(t *testing.T) {
	t.Run("neither entry", func(t *testing.T) {
		home, root := publishedHome(t)
		log := fakeCodex(t, `{"installed":[]}`, false)
		if err := DefaultUnregister(RegisterRequest{Home: home, Root: root}); err != nil {
			t.Fatalf("unregister: %v", err)
		}
		if got := calls(t, log); got != "" {
			t.Errorf("codex calls = %q, want none", got)
		}
	})
	t.Run("only the marketplace", func(t *testing.T) {
		home, root := publishedHome(t)
		writeFile(t, ConfigPath(root), "[marketplaces."+MarketplaceName+"]\nsource_type = \"local\"\n")
		log := fakeCodex(t, `{"installed":[]}`, false)
		if err := DefaultUnregister(RegisterRequest{Home: home, Root: root}); err != nil {
			t.Fatalf("unregister: %v", err)
		}
		if got := calls(t, log); got != "plugin marketplace remove "+MarketplaceName+"\n" {
			t.Errorf("codex calls = %q, want only the marketplace removed", got)
		}
	})
	t.Run("codex absent and nothing registered", func(t *testing.T) {
		home, root := publishedHome(t)
		t.Setenv(BinaryEnv, "")
		t.Setenv("PATH", t.TempDir())
		if err := DefaultUnregister(RegisterRequest{Home: home, Root: root}); err != nil {
			t.Errorf("unregister = %v, want nothing to do", err)
		}
	})
}

// TestRegisterRequiresPublishedTree proves registration refuses before running
// the native command when the generated tree is absent.
func TestRegisterRequiresPublishedTree(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	_, err := DefaultRegister(RegisterRequest{Home: home, Root: root, OperationID: "no-tree"})
	if err == nil {
		t.Fatal("registration ran without a published marketplace tree")
	}
	if !strings.Contains(err.Error(), "not published") {
		t.Errorf("error %q does not name the missing tree", err)
	}
}
