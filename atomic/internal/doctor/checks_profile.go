package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
)

// ProfileRef is the @-ref string that must appear in one of the candidate
// CLAUDE.md files to wire the user profile into every Claude session.
// Exported so tests can reference the canonical value without duplicating it.
const ProfileRef = "@~/.claude/.atomic/profile.md"

// profileStaleDays is the doctor-WARN threshold for lastcheck freshness.
//
// INTENTIONALLY distinct from the 7-day session-start --if-stale gate:
//   - The 7d gate keeps the environment block fresh during active use (fires on
//     every session open; cheap file-read check).
//   - The 30d threshold here is a longer safety net — it fires only when the
//     user hasn't opened a Claude session (and thus hasn't run the hook) for a
//     month or more. Implementers must NOT unify these two constants.
//
// (See docs/spec/user-profile.md §v2 Doctor staleness extension.)
const profileStaleDays = 30

// checkProfile implements category 10: user profile wired.
//
// Checks three conditions against the installed ~/.claude/ directory:
//  1. ~/.claude/.atomic/profile.md exists on disk and is readable.
//  2. The @~/.claude/.atomic/profile.md @-ref is present in one of the
//     candidate files (same search order as checkRefs, but rooted at
//     claudeHome, not the git repo toplevel).
//  3. The <deterministic lastcheck=YYYY-MM-DD> stamp inside the file is
//     within the last 30 days (see profileStaleDays).
//
// Returns PASS when all three conditions hold. WARN otherwise with detail
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

	// Use os.Stat for existence and os.ReadFile for content so that a file
	// that exists but is unreadable (e.g. permissions 000) is reported as
	// "unreadable" rather than "absent".
	_, statErr := os.Stat(profilePath)
	fileExists := statErr == nil
	var raw []byte
	var readErr error
	if fileExists {
		raw, readErr = os.ReadFile(profilePath)
	}

	refFound := false
	refFile := ""
	for _, name := range candidateFiles {
		path := filepath.Join(claudeHome, name)
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), ProfileRef) {
			refFound = true
			refFile = name
			break
		}
	}

	// A file that exists but can't be read is a distinct failure from absent.
	if fileExists && readErr != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("profile.md exists but is unreadable: %v", readErr)}
	}

	switch {
	case !fileExists && !refFound:
		return Result{Severity: WARN, Detail: "profile.md absent and @-ref not found in any candidate file"}
	case !fileExists && refFound:
		return Result{Severity: WARN, Detail: fmt.Sprintf("@-ref wired in %s but ~/.claude/.atomic/profile.md does not exist", refFile)}
	case fileExists && !refFound:
		return Result{Severity: WARN, Detail: "profile.md present but @-ref not found in CLAUDE.md, claude.local.md, CLAUDE.local.md, or claude.md"}
	}

	// Leg 3: lastcheck freshness.  Both file and ref are present; check whether
	// the <deterministic> block has been refreshed within the last 30 days.
	// today is computed here (real clock) so tests control freshness via fixture
	// content rather than a clock-injection seam.
	today := time.Now().Format("2006-01-02")
	content := string(raw)

	lc, ok := profile.ParseLastcheck(content)
	if !ok {
		// v1-format file: no lastcheck attribute.  Not a broken install — user
		// just needs to run the refresh subcommand to stamp the attribute.
		return Result{Severity: WARN, Detail: "profile.md has no lastcheck stamp; run `atomic profile refresh` to update the Environment section"}
	}
	if profile.IsStale(lc, today, profileStaleDays) {
		return Result{Severity: WARN, Detail: fmt.Sprintf("profile.md lastcheck=%s is older than %d days; run `atomic profile refresh` to update", lc, profileStaleDays)}
	}

	return Result{Severity: PASS, Detail: fmt.Sprintf("profile.md present; ref wired in %s; lastcheck %s", refFile, lc)}
}
