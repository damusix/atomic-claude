package doctor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// writeRepoConfig writes content to <root>/.claude/atomic.toml.
func writeRepoConfig(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeRepoConfig mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "atomic.toml"), []byte(content), 0o644); err != nil {
		t.Fatalf("writeRepoConfig write: %v", err)
	}
}

// TestCheckRepoConfigAbsent is the key opt-in-absence assertion: no
// .claude/atomic.toml must produce PASS (informational), never WARN — the
// repo config is optional and indexing proceeds unfiltered without it.
func TestCheckRepoConfigAbsent(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS when repo config absent", r.Severity)
	}
	if !strings.Contains(r.Detail, "not present") {
		t.Errorf("Detail = %q, want mention of 'not present'", r.Detail)
	}
}

// TestCheckRepoConfigAbsent_UnderNonDefaultHarnessDir verifies the Detail
// string's path derives from the harness-aware resolver — under a ".pi"
// harness dir it must read ".pi/atomic.toml", never the default-harness
// literal ".claude/atomic.toml" (CP2 review finding).
func TestCheckRepoConfigAbsent_UnderNonDefaultHarnessDir(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	root := t.TempDir()
	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS when repo config absent", r.Severity)
	}
	if !strings.Contains(r.Detail, ".pi/atomic.toml") {
		t.Errorf("Detail = %q, want mention of the harness-aware path \".pi/atomic.toml\"", r.Detail)
	}
	if strings.Contains(r.Detail, ".claude/atomic.toml") {
		t.Errorf("Detail = %q, must not show the default-harness literal under a .pi harness", r.Detail)
	}
}

// TestCheckRepoConfigValid_UnderNonDefaultHarnessDir verifies the PASS Detail
// string names the harness-aware path for a well-formed config under ".pi".
func TestCheckRepoConfigValid_UnderNonDefaultHarnessDir(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	root := t.TempDir()
	dir := filepath.Join(root, ".pi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "atomic.toml"), []byte("[code]\nignore = [\"vendor/**\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, ".pi/atomic.toml") {
		t.Errorf("Detail = %q, want mention of \".pi/atomic.toml\"", r.Detail)
	}
}

// TestCheckRepoConfigValid verifies PASS with a pattern-count detail for a
// well-formed config.
func TestCheckRepoConfigValid(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "[code]\nignore = [\"vendor/**\", \"*.min.js\"]\n")

	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (valid config); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "2 ignore pattern") {
		t.Errorf("Detail = %q, want mention of '2 ignore pattern(s)'", r.Detail)
	}
}

// TestCheckRepoConfigMalformed verifies WARN (not FAIL) on unparseable TOML —
// indexing degrades to unfiltered rather than hard-failing, so doctor mirrors
// that severity.
func TestCheckRepoConfigMalformed(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "[code\nignore = [\"vendor/**\"\n")

	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (malformed TOML); detail: %s", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

// TestCheckRepoConfigUnknownKey verifies WARN with the offending key named
// in the detail.
func TestCheckRepoConfigUnknownKey(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "[code]\nignore = [\"vendor/**\"]\n[bogus]\nkey = \"value\"\n")

	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (unknown key); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "bogus") {
		t.Errorf("Detail = %q, want mention of unknown key %q", r.Detail, "bogus")
	}
}

// TestCheckRepoConfigInvalidGlob verifies WARN with the offending pattern
// named in the detail.
func TestCheckRepoConfigInvalidGlob(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "[code]\nignore = [\"vendor[/**\"]\n")

	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (invalid glob pattern); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "vendor[/**") {
		t.Errorf("Detail = %q, want mention of the invalid pattern", r.Detail)
	}
}

// TestCheckRepoConfigValid_ScopeInDetail verifies a valid scope marker is
// named in the PASS detail alongside the ignore-pattern count.
func TestCheckRepoConfigValid_ScopeInDetail(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "scope = \"repo\"\n[code]\nignore = [\"vendor/**\"]\n")

	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "scope=repo") {
		t.Errorf("Detail = %q, want mention of \"scope=repo\"", r.Detail)
	}
}

// TestCheckRepoConfigInvalidScope verifies WARN naming the offending value
// and the two accepted values when scope is present but not "repo"/"realm".
func TestCheckRepoConfigInvalidScope(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "scope = \"bogus\"\n")

	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (invalid scope value); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "bogus") {
		t.Errorf("Detail = %q, want mention of the offending value %q", r.Detail, "bogus")
	}
	if !strings.Contains(r.Detail, "repo") || !strings.Contains(r.Detail, "realm") {
		t.Errorf("Detail = %q, want the two accepted values (repo, realm) named", r.Detail)
	}
}

