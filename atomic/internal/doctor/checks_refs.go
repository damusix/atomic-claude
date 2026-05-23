package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// candidateFiles is the search order for @-refs per the atomic-signals skill.
// Checked in order; first file containing both refs wins.
var candidateFiles = []string{
	"claude.local.md",
	"CLAUDE.local.md",
	"CLAUDE.md",
	"claude.md",
}

const (
	deterministicSignalsRef = "@.claude/project/deterministic-signals.md"
	signalsRef              = "@.claude/project/signals.md"
)

// checkRefs implements category 4: @-refs wired.
//
// Searches for both signals @-refs in the candidate files starting from the
// git repo toplevel (falls back to cwd if not in a repo). Both refs must appear
// in the same file. Severity: FAIL.
func checkRefs(_ Opts) Result {
	cwd, err := os.Getwd()
	if err != nil {
		return Result{Severity: FAIL, Detail: fmt.Sprintf("could not determine cwd: %v", err)}
	}
	searchRoot := gitToplevel(cwd)
	return RunCheckRefsWith(searchRoot)
}

// RunCheckRefsWith runs the refs check against an explicit repo root.
// Exported for testing; production callers use checkRefs.
func RunCheckRefsWith(repoRoot string) Result {
	// Track whether we saw partial refs across ANY file (for error detail).
	var partialFile string
	var partialMissing string

	for _, name := range candidateFiles {
		path := filepath.Join(repoRoot, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			// File absent or unreadable — skip.
			continue
		}
		content := string(raw)
		hasDet := strings.Contains(content, deterministicSignalsRef)
		hasInf := strings.Contains(content, signalsRef)

		if hasDet && hasInf {
			return Result{Severity: PASS, Detail: fmt.Sprintf("refs wired in %s", name)}
		}

		if hasDet || hasInf {
			// Partial match — record for detail but keep searching.
			if partialFile == "" {
				partialFile = name
				if hasDet {
					partialMissing = signalsRef
				} else {
					partialMissing = deterministicSignalsRef
				}
			}
		}
	}

	if partialFile != "" {
		return Result{
			Severity: FAIL,
			Detail:   fmt.Sprintf("partial refs in %s: missing %s", partialFile, partialMissing),
		}
	}

	return Result{
		Severity: FAIL,
		Detail:   "refs not present in CLAUDE.md, claude.local.md, CLAUDE.local.md, or claude.md",
	}
}
