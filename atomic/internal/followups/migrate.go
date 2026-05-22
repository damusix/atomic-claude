package followups

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/damusix/atomic-claude/atomic/internal/ids"
	"gopkg.in/yaml.v3"
)

const (
	// legacyRelPath is the path of the legacy flat file relative to repo root.
	legacyRelPath = ".claude/project/followups.md"
	// folderRelPath is the path of the new folder relative to repo root.
	folderRelPath = ".claude/project/followups"
)

// ErrMigrateRefused is returned when migration is refused (zero entries, id collision).
var ErrMigrateRefused = fmt.Errorf("followups migrate: refused")

// legacyBlock holds a parsed H3 block from the legacy followups.md.
type legacyBlock struct {
	header       string // raw H3 header text (after "### "), closed marker stripped
	bucket       string // enclosing bucket name (e.g. "🟡 risks")
	body         string // text body (excluding Origin paragraph)
	origin       string // extracted Origin paragraph
	closed       bool   // true when *(closed ...)* appears in header
	closedMarker string // the full "*(closed YYYY-MM-DD ...)*" token
}

// bucketRe matches H2 section headers (bucket lines).
var bucketRe = regexp.MustCompile(`^##\s+(.+)$`)

// h3Re matches H3 entry headers.
var h3Re = regexp.MustCompile(`^###\s+(.+)$`)

// closedRe matches the *(closed ...)* marker in a H3 header.
var closedRe = regexp.MustCompile(`\*\(closed[^)]*\)\*`)

// isoDateRe matches the first YYYY-MM-DD pattern in a string.
var isoDateRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// backtickedPathLineRe matches a line that is just a backtick-enclosed path:lines ref.
var backtickedPathLineRe = regexp.MustCompile("^`([^`]+)`$")

// parseLegacyBlocks splits a legacy followups.md into individual H3 blocks.
func parseLegacyBlocks(content string) ([]legacyBlock, error) {
	lines := strings.Split(content, "\n")
	var blocks []legacyBlock
	currentBucket := ""
	var currentHeader string
	var currentLines []string
	inBlock := false

	flush := func() {
		if !inBlock {
			return
		}
		raw := strings.Join(currentLines, "\n")
		b := legacyBlock{
			header: currentHeader,
			bucket: currentBucket,
		}
		// Detect closed marker in header.
		if m := closedRe.FindString(b.header); m != "" {
			b.closed = true
			b.closedMarker = m
			b.header = strings.TrimSpace(closedRe.ReplaceAllString(b.header, ""))
		}
		b.origin = extractOrigin(raw)
		b.body = stripOriginParagraph(raw)
		blocks = append(blocks, b)
		currentLines = nil
		inBlock = false
	}

	for _, line := range lines {
		if m := bucketRe.FindStringSubmatch(line); m != nil {
			flush()
			currentBucket = strings.TrimSpace(m[1])
			continue
		}
		if m := h3Re.FindStringSubmatch(line); m != nil {
			flush()
			currentHeader = strings.TrimSpace(m[1])
			inBlock = true
			continue
		}
		if inBlock {
			currentLines = append(currentLines, line)
		}
	}
	flush()

	return blocks, nil
}

// severityFromBucket maps a bucket label to a Severity value.
func severityFromBucket(bucket string) Severity {
	b := strings.TrimPrefix(bucket, "## ")
	b = strings.TrimSpace(b)
	switch {
	case strings.Contains(b, "risk"):
		return SeverityRisk
	case strings.Contains(b, "nit"):
		return SeverityNit
	case strings.Contains(b, "question"):
		return SeverityQuestion
	}
	return SeverityRisk
}

// idFromHeader derives the entry id and detects closure.
// If the header starts with a bare "F-N" pattern (no slug prefix), synthesize an id
// from the bucket name + first 4-5 words of title.
// The caller may pass the original header (with or without closed marker) —
// this function handles both.
func idFromHeader(header, bucket string) (id string, closed bool) {
	// Handle closed marker if still present.
	if m := closedRe.FindString(header); m != "" {
		closed = true
		header = strings.TrimSpace(closedRe.ReplaceAllString(header, ""))
	}

	rawID, _ := splitHeaderIDTitle(header)
	if rawID == "" {
		rawID = strings.TrimSpace(header)
	}

	// Bare F-N pattern: just "F-N" with no prefix slug.
	if isBareEntry(rawID) {
		bucketSlug := bucketToSlug(bucket)
		_, titlePart := splitHeaderIDTitle(header)
		wordSlug := firstWordsSlug(titlePart, 4)
		synthesized := bucketSlug + "-" + wordSlug + "-" + rawID
		return normalizeID(synthesized), closed
	}

	return normalizeID(rawID), closed
}

// bareEntryRe matches bare "F-N" identifiers.
var bareEntryRe = regexp.MustCompile(`^F-\d+$`)

