package signals

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ManifestEntry holds the display line for one detected manifest.
type ManifestEntry struct {
	filename string
	line     string
}

// ScanManifests detects and extracts key fields from known manifests in root.
// Returns a sorted (by filename) multi-line string, one bullet per manifest.
func ScanManifests(root string) (string, error) {
	var entries []ManifestEntry

	handlers := []struct {
		name    string
		parseFn func(path string) (string, error)
	}{
		{"Cargo.toml", parseCargoTOML},
		{"Gemfile", parseGemfile},
		{"composer.json", parseComposerJSON},
		{"go.mod", parseGoMod},
		{"package.json", parsePackageJSON},
		{"pom.xml", parsePomXML},
		{"pyproject.toml", parsePyprojectTOML},
		{"requirements.txt", parseRequirementsTXT},
	}

	for _, h := range handlers {
		path := filepath.Join(root, h.name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		line, err := h.parseFn(path)
		if err != nil {
			// Skip manifests we can't parse cleanly.
			continue
		}
		if line != "" {
			entries = append(entries, ManifestEntry{filename: h.name, line: line})
		}
	}

	// Already sorted by handler order (alphabetical), but enforce explicitly.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].filename < entries[j].filename
	})

	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, "- "+e.line)
	}
	return strings.Join(lines, "\n"), nil
}

// parsePackageJSON extracts name, version, and script names.
func parsePackageJSON(path string) (string, error) {
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
	return "package.json: " + strings.Join(parts, ", "), nil
}

// parseGoMod extracts module path and Go version using simple line scanning.
func parseGoMod(path string) (string, error) {
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
	return "go.mod: " + strings.Join(parts, ", "), nil
}

// parseCargoTOML extracts package name and version using line regex.
func parseCargoTOML(path string) (string, error) {
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
	return "Cargo.toml: " + strings.Join(parts, ", "), nil
}

// parsePyprojectTOML extracts project name and version.
// Tries [project] table first, falls back to [tool.poetry].
func parsePyprojectTOML(path string) (string, error) {
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
	return "pyproject.toml: " + strings.Join(parts, ", "), nil
}

// parseRequirementsTXT counts pinned packages (non-comment, non-blank lines).
func parseRequirementsTXT(path string) (string, error) {
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
	return "requirements.txt: packages=" + itoa(count), nil
}

// parseGemfile counts declared gems (lines starting with "gem ").
func parseGemfile(path string) (string, error) {
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
	return "Gemfile: gems=" + itoa(count), nil
}

// parseComposerJSON extracts name and script names.
func parseComposerJSON(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var pkg struct {
		Name    string            `json:"name"`
		Scripts map[string]string `json:"scripts"`
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
	return "composer.json: " + strings.Join(parts, ", "), nil
}

// parsePomXML extracts artifactId and version using simple string search.
func parsePomXML(path string) (string, error) {
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
	return "pom.xml: " + strings.Join(parts, ", "), nil
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

// itoa is a minimal int-to-string helper to avoid importing strconv elsewhere.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
