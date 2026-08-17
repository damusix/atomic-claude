// Package repoinit scaffolds the .claude/ layout and its ignore rules.
// Idempotent and non-destructive: it only adds what is missing, never rewrites
// or reorders existing content, and never commits.
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

// Action is one guarantee's outcome: what it names and whether Init created it
// or found it already satisfied.
type Action struct {
	Name string
	Kind ActionKind
}

// managedHeader is written only when the nested .gitignore is created fresh;
// an existing file never gets it retroactively.
func managedHeader(harnessDirRel string) string {
	return fmt.Sprintf("# managed by atomic repo init; rules are relative to %s/\n", harnessDirRel)
}

// probeFile never needs to exist: git check-ignore evaluates ignore patterns
// against the pathname alone.
const probeFile = ".repoinit-probe"

// Init runs the layout guarantees against root, returning one Action each.
// Safe to call repeatedly. Errors only on irrecoverable I/O failure or when
// the repo config already declares a scope other than "repo".
//
// Passing "" as the root to config.ScratchpadDir et al. yields the
// harness-relative subpath alone, which is what the ignore rules need.
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

// ensureScopeMarker declares root's scope as "repo". A file already declaring
// a different scope is an error, never a rewrite.
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

func dirName(rel string) string {
	return filepath.ToSlash(rel) + "/"
}

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
	probeDirRel   string
	ignoreFileRel string
	ruleLine      string
	name          string
	header        string // written only when ignoreFileRel is created fresh
}

// ensureIgnored asks git whether the effect is already in place, degrading to a
// literal line scan, and appends the managed rule only when it is not.
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

// isIgnoredByGit reports determined=false when git cannot answer at all (no
// binary, or root is not a work tree), leaving the caller to degrade.
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
	// git not on PATH, or exit 128 ("not a git repository").
	return false, false
}

// ignoreFileHasLine is the degraded fallback for isIgnoredByGit: an exact-line
// match, so it cannot see rules that only match by pattern.
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

// appendRule preserves every existing byte, inserting a newline first when the
// file does not end in one.
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
