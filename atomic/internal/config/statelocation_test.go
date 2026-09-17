package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withHome points projectHome at a temp dir and clears the resolution caches, so
// a test can change config.toml and observe the new resolution.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	restore := SetHomeDirForTest(home)
	t.Cleanup(restore)
	resetStateResolutionCacheForTest()
	t.Cleanup(resetStateResolutionCacheForTest)
	return home
}

func writeStateDirConfig(t *testing.T, home, value string) {
	t.Helper()
	cfg := Default()
	if value != "" {
		if err := Set(cfg, "state.dir", value); err != nil {
			t.Fatalf("Set(state.dir, %q): %v", value, err)
		}
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}
	resetStateResolutionCacheForTest()
}

// The ladder is process override, record, configured segment, built-in default,
// most specific first.
func TestResolveStateLocation_Order(t *testing.T) {
	home := withHome(t)
	repo := t.TempDir()
	writeStateDirConfig(t, home, ".state")

	got, err := ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation: %v", err)
	}
	if got.Root != filepath.Join(repo, ".state") || got.Source != StateSourceConfig {
		t.Fatalf("configured resolution = %+v, want %s/.state (config)", got, repo)
	}

	if _, err := PersistStateSelection(repo, ".record"); err != nil {
		t.Fatalf("PersistStateSelection: %v", err)
	}
	got, err = ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation after record: %v", err)
	}
	if got.Root != filepath.Join(repo, ".record") || got.Source != StateSourceRecord {
		t.Fatalf("record resolution = %+v, want %s/.record (record)", got, repo)
	}

	override := t.TempDir()
	t.Setenv(StateDirEnvVar, override)
	got, err = ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation with override: %v", err)
	}
	if got.Root != override || got.Source != StateSourceProcessEnv {
		t.Fatalf("process resolution = %+v, want %s (process-env)", got, override)
	}
}

func TestResolveStateLocation_BuiltinDefault(t *testing.T) {
	withHome(t)
	repo := t.TempDir()

	got, err := ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation: %v", err)
	}
	if got.Root != filepath.Join(repo, ".claude") || got.Source != StateSourceDefault {
		t.Fatalf("default resolution = %+v, want %s/.claude", got, repo)
	}
}

// A process override affects one process only: it never writes the persisted
// selection record.
func TestResolveStateLocation_ProcessOverridePersistsNothing(t *testing.T) {
	home := withHome(t)
	repo := t.TempDir()
	override := t.TempDir()
	t.Setenv(StateDirEnvVar, override)

	if _, err := ResolveStateLocation(repo); err != nil {
		t.Fatalf("ResolveStateLocation: %v", err)
	}
	if _, err := os.Stat(StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Errorf("process override created a selection record at %s", StateLocationRecordPath(repo))
	}
	if _, err := os.Stat(filepath.Join(home, ".atomic")); !os.IsNotExist(err) {
		t.Errorf("process override wrote under the real state home")
	}
}

// The retired harness variable blocks resolution with instructions and is never
// treated as an alias; an explicit process override still takes precedence.
func TestResolveStateLocation_ObsoleteHarnessEnvRefuses(t *testing.T) {
	withHome(t)
	repo := t.TempDir()
	t.Setenv(ObsoleteHarnessEnvVar, "pi")

	_, err := ResolveStateLocation(repo)
	if err == nil {
		t.Fatal("expected refusal for obsolete harness env")
	}
	if !errors.Is(err, ErrObsoleteHarnessEnv) {
		t.Errorf("error = %v, want ErrObsoleteHarnessEnv", err)
	}
	if !strings.Contains(err.Error(), StateDirEnvVar) {
		t.Errorf("error %q must name %s as the replacement", err, StateDirEnvVar)
	}
	if !strings.Contains(err.Error(), ObsoleteHarnessEnvVar) {
		t.Errorf("error %q must name the obsolete variable", err)
	}

	override := t.TempDir()
	t.Setenv(StateDirEnvVar, override)
	got, err := ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("explicit override must win over the obsolete variable: %v", err)
	}
	if got.Root != override || got.Source != StateSourceProcessEnv {
		t.Fatalf("resolution with both variables = %+v, want %s", got, override)
	}
}

