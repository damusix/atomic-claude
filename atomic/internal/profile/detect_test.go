package profile_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/profile"
)

// --- Registry ---

// TestRegistry_MinimumSize verifies the registry has at least 45 entries spanning all 7 categories.
func TestRegistry_MinimumSize(t *testing.T) {
	reg := profile.DefaultRegistry()
	if len(reg) < 45 {
		t.Errorf("registry has %d entries, want >= 45", len(reg))
	}
}

// TestRegistry_AllCategories verifies all 7 required categories are present.
func TestRegistry_AllCategories(t *testing.T) {
	reg := profile.DefaultRegistry()
	seen := map[profile.ToolCategory]bool{}
	for _, e := range reg {
		seen[e.Category] = true
	}
	required := []profile.ToolCategory{
		profile.CategoryLanguageRuntime,
		profile.CategoryPackageManager,
		profile.CategoryVersionManager,
		profile.CategoryContainer,
		profile.CategoryMonorepo,
		profile.CategoryCLI,
		profile.CategoryCloud,
	}
	for _, c := range required {
		if !seen[c] {
			t.Errorf("registry missing category %q", c)
		}
	}
}

// TestRegistry_AllStrategiesValid verifies every entry has a valid detection strategy.
func TestRegistry_AllStrategiesValid(t *testing.T) {
	reg := profile.DefaultRegistry()
	for _, e := range reg {
		switch e.Strategy {
		case profile.StrategyBinary, profile.StrategyDirectory, profile.StrategyBoth:
			// ok
		default:
			t.Errorf("entry %q has invalid strategy %q", e.Name, e.Strategy)
		}
	}
}

// --- Detection: directory strategy finds tool when LookPath would fail ---

