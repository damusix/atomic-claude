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

// resolveScopeIdentity names the served tree, preferring the origin remote's
// owner/repo over the directory name — a directory name is a local accident,
// and worktrees, renamed clones, and "src" checkouts all misreport under it.
// Every step falls back to the next, ending at the served directory's name;
// a failure here is not worth surfacing in a header chip.
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

// gitRemoteName returns origin as "owner/repo", handling both remote forms:
//
//	git@github.com:owner/repo.git  → owner/repo
//	https://github.com/owner/repo  → owner/repo
func gitRemoteName(root string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	url := strings.TrimSpace(string(out))
	url = strings.TrimSuffix(url, ".git")

	// SCP-style has no scheme to strip, so split on the colon instead.
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
	// Last two only, so a nested GitLab group path collapses to group/repo.
	owner, repo := segments[len(segments)-2], segments[len(segments)-1]
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}

// gitBranch uses symbolic-ref rather than rev-parse --abbrev-ref: rev-parse
// fails on an unborn branch, leaving a freshly initialised repo with no branch
// shown. symbolic-ref reads HEAD directly, so it answers before the first
// commit and still fails, correctly, on a detached HEAD.
func gitBranch(root string) string {
	cmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