// state.dir accepts exactly one safe path segment, shared with the other
// path-segment sources.
func TestStateDirSegmentValidation(t *testing.T) {
	for _, valid := range []string{".claude", ".pi", "state", ".my-state_1"} {
		if err := ValidateStateDirSegment(valid); err != nil {
			t.Errorf("ValidateStateDirSegment(%q): unexpected error %v", valid, err)
		}
	}
	for _, invalid := range []string{"", ".", "..", "a/b", "/abs", "..\\win", "sp ace"} {
		if err := ValidateStateDirSegment(invalid); err == nil {
			t.Errorf("ValidateStateDirSegment(%q): expected error", invalid)
		}
	}
}

// The config surface accepts, resolves, and rejects state.dir by the same rules.
func TestConfigStateDirKey(t *testing.T) {
	cfg := Default()
	if got, _ := Get(cfg, "state.dir"); got != ".claude" {
		t.Errorf("default state.dir = %q, want .claude", got)
	}
	if err := Set(cfg, "state.dir", "a/b"); err == nil {
		t.Error("Set(state.dir, \"a/b\") must be rejected")
	}
	if err := Set(cfg, "state.dir", ".state"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate after set: %v", err)
	}
	if err := Unset(cfg, "state.dir"); err != nil {
		t.Fatalf("Unset: %v", err)
	}
	if cfg.State.Dir != "" {
		t.Errorf("state.dir after Unset = %q, want unset", cfg.State.Dir)
	}
}

