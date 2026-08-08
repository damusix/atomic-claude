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
// invalid ignore glob patterns, an invalid scope value, and an invalid
// [repl] idle_timeout are all non-fatal by design at index/discovery time,
// so they surface here as WARN, never FAIL.
//
// Beyond RunCheckRepoConfigWith's root-only validation, this dispatcher also
// WARNs when root's marker declares scope = "repo" while root is registered
// as a realm root in opts.ClaudeMDPath's <wikis> block — two mechanisms
// making incompatible claims about one directory (scope-marker design
// decision 2). An empty opts.ClaudeMDPath skips that sub-check.
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

// RunCheckRepoConfig is the exported entry point for the dispatcher. It
// delegates to checkRepoConfig so tests can exercise the <wikis>-contradiction
// sub-check (which needs opts.ClaudeMDPath) without package-internal access.
func RunCheckRepoConfig(opts Opts) Result {
	return checkRepoConfig(opts)
}

// scopeWikisContradiction reports a non-empty WARN detail when root's repo
// config declares scope = "repo" while root is also registered as a realm
// root in claudeMDPath's <wikis> block. Returns "" — no contradiction — when
// the marker is absent, invalid, declares "realm" instead, the <wikis> block
// is unreadable or empty, or no registered realm root matches root.
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
		// A registered index path is <realmRoot>/wiki/index.md — the realm
		// root is its grandparent.
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

// normalizeScopeDir canonicalizes dir for comparison: absolutize, clean, then
// resolve symlinks. Symlink resolution is required here because the two sides
// of the comparison arrive through different mechanisms — opts.RepoRoot comes
// from gitToplevelFn ("git rev-parse --show-toplevel"), which resolves
// symlinks (a repo under macOS /tmp reports /private/tmp/...), while
// wiki.ReadWikiIndexPaths returns each <wikis> entry exactly as written,
// unresolved. Without this, a realm registered via the symlinked form never
// compares equal to the resolved root and the contradiction WARN never fires.
// Degrades to the unresolved cleaned path when EvalSymlinks errors — a
// registered realm root that no longer exists on disk must not break this
// check or the doctor run.
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
// project root. Exported for testing; production callers use checkRepoConfig.
// Root-only — it does not run the <wikis>-contradiction sub-check, which
// needs a CLAUDE.md path (see checkRepoConfig).
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
