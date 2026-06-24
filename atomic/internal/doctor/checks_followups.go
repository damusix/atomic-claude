package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/followups"
)

// checkFollowups implements category 6: followups folder integrity.
func checkFollowups(opts Opts) Result {
	root := opts.RepoRoot
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Result{Severity: WARN, Detail: fmt.Sprintf("could not get cwd: %v", err)}
		}
		root = gitToplevelFn(cwd)
	}
	return RunCheckFollowupsWith(root)
}

// RunCheckFollowupsWith runs the followups check against an explicit root.
// Exported for testing; production callers use checkFollowups.
//
// Decision table:
//   - folder absent → SKIP
//   - folder present, invalid/missing frontmatter in any entry → WARN
//   - folder present, one or more entries past review_by → WARN
//   - folder present, INDEX.md missing or byte-differs from re-render → WARN
//   - folder present, all entries fresh, INDEX in sync → PASS
func RunCheckFollowupsWith(root string) Result {
	folderPath := filepath.Join(root, ".claude", "project", "followups")

	if !dirExists(folderPath) {
		return Result{Severity: SKIP, Detail: "no followups folder"}
	}

	// Folder exists — load entries.
	entries, parseErrs, err := followups.LoadEntriesWithErrors(folderPath)
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not read followups folder: %v", err)}
	}

	var issues []string

	// WARN: invalid/missing frontmatter.
	if len(parseErrs) > 0 {
		filenames := make([]string, 0, len(parseErrs))
		for name := range parseErrs {
			filenames = append(filenames, name)
		}
		// Sort for deterministic output.
		sortStrings(filenames)
		listed := filenames
		suffix := ""
		if len(listed) > 3 {
			listed = listed[:3]
			suffix = " ..."
		}
		issues = append(issues, fmt.Sprintf("invalid frontmatter in: %s%s", strings.Join(listed, ", "), suffix))
	}

	// WARN: stale entries.
	today := time.Now()
	stale := staleEntries(entries, today)
	if len(stale) > 0 {
		listed := stale
		suffix := ""
		if len(listed) > 3 {
			listed = listed[:3]
			suffix = " ..."
		}
		issues = append(issues, fmt.Sprintf("%d stale entr%s: %s%s — run /follow-up review",
			len(stale), pluralSuffix(len(stale)), strings.Join(listed, ", "), suffix))
	}

	// WARN: INDEX.md missing or out of sync (byte-compare).
	indexPath := filepath.Join(folderPath, "INDEX.md")
	expected := followups.Render(entries, today)
	actual, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			issues = append(issues, "INDEX.md missing — run `atomic followups render`")
		} else {
			issues = append(issues, fmt.Sprintf("could not read INDEX.md: %v", err))
		}
	} else if string(actual) != expected {
		issues = append(issues, "INDEX.md out of sync — run `atomic followups render`")
	}

	if len(issues) > 0 {
		return Result{Severity: WARN, Detail: strings.Join(issues, "; ")}
	}

	return Result{
		Severity: PASS,
		Detail:   fmt.Sprintf("%d entries, INDEX in sync", len(entries)),
	}
}

// dirExists returns true when path is a directory that can be stat'd.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// staleEntries returns the IDs of entries past their review_by date.
func staleEntries(entries []followups.Entry, today time.Time) []string {
	t := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	var out []string
	for _, e := range entries {
		if e.ReviewBy == "" {
			continue
		}
		rb, err := time.Parse("2006-01-02", e.ReviewBy)
		if err != nil {
			continue
		}
		if t.After(rb) {
			out = append(out, e.ID)
		}
	}
	sortStrings(out)
	return out
}

// pluralSuffix returns "y" when n==1 and "ies" otherwise (for "entry"/"entries").
func pluralSuffix(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// sortStrings sorts a string slice in place (insertion sort — n is always small).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
