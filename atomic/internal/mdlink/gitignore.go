package mdlink

import (
	"os/exec"
	"strings"
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
	res := map[string]bool{}
	if len(tokens) == 0 {
		return res
	}
	if _, err := exec.LookPath("git"); err != nil {
		return res
	}
	out, err := exec.Command("git", "-C", baseDir, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return res
	}

	// One batched check-ignore call; it echoes each ignored path verbatim.
	cmd := exec.Command("git", "-C", baseDir, "check-ignore", "--stdin")
	cmd.Stdin = strings.NewReader(strings.Join(tokens, "\n") + "\n")
	stdout, err := cmd.Output()
	if err != nil {
		// Exit 1 means "nothing ignored", not a failure.
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			return res
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(string(stdout)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			res[line] = true
		}
	}
	return res
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
