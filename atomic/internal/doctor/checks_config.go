package doctor

import (
	"fmt"
	"os"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// checkConfig implements category 9: config integrity.
//
// Resolves the user's home directory then calls RunCheckConfigWith.
func checkConfig(opts Opts) Result {
	home, err := resolveHome()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve home dir: %v", err)}
	}
	return RunCheckConfigWith(home)
}

// RunCheckConfigWith runs the config check against an explicit home dir.
// Exported for testing; production callers use checkConfig.
func RunCheckConfigWith(home string) Result {
	tomlPath := config.TOMLPath(home)
	resolvedPath := config.ResolvedPath(home)

	// If config.toml does not exist, defaults are valid — PASS.
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		return Result{Severity: PASS, Detail: "config.toml not present (using built-in defaults)"}
	}

	// Load config (lenient: unknown keys → warnings, parse error → error).
	cfg, warns, err := config.Load(tomlPath)
	if err != nil {
		return Result{Severity: FAIL, Detail: fmt.Sprintf("config parse error: %v", err)}
	}

	// Invalid values → FAIL (includes [agents] effort validation; model is lenient).
	// Unknown keys are non-fatal for the drift check, but invalid values mean we
	// cannot render a valid resolved.md, so stop here.
	if err := config.Validate(cfg); err != nil {
		return Result{Severity: FAIL, Detail: err.Error()}
	}

	// Append non-fatal [agents] unknown-agent-name warnings after Validate passes.
	warns = append(warns, config.AgentWarnings(cfg)...)

	// Build combined warning detail (if any); do NOT return early — also check drift.
	var unknownKeysDetail string
	if len(warns) > 0 {
		keys := make([]string, 0, len(warns))
		for _, w := range warns {
			keys = append(keys, w.Message)
		}
		unknownKeysDetail = strings.Join(keys, "; ") + " — run `atomic config unset <key>` to remove"
	}

	// Check resolved.md sync.
	var driftDetail string
	expected := config.Render(cfg)
	actual, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			driftDetail = "config.resolved.md out of sync — run `atomic doctor --fix` to re-render"
		} else {
			driftDetail = fmt.Sprintf("read config.resolved.md: %v", err)
		}
	} else if string(actual) != expected {
		driftDetail = "config.resolved.md out of sync — run `atomic doctor --fix` to re-render"
	}

	// Combine findings into a single result.
	switch {
	case unknownKeysDetail != "" && driftDetail != "":
		return Result{Severity: WARN, Detail: unknownKeysDetail + "; " + driftDetail}
	case unknownKeysDetail != "":
		return Result{Severity: WARN, Detail: unknownKeysDetail}
	case driftDetail != "":
		return Result{Severity: WARN, Detail: driftDetail}
	default:
		return Result{Severity: PASS, Detail: "config.toml ok; config.resolved.md in sync"}
	}
}

// RunConfigRepairWith performs the config repair against an explicit home dir.
// Exported for testing.
//
// Repair logic:
//   - If config.toml is absent → nothing to do (PASS state; no repair needed).
//   - If config.toml doesn't parse → cannot auto-fix; returns error.
//   - If config.toml has unknown keys → re-renders resolved.md from current schema.
//   - If resolved.md is missing or drifted → re-renders it.
func RunConfigRepairWith(home string) error {
	tomlPath := config.TOMLPath(home)
	resolvedPath := config.ResolvedPath(home)

	// No TOML = nothing to repair.
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		return nil
	}

	cfg, _, err := config.Load(tomlPath)
	if err != nil {
		return fmt.Errorf("cannot auto-fix: config.toml does not parse: %v — edit manually or run `atomic config unset` on problem keys", err)
	}

	// Invalid values (e.g. output.signals.max_depth = "bogus") cannot be written into
	// config.resolved.md — that file gets @-ref'd into every Claude session.
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("cannot auto-fix: invalid config value: %v — edit manually or run `atomic config unset <key>` to remove", err)
	}

	rendered := config.Render(cfg)
	if err := os.MkdirAll(config.Dir(home), 0o755); err != nil {
		return fmt.Errorf("mkdir .atomic: %w", err)
	}
	return os.WriteFile(resolvedPath, []byte(rendered), 0o644)
}