func isBareEntry(rawID string) bool {
	return bareEntryRe.MatchString(rawID)
}

// bucketToSlug converts a bucket display name to a short ASCII slug.
func bucketToSlug(bucket string) string {
	b := strings.TrimPrefix(bucket, "## ")
	b = strings.TrimSpace(b)
	var sb strings.Builder
	for _, r := range b {
		if r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ') {
			sb.WriteRune(r)
		}
	}
	words := strings.Fields(sb.String())
	if len(words) == 0 {
		return "misc"
	}
	return strings.ToLower(words[0])
}

// splitHeaderIDTitle splits a header line into (rawID, title) around the first separator.
func splitHeaderIDTitle(header string) (rawID, title string) {
	for _, sep := range []string{" — ", " -- ", " - "} {
		if idx := strings.Index(header, sep); idx >= 0 {
			return strings.TrimSpace(header[:idx]), strings.TrimSpace(header[idx+len(sep):])
		}
	}
	return "", strings.TrimSpace(header)
}

// firstWordsSlug takes the first n words of s and returns a kebab-case slug.
func firstWordsSlug(s string, n int) string {
	clean := strings.Map(func(r rune) rune {
		if r == '`' || r == '\'' || r == '"' {
			return -1
		}
		return r
	}, s)
	words := strings.Fields(clean)
	if len(words) > n {
		words = words[:n]
	}
	return ids.Slug(strings.Join(words, " "))
}

// normalizeID forces an id to lowercase kebab-case.
func normalizeID(s string) string {
	return ids.Slug(strings.ToLower(s))
}

// extractOrigin finds the "Origin:" paragraph in a block body.
func extractOrigin(body string) string {
	lines := strings.Split(body, "\n")
	var result []string
	inOrigin := false
	for _, line := range lines {
		if inOrigin {
			if strings.TrimSpace(line) == "" {
				break
			}
			result = append(result, line)
			continue
		}
		if strings.HasPrefix(line, "Origin:") {
			text := strings.TrimSpace(strings.TrimPrefix(line, "Origin:"))
			inOrigin = true
			if text != "" {
				result = append(result, text)
			}
		}
	}
	return strings.Join(result, "\n")
}

// stripOriginParagraph removes the "Origin: ..." paragraph from body.
func stripOriginParagraph(body string) string {
	lines := strings.Split(body, "\n")
	var result []string
	skip := false
	for _, line := range lines {
		if strings.HasPrefix(line, "Origin:") {
			skip = true
			continue
		}
		if skip {
			if strings.TrimSpace(line) == "" {
				skip = false
				continue
			}
		}
		result = append(result, line)
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}

// extractOriginDate returns the first YYYY-MM-DD found in origin, or "" if none.
func extractOriginDate(origin string) string {
	return isoDateRe.FindString(origin)
}

// extractFileRef returns a file:line reference from the first non-empty line of body
// if it matches a backtick-enclosed path.
func extractFileRef(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if m := backtickedPathLineRe.FindStringSubmatch(trimmed); m != nil {
			return m[1]
		}
		break
	}
	return ""
}

