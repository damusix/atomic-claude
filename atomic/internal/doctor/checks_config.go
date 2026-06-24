package doctor

import (
	"fmt"
	"os"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// checkConfig implements category 9: config integrity.
//
// Resolves ~/.claude/ then calls RunCheckConfigWith.
func checkConfig(opts Opts) Result {
	claudeHome, err := resolveClaudeHome()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve claude home: %v", err)}
	}
	return RunCheckConfigWith(claudeHome)
}

// RunCheckConfigWith runs the config check against an explicit claudeHome.
// Exported for testing; production callers use checkConfig.
func RunCheckConfigWith(claudeHome string) Result {
	tomlPath := config.TOMLPath(claudeHome)
	resolvedPath := config.ResolvedPath(claudeHome)

	// If config.toml does not exist, defaults are valid — PASS.
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		return Result{Severity: PASS, Detail: "config.toml not present (using built-in defaults)"}
	}

	// Load config (lenient: unknown keys → warnings, parse error → error).
	cfg, warns, err := config.Load(tomlPath)
	if err != nil {
		return Result{Severity: FAIL, Detail: fmt.Sprintf("config parse error: %v", err)}
	}

	// Build unknown-keys detail (if any); do NOT return early — also check drift.
	var unknownKeysDetail string
	if len(warns) > 0 {
		keys := make([]string, 0, len(warns))
		for _, w := range warns {
			keys = append(keys, w.Message)
		}
		unknownKeysDetail = strings.Join(keys, "; ") + " — run `atomic config unset <key>` to remove"
	}

	// Invalid values → FAIL (unknown keys are non-fatal for drift check but
	// invalid values mean we cannot render a valid resolved.md, so stop here).
	if err := config.Validate(cfg); err != nil {
		return Result{Severity: FAIL, Detail: err.Error()}
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

// RunConfigRepairWith performs the config repair against an explicit claudeHome.
// Exported for testing.
//
// Repair logic:
//   - If config.toml is absent → nothing to do (PASS state; no repair needed).
//   - If config.toml doesn't parse → cannot auto-fix; returns error.
//   - If config.toml has unknown keys → re-renders resolved.md from current schema.
//   - If resolved.md is missing or drifted → re-renders it.
func RunConfigRepairWith(claudeHome string) error {
	tomlPath := config.TOMLPath(claudeHome)
	resolvedPath := config.ResolvedPath(claudeHome)

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
	if err := os.MkdirAll(config.Dir(claudeHome), 0o755); err != nil {
		return fmt.Errorf("mkdir .atomic: %w", err)
	}
	return os.WriteFile(resolvedPath, []byte(rendered), 0o644)
}
