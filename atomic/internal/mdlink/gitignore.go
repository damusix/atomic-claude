package mdlink

import (
	"os/exec"
	"strings"
)

// LinkifyFile behaves like Linkify but adds an optional gitignore-aware layer:
// when baseDir is inside a git work tree (and git is on PATH), any inline-code
// token that git reports as ignored is left plain text, on top of the static
// skip-set. When git is absent or baseDir is not a git repo, it degrades to
// exactly Linkify — no error, nothing breaks.
func LinkifyFile(content, fileAbsPath, baseDir string) string {
	tokens := extractTokens(content)
	ignored := gitIgnored(baseDir, tokens)
	return linkify(content, fileAbsPath, baseDir, ignored)
}

// gitIgnored is the seam for testing. Production uses defaultGitIgnored; tests
// override it to avoid spawning git.
var gitIgnored = defaultGitIgnored

// defaultGitIgnored returns the subset of tokens that git considers ignored
// under baseDir. Returns an empty (non-nil) map when git is unavailable, baseDir
// is not a git work tree, or no token is ignored. Never returns an error — the
// gitignore layer is best-effort and must not break linkification.
func defaultGitIgnored(baseDir string, tokens []string) map[string]bool {
	res := map[string]bool{}
	if len(tokens) == 0 {
		return res
	}
	if _, err := exec.LookPath("git"); err != nil {
		return res
	}
	// Confirm baseDir is inside a git work tree before asking about ignores.
	out, err := exec.Command("git", "-C", baseDir, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return res
	}

	// Batch all tokens through one check-ignore call. check-ignore echoes back
	// each ignored input path verbatim, one per line.
	cmd := exec.Command("git", "-C", baseDir, "check-ignore", "--stdin")
	cmd.Stdin = strings.NewReader(strings.Join(tokens, "\n") + "\n")
	stdout, err := cmd.Output()
	if err != nil {
		// Exit code 1 means "no path is ignored" — not an error for us.
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

// extractTokens collects inline-code spans (`token`) from prose, skipping fenced
// code blocks. Mirrors Linkify's fence/inline-span recognition so the gitignore
// batch sees exactly the tokens that linkification would consider.
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
