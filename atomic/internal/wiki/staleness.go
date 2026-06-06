package wiki

// staleness.go — CP5: CheckStaleness + MarkDirty + ReadWikiIndexPaths.
//
// CheckStaleness reads the <wikis> block from <claudeHome>/CLAUDE.md, and for
// each registered wiki index reads the <wiki-scan> generated date and stats the
// sibling .dirty marker.  It emits one nudge line per wiki where:
//
//   - (clock() - generated) > thresholdDays, OR
//   - .dirty marker exists.
//
// CONTRACT: zero git spawns.  The runner parameter exists solely so a test can
// pass a recording runner and assert it was NEVER called.  CheckStaleness itself
// only performs os.ReadFile / os.Stat — no exec.Command.
//
// MarkDirty reads the <wikis> block, normalises each registered index path, derives
// the wiki root (parent of the wiki/ directory = parent of parent of index.md),
// and checks whether cwd is under that root via a normalized path-prefix comparison
// (filepath.Abs + filepath.Clean, no symlink resolution).  If so it touches
// <root>/wiki/.dirty.  No-op (returns nil) when cwd is under no registered root.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExecRunner is the function signature for spawning an external command.
// CheckStaleness accepts this as a parameter to allow tests to inject a
// recording no-op that proves no git is ever spawned.
type ExecRunner func(name string, args ...string) error

