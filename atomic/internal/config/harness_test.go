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

// TestResolveHarnessDir_AtomicHarnessEnv: ATOMIC_HARNESS names the harness
// directly (no leading dot) and wins over everything else.
func TestResolveHarnessDir_AtomicHarnessEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ATOMIC_HARNESS", "pi")
	got := resolveHarnessDir(home)
	if got != ".pi" {
		t.Errorf("resolveHarnessDir(ATOMIC_HARNESS=pi) = %q, want \".pi\"", got)
	}
}

// A leading dot in the env value is normalized rather than double-dotted.
func TestResolveHarnessDir_AtomicHarnessEnv_LeadingDotTolerated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ATOMIC_HARNESS", ".pi")
	got := resolveHarnessDir(home)
	if got != ".pi" {
		t.Errorf("resolveHarnessDir(ATOMIC_HARNESS=.pi) = %q, want \".pi\"", got)
	}
}

// An invalid ATOMIC_HARNESS falls through to the next rung rather than erroring.
// Both fingerprint envs are cleared so ambient ones cannot mask the fallthrough
// (this suite may itself run under a harness), and the landing rung is made
// observable with a config value no fingerprint rung can produce.
func TestResolveHarnessDir_AtomicHarnessEnv_InvalidFallsThrough(t *testing.T) {
	cases := []string{"foo/bar", "..", "."}
	for _, invalid := range cases {
		t.Run(invalid, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("ATOMIC_HARNESS", invalid)
			t.Setenv("PI_CODING_AGENT", "")
			t.Setenv("CLAUDECODE", "")
			cfg := Default()
			if err := Set(cfg, "harness.dir", ".pi"); err != nil {
				t.Fatalf("Set: %v", err)
			}
			if err := WritePersist(TOMLPath(home), cfg); err != nil {
				t.Fatalf("WritePersist: %v", err)
			}
			got := resolveHarnessDir(home)
			if got != ".pi" {
				t.Errorf("resolveHarnessDir(ATOMIC_HARNESS=%q) = %q, want config fallthrough %q", invalid, got, ".pi")
			}
		})
	}
}

func TestResolveHarnessDir_PiFingerprint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PI_CODING_AGENT", "true")
	got := resolveHarnessDir(home)
	if got != ".pi" {
		t.Errorf("resolveHarnessDir(PI_CODING_AGENT=true) = %q, want \".pi\"", got)
	}
}

func TestResolveHarnessDir_ClaudeFingerprint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDECODE", "1")
	got := resolveHarnessDir(home)
	if got != ".claude" {
		t.Errorf("resolveHarnessDir(CLAUDECODE=1) = %q, want \".claude\"", got)
	}
}

func TestResolveHarnessDir_AtomicHarnessBeatsFingerprints(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ATOMIC_HARNESS", "custom")
	t.Setenv("PI_CODING_AGENT", "true")
	t.Setenv("CLAUDECODE", "1")
	got := resolveHarnessDir(home)
	if got != ".custom" {
		t.Errorf("resolveHarnessDir = %q, want \".custom\"", got)
	}
}

// The nested-harness case: pi launched from within Claude Code exposes both
// fingerprints, and PI wins.
func TestResolveHarnessDir_PiBeatsClaudecodeWhenBothSet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PI_CODING_AGENT", "true")
	t.Setenv("CLAUDECODE", "1")
	got := resolveHarnessDir(home)
	if got != ".pi" {
		t.Errorf("resolveHarnessDir(both fingerprints) = %q, want \".pi\"", got)
	}
}

func TestResolveHarnessDir_FingerprintBeatsConfig(t *testing.T) {
	home := t.TempDir()
	cfg := Default()
	if err := Set(cfg, "harness.dir", ".other"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}
	t.Setenv("PI_CODING_AGENT", "true")
	got := resolveHarnessDir(home)
	if got != ".pi" {
		t.Errorf("resolveHarnessDir(fingerprint + config) = %q, want \".pi\"", got)
	}
}

func TestResolveHarnessDir_ConfigWinsOverDefaultWhenNoEnv(t *testing.T) {
	// Clear ambient env: this suite may itself run under a harness whose
	// fingerprint would otherwise leak in.
	t.Setenv("ATOMIC_HARNESS", "")
	t.Setenv("PI_CODING_AGENT", "")
	t.Setenv("CLAUDECODE", "")
	home := t.TempDir()
	cfg := Default()
	if err := Set(cfg, "harness.dir", ".other"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		t.Fatalf("WritePersist: %v", err)
	}
	got := resolveHarnessDir(home)
	if got != ".other" {
		t.Errorf("resolveHarnessDir(config only) = %q, want \".other\"", got)
	}
}

// The seam makes harnessDir() return the override without touching the real
// home or the process cache.
func TestSetHarnessDirForTest_Override(t *testing.T) {
	restore := SetHarnessDirForTest(".pi")
	defer restore()

	if got := harnessDir(); got != ".pi" {
		t.Errorf("harnessDir() under seam = %q, want \".pi\"", got)
	}
}

// A nested call's restore func puts back the previous override.
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
