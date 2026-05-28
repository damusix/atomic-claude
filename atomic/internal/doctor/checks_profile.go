package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// ProfileRef is the @-ref string that must appear in one of the candidate
// CLAUDE.md files to wire the user profile into every Claude session.
// Exported so tests can reference the canonical value without duplicating it.
const ProfileRef = "@~/.claude/.atomic/profile.md"

// checkProfile implements category 10: user profile wired.
//
// Checks two conditions against the installed ~/.claude/ directory:
//  1. ~/.claude/.atomic/profile.md exists on disk.
//  2. The @~/.claude/.atomic/profile.md @-ref is present in one of the
//     candidate files (same search order as checkRefs, but rooted at
//     claudeHome, not the git repo toplevel).
//
// Returns PASS when both conditions hold. WARN otherwise with detail
// explaining which leg(s) failed. Severity: WARN (profile absence is
// degraded experience, not a broken installation).
func checkProfile(_ Opts) Result {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not determine home dir: %v", err)}
	}
	return RunCheckProfileWith(filepath.Join(home, ".claude"))
}

// RunCheckProfileWith runs the profile check against an explicit claudeHome.
// Exported for testing; production callers use checkProfile.
func RunCheckProfileWith(claudeHome string) Result {
	profilePath := config.ProfilePath(claudeHome)
	_, statErr := os.Stat(profilePath)
	fileExists := statErr == nil

	refFound := false
	refFile := ""
	for _, name := range candidateFiles {
		path := filepath.Join(claudeHome, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(raw), ProfileRef) {
			refFound = true
			refFile = name
			break
		}
	}

	switch {
	case fileExists && refFound:
		return Result{Severity: PASS, Detail: fmt.Sprintf("profile.md present; ref wired in %s", refFile)}
	case fileExists && !refFound:
		return Result{Severity: WARN, Detail: "profile.md present but @-ref not found in CLAUDE.md, claude.local.md, CLAUDE.local.md, or claude.md"}
	case !fileExists && refFound:
		return Result{Severity: WARN, Detail: fmt.Sprintf("@-ref wired in %s but ~/.claude/.atomic/profile.md does not exist", refFile)}
	default:
		return Result{Severity: WARN, Detail: "profile.md absent and @-ref not found in any candidate file"}
	}
}
