// Package wiki implements the deterministic core of the atomic wiki feature:
// repo discovery, classification, scaffold creation, and idempotent
// <wiki-scan> block writes.
//
// CP1 scope: pure package logic + tests. No CLI wiring, no <wikis> registry,
// no stale/stamp/mark-dirty/CheckStaleness. Those arrive in later checkpoints.
package wiki

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// skipDirs is the set of directory base-names that the discovery walk skips.
// Mirrors the skip set from internal/signals/tree.go plus .worktrees.
var skipDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"vendor":       true,
	".worktrees":   true,
	"tmp":          true,
	".git":         true,
}

// scanMarkerOpen is the literal prefix of the managed block open tag.
const scanMarkerOpen = "<wiki-scan"

// scanMarkerClose is the literal close tag of the managed block.
const scanMarkerClose = "</wiki-scan>"

// membersMarkerStart / membersMarkerEnd are the HTML-comment boundaries of the
// managed ## Members section. Content between the markers is replaced on each
// scan; content outside the markers (including the heading itself) is preserved.
const membersMarkerStart = "<!-- wiki-members:start -->"
const membersMarkerEnd = "<!-- wiki-members:end -->"

// Options configures a Scan run.
type Options struct {
	// Clock returns the current time. If nil, time.Now().UTC() is used.
	// Inject a fixed clock in tests to get deterministic generated dates.
	Clock func() time.Time
}

func (o Options) clock() time.Time {
	if o.Clock != nil {
		return o.Clock()
	}
	return time.Now().UTC()
}

// Member represents a discovered git repository under the scan root.
type Member struct {
	// Path is the repo path relative to root, e.g. "repoA" or "not-a-repo/repoC".
	Path string
	// Status is one of "indexed", "pending", or "summarized".
	Status string
	// SignalsPath is the absolute path to the indexed-member router file when
	// Status == "indexed". Preferred location is docs/wiki/index.md (new layout);
	// falls back to .claude/project/signals.md (legacy layout). Set by classifyMembers.
	SignalsPath string
	// SummaryPath is the value of the summary attribute when Status == "summarized".
	SummaryPath string
}

// Scan runs the full CP1 wiki operation: discover repos under root, scaffold
// wiki/, and write (or update) wiki/index.md with an idempotent <wiki-scan> block.
// It returns the classified members so callers can use them directly without a
// second filesystem walk.
//
// Collision refusal: if wiki/ already exists but index.md is absent or lacks a
// <wiki-scan> marker, Scan returns an error naming the path.
func Scan(root string, opts Options) ([]Member, error) {
	wikiDir := filepath.Join(root, "wiki")

	// --- Collision check ---
	if err := checkCollision(wikiDir); err != nil {
		return nil, err
	}

	// --- Parse existing entries from index.md (for summarized-preservation) ---
	prior, err := parsePriorEntries(filepath.Join(wikiDir, "index.md"))
	if err != nil {
		return nil, fmt.Errorf("wiki scan: parse prior entries: %w", err)
	}

	// --- Discover members ---
	rawMembers, err := discoverMembers(root, wikiDir)
	if err != nil {
		return nil, fmt.Errorf("wiki scan: discover: %w", err)
	}

	// --- Classify members ---
	classified := classifyMembers(root, wikiDir, rawMembers, prior)

	// --- Scaffold ---
	if err := scaffold(wikiDir, root); err != nil {
		return nil, fmt.Errorf("wiki scan: scaffold: %w", err)
	}

	// --- Write <wiki-scan> block ---
	indexPath := filepath.Join(wikiDir, "index.md")
	if err := writeWikiScanBlock(indexPath, root, classified, opts); err != nil {
		return nil, fmt.Errorf("wiki scan: write block: %w", err)
	}

	// --- Write ## Members section ---
	if err := writeMembersSection(indexPath, classified); err != nil {
		return nil, fmt.Errorf("wiki scan: write members section: %w", err)
	}

	return classified, nil
}

