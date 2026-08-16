// embedded-sql-admission: scan source files in a directory tree and report
// all string literals that pass the IsSQLLiteral admission gate.
//
// Usage:
//   embedded-sql-admission <dir> [--no-dump]
//
// Output (to stdout):
//   CORPUS_ROOT: <dir>
//   TOTAL_LITERALS_SCANNED: <n>
//   ADMITTED_COUNT: <n>
//   ---ADMITTED LITERALS---
//   [file:line] <literal text (first 200 chars)>
//   ...
//
// Exit codes:
//   0: success
//   1: usage error or fatal IO error

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: embedded-sql-admission <dir> [--no-dump]")
		os.Exit(1)
	}
	root := os.Args[1]
	dump := true
	for _, a := range os.Args[2:] {
		if a == "--no-dump" {
			dump = false
		}
	}

	type admittedLiteral struct {
		file string
		line int
		text string
	}

	var admitted []admittedLiteral
	totalScanned := 0
	skipped := 0

	extensions := map[string]bool{
		".go":  true,
		".py":  true,
		".ts":  true,
		".tsx": true,
	}

	skipDirs := map[string]bool{
		"vendor": true, "node_modules": true, ".git": true, ".claude": true,
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			skipped++
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			name := d.Name()
			if skipDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !extensions[ext] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			skipped++
			return nil // skip unreadable file
		}

		// Use the proper Go harvester for .go files.
		// For Python/TS: use a best-effort double-quoted scanner — the
		// adversarial corpus is primarily Go, so this is adequate for
		// the FP/admission surface report.
		var spans []standalone.StringLiteralSpan
		switch ext {
		case ".go":
			spans = standalone.HarvestGoStringLiterals(string(src))
		default:
			spans = roughHarvestDoubleQuoted(string(src))
		}

		totalScanned += len(spans)
		rel, _ := filepath.Rel(root, path)

		for _, span := range spans {
			if standalone.IsSQLLiteral(span.Text) {
				admitted = append(admitted, admittedLiteral{
					file: rel,
					line: span.StartLine,
					text: span.Text,
				})
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "walk error:", err)
		os.Exit(1)
	}

	fmt.Printf("CORPUS_ROOT: %s\n", root)
	fmt.Printf("TOTAL_LITERALS_SCANNED: %d\n", totalScanned)
	fmt.Printf("ADMITTED_COUNT: %d\n", len(admitted))
	fmt.Printf("SKIPPED_COUNT: %d\n", skipped)

	if dump && len(admitted) > 0 {
		fmt.Println("---ADMITTED LITERALS---")
		for _, a := range admitted {
			text := a.text
			if len(text) > 200 {
				text = text[:200] + "...(truncated)"
			}
			// Replace newlines for single-line display.
			text = strings.ReplaceAll(text, "\n", "\\n")
			fmt.Printf("[%s:%d] %s\n", a.file, a.line, text)
		}
	}
}

// roughHarvestDoubleQuoted does a simple scan for double-quoted strings on
// non-Go files where we don't have a proper harvester. Used only for
// admission surface reporting on the adversarial corpus; acceptable to miss
// edge cases (raw strings, template literals, etc.).
func roughHarvestDoubleQuoted(src string) []standalone.StringLiteralSpan {
	var spans []standalone.StringLiteralSpan
	line := 1
	i := 0
	n := len(src)
	for i < n {
		ch := src[i]
		if ch == '\n' {
			line++
			i++
			continue
		}
		if ch == '"' {
			startLine := line
			i++
			start := i
			for i < n && src[i] != '"' && src[i] != '\n' {
				if src[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				i++
			}
			text := src[start:i]
			if i < n && src[i] == '"' {
				i++
			}
			spans = append(spans, standalone.StringLiteralSpan{
				Text:      text,
				StartLine: startLine,
				EndLine:   line,
			})
			continue
		}
		i++
	}
	return spans
}
