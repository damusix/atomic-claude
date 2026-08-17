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

func TestRegistry_MinimumSize(t *testing.T) {
	reg := profile.DefaultRegistry()
	if len(reg) < 45 {
		t.Errorf("registry has %d entries, want >= 45", len(reg))
	}
}

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

func TestRegistry_AllStrategiesValid(t *testing.T) {
	reg := profile.DefaultRegistry()
	for _, e := range reg {
		switch e.Strategy {
		case profile.StrategyBinary, profile.StrategyDirectory, profile.StrategyBoth:
		default:
			t.Errorf("entry %q has invalid strategy %q", e.Name, e.Strategy)
		}
	}
}

// A version manager absent from PATH is still found via its install directory.
func TestDetect_DirectoryFallback(t *testing.T) {
	home := t.TempDir()

	// nvm installs as a shell function, so only the directory exists.
	if err := os.MkdirAll(filepath.Join(home, ".nvm"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := profile.DetectOptions{Home: home}
	results := profile.DetectAll(opts)

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

func TestDetect_DirectoryFallback_Absent(t *testing.T) {
	home := t.TempDir() // empty — no .nvm, no .sdkman, etc.

	opts := profile.DetectOptions{Home: home}
	results := profile.DetectAll(opts)

	for _, r := range results {
		if r.Name == "nvm" && r.Installed {
			t.Errorf("nvm: expected Installed=false in empty home, got true")
		}
	}
}

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

func TestVersionCapture_TrimmedFirstLine(t *testing.T) {
	// go is reliably present in the test environment.
	v := profile.CaptureVersion("go", []string{"version"})
	if v == "" {
		t.Error("CaptureVersion returned empty string for 'go version'")
	}
	for _, ch := range v {
		if ch == '\n' || ch == '\r' {
			t.Errorf("CaptureVersion returned multi-line output: %q", v)
			break
		}
	}
}

func TestVersionCapture_ErrorYieldsUnknown(t *testing.T) {
	v := profile.CaptureVersion("__nonexistent_tool_xyz__", []string{"--version"})
	if v != "unknown" {
		t.Errorf("CaptureVersion with bad binary: got %q, want %q", v, "unknown")
	}
}

// Guards against a rustup/kubectl-style error message landing in profile.md as
// the version: non-zero exit yields "unknown" whatever was printed.
func TestVersionCapture_NonZeroExitWithOutputYieldsUnknown(t *testing.T) {
	// `false` exits non-zero but prints nothing; this needs both.
	v := profile.CaptureVersion("sh", []string{"-c", "echo 'error: rustup could not choose a version'; exit 1"})
	if v != "unknown" {
		t.Errorf("CaptureVersion with non-zero exit and output: got %q, want %q", v, "unknown")
	}
}

// Bounded parallelism must still return results in registry order.
func TestDetectAll_OrderDeterministic(t *testing.T) {
	opts := profile.DetectOptions{Home: t.TempDir()}

	reg := profile.DefaultRegistry()

	results1 := profile.DetectAll(opts)
	results2 := profile.DetectAll(opts)

	if len(results1) != len(reg) {
		t.Fatalf("DetectAll run 1: got %d results, want %d", len(results1), len(reg))
	}
	if len(results2) != len(reg) {
		t.Fatalf("DetectAll run 2: got %d results, want %d", len(results2), len(reg))
	}

	for i, e := range reg {
		if results1[i].Name != e.Name {
			t.Errorf("run 1 index %d: got name %q, want %q (registry order)", i, results1[i].Name, e.Name)
		}
		if results2[i].Name != e.Name {
			t.Errorf("run 2 index %d: got name %q, want %q (registry order)", i, results2[i].Name, e.Name)
		}
	}
}

// A version-manager path yields the manager's name, not a generic label.
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

func TestClassifySource_System(t *testing.T) {
	cases := []string{
		"/usr/bin/python3",
		"/bin/bash",
		// /usr/local/bin is system unless it sits under Cellar or opt.
		"/usr/local/bin/git",
	}
	for _, p := range cases {
		got := profile.ClassifySource(p)
		if got != profile.SourceSys {
			t.Errorf("ClassifySource(%q) = %q, want %q", p, got, profile.SourceSys)
		}
	}
}

func TestClassifySource_Other(t *testing.T) {
	got := profile.ClassifySource("/home/user/.cargo/bin/rustc")
	if got != profile.SourceOther {
		t.Errorf("ClassifySource(%q) = %q, want %q", "/home/user/.cargo/bin/rustc", got, profile.SourceOther)
	}
}

// Elixir-style output: the banner comes first, but the Elixir line must win.
func TestCaptureVersion_ElixirStylePrefix(t *testing.T) {
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

// No matching line yields "unknown", never the banner and never an empty string.
func TestCaptureVersion_PrefixNoMatch(t *testing.T) {
	script := `echo 'Erlang/OTP 27 [erts-15.0]'`
	v := profile.CaptureVersionWithPrefix("sh", []string{"-c", script}, "Elixir")
	if v != "unknown" {
		t.Errorf("CaptureVersionWithPrefix no-match: got %q, want \"unknown\"", v)
	}
}

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

func TestShellEnumeration_OhMyZshCustomScripts(t *testing.T) {
	home := t.TempDir()
	customDir := filepath.Join(home, ".oh-my-zsh", "custom")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
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

func TestShellEnumeration_NoCustomScriptsWhenAbsent(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".oh-my-zsh"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := profile.ShellEnvOptions{Shell: "/bin/zsh", Home: home}
	result := profile.DetectShell(opts)

	if len(result.CustomScripts) != 0 {
		t.Errorf("expected 0 CustomScripts, got %v", result.CustomScripts)
	}
}

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

// The LookPath seam keeps a starship on the runner's real PATH from false-positiving.
func TestShellEnumeration_NoFramework(t *testing.T) {
	home := t.TempDir() // empty

	neverFound := func(string) (string, error) {
		return "", fmt.Errorf("not found")
	}

	opts := profile.ShellEnvOptions{Shell: "/bin/bash", Home: home, LookPath: neverFound}
	result := profile.DetectShell(opts)
	if result.Framework != "" {
		t.Errorf("Framework = %q, want empty (no framework installed)", result.Framework)
	}
}

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

func TestShellEnumeration_OhMyZshThemes(t *testing.T) {
	home := t.TempDir()
	customThemes := filepath.Join(home, ".oh-my-zsh", "custom", "themes")
	if err := os.MkdirAll(customThemes, 0o755); err != nil {
		t.Fatal(err)
	}
	// Themes are usually files, not dirs.
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

func TestShellEnumeration_EmptyOhMyZshDirs(t *testing.T) {
	home := t.TempDir()
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

// One hung --version must not stall install, session-start, or manual refresh.
func TestCaptureVersion_TimeoutYieldsUnknown(t *testing.T) {
	start := time.Now()
	v := profile.CaptureVersion("sh", []string{"-c", "sleep 10; echo X"})
	elapsed := time.Since(start)

	if v != "unknown" {
		t.Errorf("CaptureVersion (hung): got %q, want %q", v, "unknown")
	}
	// 2× the timeout leaves headroom for WaitDelay and OS scheduling; 1× is tight.
	const maxElapsed = 6 * time.Second // = 2 × versionCmdTimeout (3s)
	if elapsed > maxElapsed {
		t.Errorf("CaptureVersion (hung): took %v, want <= %v (2× per-tool timeout)", elapsed, maxElapsed)
	}
}

func TestCaptureVersion_FastToolUnaffectedByTimeout(t *testing.T) {
	v := profile.CaptureVersion("sh", []string{"-c", "echo fastversion"})
	if v != "fastversion" {
		t.Errorf("CaptureVersion (fast): got %q, want %q", v, "fastversion")
	}
}

// Three slow entries plus one fast one, driven through the production pool:
// serialised that is ≥10.5s, concurrent ≈3.5s, so a 6s ceiling catches a
// regression in the semaphore without going flaky on slow CI.
func TestDetectBatch_SlowEntryDoesNotBlockFastEntry(t *testing.T) {
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

	byName := make(map[string]string, len(results))
	for _, r := range results {
		byName[r.Name] = r.Version
	}

	if byName["fast"] != "1.2.3" {
		t.Errorf("fast entry: got %q, want %q", byName["fast"], "1.2.3")
	}

	for _, name := range []string{"slow1", "slow2", "slow3"} {
		if byName[name] != "unknown" {
			t.Errorf("slow entry %q: got %q, want %q (should have timed out)", name, byName[name], "unknown")
		}
	}

	const batchMax = 6 * time.Second
	if totalElapsed > batchMax {
		t.Errorf("batch: total elapsed %v, want <= %v; DetectAll likely serialised", totalElapsed, batchMax)
	}
}