// TestCheckRepoConfigValid_IdleTimeout verifies PASS for a well-formed
// [repl] idle_timeout.
func TestCheckRepoConfigValid_IdleTimeout(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "[repl]\nidle_timeout = \"2h\"\n")

	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS; detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckRepoConfigInvalidIdleTimeout verifies WARN with the offending
// value named in the detail.
func TestCheckRepoConfigInvalidIdleTimeout(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "[repl]\nidle_timeout = \"bogus\"\n")

	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (invalid idle_timeout); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "bogus") {
		t.Errorf("Detail = %q, want mention of the offending value %q", r.Detail, "bogus")
	}
}

// TestCheckRepoConfigInvalidIdleTimeout_ZeroRejected verifies a zero-duration
// idle_timeout is treated as invalid (WARN), never as "disable".
func TestCheckRepoConfigInvalidIdleTimeout_ZeroRejected(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "[repl]\nidle_timeout = \"0s\"\n")

	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (zero idle_timeout is invalid); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "0s") {
		t.Errorf("Detail = %q, want mention of the offending value %q", r.Detail, "0s")
	}
}

// writeWikisClaudeMD writes <root>/wiki/index.md and a CLAUDE.md whose
// <wikis> block registers it — root becomes a realm root recognized by the
// <wikis> registry, mirroring the fixture pattern in checks_code_index_test.go.
func writeWikisClaudeMD(t *testing.T, root string) string {
	t.Helper()
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("writeWikisClaudeMD mkdir wiki: %v", err)
	}
	wikiIndex := filepath.Join(wikiDir, "index.md")
	if err := os.WriteFile(wikiIndex, []byte("# wiki\n"), 0o644); err != nil {
		t.Fatalf("writeWikisClaudeMD write wiki/index.md: %v", err)
	}

	claudeDir := t.TempDir()
	claudeMD := filepath.Join(claudeDir, "CLAUDE.md")
	block := fmt.Sprintf("<wikis>\n- %s\n</wikis>\n", wikiIndex)
	if err := os.WriteFile(claudeMD, []byte(block), 0o644); err != nil {
		t.Fatalf("writeWikisClaudeMD write CLAUDE.md: %v", err)
	}
	return claudeMD
}

// TestCheckRepoConfig_WikisContradiction verifies the checkRepoConfig
// dispatcher (exercised via RunCheckRepoConfig) WARNs when the marker says
// scope=repo while root is also a <wikis>-registered realm root — two
// mechanisms making incompatible claims about one directory.
func TestCheckRepoConfig_WikisContradiction(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "scope = \"repo\"\n")
	claudeMD := writeWikisClaudeMD(t, root)

	r := doctor.RunCheckRepoConfig(doctor.Opts{RepoRoot: root, ClaudeMDPath: claudeMD})
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (marker/<wikis> contradiction); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "realm") {
		t.Errorf("Detail = %q, want mention of the realm contradiction", r.Detail)
	}
}

