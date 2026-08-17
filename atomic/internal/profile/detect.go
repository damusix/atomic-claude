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
	StrategyBinary    DetectionStrategy = "binary"
	StrategyDirectory DetectionStrategy = "directory"
	// StrategyBoth tries binary first, then directory.
	StrategyBoth DetectionStrategy = "both"
)

// RegistryEntry describes a single tool in the detection registry.
type RegistryEntry struct {
	Name string
	// Binaries are candidate names tried in order by exec.LookPath.
	Binaries    []string
	VersionArgs []string
	// VersionLinePrefix picks the first output line with this prefix instead of
	// the literal first line, for tools that lead with an unrelated banner.
	VersionLinePrefix string
	Category          ToolCategory
	Strategy          DetectionStrategy
	// InstallDirs are home-relative paths for the directory strategies; a leading
	// "$" names an env var to consult first.
	InstallDirs []string
}

// ToolResult holds the detection outcome for a single registry entry.
type ToolResult struct {
	Name      string
	Category  ToolCategory
	Installed bool
	// Version is the trimmed version line; empty when the binary was absent or
	// detection was directory-only, "unknown" when the version command errored.
	Version string
	// ResolvedPath is empty for directory-only detection.
	ResolvedPath string
	SourceClass  SourceClass
}

// SourceClass is the version manager's name when the resolved path sits under a
// known manager directory, otherwise one of the fixed labels below.
type SourceClass string

const (
	SourceBrew SourceClass = "brew"
	SourceSys  SourceClass = "sys"
	// SourceOther is the fallback when no known prefix matches.
	SourceOther SourceClass = "other"
)

// DetectOptions configures a DetectAll call. Both fields exist so tests can run
// against a tempdir and a fixed registry rather than the real $HOME.
type DetectOptions struct {
	Home     string
	Registry []RegistryEntry
}

// ShellEnvOptions controls DetectShell. All fields are injectable so tests work
// without reading the real $SHELL, $HOME, or PATH.
type ShellEnvOptions struct {
	Shell    string
	Home     string
	LookPath func(file string) (string, error)
}

// ShellResult holds the shell-environment detection output.
type ShellResult struct {
	LoginShell string
	// Framework is one of "oh-my-zsh", "prezto", "starship", or empty.
	Framework      string
	OhMyZshPlugins []string
	OhMyZshThemes  []string
	// CustomScripts are top-level *.zsh files under ~/.oh-my-zsh/custom/, which
	// oh-my-zsh auto-sources on startup.
	CustomScripts []string
}

