// Package mdlink provides a reusable linkifier that rewrites inline-code path
// tokens (` `token` `) in markdown prose into relative markdown links when the
// token resolves to a real file or directory under a given base directory.
//
// Design constraints (from spec signals-wiki-linkify.md):
//   - Disk resolution is the only filter. Tokens that don't stat from base are
//     left untouched (e.g. `git status`, `atomic signals scan`).
//   - Fenced code blocks (``` ... ```) are never linkified.
//   - Already-linked tokens (` [`token`](...) `) are skipped so re-runs are
//     byte-identical (idempotency).
//   - Links are file-relative using filepath.Rel — portable across Obsidian,
//     markdown servers, and GitHub.
package mdlink

import (
	"os"
	"path/filepath"
	"strings"
)

// skipDirs are path segments that are never linkified even when they resolve
// on disk. Linking to .git/, node_modules/, or build output is noise, not
// navigation. Mirrors the junk-dir skip set used by signals/wiki discovery.
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

// isSkipped reports whether any path segment of token is in skipDirs.
func isSkipped(token string) bool {
	for _, seg := range strings.Split(token, "/") {
		if skipDirs[seg] {
			return true
		}
	}
	return false
}

// Linkify scans content line by line, skipping fenced code blocks, and for
// each inline-code span “ `token` “ in prose/tables/bullets:
//   - If filepath.Join(baseDir, token) exists on disk, replaces “ `token` “
//     with “ [`token`](relpath) “ where relpath = filepath.Rel(dir(fileAbsPath), filepath.Join(baseDir, token)).
//   - If the token is already the text of a markdown link (“ [`token`](...) “), it is skipped.
//   - Tokens that don't resolve → untouched.
//
// fileAbsPath is the absolute path of the file being processed (used to
// compute the correct relative path). baseDir is the root directory against
// which token paths are resolved (e.g. repo root).
func Linkify(content, fileAbsPath, baseDir string) string {
	return linkify(content, fileAbsPath, baseDir, nil)
}

// linkify is the core implementation. ignored, when non-nil, is a set of tokens
// that must stay plain text even if they resolve on disk (e.g. gitignored
// paths). A nil set means skip-set filtering only.
func linkify(content, fileAbsPath, baseDir string, ignored map[string]bool) string {
	fileDir := filepath.Dir(fileAbsPath)

	var sb strings.Builder
	sb.Grow(len(content))

	inFence := false
	lines := splitLines(content)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track fenced code block boundaries.
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			sb.WriteString(line)
			continue
		}

		if inFence {
			sb.WriteString(line)
			continue
		}

		// Linkify inline-code spans in prose/tables/bullets.
		sb.WriteString(linkifyLine(line, fileDir, baseDir, ignored))
	}

	return sb.String()
}

// splitLines splits content into lines, preserving the newline character at the
// end of each line (except potentially the last). This ensures the reconstructed
// content is byte-identical to the input modulo link insertions.
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

// linkifyLine processes a single non-fenced line, replacing resolvable
// inline-code tokens with markdown links.
func linkifyLine(line, fileDir, baseDir string, ignored map[string]bool) string {
	// Fast path: if there are no backticks, nothing to do.
	if !strings.ContainsRune(line, '`') {
		return line
	}

	var sb strings.Builder
	sb.Grow(len(line))

	i := 0
	for i < len(line) {
		// Look for the next backtick.
		bt := strings.IndexByte(line[i:], '`')
		if bt == -1 {
			// No more backticks — write the rest.
			sb.WriteString(line[i:])
			break
		}

		pos := i + bt

		// Check if this backtick is part of an already-linked token:
		// pattern: [`token`](...)
		// The backtick at pos would be the opening one after '['.
		if pos > 0 && line[pos-1] == '[' {
			// Already linked — find the closing backtick of the token text.
			closePos := strings.IndexByte(line[pos+1:], '`')
			if closePos != -1 {
				// Token text is line[pos+1 : pos+1+closePos].
				afterClose := pos + 1 + closePos + 1 // position after the closing backtick
				// Check for '](' immediately after the closing backtick.
				if afterClose < len(line) && afterClose+1 < len(line) && line[afterClose] == ']' && line[afterClose+1] == '(' {
					// Find the closing ')'.
					closeLink := strings.IndexByte(line[afterClose+2:], ')')
					if closeLink != -1 {
						// This entire span is already a markdown link — copy it verbatim.
						end := afterClose + 2 + closeLink + 1
						sb.WriteString(line[i:end])
						i = end
						continue
					}
				}
			}
		}

		// Write everything before this backtick.
		sb.WriteString(line[i:pos])

		// Find the closing backtick.
		closePos := strings.IndexByte(line[pos+1:], '`')
		if closePos == -1 {
			// Unclosed backtick — write the rest verbatim.
			sb.WriteString(line[pos:])
			break
		}

		token := line[pos+1 : pos+1+closePos]
		end := pos + 1 + closePos + 1 // position after closing backtick

		// Skip junk-dir tokens and gitignored tokens even if they resolve on disk.
		if isSkipped(token) || ignored[token] {
			sb.WriteString("`")
			sb.WriteString(token)
			sb.WriteString("`")
			i = end
			continue
		}

		// Try to resolve token against baseDir.
		resolved := filepath.Join(baseDir, token)
		if _, err := os.Stat(resolved); err != nil {
			// Doesn't exist — emit the backtick span unchanged.
			sb.WriteString("`")
			sb.WriteString(token)
			sb.WriteString("`")
			i = end
			continue
		}

		// Compute relative path from fileDir to resolved.
		rel, err := filepath.Rel(fileDir, resolved)
		if err != nil {
			// Fallback: emit unchanged.
			sb.WriteString("`")
			sb.WriteString(token)
			sb.WriteString("`")
			i = end
			continue
		}

		// Emit markdown link.
		sb.WriteString("[`")
		sb.WriteString(token)
		sb.WriteString("`](")
		sb.WriteString(rel)
		sb.WriteString(")")
		i = end
	}

	return sb.String()
}