// checkCollision verifies that an existing wiki/ dir is owned by this tool.
// If wiki/ exists but index.md is absent or lacks a <wiki-scan> marker,
// returns an error naming the path.
func checkCollision(wikiDir string) error {
	if _, err := os.Lstat(wikiDir); os.IsNotExist(err) {
		// wiki/ doesn't exist yet — no collision, scaffold will create it.
		return nil
	}

	indexPath := filepath.Join(wikiDir, "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("wiki scan: collision: %s exists but index.md is absent — refusing to overwrite", wikiDir)
		}
		return fmt.Errorf("wiki scan: read index.md: %w", err)
	}

	if !strings.Contains(string(data), scanMarkerOpen) {
		return fmt.Errorf("wiki scan: collision: %s lacks a <wiki-scan> marker — refusing to overwrite", indexPath)
	}

	return nil
}

// priorEntry holds the parsed status from a previous scan block entry.
type priorEntry struct {
	status      string
	summaryAttr string // e.g. "repos/repoA.md"
}

// parsePriorEntries reads wiki/index.md (if present) and extracts the prior
// status for each repo path from the <wiki-scan> block. Used for
// summarized-preservation.
func parsePriorEntries(indexPath string) (map[string]priorEntry, error) {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	content := string(data)
	blockContent := extractBlockContent(content)
	if blockContent == "" {
		return nil, nil
	}

	entries := map[string]priorEntry{}
	for _, line := range strings.Split(blockContent, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "<repo ") {
			continue
		}
		path := attrValue(line, "path")
		status := attrValue(line, "status")
		summary := attrValue(line, "summary")
		if path != "" && status != "" {
			entries[path] = priorEntry{status: status, summaryAttr: summary}
		}
	}

	return entries, nil
}

// extractBlockContent returns the content between <wiki-scan ...> and </wiki-scan>.
// Returns empty string if the block is not present.
func extractBlockContent(content string) string {
	openIdx := strings.Index(content, scanMarkerOpen)
	if openIdx == -1 {
		return ""
	}
	// Find the end of the open tag.
	closeTagIdx := strings.Index(content[openIdx:], ">")
	if closeTagIdx == -1 {
		return ""
	}
	afterOpen := openIdx + closeTagIdx + 1

	closeIdx := strings.Index(content[afterOpen:], scanMarkerClose)
	if closeIdx == -1 {
		return ""
	}
	return content[afterOpen : afterOpen+closeIdx]
}

// attrValue extracts the value of an XML attribute from a self-closing tag line.
// e.g. attrValue(`<repo path="foo" status="indexed"/>`, "path") → "foo"
func attrValue(line, attr string) string {
	needle := attr + `="`
	idx := strings.Index(line, needle)
	if idx == -1 {
		return ""
	}
	start := idx + len(needle)
	end := strings.Index(line[start:], `"`)
	if end == -1 {
		return ""
	}
	return line[start : start+end]
}

// discoverMembers recursively walks root's children to find git repos.
// The root itself is never returned as a member.
// Returns relative paths sorted stably.
func discoverMembers(root, wikiDir string) ([]string, error) {
	var members []string

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read root dir: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		base := e.Name()
		if skipDirs[base] {
			continue
		}
		// Skip the wiki output dir itself.
		absDir := filepath.Join(root, base)
		if absDir == wikiDir {
			continue
		}

		found, err := walkForRepos(root, absDir, wikiDir)
		if err != nil {
			return nil, err
		}
		members = append(members, found...)
	}

	sort.Strings(members)
	return members, nil
}

// walkForRepos checks if dir is a git repo member. If it is, returns just that
// path (relative to root) and stops recursing. If not, recurses into children.
func walkForRepos(root, dir, wikiDir string) ([]string, error) {
	// Is dir itself a git repo?
	if isGitMember(dir) {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return nil, err
		}
		return []string{rel}, nil
	}

	// Not a member — descend into children.
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Unreadable directories are silently skipped.
		return nil, nil
	}

	var found []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		base := e.Name()
		if skipDirs[base] {
			continue
		}
		child := filepath.Join(dir, base)
		if child == wikiDir {
			continue
		}
		sub, err := walkForRepos(root, child, wikiDir)
		if err != nil {
			return nil, err
		}
		found = append(found, sub...)
	}
	return found, nil
}

