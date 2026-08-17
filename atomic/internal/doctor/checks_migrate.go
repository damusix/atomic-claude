package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

// checkMigrateDrift implements category 12: migrate. WARNs when [install].version
// lags the running binary (migration steps may be pending) or when
// ~/.claude/.atomic is still a real directory — MigrateUserState runs before
// every verb, so a surviving dir means a prior migration failed or ~/.atomic
// already existed. Either condition WARNs; both concatenate into one detail.
func checkMigrateDrift(_ Opts) Result {
	home, err := resolveHome()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve home dir: %v", err)}
	}
	return RunCheckMigrateDriftWith(home, version.Version)
}

// RunCheckMigrateDriftWith runs the migrate check against an explicit home
// dir and binary version. Exported for testing.
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

// versionDriftCondition returns a non-empty warnDetail (+ remediation) when
// the binary is newer than the recorded install version; otherwise a
// passDetail explaining why not.
//
// CompareSemver parses "dev" (local builds) as 0.0.0, so a dev build is never
// newer than a recorded version and never nudges.
func versionDriftCondition(home, binaryVersion string) (warnDetail, passDetail, remediation string) {
	tomlPath := config.TOMLPath(home)

	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		return "", "no config.toml; migration not applicable", ""
	}

	cfg, _, err := config.Load(tomlPath)
	if err != nil {
		// The config check already reports parse errors; don't double-report.
		return "", "config not readable; skipping migrate-drift check", ""
	}

	installVersion := cfg.Install.Version
	if installVersion == "" {
		return "", "no install.version recorded (pre-framework install)", ""
	}

	if selfupdate.CompareSemver(binaryVersion, installVersion) > 0 {
		return fmt.Sprintf("binary %s > last install %s; migration steps may be pending", binaryVersion, installVersion),
			"", "atomic migrate"
	}

	return "", fmt.Sprintf("install.version %s matches binary", installVersion), ""
}

// legacyStateDirCondition reports a detail only for a real (not symlinked)
// ~/.claude/.atomic directory. The compat symlink, an absent path, and a
// non-directory all have nothing to migrate.
func legacyStateDirCondition(home string) string {
	legacyDir := filepath.Join(home, ".claude", ".atomic")
	info, err := os.Lstat(legacyDir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ""
	}
	return fmt.Sprintf("%s is still a real directory; migration to ~/.atomic runs automatically on any atomic verb invocation but hasn't completed here — check for a prior failure (e.g. a permission error) or a conflicting ~/.atomic", legacyDir)
}
