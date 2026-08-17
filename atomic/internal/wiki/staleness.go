package wiki

// Staleness nudges for registered wikis, driven by the <wikis> block in
// <claudeHome>/CLAUDE.md.
//
// Hard contract: zero git spawns. This runs at session start, where a
// subprocess per registered wiki would be felt. Path comparisons use
// Abs+Clean and never resolve symlinks, for the same reason.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExecRunner exists so a test can hand CheckStaleness a recording no-op and
// assert it was never invoked, proving the zero-git-spawn contract.
type ExecRunner func(name string, args ...string) error

// ReadWikiIndexPaths returns the registered index.md paths, or nothing at all
// when the file or block is absent. Those cases are never errors.
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

// CheckStaleness returns one human-readable nudge per wiki that is either
// neglected past thresholdDays or carrying a .dirty marker. It never fails:
// an unreadable registry, index, or date skips the wiki in question.
func CheckStaleness(claudeHome string, thresholdDays int, runner ExecRunner, clock func() time.Time) ([]string, error) {
	_ = runner

	claudeMDPath := filepath.Join(claudeHome, "CLAUDE.md")
	indexPaths, err := ReadWikiIndexPaths(claudeMDPath)
	if err != nil {
		return nil, nil
	}
	if len(indexPaths) == 0 {
		return nil, nil
	}

	now := clock()
	var nudges []string

	for _, rawIndexPath := range indexPaths {
		indexPath := filepath.Clean(rawIndexPath)
		wikiDir := filepath.Dir(indexPath)

		data, readErr := os.ReadFile(indexPath)
		if readErr != nil {
			continue
		}

		content := string(data)
		needsNudge := false
		reason := ""

		// .dirty outranks age: it means known drift, not suspected neglect.
		dirtyPath := filepath.Join(wikiDir, ".dirty")
		if _, statErr := os.Stat(dirtyPath); statErr == nil {
			needsNudge = true
			reason = "uncommitted changes since last refresh (.dirty)"
		}

		if !needsNudge {
			generatedDate := extractGeneratedDate(content)
			if generatedDate == "" {
				// An unreadable date is treated as stale, never as fresh.
				needsNudge = true
				reason = "wiki scan date unknown — re-run atomic wiki scan"
			} else {
				generated, parseErr := time.Parse("2006-01-02", generatedDate)
				if parseErr != nil {
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

// extractGeneratedDate reads the <wiki-scan> open tag's generated attribute,
// or "" when there is none.
func extractGeneratedDate(content string) string {
	openIdx := strings.Index(content, scanMarkerOpen)
	if openIdx == -1 {
		return ""
	}
	closeTagIdx := strings.Index(content[openIdx:], ">")
	if closeTagIdx == -1 {
		return ""
	}
	openTagLine := content[openIdx : openIdx+closeTagIdx+1]
	return attrValue(openTagLine, "generated")
}

// MarkDirty touches <root>/wiki/.dirty when cwd sits under a registered wiki
// root, and does nothing otherwise.
func MarkDirty(claudeHome, cwd string) error {
	claudeMDPath := filepath.Join(claudeHome, "CLAUDE.md")
	indexPaths, err := ReadWikiIndexPaths(claudeMDPath)
	if err != nil || len(indexPaths) == 0 {
		return nil
	}

	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("wiki mark-dirty: normalize cwd: %w", err)
	}
	absCwd = filepath.Clean(absCwd)

	for _, rawIndexPath := range indexPaths {
		absIndex, err := filepath.Abs(rawIndexPath)
		if err != nil {
			continue
		}
		absIndex = filepath.Clean(absIndex)

		// index.md sits at <root>/wiki/index.md.
		wikiDir := filepath.Dir(absIndex)
		root := filepath.Dir(wikiDir)
		absRoot := filepath.Clean(root)

		if isUnder(absCwd, absRoot) {
			dirtyPath := filepath.Join(wikiDir, ".dirty")
			if err := touchFile(dirtyPath); err != nil {
				return fmt.Errorf("wiki mark-dirty: touch %s: %w", dirtyPath, err)
			}
			// A cwd can be under only one root.
			return nil
		}
	}

	return nil
}

// isUnder compares normalized paths, requiring a separator after the parent so
// /home/user/realm-other does not read as inside /home/user/realm.
func isUnder(child, parent string) bool {
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)

	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

// touchFile creates path if absent. The marker is existence-based, so an
// existing file's mtime is deliberately left alone.
func touchFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}
