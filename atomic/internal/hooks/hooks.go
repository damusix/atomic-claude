// Package hooks builds the Claude Code session-start payload and manages its
// settings.json registration.
package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/profile"
	"github.com/damusix/atomic-claude/atomic/internal/reminder"
	"github.com/damusix/atomic-claude/atomic/internal/where"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// ProfileRefresh is a test seam — spying on it avoids real tool detection and
// disk writes. DefaultProfileRefresh lets a test restore production behavior.
var DefaultProfileRefresh = profile.RefreshIfStale

var ProfileRefresh = profile.RefreshIfStale

// refreshProfile swallows errors: reminder context is the hook's job, and the
// refresh only rides along.
func refreshProfile(now time.Time) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	today := now.Format("2006-01-02")
	_, _ = ProfileRefresh(home, today, profile.DefaultRefreshDays)
}

// WikiCheckStalenessFn takes raw func types rather than wiki.ExecRunner so test
// function literals assign without a cast.
type WikiCheckStalenessFn func(claudeHome string, thresholdDays int, runner func(string, ...string) error, clock func() time.Time) ([]string, error)

// WikiCheckStaleness is a test seam; DefaultWikiCheckStaleness restores it.
var DefaultWikiCheckStaleness WikiCheckStalenessFn = func(claudeHome string, thresholdDays int, runner func(string, ...string) error, clock func() time.Time) ([]string, error) {
	return wiki.CheckStaleness(claudeHome, thresholdDays, wiki.ExecRunner(runner), clock)
}

var WikiCheckStaleness WikiCheckStalenessFn = DefaultWikiCheckStaleness

// wikiStalenessThresholdDays is the deterministic floor for wiki neglect; the
// "from memory" override is an LLM-layer concern, not this code's.
const wikiStalenessThresholdDays = 30

// checkWikiStaleness is silent on both errors and empty results — the hook must
// never block.
func checkWikiStaleness(now time.Time) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	claudeHome := filepath.Join(home, ".claude")
	// nil runner is deliberate: CheckStaleness only stats and reads in production;
	// the runner exists as a test-recording seam.
	nudges, err := WikiCheckStaleness(claudeHome, wikiStalenessThresholdDays, nil, func() time.Time { return now })
	if err != nil {
		return nil
	}
	return nudges
}

type WherePositionFn func(cwd, claudeMDPath string) (where.Report, error)

// WherePosition is a test seam — stubbing it keeps tests off the developer
// machine's ~/.claude/CLAUDE.md <wikis> registry. DefaultWherePosition restores it.
var DefaultWherePosition WherePositionFn = where.Resolve

var WherePosition WherePositionFn = DefaultWherePosition

// checkWherePosition returns nil on failure or on the plain no-wiki/no-realm
// case — the hook must never block.
func checkWherePosition(cwd string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	claudeMDPath := filepath.Join(home, ".claude", "CLAUDE.md")
	report, err := WherePosition(cwd, claudeMDPath)
	if err != nil {
		return nil
	}
	if isPlainPosition(report) {
		return nil
	}
	return []string{whereNudgeLine(report)}
}

func isPlainPosition(report where.Report) bool {
	return !report.RepoScope.Found && report.RealmScope.Position == where.RealmNone
}

func whereNudgeLine(report where.Report) string {
	var parts []string
	if report.RepoScope.Found {
		parts = append(parts, fmt.Sprintf("repo-scope wiki at %s", report.RepoScope.Path))
	}
	switch report.RealmScope.Position {
	case where.RealmRoot:
		parts = append(parts, fmt.Sprintf("realm root (%s)", report.RealmScope.RealmRoot))
	case where.RealmMember:
		parts = append(parts, fmt.Sprintf("realm member of %s", report.RealmScope.RealmRoot))
	case where.RealmOrphaned:
		parts = append(parts, fmt.Sprintf("orphaned under realm root %s (not a registered member)", report.RealmScope.RealmRoot))
	}
	return strings.Join(parts, "; ") + " — run `atomic where` for full detail"
}

const (
	// Registered inline: Claude Code runs hook commands through a shell, so this
	// resolves `atomic` on PATH without a wrapper script.
	sessionStartCommand = "atomic hooks session-start"

	// The pre-inline wrapper script older installs registered. Retained only so
	// Install can migrate it away and Uninstall can clean it up.
	legacyScriptName  = "session-start-reminders.sh"
	legacyHooksSubdir = ".claude/hooks"

	settingsRelPath = ".claude/settings.json"
	maxReminders    = 10
	previewMaxLen   = 80
	oldThresholdDay = 14
)

