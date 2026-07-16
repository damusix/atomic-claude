package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveHarnessDirFromHome_Default: no config.toml present in home →
// falls back to the built-in default. Hermetic — never touches os.UserHomeDir.
func TestResolveHarnessDirFromHome_Default(t *testing.T) {
	home := t.TempDir()
	got := resolveHarnessDirFromHome(home)
	if got != harnessDirDefault {
		t.Errorf("resolveHarnessDirFromHome(empty home) = %q, want %q", got, harnessDirDefault)
	}
}

// TestResolveHarnessDirFromHome_RealConfigFile: a real config.toml with
// harness.dir = ".pi" written to a temp home resolves to ".pi" — exercises
// the actual Load path, not just the seam.
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

// TestResolveHarnessDirFromHome_MalformedConfig: unparseable config.toml
// falls back to the default rather than propagating an error — the resolver
// is lenient on any load error.
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

// TestResolveHarnessDirFromHome_InvalidStoredValueFallsBack: a config.toml
// carrying a harness.dir value that would fail validateHarnessDir (written
// directly to disk, bypassing Set's validation) must fall back to the
// built-in default rather than passing an unvalidated value through to
// filepath.Join in the repo-local helpers — a path-escape risk for values
// like "..".
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

// TestSetHarnessDirForTest_Override: the seam makes harnessDir() return the
// overridden value without touching the real home or the process cache.
func TestSetHarnessDirForTest_Override(t *testing.T) {
	restore := SetHarnessDirForTest(".pi")
	defer restore()

	if got := harnessDir(); got != ".pi" {
		t.Errorf("harnessDir() under seam = %q, want \".pi\"", got)
	}
}

// TestSetHarnessDirForTest_Restore: the restore func returned by a nested
// SetHarnessDirForTest call puts back the previous override.
func TestSetHarnessDirForTest_Restore(t *testing.T) {
	restoreOuter := SetHarnessDirForTest(".pi")
	defer restoreOuter()

	restoreInner := SetHarnessDirForTest(".foo")
	if got := harnessDir(); got != ".foo" {
		t.Fatalf("harnessDir() under inner seam = %q, want \".foo\"", got)
	}

	restoreInner()
	if got := harnessDir(); got != ".pi" {
		t.Errorf("after inner restore, harnessDir() = %q, want \".pi\"", got)
	}
}

// TestRepoLocalHelpers_UnderNonDefaultHarnessDir: every repo-local path
// helper joins root + the overridden harness.dir + its fixed suffix.
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
		{"RemindersDir", RemindersDir(root), filepath.Join(root, ".pi", ".scratchpad", "reminders")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// TestRepoLocalHelpers_DefaultHarnessDir: with the seam explicitly set to the
// built-in default, every helper resolves exactly under ".claude" — matches
// today's layout byte-for-byte.
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
		{"RemindersDir", RemindersDir(root), filepath.Join(root, ".claude", ".scratchpad", "reminders")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}
