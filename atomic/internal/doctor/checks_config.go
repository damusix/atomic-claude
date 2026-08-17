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
func checkConfig(opts Opts) Result {
	home, err := resolveHome()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve home dir: %v", err)}
	}
	return RunCheckConfigWith(home)
}

// RunCheckConfigWith runs the config check against an explicit home dir.
// Exported for testing. It merges config.toml validity with a chronic
// background update-check failure; the chronic finding never escalates past
// WARN, since a failing background check is not a config validity problem.
func RunCheckConfigWith(home string) Result {
	base := checkConfigValidity(home)
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

// checkChronicUpdateFailure reports a detail when the background update
// check's last_result is non-empty, which is how it records a failure.
// A missing state file loads as the zero value and so reports "".
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

// checkConfigValidity implements the config.toml validity half of category 9.
func checkConfigValidity(home string) Result {
	tomlPath := config.TOMLPath(home)

	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		return Result{Severity: PASS, Detail: "config.toml not present (using built-in defaults)"}
	}

	cfg, warns, err := config.Load(tomlPath)
	if err != nil {
		return Result{Severity: FAIL, Detail: fmt.Sprintf("config parse error: %v", err)}
	}

	// An unknown key only WARNs; an invalid value FAILs.
	if err := config.Validate(cfg); err != nil {
		return Result{Severity: FAIL, Detail: err.Error()}
	}

	warns = append(warns, config.AgentWarnings(cfg)...)

	var unknownKeysDetail string
	if len(warns) > 0 {
		keys := make([]string, 0, len(warns))
		for _, w := range warns {
			keys = append(keys, w.Message)
		}
		unknownKeysDetail = strings.Join(keys, "; ") + " — run `atomic config unset <key>` to remove"
	}

	if unknownKeysDetail != "" {
		return Result{Severity: WARN, Detail: unknownKeysDetail}
	}
	return Result{Severity: PASS, Detail: "config.toml ok"}
}
