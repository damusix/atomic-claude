package signals

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// manifestHandler maps a base filename to its parser.
type manifestHandler struct {
	name    string
	parseFn func(absPath, relPath string) (string, error)
}

// manifestHandlers lists all supported manifest types by base filename.
var manifestHandlers = []manifestHandler{
	{"Cargo.toml", func(p, rel string) (string, error) { return parseCargoTOML(p, rel) }},
	{"Gemfile", func(p, rel string) (string, error) { return parseGemfile(p, rel) }},
	{"composer.json", func(p, rel string) (string, error) { return parseComposerJSON(p, rel) }},
	{"go.mod", func(p, rel string) (string, error) { return parseGoMod(p, rel) }},
	{"package.json", func(p, rel string) (string, error) { return parsePackageJSON(p, rel) }},
	{"pom.xml", func(p, rel string) (string, error) { return parsePomXML(p, rel) }},
	{"pyproject.toml", func(p, rel string) (string, error) { return parsePyprojectTOML(p, rel) }},
	{"requirements.txt", func(p, rel string) (string, error) { return parseRequirementsTXT(p, rel) }},
}

// handlerByName provides O(1) lookup for manifest parsers.
var handlerByName = func() map[string]func(absPath, relPath string) (string, error) {
	m := make(map[string]func(absPath, relPath string) (string, error), len(manifestHandlers))
	for _, h := range manifestHandlers {
		m[h.name] = h.parseFn
	}
	return m
}()

// ManifestEntry holds the display line for one detected manifest.
type ManifestEntry struct {
	relPath string // repo-relative path (e.g. "atomic/go.mod")
	line    string
}

// ScanManifests detects and extracts key fields from known manifests anywhere
// in the repo (not just the root). Uses enumerateFiles as the source of truth
// so that git-tracked manifests in subdirectories (e.g. atomic/go.mod) appear.
// Returns a sorted (by relPath) multi-line string, one bullet per manifest.
func ScanManifests(root string) (string, error) {
	files, err := enumerateFiles(root)
	if err != nil {
		return "", err
	}

	var entries []ManifestEntry
	for _, rel := range files {
		base := filepath.Base(filepath.ToSlash(rel))
		parseFn, ok := handlerByName[base]
		if !ok {
			continue
		}
		absPath := filepath.Join(root, filepath.FromSlash(rel))
		line, err := parseFn(absPath, rel)
		if err != nil || line == "" {
			continue
		}
		entries = append(entries, ManifestEntry{relPath: rel, line: line})
	}

	// Sort by repo-relative path for deterministic output.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].relPath < entries[j].relPath
	})

	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, "- "+e.line)
	}
	return strings.Join(lines, "\n"), nil
}

// parsePackageJSON extracts name, version, and script names.
func parsePackageJSON(path, rel string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var pkg struct {
		Name    string            `json:"name"`
		Version string            `json:"version"`
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", err
	}

	var parts []string
	if pkg.Name != "" {
		parts = append(parts, "name="+pkg.Name)
	}
	if pkg.Version != "" {
		parts = append(parts, "version="+pkg.Version)
	}
	if len(pkg.Scripts) > 0 {
		keys := make([]string, 0, len(pkg.Scripts))
		for k := range pkg.Scripts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts = append(parts, "scripts=["+strings.Join(keys, ", ")+"]")
	}
	return rel + ": " + strings.Join(parts, ", "), nil
}

// parseGoMod extracts module path and Go version using simple line scanning.
func parseGoMod(path, rel string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var module, goVer string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "module ") && module == "" {
			module = strings.TrimPrefix(line, "module ")
		}
		if strings.HasPrefix(line, "go ") && goVer == "" {
			goVer = strings.TrimPrefix(line, "go ")
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}

	var parts []string
	if module != "" {
		parts = append(parts, "module="+module)
	}
	if goVer != "" {
		parts = append(parts, "go="+goVer)
	}
	return rel + ": " + strings.Join(parts, ", "), nil
}

