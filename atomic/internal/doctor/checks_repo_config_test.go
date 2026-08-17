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

// The repo config is optional: without one, indexing proceeds unfiltered, so
// absence must read as an informational PASS rather than a WARN.
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

// Unparseable TOML only degrades indexing to unfiltered, so doctor mirrors
// that severity with WARN rather than FAIL.
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

// A zero duration is invalid, not a way to disable the timeout.
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

// writeWikisClaudeMD makes root a <wikis>-registered realm root and returns
// the CLAUDE.md carrying the registration.
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

// Marker and registry make incompatible claims about one directory.
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

func TestCheckRepoConfig_WikisContradiction_EmptyClaudeMDPathSkips(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeRepoConfig(t, root, "scope = \"repo\"\n")
	// A contradicting registration exists, but an empty ClaudeMDPath must
	// stop the sub-check from ever consulting it.
	writeWikisClaudeMD(t, root)

	r := doctor.RunCheckRepoConfig(doctor.Opts{RepoRoot: root})
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (sub-check skipped, empty ClaudeMDPath); detail: %s", r.Severity, r.Detail)
	}
}

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

// The two comparison sides reach one directory through different literal
// forms, as they do in production: gitToplevelFn resolves symlinks while
// <wikis> entries stay as written. Every other test here builds both sides
// from the same t.TempDir() string, so only this one can tell a symlink-aware
// comparison from an Abs+Clean-only one.
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

	// Files live under the real directory; RepoRoot mirrors gitToplevelFn's
	// resolved output.
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
	// Registered under the symlinked alias, unresolved, as in production.
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

// The silence here is deliberate, not a gap: the marker outranks <wikis>, and
// absence from <wikis> only costs the realm its staleness nudge.
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

// RunCheckRepoConfigWith stays root-only so its callers need no ClaudeMDPath.
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

// A repo-config problem degrades indexing to unfiltered; it never blocks the
// doctor run.
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
