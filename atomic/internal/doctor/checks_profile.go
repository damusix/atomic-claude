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

// ProfileRef is the @-ref that must appear in one of the candidate CLAUDE.md
// files to wire the user profile into every Claude session.
const ProfileRef = "@~/.atomic/profile.md"

// legacyProfileRef is the pre-relocation @-ref. The compat symlink keeps it
// resolving, so a CLAUDE.md carrying it still works — the check nudges anyway.
const legacyProfileRef = "@~/.claude/.atomic/profile.md"

// profileStaleDays is the doctor-WARN threshold for lastcheck freshness.
// Deliberately not the 7-day session-start --if-stale gate: that one keeps the
// environment block fresh during active use, this one is the longer safety net
// for a user who hasn't opened a session in a month. Do not unify them.
const profileStaleDays = 30

// checkProfile implements category 10: user profile wired. PASS when
// profile.md exists, its @-ref is present in a candidate file, the lastcheck
// stamp is within profileStaleDays, and no candidate carries legacyProfileRef.
// Any failed leg WARNs — an unwired profile is degraded, not broken.
func checkProfile(_ Opts) Result {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not determine home dir: %v", err)}
	}
	return RunCheckProfileWith(home)
}

// RunCheckProfileWith runs the profile check against an explicit home dir.
// Exported for testing.
func RunCheckProfileWith(home string) Result {
	profilePath := config.ProfilePath(home)
	// The installed CLAUDE.md family lives under ~/.claude, not the ~/.atomic
	// state root.
	claudeHome := filepath.Join(home, ".claude")

	// Stat then ReadFile so an existing-but-unreadable file (mode 000) is
	// reported as unreadable rather than absent.
	_, statErr := os.Stat(profilePath)
	fileExists := statErr == nil
	var raw []byte
	var readErr error
	if fileExists {
		raw, readErr = os.ReadFile(profilePath)
	}

	refFound := false
	legacyRefFound := false
	refFile := ""
	for _, name := range candidateFiles {
		path := filepath.Join(claudeHome, name)
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(b)
		if strings.Contains(content, ProfileRef) {
			refFound = true
			refFile = name
			break
		}
		if strings.Contains(content, legacyProfileRef) {
			legacyRefFound = true
			refFile = name
		}
	}

	if fileExists && readErr != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("profile.md exists but is unreadable: %v", readErr)}
	}

	if !refFound && legacyRefFound {
		return Result{Severity: WARN, Detail: fmt.Sprintf("%s carries the legacy @~/.claude/.atomic/profile.md ref; run `atomic claude install` to update to @~/.atomic/profile.md", refFile)}
	}

	switch {
	case !fileExists && !refFound:
		return Result{Severity: WARN, Detail: "profile.md absent and @-ref not found in any candidate file"}
	case !fileExists && refFound:
		return Result{Severity: WARN, Detail: fmt.Sprintf("@-ref wired in %s but ~/.atomic/profile.md does not exist", refFile)}
	case fileExists && !refFound:
		return Result{Severity: WARN, Detail: "profile.md present but @-ref not found in CLAUDE.md, claude.local.md, CLAUDE.local.md, or claude.md"}
	}

	// Real clock rather than an injected seam: tests control freshness through
	// fixture content instead.
	today := time.Now().Format("2006-01-02")
	content := string(raw)

	lc, ok := profile.ParseLastcheck(content)
	if !ok {
		return Result{Severity: WARN, Detail: "profile.md has no lastcheck stamp; run `atomic profile refresh` to update the Environment section"}
	}
	if profile.IsStale(lc, today, profileStaleDays) {
		return Result{Severity: WARN, Detail: fmt.Sprintf("profile.md lastcheck=%s is older than %d days; run `atomic profile refresh` to update", lc, profileStaleDays)}
	}

	return Result{Severity: PASS, Detail: fmt.Sprintf("profile.md present; ref wired in %s; lastcheck %s", refFile, lc)}
}