// parseCargoTOML extracts package name and version using line regex.
func parseCargoTOML(path, rel string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var name, ver string
	inPackage := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "[package]" {
			inPackage = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			inPackage = false
		}
		if inPackage {
			if strings.HasPrefix(line, "name") && name == "" {
				name = extractTOMLValue(line)
			}
			if strings.HasPrefix(line, "version") && ver == "" {
				ver = extractTOMLValue(line)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}

	var parts []string
	if name != "" {
		parts = append(parts, "name="+name)
	}
	if ver != "" {
		parts = append(parts, "version="+ver)
	}
	return rel + ": " + strings.Join(parts, ", "), nil
}

// parsePyprojectTOML extracts project name and version.
// Tries [project] table first, falls back to [tool.poetry].
func parsePyprojectTOML(path, rel string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	type tableEntry struct {
		name string
		ver  string
	}
	tables := map[string]*tableEntry{
		"[project]":     {},
		"[tool.poetry]": {},
	}

	var currentTable string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if _, ok := tables[line]; ok {
			currentTable = line
			continue
		}
		if strings.HasPrefix(line, "[") {
			currentTable = ""
		}
		if currentTable != "" {
			te := tables[currentTable]
			if strings.HasPrefix(line, "name") && te.name == "" {
				te.name = extractTOMLValue(line)
			}
			if strings.HasPrefix(line, "version") && te.ver == "" {
				te.ver = extractTOMLValue(line)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}

	// Prefer [project] over [tool.poetry].
	var name, ver string
	if te := tables["[project]"]; te.name != "" || te.ver != "" {
		name, ver = te.name, te.ver
	} else if te := tables["[tool.poetry]"]; te.name != "" || te.ver != "" {
		name, ver = te.name, te.ver
	}

	var parts []string
	if name != "" {
		parts = append(parts, "name="+name)
	}
	if ver != "" {
		parts = append(parts, "version="+ver)
	}
	return rel + ": " + strings.Join(parts, ", "), nil
}

// parseRequirementsTXT counts pinned packages (non-comment, non-blank lines).
func parseRequirementsTXT(path, rel string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	count := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return rel + ": packages=" + strconv.Itoa(count), nil
}

// parseGemfile counts declared gems (lines starting with "gem ").
func parseGemfile(path, rel string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	count := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "gem ") || strings.HasPrefix(line, "gem\t") {
			count++
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return rel + ": gems=" + strconv.Itoa(count), nil
}

// parseComposerJSON extracts name and script names.
// Scripts values may be strings or arrays, so we decode into json.RawMessage
// and extract keys only.
func parseComposerJSON(path, rel string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var pkg struct {
		Name    string                     `json:"name"`
		Scripts map[string]json.RawMessage `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", err
	}

	var parts []string
	if pkg.Name != "" {
		parts = append(parts, "name="+pkg.Name)
	}
	if len(pkg.Scripts) > 0 {
		keys := make([]string, 0, len(pkg.Scripts))
		for k := range pkg.Scripts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts = append(parts, "scripts=["+strings.Join(keys, ", ")+"]")
	}
	return rel + ": " + strings.Join(parts, ", "), nil
}

// parsePomXML extracts artifactId and version using simple string search.
func parsePomXML(path, rel string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)

	artifactID := extractXMLTag(content, "artifactId")
	ver := extractXMLTag(content, "version")

	var parts []string
	if artifactID != "" {
		parts = append(parts, "artifactId="+artifactID)
	}
	if ver != "" {
		parts = append(parts, "version="+ver)
	}
	return rel + ": " + strings.Join(parts, ", "), nil
}

// extractXMLTag returns the first text content of <tag>...</tag> using simple string search.
func extractXMLTag(content, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(content, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(content[start:], close)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(content[start : start+end])
}

// extractTOMLValue parses a TOML key = "value" or key = value line.
func extractTOMLValue(line string) string {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return ""
	}
	val := strings.TrimSpace(line[idx+1:])
	// Strip surrounding quotes.
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		return val[1 : len(val)-1]
	}
	return val
}
