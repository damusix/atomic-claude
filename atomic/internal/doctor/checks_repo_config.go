package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// repoConfigRelDisplay returns the harness-aware relative path of the repo
// config, for display in this check's Detail strings (e.g. ".pi/atomic.toml"
// under a ".pi" harness dir rather than the default ".claude/atomic.toml").
func repoConfigRelDisplay(root string) string {
	abs := config.RepoConfigPath(root)
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

// checkRepoConfig implements category 13: repo-scoped config integrity
// (<projectRoot>/.claude/atomic.toml).
//
// The repo config is optional — its absence is normal (code-intel indexing
// proceeds unfiltered) and reports PASS informational, mirroring the
// code-index check's opt-in-absence contract. Parse errors, unknown keys,
// and invalid ignore glob patterns are all non-fatal by design at index time
// (indexing degrades to unfiltered with a CLI warning rather than failing),
// so they surface here as WARN, never FAIL.
func checkRepoConfig(opts Opts) Result {
	root := opts.RepoRoot
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Result{Severity: WARN, Detail: fmt.Sprintf("could not get cwd: %v", err)}
		}
		root = gitToplevelFn(cwd)
	}
	return RunCheckRepoConfigWith(root)
}

// RunCheckRepoConfigWith runs the repo-config check against an explicit
// project root. Exported for testing; production callers use checkRepoConfig.
func RunCheckRepoConfigWith(root string) Result {
	path := config.RepoConfigPath(root)
	display := repoConfigRelDisplay(root)

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Result{
				Severity: PASS,
				Detail:   fmt.Sprintf("%s not present (optional)", display),
			}
		}
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not stat %s: %v", display, err)}
	}

	cfg, warns, err := config.LoadRepoConfig(path)
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("%s: %v", display, err)}
	}

	matcher, matcherWarns := config.NewIgnoreMatcher(cfg.Code.Ignore)
	warns = append(warns, matcherWarns...)

	if len(warns) > 0 {
		msgs := make([]string, 0, len(warns))
		for _, w := range warns {
			msgs = append(msgs, w.Message)
		}
		return Result{Severity: WARN, Detail: strings.Join(msgs, "; ")}
	}

	return Result{
		Severity: PASS,
		Detail:   fmt.Sprintf("%s ok (%d ignore pattern(s))", display, matcher.PatternCount()),
	}
}
