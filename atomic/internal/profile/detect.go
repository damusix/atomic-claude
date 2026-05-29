package profile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ToolCategory identifies which group a registry entry belongs to.
type ToolCategory string

const (
	CategoryLanguageRuntime ToolCategory = "language-runtime"
	CategoryPackageManager  ToolCategory = "package-manager"
	CategoryVersionManager  ToolCategory = "version-manager"
	CategoryContainer       ToolCategory = "container"
	CategoryMonorepo        ToolCategory = "monorepo"
	CategoryCLI             ToolCategory = "cli"
	CategoryCloud           ToolCategory = "cloud"
)

// DetectionStrategy controls how presence is checked.
type DetectionStrategy string

const (
	// StrategyBinary checks exec.LookPath only.
	StrategyBinary DetectionStrategy = "binary"
	// StrategyDirectory checks a known install directory only (e.g. nvm, sdkman).
	StrategyDirectory DetectionStrategy = "directory"
	// StrategyBoth tries binary first, then directory.
	StrategyBoth DetectionStrategy = "both"
)

// RegistryEntry describes a single tool in the detection registry.
type RegistryEntry struct {
	// Name is the canonical tool name used in results.
	Name string
	// Binaries are candidate binary names tried in order by exec.LookPath.
	Binaries []string
	// VersionArgs are the arguments passed to the binary to retrieve its version.
	VersionArgs []string
	// VersionLinePrefix, when non-empty, causes CaptureVersion to capture the first
	// output line that starts with this prefix rather than the literal first line.
	// This handles tools (e.g. elixir, mix) whose --version output leads with an
	// unrelated banner (Erlang/OTP) before the relevant version line.
	VersionLinePrefix string
	// Category groups the tool by concern.
	Category ToolCategory
	// Strategy controls how presence is detected.
	Strategy DetectionStrategy
	// InstallDirs are home-relative paths checked when Strategy is directory or both.
	// A leading "$" prefix means an env var is checked first (e.g. "$ASDF_DIR").
	InstallDirs []string
}

// ToolResult holds the detection outcome for a single registry entry.
type ToolResult struct {
	// Name matches RegistryEntry.Name.
	Name string
	// Category matches RegistryEntry.Category.
	Category ToolCategory
	// Installed is true when the tool was found via binary or directory check.
	Installed bool
	// Version is the trimmed first line of the version command output.
	// Empty string means the binary was not found or detection was directory-only.
	// "unknown" means the binary was found but the version command errored.
	Version string
	// ResolvedPath is the full path of the resolved binary (empty for directory-only).
	ResolvedPath string
	// SourceClass classifies the resolved binary's origin.
	SourceClass SourceClass
}

// SourceClass classifies where a binary comes from.
// When the resolved path is under a known version-manager directory, the value
// is that manager's name (e.g. "pyenv", "nvm", "asdf"). Otherwise one of the
// fixed labels below.
type SourceClass string

const (
	// SourceBrew is the fixed label for Homebrew-managed binaries.
	SourceBrew SourceClass = "brew"
	// SourceSys is the fixed label for system-installed binaries (/usr/bin, /bin, etc.).
	SourceSys SourceClass = "sys"
	// SourceOther is the fallback when no known prefix matches.
	SourceOther SourceClass = "other"
)

// DetectOptions configures a DetectAll call. The Home field overrides the user's
// home directory for testing against a tempdir without reading the real $HOME.
type DetectOptions struct {
	// Home overrides os.UserHomeDir(). If empty, os.UserHomeDir() is used.
	Home string
	// Registry overrides DefaultRegistry() when non-nil; for tests.
	Registry []RegistryEntry
}

// ShellEnvOptions controls DetectShell. All fields are injectable so tests
// work without reading the real $SHELL, $HOME, or PATH.
type ShellEnvOptions struct {
	// Shell overrides os.Getenv("SHELL"). If empty, os.Getenv("SHELL") is used.
	Shell string
	// Home overrides os.UserHomeDir(). If empty, os.UserHomeDir() is used.
	Home string
	// LookPath overrides exec.LookPath for framework binary detection (e.g. starship).
	// If nil, exec.LookPath is used. Injected in tests to avoid real PATH dependency.
	LookPath func(file string) (string, error)
}

