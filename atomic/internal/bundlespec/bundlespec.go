// Package bundlespec contains pure predicate functions that define the bundle
// inclusion rules for atomic Claude Code artifacts. Used by both
// bundlemirror (build-time mirror) and manifestcheck (runtime validator) so
// the rules have a single source of truth.
package bundlespec

import "strings"

// MatchesAgent reports whether name is a bundleable agent file.
// Rule: agents/atomic-*.md — atomic- prefix, .md suffix, files only.
func MatchesAgent(name string) bool {
	return strings.HasPrefix(name, "atomic-") && strings.HasSuffix(name, ".md")
}

// MatchesSkillDir reports whether name is a bundleable skill directory.
// Rule: skills/atomic-*/ — atomic- prefix, directory. Caller still checks
// that SKILL.md exists inside the directory before bundling.
func MatchesSkillDir(name string) bool {
	return strings.HasPrefix(name, "atomic-")
}

// MatchesOutputStyle reports whether name is a bundleable output-style file.
// Rule: output-styles/atomic*.md — atomic prefix (no required dash), .md suffix.
func MatchesOutputStyle(name string) bool {
	return strings.HasPrefix(name, "atomic") && strings.HasSuffix(name, ".md")
}

// MatchesCommand reports whether name is a bundleable command file.
// Rule: commands/*.md — any .md file at the top level (caller skips subdirs).
func MatchesCommand(name string) bool {
	return strings.HasSuffix(name, ".md")
}

// MatchesRule reports whether path is a bundleable rule file.
// Rule: rules/**/*.md — any .md file under the rules tree (recursive walk).
// path is the full path or relative path as seen by the walker.
func MatchesRule(path string) bool {
	return strings.HasSuffix(path, ".md")
}

// IsClaudeMd reports whether name is the canonical CLAUDE.md artifact.
// Exact match only — case-sensitive.
func IsClaudeMd(name string) bool {
	return name == "CLAUDE.md"
}
