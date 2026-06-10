package standalone

// go_harvester.go — CP2: Go string-literal harvester.
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
// WHY separate file: CP3/CP4 will add Python/TypeScript harvesters alongside
// this one. Keeping each harvester in its own file keeps diffs surgical.

// StringLiteralSpan holds one harvested literal's content and file-absolute
// line numbers. StartLine and EndLine are 1-based.
type StringLiteralSpan struct {
	Text      string // content of the literal (without surrounding delimiters)
	StartLine int    // 1-based line in the host file where the opening delimiter sits
	EndLine   int    // 1-based line where the closing delimiter sits
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

	for i < n {
		ch := src[i]

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
				Text:      text,
				StartLine: startLine,
				EndLine:   endLine,
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
				Text:      string(buf),
				StartLine: startLine,
				EndLine:   endLine,
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
