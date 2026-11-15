package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
)

// candidateFiles is the Claude steering @-ref search order; the first file
// carrying the ref wins.
var candidateFiles = []string{
	"claude.local.md",
	"CLAUDE.local.md",
	"CLAUDE.md",
	"claude.md",
}

// repoRefFiles is the repository order the wiki pipeline wires the router ref
// by: the shared AGENTS.md sits ahead of the CLAUDE.md loader that imports it.
var repoRefFiles = []string{
	"claude.local.md",
	"CLAUDE.local.md",
	"AGENTS.md",
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
	if name, ok := wiredRefFile(repoRoot, signalsRef); ok {
		return Result{Severity: PASS, Detail: fmt.Sprintf("ref wired in %s", name)}
	}
	return Result{
		Severity: FAIL,
		Detail:   "ref not present in claude.local.md, CLAUDE.local.md, AGENTS.md (when CLAUDE.md is absent or imports it), CLAUDE.md, or claude.md",
	}
}

// wiredRefFile returns the first repoRefFiles entry under root that carries ref
// and that Claude loads; see bundlespec.SteeringSource.LoaderWithoutImport.
func wiredRefFile(root, ref string) (string, bool) {
	for _, name := range repoRefFiles {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !strings.Contains(string(raw), ref) {
			continue
		}
		if name == "AGENTS.md" && bundlespec.ScopeSteering.LoaderWithoutImport(root) != "" {
			continue
		}
		return name, true
	}
	return "", false
}
