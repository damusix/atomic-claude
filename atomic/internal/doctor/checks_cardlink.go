package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

const (
	cardLinkPath      = ".claude/rules/atomic"
	relinkRemediation = "remove the link and run `atomic repo init`"
)

// checkCardLink is category 25: whether Claude reads the wiki cards through
// the .claude/rules/atomic symlink, and the session limit that comes with it.
func checkCardLink(opts Opts) Result {
	root := opts.RepoRoot
	if root == "" {
		if cwd, err := os.Getwd(); err == nil {
			root = gitToplevelFn(cwd)
		}
	}
	return RunCheckCardLinkWith(root)
}

// RunCheckCardLinkWith reports the card link under root. Claude asks before
// following it from a subdirectory: https://code.claude.com/docs/en/memory
func RunCheckCardLinkWith(root string) Result {
	if root == "" {
		return Result{Severity: PASS, Detail: "no repository"}
	}
	link := filepath.Join(root, filepath.FromSlash(cardLinkPath))
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return Result{Severity: PASS, Detail: "no card link"}
	}
	target, err := os.Readlink(link)
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("read %s: %v", link, err), Remediation: relinkRemediation}
	}
	stateRoot, err := config.RepositoryStateDir(root)
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve the state root of %s: %v", root, err),
			Remediation: "fix the repository state root, then rerun `atomic doctor`"}
	}
	want := filepath.Join(stateRoot, "rules")
	got, gotErr := filepath.EvalSymlinks(link)
	resolved, wantErr := filepath.EvalSymlinks(want)
	if gotErr != nil || wantErr != nil || got != resolved || !dirExists(got) {
		return Result{
			Severity:    WARN,
			Detail:      fmt.Sprintf("%s points to %s, not the existing %s, so it serves no wiki card", link, target, want),
			Remediation: relinkRemediation,
		}
	}
	if wantTarget, err := filepath.Rel(filepath.Dir(link), want); err == nil && target != wantTarget {
		return Result{
			Severity:    WARN,
			Detail:      fmt.Sprintf("%s points to %s, not the relative %s that `atomic repo init` writes, so init reports it as a conflict", link, target, wantTarget),
			Remediation: relinkRemediation,
		}
	}
	return Result{
		Severity: PASS,
		Detail: cardLinkPath + " serves the wiki cards; a Claude session started in a " +
			"subdirectory loads them only after the user approves the link",
	}
}
