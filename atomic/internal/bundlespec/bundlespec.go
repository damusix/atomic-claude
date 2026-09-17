// Package bundlespec holds the bundle inclusion predicates and the steering
// source descriptors. It is a pure leaf: artifacts.Load applies the predicates
// while enumerating the canonical corpus, bundlemirror maps that corpus to
// Claude-native targets, and manifestcheck consumes the mapping at runtime.
package bundlespec

import (
	"path/filepath"
	"strings"
)

// ContextDir is the only tree that ships to a user's harness targets;
// artifacts.Load enumerates it into the canonical corpus and bundlemirror
// projects that corpus into Claude-native files.
const ContextDir = "context"

// SourceRoot is what bundle targets are relative to, so context/agents/x.md
// installs to ~/.claude/agents/x.md — the context/ segment never ships.
func SourceRoot(repoRoot string) string {
	return filepath.Join(repoRoot, ContextDir)
}

// Rule: agents/atomic-*.md — atomic- prefix, .md suffix, files only.
func MatchesAgent(name string) bool {
	return strings.HasPrefix(name, "atomic-") && strings.HasSuffix(name, ".md")
}

// Rule: skills/atomic-*/. Name-only — the caller must gate on IsDir() itself
// and confirm SKILL.md exists, since "atomic-foo.md" also matches.
func MatchesSkillDir(name string) bool {
	return strings.HasPrefix(name, "atomic-")
}

// Rule: output-styles/atomic*.md — atomic prefix (no required dash), .md suffix.
func MatchesOutputStyle(name string) bool {
	return strings.HasPrefix(name, "atomic") && strings.HasSuffix(name, ".md")
}

// Rule: commands/**/*.md — any .md file, including subdirectories.
func MatchesCommand(name string) bool {
	return strings.HasSuffix(name, ".md")
}

// Rule: rules/**/*.md — any .md file under the rules tree (recursive walk).
func MatchesRule(path string) bool {
	return strings.HasSuffix(path, ".md")
}

// SteeringSource is the canonical steering contract: one authored AGENTS.md per
// scope plus the Claude-native file that scope renders to. Global and
// scope-local steering differ only in how Claude consumes the authored bytes —
// see GlobalSteering and ScopeSteering.
type SteeringSource struct {
	// Source is the authored file name, relative to the scope root.
	Source string
	// ClaudeTarget is the Claude-native file name for the scope.
	ClaudeTarget string
	// Loader reports whether ClaudeTarget is a thin import of Source rather
	// than a direct rendering of it.
	Loader bool
}

var (
	// GlobalSteering is the sole authored global Atomic contract. Claude
	// renders it directly into its user-level CLAUDE.md: there is no global
	// ~/.claude/AGENTS.md and no global loader.
	GlobalSteering = SteeringSource{Source: "AGENTS.md", ClaudeTarget: "CLAUDE.md"}
	// ScopeSteering is the repository and realm steering pair: authored
	// guidance in AGENTS.md, with an adjacent thin CLAUDE.md that imports it.
	ScopeSteering = SteeringSource{Source: "AGENTS.md", ClaudeTarget: "CLAUDE.md", Loader: true}
)

// IsGlobalSteeringSource reports whether a context/-relative name is the
// authored global contract.
func IsGlobalSteeringSource(name string) bool {
	return name == GlobalSteering.Source
}

// LoaderBody is the thin Claude loader for a ScopeSteering pair — one relative
// import of the adjacent authored file and nothing else, so a scope's guidance
// is delivered exactly once.
func (s SteeringSource) LoaderBody() string {
	return "@" + s.Source + "\n"
}
