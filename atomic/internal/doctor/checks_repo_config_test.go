package doctor_test

import (
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