// TestCheckRepoConfig_WikisContradiction_EmptyClaudeMDPathSkips verifies the
// sub-check is skipped entirely when opts.ClaudeMDPath is empty — the plain
// RunCheckRepoConfigWith result passes through unchanged.
func TestCheckRepoConfig_WikisContradiction_EmptyClaudeMDPathSkips(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "scope = \"repo\"\n")
	// A realm registration exists at root, but ClaudeMDPath is left empty so
	// the sub-check must never consult it.
	writeWikisClaudeMD(t, root)

	r := doctor.RunCheckRepoConfig(doctor.Opts{RepoRoot: root})
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (sub-check skipped, empty ClaudeMDPath); detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckRepoConfig_WikisContradiction_ScopeRealmNoWarn verifies scope=realm
// at a <wikis>-registered realm root is NOT a contradiction — marker and
// registry agree, so no WARN fires.
func TestCheckRepoConfig_WikisContradiction_ScopeRealmNoWarn(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "scope = \"realm\"\n")
	claudeMD := writeWikisClaudeMD(t, root)

	r := doctor.RunCheckRepoConfig(doctor.Opts{RepoRoot: root, ClaudeMDPath: claudeMD})
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (scope=realm agrees with <wikis>); detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckRepoConfig_WikisContradiction_SymlinkedRoot verifies the
// marker/<wikis> contradiction check fires when the two comparison paths
// reach the same directory through different literal forms — the exact
// production divergence: gitToplevelFn resolves symlinks (opts.RepoRoot
// arrives already resolved) while wiki.ReadWikiIndexPaths returns each
// <wikis> entry exactly as written (commonly the symlinked form, e.g. macOS
// /tmp vs. its real /private/tmp target). Every other test in this file
// builds both comparison sides from the same t.TempDir() string, so they are
// always textually identical and cannot distinguish a correct symlink-aware
// comparison from a broken Abs+Clean-only one — this test can.
func TestCheckRepoConfig_WikisContradiction_SymlinkedRoot(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	symDir := filepath.Join(base, "sym")
	if err := os.Symlink(realDir, symDir); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	// The repo config and wiki/index.md physically live under the real
	// directory; opts.RepoRoot mirrors gitToplevelFn's resolved output.
	writeRepoConfig(t, realDir, "scope = \"repo\"\n")

	wikiDir := filepath.Join(realDir, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wikiDir, "index.md"), []byte("# wiki\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	claudeDir := t.TempDir()
	claudeMD := filepath.Join(claudeDir, "CLAUDE.md")
	// Registered via the symlinked alias, not the resolved real path — this
	// is what wiki.ReadWikiIndexPaths returns unresolved in production.
	block := fmt.Sprintf("<wikis>\n- %s\n</wikis>\n", filepath.Join(symDir, "wiki", "index.md"))
	if err := os.WriteFile(claudeMD, []byte(block), 0o644); err != nil {
		t.Fatal(err)
	}

	r := doctor.RunCheckRepoConfig(doctor.Opts{RepoRoot: realDir, ClaudeMDPath: claudeMD})
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (marker/<wikis> contradiction across a symlink boundary); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "realm") {
		t.Errorf("Detail = %q, want mention of the realm contradiction", r.Detail)
	}
}

// TestCheckRepoConfig_ScopeRealm_NoWikisRegistrationsSilent locks design
// decision 1: a marker-declared realm absent from every <wikis> registration
// produces no WARN — deliberate (marker outranks <wikis>; absence from
// <wikis> only means it gets no staleness nudge), not a gap. A future
// refactor that starts warning here would be a regression this test catches.
func TestCheckRepoConfig_ScopeRealm_NoWikisRegistrationsSilent(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "scope = \"realm\"\n")

	claudeDir := t.TempDir()
	claudeMD := filepath.Join(claudeDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte("# no <wikis> block\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := doctor.RunCheckRepoConfig(doctor.Opts{RepoRoot: root, ClaudeMDPath: claudeMD})
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (scope=realm with zero <wikis> registrations is silent per decision 1); detail: %s", r.Severity, r.Detail)
	}
}

// TestRunCheckRepoConfigWith_NoWikisSubCheck verifies RunCheckRepoConfigWith
// stays root-only: even when root is a <wikis>-registered realm root marked
// scope=repo, calling it directly never runs the contradiction sub-check —
// existing callers and tests need no ClaudeMDPath plumbing.
func TestRunCheckRepoConfigWith_NoWikisSubCheck(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "scope = \"repo\"\n")
	writeWikisClaudeMD(t, root)

	r := doctor.RunCheckRepoConfigWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (RunCheckRepoConfigWith is root-only); detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckRepoConfigNeverFail asserts the check never produces FAIL,
// mirroring the code-index check's opt-in contract: a repo config problem
// degrades indexing to unfiltered, it never blocks doctor with a hard FAIL.
func TestCheckRepoConfigNeverFail(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	cases := []struct {
		name    string
		content string
	}{
		{"absent", ""},
		{"malformed", "[code\nignore = [\"vendor/**\"\n"},
		{"unknown key", "[code]\nignore = [\"vendor/**\"]\n[bogus]\nkey = \"value\"\n"},
		{"invalid glob", "[code]\nignore = [\"vendor[/**\"]\n"},
		{"invalid idle_timeout", "[repl]\nidle_timeout = \"bogus\"\n"},
		{"valid", "[code]\nignore = [\"vendor/**\"]\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.content != "" {
				writeRepoConfig(t, root, tc.content)
			}
			r := doctor.RunCheckRepoConfigWith(root)
			if r.Severity == doctor.FAIL {
				t.Errorf("severity = FAIL, want PASS or WARN (repo-config check must never FAIL)")
			}
		})
	}
}
