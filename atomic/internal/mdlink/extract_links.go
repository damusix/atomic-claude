package mdlink

import (
	"strings"
)

type LinkKind int

const (
	// MarkdownLink is [text](target).
	MarkdownLink LinkKind = iota
	// Wikilink is [[page]] or [[page|alias]].
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

type Link struct {
	// Text is the bracket text, or a wikilink's alias falling back to its page name.
	Text string

	// Target is the parenthesized destination, or a wikilink's page name.
	Target string

	Kind LinkKind

	// Line is 1-based.
	Line int
}

// ExtractLinks returns every markdown link and wikilink in content, excluding
// fenced blocks and inline code spans via Linkify's fence tracking.
func ExtractLinks(content string) []Link {
	var results []Link

	lines := splitLines(content)
	lineNum := 0
	var fence fenceState

	for _, line := range lines {
		lineNum++
		trimmed := strings.TrimSpace(line)

		if fence.char == 0 {
			if ch, n := isFenceOpener(trimmed); ch != 0 {
				fence = fenceState{char: ch, length: n}
				continue
			}
			results = append(results, extractLineLinks(line, lineNum)...)
		} else {
			if isCloser(trimmed, fence) {
				fence = fenceState{}
			}
		}
	}

	return results
}

func extractLineLinks(line string, lineNum int) []Link {
	var results []Link
	i := 0
	n := len(line)

	for i < n {
		if line[i] == '`' {
			close := strings.IndexByte(line[i+1:], '`')
			if close == -1 {
				break
			}
			i = i + 1 + close + 1
			continue
		}

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

		if line[i] == '[' {
			closeBracket := strings.IndexByte(line[i+1:], ']')
			if closeBracket != -1 {
				afterBracket := i + 1 + closeBracket + 1
				if afterBracket < n && line[afterBracket] == '(' {
					closeParen := strings.IndexByte(line[afterBracket+1:], ')')
					if closeParen != -1 {
						text := line[i+1 : i+1+closeBracket]
						target := line[afterBracket+1 : afterBracket+1+closeParen]
						// ![...](...) is an image, not a link.
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

// parseWikilink handles both [[page]] and [[page|alias]].
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