// TestDetect_DirectoryFallback verifies that a version manager without a binary
// on PATH is detected when its install directory exists.
func TestDetect_DirectoryFallback(t *testing.T) {
	home := t.TempDir()

	// Create ~/.nvm to simulate nvm installed as shell function (not on PATH).
	if err := os.MkdirAll(filepath.Join(home, ".nvm"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := profile.DetectOptions{Home: home}
	results := profile.DetectAll(opts)

	// Find nvm in results.
	var nvmResult *profile.ToolResult
	for i := range results {
		if results[i].Name == "nvm" {
			nvmResult = &results[i]
			break
		}
	}
	if nvmResult == nil {
		t.Fatal("nvm not found in DetectAll results")
	}
	if !nvmResult.Installed {
		t.Errorf("nvm: expected Installed=true when ~/.nvm exists, got false")
	}
}

// TestDetect_DirectoryFallback_Absent verifies that when the install dir is absent
// and the binary is not on PATH, the tool is reported as not installed.
func TestDetect_DirectoryFallback_Absent(t *testing.T) {
	home := t.TempDir() // empty — no .nvm, no .sdkman, etc.

	opts := profile.DetectOptions{Home: home}
	results := profile.DetectAll(opts)

	// Verify nvm is absent.
	for _, r := range results {
		if r.Name == "nvm" && r.Installed {
			t.Errorf("nvm: expected Installed=false in empty home, got true")
		}
	}
}

// TestDetect_SDKManDirectory verifies sdkman is detected via ~/.sdkman directory.
func TestDetect_SDKManDirectory(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".sdkman"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := profile.DetectOptions{Home: home}
	results := profile.DetectAll(opts)

	for _, r := range results {
		if r.Name == "sdkman" {
			if !r.Installed {
				t.Errorf("sdkman: expected Installed=true when ~/.sdkman exists")
			}
			return
		}
	}
	t.Error("sdkman not found in DetectAll results")
}

// --- Version capture ---

// TestVersionCapture_TrimmedFirstLine verifies that version output is trimmed
// to the first line with no leading/trailing whitespace.
func TestVersionCapture_TrimmedFirstLine(t *testing.T) {
	// We use "go version" which is reliably available in the test environment.
	v := profile.CaptureVersion("go", []string{"version"})
	if v == "" {
		t.Error("CaptureVersion returned empty string for 'go version'")
	}
	// Must be single line — no newlines.
	for _, ch := range v {
		if ch == '\n' || ch == '\r' {
			t.Errorf("CaptureVersion returned multi-line output: %q", v)
			break
		}
	}
}

// TestVersionCapture_ErrorYieldsUnknown verifies that a binary that exists but
// whose version command fails returns "unknown" rather than panicking or empty.
func TestVersionCapture_ErrorYieldsUnknown(t *testing.T) {
	// Pass a non-existent binary name — LookPath will fail, should return "unknown".
	v := profile.CaptureVersion("__nonexistent_tool_xyz__", []string{"--version"})
	if v != "unknown" {
		t.Errorf("CaptureVersion with bad binary: got %q, want %q", v, "unknown")
	}
}

// TestVersionCapture_NonZeroExitWithOutputYieldsUnknown verifies that when a
// command exits non-zero AND produces output, the output text is NOT recorded
// as the version. This guards against rustup/kubectl-style error messages
// (e.g. "error: rustup could not choose a version...") being stored in profile.md.
// The fix for F-3: non-zero exit → "unknown" regardless of output.
func TestVersionCapture_NonZeroExitWithOutputYieldsUnknown(t *testing.T) {
	// Use a shell command that exits non-zero but produces stdout+stderr output.
	// "false" exits with code 1 and produces no output, but we need output too.
	// Use "sh -c 'echo error: some error text; exit 1'" to simulate the rustup pattern.
	v := profile.CaptureVersion("sh", []string{"-c", "echo 'error: rustup could not choose a version'; exit 1"})
	if v != "unknown" {
		t.Errorf("CaptureVersion with non-zero exit and output: got %q, want %q", v, "unknown")
	}
}

// TestDetectAll_OrderDeterministic verifies that DetectAll returns results in
// stable registry order across multiple calls, even under concurrent execution.
// This guards F-2: bounded parallel DetectAll must produce identical ordering.
func TestDetectAll_OrderDeterministic(t *testing.T) {
	opts := profile.DetectOptions{Home: t.TempDir()}

	reg := profile.DefaultRegistry()

	// Run DetectAll twice and confirm the names appear in registry order both times.
	results1 := profile.DetectAll(opts)
	results2 := profile.DetectAll(opts)

	if len(results1) != len(reg) {
		t.Fatalf("DetectAll run 1: got %d results, want %d", len(results1), len(reg))
	}
	if len(results2) != len(reg) {
		t.Fatalf("DetectAll run 2: got %d results, want %d", len(results2), len(reg))
	}

	// Order must match registry order on both runs.
	for i, e := range reg {
		if results1[i].Name != e.Name {
			t.Errorf("run 1 index %d: got name %q, want %q (registry order)", i, results1[i].Name, e.Name)
		}
		if results2[i].Name != e.Name {
			t.Errorf("run 2 index %d: got name %q, want %q (registry order)", i, results2[i].Name, e.Name)
		}
	}
}

// --- Source-class classification ---

// TestClassifySource_VersionManager verifies shim paths yield "version-manager".
func TestClassifySource_VersionManager(t *testing.T) {
	cases := []struct {
		path string
	}{
		{"/home/user/.pyenv/shims/python"},
		{"/home/user/.asdf/shims/node"},
		{"/home/user/.nvm/versions/node/v20.0.0/bin/node"},
		{"/home/user/.rbenv/shims/ruby"},
	}
	for _, c := range cases {
		got := profile.ClassifySource(c.path)
		if got != profile.SourceVersionManager {
			t.Errorf("ClassifySource(%q) = %q, want %q", c.path, got, profile.SourceVersionManager)
		}
	}
}

// TestClassifySource_Homebrew verifies Homebrew paths are classified correctly.
func TestClassifySource_Homebrew(t *testing.T) {
	cases := []string{
		"/opt/homebrew/bin/python3",
		"/opt/homebrew/opt/python/bin/python3",
		"/usr/local/Cellar/python/3.12/bin/python3",
		"/home/linuxbrew/.linuxbrew/bin/node",
	}
	for _, p := range cases {
		got := profile.ClassifySource(p)
		if got != profile.SourceHomebrew {
			t.Errorf("ClassifySource(%q) = %q, want %q", p, got, profile.SourceHomebrew)
		}
	}
}

// TestClassifySource_System verifies system paths are classified correctly.
func TestClassifySource_System(t *testing.T) {
	cases := []string{
		"/usr/bin/python3",
		"/bin/bash",
		// /usr/local/bin is system when NOT under Homebrew Cellar/opt
		"/usr/local/bin/git",
	}
	for _, p := range cases {
		got := profile.ClassifySource(p)
		if got != profile.SourceSystem {
			t.Errorf("ClassifySource(%q) = %q, want %q", p, got, profile.SourceSystem)
		}
	}
}

// TestClassifySource_Other verifies an arbitrary path yields "other".
func TestClassifySource_Other(t *testing.T) {
	got := profile.ClassifySource("/home/user/.cargo/bin/rustc")
	if got != profile.SourceOther {
		t.Errorf("ClassifySource(%q) = %q, want %q", "/home/user/.cargo/bin/rustc", got, profile.SourceOther)
	}
}

// --- Shell enumeration ---

// TestShellEnumeration_LoginShell verifies $SHELL is captured.
func TestShellEnumeration_LoginShell(t *testing.T) {
	opts := profile.ShellEnvOptions{
		Shell: "/bin/zsh",
		Home:  t.TempDir(),
	}
	result := profile.DetectShell(opts)
	if result.LoginShell != "/bin/zsh" {
		t.Errorf("LoginShell = %q, want %q", result.LoginShell, "/bin/zsh")
	}
}

// TestShellEnumeration_OhMyZsh verifies oh-my-zsh detection via ~/.oh-my-zsh.
func TestShellEnumeration_OhMyZsh(t *testing.T) {
	home := t.TempDir()
	ommzDir := filepath.Join(home, ".oh-my-zsh")
	if err := os.MkdirAll(ommzDir, 0o755); err != nil {
		t.Fatal(err)
	}

	opts := profile.ShellEnvOptions{Shell: "/bin/zsh", Home: home}
	result := profile.DetectShell(opts)
	if result.Framework != "oh-my-zsh" {
		t.Errorf("Framework = %q, want %q", result.Framework, "oh-my-zsh")
	}
}

// TestShellEnumeration_Prezto verifies prezto detection via ~/.zprezto.
func TestShellEnumeration_Prezto(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".zprezto"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := profile.ShellEnvOptions{Shell: "/bin/zsh", Home: home}
	result := profile.DetectShell(opts)
	if result.Framework != "prezto" {
		t.Errorf("Framework = %q, want %q", result.Framework, "prezto")
	}
}

// TestShellEnumeration_NoFramework verifies empty framework when none installed.
// The LookPath seam is injected to prevent false positives when the test runner
// has starship on its real PATH.
func TestShellEnumeration_NoFramework(t *testing.T) {
	home := t.TempDir() // empty

	// Seam: always report starship as not found, regardless of real PATH.
	neverFound := func(string) (string, error) {
		return "", fmt.Errorf("not found")
	}

	opts := profile.ShellEnvOptions{Shell: "/bin/bash", Home: home, LookPath: neverFound}
	result := profile.DetectShell(opts)
	if result.Framework != "" {
		t.Errorf("Framework = %q, want empty (no framework installed)", result.Framework)
	}
}

// TestShellEnumeration_OhMyZshPlugins verifies plugin enumeration under custom/plugins/.
func TestShellEnumeration_OhMyZshPlugins(t *testing.T) {
	home := t.TempDir()
	customPlugins := filepath.Join(home, ".oh-my-zsh", "custom", "plugins")
	if err := os.MkdirAll(filepath.Join(customPlugins, "myplugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(customPlugins, "anotherplugin"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := profile.ShellEnvOptions{Shell: "/bin/zsh", Home: home}
	result := profile.DetectShell(opts)

	pluginSet := map[string]bool{}
	for _, p := range result.OhMyZshPlugins {
		pluginSet[p] = true
	}
	if !pluginSet["myplugin"] {
		t.Errorf("OhMyZshPlugins missing 'myplugin'; got: %v", result.OhMyZshPlugins)
	}
	if !pluginSet["anotherplugin"] {
		t.Errorf("OhMyZshPlugins missing 'anotherplugin'; got: %v", result.OhMyZshPlugins)
	}
}

// TestShellEnumeration_OhMyZshThemes verifies theme enumeration under custom/themes/.
func TestShellEnumeration_OhMyZshThemes(t *testing.T) {
	home := t.TempDir()
	customThemes := filepath.Join(home, ".oh-my-zsh", "custom", "themes")
	if err := os.MkdirAll(customThemes, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a .zsh-theme file (themes are typically files, not dirs).
	if err := os.WriteFile(filepath.Join(customThemes, "mytheme.zsh-theme"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := profile.ShellEnvOptions{Shell: "/bin/zsh", Home: home}
	result := profile.DetectShell(opts)

	themeSet := map[string]bool{}
	for _, th := range result.OhMyZshThemes {
		themeSet[th] = true
	}
	if !themeSet["mytheme.zsh-theme"] {
		t.Errorf("OhMyZshThemes missing 'mytheme.zsh-theme'; got: %v", result.OhMyZshThemes)
	}
}

// TestShellEnumeration_EmptyOhMyZshDirs verifies no panic when custom/ subdirs are absent.
func TestShellEnumeration_EmptyOhMyZshDirs(t *testing.T) {
	home := t.TempDir()
	// Create ~/.oh-my-zsh but no custom/plugins or custom/themes.
	if err := os.MkdirAll(filepath.Join(home, ".oh-my-zsh"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := profile.ShellEnvOptions{Shell: "/bin/zsh", Home: home}
	result := profile.DetectShell(opts) // must not panic
	if result.Framework != "oh-my-zsh" {
		t.Errorf("Framework = %q, want oh-my-zsh", result.Framework)
	}
	if len(result.OhMyZshPlugins) != 0 {
		t.Errorf("expected 0 plugins, got %v", result.OhMyZshPlugins)
	}
	if len(result.OhMyZshThemes) != 0 {
		t.Errorf("expected 0 themes, got %v", result.OhMyZshThemes)
	}
}

// TestDetect_AsdfDirectory verifies asdf detected via $ASDF_DIR or ~/.asdf.
func TestDetect_AsdfDirectory(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".asdf"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := profile.DetectOptions{Home: home}
	results := profile.DetectAll(opts)

	for _, r := range results {
		if r.Name == "asdf" {
			if !r.Installed {
				t.Errorf("asdf: expected Installed=true when ~/.asdf exists")
			}
			return
		}
	}
	t.Error("asdf not found in DetectAll results")
}
