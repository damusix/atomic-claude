// Package repoinit scaffolds the deterministic .claude/ layout a repo needs
// to work with the atomic ecosystem: the scratchpad and project directories,
// and the git-ignore rules that keep them (and tmp/, .claude/worktrees/) out of
// version control. Init is idempotent and non-destructive — it only creates
// what is missing and never rewrites, reorders, or removes existing content.
// It never runs git commit; the caller owns that.
package repoinit

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// ActionKind classifies what Init did for one guarantee.
type ActionKind string

const (
	ActionCreated ActionKind = "created"
	ActionOK      ActionKind = "ok"
)

// Action describes the outcome of one guarantee: what it names (a directory
// path or an ignore-rule description) and whether Init created it or found
// it already satisfied.
type Action struct {
	Name string
	Kind ActionKind
}

// managedHeader precedes the managed rules when the nested <harness.dir>/.gitignore
// is created fresh (it does not exist yet). An existing file is never given
// this header retroactively. harnessDirRel is the harness dir's slash-form
// name (e.g. ".claude" or ".pi").
func managedHeader(harnessDirRel string) string {
	return fmt.Sprintf("# managed by atomic repo init; rules are relative to %s/\n", harnessDirRel)
}

// probeFile is the nonexistent filename checked under each guarded directory
// to answer "is it ignored" via git check-ignore. It never needs to exist —
// git evaluates ignore patterns against the pathname alone.
const probeFile = ".repoinit-probe"

// Init runs the seven layout guarantees against root, in order, and returns
// one Action per guarantee. It is safe to call repeatedly: a guarantee
// already satisfied reports ActionOK and touches nothing. Returns an error
// only on irrecoverable I/O failure (e.g. an unwritable directory) or when
// the repo config already declares a scope other than "repo" — a
// conflicting marker is never rewritten (see config.EnsureScopeMarker).
//
// Every guarantee but the root tmp/ rule is nested under the resolved
// harness.dir (default ".claude"; see config.ScratchpadDir et al.) — passing
// "" as the root to those helpers yields the harness-relative subpath alone
// (e.g. ".claude/.scratchpad" or ".pi/.scratchpad"), reusing the same
// resolver repo-local consumers thread through elsewhere.
func Init(root string) ([]Action, error) {
	actions := make([]Action, 0, 7)

	scratchpadRel := config.ScratchpadDir("")
	projectRel := config.ProjectDir("")
	indexRel := config.IndexDir("")
	worktreesRel := config.WorktreesDir("")
	harnessDirRel := filepath.Dir(scratchpadRel)
	nestedGitignoreRel := filepath.Join(harnessDirRel, ".gitignore")
	header := managedHeader(filepath.ToSlash(harnessDirRel))

	a, err := ensureDir(root, scratchpadRel, dirName(scratchpadRel))
	if err != nil {
		return nil, err
	}
	actions = append(actions, a)

	a, err = ensureDir(root, projectRel, dirName(projectRel))
	if err != nil {
		return nil, err
	}
	actions = append(actions, a)

	a, err = ensureIgnored(root, ignoreGuarantee{
		probeDirRel:   scratchpadRel,
		ignoreFileRel: nestedGitignoreRel,
		ruleLine:      "/.scratchpad/",
		name:          dirName(scratchpadRel) + " ignored",
		header:        header,
	})
	if err != nil {
		return nil, err
	}
	actions = append(actions, a)

	a, err = ensureIgnored(root, ignoreGuarantee{
		probeDirRel:   indexRel,
		ignoreFileRel: nestedGitignoreRel,
		ruleLine:      "/.atomic-index/",
		name:          dirName(indexRel) + " ignored",
		header:        header,
	})
	if err != nil {
		return nil, err
	}
	actions = append(actions, a)

	a, err = ensureIgnored(root, ignoreGuarantee{
		probeDirRel:   "tmp",
		ignoreFileRel: ".gitignore",
		ruleLine:      "tmp/",
		name:          "tmp/ ignored",
	})
	if err != nil {
		return nil, err
	}
	actions = append(actions, a)

	a, err = ensureIgnored(root, ignoreGuarantee{
		probeDirRel:   worktreesRel,
		ignoreFileRel: nestedGitignoreRel,
		ruleLine:      "/worktrees/",
		name:          dirName(worktreesRel) + " ignored",
		header:        header,
	})
	if err != nil {
		return nil, err
	}
	actions = append(actions, a)

	a, err = ensureScopeMarker(root)
	if err != nil {
		return nil, err
	}
	actions = append(actions, a)

	return actions, nil
}