// Migrate migrates the legacy .claude/project/followups.md to the folder layout.
// Idempotent: if followups/ exists and followups.md is absent → no-op.
// Refuses if zero entries are parsed or an id collision is detected.
func Migrate(repoRoot string, today time.Time) error {
	srcPath := filepath.Join(repoRoot, legacyRelPath)
	dstDir := filepath.Join(repoRoot, folderRelPath)

	// Idempotency: folder exists and source absent → no-op.
	if _, err := os.Stat(dstDir); err == nil {
		if _, err2 := os.Stat(srcPath); os.IsNotExist(err2) {
			return nil
		}
	}

	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("followups migrate: read %q: %w", srcPath, err)
	}

	blocks, err := parseLegacyBlocks(string(raw))
	if err != nil {
		return fmt.Errorf("followups migrate: parse: %w", err)
	}

	if len(blocks) == 0 {
		return fmt.Errorf("%w: zero entries parsed from %q", ErrMigrateRefused, srcPath)
	}

	// Resolve ids and detect collisions.
	type resolvedBlock struct {
		block  legacyBlock
		id     string
		closed bool
		title  string
	}

	idSeen := map[string]bool{}
	var resolved []resolvedBlock
	for _, b := range blocks {
		id, closed := idFromHeader(b.header, b.bucket)
		if b.closed {
			closed = true
		}
		if id == "" {
			continue
		}
		if idSeen[id] {
			return fmt.Errorf("%w: id collision on %q after synthesis", ErrMigrateRefused, id)
		}
		idSeen[id] = true
		_, title := splitHeaderIDTitle(b.header)
		if title == "" {
			title = b.header
		}
		resolved = append(resolved, resolvedBlock{block: b, id: id, closed: closed, title: title})
	}

	if len(resolved) == 0 {
		return fmt.Errorf("%w: zero entries parsed from %q", ErrMigrateRefused, srcPath)
	}

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("followups migrate: mkdir %q: %w", dstDir, err)
	}

	todayStr := today.Format("2006-01-02")
	closedPath := filepath.Join(dstDir, "CLOSED.md")

	for _, rb := range resolved {
		b := rb.block

		created := extractOriginDate(b.origin)
		if created == "" {
			created = todayStr
		}
		if _, err := time.Parse("2006-01-02", created); err != nil {
			created = todayStr
		}

		createdTime, _ := time.Parse("2006-01-02", created)
		reviewBy := createdTime.AddDate(0, 0, 60).Format("2006-01-02")
		severity := severityFromBucket(b.bucket)
		fileRef := extractFileRef(b.body)

		if rb.closed {
			marker := b.closedMarker
			if marker == "" {
				marker = "*(closed " + todayStr + ")*"
			}
			if err := AppendClosed(closedPath, rb.id, rb.title, marker, today); err != nil {
				return fmt.Errorf("followups migrate: append closed %q: %w", rb.id, err)
			}
			continue
		}

		content, err := buildEntryFile(rb.id, rb.title, created, reviewBy, string(severity), b.origin, fileRef, b.body)
		if err != nil {
			return fmt.Errorf("followups migrate: build entry %q: %w", rb.id, err)
		}
		entryPath := filepath.Join(dstDir, rb.id+".md")
		if err := os.WriteFile(entryPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("followups migrate: write %q: %w", entryPath, err)
		}
	}

	// Render INDEX.md.
	entries, err := LoadEntries(dstDir)
	if err != nil {
		return fmt.Errorf("followups migrate: load entries: %w", err)
	}
	indexContent := Render(entries, today)
	indexPath := filepath.Join(dstDir, "INDEX.md")
	if err := os.WriteFile(indexPath, []byte(indexContent), 0o644); err != nil {
		return fmt.Errorf("followups migrate: write INDEX.md: %w", err)
	}

	// Rewrite @-ref (silent no-op if not found).
	_ = rewriteAtRef(repoRoot)

	// Delete the legacy file.
	if err := os.Remove(srcPath); err != nil {
		return fmt.Errorf("followups migrate: remove %q: %w", srcPath, err)
	}

	return nil
}

// entryYAML is the YAML-marshallable shape for entry frontmatter.
type entryYAML struct {
	ID       string `yaml:"id"`
	Title    string `yaml:"title"`
	Created  string `yaml:"created"`
	Origin   string `yaml:"origin"`
	Severity string `yaml:"severity"`
	ReviewBy string `yaml:"review_by"`
	Status   string `yaml:"status"`
	File     string `yaml:"file,omitempty"`
}

// buildEntryFile constructs the YAML frontmatter + body for an entry file.
func buildEntryFile(id, title, created, reviewBy, severity, origin, fileRef, body string) (string, error) {
	data := entryYAML{
		ID:       id,
		Title:    title,
		Created:  created,
		Origin:   origin + "\n",
		Severity: severity,
		ReviewBy: reviewBy,
		Status:   string(StatusOpen),
		File:     fileRef,
	}

	yamlBytes, err := yaml.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal frontmatter: %w", err)
	}

	cleanBody := cleanupBodyFirstLine(body, fileRef)
	var bodySection string
	if cleanBody != "" {
		bodySection = "\n" + cleanBody + "\n"
	} else {
		bodySection = "\n"
	}

	return "---\n" + string(yamlBytes) + "---\n" + bodySection, nil
}

// cleanupBodyFirstLine removes the file-ref first line from body if it matches fileRef.
func cleanupBodyFirstLine(body, fileRef string) string {
	if fileRef == "" {
		return strings.TrimSpace(body)
	}
	lines := strings.Split(body, "\n")
	start := 0
	for start < len(lines) {
		trimmed := strings.TrimSpace(lines[start])
		if trimmed == "" {
			start++
			continue
		}
		if m := backtickedPathLineRe.FindStringSubmatch(trimmed); m != nil && m[1] == fileRef {
			start++
			break
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines[start:], "\n"))
}

// rewriteAtRef rewrites the @-ref from followups.md to followups/INDEX.md in the
// first auto-loaded instructions file found.
func rewriteAtRef(repoRoot string) error {
	candidates := []string{
		"claude.local.md",
		"CLAUDE.local.md",
		"claude.md",
		"CLAUDE.md",
	}
	const oldRef = "@.claude/project/followups.md"
	const newRef = "@.claude/project/followups/INDEX.md"

	for _, name := range candidates {
		path := filepath.Join(repoRoot, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(raw)
		if !strings.Contains(content, oldRef) {
			continue
		}
		updated := strings.ReplaceAll(content, oldRef, newRef)
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			return fmt.Errorf("rewrite @-ref in %q: %w", path, err)
		}
		return nil
	}
	return nil
}