// isGitMember reports whether dir has a .git entry (file or directory).
// This mirrors the pattern from internal/validate/repo.go (worktree-aware).
func isGitMember(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// fileExists reports whether the named file exists and is accessible.
func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// classifyMembers derives the status for each member.
//
// Classification rules:
//  1. If prior status was "summarized" AND the summary file still exists → keep "summarized".
//  2. If docs/wiki/index.md exists (new layout) → "indexed" with SignalsPath pointing there.
//     Else if .claude/project/signals.md exists (legacy layout) → "indexed" with SignalsPath
//     pointing there. Either layout counts; new layout takes precedence. A leftover summary
//     does not demote a graduated repo.
//  3. If a summary exists on disk under wiki/repos/ (repos/<name>.md or
//     repos/<name>/ with at least one .md) → "summarized". This makes the
//     status reachable on first derivation — /refresh-wiki writes summaries
//     after the initial scan, so the re-scan must discover them.
//  4. Otherwise → "pending".
func classifyMembers(root, wikiDir string, members []string, prior map[string]priorEntry) []Member {
	result := make([]Member, 0, len(members))

	for _, rel := range members {
		absRepo := filepath.Join(root, rel)

		// Check summarized-preservation.
		if pe, ok := prior[rel]; ok && pe.status == "summarized" && pe.summaryAttr != "" {
			summaryAbs := filepath.Join(wikiDir, pe.summaryAttr)
			if _, err := os.Lstat(summaryAbs); err == nil {
				result = append(result, Member{
					Path:        rel,
					Status:      "summarized",
					SummaryPath: pe.summaryAttr,
				})
				continue
			}
			// Summary file gone — fall through to re-derive.
		}

		// Derive from index presence — migration-aware dual-layout detection.
		// New layout (docs/wiki/index.md) takes precedence; legacy (.claude/project/signals.md)
		// is accepted for un-migrated repos so existing users don't regress.
		if indexAbs := filepath.Join(absRepo, "docs", "wiki", "index.md"); fileExists(indexAbs) {
			result = append(result, Member{
				Path:        rel,
				Status:      "indexed",
				SignalsPath: indexAbs,
			})
			continue
		}
		if signalsAbs := filepath.Join(absRepo, ".claude", "project", "signals.md"); fileExists(signalsAbs) {
			result = append(result, Member{
				Path:        rel,
				Status:      "indexed",
				SignalsPath: signalsAbs,
			})
			continue
		}

		// Derive from a summary on disk.
		if summaryRel := discoverSummary(wikiDir, rel); summaryRel != "" {
			result = append(result, Member{
				Path:        rel,
				Status:      "summarized",
				SummaryPath: summaryRel,
			})
			continue
		}

		result = append(result, Member{
			Path:   rel,
			Status: "pending",
		})
	}

	return result
}

// discoverSummary checks the wiki/repos/ directory for a summary belonging to
// member rel. Summary files are named by the member's base name (the same
// convention memberLinkTarget and /refresh-wiki use): repos/<name>.md for a
// single-file summary, or repos/<name>/ containing at least one .md for a
// domain-split summary. Returns the wiki-relative summary path, or "" when no
// summary exists.
func discoverSummary(wikiDir, rel string) string {
	name := filepath.Base(rel)

	fileForm := filepath.Join(wikiDir, "repos", name+".md")
	if _, err := os.Lstat(fileForm); err == nil {
		return "repos/" + name + ".md"
	}

	dirForm := filepath.Join(wikiDir, "repos", name)
	entries, err := os.ReadDir(dirForm)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			return "repos/" + name + "/"
		}
	}
	return ""
}