// ensureScopeMarker declares root's scope as "repo" via
// config.EnsureScopeMarker, reporting the outcome as an Action in the same
// shape (created / ok) the rest of Init uses — a created or added key both
// report ActionCreated, mirroring ensureIgnored's "wrote something" ==
// created convention. A conflicting marker (the file already declares a
// different scope) is never rewritten and is surfaced as an error.
func ensureScopeMarker(root string) (Action, error) {
	name := filepath.ToSlash(config.RepoConfigPath("")) + ` scope="repo"`

	outcome, err := config.EnsureScopeMarker(root, "repo")
	if err != nil {
		return Action{}, fmt.Errorf("repoinit: scope marker: %w", err)
	}
	if outcome == config.ScopeMarkerConflict {
		return Action{}, fmt.Errorf("repoinit: %s already declares a different scope — refusing to overwrite it", filepath.ToSlash(config.RepoConfigPath("")))
	}

	kind := ActionOK
	if outcome == config.ScopeMarkerCreated || outcome == config.ScopeMarkerAdded {
		kind = ActionCreated
	}
	return Action{Name: name, Kind: kind}, nil
}

// dirName renders a root-relative path as the slash-form display name used
// in Action.Name (e.g. ".claude/.scratchpad" → ".claude/.scratchpad/").
func dirName(rel string) string {
	return filepath.ToSlash(rel) + "/"
}

// ensureDir guarantees dirRel exists under root, reporting name as created or
// already-ok.
func ensureDir(root, dirRel, name string) (Action, error) {
	full := filepath.Join(root, dirRel)
	if info, err := os.Stat(full); err == nil && info.IsDir() {
		return Action{Name: name, Kind: ActionOK}, nil
	}
	if err := os.MkdirAll(full, 0755); err != nil {
		return Action{}, fmt.Errorf("repoinit: create %s: %w", dirRel, err)
	}
	return Action{Name: name, Kind: ActionCreated}, nil
}

// ignoreGuarantee describes one "is X ignored, else append a rule" guarantee.
type ignoreGuarantee struct {
	probeDirRel   string // directory whose ignore-effect is being checked
	ignoreFileRel string // ignore file the rule is appended to when missing
	ruleLine      string // the managed rule line
	name          string // Action.Name reported to the caller
	header        string // precedes the rule when creating ignoreFileRel fresh; empty means no header
}

// ensureIgnored guarantees g.ruleLine's effect is already present (via
// git check-ignore against a nonexistent probe path, falling back to a
// literal line scan when git is unavailable or root is not a work tree) —
// else appends the managed rule to g.ignoreFileRel.
func ensureIgnored(root string, g ignoreGuarantee) (Action, error) {
	probe := filepath.Join(g.probeDirRel, probeFile)
	ignoreFile := filepath.Join(root, g.ignoreFileRel)

	ignored, determined := isIgnoredByGit(root, probe)
	if !determined {
		ignored = ignoreFileHasLine(ignoreFile, g.ruleLine)
	}
	if ignored {
		return Action{Name: g.name, Kind: ActionOK}, nil
	}

	if err := appendRule(ignoreFile, g.ruleLine, g.header); err != nil {
		return Action{}, fmt.Errorf("repoinit: append %q to %s: %w", g.ruleLine, g.ignoreFileRel, err)
	}
	return Action{Name: g.name, Kind: ActionCreated}, nil
}

// isIgnoredByGit answers "is probe (relative to root) ignored" by running
// git check-ignore. determined is false when the answer could not be
// established deterministically (git binary absent, or root is not a work
// tree) — the caller degrades to a literal line scan in that case.
func isIgnoredByGit(root, probe string) (ignored bool, determined bool) {
	cmd := exec.Command("git", "check-ignore", "-q", probe)
	cmd.Dir = root
	err := cmd.Run()
	if err == nil {
		return true, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, true
	}
	// exec.Error (git not on PATH) or any other exit code (e.g. 128, "not a
	// git repository") — the deterministic path is unavailable.
	return false, false
}

// ignoreFileHasLine reports whether ruleLine appears as its own line in the
// file at path (trimmed of surrounding whitespace). A missing file reports
// false. Used only as the degraded fallback when git is unavailable.
func ignoreFileHasLine(path, ruleLine string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	want := strings.TrimSpace(ruleLine)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// appendRule appends ruleLine to the file at path, preserving every existing
// byte. A missing file is created; a non-empty header precedes the rule only
// in that fresh-file case. An existing file that does not end in a newline
// gets one inserted before the appended rule; its content is otherwise
// untouched.
func appendRule(path, ruleLine, header string) error {
	existing, err := os.ReadFile(path)
	fileExists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var buf strings.Builder
	switch {
	case !fileExists:
		if header != "" {
			buf.WriteString(header)
		}
	case len(existing) > 0:
		buf.Write(existing)
		if existing[len(existing)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	buf.WriteString(ruleLine)
	buf.WriteByte('\n')

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(buf.String()), 0644)
}