// SessionStart returns the JSON hook payload, or "" when there is nothing to
// surface. now is the reference time for relative date formatting.
func SessionStart(repoRoot string, now time.Time) (string, error) {
	refreshProfile(now)

	wikiNudges := checkWikiStaleness(now)

	whereNudges := checkWherePosition(repoRoot)

	// Filter once, so the body builder and the systemMessage count agree.
	rows, err := reminder.List(repoRoot)
	if err != nil {
		return "", err
	}

	pastDue := filterPastDue(rows, now)

	body, err := buildBody(pastDue, wikiNudges, whereNudges, now)
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

	// Age is measured over the surfaced set only — systemMessage warns about what
	// Claude actually sees.
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

// SessionStartText is SessionStart without the JSON envelope.
func SessionStartText(repoRoot string, now time.Time) (string, error) {
	refreshProfile(now)

	wikiNudges := checkWikiStaleness(now)
	whereNudges := checkWherePosition(repoRoot)

	rows, err := reminder.List(repoRoot)
	if err != nil {
		return "", fmt.Errorf("hooks session-start: list reminders: %w", err)
	}
	return buildBody(filterPastDue(rows, now), wikiNudges, whereNudges, now)
}

// buildBody orders orientation, then wiki staleness, then reminders.
func buildBody(pastDue []reminder.Row, wikiNudges []string, whereNudges []string, now time.Time) (string, error) {
	reminderBody, err := buildBodyFromPastDue(pastDue, now)
	if err != nil {
		return "", err
	}

	var sections []string
	if len(whereNudges) > 0 {
		sections = append(sections, buildNudgeSection("## Orientation", whereNudges))
	}
	if len(wikiNudges) > 0 {
		sections = append(sections, buildNudgeSection(fmt.Sprintf("## Wiki staleness (%d)", len(wikiNudges)), wikiNudges))
	}
	if reminderBody != "" {
		sections = append(sections, reminderBody)
	}

	return strings.Join(sections, "\n\n"), nil
}

func buildNudgeSection(header string, nudges []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n", header)
	for _, nudge := range nudges {
		fmt.Fprintf(&sb, "- %s\n", nudge)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// buildBodyFromPastDue applies the maxReminders cap to an already-filtered slice.
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

// filterPastDue keeps rows at or past their due time. A missing or unparseable
// Due surfaces the row rather than dropping it silently.
func filterPastDue(rows []reminder.Row, now time.Time) []reminder.Row {
	out := make([]reminder.Row, 0, len(rows))
	for _, r := range rows {
		if r.Due == "" {
			out = append(out, r)
			continue
		}
		due, err := time.Parse(time.RFC3339, r.Due)
		if err != nil {
			fmt.Fprintf(os.Stderr, "hooks: reminder %q has malformed due %q: %v; treating as past-due\n", r.ID, r.Due, err)
			out = append(out, r)
			continue
		}
		if !now.Before(due) {
			out = append(out, r)
		}
	}
	return out
}

// relativeAge renders "today", "3 days ago", "1 month ago", and so on.
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

// parseDateDays returns how many days ago createdDate (YYYY-MM-DD) was, both
// sides truncated to a UTC date so partial days never count.
func parseDateDays(createdDate string, now time.Time) (int, error) {
	t, err := time.Parse("2006-01-02", createdDate)
	if err != nil {
		return 0, err
	}
	created := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	diff := today.Sub(created)
	days := int(diff.Hours() / 24)
	if days < 0 {
		days = 0
	}
	return days, nil
}

// truncate caps s at maxLen runes, appending "…" when it cuts.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

func legacyScriptPath(scopeRoot string) string {
	return filepath.Join(scopeRoot, legacyHooksSubdir, legacyScriptName)
}

func settingsPath(scopeRoot string) string {
	return filepath.Join(scopeRoot, settingsRelPath)
}

// Install registers the inline command under scopeRoot; repoRoot is unused here.
// Any older wrapper-script registration is removed first so the hook cannot
// double-fire. Idempotent.
func Install(repoRoot, scopeRoot string) error {
	sfPath := settingsPath(scopeRoot)

	if err := migrateLegacy(sfPath, scopeRoot); err != nil {
		return err
	}

	return registerInSettings(sfPath, sessionStartCommand)
}

// Uninstall removes the registration and any lingering legacy wrapper script.
func Uninstall(repoRoot, scopeRoot string) error {
	if err := os.Remove(legacyScriptPath(scopeRoot)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("hooks uninstall: remove legacy script: %w", err)
	}

	sfPath := settingsPath(scopeRoot)
	if _, err := os.Stat(sfPath); os.IsNotExist(err) {
		return nil
	}

	if err := unregisterFromSettings(sfPath, sessionStartCommand); err != nil {
		return err
	}
	return unregisterFromSettings(sfPath, legacyScriptPath(scopeRoot))
}

// migrateLegacy is a no-op when no wrapper-script install exists. A malformed
// settings.json errors, so Install refuses to proceed.
func migrateLegacy(sfPath, scopeRoot string) error {
	if _, err := os.Stat(sfPath); err == nil {
		if err := unregisterFromSettings(sfPath, legacyScriptPath(scopeRoot)); err != nil {
			return err
		}
	}
	if err := os.Remove(legacyScriptPath(scopeRoot)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("hooks install: remove legacy script: %w", err)
	}
	return nil
}

func hasRegistration(settings map[string]any, command string) bool {
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
			if hm["command"] == command {
				return true
			}
		}
	}
	return false
}

// IsInstalled reports registration state in scopeRoot/.claude/settings.json.
// drifted means the hook still fires but through a legacy wrapper-script (or a
// half-migrated pair), and `atomic hooks install` should be re-run.
func IsInstalled(scopeRoot string) (installed bool, drifted bool, err error) {
	sfPath := settingsPath(scopeRoot)
	settings, _, _, readErr := readSettingsHujson(sfPath)
	if readErr != nil {
		return false, false, readErr
	}

	inline := hasRegistration(settings, sessionStartCommand)
	legacy := hasRegistration(settings, legacyScriptPath(scopeRoot))

	switch {
	case inline && !legacy:
		return true, false, nil
	case inline && legacy:
		return true, true, nil
	case legacy:
		return true, true, nil
	default:
		return false, false, nil
	}
}

// malformedSettingsError embeds the real command in the snippet so the user can
// paste it without substitution.
func malformedSettingsError(sfPath, command string) error {
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
}`, command)
	return fmt.Errorf(
		"hooks: %s contains malformed JSON; refusing to write.\n"+
			"Add the following manually under the \"hooks\" key:\n%s",
		sfPath, snippet,
	)
}
