package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// Level names where a resolved value came from.
type Level string

const (
	LevelRepo    Level = "repo"
	LevelRealm   Level = "realm"
	LevelProfile Level = "profile"
	LevelOrigin  Level = "origin"
	LevelDerived Level = "derived"
	LevelDefault Level = "default"
)

// Value is one resolved key with the level that supplied it.
type Value struct {
	Key   Key    `json:"key"`
	Value string `json:"value"`
	Level Level  `json:"level"`
}

// Resolved is the outcome of Resolve. Unset keys are absent from Values.
type Resolved struct {
	Values   []Value
	Warnings []string
	Read     []string
}

// Options selects what Resolve reads. Home defaults to the user's home and
// Harness is a harness kind name, "" when unknown.
type Options struct {
	Dir     string
	Home    string
	Harness string
}

type levelBlock struct {
	level Level
	block *Block
}

// Resolve applies the precedence rule to the levels visible from Dir. It fails
// only when Dir is unreadable; every other problem is a warning.
func Resolve(o Options) (Resolved, error) {
	dir, err := filepath.Abs(o.Dir)
	if err != nil {
		return Resolved{}, fmt.Errorf("workflow: %w", err)
	}
	if _, err := os.Stat(dir); err != nil {
		return Resolved{}, fmt.Errorf("workflow: %w", err)
	}
	home := o.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}

	root, repoPath, warnings := findRepo(dir)
	realmPath, ws := findRealmPath(dir, home)
	warnings = append(warnings, ws...)
	levels, ws := loadLevels(repoPath, realmPath, home)
	warnings = append(warnings, ws...)

	origin := Origin{}
	if root != "" {
		origin = ProviderForOrigin(OriginURL(root))
	}
	worktreeRoot := ""
	if repoPath != "" {
		worktreeRoot = root
	}
	return apply(levels, origin, worktreeRoot, o.Harness, warnings), nil
}

// Worktrees is the worktrees value Resolve reports for the repository at root,
// found without spawning Git; dir locates the realm level.
func Worktrees(root, dir, home, harness string) string {
	repoPath, err := config.WorkflowPrefsPath(root)
	if err != nil {
		repoPath, root = "", ""
	}
	realmPath, _ := findRealmPath(dir, home)
	levels, _ := loadLevels(repoPath, realmPath, home)
	return worktreesValue(levels, root, harness).Value
}

func loadLevels(repoPath, realmPath, home string) ([]levelBlock, []string) {
	var warnings []string
	var levels []levelBlock
	add := func(path string, profile bool, allowed ...Level) {
		b, level, ws := loadLevel(path, profile, allowed...)
		warnings = append(warnings, ws...)
		if b != nil {
			levels = append(levels, levelBlock{level, b})
		}
	}
	switch {
	case repoPath != "" && repoPath == realmPath:
		add(repoPath, false, LevelRepo, LevelRealm)
	default:
		if repoPath != "" {
			add(repoPath, false, LevelRepo)
		}
		if realmPath != "" {
			add(realmPath, false, LevelRealm)
		}
	}
	if home != "" {
		add(config.ProfilePath(home), true, LevelProfile)
	}
	return levels, warnings
}

// findRepo keeps the Git root when only the repo level is skipped, so origin
// inference still runs.
func findRepo(dir string) (root, path string, warnings []string) {
	root, path, err := repoPrefsPath(dir)
	switch {
	case errors.Is(err, ErrNoLocation):
		return "", "", []string{"outside a repository: repo level skipped"}
	case err != nil:
		return root, "", []string{fmt.Sprintf("repo level skipped: %v", err)}
	}
	return root, path, nil
}

func findRealmPath(dir, home string) (string, []string) {
	path, err := realmPrefsPath(dir, home)
	switch {
	case errors.Is(err, ErrNoLocation):
		return "", nil
	case err != nil:
		return "", []string{fmt.Sprintf("realm level skipped: %v", err)}
	}
	return path, nil
}

// repoPrefsPath returns the repository root along with the file path; the root
// survives a refused state root so origin inference still runs.
func repoPrefsPath(dir string) (root, path string, err error) {
	root, source, err := repoctx.ResolveRootFrom(dir, "")
	if err != nil {
		return "", "", err
	}
	if source == config.ScopeSourceCwd {
		return "", "", fmt.Errorf("%w: %s is outside a repository", ErrNoLocation, dir)
	}
	path, err = config.WorkflowPrefsPath(root)
	return root, path, err
}

func realmPrefsPath(dir, home string) (string, error) {
	claudeMD := ""
	if home != "" {
		claudeMD = filepath.Join(home, ".claude", "CLAUDE.md")
	}
	loc, found, err := wiki.FindRealm(dir, claudeMD)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("%w: %s is outside a registered realm", ErrNoLocation, dir)
	}
	return config.WorkflowPrefsPath(loc.Root)
}

