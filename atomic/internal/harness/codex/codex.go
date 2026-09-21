// Package codex adopts the Codex CLI to the common adapter contract: it
// resolves an isolated CODEX_HOME, generates the Atomic plugin package the
// native marketplace registration consumes, enrols the home through the shared
// transaction engine, and reports every native surface CP0 could not prove.
//
// Every native claim is gated by the CP0 capability record. Codex CLI 0.147.0
// proved exactly two things: a local marketplace tree can be registered and
// listed inside an isolated CODEX_HOME, and the installed plugin's cache path
// is `<CODEX_HOME>/plugins/cache/<marketplace>/<plugin>/<version>`. The
// isolated runtime reached thread creation but the account rejected the
// configured model before any hook event, so steering discovery, plugin-hook
// trust, session-baseline context, the pre-operation event, context return,
// deny, resume, child behavior, payload limits, and every tool-coverage claim
// are unsupported. The adapter therefore generates the plugin and its rule
// index, ships the rule, skill, and agent corpus, and emits no hook
// configuration at all: a missing capability row is reported, never fabricated.
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

// HomeEnv is the environment variable that relocates the Codex native root. CP0
// observed the adapter's whole registration and runtime flow inside an isolated
// CODEX_HOME, so the variable — not a guessed directory — is the discovered
// root.
const HomeEnv = "CODEX_HOME"

// Native identities the generated plugin publishes. The marketplace tree is the
// package Atomic generates; the plugin is the Atomic corpus materialized for
// Codex. The version is the manifest version Codex keys the cache path by.
const (
	// DefaultHomeDir is the documented native root, relative to the OS home. CP0
	// never exercised it, so discovery reports it only as a diagnostic hint and
	// never resolves a root from it.
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
	// ResolveHome resolves the CODEX_HOME native root for an OS home. It
	// defaults to DefaultHome; tests inject a stub so discovery never depends on
	// the process environment.
	ResolveHome func(home string) (string, error)
	// Corpus loads the canonical corpus the plugin publishes. It defaults to the
	// selected binary's embedded generation; tests inject a fixture corpus.
	Corpus func() (*artifacts.Catalog, error)
	// RegisterFn performs the CP0-proven native marketplace and plugin
	// registration for one home. It defaults to DefaultRegister; a nil seam
	// reports the surface unsupported rather than guessing.
	RegisterFn func(RegisterRequest) (RegisterResult, error)
}

// New returns an adapter resolving the real CODEX_HOME and the embedded corpus.
func New() *Adapter {
	return &Adapter{ResolveHome: DefaultHome, Corpus: embeddedcorpus.Load, RegisterFn: DefaultRegister}
}

// Kind reports the harness this adapter serves.
func (a *Adapter) Kind() harness.Kind { return harness.KindCodex }

// Capabilities returns the CP0 record for Codex CLI 0.147.0.
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

// DefaultHome resolves the CODEX_HOME native root for one OS home. CP0 observed
// only the variable-relocated root; the default directory was never exercised,
// so an unset variable is an explicit unsupported answer instead of a guess at
// where Atomic would write. A relative value resolves against home so a
// relocated root keeps its meaning.
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

// Discover reports the CODEX_HOME instance visible from home. Codex has no
// named profile surface CP0 observed, so exactly one instance is reported;
// discovery reads the filesystem, runs no registration, and creates no ledger,
// journal, cache, or package.
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

// ConfigPath is a home's native configuration file. Codex writes its marketplace
// and plugin registry here; the adapter never rewrites it, because the file also
// carries user configuration Atomic does not own.
func ConfigPath(root string) string { return filepath.Join(root, "config.toml") }

// CacheRoot is the directory Codex materializes `<plugin>@<marketplace>` into:
// `<CODEX_HOME>/plugins/cache/<marketplace>/<plugin>`. CP0 observed the exact
// path shape.
func CacheRoot(root string) string {
	return filepath.Join(root, "plugins", "cache", MarketplaceName, PluginName)
}

// CachePath is the installed plugin's versioned directory, the path CP0
// observed Codex copy the plugin to.
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
