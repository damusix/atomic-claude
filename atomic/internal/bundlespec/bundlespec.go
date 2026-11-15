// Package bundlespec holds the bundle inclusion predicates and the steering source
// descriptors shared by artifacts, bundlemirror, and manifestcheck.
package bundlespec

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/mdparse"
)

// ContextDir is the only tree that ships to a user's harness targets.
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

// SteeringSource is one scope's authored AGENTS.md plus the Claude-native file that
// scope renders to.
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
	// GlobalSteering is the sole authored global Atomic contract, rendered
	// directly into Claude's user-level CLAUDE.md with no global loader.
	GlobalSteering = SteeringSource{Source: "AGENTS.md", ClaudeTarget: "CLAUDE.md"}
	// ScopeSteering is the repository and realm steering pair, shaped by the
	// steering mode; loader mode is AGENTS.md beside a CLAUDE.md that imports it.
	ScopeSteering = SteeringSource{Source: "AGENTS.md", ClaudeTarget: "CLAUDE.md", Loader: true}
)

// SteeringMode is the shape of the repository and realm steering pairs Claude
// reads, set by the `[steering] claude` config key.
type SteeringMode string

const (
	// SteeringLoader writes AGENTS.md plus a thin CLAUDE.md importing it.
	SteeringLoader SteeringMode = "loader"
	// SteeringAgentsMD writes AGENTS.md alone, which Claude Code 2.1.277 and later
	// reads when no CLAUDE.md sits beside it.
	SteeringAgentsMD SteeringMode = "agents-md"
	// SteeringClaudeMD writes CLAUDE.md alone, with the guidance inline.
	SteeringClaudeMD SteeringMode = "claude-md"
)

// SteeringModes lists every valid mode, the default first.
var SteeringModes = []SteeringMode{SteeringLoader, SteeringAgentsMD, SteeringClaudeMD}

// GuidanceFile names the file that carries a scope's guidance in mode.
func (s SteeringSource) GuidanceFile(mode SteeringMode) string {
	if mode == SteeringClaudeMD {
		return s.ClaudeTarget
	}
	return s.Source
}

// IsGlobalSteeringSource reports whether a context/-relative name is the
// authored global contract.
func IsGlobalSteeringSource(name string) bool {
	return name == GlobalSteering.Source
}

// LoaderBody is the thin Claude loader for a ScopeSteering pair: one relative
// import, so a scope's guidance is delivered exactly once.
func (s SteeringSource) LoaderBody() string {
	return "@" + s.Source + "\n"
}

// LoaderWithoutImport returns the Claude file in dir that does not import
// Source, or "" when Claude loads Source: read alone, or through the import.
func (s SteeringSource) LoaderWithoutImport(dir string) string {
	for _, name := range []string{s.ClaudeTarget, strings.ToLower(s.ClaudeTarget)} {
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err == nil && !s.imported(string(raw)) {
			return path
		}
	}
	return ""
}

// imported reports whether body holds an @Source token outside code blocks and
// code spans, where Claude expands none: https://code.claude.com/docs/en/memory
func (s SteeringSource) imported(body string) bool {
	for _, seg := range mdparse.TextSegments([]byte(body)) {
		for _, field := range strings.Fields(withoutCodeSpans(seg.Text)) {
			if field == "@"+s.Source || field == "@./"+s.Source {
				return true
			}
		}
	}
	return false
}

// withoutCodeSpans drops every backtick code span from line; a run with no
// closing run of the same length stays as text.
func withoutCodeSpans(line string) string {
	var out strings.Builder
	for {
		start := strings.Index(line, "`")
		if start < 0 {
			return out.String() + line
		}
		run := len(line[start:]) - len(strings.TrimLeft(line[start:], "`"))
		delim := line[start : start+run]
		end := closingRun(line[start+run:], delim)
		if end < 0 {
			out.WriteString(line[:start+run])
			line = line[start+run:]
			continue
		}
		out.WriteString(line[:start] + " ")
		line = line[start+run+end+run:]
	}
}

// closingRun returns the index in rest of a backtick run exactly as long as
// delim, or -1.
func closingRun(rest, delim string) int {
	for i := 0; i < len(rest); {
		if rest[i] != '`' {
			i++
			continue
		}
		run := len(rest[i:]) - len(strings.TrimLeft(rest[i:], "`"))
		if run == len(delim) {
			return i
		}
		i += run
	}
	return -1
}

// OwnsLoader reports whether raw's Atomic block is exactly the thin loader, the
// CLAUDE.md a steering-mode conversion treats as Atomic's own.
func (s SteeringSource) OwnsLoader(raw []byte) bool {
	return managedfile.BlocksEqual(raw, s.LoaderDocument())
}

// LoaderDocument renders LoaderBody as a managed block, with blank lines around the
// import so the block's HTML tags cannot swallow it.
func (s SteeringSource) LoaderDocument() []byte {
	return managedfile.BracketedBlockDocument([]byte(s.LoaderBody()))
}
