package doctor

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

// checkMigrateDrift implements category 12: migration-drift check.
//
// Reads [install].version from config.toml. When the binary version is newer
// than the recorded install version, emit a WARN nudging the user to run
// `atomic migrate` so any pending versioned migration steps are applied.
//
// Conditions that produce no nudge (PASS):
//   - config.toml is absent (not atomic-installed)
//   - config.toml present but [install].version is empty (pre-framework install)
//   - binary version is not newer than install version (up-to-date or install ahead)
//
// The binary version string "dev" (default for local builds) is treated as
// 0.0.0 by selfupdate.CompareSemver, so a dev build is never considered newer
// than any valid recorded install version — dev builds never nudge.
func checkMigrateDrift(_ Opts) Result {
	claudeHome, err := resolveClaudeHome()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve claude home: %v", err)}
	}
	return RunCheckMigrateDriftWith(claudeHome, version.Version)
}

// RunCheckMigrateDriftWith runs the migration-drift check against an explicit
// claudeHome and binaryVersion string. Exported for testing; production
// callers use checkMigrateDrift.
func RunCheckMigrateDriftWith(claudeHome, binaryVersion string) Result {
	tomlPath := config.TOMLPath(claudeHome)

	// No config.toml → no install manifest; migration not applicable.
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		return Result{Severity: PASS, Detail: "no config.toml; migration not applicable"}
	}

	cfg, _, err := config.Load(tomlPath)
	if err != nil {
		// Config parse errors are reported by check 9 (config). Degrade to PASS
		// here so the migrate check doesn't double-report on the same issue.
		return Result{Severity: PASS, Detail: "config not readable; skipping migrate-drift check"}
	}

	installVersion := cfg.Install.Version
	if installVersion == "" {
		// No [install].version → pre-framework install or not installed via
		// atomic claude install. Do not nudge.
		return Result{Severity: PASS, Detail: "no install.version recorded (pre-framework install)"}
	}

	// CompareSemver("dev", installVersion): "dev" parses as 0.0.0, which is
	// always <= any valid semver — dev builds never trigger the nudge.
	if selfupdate.CompareSemver(binaryVersion, installVersion) > 0 {
		return Result{
			Severity:    WARN,
			Detail:      fmt.Sprintf("binary %s > last install %s; migration steps may be pending", binaryVersion, installVersion),
			Remediation: "atomic migrate",
		}
	}

	return Result{
		Severity: PASS,
		Detail:   fmt.Sprintf("install.version %s matches binary", installVersion),
	}
}
