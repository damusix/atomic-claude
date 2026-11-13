package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/pelletier/go-toml/v2"
)

// BinaryEnv overrides the Codex executable the registration seam resolves; empty
// falls back to `codex` on PATH.
const BinaryEnv = "CODEX_BIN"

// RegisterRequest is the caller's explicit request to register the generated
// plugin with one Codex home.
type RegisterRequest struct {
	Home string
	// Root is the CODEX_HOME the native commands run against.
	Root string
	// MarketplaceRoot is the generated marketplace tree to add. Empty selects
	// PackageResource(Home), the published package.
	MarketplaceRoot string
	OperationID     string
}

// RegisterResult reports the native registration as `plugin list` and the
// filesystem show it afterwards, never inferred from exit status alone.
type RegisterResult struct {
	Target          harness.Target    `json:"target"`
	PluginID        string            `json:"plugin_id"`
	MarketplaceRoot string            `json:"marketplace_root"`
	CachePath       string            `json:"cache_path"`
	Installed       bool              `json:"installed"`
	Enabled         bool              `json:"enabled"`
	Applied         []string          `json:"applied,omitempty"`
	State           RegistrationState `json:"state"`
}

// Register runs native registration for one home through the adapter's seam; a
// nil seam reports the surface unsupported.
func (a *Adapter) Register(req RegisterRequest) (RegisterResult, error) {
	if a.RegisterFn == nil {
		return RegisterResult{}, fmt.Errorf("codex: register: %w", harness.ErrUnsupported)
	}
	return a.RegisterFn(req)
}

// DefaultRegister registers the generated marketplace and installs the plugin
// with the Codex CLI, which owns the bytes, so the result is observed, not staged.
func DefaultRegister(req RegisterRequest) (RegisterResult, error) {
	var result RegisterResult
	if req.Home == "" {
		return result, fmt.Errorf("codex: register: no home")
	}
	if req.Root == "" {
		return result, fmt.Errorf("codex: register: no CODEX_HOME root")
	}
	marketplaceRoot := req.MarketplaceRoot
	if marketplaceRoot == "" {
		marketplaceRoot = PackageResource(req.Home)
	}
	if _, err := os.Stat(filepath.Join(marketplaceRoot, filepath.FromSlash(MarketplaceManifestPath))); err != nil {
		return result, fmt.Errorf("codex: register: the generated marketplace tree at %s is not published: %w", marketplaceRoot, err)
	}

	binary, err := codexBinary()
	if err != nil {
		return result, err
	}

	operationID := req.OperationID
	if operationID == "" {
		operationID = "codex-register"
	}
	lock, err := installstate.AcquireLock(req.Home, installstate.WriterIdentity{OperationID: operationID})
	if err != nil {
		return result, err
	}
	defer lock.Release()

	target := harness.Target{Kind: harness.KindCodex, Instance: req.Root, NativeRoot: req.Root}
	result = RegisterResult{Target: target, PluginID: PluginID(), MarketplaceRoot: marketplaceRoot, CachePath: CachePath(req.Root)}

	run := runner(binary, req)

	// A plugin the user deliberately disabled must stay disabled, so the list is
	// read before Codex is changed at all.
	preOut, err := run("plugin", "list", "--json")
	if err != nil {
		return result, err
	}
	preInstalled, preEnabled, err := parsePluginList(preOut, PluginID())
	if err != nil {
		return result, err
	}
	before, err := ObserveRegistration(req.Root)
	if err != nil {
		return result, err
	}
	if preInstalled && !preEnabled {
		result.Installed, result.Enabled, result.State = true, false, before
		return result, fmt.Errorf("codex: register %s: the plugin is installed but disabled; refusing to re-enable a deliberate disablement", PluginID())
	}

	if _, err := run("plugin", "marketplace", "add", marketplaceRoot, "--json"); err != nil {
		return result, err
	}
	// A failure undoes only the entries this call added, so a registration the
	// user already had survives; `plugin remove` exits 0 when nothing was added.
	rollback := func(cause error) error {
		errs := []error{cause}
		if !before.Plugin {
			if _, err := run("plugin", "remove", PluginID(), "--json"); err != nil {
				errs = append(errs, err)
			}
		}
		if !before.Marketplace {
			if _, err := run("plugin", "marketplace", "remove", MarketplaceName); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	if _, err := run("plugin", "add", PluginID(), "--json"); err != nil {
		return result, rollback(err)
	}
	listOut, err := run("plugin", "list", "--json")
	if err != nil {
		return result, rollback(err)
	}
	installed, enabled, err := parsePluginList(listOut, PluginID())
	if err != nil {
		return result, rollback(err)
	}
	result.Installed, result.Enabled = installed, enabled
	if !installed || !enabled {
		return result, rollback(fmt.Errorf("codex: register %s: plugin list reported installed=%v enabled=%v after registration", PluginID(), installed, enabled))
	}
	if info, err := os.Stat(result.CachePath); err != nil || !info.IsDir() {
		return result, rollback(fmt.Errorf("codex: register %s: %s was not materialized after registration", PluginID(), result.CachePath))
	}
	result.Applied = append(result.Applied, result.CachePath)

	state, err := ObserveRegistration(req.Root)
	if err != nil {
		return result, rollback(err)
	}
	result.State = state

	// config.toml stays unclaimed, or uninstall would delete the user's Codex
	// configuration; the cache row is what tells uninstall to unregister.
	if err := recordCacheOwnership(req.Home, target, result.CachePath); err != nil {
		return result, rollback(err)
	}
	return result, nil
}

// recordCacheOwnership records the plugin cache directory as an owned tree
// resource of target, keyed by its path so two homes never share the row.
func recordCacheOwnership(home string, target harness.Target, cachePath string) error {
	digest, _, err := managedfile.TreeDigest(cachePath)
	if err != nil {
		return fmt.Errorf("codex: digest plugin cache %s: %w", cachePath, err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return err
	}
	ledger.Upsert(installstate.Row{
		Target:   target.Key(),
		Resource: cachePath,
		Consumer: target.Key(),
		Applied:  installstate.AppliedValue{Path: cachePath, Kind: managedfile.KindTree, Digest: digest},
	})
	return ledger.Save(config.LedgerPath(home))
}

// DefaultUnregister removes only the entries config.toml records, because
// `codex plugin marketplace remove` exits 1 for an absent marketplace.
func DefaultUnregister(req RegisterRequest) error {
	state, err := ObserveRegistration(req.Root)
	if err != nil {
		return err
	}
	if !state.Plugin && !state.Marketplace {
		return nil
	}
	binary, err := codexBinary()
	if err != nil {
		return err
	}
	run := runner(binary, req)
	if state.Plugin {
		if _, err := run("plugin", "remove", PluginID(), "--json"); err != nil {
			return err
		}
	}
	if state.Marketplace {
		_, err = run("plugin", "marketplace", "remove", MarketplaceName)
	}
	return err
}

func runner(binary string, req RegisterRequest) func(args ...string) ([]byte, error) {
	return func(args ...string) ([]byte, error) {
		cmd := exec.Command(binary, args...)
		cmd.Env = registerEnv(os.Environ(), req.Home, req.Root)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = strings.TrimSpace(stdout.String())
			}
			return nil, fmt.Errorf("codex: %s: %w: %s", strings.Join(append([]string{binary}, args...), " "), err, msg)
		}
		return stdout.Bytes(), nil
	}
}

// codexBinary resolves the Codex executable: BinaryEnv when set, else `codex` on
// PATH. A missing binary is an unsupported surface, never a silent skip.
func codexBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv(BinaryEnv)); override != "" {
		return override, nil
	}
	found, err := exec.LookPath("codex")
	if err != nil {
		return "", fmt.Errorf("codex: the codex binary is required for native registration and was not found on PATH: %w", harness.ErrUnsupported)
	}
	return found, nil
}

