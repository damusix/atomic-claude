// Package repoinit scaffolds the state-root layout and its ignore rules.
// Idempotent and non-destructive: it only adds what is missing, never rewrites
// or reorders existing content, and never commits.
package repoinit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/docs"
)

// ActionKind classifies what Init did for one guarantee.
type ActionKind string

const (
	ActionCreated ActionKind = "created"
	ActionOK      ActionKind = "ok"
	// ActionNegated: the rule is present, but a later rule un-ignores the path.
	ActionNegated ActionKind = "negated"
	// ActionTracked: the rule covers the path, but Git already tracks the file.
	ActionTracked ActionKind = "tracked"
)

// Action is one guarantee's outcome: what it names, whether Init created it or
// found it satisfied, and for a guarantee it cannot satisfy, what to do.
type Action struct {
	Name   string
	Kind   ActionKind
	Detail string
}

// managedHeader is written only when the nested .gitignore is created fresh;
// an existing file never gets it retroactively.
func managedHeader(harnessDirRel string) string {
	return fmt.Sprintf("# managed by atomic repo init; rules are relative to %s/\n", harnessDirRel)
}

// probeFile never needs to exist: git check-ignore evaluates ignore patterns
// against the pathname alone.
const probeFile = ".repoinit-probe"

// Init runs the layout guarantees against the repository root, one Action each.
// A state root outside it gets no ignore rule; claudeEnrolled adds Claude's.
func Init(root string, claudeEnrolled bool) ([]Action, error) {
	state, err := config.RepositoryStateDir(root)
	if err != nil {
		return nil, err
	}
	stateRel, inside := config.RepoRelative(root, state)

	scratchpad := config.ScratchpadDir(root)
	var actions []Action
	for _, dir := range []string{scratchpad, config.ProjectDir(root)} {
		a, err := ensureDir(dir, dirName(label(root, dir)))
		if err != nil {
			return nil, err
		}
		actions = append(actions, a)
	}

	var guarantees []ignoreGuarantee
	stateRule := func(dir, rule string) ignoreGuarantee {
		rel := label(root, dir)
		return ignoreGuarantee{
			probeRel:      filepath.Join(rel, probeFile),
			ignoreFileRel: filepath.Join(stateRel, ".gitignore"),
			ruleLine:      rule,
			name:          dirName(rel) + " ignored",
			header:        managedHeader(filepath.ToSlash(stateRel)),
		}
	}
	if inside {
		docSurfaces := label(root, docs.CachePath(root))
		guarantees = append(guarantees,
			stateRule(scratchpad, "/.scratchpad/"),
			stateRule(config.IndexDir(root), "/.atomic-index/"),
			ignoreGuarantee{
				probeRel:      docSurfaces,
				ignoreFileRel: filepath.Join(stateRel, ".gitignore"),
				ruleLine:      config.DocSurfacesIgnoreRule,
				name:          filepath.ToSlash(docSurfaces) + " ignored",
				header:        managedHeader(filepath.ToSlash(stateRel)),
				trackable:     true,
			})
	}
	guarantees = append(guarantees, ignoreGuarantee{
		probeRel:      filepath.Join("tmp", probeFile),
		ignoreFileRel: ".gitignore",
		ruleLine:      "tmp/",
		name:          "tmp/ ignored",
	})
	if inside {
		guarantees = append(guarantees,
			stateRule(filepath.Join(state, "worktrees"), "/worktrees/"))
	}
	const claude = ".claude"
	if claudeDir := filepath.Join(root, claude); state != claudeDir && (claudeEnrolled || isDir(claudeDir)) {
		guarantees = append(guarantees, ignoreGuarantee{
			probeRel:      filepath.Join(claude, "worktrees", probeFile),
			ignoreFileRel: filepath.Join(claude, ".gitignore"),
			ruleLine:      "/worktrees/",
			name:          claude + "/worktrees/ ignored",
			header:        managedHeader(claude),
		})
	}
	for _, g := range guarantees {
		a, err := ensureIgnored(root, g)
		if err != nil {
			return nil, err
		}
		actions = append(actions, a)
	}

	a, err := ensureScopeMarker(root)
	if err != nil {
		return nil, err
	}
	return append(actions, a), nil
}

// label is the repository-relative spelling of abs, or abs itself outside root.
func label(root, abs string) string {
	if rel, ok := config.RepoRelative(root, abs); ok {
		return rel
	}
	return abs
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ensureScopeMarker declares root's scope as "repo". A file already declaring
// a different scope is an error, never a rewrite.
func ensureScopeMarker(root string) (Action, error) {
	path := filepath.ToSlash(label(root, config.RepoConfigPath(root)))
	name := path + ` scope="repo"`

	outcome, err := config.EnsureScopeMarker(root, "repo")
	if err != nil {
		return Action{}, fmt.Errorf("repoinit: scope marker: %w", err)
	}
	if outcome == config.ScopeMarkerConflict {
		return Action{}, fmt.Errorf("repoinit: %s already declares a different scope — refusing to overwrite it", path)
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

func ensureDir(full, name string) (Action, error) {
	if isDir(full) {
		return Action{Name: name, Kind: ActionOK}, nil
	}
	if err := os.MkdirAll(full, 0755); err != nil {
		return Action{}, fmt.Errorf("repoinit: create %s: %w", full, err)
	}
	return Action{Name: name, Kind: ActionCreated}, nil
}

// ignoreGuarantee describes one "is X ignored, else append a rule" guarantee.
type ignoreGuarantee struct {
	probeRel      string
	ignoreFileRel string
	ruleLine      string
	name          string
	header        string // written only when ignoreFileRel is created fresh
	trackable     bool   // probeRel is a real file Git may already track
}

// ensureIgnored asks git whether the effect is already in place, degrading to a
// literal line scan, and appends the managed rule only when it is not.
func ensureIgnored(root string, g ignoreGuarantee) (Action, error) {
	ignoreFile := filepath.Join(root, g.ignoreFileRel)

	ignored, determined := config.GitCheckIgnore(root, g.probeRel)
	if !determined {
		ignored = ignoreFileHasLine(ignoreFile, g.ruleLine)
	}
	kind := ActionOK
	if !ignored {
		wrote, err := config.AppendIgnoreRuleWithHeader(ignoreFile, g.ruleLine, g.header)
		if err != nil {
			return Action{}, fmt.Errorf("repoinit: append %q to %s: %w", g.ruleLine, g.ignoreFileRel, err)
		}
		if !wrote {
			return Action{Name: g.name, Kind: ActionNegated,
				Detail: fmt.Sprintf("%s already holds %s, but a later rule negates it", filepath.ToSlash(g.ignoreFileRel), g.ruleLine)}, nil
		}
		kind = ActionCreated
	}
	if g.trackable && isTrackedByGit(root, g.probeRel) {
		path := filepath.ToSlash(g.probeRel)
		return Action{Name: g.name, Kind: ActionTracked,
			Detail: fmt.Sprintf("git still tracks %s; run `git rm --cached %s`", path, path)}, nil
	}
	return Action{Name: g.name, Kind: kind}, nil
}

// isTrackedByGit reports whether rel is in the index; any git failure reads as
// untracked.
func isTrackedByGit(root, rel string) bool {
	cmd := exec.Command("git", "ls-files", "--", rel)
	cmd.Dir = root
	out, err := cmd.Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

// ignoreFileHasLine is the degraded fallback for config.GitCheckIgnore: an exact-line
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
