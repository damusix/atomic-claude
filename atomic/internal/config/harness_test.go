package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// No config.toml in home falls back to the built-in default. Hermetic — never
// touches os.UserHomeDir.
func TestResolveHarnessDirFromHome_Default(t *testing.T) {
	home := t.TempDir()
	got := resolveHarnessDirFromHome(home)
	if got != stateDirDefault {
		t.Errorf("resolveHarnessDirFromHome(empty home) = %q, want %q", got, stateDirDefault)
	}
}

// Unparseable config.toml falls back to the default rather than propagating an
// error — the resolver is lenient on any load error.
func TestResolveHarnessDirFromHome_MalformedConfig(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(Dir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TOMLPath(home), []byte("[harness\ndir = \".pi\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveHarnessDirFromHome(home)
	if got != stateDirDefault {
		t.Errorf("resolveHarnessDirFromHome(malformed config) = %q, want default %q", got, stateDirDefault)
	}
}

// The retired harness variable is not a rung: with no state.dir configured the
// resolution lands on the built-in default, and no fingerprint changes it.
func TestResolveHarnessDir_IgnoresHarnessEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ATOMIC_HARNESS", "pi")
	t.Setenv("PI_CODING_AGENT", "true")
	t.Setenv("CLAUDECODE", "1")
	got := resolveHarnessDirFromHome(home)
	if got != stateDirDefault {
		t.Errorf("resolveHarnessDirFromHome with harness env = %q, want default %q", got, stateDirDefault)
	}
}

// The seam makes the repo-local helpers use the override without touching the
// real home or the process cache.
func TestSetStateDirForTest_Override(t *testing.T) {
	restore := SetStateDirForTest(".pi")
	defer restore()

	if got := IndexDir(""); got != ".pi/.atomic-index" {
		t.Errorf("IndexDir() under seam = %q, want \".pi/.atomic-index\"", got)
	}
}

// A nested call's restore func puts back the previous override.
func TestSetStateDirForTest_Restore(t *testing.T) {
	restoreOuter := SetStateDirForTest(".pi")
	defer restoreOuter()

	restoreInner := SetStateDirForTest(".foo")
	if got := IndexDir(""); got != ".foo/.atomic-index" {
		t.Fatalf("IndexDir() under inner seam = %q, want \".foo/.atomic-index\"", got)
	}

	restoreInner()
	if got := IndexDir(""); got != ".pi/.atomic-index" {
		t.Errorf("after inner restore, IndexDir() = %q, want \".pi/.atomic-index\"", got)
	}
}

// Every repo-local helper joins root + the overridden harness.dir + its suffix.
func TestRepoLocalHelpers_UnderNonDefaultHarnessDir(t *testing.T) {
	restore := SetStateDirForTest(".pi")
	defer restore()

	root := "/repo"
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"ScratchpadDir", ScratchpadDir(root), filepath.Join(root, ".pi", ".scratchpad")},
		{"ProjectDir", ProjectDir(root), filepath.Join(root, ".pi", "project")},
		{"FollowupsDir", FollowupsDir(root), filepath.Join(root, ".pi", "project", "followups")},
		{"IndexDir", IndexDir(root), filepath.Join(root, ".pi", ".atomic-index")},
		{"IndexDBPath", IndexDBPath(root), filepath.Join(root, ".pi", ".atomic-index", "atomic.db")},
		{"WorktreeDir", WorktreeDir(root, "omp", ""), filepath.Join(root, ".pi", "worktrees")},
		{"RepoConfigPath", RepoConfigPath(root), filepath.Join(root, ".pi", "atomic.toml")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// Only Claude keeps worktrees in .claude; any other or unknown harness uses the
// state root, so a new harness never lands in .claude by accident.
func TestWorktreeDir_KeyedByHarness(t *testing.T) {
	defer SetStateDirForTest(".state")()

	root := "/repo"
	cases := []struct {
		harness, want string
	}{
		{"claude", filepath.Join(root, ".claude", "worktrees")},
		{"omp", filepath.Join(root, ".state", "worktrees")},
		{"codex", filepath.Join(root, ".state", "worktrees")},
		{"zed", filepath.Join(root, ".state", "worktrees")},
		{"", filepath.Join(root, ".state", "worktrees")},
	}
	for _, tc := range cases {
		if got := WorktreeDir(root, tc.harness, ""); got != tc.want {
			t.Errorf("WorktreeDir(%q) = %q, want %q", tc.harness, got, tc.want)
		}
	}
}

// A preference outranks the harness default; relative ones anchor at the
// repository root, and off passes through for the caller to interpret.
func TestWorktreeDir_PreferenceWinsAndOffStaysOff(t *testing.T) {
	defer SetStateDirForTest(".state")()

	root := "/repo"
	for _, harness := range []string{"claude", "omp"} {
		if got, want := WorktreeDir(root, harness, "trees"), filepath.Join(root, "trees"); got != want {
			t.Errorf("%s relative preference = %q, want %q", harness, got, want)
		}
		if got := WorktreeDir(root, harness, "/elsewhere/wt"); got != "/elsewhere/wt" {
			t.Errorf("%s absolute preference = %q, want /elsewhere/wt", harness, got)
		}
		if got := WorktreeDir(root, harness, "off"); got != "off" {
			t.Errorf("%s off preference = %q, want off", harness, got)
		}
	}
}

func TestWorkflowPrefsPath_InStateRoot(t *testing.T) {
	defer SetStateDirForTest(".state")()

	got, err := WorkflowPrefsPath("/repo")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/repo", ".state", "workflow-preferences.md"); got != want {
		t.Errorf("WorkflowPrefsPath = %q, want %q", got, want)
	}
}

func TestWorkflowPrefsPath_AmbiguousStateRootIsAnError(t *testing.T) {
	home := withHome(t)
	writeStateDirConfig(t, home, ".state")
	repo := t.TempDir()
	populate(t, repo, ".atomic")
	populate(t, repo, ".claude")

	if _, err := WorkflowPrefsPath(repo); !errors.Is(err, ErrAmbiguousStateRoot) {
		t.Errorf("error = %v, want ErrAmbiguousStateRoot", err)
	}
}

// With the seam pinned to the built-in default, every helper resolves under
// ".atomic".
func TestRepoLocalHelpers_DefaultHarnessDir(t *testing.T) {
	restore := SetStateDirForTest(stateDirDefault)
	defer restore()

	root := "/repo"
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"ScratchpadDir", ScratchpadDir(root), filepath.Join(root, ".atomic", ".scratchpad")},
		{"ProjectDir", ProjectDir(root), filepath.Join(root, ".atomic", "project")},
		{"FollowupsDir", FollowupsDir(root), filepath.Join(root, ".atomic", "project", "followups")},
		{"IndexDir", IndexDir(root), filepath.Join(root, ".atomic", ".atomic-index")},
		{"IndexDBPath", IndexDBPath(root), filepath.Join(root, ".atomic", ".atomic-index", "atomic.db")},
		{"WorktreeDir", WorktreeDir(root, "omp", ""), filepath.Join(root, ".atomic", "worktrees")},
		{"RepoConfigPath", RepoConfigPath(root), filepath.Join(root, ".atomic", "atomic.toml")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func writeRawConfig(t *testing.T, home, body string) {
	t.Helper()
	if err := os.MkdirAll(Dir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TOMLPath(home), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A stored harness.dir changes no resolution, draws no warning, and is dropped
// by the next write.
func TestStoredHarnessDirIsInert(t *testing.T) {
	home := withHome(t)
	writeRawConfig(t, home, "[harness]\ndir = \".pi\"\n")
	resetStateResolutionCacheForTest()

	if got := resolveHarnessDirFromHome(home); got != stateDirDefault {
		t.Errorf("resolveHarnessDirFromHome = %q, want %q", got, stateDirDefault)
	}
	repo := t.TempDir()
	got, err := ResolveStateLocation(repo)
	if err != nil {
		t.Fatalf("ResolveStateLocation: %v", err)
	}
	if got.Root != filepath.Join(repo, stateDirDefault) {
		t.Errorf("resolution = %+v, want %s", got, stateDirDefault)
	}

	cfg, warns, err := Load(TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "harness") {
		t.Errorf("rewritten config still carries harness:\n%s", raw)
	}
}

func TestHarnessDirIsAnUnknownKey(t *testing.T) {
	cfg := Default()
	if err := Set(cfg, "harness.dir", "x"); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("Set(harness.dir) error = %v, want unknown key", err)
	}
	if _, ok := Resolved(cfg)["harness.dir"]; ok {
		t.Error("Resolved still lists harness.dir")
	}
}

func TestStoredPiTableLoadsSilentlyAndIsDroppedOnWrite(t *testing.T) {
	home := withHome(t)
	writeRawConfig(t, home, "[pi.agents.scout]\nmodel = \"x\"\n")

	cfg, warns, err := Load(TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "[pi") {
		t.Errorf("config write kept the [pi] table:\n%s", data)
	}
}
