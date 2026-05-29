package profile_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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

// TestClassifySource_VersionManagerNamed verifies that paths under known version-manager
// dirs return the manager's name, not the generic "version-manager" label.
func TestClassifySource_VersionManagerNamed(t *testing.T) {
	cases := []struct {
		path string
		want profile.SourceClass
	}{
		{"/home/user/.pyenv/shims/python", "pyenv"},
		{"/home/user/.pyenv/versions/3.12/bin/python", "pyenv"},
		{"/home/user/.asdf/shims/node", "asdf"},
		{"/home/user/.asdf/installs/nodejs/20/bin/node", "asdf"},
		{"/home/user/.nvm/versions/node/v20.0.0/bin/node", "nvm"},
		{"/home/user/.rbenv/shims/ruby", "rbenv"},
		{"/home/user/.rbenv/versions/3.2.0/bin/ruby", "rbenv"},
		{"/home/user/.volta/bin/node", "volta"},
		{"/home/user/.volta/tools/image/node/20/bin/node", "volta"},
		{"/home/user/.fnm/node-versions/v20/bin/node", "fnm"},
		{"/home/user/.local/share/mise/installs/python/3.12/bin/python", "mise"},
		{"/home/user/.rustup/toolchains/stable/bin/rustc", "rustup"},
	}
	for _, c := range cases {
		got := profile.ClassifySource(c.path)
		if got != c.want {
			t.Errorf("ClassifySource(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestClassifySource_Homebrew verifies Homebrew paths are classified as "brew".
func TestClassifySource_Homebrew(t *testing.T) {
	cases := []string{
		"/opt/homebrew/bin/python3",
		"/opt/homebrew/opt/python/bin/python3",
		"/usr/local/Cellar/python/3.12/bin/python3",
		"/home/linuxbrew/.linuxbrew/bin/node",
	}
	for _, p := range cases {
		got := profile.ClassifySource(p)
		if got != profile.SourceBrew {
			t.Errorf("ClassifySource(%q) = %q, want %q", p, got, profile.SourceBrew)
		}
	}
}

// TestClassifySource_System verifies system paths are classified as "sys".
func TestClassifySource_System(t *testing.T) {
	cases := []string{
		"/usr/bin/python3",
		"/bin/bash",
		// /usr/local/bin is system when NOT under Homebrew Cellar/opt
		"/usr/local/bin/git",
	}
	for _, p := range cases {
		got := profile.ClassifySource(p)
		if got != profile.SourceSys {
			t.Errorf("ClassifySource(%q) = %q, want %q", p, got, profile.SourceSys)
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

// --- Version-line prefix ---

// TestCaptureVersion_ElixirPrefix verifies that when elixir-style output (Erlang
// banner first, then "Elixir 1.18.3 ...") is fed through a fake binary, the
// captured version is the Elixir line, not the banner.
//
// We simulate this by using "sh -c" to echo the multi-line output. The real
// elixir binary is not required.
func TestCaptureVersion_ElixirStylePrefix(t *testing.T) {
	// Simulate elixir --version: Erlang banner, blank line, Elixir version line.
	script := `echo 'Erlang/OTP 27 [erts-15.0] [source] [64-bit] [smp:10:10]'
echo ''
echo 'Elixir 1.18.3 (compiled with Erlang/OTP 27)'`
	v := profile.CaptureVersionWithPrefix("sh", []string{"-c", script}, "Elixir")
	if v != "Elixir 1.18.3 (compiled with Erlang/OTP 27)" {
		t.Errorf("CaptureVersionWithPrefix: got %q, want Elixir 1.18.3 line", v)
	}
}

func TestCaptureVersion_MixStylePrefix(t *testing.T) {
	script := `echo 'Erlang/OTP 27 [erts-15.0] [source] [64-bit]'
echo ''
echo 'Mix 1.18.3 (compiled with Erlang/OTP 27)'`
	v := profile.CaptureVersionWithPrefix("sh", []string{"-c", script}, "Mix")
	if v != "Mix 1.18.3 (compiled with Erlang/OTP 27)" {
		t.Errorf("CaptureVersionWithPrefix: got %q, want Mix 1.18.3 line", v)
	}
}

// TestCaptureVersion_PrefixFallsBackToUnknown verifies that when no line matches
// the prefix, "unknown" is returned rather than the banner or empty string.
func TestCaptureVersion_PrefixNoMatch(t *testing.T) {
	script := `echo 'Erlang/OTP 27 [erts-15.0]'`
	v := profile.CaptureVersionWithPrefix("sh", []string{"-c", script}, "Elixir")
	if v != "unknown" {
		t.Errorf("CaptureVersionWithPrefix no-match: got %q, want \"unknown\"", v)
	}
}

// TestDetectEntry_ElixirUsesPrefix verifies that the elixir registry entry now
// has a VersionLinePrefix and that its VersionArgs are non-nil.
func TestDetectEntry_ElixirAndMixHavePrefix(t *testing.T) {
	reg := profile.DefaultRegistry()
	for _, e := range reg {
		switch e.Name {
		case "elixir":
			if e.VersionArgs == nil {
				t.Error("elixir: VersionArgs should be non-nil after v2.1 reversal")
			}
			if e.VersionLinePrefix == "" {
				t.Error("elixir: VersionLinePrefix should be set to 'Elixir'")
			}
		case "mix":
			if e.VersionArgs == nil {
				t.Error("mix: VersionArgs should be non-nil after v2.1 reversal")
			}
			if e.VersionLinePrefix == "" {
				t.Error("mix: VersionLinePrefix should be set to 'Mix'")
			}
		}
	}
}

// --- oh-my-zsh custom scripts ---

// TestShellEnumeration_OhMyZshCustomScripts verifies that top-level *.zsh files
// under ~/.oh-my-zsh/custom/ are enumerated in CustomScripts.
func TestShellEnumeration_OhMyZshCustomScripts(t *testing.T) {
	home := t.TempDir()
	customDir := filepath.Join(home, ".oh-my-zsh", "custom")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create two .zsh files and one non-.zsh file (should be ignored).
	for _, name := range []string{"aliases.zsh", "functions.zsh", "README.md"} {
		if err := os.WriteFile(filepath.Join(customDir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	opts := profile.ShellEnvOptions{Shell: "/bin/zsh", Home: home}
	result := profile.DetectShell(opts)

	scriptSet := map[string]bool{}
	for _, s := range result.CustomScripts {
		scriptSet[s] = true
	}
	if !scriptSet["aliases.zsh"] {
		t.Errorf("CustomScripts missing 'aliases.zsh'; got: %v", result.CustomScripts)
	}
	if !scriptSet["functions.zsh"] {
		t.Errorf("CustomScripts missing 'functions.zsh'; got: %v", result.CustomScripts)
	}
	if scriptSet["README.md"] {
		t.Errorf("CustomScripts should not include non-.zsh file 'README.md'; got: %v", result.CustomScripts)
	}
}

// TestShellEnumeration_NoCustomScriptsWhenAbsent verifies CustomScripts is nil/empty
// when custom/*.zsh files are absent.
func TestShellEnumeration_NoCustomScriptsWhenAbsent(t *testing.T) {
	home := t.TempDir()
	// Only the .oh-my-zsh dir, no custom/*.zsh files.
	if err := os.MkdirAll(filepath.Join(home, ".oh-my-zsh"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := profile.ShellEnvOptions{Shell: "/bin/zsh", Home: home}
	result := profile.DetectShell(opts)

	if len(result.CustomScripts) != 0 {
		t.Errorf("expected 0 CustomScripts, got %v", result.CustomScripts)
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

// --- Per-tool detection timeout ---

// TestCaptureVersion_TimeoutYieldsUnknown verifies that a version command that hangs
// longer than the per-tool timeout yields "unknown" and returns well within 2× the
// timeout (~6s), not the full sleep duration.
//
// Why: one hung --version must not stall install, session-start, or manual refresh.
func TestCaptureVersion_TimeoutYieldsUnknown(t *testing.T) {
	start := time.Now()
	// Sleep 10s — far beyond the ~3s per-tool timeout.
	v := profile.CaptureVersion("sh", []string{"-c", "sleep 10; echo X"})
	elapsed := time.Since(start)

	if v != "unknown" {
		t.Errorf("CaptureVersion (hung): got %q, want %q", v, "unknown")
	}
	// Must return in <= 2× versionCmdTimeout (~6s), not the full 10s sleep.
	// 2× gives headroom for WaitDelay and OS scheduling; 1× would be too tight.
	const maxElapsed = 6 * time.Second // = 2 × versionCmdTimeout (3s)
	if elapsed > maxElapsed {
		t.Errorf("CaptureVersion (hung): took %v, want <= %v (2× per-tool timeout)", elapsed, maxElapsed)
	}
}

// TestCaptureVersion_FastToolUnaffectedByTimeout verifies that a fast version command
// still returns its output correctly (timeout does not break normal operation).
func TestCaptureVersion_FastToolUnaffectedByTimeout(t *testing.T) {
	v := profile.CaptureVersion("sh", []string{"-c", "echo fastversion"})
	if v != "fastversion" {
		t.Errorf("CaptureVersion (fast): got %q, want %q", v, "fastversion")
	}
}

// TestDetectBatch_SlowEntryDoesNotBlockFastEntry verifies that DetectAll runs entries
// concurrently via its real goroutine pool. The injected registry has 3 SLOW entries
// (sleep 10 — beyond the 3s per-tool timeout) plus 1 FAST entry (echo). If DetectAll
// serialised, total would be ≥ 3×3.5s ≈ 10.5s; with true concurrency all 3 slow entries
// run in parallel → total ≈ 3.5s. The 6s ceiling catches serialisation without being
// flaky on slow CI.
//
// This test drives the production DetectAll path (not test-local goroutines) so any
// regression in the goroutine pool or semaphore is caught.
func TestDetectBatch_SlowEntryDoesNotBlockFastEntry(t *testing.T) {
	// sh is required on every supported platform (macOS, Linux).
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}

	slowEntry := func(name string) profile.RegistryEntry {
		return profile.RegistryEntry{
			Name:        name,
			Binaries:    []string{"sh"},
			VersionArgs: []string{"-c", "sleep 10"},
			Strategy:    profile.StrategyBinary,
			Category:    profile.CategoryCLI,
		}
	}

	reg := []profile.RegistryEntry{
		slowEntry("slow1"),
		slowEntry("slow2"),
		slowEntry("slow3"),
		{
			Name:        "fast",
			Binaries:    []string{"sh"},
			VersionArgs: []string{"-c", "echo 1.2.3"},
			Strategy:    profile.StrategyBinary,
			Category:    profile.CategoryCLI,
		},
	}

	start := time.Now()
	results := profile.DetectAll(profile.DetectOptions{Registry: reg})
	totalElapsed := time.Since(start)

	// Build a name→version map for assertions.
	byName := make(map[string]string, len(results))
	for _, r := range results {
		byName[r.Name] = r.Version
	}

	// (a) Fast entry must resolve to its real version — not "unknown".
	if byName["fast"] != "1.2.3" {
		t.Errorf("fast entry: got %q, want %q", byName["fast"], "1.2.3")
	}

	// (b) Each slow entry must yield "unknown" (timed out).
	for _, name := range []string{"slow1", "slow2", "slow3"} {
		if byName[name] != "unknown" {
			t.Errorf("slow entry %q: got %q, want %q (should have timed out)", name, byName[name], "unknown")
		}
	}

	// (c) Whole batch must complete within 6s.
	// Serial execution would be ≥ 3×3.5s ≈ 10.5s — far above the ceiling.
	// Parallel execution: all 3 slow entries run concurrently → ~3.5s total.
	const batchMax = 6 * time.Second
	if totalElapsed > batchMax {
		t.Errorf("batch: total elapsed %v, want <= %v; DetectAll likely serialised", totalElapsed, batchMax)
	}
}
