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
	body, err := buildAdditionalContext(repoRoot, now)
	if err != nil {
		return "", err
	}
	if body == "" {
		return "", nil
	}

	rows, err := reminder.List(repoRoot)
	if err != nil {
		return "", err
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
	oldCount := 0
	for _, r := range rows {
		d, err := parseDateDays(r.Created, now)
		if err != nil {
			continue
		}
		if d > oldThresholdDay {
			oldCount++
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
	_ = oldCount

	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("hooks session-start: marshal: %w", err)
	}
	return string(out), nil
}

// SessionStartText returns the plain-markdown version of the session-start
// context (no JSON envelope). Returns empty string when no reminders exist.
func SessionStartText(repoRoot string, now time.Time) (string, error) {
	return buildAdditionalContext(repoRoot, now)
}

// buildAdditionalContext constructs the markdown body for the reminder list.
// Returns "" when there are no reminders.
func buildAdditionalContext(repoRoot string, now time.Time) (string, error) {
	rows, err := reminder.List(repoRoot)
	if err != nil {
		return "", fmt.Errorf("hooks session-start: list reminders: %w", err)
	}
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

// registerInSettings adds the hook entry to settings.json if not already present.
func registerInSettings(sfPath, scriptAbsPath string) error {
	settings, raw, err := readSettings(sfPath)
	if err != nil {
		return err
	}

	// Check idempotency: look for existing entry with the same command.
	if hasRegistration(settings, scriptAbsPath) {
		return nil
	}

	// Build the new entry.
	newEntry := map[string]any{
		"matcher": ".*",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": scriptAbsPath,
			},
		},
	}

	// Ensure hooks key exists.
	hooksMap := ensureHooksMap(settings)

	// Append to SessionStart array.
	ss, _ := hooksMap["SessionStart"].([]any)
	hooksMap["SessionStart"] = append(ss, newEntry)
	settings["hooks"] = hooksMap

	return writeSettings(sfPath, settings, raw)
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

// unregisterFromSettings removes the entry matching scriptAbsPath from settings.json.
func unregisterFromSettings(sfPath, scriptAbsPath string) error {
	settings, raw, err := readSettings(sfPath)
	if err != nil {
		return err
	}

	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil
	}

	ss, ok := hooksMap["SessionStart"].([]any)
	if !ok {
		return nil
	}

	filtered := ss[:0]
	for _, entry := range ss {
		e, ok := entry.(map[string]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		inner, ok := e["hooks"].([]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		matched := false
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if hm["command"] == scriptAbsPath {
				matched = true
				break
			}
		}
		if !matched {
			filtered = append(filtered, entry)
		}
	}

	if len(filtered) == 0 {
		delete(hooksMap, "SessionStart")
	} else {
		hooksMap["SessionStart"] = filtered
	}

	if len(hooksMap) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooksMap
	}

	return writeSettings(sfPath, settings, raw)
}

// readSettings reads settings.json if it exists, returning a parsed map and
// the original raw bytes (used to detect malformed JSON). If the file does not
// exist, returns an empty map and nil raw.
func readSettings(sfPath string) (map[string]any, []byte, error) {
	raw, err := os.ReadFile(sfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil, nil
		}
		return nil, nil, fmt.Errorf("hooks: read settings.json: %w", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, raw, malformedError(sfPath)
	}
	return settings, raw, nil
}

// writeSettings writes settings back to disk with 2-space indent.
func writeSettings(sfPath string, settings map[string]any, _ []byte) error {
	if err := os.MkdirAll(filepath.Dir(sfPath), 0o755); err != nil {
		return fmt.Errorf("hooks: mkdir for settings.json: %w", err)
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("hooks: marshal settings.json: %w", err)
	}
	if err := os.WriteFile(sfPath, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("hooks: write settings.json: %w", err)
	}
	return nil
}

// ensureHooksMap returns the hooks sub-map from settings, creating it if absent.
func ensureHooksMap(settings map[string]any) map[string]any {
	existing, ok := settings["hooks"].(map[string]any)
	if !ok {
		existing = map[string]any{}
	}
	return existing
}

// malformedError returns the error for a malformed settings.json, including the
// manual-registration snippet the user can paste.
func malformedError(sfPath string) error {
	snippet := `{
  "hooks": {
    "SessionStart": [
      {
        "matcher": ".*",
        "hooks": [
          { "type": "command", "command": "<scope>/.claude/hooks/session-start-reminders.sh" }
        ]
      }
    ]
  }
}`
	return fmt.Errorf(
		"hooks: %s contains malformed JSON; refusing to write.\n"+
			"Add the following manually under the \"hooks\" key:\n%s",
		sfPath, snippet,
	)
}
