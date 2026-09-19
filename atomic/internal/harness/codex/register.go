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

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/pelletier/go-toml/v2"
)

// BinaryEnv overrides the Codex executable the registration seam resolves, so a
// caller can pin the tested binary or a fixture. Empty falls back to `codex` on
// PATH, which is what the CP0 replay used.
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

// RegisterResult reports the verified native registration: the installed
// plugin identity, the cache directory Codex materialized, and the observed
// registry state. Nothing here is inferred from the commands' exit status alone:
// the result is what `plugin list` and the filesystem afterwards report.
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

// Register runs the CP0-proven native registration for one home through the
// adapter's seam. A nil seam reports the surface unsupported instead of
// pretending registration happened.
func (a *Adapter) Register(req RegisterRequest) (RegisterResult, error) {
	if a.RegisterFn == nil {
		return RegisterResult{}, fmt.Errorf("codex: register: %w", harness.ErrUnsupported)
	}
	return a.RegisterFn(req)
}

// DefaultRegister performs native registration with the Codex CLI: it registers
// the generated marketplace tree, installs the Atomic plugin from it, and then
// verifies the installed and enabled state from `plugin list` plus the on-disk
// cache directory. The commands are exactly the CP0-observed surface; Codex owns
// their byte effects, so the adapter observes the result instead of staging it.
//
// The lifecycle lock is held across the commands, and unresolved journals are
// recovered first, so registration cannot race another lifecycle operation.
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

	run := func(args ...string) ([]byte, error) {
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

	if _, err := run("plugin", "marketplace", "add", marketplaceRoot, "--json"); err != nil {
		return result, err
	}
	if _, err := run("plugin", "add", PluginID(), "--json"); err != nil {
		return result, err
	}
	listOut, err := run("plugin", "list", "--json")
	if err != nil {
		return result, err
	}
	installed, enabled, err := parsePluginList(listOut, PluginID())
	if err != nil {
		return result, err
	}
	result.Installed, result.Enabled = installed, enabled
	if !installed || !enabled {
		return result, fmt.Errorf("codex: register %s: plugin list reported installed=%v enabled=%v after registration", PluginID(), installed, enabled)
	}
	if info, err := os.Stat(result.CachePath); err != nil || !info.IsDir() {
		return result, fmt.Errorf("codex: register %s: %s was not materialized after registration", PluginID(), result.CachePath)
	}
	result.Applied = append(result.Applied, result.CachePath)

	state, err := ObserveRegistration(req.Root)
	if err != nil {
		return result, err
	}
	result.State = state
	return result, nil
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
// `plugin list --json`. A plugin absent from the installed array is reported
// false rather than assumed.
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
// records for the generated plugin. It writes nothing and infers nothing: a
// missing marketplace or plugin entry is `false`, and an unreadable file is an
// error the caller reports as an observation gap.
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