// DefaultRegistry is the sole source of truth for detection; extend it here when
// adding a tool. All 7 categories must stay represented.
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
			// Leads with the Erlang/OTP banner before the Elixir version line.
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
			// Leads with the Erlang/OTP banner before the Mix version line.
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
		// nvm and sdkman are shell functions, never binaries on PATH.
		{
			Name: "nvm", Binaries: []string{"nvm"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyDirectory,
			InstallDirs: []string{".nvm"},
		},
		{
			Name: "pyenv", Binaries: []string{"pyenv"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBoth,
			InstallDirs: []string{".pyenv"},
		},
		{
			Name: "rbenv", Binaries: []string{"rbenv"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBoth,
			InstallDirs: []string{".rbenv"},
		},
		{
			Name: "asdf", Binaries: []string{"asdf"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBoth,
			InstallDirs: []string{"$ASDF_DIR", ".asdf"},
		},
		{
			Name: "mise", Binaries: []string{"mise"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBoth,
			InstallDirs: []string{".local/share/mise"},
		},
		{
			Name: "rustup", Binaries: []string{"rustup"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBinary,
		},
		{
			Name: "volta", Binaries: []string{"volta"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBoth,
			InstallDirs: []string{".volta"},
		},
		{
			Name: "fnm", Binaries: []string{"fnm"}, VersionArgs: []string{"--version"},
			Category: CategoryVersionManager, Strategy: StrategyBinary,
		},
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
			// Newer kubectl dropped --short from `version --client`.
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

func resolveHome(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return os.UserHomeDir()
}

// expandInstallDir resolves an InstallDirs entry, returning "" for an unset env
// var so the caller skips that candidate.
func expandInstallDir(entry string, home string) string {
	if strings.HasPrefix(entry, "$") {
		varName := entry[1:]
		val := os.Getenv(varName)
		if val != "" {
			return val
		}
		return ""
	}
	return filepath.Join(home, entry)
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// detectEntry requires an already-resolved, non-empty home.
func detectEntry(e RegistryEntry, home string) ToolResult {
	result := ToolResult{
		Name:     e.Name,
		Category: e.Category,
	}

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
		// Empty VersionArgs means presence-only.
		if len(e.VersionArgs) > 0 {
			result.Version = CaptureVersionWithPrefix(resolvedPath, e.VersionArgs, e.VersionLinePrefix)
		}
		return result
	}

	if e.Strategy == StrategyDirectory || e.Strategy == StrategyBoth {
		for _, dir := range e.InstallDirs {
			expanded := expandInstallDir(dir, home)
			if dirExists(expanded) {
				result.Installed = true
				return result
			}
		}
	}

	return result
}

// detectConcurrency bounds parallel detections so the whole registry's version
// subprocesses don't spawn at once.
const detectConcurrency = 8

// DetectAll returns one result per registry entry, in registry order regardless
// of completion order.
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

	sem := make(chan struct{}, detectConcurrency)
	var wg sync.WaitGroup

	for i, e := range reg {
		wg.Add(1)
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

// CaptureVersion returns the first non-prompt line of the binary's combined
// output, or "unknown".
//
// Non-zero exit yields "unknown" whatever the command printed, so an error
// message never lands in the version field. Lines starting with "!" are skipped
// because corepack prefixes a download prompt to pnpm/yarn output.
func CaptureVersion(binary string, args []string) string {
	return CaptureVersionWithPrefix(binary, args, "")
}

// versionCmdTimeout keeps one hung --version from stalling the whole detection
// batch, and with it install, session-start, and manual refresh.
const versionCmdTimeout = 3 * time.Second

// versionCmdWaitDelay drains pipes held open by grandchild processes after the
// parent is killed. Keep it well under versionCmdTimeout so the worst-case wait
// per entry stays below 2× that timeout.
const versionCmdWaitDelay = 500 * time.Millisecond

// CaptureVersionWithPrefix returns the first output line starting with prefix,
// or "unknown" when none matches. Empty prefix behaves like CaptureVersion.
func CaptureVersionWithPrefix(binary string, args []string, prefix string) string {
	ctx, cancel := context.WithTimeout(context.Background(), versionCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec // binary comes from the registry, not user input
	cmd.WaitDelay = versionCmdWaitDelay
	// Combined, because some tools (java) write the version to stderr.
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "unknown"
	}

	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "!") {
			continue
		}
		if prefix != "" {
			if strings.HasPrefix(line, prefix) {
				return line
			}
			continue
		}
		return line
	}
	return "unknown"
}

// vmPathRules maps a path substring to the version manager it signals. First
// match wins, so more-specific substrings must come first.
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

// ClassifySource maps a resolved binary path to its origin.
func ClassifySource(path string) SourceClass {
	for _, rule := range vmPathRules {
		if strings.Contains(path, rule.substr) {
			return rule.manager
		}
	}

	// Homebrew must precede the system check: /usr/local/Cellar/ and
	// /usr/local/opt/ share the /usr/local/ prefix with /usr/local/bin/.
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

// DetectShell probes the filesystem for the login shell and its framework.
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

	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	// First match wins.
	switch {
	case dirExists(filepath.Join(home, ".oh-my-zsh")):
		result.Framework = "oh-my-zsh"
		result.OhMyZshPlugins = enumerateDir(filepath.Join(home, ".oh-my-zsh", "custom", "plugins"))
		result.OhMyZshThemes = enumerateDir(filepath.Join(home, ".oh-my-zsh", "custom", "themes"))
		result.CustomScripts = enumerateZshFiles(filepath.Join(home, ".oh-my-zsh", "custom"))
	case dirExists(filepath.Join(home, ".zprezto")):
		result.Framework = "prezto"
	default:
		if _, err := lookPath("starship"); err == nil {
			result.Framework = "starship"
		}
	}

	return result
}

// enumerateZshFiles returns nil for an unreadable or missing dir.
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

// enumerateDir returns nil for an unreadable or missing dir.
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
