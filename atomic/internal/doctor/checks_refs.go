package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// candidateFiles is the search order for @-refs per the atomic-signals skill.
// Checked in order; first file containing the ref wins.
var candidateFiles = []string{
	"claude.local.md",
	"CLAUDE.local.md",
	"CLAUDE.md",
	"claude.md",
}

const signalsRef = "@.claude/project/signals.md"

// checkRefs implements category 4: @-refs wired.
//
// Searches for the signals.md @-ref in candidate files starting from the
// git repo toplevel (falls back to cwd if not in a repo). Only signals.md
// needs to be @-ref'd — deterministic-signals.md is too large for context
// and is read on demand by the inferrer. Severity: FAIL.
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
	for _, name := range candidateFiles {
		path := filepath.Join(repoRoot, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(raw), signalsRef) {
			return Result{Severity: PASS, Detail: fmt.Sprintf("ref wired in %s", name)}
		}
	}

	return Result{
		Severity: FAIL,
		Detail:   "ref not present in CLAUDE.md, claude.local.md, CLAUDE.local.md, or claude.md",
	}
}
