package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadRepoConfig_Parse: a well-formed [code] ignore array loads correctly.
func TestLoadRepoConfig_Parse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.toml")
	content := "[code]\nignore = [\"vendor/**\", \"*.min.js\"]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := LoadRepoConfig(path)
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	want := []string{"vendor/**", "*.min.js"}
	if len(cfg.Code.Ignore) != len(want) {
		t.Fatalf("Code.Ignore = %v, want %v", cfg.Code.Ignore, want)
	}
	for i, p := range want {
		if cfg.Code.Ignore[i] != p {
			t.Errorf("Code.Ignore[%d] = %q, want %q", i, cfg.Code.Ignore[i], p)
		}
	}
}

// TestLoadRepoConfig_MissingFile: an absent config file must produce
// discovery output identical to today — empty RepoConfig, no warnings, no error.
func TestLoadRepoConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.toml")

	cfg, warns, err := LoadRepoConfig(path)
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if warns != nil {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if len(cfg.Code.Ignore) != 0 {
		t.Errorf("Code.Ignore = %v, want empty", cfg.Code.Ignore)
	}
}

// TestLoadRepoConfig_UnknownTopLevelKeyWarns: an unrecognized top-level
// table produces a warning but does not fail the load or drop known data.
func TestLoadRepoConfig_UnknownTopLevelKeyWarns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.toml")
	content := "[code]\nignore = [\"vendor/**\"]\n[bogus]\nkey = \"value\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := LoadRepoConfig(path)
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if len(warns) != 1 {
		t.Fatalf("warns = %v, want exactly 1", warns)
	}
	if !strings.Contains(warns[0].Message, "bogus") {
		t.Errorf("warning %q does not mention unknown key %q", warns[0].Message, "bogus")
	}
	if len(cfg.Code.Ignore) != 1 || cfg.Code.Ignore[0] != "vendor/**" {
		t.Errorf("Code.Ignore = %v, want [vendor/**]", cfg.Code.Ignore)
	}
}

// TestLoadRepoConfig_UnknownLeafKeyWarns: an unrecognized key nested inside
// a known section ([code]) also warns, dotted-path included.
func TestLoadRepoConfig_UnknownLeafKeyWarns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.toml")
	content := "[code]\nignore = [\"vendor/**\"]\nbogus_leaf = true\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, warns, err := LoadRepoConfig(path)
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0].Message, "code.bogus_leaf") {
		t.Errorf("warns = %v, want one warning mentioning code.bogus_leaf", warns)
	}
}

// TestLoadRepoConfig_MalformedTOML: unparseable TOML returns an error the
// caller can degrade on (indexing proceeds unfiltered) — never a panic.
func TestLoadRepoConfig_MalformedTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.toml")
	content := "[code\nignore = [\"vendor/**\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := LoadRepoConfig(path)
	if err == nil {
		t.Fatal("LoadRepoConfig should error on malformed TOML, got nil")
	}
}

// TestLoadRepoConfig_WrongTypedIgnore: a non-array ignore value is treated
// as malformed (decode error), never a panic.
func TestLoadRepoConfig_WrongTypedIgnore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.toml")
	content := "[code]\nignore = \"vendor/**\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := LoadRepoConfig(path)
	if err == nil {
		t.Fatal("LoadRepoConfig should error on wrong-typed ignore, got nil")
	}
}

// TestIgnoreMatcher: table-driven coverage of the matcher semantics in
// docs/spec/graphignore.md SC3.
func TestIgnoreMatcher(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		path     string
		want     bool
	}{
		{"doublestar full-path match", []string{"atomic/internal/serve/assets/vendor/**"}, "atomic/internal/serve/assets/vendor/mermaid.min.js", true},
		{"doublestar full-path no match", []string{"atomic/internal/serve/assets/vendor/**"}, "atomic/internal/serve/other.go", false},
		{"basename match at any depth", []string{"*.min.js"}, "a/b/lib.min.js", true},
		{"basename no match", []string{"*.min.js"}, "a/b/lib.js", false},
		{"leading dot-slash stripped from pattern", []string{"./vendor/**"}, "vendor/lib.js", true},
		{"trailing-slash-only pattern matches nothing", []string{"vendor/"}, "vendor/lib.js", false},
		{"invalid pattern never matches everything", []string{"vendor[/**"}, "vendor/lib.js", false},
		{"invalid pattern does not leak into matching arbitrary paths", []string{"vendor[/**"}, "anything/at/all.go", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := NewIgnoreMatcher(tc.patterns)
			if got := m.Match(tc.path); got != tc.want {
				t.Errorf("Match(%q) with patterns %v = %v, want %v", tc.path, tc.patterns, got, tc.want)
			}
		})
	}
}

// TestNewIgnoreMatcher_InvalidPatternWarns: an invalid glob pattern is
// surfaced as a Warning the caller can report, and the remaining valid
// patterns still match — one bad pattern does not disable the rest.
func TestNewIgnoreMatcher_InvalidPatternWarns(t *testing.T) {
	m, warns := NewIgnoreMatcher([]string{"vendor[/**", "*.min.js"})
	if len(warns) != 1 {
		t.Fatalf("warns = %v, want exactly 1", warns)
	}
	if !strings.Contains(warns[0].Message, "vendor[/**") {
		t.Errorf("warning %q does not mention the invalid pattern", warns[0].Message)
	}
	if !m.Match("a/b/lib.min.js") {
		t.Error("remaining valid pattern should still match")
	}
}

// TestIgnoreMatcher_NilSafe: a nil matcher matches nothing rather than
// panicking — callers may pass a nil matcher when config load fails.
func TestIgnoreMatcher_NilSafe(t *testing.T) {
	var m *IgnoreMatcher
	if m.Match("anything.go") {
		t.Error("nil matcher should match nothing")
	}
}

// TestRepoConfigPath: joins projectRoot with the harness dir and the fixed
// repo-relative suffix. Uses the test seam so it never touches the real home
// (RepoConfigPath is harness-dir-aware as of CP2; see harness.go).
func TestRepoConfigPath(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	got := RepoConfigPath("/repo")
	want := filepath.Join("/repo", ".claude", "atomic.toml")
	if got != want {
		t.Errorf("RepoConfigPath(%q) = %q, want %q", "/repo", got, want)
	}
}

// TestIgnoreMatcher_PatternCount: counts only successfully compiled
// patterns — an invalid pattern is dropped by NewIgnoreMatcher and must not
// be counted as active. A nil matcher (config load failed) counts 0.
func TestIgnoreMatcher_PatternCount(t *testing.T) {
	m, _ := NewIgnoreMatcher([]string{"vendor/**", "*.min.js", "vendor[/**"})
	if got := m.PatternCount(); got != 2 {
		t.Errorf("PatternCount() = %d, want 2 (invalid pattern excluded)", got)
	}

	var nilM *IgnoreMatcher
	if got := nilM.PatternCount(); got != 0 {
		t.Errorf("nil matcher PatternCount() = %d, want 0", got)
	}
}