// ShellResult holds the shell-environment detection output.
type ShellResult struct {
	// LoginShell is the value of $SHELL (or the override).
	LoginShell string
	// Framework is one of "oh-my-zsh", "prezto", "starship", or empty.
	Framework string
	// OhMyZshPlugins enumerates entries under ~/.oh-my-zsh/custom/plugins/.
	OhMyZshPlugins []string
	// OhMyZshThemes enumerates entries under ~/.oh-my-zsh/custom/themes/.
	OhMyZshThemes []string
	// CustomScripts enumerates top-level *.zsh files under ~/.oh-my-zsh/custom/.
	// oh-my-zsh auto-sources these on shell startup.
	CustomScripts []string
}

// DefaultRegistry returns the static detection registry.
// All 7 categories must be represented. The registry is the sole source of truth;
// extend it here when adding tools.
func DefaultRegistry() []RegistryEntry {
	return []RegistryEntry{
		// --- Language runtimes ---
		{
			Name: "node", Binaries: []string{"node"}, VersionArgs: []string{"--version"},
			Category: CategoryLanguageRuntime, Strategy: StrategyBinary,
		},
		{
			Name: "python", Binaries: []string{"python3", "python"}, VersionArgs: []string{"--version"},
			Category: CategoryLanguageRuntime, Strategy: StrategyBinary,
		},
		{
			Name: "go", Binaries: []string{"go"}, VersionArgs: []string{"version"},
			Category: CategoryLanguageRuntime, Strategy: StrategyBinary,
		},
		{
			Name: "rustc", Binaries: []string{"rustc"}, VersionArgs: []string{"--version"},
			Category: CategoryLanguageRuntime, Strategy: StrategyBinary,
		},
		{
			Name: "ruby", Binaries: []string{"ruby"}, VersionArgs: []string{"--version"},
			Category: CategoryLanguageRuntime, Strategy: StrategyBinary,
		},
		{
			Name: "java", Binaries: []string{"java"}, VersionArgs: []string{"-version"},
			Category: CategoryLanguageRuntime, Strategy: StrategyBinary,
		},
		{
			// elixir --version emits the Erlang/OTP banner first, then the Elixir version
			// line. VersionLinePrefix "Elixir" captures the right line (v2.1 reversal of F-16).
			Name: "elixir", Binaries: []string{"elixir"}, VersionArgs: []string{"--version"},
			VersionLinePrefix: "Elixir",
			Category:          CategoryLanguageRuntime, Strategy: StrategyBinary,
		},
		{
			Name: "deno", Binaries: []string{"deno"}, VersionArgs: []string{"--version"},
			Category: CategoryLanguageRuntime, Strategy: StrategyBinary,
		},
		{
			Name: "bun", Binaries: []string{"bun"}, VersionArgs: []string{"--version"},
			Category: CategoryLanguageRuntime, Strategy: StrategyBinary,
		},
		{
			Name: "php", Binaries: []string{"php"}, VersionArgs: []string{"--version"},
			Category: CategoryLanguageRuntime, Strategy: StrategyBinary,
		},
		{
			Name: "gcc", Binaries: []string{"gcc"}, VersionArgs: []string{"--version"},
			Category: CategoryLanguageRuntime, Strategy: StrategyBinary,
		},
		{
			Name: "clang", Binaries: []string{"clang"}, VersionArgs: []string{"--version"},
			Category: CategoryLanguageRuntime, Strategy: StrategyBinary,
		},

		// --- Package / build managers ---
		{
			Name: "npm", Binaries: []string{"npm"}, VersionArgs: []string{"--version"},
			Category: CategoryPackageManager, Strategy: StrategyBinary,
		},
		{
			Name: "pnpm", Binaries: []string{"pnpm"}, VersionArgs: []string{"--version"},
			Category: CategoryPackageManager, Strategy: StrategyBinary,
		},
		{
			Name: "yarn", Binaries: []string{"yarn"}, VersionArgs: []string{"--version"},
			Category: CategoryPackageManager, Strategy: StrategyBinary,
		},
		{
			Name: "pip", Binaries: []string{"pip3", "pip"}, VersionArgs: []string{"--version"},
			Category: CategoryPackageManager, Strategy: StrategyBinary,
		},
		{
			Name: "cargo", Binaries: []string{"cargo"}, VersionArgs: []string{"--version"},
			Category: CategoryPackageManager, Strategy: StrategyBinary,
		},
		{
			Name: "bundler", Binaries: []string{"bundle"}, VersionArgs: []string{"--version"},
			Category: CategoryPackageManager, Strategy: StrategyBinary,
		},
		{
			// mix --version emits the Erlang/OTP banner first, then the Mix version line.
			// VersionLinePrefix "Mix" captures the right line (v2.1 reversal of F-16).
			Name: "mix", Binaries: []string{"mix"}, VersionArgs: []string{"--version"},
			VersionLinePrefix: "Mix",
			Category:          CategoryPackageManager, Strategy: StrategyBinary,
		},
		{
			Name: "maven", Binaries: []string{"mvn"}, VersionArgs: []string{"--version"},
			Category: CategoryPackageManager, Strategy: StrategyBinary,
		},
		{
			Name: "gradle", Binaries: []string{"gradle"}, VersionArgs: []string{"--version"},
			Category: CategoryPackageManager, Strategy: StrategyBinary,
		},
		{
			Name: "make", Binaries: []string{"make"}, VersionArgs: []string{"--version"},
			Category: CategoryPackageManager, Strategy: StrategyBinary,
		},
		{
			Name: "bazel", Binaries: []string{"bazel"}, VersionArgs: []string{"version"},
			Category: CategoryPackageManager, Strategy: StrategyBinary,
		},

		// --- Version managers ---
		// nvm is a shell function; installed via ~/.nvm directory.
		{
			Name: "nvm", Binaries: []string{"nvm"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyDirectory,
			InstallDirs: []string{".nvm"},
		},
		// pyenv ships a binary on PATH.
		{
			Name: "pyenv", Binaries: []string{"pyenv"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBoth,
			InstallDirs: []string{".pyenv"},
		},
		// rbenv ships a binary on PATH.
		{
			Name: "rbenv", Binaries: []string{"rbenv"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBoth,
			InstallDirs: []string{".rbenv"},
		},
		// asdf may use $ASDF_DIR or default ~/.asdf.
		{
			Name: "asdf", Binaries: []string{"asdf"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBoth,
			InstallDirs: []string{"$ASDF_DIR", ".asdf"},
		},
		// mise (formerly rtx) is a binary.
		{
			Name: "mise", Binaries: []string{"mise"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBoth,
			InstallDirs: []string{".local/share/mise"},
		},
		// rustup is a binary.
		{
			Name: "rustup", Binaries: []string{"rustup"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBinary,
		},
		// volta is a binary but also has a data dir.
		{
			Name: "volta", Binaries: []string{"volta"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBoth,
			InstallDirs: []string{".volta"},
		},
		// fnm is a binary.
		{
			Name: "fnm", Binaries: []string{"fnm"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBinary,
		},
		// sdkman is a shell function installed via ~/.sdkman.
		{
			Name: "sdkman", Binaries: []string{"sdk"}, VersionArgs: []string{"version"},
			Category: CategoryVersionManager, Strategy: StrategyDirectory,
			InstallDirs: []string{".sdkman"},
		},

		// --- Containers / orchestration ---
		{
			Name: "docker", Binaries: []string{"docker"}, VersionArgs: []string{"--version"},
			Category: CategoryContainer, Strategy: StrategyBinary,
		},
		{
			Name: "docker-compose", Binaries: []string{"docker-compose"}, VersionArgs: []string{"--version"},
			Category: CategoryContainer, Strategy: StrategyBinary,
		},
		{
			Name: "podman", Binaries: []string{"podman"}, VersionArgs: []string{"--version"},
			Category: CategoryContainer, Strategy: StrategyBinary,
		},
		{
			// kubectl version --client --short was removed in newer kubectl versions.
			// Use version --client instead; the F-3 guard handles any error output.
			Name: "kubectl", Binaries: []string{"kubectl"}, VersionArgs: []string{"version", "--client"},
			Category: CategoryContainer, Strategy: StrategyBinary,
		},
		{
			Name: "helm", Binaries: []string{"helm"}, VersionArgs: []string{"version", "--short"},
			Category: CategoryContainer, Strategy: StrategyBinary,
		},
		{
			Name: "k9s", Binaries: []string{"k9s"}, VersionArgs: []string{"version", "--short"},
			Category: CategoryContainer, Strategy: StrategyBinary,
		},
		{
			Name: "minikube", Binaries: []string{"minikube"}, VersionArgs: []string{"version"},
			Category: CategoryContainer, Strategy: StrategyBinary,
		},
		{
			Name: "kind", Binaries: []string{"kind"}, VersionArgs: []string{"--version"},
			Category: CategoryContainer, Strategy: StrategyBinary,
		},

		// --- Monorepo / build ---
		{
			Name: "nx", Binaries: []string{"nx"}, VersionArgs: []string{"--version"},
			Category: CategoryMonorepo, Strategy: StrategyBinary,
		},
		{
			Name: "turbo", Binaries: []string{"turbo"}, VersionArgs: []string{"--version"},
			Category: CategoryMonorepo, Strategy: StrategyBinary,
		},

		// --- CLI tools ---
		{
			Name: "jq", Binaries: []string{"jq"}, VersionArgs: []string{"--version"},
			Category: CategoryCLI, Strategy: StrategyBinary,
		},
		{
			Name: "yq", Binaries: []string{"yq"}, VersionArgs: []string{"--version"},
			Category: CategoryCLI, Strategy: StrategyBinary,
		},
		{
			Name: "rg", Binaries: []string{"rg"}, VersionArgs: []string{"--version"},
			Category: CategoryCLI, Strategy: StrategyBinary,
		},
		{
			Name: "sg", Binaries: []string{"sg", "ast-grep"}, VersionArgs: []string{"--version"},
			Category: CategoryCLI, Strategy: StrategyBinary,
		},
		{
			Name: "fd", Binaries: []string{"fd"}, VersionArgs: []string{"--version"},
			Category: CategoryCLI, Strategy: StrategyBinary,
		},
		{
			Name: "fzf", Binaries: []string{"fzf"}, VersionArgs: []string{"--version"},
			Category: CategoryCLI, Strategy: StrategyBinary,
		},
		{
			Name: "gh", Binaries: []string{"gh"}, VersionArgs: []string{"--version"},
			Category: CategoryCLI, Strategy: StrategyBinary,
		},
		{
			Name: "git", Binaries: []string{"git"}, VersionArgs: []string{"--version"},
			Category: CategoryCLI, Strategy: StrategyBinary,
		},
		{
			Name: "curl", Binaries: []string{"curl"}, VersionArgs: []string{"--version"},
			Category: CategoryCLI, Strategy: StrategyBinary,
		},

		// --- Cloud ---
		{
			Name: "aws", Binaries: []string{"aws"}, VersionArgs: []string{"--version"},
			Category: CategoryCloud, Strategy: StrategyBinary,
		},
		{
			Name: "gcloud", Binaries: []string{"gcloud"}, VersionArgs: []string{"version"},
			Category: CategoryCloud, Strategy: StrategyBinary,
		},
		{
			Name: "az", Binaries: []string{"az"}, VersionArgs: []string{"--version"},
			Category: CategoryCloud, Strategy: StrategyBinary,
		},
		{
			Name: "terraform", Binaries: []string{"terraform"}, VersionArgs: []string{"version"},
			Category: CategoryCloud, Strategy: StrategyBinary,
		},
		{
			Name: "pulumi", Binaries: []string{"pulumi"}, VersionArgs: []string{"version"},
			Category: CategoryCloud, Strategy: StrategyBinary,
		},
	}
}

// resolveHome returns the effective home directory for the given options.
func resolveHome(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return os.UserHomeDir()
}

// expandInstallDir resolves a single InstallDirs entry against the home directory.
// Entries beginning with "$" are treated as env vars; the home parameter is used
// for relative path entries.
func expandInstallDir(entry string, home string) string {
	if strings.HasPrefix(entry, "$") {
		varName := entry[1:]
		val := os.Getenv(varName)
		if val != "" {
			return val
		}
		// env var absent — fall through returns "" so caller skips this entry.
		return ""
	}
	return filepath.Join(home, entry)
}

// dirExists reports whether the given path is an existing directory.
func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// detectEntry runs the detection logic for a single registry entry.
// home must be the resolved (non-empty) home directory.
func detectEntry(e RegistryEntry, home string) ToolResult {
	result := ToolResult{
		Name:     e.Name,
		Category: e.Category,
	}

	// Binary check.
	var resolvedPath string
	if e.Strategy == StrategyBinary || e.Strategy == StrategyBoth {
		for _, bin := range e.Binaries {
			p, err := exec.LookPath(bin)
			if err == nil {
				resolvedPath = p
				break
			}
		}
	}

	if resolvedPath != "" {
		result.Installed = true
		result.ResolvedPath = resolvedPath
		result.SourceClass = ClassifySource(resolvedPath)
		// Only capture version when args are provided; nil/empty means presence-only.
		if len(e.VersionArgs) > 0 {
			result.Version = CaptureVersionWithPrefix(resolvedPath, e.VersionArgs, e.VersionLinePrefix)
		}
		return result
	}

	// Directory check (for directory or both strategies).
	if e.Strategy == StrategyDirectory || e.Strategy == StrategyBoth {
		for _, dir := range e.InstallDirs {
			expanded := expandInstallDir(dir, home)
			if dirExists(expanded) {
				result.Installed = true
				// No binary path or version for directory-only detection.
				return result
			}
		}
	}

	return result
}

// detectConcurrency is the maximum number of registry entries detected in parallel.
// Each detection may spawn a subprocess; bounded concurrency prevents spawning
// all ~55 subprocesses simultaneously while still parallelizing the wait time.
const detectConcurrency = 8

// DetectAll runs detection for every registry entry and returns a result per entry.
// Results are returned in registry order regardless of detection completion order.
// Detection runs with bounded concurrency (detectConcurrency workers) to avoid
// spawning all subprocesses simultaneously.
func DetectAll(opts DetectOptions) []ToolResult {
	home, err := resolveHome(opts.Home)
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}

	reg := opts.Registry
	if reg == nil {
		reg = DefaultRegistry()
	}
	results := make([]ToolResult, len(reg))

	// Semaphore limits active subprocesses to detectConcurrency at a time.
	sem := make(chan struct{}, detectConcurrency)
	var wg sync.WaitGroup

	for i, e := range reg {
		wg.Add(1)
		// Capture loop variables.
		i, e := i, e
		go func() {
			defer wg.Done()
			sem <- struct{}{} // acquire
			results[i] = detectEntry(e, home)
			<-sem // release
		}()
	}

	wg.Wait()
	return results
}

// CaptureVersion runs `binary args...` and returns the trimmed first non-prompt
// line of combined stdout+stderr output. Returns "unknown" on any error.
// The binary parameter may be a full path or a name resolvable by exec.LookPath.
//
// Non-zero exit always yields "unknown", regardless of any output the command
// produced. This prevents error messages (e.g. rustup "no default toolchain",
// kubectl "unknown flag: --short") from being recorded as the version.
//
// Lines starting with "!" are skipped before taking the first line; this
// handles corepack-intercepted pnpm/yarn which prefix a download prompt with "!".
func CaptureVersion(binary string, args []string) string {
	return CaptureVersionWithPrefix(binary, args, "")
}

// versionCmdTimeout is the per-tool timeout for version commands. One hung
// --version must not stall the entire detection batch (and therefore not block
// install, session-start, or manual refresh). Distinct from the refresh-window
// constant W — this bounds a single subprocess, not the staleness window.
const versionCmdTimeout = 3 * time.Second

// versionCmdWaitDelay is the additional grace period given after context
// cancellation for the subprocess's I/O pipes to drain. Some tools (e.g.
// "sh -c 'sleep N'") spawn child processes that hold the pipe open after the
// parent is killed; WaitDelay forces Wait to return once this window expires.
// Keep this well below versionCmdTimeout (currently ~1/6 of it) so the total
// worst-case wait per entry stays under 2× versionCmdTimeout.
const versionCmdWaitDelay = 500 * time.Millisecond

// CaptureVersionWithPrefix is like CaptureVersion but when prefix is non-empty,
// returns the first output line that starts with prefix (trimmed) instead of the
// literal first line. Returns "unknown" if no matching line is found.
// This handles tools like elixir and mix whose --version output leads with an
// unrelated Erlang/OTP banner before the relevant version line.
func CaptureVersionWithPrefix(binary string, args []string, prefix string) string {
	ctx, cancel := context.WithTimeout(context.Background(), versionCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec // binary comes from the registry, not user input
	// WaitDelay ensures CombinedOutput returns promptly after context cancellation
	// even when child processes hold the I/O pipe open after the parent is killed.
	cmd.WaitDelay = versionCmdWaitDelay
	// Capture combined output (some tools write version to stderr, e.g. java).
	out, err := cmd.CombinedOutput()
	// Non-zero exit or context timeout → unknown, regardless of any output.
	// This prevents error messages (and timed-out partial output) from being
	// recorded as the version string.
	if err != nil {
		return "unknown"
	}

	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "!") {
			continue
		}
		if prefix != "" {
			// Prefix mode: return the first line that starts with the given prefix.
			if strings.HasPrefix(line, prefix) {
				return line
			}
			// Keep scanning for a matching line.
			continue
		}
		// No-prefix mode: return the first non-empty, non-prompt line.
		return line
	}
	// Either no line matched the prefix, or the output was entirely empty/skipped.
	return "unknown"
}

// vmPathRules maps a path substring to the version-manager name it signals.
// Checked in declaration order; first match wins. Entries are ordered so that
// more-specific substrings appear before less-specific ones to avoid false
// positives (e.g. "/.volta/tools/" before "/.volta/").
var vmPathRules = []struct {
	substr  string
	manager SourceClass
}{
	{"/.pyenv/shims/", "pyenv"},
	{"/.pyenv/versions/", "pyenv"},
	{"/.asdf/shims/", "asdf"},
	{"/.asdf/installs/", "asdf"},
	{"/.nvm/versions/", "nvm"},
	{"/.rbenv/shims/", "rbenv"},
	{"/.rbenv/versions/", "rbenv"},
	{"/.volta/tools/", "volta"},
	{"/.volta/bin/", "volta"},
	{"/.fnm/", "fnm"},
	{"/.local/share/mise/", "mise"},
	{"/.rustup/toolchains/", "rustup"},
}

// ClassifySource determines where a binary came from based on its resolved path.
// When the path is under a known version-manager directory, the manager's name is
// returned (e.g. "pyenv", "nvm"). Otherwise one of the fixed labels: "brew", "sys",
// or "other".
func ClassifySource(path string) SourceClass {
	// Version-manager paths — checked first; return manager name directly.
	for _, rule := range vmPathRules {
		if strings.Contains(path, rule.substr) {
			return rule.manager
		}
	}

	// Homebrew paths. These are checked BEFORE system paths because
	// /usr/local/Cellar/ and /usr/local/opt/ share the /usr/local/ prefix with
	// /usr/local/bin/. Swapping the order would misclassify Homebrew binaries as
	// system — the ordering here is load-bearing.
	homebrewPrefixes := []string{
		"/opt/homebrew/",
		"/usr/local/Cellar/",
		"/usr/local/opt/",
		"/home/linuxbrew/",
		"/opt/linuxbrew/",
	}
	for _, prefix := range homebrewPrefixes {
		if strings.HasPrefix(path, prefix) {
			return SourceBrew
		}
	}

	// System paths (non-Homebrew /usr/local/bin counts as system).
	systemPrefixes := []string{
		"/usr/bin/",
		"/bin/",
		"/usr/local/bin/",
		"/usr/sbin/",
		"/sbin/",
	}
	for _, prefix := range systemPrefixes {
		if strings.HasPrefix(path, prefix) {
			return SourceSys
		}
	}

	return SourceOther
}

// DetectShell detects the login shell and shell framework via filesystem probes.
// The ShellEnvOptions fields allow injection of $SHELL and $HOME for testing.
func DetectShell(opts ShellEnvOptions) ShellResult {
	shell := opts.Shell
	if shell == "" {
		shell = os.Getenv("SHELL")
	}

	home, err := resolveHome(opts.Home)
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}

	result := ShellResult{LoginShell: shell}

	// Resolve the LookPath function — use the injected seam or fall back to exec.LookPath.
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	// Framework detection (first match wins).
	switch {
	case dirExists(filepath.Join(home, ".oh-my-zsh")):
		result.Framework = "oh-my-zsh"
		result.OhMyZshPlugins = enumerateDir(filepath.Join(home, ".oh-my-zsh", "custom", "plugins"))
		result.OhMyZshThemes = enumerateDir(filepath.Join(home, ".oh-my-zsh", "custom", "themes"))
		result.CustomScripts = enumerateZshFiles(filepath.Join(home, ".oh-my-zsh", "custom"))
	case dirExists(filepath.Join(home, ".zprezto")):
		result.Framework = "prezto"
	default:
		// Check for starship binary via the injectable seam.
		if _, err := lookPath("starship"); err == nil {
			result.Framework = "starship"
		}
	}

	return result
}

// enumerateZshFiles returns the names of top-level *.zsh files in dir.
// Subdirectories and non-.zsh files are excluded.
// Returns nil if the directory does not exist or cannot be read.
func enumerateZshFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".zsh") {
			names = append(names, e.Name())
		}
	}
	return names
}

// enumerateDir returns the names of all top-level entries in dir.
// Returns nil if the directory does not exist or cannot be read.
func enumerateDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
