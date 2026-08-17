package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// repoConfigRelDisplay returns the harness-aware relative path of the repo
// config for display in Detail strings (".pi/atomic.toml" under a ".pi"
// harness dir, rather than the default ".claude/atomic.toml").
func repoConfigRelDisplay(root string) string {
	abs := config.RepoConfigPath(root)
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

// checkRepoConfig implements category 13: repo-scoped config integrity. The
// config is optional, so absence is an informational PASS; every defect
// (parse error, unknown key, bad glob, bad scope, bad idle_timeout) is
// non-fatal at index time and so WARNs here, never FAILs. On top of
// RunCheckRepoConfigWith it also WARNs when the marker claims scope="repo"
// while <wikis> registers the same root as a realm. Empty ClaudeMDPath skips
// that sub-check.
func checkRepoConfig(opts Opts) Result {
	root := opts.RepoRoot
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Result{Severity: WARN, Detail: fmt.Sprintf("could not get cwd: %v", err)}
		}
		root = gitToplevelFn(cwd)
	}

	result := RunCheckRepoConfigWith(root)

	if opts.ClaudeMDPath == "" {
		return result
	}

	if contradiction := scopeWikisContradiction(root, opts.ClaudeMDPath); contradiction != "" {
		result.Severity = WARN
		if result.Detail == "" {
			result.Detail = contradiction
		} else {
			result.Detail = result.Detail + "; " + contradiction
		}
	}

	return result
}

// RunCheckRepoConfig is the dispatcher entry point for the repo-config check.
func RunCheckRepoConfig(opts Opts) Result {
	return checkRepoConfig(opts)
}

// scopeWikisContradiction returns a WARN detail when root declares
// scope = "repo" while <wikis> in claudeMDPath registers root as a realm
// root, and "" in every other case.
func scopeWikisContradiction(root, claudeMDPath string) string {
	cfg, _, err := config.LoadRepoConfig(config.RepoConfigPath(root))
	if err != nil || cfg == nil || cfg.Scope != "repo" {
		return ""
	}

	indexPaths, err := wiki.ReadWikiIndexPaths(claudeMDPath)
	if err != nil || len(indexPaths) == 0 {
		return ""
	}

	wantRoot := normalizeScopeDir(root)
	for _, indexPath := range indexPaths {
		// A registered path is <realmRoot>/wiki/index.md.
		realmRoot := filepath.Dir(filepath.Dir(indexPath))
		if normalizeScopeDir(realmRoot) == wantRoot {
			return fmt.Sprintf(
				"scope=repo conflicts with the <wikis> registry: this root is registered as a realm root in %s",
				claudeMDPath,
			)
		}
	}
	return ""
}

// normalizeScopeDir canonicalizes dir for comparison. Symlink resolution
// matters because the two sides arrive differently: gitToplevelFn resolves
// symlinks (macOS /tmp reports /private/tmp/...) while <wikis> entries are
// stored exactly as written, so the symlinked form would never compare equal.
// A dir that no longer exists degrades to the cleaned path rather than
// breaking the run.
func normalizeScopeDir(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	dir = filepath.Clean(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

// RunCheckRepoConfigWith runs the repo-config check against an explicit
// project root. Exported for testing. Root-only: it skips the
// <wikis>-contradiction sub-check, which needs a CLAUDE.md path.
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

	if cfg.Scope != "" && !config.ValidScope(cfg.Scope) {
		warns = append(warns, config.Warning{
			Message: fmt.Sprintf("scope %q is not valid (accepted values: repo, realm)", cfg.Scope),
		})
	}

	if cfg.Repl.IdleTimeout != "" {
		if _, err := config.ValidateIdleTimeout(cfg.Repl.IdleTimeout); err != nil {
			warns = append(warns, config.Warning{Message: err.Error()})
		}
	}

	if len(warns) > 0 {
		msgs := make([]string, 0, len(warns))
		for _, w := range warns {
			msgs = append(msgs, w.Message)
		}
		return Result{Severity: WARN, Detail: strings.Join(msgs, "; ")}
	}

	detail := fmt.Sprintf("%s ok (%d ignore pattern(s))", display, matcher.PatternCount())
	if cfg.Scope != "" {
		detail = fmt.Sprintf("%s ok (scope=%s, %d ignore pattern(s))", display, cfg.Scope, matcher.PatternCount())
	}

	return Result{
		Severity: PASS,
		Detail:   detail,
	}
}
