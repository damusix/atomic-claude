package signals

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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

// ScanLanguages counts LOC per language by extension across the repo,
// excluding the standard skip set. Returns top 10 by LOC, sorted descending.
func ScanLanguages(root string) (string, error) {
	locByLang := make(map[string]int)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		lang, ok := extToLang[ext]
		if !ok {
			return nil
		}
		loc, err := countLines(path)
		if err != nil {
			return nil // skip unreadable files
		}
		locByLang[lang] += loc
		return nil
	})
	if err != nil {
		return "", err
	}

	// Compute total.
	total := 0
	for _, n := range locByLang {
		total += n
	}

	type langEntry struct {
		name string
		loc  int
	}
	entries := make([]langEntry, 0, len(locByLang))
	for lang, loc := range locByLang {
		entries = append(entries, langEntry{lang, loc})
	}
	// Sort descending by LOC; break ties alphabetically.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].loc != entries[j].loc {
			return entries[i].loc > entries[j].loc
		}
		return entries[i].name < entries[j].name
	})

	// Top 10.
	if len(entries) > 10 {
		entries = entries[:10]
	}

	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		pct := 0
		if total > 0 {
			pct = (e.loc * 100) / total
		}
		lines = append(lines, fmt.Sprintf("- %s: %d LOC (%d%%)", e.name, e.loc, pct))
	}
	return strings.Join(lines, "\n"), nil
}

// countLines counts the number of lines in a file.
func countLines(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	count := strings.Count(string(data), "\n")
	// If file doesn't end in newline, the last line still counts.
	if data[len(data)-1] != '\n' {
		count++
	}
	return count, nil
}