// An empty repository has no candidate and adopts nothing — no record is written
// until a stateful operation asks for one.
func TestAdoptStateRoot_EmptyRepoWritesNothing(t *testing.T) {
	withHome(t)
	repo := t.TempDir()

	got, err := AdoptStateRoot(repo)
	if err != nil {
		t.Fatalf("AdoptStateRoot: %v", err)
	}
	if got.Source != StateSourceDefault {
		t.Errorf("empty-repo adoption source = %q, want builtin-default", got.Source)
	}
	if _, err := os.Stat(StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Errorf("empty repo created an adoption record")
	}
	candidates, err := DiscoverStateCandidates(repo)
	if err != nil {
		t.Fatalf("DiscoverStateCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("empty repo candidates = %v, want none", candidates)
	}
}

// One unambiguous populated root is adopted in place: files stay where they are
// and the record is persisted before the caller switches resolution.
func TestAdoptStateRoot_OneRootInPlace(t *testing.T) {
	withHome(t)
	repo := t.TempDir()
	marker := filepath.Join(repo, ".claude", "atomic.toml")
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("scope = \"repo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := AdoptStateRoot(repo)
	if err != nil {
		t.Fatalf("AdoptStateRoot: %v", err)
	}
	if got.Root != filepath.Join(repo, ".claude") || got.Source != StateSourceRecord {
		t.Fatalf("adoption = %+v, want %s/.claude (record)", got, repo)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("adoption moved repository state: %v", err)
	}
	resolved, err := ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation after adoption: %v", err)
	}
	if resolved.Root != got.Root || resolved.Source != StateSourceRecord {
		t.Errorf("resolution after adoption = %+v, want %+v", resolved, got)
	}
}

// An empty `.pi` (or `.claude`) directory is not a candidate: only real Atomic
// state signatures count.
func TestDiscoverStateCandidates_IgnoresEmptyDirectories(t *testing.T) {
	withHome(t)
	repo := t.TempDir()
	for _, name := range []string{".claude", ".pi"} {
		if err := os.MkdirAll(filepath.Join(repo, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := DiscoverStateCandidates(repo)
	if err != nil {
		t.Fatalf("DiscoverStateCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %v, want none for empty harness dirs", candidates)
	}
	if _, err := AdoptStateRoot(repo); err != nil {
		t.Fatalf("AdoptStateRoot: %v", err)
	}
	if _, err := os.Stat(StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Errorf("empty harness dirs produced an adoption record")
	}
}

// Two populated candidates are ambiguous and refuse with actionable guidance.
func TestAdoptStateRoot_AmbiguousRefuses(t *testing.T) {
	withHome(t)
	repo := t.TempDir()
	for _, name := range []string{".claude", ".pi"} {
		dir := filepath.Join(repo, name, ".atomic-index")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	_, err := AdoptStateRoot(repo)
	if err == nil {
		t.Fatal("expected ambiguity refusal")
	}
	if !errors.Is(err, ErrAmbiguousStateRoot) {
		t.Errorf("error = %v, want ErrAmbiguousStateRoot", err)
	}
	if !strings.Contains(err.Error(), "atomic state adopt") {
		t.Errorf("error %q must name the explicit selection verb", err)
	}
	if _, err := os.Stat(StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Errorf("ambiguous adoption wrote a selection record")
	}
}

// A persisted selection is withdrawn only while its bytes are unchanged — a
// later state write turns rollback into a conflict.
func TestWithdrawStateSelection_OnlyWhenUnchanged(t *testing.T) {
	withHome(t)
	repo := t.TempDir()

	first, err := PersistStateSelection(repo, ".claude")
	if err != nil {
		t.Fatalf("PersistStateSelection: %v", err)
	}
	if err := WithdrawStateSelection(repo, first); err != nil {
		t.Fatalf("withdraw of an unchanged record: %v", err)
	}
	if _, err := os.Stat(StateLocationRecordPath(repo)); !os.IsNotExist(err) {
		t.Fatalf("withdraw left the record in place")
	}

	again, err := PersistStateSelection(repo, ".claude")
	if err != nil {
		t.Fatalf("PersistStateSelection: %v", err)
	}
	if _, err := PersistStateSelection(repo, ".other"); err != nil {
		t.Fatalf("PersistStateSelection (later write): %v", err)
	}
	err = WithdrawStateSelection(repo, again)
	if !errors.Is(err, ErrStateSelectionConflict) {
		t.Fatalf("withdraw after a later write = %v, want ErrStateSelectionConflict", err)
	}
	sel, ok, rerr := ReadStateSelection(repo)
	if rerr != nil || !ok || sel.StateDir != ".other" {
		t.Errorf("later selection was lost: sel=%+v ok=%v err=%v", sel, ok, rerr)
	}
}

// Pi agent configuration stays independent: no fingerprint selects state, a
// `.pi` tree without Atomic signatures is not adopted, and the [pi] table
// survives state.dir writes untouched.
func TestPiConfigurationRemainsIndependent(t *testing.T) {
	home := withHome(t)
	repo := t.TempDir()
	t.Setenv("PI_CODING_AGENT", "true")
	t.Setenv("CLAUDECODE", "1")

	got, err := ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation: %v", err)
	}
	if got.Root != filepath.Join(repo, ".claude") {
		t.Errorf("fingerprints changed repository state to %q", got.Root)
	}

	piDir := filepath.Join(repo, ".pi", "agents")
	if err := os.MkdirAll(piDir, 0o755); err != nil {
		t.Fatal(err)
	}
	candidates, err := DiscoverStateCandidates(repo)
	if err != nil {
		t.Fatalf("DiscoverStateCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("pi agent tree adopted as state candidate: %v", candidates)
	}

	cfg := Default()
	cfg.Pi = map[string]any{"agents": map[string]any{"atomic-implementer": map[string]any{"model": "anthropic/claude-sonnet-4"}}}
	if err := Set(cfg, "state.dir", ".state"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}
	loaded, _, err := Load(TOMLPath(home))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Pi == nil {
		t.Fatal("state.dir write discarded the [pi] table")
	}
	env := ResolvePiAgents(TOMLPath(home), filepath.Join(home, "missing.toml"))
	if env.Agents["atomic-implementer"].Model != "anthropic/claude-sonnet-4" {
		t.Errorf("Pi agent override lost: %+v", env.Agents)
	}
}

// MutablePaths names every ~/.atomic authority with exactly one writer.
func TestMutablePathsFor(t *testing.T) {
	home := "/home/u"
	m := MutablePathsFor(home)
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"ConfigTOML", m.ConfigTOML, "/home/u/.atomic/config.toml"},
		{"ProfileMD", m.ProfileMD, "/home/u/.atomic/profile.md"},
		{"WikisMD", m.WikisMD, "/home/u/.atomic/wikis.md"},
		{"InstallDir", m.InstallDir, "/home/u/.atomic/install"},
		{"BackupsDir", m.BackupsDir, "/home/u/.atomic/backups"},
		{"PackagesDir", m.PackagesDir, "/home/u/.atomic/packages"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if got := PackageRoot(home, "omp"); got != "/home/u/.atomic/packages/omp/atomic" {
		t.Errorf("PackageRoot = %q", got)
	}
}

// A legacy explicit harness.dir supplies the initial selection evidence: its
// populated directory is adopted and resolution follows it.
func TestAdoptStateRoot_LegacyHarnessDirEvidence(t *testing.T) {
	home := withHome(t)
	repo := t.TempDir()
	sig := filepath.Join(repo, ".pi", ".atomic-index")
	if err := os.MkdirAll(sig, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := Default()
	if err := Set(cfg, "harness.dir", ".pi"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}
	resetStateResolutionCacheForTest()

	got, err := AdoptStateRoot(repo)
	if err != nil {
		t.Fatalf("AdoptStateRoot: %v", err)
	}
	if got.Root != filepath.Join(repo, ".pi") || got.Source != StateSourceRecord {
		t.Fatalf("adoption = %+v, want %s/.pi (record)", got, repo)
	}
	resolved, err := ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation: %v", err)
	}
	if resolved.Root != filepath.Join(repo, ".pi") {
		t.Errorf("resolution = %+v, want %s/.pi", resolved, repo)
	}
}

// An empty repository adopts nothing, and the root it returns must equal what
// the ladder resolves — a configured state.dir segment, legacy harness.dir
// evidence, or the built-in default — so state created at the returned root is
// never split from neutral resolution.
func TestAdoptStateRoot_EmptyRepoAgreesWithResolution(t *testing.T) {
	home := withHome(t)

	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{"state.dir segment", "state.dir", ".state"},
		{"legacy harness.dir evidence", "harness.dir", ".pi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			if err := Set(cfg, tc.key, tc.value); err != nil {
				t.Fatalf("Set(%s, %q): %v", tc.key, tc.value, err)
			}
			if err := WritePersist(TOMLPath(home), cfg); err != nil {
				t.Fatalf("WritePersist: %v", err)
			}
			resetStateResolutionCacheForTest()

			repo := t.TempDir()
			adopted, err := AdoptStateRoot(repo)
			if err != nil {
				t.Fatalf("AdoptStateRoot: %v", err)
			}
			resolved, err := ResolveStateLocation(repo)
			if err != nil {
				t.Fatalf("ResolveStateLocation: %v", err)
			}
			if adopted != resolved {
				t.Fatalf("adoption = %+v, resolution = %+v; must name the same root", adopted, resolved)
			}
			if want := filepath.Join(repo, tc.value); adopted.Root != want {
				t.Errorf("adopted root = %q, want %q", adopted.Root, want)
			}
			if _, err := os.Stat(StateLocationRecordPath(repo)); !os.IsNotExist(err) {
				t.Errorf("empty repo created an adoption record")
			}
		})
	}
}