// ReadWikiIndexPaths parses the <wikis> block from claudeMDPath and returns the
// list of registered wiki index.md absolute paths.  Returns an empty slice when
// the block is absent or the file does not exist — never returns an error for
// those cases (best-effort, matches the spec's non-fatal requirement).
func ReadWikiIndexPaths(claudeMDPath string) ([]string, error) {
	data, err := os.ReadFile(claudeMDPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("wiki registry read: %w", err)
	}

	content := string(data)
	openIdx := strings.Index(content, wikisMarkerOpen)
	if openIdx == -1 {
		return nil, nil
	}
	closeIdx := strings.Index(content[openIdx:], wikisMarkerClose)
	if closeIdx == -1 {
		return nil, nil
	}

	blockContent := content[openIdx+len(wikisMarkerOpen) : openIdx+closeIdx]

	var paths []string
	for _, line := range strings.Split(blockContent, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "- ") {
			continue
		}
		p := strings.TrimPrefix(line, "- ")
		p = strings.TrimSpace(p)
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// CheckStaleness checks registered wikis for neglect (old generated date) or
// uncommitted drift (.dirty marker).
//
// Parameters:
//   - claudeHome: path to ~/.claude; CLAUDE.md is read from here.
//   - thresholdDays: number of days after which a wiki is considered stale.
//   - runner: injected exec runner — CheckStaleness itself never calls it;
//     it is accepted solely so tests can assert it was never invoked.
//   - clock: returns "now" for age calculation; inject a fixed clock in tests.
//
// Returns a slice of human-readable nudge lines (one per stale wiki) and nil
// error.  Missing CLAUDE.md, absent <wikis> block, missing wiki index files,
// and garbled generated dates are all non-fatal — the affected wiki is skipped.
func CheckStaleness(claudeHome string, thresholdDays int, runner ExecRunner, clock func() time.Time) ([]string, error) {
	// runner is accepted but never called — its only purpose is to let tests
	// assert zero invocations (proving no git spawns).
	_ = runner

	claudeMDPath := filepath.Join(claudeHome, "CLAUDE.md")
	indexPaths, err := ReadWikiIndexPaths(claudeMDPath)
	if err != nil {
		// Non-fatal: if we can't read the registry, return no nudges.
		return nil, nil
	}
	if len(indexPaths) == 0 {
		return nil, nil
	}

	now := clock()
	var nudges []string

	for _, rawIndexPath := range indexPaths {
		// Normalize the index path.
		indexPath := filepath.Clean(rawIndexPath)
		wikiDir := filepath.Dir(indexPath)

		// Read the index.md.
		data, readErr := os.ReadFile(indexPath)
		if readErr != nil {
			// Missing index — skip this wiki (non-fatal).
			continue
		}

		content := string(data)
		needsNudge := false
		reason := ""

		// --- Check .dirty marker ---
		dirtyPath := filepath.Join(wikiDir, ".dirty")
		if _, statErr := os.Stat(dirtyPath); statErr == nil {
			// .dirty exists → nudge regardless of age.
			needsNudge = true
			reason = "uncommitted changes since last refresh (.dirty)"
		}

		// --- Check generated age ---
		if !needsNudge {
			generatedDate := extractGeneratedDate(content)
			if generatedDate == "" {
				// No generated date — treat as stale (fail-safe).
				needsNudge = true
				reason = "wiki scan date unknown — re-run atomic wiki scan"
			} else {
				generated, parseErr := time.Parse("2006-01-02", generatedDate)
				if parseErr != nil {
					// Garbled date — treat as stale (fail-safe).
					needsNudge = true
					reason = "wiki scan date unreadable — re-run atomic wiki scan"
				} else {
					ageDays := int(now.Sub(generated).Hours() / 24)
					if ageDays > thresholdDays {
						needsNudge = true
						reason = fmt.Sprintf("wiki not refreshed in %d days (threshold: %d)", ageDays, thresholdDays)
					}
				}
			}
		}

		if needsNudge {
			nudges = append(nudges, fmt.Sprintf("wiki %s is stale: %s — run /refresh-wiki", indexPath, reason))
		}
	}

	return nudges, nil
}

// extractGeneratedDate extracts the generated="YYYY-MM-DD" attribute value from
// the <wiki-scan ...> open tag in content.  Returns "" when not found.
func extractGeneratedDate(content string) string {
	openIdx := strings.Index(content, scanMarkerOpen)
	if openIdx == -1 {
		return ""
	}
	// Find the end of the open tag (">").
	closeTagIdx := strings.Index(content[openIdx:], ">")
	if closeTagIdx == -1 {
		return ""
	}
	openTagLine := content[openIdx : openIdx+closeTagIdx+1]
	return attrValue(openTagLine, "generated")
}

// MarkDirty reads the <wikis> block, checks whether cwd is under any registered
// wiki root (normalized path-prefix, no symlink resolution), and if so touches
// <root>/wiki/.dirty.  No-op when cwd is under no registered root.
// Never errors on missing CLAUDE.md or absent <wikis> block.
func MarkDirty(claudeHome, cwd string) error {
	claudeMDPath := filepath.Join(claudeHome, "CLAUDE.md")
	indexPaths, err := ReadWikiIndexPaths(claudeMDPath)
	if err != nil || len(indexPaths) == 0 {
		// Non-fatal: no wikis registered.
		return nil
	}

	// Normalize cwd.
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("wiki mark-dirty: normalize cwd: %w", err)
	}
	absCwd = filepath.Clean(absCwd)

	for _, rawIndexPath := range indexPaths {
		// Normalize the index path.
		absIndex, err := filepath.Abs(rawIndexPath)
		if err != nil {
			continue
		}
		absIndex = filepath.Clean(absIndex)

		// wikiDir = parent of index.md (= <root>/wiki/)
		wikiDir := filepath.Dir(absIndex)
		// root = parent of wiki/ dir
		root := filepath.Dir(wikiDir)
		absRoot := filepath.Clean(root)

		// Check: is cwd under root?  Use path-prefix with a separator suffix to
		// avoid false matches (e.g. /home/user/realm-other matching /home/user/realm).
		if isUnder(absCwd, absRoot) {
			dirtyPath := filepath.Join(wikiDir, ".dirty")
			if err := touchFile(dirtyPath); err != nil {
				return fmt.Errorf("wiki mark-dirty: touch %s: %w", dirtyPath, err)
			}
			// A cwd can only be under one root — stop after first match.
			return nil
		}
	}

	// cwd is under no registered root — no-op.
	return nil
}

// isUnder reports whether child is equal to or under parent, using normalized
// path-prefix comparison (no symlink resolution).
func isUnder(child, parent string) bool {
	// Ensure both are clean (already done by caller, but be defensive).
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)

	if child == parent {
		return true
	}
	// child must start with parent + separator.
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

// touchFile creates the file at path if absent (existence-based marker, not
// mtime-based).  It does NOT update the modification time of an existing file.
func touchFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}
