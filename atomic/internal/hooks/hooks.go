// Package hooks implements the session-start hook output and install/uninstall
// of the Claude Code session-start hook script and settings.json registration.
package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/reminder"
)

const (
	scriptName      = "session-start-reminders.sh"
	hooksSubdir     = ".claude/hooks"
	settingsRelPath = ".claude/settings.json"
	maxReminders    = 10
	previewMaxLen   = 80
	oldThresholdDay = 14
)

// SessionStart returns the JSON hook payload for the SessionStart event.
// If no reminders are pending, it returns an empty string (no-op for Claude).
// now is the reference time used for relative date formatting (allows testing).
func SessionStart(repoRoot string, now time.Time) (string, error) {
	// Call reminder.List once and pass rows to both the body builder and the
	// age scan — avoids a second filesystem traversal.
	rows, err := reminder.List(repoRoot)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}

	body, err := buildAdditionalContextFromRows(rows, now)
	if err != nil {
		return "", err
	}
	if body == "" {
		return "", nil
	}

	payload := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "SessionStart",
			"additionalContext": body,
		},
		"suppressOutput": true,
	}

	// Check for old reminders (older than oldThresholdDay days).
	oldestDays := 0
	for _, r := range rows {
		d, err := parseDateDays(r.Created, now)
		if err != nil {
			continue
		}
		if d > oldestDays {
			oldestDays = d
		}
	}
	if oldestDays > oldThresholdDay {
		payload["systemMessage"] = fmt.Sprintf(
			"%d reminders pending, oldest is %d days old",
			len(rows), oldestDays,
		)
	}

	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("hooks session-start: marshal: %w", err)
	}
	return string(out), nil
}

// SessionStartText returns the plain-markdown version of the session-start
// context (no JSON envelope). Returns empty string when no reminders exist.
func SessionStartText(repoRoot string, now time.Time) (string, error) {
	rows, err := reminder.List(repoRoot)
	if err != nil {
		return "", fmt.Errorf("hooks session-start: list reminders: %w", err)
	}
	return buildAdditionalContextFromRows(rows, now)
}

// buildAdditionalContextFromRows constructs the markdown body from pre-fetched rows.
// Truncation of the preview is done here; reminder.List returns raw body lines.
func buildAdditionalContextFromRows(rows []reminder.Row, now time.Time) (string, error) {
	if len(rows) == 0 {
		return "", nil
	}

	total := len(rows)
	shown := rows
	overflow := 0
	if total > maxReminders {
		shown = rows[:maxReminders]
		overflow = total - maxReminders
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Pending reminders (%d)\n", total)
	for _, r := range shown {
		// Truncate the raw preview from the reminder package at the rendering layer.
		preview := truncate(r.Preview, previewMaxLen)
		ago := relativeAge(r.Created, now)
		fmt.Fprintf(&sb, "- [%s] %s (created %s)\n", r.ID, preview, ago)
	}
	if overflow > 0 {
		fmt.Fprintf(&sb, "- (and %d more)\n", overflow)
	}

	return strings.TrimRight(sb.String(), "\n"), nil
}

// relativeAge returns a human-readable string like "today", "yesterday",
// "3 days ago", "2 weeks ago", "1 month ago".
func relativeAge(createdDate string, now time.Time) string {
	days, err := parseDateDays(createdDate, now)
	if err != nil {
		return "unknown"
	}
	switch {
	case days == 0:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 7:
		return fmt.Sprintf("%d days ago", days)
	case days < 14:
		return "1 week ago"
	case days < 30:
		weeks := days / 7
		return fmt.Sprintf("%d weeks ago", weeks)
	default:
		months := days / 30
		return fmt.Sprintf("%d month%s ago", months, pluralS(months))
	}
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// parseDateDays returns how many days ago createdDate (YYYY-MM-DD) was from now.
func parseDateDays(createdDate string, now time.Time) (int, error) {
	t, err := time.Parse("2006-01-02", createdDate)
	if err != nil {
		return 0, err
	}
	// Truncate both to UTC date.
	created := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	diff := today.Sub(created)
	days := int(diff.Hours() / 24)
	if days < 0 {
		days = 0
	}
	return days, nil
}

// truncate shortens s to at most maxLen runes, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// scriptPath returns the absolute path for the hook script under scopeRoot.
func scriptPath(scopeRoot string) string {
	return filepath.Join(scopeRoot, hooksSubdir, scriptName)
}

// settingsPath returns the absolute path for settings.json under scopeRoot.
func settingsPath(scopeRoot string) string {
	return filepath.Join(scopeRoot, settingsRelPath)
}

// Install writes the session-start hook script and registers it in settings.json
// under scopeRoot. repoRoot is unused at this layer (scopeRoot determines paths).
func Install(repoRoot, scopeRoot string) error {
	sp := scriptPath(scopeRoot)

	// 1. Write the script.
	if err := os.MkdirAll(filepath.Dir(sp), 0o755); err != nil {
		return fmt.Errorf("hooks install: mkdir hooks dir: %w", err)
	}
	scriptContent := "#!/usr/bin/env bash\nexec atomic hooks session-start\n"
	if err := os.WriteFile(sp, []byte(scriptContent), 0o755); err != nil {
		return fmt.Errorf("hooks install: write script: %w", err)
	}

	// 2. Register in settings.json.
	return registerInSettings(settingsPath(scopeRoot), sp)
}

// Uninstall removes the hook script and its registration from settings.json.
func Uninstall(repoRoot, scopeRoot string) error {
	sp := scriptPath(scopeRoot)

	// 1. Remove the script (no-op if absent).
	if err := os.Remove(sp); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("hooks uninstall: remove script: %w", err)
	}

	// 2. Remove registration from settings.json.
	sfPath := settingsPath(scopeRoot)
	if _, err := os.Stat(sfPath); os.IsNotExist(err) {
		return nil
	}
	return unregisterFromSettings(sfPath, sp)
}

// hasRegistration returns true if settings already has a SessionStart entry
// whose inner hooks[].command equals scriptPath.
func hasRegistration(settings map[string]any, scriptAbsPath string) bool {
	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false
	}
	ss, ok := hooksMap["SessionStart"].([]any)
	if !ok {
		return false
	}
	for _, entry := range ss {
		e, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		inner, ok := e["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if hm["command"] == scriptAbsPath {
				return true
			}
		}
	}
	return false
}

// malformedErrorWithScript returns an error for a malformed settings.json,
// including the manual-registration snippet with the actual resolved script path
// so the user can copy-paste it without manual substitution.
func malformedErrorWithScript(sfPath, scriptAbsPath string) error {
	snippet := fmt.Sprintf(`{
  "hooks": {
    "SessionStart": [
      {
        "matcher": ".*",
        "hooks": [
          { "type": "command", "command": %q }
        ]
      }
    ]
  }
}`, scriptAbsPath)
	return fmt.Errorf(
		"hooks: %s contains malformed JSON; refusing to write.\n"+
			"Add the following manually under the \"hooks\" key:\n%s",
		sfPath, snippet,
	)
}
