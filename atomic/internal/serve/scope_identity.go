package serve

import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
)

// scopeIdentity is what the shell header labels the served tree with.
type scopeIdentity struct {
	// Name is the directory name of the resolved scope root.
	Name string
	// Branch is the checked-out git branch, empty when the root is not a work
	// tree or HEAD is detached.
	Branch string
}

// resolveScopeIdentity names the served tree. The scope root is resolved the
// way the rest of atomic resolves it — the directory holding the nearest
// scope="repo" marker in .claude/atomic.toml, else the git work-tree root,
// else the served directory (repoctx.ResolveFrom owns that precedence).
//
// The NAME then prefers the origin remote's owner/repo over that directory's
// own name, because a directory name is a local accident: worktrees, clones
// into a renamed folder, and "src" checkouts all misreport the project.
// owner/repo is what the project is actually called everywhere else.
//
// Resolution failure is not an error worth surfacing in a header chip — each
// step falls back to the one below it, ending at the served directory's name.
func resolveScopeIdentity(servedRoot string) scopeIdentity {
	root := servedRoot
	if resolved, _, err := repoctx.ResolveFrom(servedRoot, ""); err == nil && resolved != "" {
		root = resolved
	}

	name := gitRemoteName(root)
	if name == "" {
		name = filepath.Base(root)
	}

	return scopeIdentity{
		Name:   name,
		Branch: gitBranch(root),
	}
}

// gitRemoteName returns the origin remote as "owner/repo", or "" when there
// is no origin or it does not parse to that shape.
//
// Handles both remote forms git itself accepts:
//
//	git@github.com:damusix/atomic-claude.git  → damusix/atomic-claude
//	https://github.com/damusix/atomic-claude  → damusix/atomic-claude
func gitRemoteName(root string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	url := strings.TrimSpace(string(out))
	url = strings.TrimSuffix(url, ".git")

	// SCP-style (git@host:owner/repo) has no scheme to strip — split on the
	// colon instead. Everything else is a URL whose host segment goes away
	// with the rest of the path prefix below.
	if at := strings.LastIndex(url, ":"); at != -1 && !strings.Contains(url, "://") {
		url = url[at+1:]
	} else if i := strings.Index(url, "://"); i != -1 {
		rest := url[i+3:]
		if slash := strings.Index(rest, "/"); slash != -1 {
			url = rest[slash+1:]
		} else {
			return ""
		}
	}

	url = strings.Trim(url, "/")
	segments := strings.Split(url, "/")
	if len(segments) < 2 {
		return ""
	}
	// Keep the last two: nested group paths (GitLab) collapse to group/repo.
	owner, repo := segments[len(segments)-2], segments[len(segments)-1]
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}

// gitBranch returns the checked-out branch name at root, or "" when root is
// not a git work tree or HEAD is detached.
//
// symbolic-ref rather than "rev-parse --abbrev-ref": rev-parse fails outright
// on an unborn branch (a repo with no commits yet), which would leave a
// freshly initialised project showing no branch at all. symbolic-ref reads
// HEAD's target directly, so it answers before the first commit and still
// fails — correctly — on a detached HEAD, which is not a branch.
func gitBranch(root string) string {
	cmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