// registerEnv returns env with HOME and CODEX_HOME pointing at the requested
// home, so an ambient value cannot register into the wrong CODEX_HOME.
func registerEnv(env []string, home, root string) []string {
	out := make([]string, 0, len(env)+2)
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, HomeEnv+"=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "HOME="+home, HomeEnv+"="+root)
}

// parsePluginList reads the installed and enabled flags for pluginID out of
// `plugin list --json`.
func parsePluginList(data []byte, pluginID string) (installed, enabled bool, err error) {
	var doc struct {
		Installed []struct {
			PluginID  string `json:"pluginId"`
			Installed bool   `json:"installed"`
			Enabled   bool   `json:"enabled"`
		} `json:"installed"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return false, false, fmt.Errorf("codex: decode plugin list: %w", err)
	}
	for _, entry := range doc.Installed {
		if entry.PluginID != pluginID {
			continue
		}
		return entry.Installed, entry.Enabled, nil
	}
	return false, false, nil
}

// ObserveRegistration reads Codex's own registry file and reports what it
// records for the generated plugin, writing and inferring nothing.
func ObserveRegistration(root string) (RegistrationState, error) {
	path := ConfigPath(root)
	state := RegistrationState{ConfigPath: path, CachePath: CachePath(root)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("codex: read %s: %w", path, err)
	}
	var doc struct {
		Marketplaces map[string]any `toml:"marketplaces"`
		Plugins      map[string]struct {
			Enabled bool `toml:"enabled"`
		} `toml:"plugins"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return state, fmt.Errorf("codex: parse %s: %w", path, err)
	}
	_, state.Marketplace = doc.Marketplaces[MarketplaceName]
	if entry, ok := doc.Plugins[PluginID()]; ok {
		state.Plugin = true
		state.Enabled = entry.Enabled
	}
	if info, err := os.Stat(state.CachePath); err == nil && info.IsDir() {
		state.CachePresent = true
	}
	return state, nil
}
