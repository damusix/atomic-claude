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

// DefaultRefreshDays is the window install, update, and the session-start hook
// all pass to RefreshIfStale. A code constant until demand for config is proven.
const DefaultRefreshDays = 1

// ParseDuration accepts only "<N>d" with N a positive integer.
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

var lastcheckRe = regexp.MustCompile(`<deterministic\s+lastcheck=(\d{4}-\d{2}-\d{2})>`)

// ParseLastcheck returns ("", false) when the <deterministic> tag carries no
// lastcheck — a v1-format file, or no file at all.
func ParseLastcheck(content string) (string, bool) {
	m := lastcheckRe.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return m[1], true
}

const dateFmt = "2006-01-02"

// IsStale treats a malformed date as stale, so the fallback is always to refresh.
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

// Refresh re-detects every registry tool and rewrites profile.md's ## Environment
// section, stamped with date. The caller injects date; time.Now is never called
// here. Reports whether the file was written.
func Refresh(home, date string) (bool, error) {
	profilePath := config.ProfilePath(home)

	existing, err := os.ReadFile(profilePath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("profile refresh: read %s: %w", profilePath, err)
	}
	content := string(existing)

	env := CaptureEnv()
	tools := DetectAll(DetectOptions{})
	shell := DetectShell(ShellEnvOptions{})

	envSection := RenderEnvironmentSection(env, tools, shell, date)

	newContent := RewriteEnvironmentSection(content, envSection)

	// Temp file beside the destination, then rename — an interrupted write must
	// not truncate the user's profile.
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

// RefreshIfStale is a no-op while lastcheck is within days of today. A missing
// lastcheck counts as infinitely stale.
func RefreshIfStale(home, today string, days int) (bool, error) {
	profilePath := config.ProfilePath(home)

	existing, err := os.ReadFile(profilePath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("profile refresh --if-stale: read %s: %w", profilePath, err)
	}

	if err == nil {
		if lastcheck, ok := ParseLastcheck(string(existing)); ok {
			if !IsStale(lastcheck, today, days) {
				return false, nil
			}
		}
	}
	return Refresh(home, today)
}
