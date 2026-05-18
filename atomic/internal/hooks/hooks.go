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
	// Call reminder.List once and filter to past-due immediately so both the
	// body builder and the systemMessage count use the same surfaced set.
	rows, err := reminder.List(repoRoot)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}

	pastDue := filterPastDue(rows, now)

	body, err := buildBodyFromPastDue(pastDue, now)
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

	// Check for old reminders (older than oldThresholdDay days) among the
	// surfaced (past-due) set only — systemMessage warns about what Claude sees.
	oldestDays := 0
	for _, r := range pastDue {
		d, err := parseDateDays(r.Created, now)
		if err != nil {
			continue
		}
		if d > oldestDays {
			oldestDays = d
		}
	}
	if oldestDays > oldThresholdDay {
		n := len(pastDue)
		word := "reminders"
		if n == 1 {
			word = "reminder"
		}
		payload["systemMessage"] = fmt.Sprintf(
			"%d %s pending, oldest is %d days old",
			n, word, oldestDays,
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

// buildBodyFromPastDue constructs the markdown body from an already-filtered
// past-due slice. Applies the 10-item cap.
func buildBodyFromPastDue(pastDue []reminder.Row, now time.Time) (string, error) {
	if len(pastDue) == 0 {
		return "", nil
	}

	total := len(pastDue)
	shown := pastDue
	overflow := 0
	if total > maxReminders {
		shown = pastDue[:maxReminders]
		overflow = total - maxReminders
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Pending reminders (%d)\n", total)
	for _, r := range shown {
		preview := truncate(r.Preview, previewMaxLen)
		ago := relativeAge(r.Created, now)
		fmt.Fprintf(&sb, "- [%s] should-remind-user: true — %s (created %s)\n", r.ID, preview, ago)
	}
	if overflow > 0 {
		fmt.Fprintf(&sb, "- (and %d more)\n", overflow)
	}

	return strings.TrimRight(sb.String(), "\n"), nil
}

// filterPastDue returns only the rows that are past-due relative to now.
// A row is past-due when:
//   - Due is empty (legacy reminder — no due field): surface to avoid silent loss.
//   - Due parses as RFC3339 and now >= due.
//   - Due is present but cannot be parsed: surface defensively and log to stderr.
func filterPastDue(rows []reminder.Row, now time.Time) []reminder.Row {
	out := make([]reminder.Row, 0, len(rows))
	for _, r := range rows {
		if r.Due == "" {
			// Legacy reminder — treat as past-due.
			out = append(out, r)
			continue
		}
		due, err := time.Parse(time.RFC3339, r.Due)
		if err != nil {
			// Malformed due value — surface defensively.
			fmt.Fprintf(os.Stderr, "hooks: reminder %q has malformed due %q: %v; treating as past-due\n", r.ID, r.Due, err)
			out = append(out, r)
			continue
		}
		if !now.Before(due) {
			// now >= due: past-due.
			out = append(out, r)
		}
		// now < due: not yet past-due — silent.
	}
	return out
}

// buildAdditionalContextFromRows filters rows to past-due reminders and
// delegates body formatting to buildBodyFromPastDue.
func buildAdditionalContextFromRows(rows []reminder.Row, now time.Time) (string, error) {
	return buildBodyFromPastDue(filterPastDue(rows, now), now)
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
	if err := os.WriteFile(sp, []byte(expectedScriptContent), 0o755); err != nil {
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

// expectedScriptContent is the exact content Install writes to the hook script.
const expectedScriptContent = "#!/usr/bin/env bash\nexec atomic hooks session-start\n"

// IsInstalled reports whether the session-start hook is registered in
// scopeRoot/.claude/settings.json and the hook script matches expected content.
//
// Returns:
//   - installed=true, drifted=false, err=nil  → hook registered and script matches
//   - installed=false, drifted=false, err=nil  → hook not registered (settings missing or no entry)
//   - installed=true, drifted=true, err=nil   → hook registered but script content differs
//   - installed=false, drifted=false, err!=nil → settings.json unreadable / malformed
func IsInstalled(scopeRoot string) (installed bool, drifted bool, err error) {
	sfPath := settingsPath(scopeRoot)
	settings, _, _, readErr := readSettingsHujson(sfPath)
	if readErr != nil {
		// Could not read / parse settings.json.
		return false, false, readErr
	}

	sp := scriptPath(scopeRoot)
	if !hasRegistration(settings, sp) {
		return false, false, nil
	}

	// Hook is registered. Now verify script content.
	raw, err := os.ReadFile(sp)
	if err != nil {
		// Script file missing — registered but drifted.
		return true, true, nil
	}
	if string(raw) != expectedScriptContent {
		return true, true, nil
	}
	return true, false, nil
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
