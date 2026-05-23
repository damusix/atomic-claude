package signals

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// fileMeta holds per-file metadata derived from a single file read.
type fileMeta struct {
	sha   string // 7-char hex prefix of SHA-256 of file bytes
	lines int
	chars int
	bytes int
}

// extToLang maps file extensions to language names.
var extToLang = map[string]string{
	".go":    "Go",
	".ts":    "TypeScript",
	".tsx":   "TypeScript",
	".js":    "JavaScript",
	".jsx":   "JavaScript",
	".py":    "Python",
	".rs":    "Rust",
	".rb":    "Ruby",
	".java":  "Java",
	".kt":    "Kotlin",
	".swift": "Swift",
	".cs":    "C#",
	".c":     "C",
	".h":     "C",
	".cpp":   "C++",
	".cc":    "C++",
	".hpp":   "C++",
	".md":    "Markdown",
	".sh":    "Shell",
	".lua":   "Lua",
	".php":   "PHP",
}

// ScanLanguages counts LOC and file count per language by extension across the
// repo. Uses enumerateFiles as the source of truth. Returns top 10 by LOC
// (tie-break: file count descending), sorted descending.
// Format: "- Go: 1820 LOC (27%), 14 files (33%)"
// Percentages are computed over the union of files that match any language.
func ScanLanguages(root string) (string, error) {
	files, err := enumerateFiles(root)
	if err != nil {
		return "", err
	}

	type langStats struct {
		loc   int
		files int
	}
	byLang := make(map[string]*langStats)

	for _, rel := range files {
		ext := strings.ToLower(filepath.Ext(rel))
		lang, ok := extToLang[ext]
		if !ok {
			continue
		}
		absPath := filepath.Join(root, filepath.FromSlash(rel))
		loc, err := countLines(absPath)
		if err != nil {
			continue
		}
		if byLang[lang] == nil {
			byLang[lang] = &langStats{}
		}
		byLang[lang].loc += loc
		byLang[lang].files++
	}

	// Totals across all matched files (for percentage computation).
	totalLOC := 0
	totalFiles := 0
	for _, s := range byLang {
		totalLOC += s.loc
		totalFiles += s.files
	}

	type langEntry struct {
		name  string
		loc   int
		files int
	}
	entries := make([]langEntry, 0, len(byLang))
	for lang, s := range byLang {
		entries = append(entries, langEntry{lang, s.loc, s.files})
	}
	// Sort descending by LOC; tie-break by file count descending; then by name.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].loc != entries[j].loc {
			return entries[i].loc > entries[j].loc
		}
		if entries[i].files != entries[j].files {
			return entries[i].files > entries[j].files
		}
		return entries[i].name < entries[j].name
	})

	// Top 10.
	if len(entries) > 10 {
		entries = entries[:10]
	}

	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		locPct := 0
		if totalLOC > 0 {
			locPct = (e.loc * 100) / totalLOC
		}
		filesPct := 0
		if totalFiles > 0 {
			filesPct = (e.files * 100) / totalFiles
		}
		fileWord := "files"
		if e.files == 1 {
			fileWord = "file"
		}
		lines = append(lines, fmt.Sprintf("- %s: %d LOC (%d%%), %d %s (%d%%)", e.name, e.loc, locPct, e.files, fileWord, filesPct))
	}
	return strings.Join(lines, "\n"), nil
}

// readFileMeta reads the file at path once and computes all per-file metadata:
// SHA-256 (7-char hex prefix), line count, character count, and byte size.
// This is the single-read source for both LOC counting and tree metadata.
func readFileMeta(path string) (fileMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileMeta{}, err
	}
	sum := sha256.Sum256(data)
	shaHex := fmt.Sprintf("%x", sum)[:7]

	byteSize := len(data)
	charCount := utf8.RuneCount(data)

	lineCount := 0
	if byteSize > 0 {
		lineCount = strings.Count(string(data), "\n")
		// If file doesn't end in newline, the last line still counts.
		if data[byteSize-1] != '\n' {
			lineCount++
		}
	}

	return fileMeta{
		sha:   shaHex,
		lines: lineCount,
		chars: charCount,
		bytes: byteSize,
	}, nil
}

// countLines counts the number of lines in a file (thin wrapper over readFileMeta).
func countLines(path string) (int, error) {
	m, err := readFileMeta(path)
	if err != nil {
		return 0, err
	}
	return m.lines, nil
}
