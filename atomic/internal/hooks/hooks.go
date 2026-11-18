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

	"github.com/damusix/atomic-claude/atomic/internal/config"
	harnesskind "github.com/damusix/atomic-claude/atomic/internal/harness/kind"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
	"github.com/damusix/atomic-claude/atomic/internal/where"
	"github.com/damusix/atomic-claude/atomic/internal/workflow"
)

// ProfileRefresh is a test seam — spying on it avoids real tool detection and
// disk writes. DefaultProfileRefresh lets a test restore production behavior.
var DefaultProfileRefresh = profile.RefreshIfStale

var ProfileRefresh = profile.RefreshIfStale

// refreshProfile swallows errors: session context is the hook's job, and the
// refresh only rides along.
func refreshProfile(now time.Time) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	today := now.Format("2006-01-02")
	_, _ = ProfileRefresh(home, today, profile.DefaultRefreshDays)
}

// DefaultSeedOutputStyle lets a test restore SeedOutputStyleFn to production
// behavior by name.
var DefaultSeedOutputStyle = SeedOutputStyleInDir

// SeedOutputStyleFn is a test seam over SeedOutputStyleInDir so SessionStart tests
// don't touch the developer's real ~/.claude/settings.json.
var SeedOutputStyleFn = DefaultSeedOutputStyle

// seedOutputStyleSilent mirrors refreshProfile's shape: resolve home, bail on
// error, swallow everything else. Ungated by design — there is no "seeded
// once" marker, so a deleted key is re-seeded next session; the documented
// opt-out is `atomic config set output_style.seed false`. A --target install
// elsewhere simply fails the style-file guard here and no-ops.
func seedOutputStyleSilent() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	configDir := config.ClaudeConfigDir(home)
	_, _ = SeedOutputStyleFn(configDir, configDir, home)
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
	claudeMDPath := filepath.Join(config.ClaudeConfigDir(home), "CLAUDE.md")
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
	sessionStartCommand = "atomic hooks session-start --harness claude"

	// Registered by installs that predate --harness; still Atomic's.
	unflaggedSessionStartCommand = "atomic hooks session-start"

	// The pre-inline wrapper script older installs registered. Retained only so
	// Install can migrate it away and Uninstall can clean it up.
	legacyScriptName = "session-start-reminders.sh"
	hooksSubdir      = "hooks"

	settingsFileName = "settings.json"
)

// SessionStart returns the JSON hook payload, or "" when there is nothing to
// surface. kind is the harness (claude, omp, codex) the session runs under.
func SessionStart(repoRoot, kind string, now time.Time) (string, error) {
	body := SessionStartText(repoRoot, kind, now)
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
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("hooks session-start: marshal: %w", err)
	}
	return string(out), nil
}

// SessionStartText is SessionStart without the JSON envelope.
func SessionStartText(repoRoot, kind string, now time.Time) string {
	refreshProfile(now)
	if kind == string(harnesskind.Claude) {
		seedOutputStyleSilent()
	}
	return buildBody(repoRoot, kind)
}

// buildBody drops a section that fails rather than blocking the session.
func buildBody(repoRoot, kind string) string {
	var sections []string
	if whereNudges := checkWherePosition(repoRoot); len(whereNudges) > 0 {
		sections = append(sections, buildNudgeSection("## Orientation", whereNudges))
	}
	resolved, err := workflow.Resolve(workflow.Options{Dir: repoRoot, Harness: kind})
	if err == nil {
		sections = append(sections, "## Workflow preferences\n"+
			strings.TrimRight(workflow.RenderText(resolved), "\n"))
	}
	return strings.Join(sections, "\n\n")
}

