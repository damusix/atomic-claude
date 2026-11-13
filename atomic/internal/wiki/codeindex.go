package wiki

import "strings"

// CodeIndexOpen and CodeIndexClose delimit the realm <code-index> block that
// `atomic code index` owns in a realm root steering file.
const (
	CodeIndexOpen  = "<code-index>"
	CodeIndexClose = "</code-index>"
)

// CodeIndexStub is the note written beside a fresh block; LegacyCodeIndexStub is
// the one earlier versions wrote, which stripping still recognizes.
const (
	CodeIndexStub       = "\n<!-- Realm steering — managed by `atomic code index`. Edit below this block freely. -->\n"
	LegacyCodeIndexStub = "\n<!-- Realm CLAUDE.md — managed by `atomic code index`. Edit below this block freely. -->\n"
)

// FindCodeIndexBlock returns the span of content's <code-index> block. An open
// tag with no close runs to the end of content.
func FindCodeIndexBlock(content string) (start, end int, ok bool) {
	start = strings.Index(content, CodeIndexOpen)
	if start == -1 {
		return 0, 0, false
	}
	closeIdx := strings.Index(content[start:], CodeIndexClose)
	if closeIdx == -1 {
		return start, len(content), true
	}
	return start, start + closeIdx + len(CodeIndexClose), true
}

// StripCodeIndex removes the block, the blank line a splice put before it, and
// the stub beside it, leaving the rest of content as it was.
func StripCodeIndex(content string) string {
	start, end, ok := FindCodeIndexBlock(content)
	if !ok {
		return content
	}
	before := content[:start]
	if strings.HasSuffix(before, "\n\n") {
		before = before[:len(before)-1]
	}
	rest := before + content[end:]
	for _, stub := range []string{CodeIndexStub, LegacyCodeIndexStub} {
		rest = strings.Replace(rest, stub, "", 1)
	}
	return rest
}
