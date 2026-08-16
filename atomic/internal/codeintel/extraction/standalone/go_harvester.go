package standalone

// go_harvester.go — Go string-literal harvester.
//
// HarvestGoStringLiterals scans a Go source string and returns all string
// literal spans. It handles:
//   - Interpreted string literals:  "..."  (double-quoted, with escape sequences)
//   - Raw string literals:          `...`  (backtick-quoted, no escapes)
//
// The scanner is a hand-written state machine rather than a regex or a second
// tree-sitter parse. Reasons:
//  1. Avoids a second full tree-sitter parse (pool borrow, grammar load).
//  2. Go string literal syntax is simple enough to handle correctly with a
//     linear scan; the only edge cases are escape sequences inside interpreted
//     strings and embedded backtick chars (impossible inside raw strings).
//  3. The result feeds IsSQLLiteral, so false negatives (skipped literals) are
//     acceptable; false positives (non-literal content) would produce noise.
//
// WHY separate file:/will add Python/TypeScript harvesters alongside
// this one. Keeping each harvester in its own file keeps diffs surgical.
//
// Heuristic limits: the callee-capture parenStack is paren-only (braces are
// not tracked), so a func literal's own "(" params frame is excluded from
// the callee vocabulary — otherwise a nested paren inside a closure passed
// as a call argument (e.g. db.Query(func(x string) string { ... })) could
// surface the nonsense callee name "func" instead of "" or the outer call.

import "strings"

// StringLiteralSpan holds one harvested literal's content and file-absolute
// line numbers. StartLine and EndLine are 1-based.
type StringLiteralSpan struct {
	Text      string // content of the literal (without surrounding delimiters)
	StartLine int    // 1-based line in the host file where the opening delimiter sits
	EndLine   int    // 1-based line where the closing delimiter sits
	// CalleeExpr is the bare name of the nearest enclosing call expression's
	// callee (e.g. "selectFrom" for db.selectFrom("x")), when the literal sits
	// in that call's argument list. Empty when the literal is not inside a
	// call expression. Used by sql-string-match (C1) confidence tiering.
	CalleeExpr string
}

// HarvestGoStringLiterals scans src (a Go source file) and returns all string
// literal spans with their file-absolute line numbers.
//
// It correctly handles:
//   - Escape sequences inside interpreted strings (e.g. \", \\, \n).
//   - Multi-line raw string literals (backtick-quoted).
//   - String literals inside single-line (//) and multi-line (/* */) comments
//     — those are skipped so comment content is never reported as a literal.
//   - Rune literals ('x') — skipped; they are too short to contain SQL.
//   - String concatenations — each piece is reported as a separate span
//     (multi-fragment queries are an accepted false-negative per spec §Non-goals).
func HarvestGoStringLiterals(src string) []StringLiteralSpan {
	var spans []StringLiteralSpan

	line := 1
	i := 0
	n := len(src)

	// parenStack tracks the bare callee name active at each nesting level of
	// '(' ... ')', for sql-string-match (C1) callee capture. A non-call paren
	// (grouping, if/for/switch conditions) pushes "" so it doesn't leak a
	// stale callee name into its body. No AST is available in this
	// hand-written scanner, so this is a best-effort heuristic: it only
	// recognizes the common `identifier(` (no space) call form.
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
			// Skip to end of line.
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
					// Interpreted strings don't span lines in valid Go, but
					// handle gracefully: record the newline and break.
					line++
					break
				}
				if src[i] == '\\' && i+1 < n {
					// Escape sequence: skip both characters.
					// We do NOT unescape — the content is used for IsSQLLiteral
					// which operates on the raw text.
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
		// Skip to avoid treating 'x' or '\n' as a string.
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

// topOfStack returns the last element of stack, or "" when empty.
func topOfStack(stack []string) string {
	if len(stack) == 0 {
		return ""
	}
	return stack[len(stack)-1]
}

// precedingGoIdentifier looks backward from src[parenPos] (the index of a '('
// byte) for an immediately-adjacent identifier (no space) and returns its bare
// name — the callee of a call expression like "identifier(". Returns "" when
// the preceding bytes are not an identifier (grouping parens, control-flow
// conditions with a space before '(', or a chained call/index expression
// this scanner does not resolve) or the identifier is a Go keyword that is
// never a callable (if/for/switch/select/range).
//
// For a qualified call like "db.Query(", this returns only the bare segment
// after the last '.' ("Query") — matching C1's "bare callee name" contract.
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