// scaffold creates the wiki directory structure:
//   - wiki/index.md (only created if absent — writeWikiScanBlock handles content)
//   - wiki/README.md
//   - wiki/repos/
//   - wiki/concerns/
//   - wiki/.gitignore (ignoring .dirty)
//   - runs git init in wiki/ if not already a git repo
func scaffold(wikiDir, root string) error {
	// Create all subdirs.
	for _, sub := range []string{wikiDir, filepath.Join(wikiDir, "repos"), filepath.Join(wikiDir, "concerns")} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}

	// .gitignore — ignores the .dirty marker.
	gitignorePath := filepath.Join(wikiDir, ".gitignore")
	if _, err := os.Lstat(gitignorePath); os.IsNotExist(err) {
		if err := os.WriteFile(gitignorePath, []byte(".dirty\n"), 0o644); err != nil {
			return fmt.Errorf("write .gitignore: %w", err)
		}
	}

	// README.md — boilerplate.
	readmePath := filepath.Join(wikiDir, "README.md")
	if _, err := os.Lstat(readmePath); os.IsNotExist(err) {
		readme := buildREADME(root)
		if err := os.WriteFile(readmePath, []byte(readme), 0o644); err != nil {
			return fmt.Errorf("write README.md: %w", err)
		}
	}

	// git init — skip if already a git repo.
	if !isGitMember(wikiDir) {
		var stderr strings.Builder
		cmd := exec.Command("git", "init", wikiDir)
		cmd.Stdout = nil
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git init wiki: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
	}

	return nil
}

// buildREADME produces the README.md content for a wiki directory.
func buildREADME(root string) string {
	var sb strings.Builder
	sb.WriteString("# Project wiki\n\n")
	sb.WriteString("Cross-repository knowledge layer generated by `atomic wiki scan`.\n\n")
	fmt.Fprintf(&sb, "**Realm root:** `%s`\n\n", root)
	sb.WriteString("## How to regenerate\n\n")
	sb.WriteString("```sh\natomic wiki scan\n```\n\n")
	sb.WriteString("Or run `/refresh-wiki` in Claude Code.\n\n")
	sb.WriteString("## Structure\n\n")
	sb.WriteString("- `index.md` — member registry with `<wiki-scan>` block + narrative\n")
	sb.WriteString("- `repos/` — per-repo summaries (written by `/refresh-wiki`)\n")
	sb.WriteString("- `concerns/` — cross-cutting concern documents\n")
	return sb.String()
}

// writeWikiScanBlock writes the <wiki-scan> block into wiki/index.md.
// If index.md does not exist, creates it with a minimal narrative stub.
// If it exists and already has a block, replaces the block in-place.
// Content outside the block is preserved byte-for-byte.
func writeWikiScanBlock(indexPath, root string, members []Member, opts Options) error {
	date := opts.clock().Format("2006-01-02")
	block := buildScanBlock(root, date, members)

	// Read existing content.
	existing, err := os.ReadFile(indexPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read index.md: %w", err)
	}

	var newContent string
	if os.IsNotExist(err) || len(existing) == 0 {
		// Create fresh with block + stub narrative.
		newContent = block + "\n" + defaultNarrative()
	} else {
		newContent = rewriteScanBlock(string(existing), block)
	}

	return os.WriteFile(indexPath, []byte(newContent), 0o644)
}

// buildScanBlock produces the full <wiki-scan ...> … </wiki-scan> block string.
func buildScanBlock(root, date string, members []Member) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<wiki-scan root=%q generated=%q>\n", root, date)
	for _, m := range members {
		sb.WriteString(repoTag(m))
		sb.WriteString("\n")
	}
	sb.WriteString("</wiki-scan>")
	return sb.String()
}

