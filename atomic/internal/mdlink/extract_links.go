package mdlink

import (
	"strings"
)

// LinkKind distinguishes the syntax used to express a link.
type LinkKind int

const (
	// MarkdownLink is a standard markdown link: [text](target)
	MarkdownLink LinkKind = iota
	// Wikilink is an Obsidian-style wikilink: [[page]] or [[page|alias]]
	Wikilink
)

func (k LinkKind) String() string {
	switch k {
	case MarkdownLink:
		return "MarkdownLink"
	case Wikilink:
		return "Wikilink"
	default:
		return "Unknown"
	}
}

// Link is a single link extracted from a markdown document.
type Link struct {
	// Text is the display text.
	//   - MarkdownLink: the bracket text, e.g. "overview" in [overview](target)
	//   - Wikilink: the alias if present, else the page name
	Text string

	// Target is the link destination.
	//   - MarkdownLink: the URL or path inside the parens
	//   - Wikilink: the page name (left of '|' when an alias is present)
	Target string

	// Kind is MarkdownLink or Wikilink.
	Kind LinkKind

	// Line is the 1-based line number of the link in the source content.
	Line int
}

// ExtractLinks returns all markdown links [text](target) and Obsidian wikilinks
// [[page]] / [[page|alias]] found in content.
//
// Fence-aware: links inside fenced code blocks (``` or ~~~) and inline code
// spans (`…`) are excluded. This reuses the same fenceState/isFenceOpener/isCloser
// fence-tracking infrastructure used by Linkify.
func ExtractLinks(content string) []Link {
	var results []Link

	lines := splitLines(content)
	lineNum := 0
	var fence fenceState

	for _, line := range lines {
		lineNum++
		trimmed := strings.TrimSpace(line)

		if fence.char == 0 {
			// Not in a fence: check for an opener.
			if ch, n := isFenceOpener(trimmed); ch != 0 {
				fence = fenceState{char: ch, length: n}
				continue
			}
			// Normal prose line — extract links, skipping inline code spans.
			results = append(results, extractLineLinks(line, lineNum)...)
		} else {
			// Inside a fence: check for the closer.
			if isCloser(trimmed, fence) {
				fence = fenceState{}
			}
			// Skip all content inside the fence (no link extraction).
		}
	}

	return results
}

// extractLineLinks extracts links from a single non-fenced line, skipping
// content inside inline code spans (`…`).
func extractLineLinks(line string, lineNum int) []Link {
	var results []Link
	i := 0
	n := len(line)

	for i < n {
		// Skip inline code spans to avoid matching links inside them.
		if line[i] == '`' {
			// Find the closing backtick.
			close := strings.IndexByte(line[i+1:], '`')
			if close == -1 {
				// Unclosed — skip the rest of the line.
				break
			}
			// Jump past the closing backtick.
			i = i + 1 + close + 1
			continue
		}

		// Check for wikilink: [[
		if i+1 < n && line[i] == '[' && line[i+1] == '[' {
			close := strings.Index(line[i+2:], "]]")
			if close != -1 {
				inner := line[i+2 : i+2+close]
				link := parseWikilink(inner, lineNum)
				results = append(results, link)
				i = i + 2 + close + 2
				continue
			}
		}

		// Check for markdown link: [text](target)
		if line[i] == '[' {
			// Find the closing ']'.
			closeBracket := strings.IndexByte(line[i+1:], ']')
			if closeBracket != -1 {
				afterBracket := i + 1 + closeBracket + 1
				// Must be immediately followed by '('.
				if afterBracket < n && line[afterBracket] == '(' {
					closeParen := strings.IndexByte(line[afterBracket+1:], ')')
					if closeParen != -1 {
						text := line[i+1 : i+1+closeBracket]
						target := line[afterBracket+1 : afterBracket+1+closeParen]
						// Skip image links: ![...](...)
						if i > 0 && line[i-1] == '!' {
							i = afterBracket + 1 + closeParen + 1
							continue
						}
						results = append(results, Link{
							Text:   text,
							Target: target,
							Kind:   MarkdownLink,
							Line:   lineNum,
						})
						i = afterBracket + 1 + closeParen + 1
						continue
					}
				}
			}
		}

		i++
	}

	return results
}

// parseWikilink parses the inner content of [[…]] and returns a Link.
// Supports [[page]] and [[page|alias]].
func parseWikilink(inner string, lineNum int) Link {
	if idx := strings.IndexByte(inner, '|'); idx != -1 {
		page := strings.TrimSpace(inner[:idx])
		alias := strings.TrimSpace(inner[idx+1:])
		return Link{
			Text:   alias,
			Target: page,
			Kind:   Wikilink,
			Line:   lineNum,
		}
	}
	page := strings.TrimSpace(inner)
	return Link{
		Text:   page,
		Target: page,
		Kind:   Wikilink,
		Line:   lineNum,
	}
}
