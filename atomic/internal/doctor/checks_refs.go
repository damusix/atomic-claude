package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// candidateFiles is the @-ref search order; the first file carrying the ref wins.
var candidateFiles = []string{
	"claude.local.md",
	"CLAUDE.local.md",
	"CLAUDE.md",
	"claude.md",
}

const signalsRef = "@docs/wiki/index.md"

// checkRefs implements category 4: @-refs wired. An unwired ref FAILs. Only
// docs/wiki/index.md is checked — scan.md is too large for context and the
// inferrer reads it on demand.
func checkRefs(opts Opts) Result {
	searchRoot := opts.RepoRoot
	if searchRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Result{Severity: FAIL, Detail: fmt.Sprintf("could not determine cwd: %v", err)}
		}
		searchRoot = gitToplevelFn(cwd)
	}
	return RunCheckRefsWith(searchRoot)
}

// RunCheckRefsWith runs the refs check against an explicit repo root.
// Exported for testing.
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