// repoTag produces a single self-closing <repo .../> tag for a member.
func repoTag(m Member) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<repo path=%q status=%q`, m.Path, m.Status)
	if m.Status == "indexed" && m.SignalsPath != "" {
		fmt.Fprintf(&sb, ` signals=%q`, m.SignalsPath)
	}
	if m.Status == "summarized" && m.SummaryPath != "" {
		fmt.Fprintf(&sb, ` summary=%q`, m.SummaryPath)
	}
	sb.WriteString("/>")
	return sb.String()
}

// defaultNarrative is the stub below the block when creating a fresh index.md.
func defaultNarrative() string {
	return "\n## Realm overview\n\n<!-- Add narrative context about this realm here. -->\n"
}

// writeMembersSection writes (or replaces) the managed ## Members section in
// indexPath. The section uses HTML-comment boundary markers so it can be
// re-spliced idempotently while narrative outside it is preserved byte-for-byte.
//
// Link targets are relative to the directory containing indexPath (wiki/).
//   - indexed (new layout)  → [<repo>](../<repo>/docs/wiki/index.md)
//   - indexed (legacy layout) → [<repo>](../<repo>/.claude/project/signals.md)
//   - summarized → [<repo>](repos/<repo>.md)
//   - pending  → [<repo>](../<repo>/)
func writeMembersSection(indexPath string, members []Member) error {
	indexDir := filepath.Dir(indexPath)

	section := buildMembersSection(indexDir, members)

	existing, err := os.ReadFile(indexPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read index.md: %w", err)
	}

	var newContent string
	if os.IsNotExist(err) || len(existing) == 0 {
		newContent = section
	} else {
		newContent = rewriteMembersSection(string(existing), section)
	}

	return os.WriteFile(indexPath, []byte(newContent), 0o644)
}

// deriveSummaryFilePath returns the absolute path to the primary summary file
// for a member, given the wiki index directory and the member's metadata.
// Returns "" when no summary file can be determined.
func deriveSummaryFilePath(indexDir string, m Member) string {
	switch m.Status {
	case "summarized":
		if m.SummaryPath == "" {
			return ""
		}
		// SummaryPath is relative to the wiki dir (e.g. "repos/repoA.md" or
		// "repos/repoA/"). For a dir form we read the index.md inside it.
		abs := filepath.Join(indexDir, m.SummaryPath)
		info, err := os.Lstat(abs)
		if err != nil {
			return ""
		}
		if info.IsDir() {
			// Domain-split: use index.md if present, else first .md file.
			candidate := filepath.Join(abs, "index.md")
			if _, err := os.Lstat(candidate); err == nil {
				return candidate
			}
			entries, err := os.ReadDir(abs)
			if err != nil {
				return ""
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
					return filepath.Join(abs, e.Name())
				}
			}
			return ""
		}
		return abs
	case "indexed":
		// No dedicated summary page — indexed repos link to signals.md which
		// has no consumer-friendly description. Return "".
		return ""
	default:
		return ""
	}
}

// DeriveMemberDescription reads a summary file and extracts a short one-line
// description suitable for use in an OKF §6 Members listing entry.
//
// Resolution order:
//  1. frontmatter "description:" key — returned verbatim (trimmed).
//  2. First non-structural prose line from the body — used as the description.
//     Skipped: blank lines, headings (#), blockquotes (>), HTML/tag lines (<),
//     table rows (|), and list items (-, *, +, N.). For each candidate line,
//     inline markdown links [text](url) are reduced to their visible text,
//     backtick inline-code and emphasis markers are stripped, and whitespace
//     collapsed. The normalized line is rejected if it still contains a " | "
//     nav separator OR has fewer than 15 letter characters. A line containing
//     a single inline link (e.g. "Alpha depends on [Beta](x) for retries.")
//     survives after normalization because the remaining text is clean prose.
//  3. Empty string — emitted when neither source yields text (link-only is
//     valid per OKF §6 SHOULD semantics).
//
// The result is always a single line (no embedded newlines) and is truncated
// to at most 120 characters. Missing or unreadable files return "".
func DeriveMemberDescription(summaryFilePath string) string {
	data, err := os.ReadFile(summaryFilePath)
	if err != nil {
		return ""
	}

	meta, body, err := frontmatter.Parse(string(data))
	if err == nil && meta != nil {
		if v, ok := meta["description"]; ok {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
				return truncate(s, 120)
			}
		}
	}

	// Fall back to first prose line in the body.
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Skip structural / non-prose lines.
		if strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, ">") ||
			strings.HasPrefix(line, "<") ||
			strings.HasPrefix(line, "|") {
			continue
		}
		// Skip list items: -, *, + or N. (ordered list).
		if isListItem(line) {
			continue
		}
		// Normalize: strip markdown links to their visible text, strip
		// backtick inline-code spans and emphasis markers, collapse whitespace.
		normalized := normalizeLine(line)
		// Reject nav/structural lines: those with a " | " separator or with
		// too few actual letter characters to form a sentence.
		if strings.Contains(normalized, " | ") {
			continue
		}
		if letterCount(normalized) < 15 {
			continue
		}
		return truncate(normalized, 120)
	}
	return ""
}

// isListItem reports whether line is a markdown list item: unordered (-, *, +)
// or ordered (one-or-more digits followed by ". ").
func isListItem(line string) bool {
	if len(line) == 0 {
		return false
	}
	switch line[0] {
	case '-', '*', '+':
		// Must be followed by a space (or be a lone marker) to be a list item,
		// not an em-dash or horizontal rule.
		return len(line) == 1 || line[1] == ' '
	}
	// Ordered list: digits followed by ". "
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	return i > 0 && i < len(line) && line[i] == '.' && (i+1 == len(line) || line[i+1] == ' ')
}

// normalizeLine reduces a markdown inline line to plain text suitable for
// prose detection:
//   - [visible text](url) → visible text
//   - `code` → code  (backtick inline-code span stripped)
//   - *em*, **strong**, _em_, __strong__ → inner text
//   - Collapses internal whitespace runs to a single space, trims edges.
func normalizeLine(line string) string {
	// Strip markdown links: [text](url) → text.
	// We scan character-by-character to handle multiple links per line.
	var sb strings.Builder
	i := 0
	for i < len(line) {
		if line[i] == '[' {
			// Look for ](…) following this bracket.
			closeText := strings.Index(line[i+1:], "]")
			if closeText >= 0 {
				afterClose := i + 1 + closeText + 1 // index of ']'+1
				if afterClose < len(line) && line[afterClose] == '(' {
					closeURL := strings.Index(line[afterClose+1:], ")")
					if closeURL >= 0 {
						// Emit visible text only.
						sb.WriteString(line[i+1 : i+1+closeText])
						i = afterClose + 1 + closeURL + 1
						continue
					}
				}
			}
		}
		sb.WriteByte(line[i])
		i++
	}
	out := sb.String()

	// Strip backtick inline-code spans: `…` → inner text.
	out = stripDelimited(out, '`', '`')
	// Strip emphasis: **…** / __…__ → inner text.
	out = strings.ReplaceAll(out, "**", "")
	out = strings.ReplaceAll(out, "__", "")
	// Strip single * and _ used for emphasis only when they appear paired —
	// a simple approach: remove lone * and _ characters flanked by word chars.
	// Rather than complex regex, just remove remaining * and _ after double
	// forms are gone.
	out = strings.ReplaceAll(out, "*", "")
	out = strings.ReplaceAll(out, "_", "")

	// Collapse whitespace.
	fields := strings.Fields(out)
	return strings.Join(fields, " ")
}

// stripDelimited removes all occurrences of text delimited by open/close byte
// (same byte for both, e.g. backtick), replacing the delimited span with the
// inner content.
func stripDelimited(s string, open, close byte) string {
	var sb strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == open {
			j := strings.IndexByte(s[i+1:], close)
			if j >= 0 {
				// Emit inner content without the delimiters.
				sb.WriteString(s[i+1 : i+1+j])
				i = i + 1 + j + 1
				continue
			}
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

// letterCount counts the number of Unicode letters in s.
func letterCount(s string) int {
	n := 0
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			n++
		}
	}
	return n
}

// truncate returns s truncated to at most n UTF-8 characters. If s is longer,
// it returns the first n runes (no ellipsis — the caller adds context).
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// buildMembersSection produces the full ## Members managed section string,
// bounded by the HTML-comment markers. Each entry follows the OKF §6 listing
// form: "- [Title](url) - description" when a description is derivable, or
// "- [Title](url)" when no description can be found (link-only is valid per
// §6 SHOULD semantics).
func buildMembersSection(indexDir string, members []Member) string {
	var sb strings.Builder
	sb.WriteString("## Members\n\n")
	sb.WriteString(membersMarkerStart)
	sb.WriteString("\n")
	for _, m := range members {
		name := filepath.Base(m.Path)
		target := memberLinkTarget(indexDir, m)
		summaryFile := deriveSummaryFilePath(indexDir, m)
		desc := ""
		if summaryFile != "" {
			desc = DeriveMemberDescription(summaryFile)
		}
		if desc != "" {
			fmt.Fprintf(&sb, "- [%s](%s) - %s\n", name, target, desc)
		} else {
			fmt.Fprintf(&sb, "- [%s](%s)\n", name, target)
		}
	}
	sb.WriteString(membersMarkerEnd)
	sb.WriteString("\n")
	return sb.String()
}

// memberLinkTarget computes the markdown link target for a member, relative to
// the index.md directory.
func memberLinkTarget(indexDir string, m Member) string {
	switch m.Status {
	case "indexed":
		// Link to m.SignalsPath — either docs/wiki/index.md (new layout) or
		// .claude/project/signals.md (legacy layout), whichever classifyMembers found.
		if m.SignalsPath != "" {
			rel, err := filepath.Rel(indexDir, m.SignalsPath)
			if err == nil {
				return rel
			}
		}
		// Fallback: prefer new layout path when path is known.
		return "../" + m.Path + "/docs/wiki/index.md"
	case "summarized":
		// Link to the summary (already relative to wiki/): repos/<repo>.md or
		// repos/<repo>/ for a domain-split summary.
		if m.SummaryPath != "" {
			return m.SummaryPath
		}
		return "repos/" + filepath.Base(m.Path) + ".md"
	default: // "pending"
		// Link to the repo directory.
		return "../" + m.Path + "/"
	}
}

// rewriteMembersSection replaces the managed ## Members section in content.
// The heading "## Members" and the markers are both managed — the whole block
// from "## Members\n" through the end marker is replaced.
// Content outside is preserved byte-for-byte.
func rewriteMembersSection(content, newSection string) string {
	// Strategy: find "## Members\n" followed eventually by the start marker.
	// If the start marker is present, replace everything from the heading to the
	// end marker. If neither exists, append.

	startIdx := strings.Index(content, membersMarkerStart)
	if startIdx == -1 {
		// No existing managed section — append.
		result := content
		if !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
		return result + "\n" + newSection
	}

	// Find the heading "## Members" before the start marker.
	// Walk backwards from startIdx to find the start of the line containing "## Members".
	headingPrefix := "## Members"
	before := content[:startIdx]
	headingIdx := strings.LastIndex(before, headingPrefix)
	if headingIdx == -1 {
		// Marker exists but no heading — replace just from the marker.
		endIdx := strings.Index(content[startIdx:], membersMarkerEnd)
		if endIdx == -1 {
			// No end marker — replace from start marker to EOF.
			return content[:startIdx] + newSection
		}
		blockEnd := startIdx + endIdx + len(membersMarkerEnd)
		return content[:startIdx] + newSection + content[blockEnd:]
	}

	// Find the end marker.
	endIdx := strings.Index(content[startIdx:], membersMarkerEnd)
	if endIdx == -1 {
		// No end marker — replace from heading to EOF.
		return content[:headingIdx] + newSection
	}
	blockEnd := startIdx + endIdx + len(membersMarkerEnd)

	// Consume trailing newline after end marker if present.
	afterBlock := content[blockEnd:]
	if strings.HasPrefix(afterBlock, "\n") {
		blockEnd++
		afterBlock = content[blockEnd:]
	}

	return content[:headingIdx] + newSection + afterBlock
}

// rewriteScanBlock replaces the <wiki-scan> block in content with newBlock.
// Content outside the block is preserved byte-for-byte.
// This mirrors the splice pattern from internal/profile/render.go
// (RewriteEnvironmentSection / findHeadingIndex / findNextH2After).
func rewriteScanBlock(content, newBlock string) string {
	openIdx := strings.Index(content, scanMarkerOpen)
	if openIdx == -1 {
		// No existing block — append.
		result := content
		if !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
		return result + "\n" + newBlock
	}

	// Find end of open tag.
	closeTagIdx := strings.Index(content[openIdx:], ">")
	if closeTagIdx == -1 {
		// Malformed open tag — append.
		return content + "\n" + newBlock
	}
	afterOpenTag := openIdx + closeTagIdx + 1

	// Find the close tag.
	closeIdx := strings.Index(content[afterOpenTag:], scanMarkerClose)
	if closeIdx == -1 {
		// No close tag — replace from open tag to EOF.
		before := content[:openIdx]
		return before + newBlock
	}

	blockEnd := afterOpenTag + closeIdx + len(scanMarkerClose)

	before := content[:openIdx]
	after := content[blockEnd:]

	return before + newBlock + after
}
