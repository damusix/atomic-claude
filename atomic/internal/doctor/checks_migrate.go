package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

// checkMigrateDrift implements category 12: migrate. Combines two
// independent conditions into a single Result (combined-detail style, as
// checks_config.go's config check does):
//
//  1. version drift — [install].version in config.toml is older than the
//     running binary, so versioned migration steps (`atomic migrate`) may be
//     pending.
//  2. legacy state dir — ~/.claude/.atomic is still a real directory, meaning
//     issue #150's user-state relocation to ~/.atomic has not completed on
//     this machine (config.MigrateUserState runs automatically before every
//     verb dispatch — a persistent real dir here means a prior run hit a
//     migration failure, or ~/.atomic already existed independently).
//
// Severity is the worst of the two (WARN if either fires); detail is the
// concatenation of whichever condition(s) are non-empty.
func checkMigrateDrift(_ Opts) Result {
	home, err := resolveHome()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve home dir: %v", err)}
	}
	return RunCheckMigrateDriftWith(home, version.Version)
}

// RunCheckMigrateDriftWith runs the migrate check against an explicit home
// dir and binaryVersion string. Exported for testing; production callers use
// checkMigrateDrift.
func RunCheckMigrateDriftWith(home, binaryVersion string) Result {
	warnDetail, passDetail, remediation := versionDriftCondition(home, binaryVersion)
	legacyDetail := legacyStateDirCondition(home)

	switch {
	case warnDetail != "" && legacyDetail != "":
		return Result{Severity: WARN, Detail: warnDetail + "; " + legacyDetail, Remediation: remediation}
	case warnDetail != "":
		return Result{Severity: WARN, Detail: warnDetail, Remediation: remediation}
	case legacyDetail != "":
		return Result{Severity: WARN, Detail: legacyDetail}
	default:
		return Result{Severity: PASS, Detail: passDetail}
	}
}

// versionDriftCondition evaluates the version-drift leg. Returns a non-empty
// warnDetail (+ remediation) when the binary is newer than the recorded
// install version; otherwise returns a passDetail explaining why not.
//
// Conditions that produce no nudge (passDetail set, warnDetail empty):
//   - config.toml is absent (not atomic-installed)
//   - config.toml present but [install].version is empty (pre-framework install)
//   - binary version is not newer than install version (up-to-date or install ahead)
//
// The binary version string "dev" (default for local builds) is treated as
// 0.0.0 by selfupdate.CompareSemver, so a dev build is never considered newer
// than any valid recorded install version — dev builds never nudge.
func versionDriftCondition(home, binaryVersion string) (warnDetail, passDetail, remediation string) {
	tomlPath := config.TOMLPath(home)

	// No config.toml → no install manifest; migration not applicable.
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		return "", "no config.toml; migration not applicable", ""
	}

	cfg, _, err := config.Load(tomlPath)
	if err != nil {
		// Config parse errors are reported by check 9 (config). Degrade to PASS
		// here so the migrate check doesn't double-report on the same issue.
		return "", "config not readable; skipping migrate-drift check", ""
	}

	installVersion := cfg.Install.Version
	if installVersion == "" {
		// No [install].version → pre-framework install or not installed via
		// atomic claude install. Do not nudge.
		return "", "no install.version recorded (pre-framework install)", ""
	}

	// CompareSemver("dev", installVersion): "dev" parses as 0.0.0, which is
	// always <= any valid semver — dev builds never trigger the nudge.
	if selfupdate.CompareSemver(binaryVersion, installVersion) > 0 {
		return fmt.Sprintf("binary %s > last install %s; migration steps may be pending", binaryVersion, installVersion),
			"", "atomic migrate"
	}

	return "", fmt.Sprintf("install.version %s matches binary", installVersion), ""
}

// legacyStateDirCondition evaluates the legacy-state-dir leg: a real (not
// symlinked) ~/.claude/.atomic means the migration hasn't completed here.
// Returns "" when the path is absent, is the compat symlink, or is occupied
// by something other than a directory (not this check's concern — nothing to
// migrate at that path either way).
func legacyStateDirCondition(home string) string {
	legacyDir := filepath.Join(home, ".claude", ".atomic")
	info, err := os.Lstat(legacyDir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ""
	}
	return fmt.Sprintf("%s is still a real directory; migration to ~/.atomic runs automatically on any atomic verb invocation but hasn't completed here — check for a prior failure (e.g. a permission error) or a conflicting ~/.atomic", legacyDir)
}
