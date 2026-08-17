package standalone

// A hand-written scanner rather than a second tree-sitter parse: it avoids a
// pool borrow and grammar load, and its output only feeds IsSQLLiteral, so a
// skipped literal is acceptable where a bogus one would be noise.

import "strings"

// StringLiteralSpan holds one harvested literal and its file-absolute line span.
type StringLiteralSpan struct {
	Text      string // content of the literal (without surrounding delimiters)
	StartLine int    // 1-based line in the host file where the opening delimiter sits
	EndLine   int    // 1-based line where the closing delimiter sits
	// CalleeExpr is the bare callee of the enclosing call (e.g. "selectFrom" for
	// db.selectFrom("x")), empty when the literal is not a call argument.
	CalleeExpr string
}

// HarvestGoStringLiterals returns every string literal span in src with
// file-absolute line numbers. Literals inside comments, and rune literals, are
// skipped; a concatenation yields one span per fragment, so multi-fragment
// queries are an accepted false negative.
func HarvestGoStringLiterals(src string) []StringLiteralSpan {
	var spans []StringLiteralSpan

	line := 1
	i := 0
	n := len(src)

	// parenStack holds the bare callee name active at each paren nesting level;
	// a non-call paren pushes "" so a stale callee cannot leak into its body.
	// Best-effort without an AST: only the adjacent identifier( form counts, and
	// braces are untracked, so a func literal's own parens surface no callee.
	var parenStack []string

	for i < n {
		ch := src[i]

		// ---------- call/group open paren ----------
		if ch == '(' {
			parenStack = append(parenStack, precedingGoIdentifier(src, i))
			i++
			continue
		}
		if ch == ')' {
			if len(parenStack) > 0 {
				parenStack = parenStack[:len(parenStack)-1]
			}
			i++
			continue
		}

		// ---------- newline ----------
		if ch == '\n' {
			line++
			i++
			continue
		}

		// ---------- single-line comment ----------
		if ch == '/' && i+1 < n && src[i+1] == '/' {
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}

		// ---------- multi-line comment ----------
		if ch == '/' && i+1 < n && src[i+1] == '*' {
			i += 2
			for i < n {
				if src[i] == '\n' {
					line++
					i++
				} else if src[i] == '*' && i+1 < n && src[i+1] == '/' {
					i += 2
					break
				} else {
					i++
				}
			}
			continue
		}

		// ---------- raw string literal `...` ----------
		if ch == '`' {
			startLine := line
			i++
			start := i
			for i < n && src[i] != '`' {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			endLine := line
			text := src[start:i]
			if i < n {
				i++ // consume closing backtick
			}
			spans = append(spans, StringLiteralSpan{
				Text:       text,
				StartLine:  startLine,
				EndLine:    endLine,
				CalleeExpr: topOfStack(parenStack),
			})
			continue
		}

		// ---------- interpreted string literal "..." ----------
		if ch == '"' {
			startLine := line
			i++
			var buf []byte
			for i < n && src[i] != '"' {
				if src[i] == '\n' {
					// Not valid Go; bail rather than run to EOF.
					line++
					break
				}
				if src[i] == '\\' && i+1 < n {
					// Kept escaped: IsSQLLiteral works on the raw text.
					buf = append(buf, src[i], src[i+1])
					i += 2
					continue
				}
				buf = append(buf, src[i])
				i++
			}
			endLine := line
			if i < n && src[i] == '"' {
				i++ // consume closing quote
			}
			spans = append(spans, StringLiteralSpan{
				Text:       string(buf),
				StartLine:  startLine,
				EndLine:    endLine,
				CalleeExpr: topOfStack(parenStack),
			})
			continue
		}

		// ---------- rune literal '...' ----------
		if ch == '\'' {
			i++
			for i < n && src[i] != '\'' {
				if src[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if src[i] == '\n' {
					line++
					break
				}
				i++
			}
			if i < n && src[i] == '\'' {
				i++
			}
			continue
		}

		i++
	}

	return spans
}

func topOfStack(stack []string) string {
	if len(stack) == 0 {
		return ""
	}
	return stack[len(stack)-1]
}

// precedingGoIdentifier returns the bare callee of the call whose open paren
// sits at parenPos: the immediately adjacent identifier, taken after the last
// dot for a qualified call like db.Query(. Returns "" when nothing adjacent is
// an identifier (grouping parens, a space before the paren, a chained
// expression this scanner cannot resolve) or when it is a non-callable keyword.
func precedingGoIdentifier(src string, parenPos int) string {
	j := parenPos
	for j > 0 && isGoIdentByte(src[j-1]) {
		j--
	}
	if j == parenPos || (src[j] >= '0' && src[j] <= '9') {
		return "" // no identifier immediately before '(', or it starts with a digit
	}
	name := src[j:parenPos]
	switch name {
	case "if", "for", "switch", "select", "range", "return", "go", "defer", "func":
		return ""
	}
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		name = name[dot+1:]
	}
	return name
}

func isGoIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
