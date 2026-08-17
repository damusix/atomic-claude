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

type fileMeta struct {
	sha   string // 7-char hex prefix of SHA-256 of file bytes
	lines int
	chars int
	bytes int
}

var extToLang = map[string]string{
	".go":         "Go",
	".ts":         "TypeScript",
	".tsx":        "TypeScript",
	".js":         "JavaScript",
	".jsx":        "JavaScript",
	".mjs":        "JavaScript",
	".cjs":        "JavaScript",
	".py":         "Python",
	".rs":         "Rust",
	".rb":         "Ruby",
	".java":       "Java",
	".kt":         "Kotlin",
	".swift":      "Swift",
	".cs":         "C#",
	".c":          "C",
	".h":          "C",
	".cpp":        "C++",
	".cc":         "C++",
	".hpp":        "C++",
	".md":         "Markdown",
	".mdx":        "MDX",
	".sh":         "Shell",
	".lua":        "Lua",
	".php":        "PHP",
	".css":        "CSS",
	".scss":       "SCSS",
	".sass":       "Sass",
	".less":       "Less",
	".styl":       "Stylus",
	".html":       "HTML",
	".htm":        "HTML",
	".vue":        "Vue",
	".svelte":     "Svelte",
	".astro":      "Astro",
	".hbs":        "Handlebars",
	".handlebars": "Handlebars",
	".ejs":        "EJS",
	".pug":        "Pug",
	".liquid":     "Liquid",
	".erb":        "ERB",
	".twig":       "Twig",
	".coffee":     "CoffeeScript",
	".graphql":    "GraphQL",
	".gql":        "GraphQL",
	".dart":       "Dart",
	".sol":        "Solidity",
	".elm":        "Elm",
	".json":       "JSON",
	".yml":        "YAML",
	".yaml":       "YAML",
	".toml":       "TOML",
	".xml":        "XML",
}

type langStats struct {
	loc   int
	files int
}

// ScanLanguages returns the top 10 languages by LOC, tie-broken on file count,
// as "- Go: 1820 LOC (27%), 14 files (33%)". Percentages are over the files that
// matched any language, not the whole repo.
func ScanLanguages(root string) (string, error) {
	files, err := enumerateFiles(root)
	if err != nil {
		return "", err
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

	return formatLangStats(byLang), nil
}

// scanLanguagesFromCache reuses the tree pass's metadata, re-reading only files
// beyond the depth cap. Output matches ScanLanguages exactly.
func scanLanguagesFromCache(root string, metaCache map[string]fileMeta) (string, error) {
	files, err := enumerateFiles(root)
	if err != nil {
		return "", err
	}

	byLang := make(map[string]*langStats)

	for _, rel := range files {
		ext := strings.ToLower(filepath.Ext(rel))
		lang, ok := extToLang[ext]
		if !ok {
			continue
		}

		var loc int
		if m, cached := metaCache[rel]; cached {
			loc = m.lines
		} else {
			absPath := filepath.Join(root, filepath.FromSlash(rel))
			l, err := countLines(absPath)
			if err != nil {
				continue
			}
			loc = l
		}

		if byLang[lang] == nil {
			byLang[lang] = &langStats{}
		}
		byLang[lang].loc += loc
		byLang[lang].files++
	}

	return formatLangStats(byLang), nil
}

// formatLangStats is shared so both scan paths emit identical output.
func formatLangStats(byLang map[string]*langStats) string {
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
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].loc != entries[j].loc {
			return entries[i].loc > entries[j].loc
		}
		if entries[i].files != entries[j].files {
			return entries[i].files > entries[j].files
		}
		return entries[i].name < entries[j].name
	})

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
	return strings.Join(lines, "\n")
}

// readFileMeta is the single read behind both LOC counting and tree metadata.
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
		// A file not ending in a newline still has a final line.
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

func countLines(path string) (int, error) {
	m, err := readFileMeta(path)
	if err != nil {
		return 0, err
	}
	return m.lines, nil
}
