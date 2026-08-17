// Package bundlespec holds the bundle inclusion predicates, shared by
// bundlemirror at build time and manifestcheck at runtime so the two cannot
// disagree about what ships.
package bundlespec

import (
	"path/filepath"
	"strings"
)

// ContextDir is the only tree that ships to a user's ~/.claude/; templates/
// renders into it and never ships itself.
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

// IsClaudeMd is a case-sensitive exact match on the single CLAUDE.md artifact.
func IsClaudeMd(name string) bool {
	return name == "CLAUDE.md"
}
