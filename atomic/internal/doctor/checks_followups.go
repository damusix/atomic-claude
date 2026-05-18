package doctor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// entryHeadingRe matches ### F-<id> — <title> or ### F-<id> - <title>
// (em-dash U+2014 or ASCII hyphen).
var entryHeadingRe = regexp.MustCompile(`^###\s+F-([A-Za-z0-9-]+)\s*[-—]`)

// bucketHeadingRe matches H2 headings that are recognized severity buckets.
var bucketHeadingRe = regexp.MustCompile(`^##\s`)

// recognizedBuckets is a set of H2 heading prefixes that are valid severity buckets.
// Match is case-insensitive on the emoji+label; we just check "## " prefix and
// that it is NOT "## Closed" — closed entries are excluded from schema validation.
var closedBucketRe = regexp.MustCompile(`(?i)^##\s+(closed)`)

// checkFollowups implements category 6: followups schema.
func checkFollowups(_ Opts) Result {
	cwd, err := os.Getwd()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not get cwd: %v", err)}
	}
	root := gitToplevel(cwd)
	return RunCheckFollowupsWith(root)
}

// RunCheckFollowupsWith runs the followups schema check against an explicit root.
// Exported for testing; production callers use checkFollowups.
func RunCheckFollowupsWith(root string) Result {
	path := filepath.Join(root, ".claude", "project", "followups.md")

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Severity: PASS, Detail: "no .claude/project/followups.md"}
		}
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not read followups.md: %v", err)}
	}
	defer f.Close()

	type entry struct {
		id        string
		inBucket  bool
		hasOrigin bool
		inClosed  bool
	}

	var entries []entry
	var current *entry
	inBucket := false
	inClosed := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// H2 heading — update bucket state.
		if bucketHeadingRe.MatchString(line) {
			if closedBucketRe.MatchString(line) {
				inBucket = false
				inClosed = true
			} else {
				inBucket = true
				inClosed = false
			}
			current = nil
			continue
		}

		// H3 F-entry heading.
		if m := entryHeadingRe.FindStringSubmatch(line); m != nil {
			// Save previous entry.
			if current != nil {
				entries = append(entries, *current)
			}
			e := entry{
				id:       m[1],
				inBucket: inBucket,
				inClosed: inClosed,
			}
			current = &e
			continue
		}

		// Any other heading terminates current entry tracking.
		if strings.HasPrefix(line, "#") {
			if current != nil {
				entries = append(entries, *current)
				current = nil
			}
			continue
		}

		// Check for Origin: in current entry body.
		if current != nil && !current.inClosed {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(trimmed), "origin:") {
				current.hasOrigin = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not read followups.md: %v", err)}
	}
	// Flush last entry.
	if current != nil {
		entries = append(entries, *current)
	}

	// Validate entries (ignore closed ones).
	var malformed []string
	validated := 0
	for _, e := range entries {
		if e.inClosed {
			continue
		}
		validated++
		if !e.inBucket || !e.hasOrigin {
			malformed = append(malformed, "F-"+e.id)
		}
	}

	if len(malformed) == 0 {
		return Result{Severity: PASS, Detail: fmt.Sprintf("%d entries, schema OK", validated)}
	}

	// List up to 3 IDs then "..." if more.
	listed := malformed
	suffix := ""
	if len(listed) > 3 {
		listed = listed[:3]
		suffix = " ..."
	}
	return Result{
		Severity: WARN,
		Detail:   fmt.Sprintf("%d entries malformed: %s%s", len(malformed), strings.Join(listed, ", "), suffix),
	}
}
