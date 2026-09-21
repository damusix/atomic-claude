package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// No config.toml in home falls back to the built-in default. Hermetic — never
// touches os.UserHomeDir.
func TestResolveHarnessDirFromHome_Default(t *testing.T) {
	home := t.TempDir()
	got := resolveHarnessDirFromHome(home)
	if got != harnessDirDefault {
		t.Errorf("resolveHarnessDirFromHome(empty home) = %q, want %q", got, harnessDirDefault)
	}
}

// A real config.toml in a temp home resolves through the actual Load path, not
// just the seam.
func TestResolveHarnessDirFromHome_RealConfigFile(t *testing.T) {
	home := t.TempDir()
	cfg := Default()
	if err := Set(cfg, "harness.dir", ".pi"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}

	got := resolveHarnessDirFromHome(home)
	if got != ".pi" {
		t.Errorf("resolveHarnessDirFromHome(home with harness.dir=.pi) = %q, want \".pi\"", got)
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
	if got != harnessDirDefault {
		t.Errorf("resolveHarnessDirFromHome(malformed config) = %q, want default %q", got, harnessDirDefault)
	}
}

// A harness.dir written straight to disk, bypassing Set's validation, must fall
// back to the default rather than reach filepath.Join unvalidated in the
// repo-local helpers — a path-escape risk for values like "..".
func TestResolveHarnessDirFromHome_InvalidStoredValueFallsBack(t *testing.T) {
	cases := []string{"..", ".", "foo/bar"}
	for _, invalid := range cases {
		t.Run(invalid, func(t *testing.T) {
			home := t.TempDir()
			if err := os.MkdirAll(Dir(home), 0o755); err != nil {
				t.Fatal(err)
			}
			content := fmt.Sprintf("[harness]\ndir = %q\n", invalid)
			if err := os.WriteFile(TOMLPath(home), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}

			got := resolveHarnessDirFromHome(home)
			if got != harnessDirDefault {
				t.Errorf("resolveHarnessDirFromHome(invalid stored value %q) = %q, want default %q", invalid, got, harnessDirDefault)
			}
		})
	}
}

// The retired harness variable is not a rung: with no state.dir configured the
// resolution lands on the built-in default, and no fingerprint changes it.
func TestResolveHarnessDir_IgnoresHarnessEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv(ObsoleteHarnessEnvVar, "pi")
	t.Setenv("PI_CODING_AGENT", "true")
	t.Setenv("CLAUDECODE", "1")
	got := resolveHarnessDirFromHome(home)
	if got != harnessDirDefault {
		t.Errorf("resolveHarnessDirFromHome with harness env = %q, want default %q", got, harnessDirDefault)
	}
}

// A configured state.dir is the rung every helper lands on, and legacy
// harness.dir evidence is consulted only while state.dir is unset.
func TestResolveHarnessDirFromHome_StateDirWinsOverLegacyHarnessDir(t *testing.T) {
	home := t.TempDir()
	cfg := Default()
	if err := Set(cfg, "harness.dir", ".pi"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := Set(cfg, "state.dir", ".state"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}
	if got := resolveHarnessDirFromHome(home); got != ".state" {
		t.Errorf("resolveHarnessDirFromHome = %q, want \".state\" (state.dir supersedes harness.dir evidence)", got)
	}
}

// Legacy harness.dir stays migration evidence while state.dir is unset.
func TestResolveHarnessDirFromHome_LegacyHarnessDirEvidence(t *testing.T) {
	home := t.TempDir()
	cfg := Default()
	if err := Set(cfg, "harness.dir", ".other"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}
	if got := resolveHarnessDirFromHome(home); got != ".other" {
		t.Errorf("resolveHarnessDirFromHome = %q, want \".other\"", got)
	}
}

// The seam makes the repo-local helpers use the override without touching the
// real home or the process cache.
func TestSetHarnessDirForTest_Override(t *testing.T) {
	restore := SetHarnessDirForTest(".pi")
	defer restore()

	if got := IndexDir(""); got != ".pi/.atomic-index" {
		t.Errorf("IndexDir() under seam = %q, want \".pi/.atomic-index\"", got)
	}
}

// A nested call's restore func puts back the previous override.
func TestSetHarnessDirForTest_Restore(t *testing.T) {
	restoreOuter := SetHarnessDirForTest(".pi")
	defer restoreOuter()

	restoreInner := SetHarnessDirForTest(".foo")
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
	restore := SetHarnessDirForTest(".pi")
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
		{"WorktreesDir", WorktreesDir(root), filepath.Join(root, ".pi", "worktrees")},
		{"RepoConfigPath", RepoConfigPath(root), filepath.Join(root, ".pi", "atomic.toml")},
		{"RemindersDirLegacy", RemindersDirLegacy(root), filepath.Join(root, ".pi", ".scratchpad", "reminders")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// With the seam pinned to the built-in default, every helper resolves under
// ".claude" — today's layout, byte for byte.
func TestRepoLocalHelpers_DefaultHarnessDir(t *testing.T) {
	restore := SetHarnessDirForTest(harnessDirDefault)
	defer restore()

	root := "/repo"
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"ScratchpadDir", ScratchpadDir(root), filepath.Join(root, ".claude", ".scratchpad")},
		{"ProjectDir", ProjectDir(root), filepath.Join(root, ".claude", "project")},
		{"FollowupsDir", FollowupsDir(root), filepath.Join(root, ".claude", "project", "followups")},
		{"IndexDir", IndexDir(root), filepath.Join(root, ".claude", ".atomic-index")},
		{"IndexDBPath", IndexDBPath(root), filepath.Join(root, ".claude", ".atomic-index", "atomic.db")},
		{"WorktreesDir", WorktreesDir(root), filepath.Join(root, ".claude", "worktrees")},
		{"RepoConfigPath", RepoConfigPath(root), filepath.Join(root, ".claude", "atomic.toml")},
		{"RemindersDirLegacy", RemindersDirLegacy(root), filepath.Join(root, ".claude", ".scratchpad", "reminders")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}
