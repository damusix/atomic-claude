package doctor

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
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
//
// Combines two independent findings: config.toml/config.resolved.md drift
// (checkConfigDrift) and a chronic background self-update-check failure
// (checkChronicUpdateFailure, read from ~/.atomic/state.json). The chronic
// finding never escalates severity past WARN — a background check failure
// is not itself a config validity problem — but its detail is appended
// alongside whatever the drift check already found.
func RunCheckConfigWith(home string) Result {
	base := checkConfigDrift(home)
	chronicDetail := checkChronicUpdateFailure(home)
	if chronicDetail == "" {
		return base
	}
	switch base.Severity {
	case PASS:
		return Result{Severity: WARN, Detail: chronicDetail}
	default:
		return Result{Severity: base.Severity, Detail: base.Detail + "; " + chronicDetail}
	}
}

// checkChronicUpdateFailure reads the self-update state file and reports a
// detail string when the background check's last_result records a failure
// (non-empty — see cmd/atomic/main.go's runUpdateCheck, which writes
// lookupErr.Error() or stageErr.Error() on failure and "" on success).
// Returns "" when the state file is absent (selfupdate.LoadState's
// zero-value contract) or the last check succeeded.
func checkChronicUpdateFailure(home string) string {
	state := selfupdate.LoadState(config.StatePath(home))
	if state.Update.LastResult == "" {
		return ""
	}
	if state.Update.LastCheck.IsZero() {
		return fmt.Sprintf("background update check failing: %s", state.Update.LastResult)
	}
	return fmt.Sprintf(
		"background update check failing since %s: %s",
		state.Update.LastCheck.Format(time.RFC3339),
		state.Update.LastResult,
	)
}

// checkConfigDrift implements the config.toml / config.resolved.md drift
// half of category 9.
func checkConfigDrift(home string) Result {
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

	// Invalid values → FAIL (includes [claude.agents] effort validation, model
	// is lenient, and [repl] idle_timeout duration validation).
	// Unknown keys are non-fatal for the drift check, but invalid values mean we
	// cannot render a valid resolved.md, so stop here.
	if err := config.Validate(cfg); err != nil {
		return Result{Severity: FAIL, Detail: err.Error()}
	}

	// Append non-fatal [claude.agents] unknown-agent-name warnings after Validate passes.
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