func buildNudgeSection(header string, nudges []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n", header)
	for _, nudge := range nudges {
		fmt.Fprintf(&sb, "- %s\n", nudge)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// configDirForScope is the `.claude` directory a scope root owns, under a project
// root or under $HOME for the user scope.
func configDirForScope(scopeRoot string) string {
	return filepath.Join(scopeRoot, ".claude")
}

func legacyScriptPath(configDir string) string {
	return filepath.Join(configDir, hooksSubdir, legacyScriptName)
}

// SettingsPath resolves a scope root to its settings.json. Exported so
// claudeinstall does not rebuild the literal a third time.
func SettingsPath(scopeRoot string) string {
	return SettingsPathInDir(configDirForScope(scopeRoot))
}

// SettingsPathInDir resolves a Claude config directory to its settings file. It
// appends no `.claude`, so a CLAUDE_CONFIG_DIR root resolves its own file.
func SettingsPathInDir(configDir string) string {
	return filepath.Join(configDir, settingsFileName)
}

// SameDir reports whether two paths name the same directory. Callers ask this
// to decide whether to skip work, so a false negative is silent and costly.
//
// Two things defeat a plain string compare, and both are routine: a $HOME or
// temp dir that is itself a symlink (the norm on macOS), and a relative path
// such as `--target .claude`. A directory that does not exist yet cannot be
// resolved at all, which is the common case on a first install, so resolution
// falls back to the parent.
func SameDir(a, b string) bool {
	return resolveDir(a) == resolveDir(b)
}

// resolveDir returns the most-resolved absolute form of path it can reach,
// degrading one step at a time rather than giving up.
func resolveDir(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	// The leaf may not exist yet; resolving the parent still catches a
	// symlinked ancestor.
	parent, base := filepath.Split(abs)
	if resolved, err := filepath.EvalSymlinks(filepath.Clean(parent)); err == nil {
		return filepath.Join(resolved, base)
	}
	return abs
}

// Install registers the inline command under scopeRoot's config directory;
// repoRoot is unused. skipped reports a read-only settings.json left untouched.
func Install(repoRoot, scopeRoot string) (skipped bool, err error) {
	return InstallInDir(configDirForScope(scopeRoot))
}

// InstallInDir registers the inline command in an explicit Claude config
// directory's settings.json, never the default target beside it.
func InstallInDir(configDir string) (skipped bool, err error) {
	sfPath := SettingsPathInDir(configDir)

	if skipped, err := migrateLegacy(sfPath, configDir); err != nil || skipped {
		return skipped, err
	}

	return registerInSettings(sfPath, sessionStartCommand)
}

// Uninstall removes the registration and any lingering legacy wrapper script.
// skipped reports a read-only settings.json left untouched.
func Uninstall(repoRoot, scopeRoot string) (skipped bool, err error) {
	return UninstallInDir(configDirForScope(scopeRoot))
}

// UninstallInDir removes the registration from an explicit Claude config
// directory, reaching only that root's settings.json.
func UninstallInDir(configDir string) (skipped bool, err error) {
	if err := os.Remove(legacyScriptPath(configDir)); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("hooks uninstall: remove legacy script: %w", err)
	}

	sfPath := SettingsPathInDir(configDir)
	if _, err := os.Stat(sfPath); os.IsNotExist(err) {
		return false, nil
	}

	if skipped, err := unregisterFromSettings(sfPath, sessionStartCommand); err != nil || skipped {
		return skipped, err
	}
	if skipped, err := unregisterFromSettings(sfPath, legacyScriptPath(configDir)); err != nil || skipped {
		return skipped, err
	}

	// This uninstall never deletes the style file itself, so the key is removed
	// only once the file it names is already gone. See the "Remove on uninstall"
	// flow in docs/spec/output-style-seed.md.
	_, skipped, err = RemoveOutputStyleIfAtomicInDir(configDir)
	return skipped, err
}

// migrateLegacy is a no-op when no wrapper-script install exists. A malformed
// settings.json errors, so Install refuses to proceed.
func migrateLegacy(sfPath, configDir string) (skipped bool, err error) {
	if _, err := os.Stat(sfPath); err == nil {
		if skipped, err := unregisterFromSettings(sfPath, legacyScriptPath(configDir)); err != nil || skipped {
			return skipped, err
		}
	}
	if err := os.Remove(legacyScriptPath(configDir)); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("hooks install: remove legacy script: %w", err)
	}
	return false, nil
}

func commandMatches(want string, got any) bool {
	return got == want || (want == sessionStartCommand && got == unflaggedSessionStartCommand)
}

func hasRegistration(settings map[string]any, command string) bool {
	return hasEventHandler(settings, "SessionStart", command)
}

func hasEventHandler(settings map[string]any, event, command string) bool {
	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false
	}
	ss, ok := hooksMap[event].([]any)
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
			if commandMatches(command, hm["command"]) {
				return true
			}
		}
	}
	return false
}

// IsInstalled reports registration state in scopeRoot's config directory.
// drifted means the hook still fires but through a legacy wrapper-script (or a
// half-migrated pair), and `atomic hooks install` should be re-run.
func IsInstalled(scopeRoot string) (installed bool, drifted bool, err error) {
	return IsInstalledInDir(configDirForScope(scopeRoot))
}

// IsInstalledInDir reports registration state in an explicit Claude config
// directory's settings.json.
func IsInstalledInDir(configDir string) (installed bool, drifted bool, err error) {
	sfPath := SettingsPathInDir(configDir)
	settings, _, _, readErr := readSettingsHujson(sfPath)
	if readErr != nil {
		return false, false, readErr
	}

	inline := hasRegistration(settings, sessionStartCommand)
	legacy := hasRegistration(settings, legacyScriptPath(configDir))

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
        "matcher": %q,
        "hooks": [
          { "type": "command", "command": %q }
        ]
      }
    ]
  }
}`, sessionStartMatcher(command), command)
	return fmt.Errorf(
		"hooks: %s contains malformed JSON; refusing to write.\n"+
			"Add the following manually under the \"hooks\" key:\n%s",
		sfPath, snippet,
	)
}
