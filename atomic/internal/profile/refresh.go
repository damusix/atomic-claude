package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// DefaultRefreshDays is the shared refresh-window constant: 1 day (24h).
// Install, update, and the session-start hook all pass this constant to RefreshIfStale.
// A config-settable override is a deliberate future amendment (axiom 2 — code constant
// for now, promote to config only when demand is proven).
const DefaultRefreshDays = 1

// ParseDuration parses a duration string in the form "<N>d" (e.g. "7d", "30d").
// Only days are accepted; any other unit or format returns an error.
// N must be a positive integer (> 0).
func ParseDuration(s string) (int, error) {
	if !strings.HasSuffix(s, "d") {
		return 0, fmt.Errorf("profile: duration %q: only days are accepted (e.g. 7d, 30d)", s)
	}
	numStr := s[:len(s)-1]
	if numStr == "" {
		return 0, fmt.Errorf("profile: duration %q: missing numeric value before 'd'", s)
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("profile: duration %q: %w", s, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("profile: duration %q: value must be > 0", s)
	}
	return n, nil
}

// lastcheckRe matches the lastcheck attribute in a <deterministic lastcheck=YYYY-MM-DD> tag.
var lastcheckRe = regexp.MustCompile(`<deterministic\s+lastcheck=(\d{4}-\d{2}-\d{2})>`)

// ParseLastcheck scans profile.md content for the lastcheck attribute on the
// <deterministic> tag and returns the YYYY-MM-DD string.
// Returns ("", false) when the attribute is absent (v1-format file or no file).
func ParseLastcheck(content string) (string, bool) {
	m := lastcheckRe.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return m[1], true
}

const dateFmt = "2006-01-02"

// IsStale reports whether (today - lastcheck) >= days (i.e. the window has elapsed).
// Malformed lastcheck or today are treated as stale (safe fallback → always refresh).
func IsStale(lastcheck, today string, days int) bool {
	lc, err := time.ParseInLocation(dateFmt, lastcheck, time.UTC)
	if err != nil {
		return true // malformed → treat as stale
	}
	td, err := time.ParseInLocation(dateFmt, today, time.UTC)
	if err != nil {
		return true // malformed → treat as stale
	}
	diff := td.Sub(lc)
	return diff >= time.Duration(days)*24*time.Hour
}

// Refresh re-detects all registry tools, renders a fresh ## Environment section
// stamped with date (YYYY-MM-DD), rewrites the profile.md at
// config.ProfilePath(claudeHome), and creates parent dirs as needed.
//
// Returns (true, nil) when the file was written, (false, nil) on no-op
// (currently never no-op in the unconditional path), or (false, err) on error.
//
// date is injected by the caller; time.Now() is never called here.
func Refresh(claudeHome, date string) (bool, error) {
	profilePath := config.ProfilePath(claudeHome)

	// Read existing content (empty string if file absent).
	existing, err := os.ReadFile(profilePath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("profile refresh: read %s: %w", profilePath, err)
	}
	content := string(existing)

	// Run detection.
	env := CaptureEnv()
	tools := DetectAll(DetectOptions{})
	shell := DetectShell(ShellEnvOptions{})

	// Render the new Environment section.
	envSection := RenderEnvironmentSection(env, tools, shell, date)

	// Rewrite the section (handles all 4 cases from the spec).
	newContent := RewriteEnvironmentSection(content, envSection)

	// Write atomically: write to a temp file beside the destination, then rename.
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		return false, fmt.Errorf("profile refresh: mkdir %s: %w", filepath.Dir(profilePath), err)
	}

	tmp := profilePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(newContent), 0o644); err != nil {
		return false, fmt.Errorf("profile refresh: write tmp: %w", err)
	}
	if err := os.Rename(tmp, profilePath); err != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("profile refresh: rename: %w", err)
	}

	return true, nil
}

// RefreshIfStale performs a staleness check before refreshing. If the current
// file's lastcheck attribute is within <days> days of today, it is a no-op
// (returns false, nil). Otherwise it calls Refresh.
//
// A missing lastcheck (v1-format file or no file) is treated as infinitely stale
// and triggers a full refresh.
func RefreshIfStale(claudeHome, today string, days int) (bool, error) {
	profilePath := config.ProfilePath(claudeHome)

	// Read existing content to check lastcheck.
	existing, err := os.ReadFile(profilePath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("profile refresh --if-stale: read %s: %w", profilePath, err)
	}

	if err == nil {
		// File exists: check lastcheck.
		if lastcheck, ok := ParseLastcheck(string(existing)); ok {
			if !IsStale(lastcheck, today, days) {
				// Fresh — no-op.
				return false, nil
			}
		}
		// lastcheck absent or stale → fall through to refresh.
	}
	// File absent or stale → full refresh.
	return Refresh(claudeHome, today)
}
