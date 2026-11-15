// Package codex adapts the Codex CLI to the common adapter contract. Every
// native claim is gated by harness.CodexCapabilities.
package codex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/embeddedcorpus"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// HomeEnv relocates the Codex native root. Only a relocated root was observed,
// so the variable, not a guessed directory, is the discovered root.
const HomeEnv = "CODEX_HOME"

// Native identities the generated plugin publishes.
const (
	// DefaultHomeDir is the documented native root, relative to the OS home. It
	// was never observed, so it is only a diagnostic hint.
	DefaultHomeDir = ".codex"
	// MarketplaceName is the marketplace the generated tree registers as.
	MarketplaceName = "atomic-claude"
	// PluginName is the plugin the generated tree publishes.
	PluginName = "atomic"
	// PluginVersion is the manifest version Codex copies into the cache path.
	PluginVersion = "1.0.0"
)

// Adapter is the Codex adapter.
type Adapter struct {
	// ResolveHome resolves the CODEX_HOME native root for an OS home; tests stub
	// it so discovery never depends on the process environment.
	ResolveHome func(home string) (string, error)
	// Corpus loads the canonical corpus the plugin publishes. It defaults to the
	// selected binary's embedded generation; tests inject a fixture corpus.
	Corpus func() (*artifacts.Catalog, error)
	// RegisterFn performs the native marketplace and plugin registration. A nil
	// seam reports the surface unsupported rather than guessing.
	RegisterFn func(RegisterRequest) (RegisterResult, error)
	// UnregisterFn undoes an Atomic registration at target uninstall. It
	// defaults to DefaultUnregister.
	UnregisterFn func(RegisterRequest) error
}

// New returns an adapter resolving the real CODEX_HOME and the embedded corpus.
func New() *Adapter {
	return &Adapter{ResolveHome: DefaultHome, Corpus: embeddedcorpus.Load, RegisterFn: DefaultRegister, UnregisterFn: DefaultUnregister}
}

// Kind reports the harness this adapter serves.
func (a *Adapter) Kind() harness.Kind { return harness.KindCodex }

// Capabilities returns the capability record for Codex CLI 0.147.0.
func (a *Adapter) Capabilities() harness.CapabilityMatrix { return harness.CodexCapabilities() }

// Lifecycle is implemented in lifecycle.go, where the projection, enrollment,
// verification, and removal seams are bound for one home.

// corpus loads the canonical corpus through the adapter's seam.
func (a *Adapter) corpus() (*artifacts.Catalog, error) {
	load := a.Corpus
	if load == nil {
		load = embeddedcorpus.Load
	}
	return load()
}

// resolve returns the home resolver, defaulting to DefaultHome.
func (a *Adapter) resolve() func(home string) (string, error) {
	if a.ResolveHome != nil {
		return a.ResolveHome
	}
	return DefaultHome
}

// DefaultHome resolves CODEX_HOME against home. An unset variable is unsupported
// rather than a guess, because the default root was never observed.
func DefaultHome(home string) (string, error) {
	if strings.TrimSpace(home) == "" {
		return "", errors.New("codex: discovery requires a home directory")
	}
	value := strings.TrimSpace(os.Getenv(HomeEnv))
	if value == "" {
		return "", fmt.Errorf("codex: %s is not set and the default %s root is unproven for the tested version; set %s to an isolated home and retry: %w",
			HomeEnv, filepath.ToSlash(filepath.Join("~", DefaultHomeDir)), HomeEnv, harness.ErrUnsupported)
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(home, value)
	}
	return filepath.Clean(value), nil
}

// Discover reports the one CODEX_HOME instance visible from home; no named
// profile surface was observed.
func (a *Adapter) Discover(home string) ([]harness.Instance, error) {
	root, err := a.resolve()(home)
	if err != nil {
		return nil, err
	}
	exists, err := isDir(root)
	if err != nil {
		return nil, err
	}
	return []harness.Instance{{
		Kind:       harness.KindCodex,
		ID:         root,
		NativeRoot: root,
		Home:       home,
		Exists:     exists,
	}}, nil
}

// isDir reports whether path is an existing directory.
func isDir(path string) (bool, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("codex: stat %s: %w", path, err)
	}
	return fi.IsDir(), nil
}

// ConfigPath is a home's native configuration file. The adapter never rewrites
// it, because it also carries user configuration Atomic does not own.
func ConfigPath(root string) string { return filepath.Join(root, "config.toml") }

// CacheRoot is the observed directory Codex materializes
// `<plugin>@<marketplace>` into.
func CacheRoot(root string) string {
	return filepath.Join(root, "plugins", "cache", MarketplaceName, PluginName)
}

// CachePath is the installed plugin's versioned directory, where Codex was
// observed copying the plugin.
func CachePath(root string) string { return filepath.Join(CacheRoot(root), PluginVersion) }

// PluginID is the installed selector Codex resolves: `<plugin>@<marketplace>`.
func PluginID() string { return PluginName + "@" + MarketplaceName }

// now returns the clock a lifecycle request uses, defaulting to UTC.
func now(fn func() time.Time) func() time.Time {
	if fn != nil {
		return fn
	}
	return func() time.Time { return time.Now().UTC() }
}
