// Package mdlink rewrites inline-code path tokens in markdown prose into
// relative markdown links, when the token resolves under a given base directory.
//
// Disk resolution is the only filter, so prose like `git status` survives
// untouched. Fenced blocks are never linkified and already-linked tokens are
// skipped, which keeps re-runs byte-identical. Links go through filepath.Rel so
// they work in Obsidian, markdown servers, and GitHub alike.
package mdlink

import (
	"os"
	"path/filepath"
	"strings"
)

// skipDirs never linkify even when they resolve: linking build output is noise,
// not navigation. Mirrors the signals/wiki discovery skip set.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"vendor":       true,
	".worktrees":   true,
	"tmp":          true,
}

func isSkipped(token string) bool {
	for _, seg := range strings.Split(token, "/") {
		if skipDirs[seg] {
			return true
		}
	}
	return false
}

// isDegenerate catches tokens made only of '.' and '/'. They always stat clean,
// so the disk gate cannot reject them, yet linking one points at the repo root or
// above it. The empty case matters most: a doubled-backtick span, used to quote
// text containing a backtick, opens a zero-width token that would resolve.
func isDegenerate(token string) bool {
	return strings.Trim(token, "./") == ""
}

// Linkify rewrites each resolvable inline-code span outside fenced blocks.
// fileAbsPath anchors the relative path; baseDir is what tokens resolve against.
func Linkify(content, fileAbsPath, baseDir string) string {
	return linkify(content, fileAbsPath, baseDir, nil)
}

// fenceState's zero value means "not in a fence". Only a run of the SAME
// character at least as long as the opener closes a block; a shorter run, or a
// run of the other fence character, is literal content.
type fenceState struct {
	char   byte // '`' or '~'; 0 when not in a fence
	length int  // number of fence characters in the opener
}

func fenceRunLength(s string, ch byte) int {
	n := 0
	for n < len(s) && s[n] == ch {
		n++
	}
	return n
}

// isFenceOpener returns the fence character and run length, or 0,0.
func isFenceOpener(trimmed string) (ch byte, length int) {
	if len(trimmed) == 0 {
		return 0, 0
	}
	c := trimmed[0]
	if c != '`' && c != '~' {
		return 0, 0
	}
	n := fenceRunLength(trimmed, c)
	if n < 3 {
		return 0, 0
	}
	// CommonMark bars backticks from a backtick fence's info string, but since
	// this only needs to skip linkification, any info string is accepted.
	return c, n
}

func isCloser(trimmed string, fs fenceState) bool {
	if fs.char == 0 {
		return false
	}
	n := fenceRunLength(trimmed, fs.char)
	if n < fs.length {
		return false
	}
	rest := strings.TrimRight(trimmed[n:], " \t")
	return rest == ""
}

// linkify's ignored set holds tokens that stay plain text even when they resolve
// on disk, such as gitignored paths. nil applies skipDirs filtering alone.
func linkify(content, fileAbsPath, baseDir string, ignored map[string]bool) string {
	fileDir := filepath.Dir(fileAbsPath)

	var sb strings.Builder
	sb.Grow(len(content))

	var fence fenceState
	lines := splitLines(content)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if fence.char == 0 {
			if ch, n := isFenceOpener(trimmed); ch != 0 {
				fence = fenceState{char: ch, length: n}
				sb.WriteString(line)
				continue
			}
			sb.WriteString(linkifyLine(line, fileDir, baseDir, ignored))
		} else {
			if isCloser(trimmed, fence) {
				fence = fenceState{}
			}
			sb.WriteString(line)
		}
	}

	return sb.String()
}

// splitLines keeps each line's trailing newline, so the rejoined content is
// byte-identical to the input apart from inserted links.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lines = append(lines, content[start:i+1])
			start = i + 1
		}
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}

func linkifyLine(line, fileDir, baseDir string, ignored map[string]bool) string {
	if !strings.ContainsRune(line, '`') {
		return line
	}

	var sb strings.Builder
	sb.Grow(len(line))

	i := 0
	for i < len(line) {
		bt := strings.IndexByte(line[i:], '`')
		if bt == -1 {
			sb.WriteString(line[i:])
			break
		}

		pos := i + bt

		// A backtick right after '[' may open an existing [`token`](...) link.
		if pos > 0 && line[pos-1] == '[' {
			closePos := strings.IndexByte(line[pos+1:], '`')
			if closePos != -1 {
				afterClose := pos + 1 + closePos + 1 // position after the closing backtick
				if afterClose < len(line) && afterClose+1 < len(line) && line[afterClose] == ']' && line[afterClose+1] == '(' {
					closeLink := strings.IndexByte(line[afterClose+2:], ')')
					if closeLink != -1 {
						// Already a link — copy verbatim.
						end := afterClose + 2 + closeLink + 1
						sb.WriteString(line[i:end])
						i = end
						continue
					}
				}
			}
		}

		sb.WriteString(line[i:pos])

		closePos := strings.IndexByte(line[pos+1:], '`')
		if closePos == -1 {
			sb.WriteString(line[pos:])
			break
		}

		token := line[pos+1 : pos+1+closePos]
		end := pos + 1 + closePos + 1 // position after closing backtick

		if isDegenerate(token) || isSkipped(token) || ignored[token] {
			sb.WriteString("`")
			sb.WriteString(token)
			sb.WriteString("`")
			i = end
			continue
		}

		resolved := filepath.Join(baseDir, token)
		if _, err := os.Stat(resolved); err != nil {
			sb.WriteString("`")
			sb.WriteString(token)
			sb.WriteString("`")
			i = end
			continue
		}

		rel, err := filepath.Rel(fileDir, resolved)
		if err != nil {
			sb.WriteString("`")
			sb.WriteString(token)
			sb.WriteString("`")
			i = end
			continue
		}

		sb.WriteString("[`")
		sb.WriteString(token)
		sb.WriteString("`](")
		sb.WriteString(rel)
		sb.WriteString(")")
		i = end
	}

	return sb.String()
}