// loadLevel parses the block at path (the profile section when profile). The
// block's level attribute picks among allowed; else allowed[0] with a warning.
func loadLevel(path string, profile bool, allowed ...Level) (*Block, Level, []string) {
	level := allowed[0]
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, level, nil
		}
		return nil, level, []string{fmt.Sprintf("%s level skipped: %v", level, err)}
	}
	text := string(raw)
	if profile {
		text = ProfileSection(text)
	}
	b, err := ParseBlock(text)
	if err != nil {
		return nil, level, []string{fmt.Sprintf("%s level skipped: %s: %v", level, path, err)}
	}
	if b == nil {
		return nil, level, nil
	}
	var ws []string
	switch {
	case b.Level == "":
		ws = append(ws, fmt.Sprintf("%s level: block has no level attribute", level))
	case slices.Contains(allowed, Level(b.Level)):
		level = Level(b.Level)
	default:
		ws = append(ws, fmt.Sprintf("%s level: block says level=%q", level, b.Level))
	}
	for _, w := range b.Warnings {
		ws = append(ws, fmt.Sprintf("%s level: %s", level, w))
	}
	return b, level, ws
}

func firstValue(levels []levelBlock, key Key, soft bool) (Value, bool) {
	for _, l := range levels {
		if l.block == nil {
			continue
		}
		if e, ok := l.block.Values[key]; ok && e.Soft == soft {
			return Value{key, e.Value, l.level}, true
		}
	}
	return Value{}, false
}

// apply is the per-key precedence in docs/spec/workflow-preferences.md.
func apply(levels []levelBlock, origin Origin, worktreeRoot, harness string, warnings []string) Resolved {
	explicit := func(k Key) (Value, bool) { return firstValue(levels, k, false) }
	soft := func(k Key) (Value, bool) { return firstValue(levels, k, true) }
	pick := func(k Key, fallback Value) Value { return pickValue(levels, k, fallback) }

	sc, scExplicit := explicit(KeySourceControl)
	switch {
	case scExplicit:
	case origin.Provider != "":
		sc = Value{KeySourceControl, origin.Provider, LevelOrigin}
	default:
		sc = pick(KeySourceControl, defaultFor(KeySourceControl, ""))
	}

	host, hostExplicit := explicit(KeySourceControlHost)
	switch {
	case hostExplicit:
	case origin.Provider != "" && origin.Provider == sc.Value:
		host = Value{KeySourceControlHost, origin.Host, LevelOrigin}
	default:
		host = pick(KeySourceControlHost, defaultFor(KeySourceControlHost, sc.Value))
	}

	if scExplicit && origin.Provider != "" && sc.Value != origin.Provider {
		warnings = append(warnings, fmt.Sprintf(
			"source-control %s (%s level) differs from the origin provider %s",
			sc.Value, sc.Level, origin.Provider))
	}
	if origin.Host != "" && origin.Provider == "" && !(scExplicit && hostExplicit) {
		warnings = append(warnings, fmt.Sprintf(
			"origin host %s maps to no provider; set source-control and source-control-host",
			origin.Host))
	}

	tracker, ok := explicit(KeyIssueTracker)
	if !ok {
		tracker, ok = soft(KeyIssueTracker)
	}
	if !ok {
		tracker = Value{KeyIssueTracker, trackerFor(sc.Value), LevelDerived}
	}

	project := pick(KeyIssueTrackerProject, defaultFor(KeyIssueTrackerProject, ""))
	worktrees := worktreesValue(levels, worktreeRoot, harness)

	var values []Value
	for _, v := range []Value{sc, host, tracker, project, worktrees} {
		if v.Value != "" {
			values = append(values, v)
		}
	}
	return Resolved{
		Values:   values,
		Warnings: warnings,
		Read: []string{
			"references/source-control/" + sc.Value + ".md",
			"references/issue-tracking/" + tracker.Value + ".md",
			"references/worktrees.md",
		},
	}
}

// defaultFor is the built-in default for key; the zero Value means no default.
// The host default depends on the resolved provider.
func defaultFor(key Key, provider string) Value {
	switch key {
	case KeySourceControl:
		return Value{key, "github", LevelDefault}
	case KeySourceControlHost:
		if host := providerHost[provider]; host != "" {
			return Value{key, host, LevelDefault}
		}
	case KeyWorktrees:
		return Value{key, "auto", LevelDefault}
	}
	return Value{}
}

func trackerFor(provider string) string {
	if hostTrackers[provider] {
		return provider
	}
	return "filesystem"
}

func pickValue(levels []levelBlock, key Key, fallback Value) Value {
	if v, ok := firstValue(levels, key, false); ok {
		return v
	}
	if v, ok := firstValue(levels, key, true); ok {
		return v
	}
	return fallback
}

func worktreesValue(levels []levelBlock, root, harness string) Value {
	v := pickValue(levels, KeyWorktrees, defaultFor(KeyWorktrees, ""))
	if root != "" {
		v.Value = worktreePath(root, harness, v.Value)
	}
	return v
}

func worktreePath(root, harness, pref string) string {
	if pref == "auto" {
		pref = ""
	}
	abs := config.WorktreeDir(root, harness, pref)
	if abs == "off" {
		return abs
	}
	if rel, ok := config.RepoRelative(root, abs); ok {
		return filepath.ToSlash(rel)
	}
	return abs
}
