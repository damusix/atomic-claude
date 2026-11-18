package mdlink

import (
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// LinkifyFile adds a gitignore-aware layer over Linkify: inside a git work tree
// with git on PATH, ignored tokens stay plain text. Otherwise it is exactly
// Linkify — no error, nothing breaks.
func LinkifyFile(content, fileAbsPath, baseDir string) string {
	tokens := extractTokens(content)
	ignored := gitIgnored(baseDir, tokens)
	return linkify(content, fileAbsPath, baseDir, ignored)
}

// gitIgnored is a test seam, overridden to avoid spawning git.
var gitIgnored = defaultGitIgnored

// defaultGitIgnored never errors: the layer is best-effort and must not break
// linkification. A non-nil empty map covers git absent, no repo, and none ignored.
func defaultGitIgnored(baseDir string, tokens []string) map[string]bool {
	return config.GitIgnoredSet(baseDir, tokens)
}

// extractTokens mirrors Linkify's fence and inline-span recognition, so the
// gitignore batch sees exactly the tokens linkification would consider.
func extractTokens(content string) []string {
	var tokens []string
	inFence := false
	for _, line := range splitLines(content) {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.ContainsRune(line, '`') {
			continue
		}
		i := 0
		for i < len(line) {
			open := strings.IndexByte(line[i:], '`')
			if open == -1 {
				break
			}
			open += i
			closeRel := strings.IndexByte(line[open+1:], '`')
			if closeRel == -1 {
				break
			}
			tokens = append(tokens, line[open+1:open+1+closeRel])
			i = open + 1 + closeRel + 1
		}
	}
	return tokens
}
