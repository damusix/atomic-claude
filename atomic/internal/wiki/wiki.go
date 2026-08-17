// Package wiki implements the deterministic core of the atomic wiki feature:
// repo discovery, classification, scaffold creation, and idempotent
// <wiki-scan> block writes.
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

// skipDirs mirrors the skip set in internal/signals/tree.go, plus .worktrees.
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

const scanMarkerOpen = "<wiki-scan"

const scanMarkerClose = "</wiki-scan>"

// Legacy HTML-comment boundaries of the ## Members section, superseded by the
// `wiki-member-list` XML region. Retained only so migrateLegacyMemberMarkers
// can detect a pre-migration section; no write emits them again.
const membersMarkerStart = "<!-- wiki-members:start -->"
const membersMarkerEnd = "<!-- wiki-members:end -->"

// Options configures a Scan run.
type Options struct {
	// Clock returns the current time; nil means time.Now().UTC().
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
	// SignalsPath is the absolute router path when Status == "indexed":
	// docs/wiki/index.md, or the legacy .claude/project/signals.md.
	SignalsPath string
	// SummaryPath is the wiki-relative summary path when Status == "summarized".
	SummaryPath string
}

// Scan discovers repos under root, scaffolds wiki/, and writes wiki/index.md
// with an idempotent <wiki-scan> block, returning the classified members so
// callers need no second filesystem walk.
//
// Refuses when wiki/ exists but index.md is absent or lacks a <wiki-scan> marker.
func Scan(root string, opts Options) ([]Member, error) {
	wikiDir := filepath.Join(root, "wiki")

	if err := checkCollision(wikiDir); err != nil {
		return nil, err
	}

	prior, err := parsePriorEntries(filepath.Join(wikiDir, "index.md"))
	if err != nil {
		return nil, fmt.Errorf("wiki scan: parse prior entries: %w", err)
	}

	rawMembers, err := discoverMembers(root, wikiDir)
	if err != nil {
		return nil, fmt.Errorf("wiki scan: discover: %w", err)
	}

	classified := classifyMembers(root, wikiDir, rawMembers, prior)

	if err := scaffold(wikiDir, root); err != nil {
		return nil, fmt.Errorf("wiki scan: scaffold: %w", err)
	}

	indexPath := filepath.Join(wikiDir, "index.md")
	if err := writeWikiScanBlock(indexPath, root, classified, opts); err != nil {
		return nil, fmt.Errorf("wiki scan: write block: %w", err)
	}

	if err := writeMembersSection(indexPath, classified); err != nil {
		return nil, fmt.Errorf("wiki scan: write members section: %w", err)
	}

	// Non-fatal: a broken bucket region must not block membership.
	if err := RebuildAllBucketIndexes(root, wikiDir); err != nil {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: bucket index rebuild: %v\n", err)
	}

	return classified, nil
}

// checkCollision refuses an existing wiki/ dir that this tool does not own,
// i.e. whose index.md is absent or carries no <wiki-scan> marker.
func checkCollision(wikiDir string) error {
	if _, err := os.Lstat(wikiDir); os.IsNotExist(err) {
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

// priorEntry is one <repo> entry parsed from a previous scan block.
type priorEntry struct {
	status      string
	summaryAttr string // e.g. "repos/repoA.md"
}

// parsePriorEntries extracts each repo's prior status from index.md's
// <wiki-scan> block, feeding summarized-preservation in classifyMembers.
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

// extractBlockContent returns the content between <wiki-scan ...> and
// </wiki-scan>, or "" when the block is absent or malformed.
func extractBlockContent(content string) string {
	openIdx := strings.Index(content, scanMarkerOpen)
	if openIdx == -1 {
		return ""
	}
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

// attrValue reads an attribute off a tag line: `<repo path="foo"/>`, "path" → "foo".
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

// discoverMembers walks root's children for git repos, returning sorted
// relative paths. Root itself is never a member.
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

// walkForRepos returns dir as the sole member when it is a git repo, without
// recursing into it; otherwise it recurses into dir's children.
func walkForRepos(root, dir, wikiDir string) ([]string, error) {
	if isGitMember(dir) {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return nil, err
		}
		return []string{rel}, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// An unreadable directory is skipped, never fatal to the walk.
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

// isGitMember reports whether dir has a .git entry, file or directory — the
// file form is a worktree, which counts.
func isGitMember(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// classifyMembers derives each member's status, first rule wins:
//  1. prior "summarized" whose summary file still exists → keep "summarized".
//  2. docs/wiki/index.md, else legacy .claude/project/signals.md → "indexed".
//     A leftover summary must not demote a repo that has graduated.
//  3. a summary already on disk under wiki/repos/ → "summarized". Needed
//     because /refresh-wiki writes summaries after the scan that ordered them.
//  4. otherwise → "pending".
func classifyMembers(root, wikiDir string, members []string, prior map[string]priorEntry) []Member {
	result := make([]Member, 0, len(members))

	for _, rel := range members {
		absRepo := filepath.Join(root, rel)

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

// discoverSummary returns the wiki-relative summary path for member rel, or ""
// when none exists. Summaries are keyed by the member's base name, the same
// convention memberLinkTarget and /refresh-wiki use: repos/<name>.md, or
// repos/<name>/ holding at least one .md for a domain-split summary.
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

// scaffold creates wiki/ with repos/, concerns/, .gitignore, README.md and
// CLAUDE.md, then git-inits it. Nothing existing is overwritten; index.md
// content is writeWikiScanBlock's job.
func scaffold(wikiDir, root string) error {
	for _, sub := range []string{wikiDir, filepath.Join(wikiDir, "repos"), filepath.Join(wikiDir, "concerns")} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}

	// Realm self-reference, so cd'ing straight into the wiki repo auto-loads
	// index.md at session start.
	if _, err := InitRealmScope(root); err != nil {
		return fmt.Errorf("init realm CLAUDE.md: %w", err)
	}

	gitignorePath := filepath.Join(wikiDir, ".gitignore")
	if _, err := os.Lstat(gitignorePath); os.IsNotExist(err) {
		if err := os.WriteFile(gitignorePath, []byte(".dirty\n"), 0o644); err != nil {
			return fmt.Errorf("write .gitignore: %w", err)
		}
	}

	readmePath := filepath.Join(wikiDir, "README.md")
	if _, err := os.Lstat(readmePath); os.IsNotExist(err) {
		readme := buildREADME(root)
		if err := os.WriteFile(readmePath, []byte(readme), 0o644); err != nil {
			return fmt.Errorf("write README.md: %w", err)
		}
	}

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

// writeWikiScanBlock splices the <wiki-scan> block into index.md, creating the
// file with a stub narrative when absent. Content outside the block survives
// byte-for-byte.
func writeWikiScanBlock(indexPath, root string, members []Member, opts Options) error {
	date := opts.clock().Format("2006-01-02")
	block := buildScanBlock(root, date, members)

	existing, err := os.ReadFile(indexPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read index.md: %w", err)
	}

	var newContent string
	if os.IsNotExist(err) || len(existing) == 0 {
		newContent = block + "\n" + defaultNarrative()
	} else {
		newContent = rewriteScanBlock(string(existing), block)
	}

	return os.WriteFile(indexPath, []byte(newContent), 0o644)
}

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

// defaultNarrative is the stub written under the block in a fresh index.md.
func defaultNarrative() string {
	return "\n## Realm overview\n\n<!-- Add narrative context about this realm here. -->\n"
}

// writeMembersSection splices the managed `wiki-member-list` region into
// indexPath, first relocating any legacy comment-delimited Members section.
// A stray unpaired legacy marker skips the write for this scan (non-fatal):
// appending a fresh region beside it would leave an orphan comment and a
// duplicate member listing for a human to untangle.
//
// Link targets are relative to indexPath's directory: indexed → the member's
// index or signals file, summarized → repos/<repo>.md, pending → ../<repo>/.
func writeMembersSection(indexPath string, members []Member) error {
	indexDir := filepath.Dir(indexPath)

	content := buildMembersSection(indexDir, members)

	existing, err := os.ReadFile(indexPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read index.md: %w", err)
	}

	document, skip := migrateLegacyMemberMarkers(string(existing))
	if skip {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: %s: unpaired legacy wiki-members marker — skipping members section write; resolve by hand\n", indexPath)
		return nil
	}

	newDocument, err := spliceManagedRegion(document, managedRegion{tag: "wiki-member-list", content: content})
	if err != nil {
		return fmt.Errorf("splice members section: %w", err)
	}

	return os.WriteFile(indexPath, []byte(newDocument), 0o644)
}

// migrateLegacyMemberMarkers relocates the legacy comment-delimited Members
// region — heading through end marker — onto an empty, well-formed
// `wiki-member-list` region at the same position. Relocating rather than
// excising is what lets the caller's immediately following splice fill the
// body in place, so narrative before and after keeps its order. Boundary
// whitespace is left entirely to spliceRegionAt.
//
// Returns skip=true, content untouched, when a legacy marker is unpaired or
// reversed — a half-migrated document is worse than a deferred one. An
// already well-formed region means migration ran; this is one-shot.
//
// Detection is line-anchored so prose or a code fence cannot false-match.
func migrateLegacyMemberMarkers(content string) (string, bool) {
	if state, _ := findRegion(content, "wiki-member-list"); state == regionWellFormed {
		return content, false
	}

	startIdx := findLineAnchored(content, membersMarkerStart)
	endIdx := findLineAnchored(content, membersMarkerEnd)

	if startIdx == -1 && endIdx == -1 {
		return content, false
	}
	if startIdx == -1 || endIdx == -1 || endIdx < startIdx {
		return content, true
	}

	spanEnd := endIdx + len(membersMarkerEnd)

	// Absorb an adjacent "## Members" heading — the region carries its own —
	// but only across pure whitespace. Prose the user typed between heading
	// and marker is never deleted, even at the cost of a duplicate heading.
	spanStart := startIdx
	if headingIdx := lastLineAnchored(content[:startIdx], "## Members"); headingIdx != -1 {
		gap := content[headingIdx+len("## Members") : startIdx]
		if strings.TrimSpace(gap) == "" {
			spanStart = headingIdx
		}
	}

	return spliceRegionAt(content, spanStart, spanEnd, managedRegion{tag: "wiki-member-list"}), false
}

// lastLineAnchored is findLineAnchored (managedregion.go) returning the
// rightmost whole-line match instead of the leftmost, or -1.
func lastLineAnchored(s, line string) int {
	if strings.HasSuffix(s, "\n"+line) {
		return len(s) - len(line)
	}
	if idx := strings.LastIndex(s, "\n"+line+"\n"); idx != -1 {
		return idx + 1
	}
	if s == line || strings.HasPrefix(s, line+"\n") {
		return 0
	}
	return -1
}

// deriveSummaryFilePath resolves a member's primary summary file, or "" when
// there is none. Indexed members have no summary page: they link to signals.md,
// which carries no consumer-friendly description.
func deriveSummaryFilePath(indexDir string, m Member) string {
	switch m.Status {
	case "summarized":
		if m.SummaryPath == "" {
			return ""
		}
		abs := filepath.Join(indexDir, m.SummaryPath)
		info, err := os.Lstat(abs)
		if err != nil {
			return ""
		}
		if info.IsDir() {
			// Domain-split summary: index.md, else the first .md.
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
		return ""
	default:
		return ""
	}
}

// DeriveMemberDescription reads a summary file and returns a one-line
// description for an OKF §6 Members listing: the frontmatter "description"
// key, else the first prose line of the body, else "" (link-only is valid per
// §6 SHOULD semantics). Always single-line, truncated to 120 characters;
// unreadable files return "".
func DeriveMemberDescription(summaryFilePath string) string {
	data, err := os.ReadFile(summaryFilePath)
	if err != nil {
		return ""
	}

	meta, body, _ := frontmatter.Parse(string(data))
	return deriveDescriptionFrom(meta, body)
}

// deriveDescriptionFrom applies DeriveMemberDescription's ladder to
// already-parsed frontmatter and body, sparing callers that have read the file
// (bucketindex.go's readTopicMeta) a second read.
func deriveDescriptionFrom(meta map[string]any, body string) string {
	if meta != nil {
		if v, ok := meta["description"]; ok {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
				return truncate(s, 120)
			}
		}
	}

	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, ">") ||
			strings.HasPrefix(line, "<") ||
			strings.HasPrefix(line, "|") {
			continue
		}
		if isListItem(line) {
			continue
		}
		normalized := normalizeLine(line)
		// Reject nav rows and fragments too short to be a sentence.
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

// isListItem reports whether line is a markdown list item, ordered or not.
func isListItem(line string) bool {
	if len(line) == 0 {
		return false
	}
	switch line[0] {
	case '-', '*', '+':
		// A space must follow, else this is an em-dash or a horizontal rule.
		return len(line) == 1 || line[1] == ' '
	}
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	return i > 0 && i < len(line) && line[i] == '.' && (i+1 == len(line) || line[i+1] == ' ')
}

// normalizeLine reduces markdown inline syntax to plain text for prose
// detection: links to their visible text, inline code and emphasis unwrapped,
// whitespace runs collapsed.
func normalizeLine(line string) string {
	var sb strings.Builder
	i := 0
	for i < len(line) {
		if line[i] == '[' {
			closeText := strings.Index(line[i+1:], "]")
			if closeText >= 0 {
				afterClose := i + 1 + closeText + 1 // index of ']'+1
				if afterClose < len(line) && line[afterClose] == '(' {
					closeURL := strings.Index(line[afterClose+1:], ")")
					if closeURL >= 0 {
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

	out = stripDelimited(out, '`', '`')
	out = strings.ReplaceAll(out, "**", "")
	out = strings.ReplaceAll(out, "__", "")
	// Whatever * and _ survive the paired forms are single-char emphasis.
	out = strings.ReplaceAll(out, "*", "")
	out = strings.ReplaceAll(out, "_", "")

	fields := strings.Fields(out)
	return strings.Join(fields, " ")
}

// stripDelimited unwraps every open…close span, keeping the inner content.
func stripDelimited(s string, open, close byte) string {
	var sb strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == open {
			j := strings.IndexByte(s[i+1:], close)
			if j >= 0 {
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

func letterCount(s string) int {
	n := 0
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			n++
		}
	}
	return n
}

// truncate returns at most the first n runes of s, with no ellipsis.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// buildMembersSection renders the `wiki-member-list` region body: a "## Members"
// heading plus one OKF §6 line per member, description omitted when none is
// derivable.
func buildMembersSection(indexDir string, members []Member) string {
	var sb strings.Builder
	sb.WriteString("## Members")

	if len(members) > 0 {
		sb.WriteString("\n\n")
		for i, m := range members {
			if i > 0 {
				sb.WriteString("\n")
			}
			name := filepath.Base(m.Path)
			target := memberLinkTarget(indexDir, m)
			summaryFile := deriveSummaryFilePath(indexDir, m)
			desc := ""
			if summaryFile != "" {
				desc = DeriveMemberDescription(summaryFile)
			}
			if desc != "" {
				fmt.Fprintf(&sb, "- [%s](%s) - %s", name, target, desc)
			} else {
				fmt.Fprintf(&sb, "- [%s](%s)", name, target)
			}
		}
	}

	return sb.String()
}

// memberLinkTarget computes a member's markdown link target, relative to the
// index.md directory.
func memberLinkTarget(indexDir string, m Member) string {
	switch m.Status {
	case "indexed":
		if m.SignalsPath != "" {
			rel, err := filepath.Rel(indexDir, m.SignalsPath)
			if err == nil {
				return rel
			}
		}
		return "../" + m.Path + "/docs/wiki/index.md"
	case "summarized":
		// SummaryPath is already relative to wiki/.
		if m.SummaryPath != "" {
			return m.SummaryPath
		}
		return "repos/" + filepath.Base(m.Path) + ".md"
	default: // "pending"
		return "../" + m.Path + "/"
	}
}

// rewriteScanBlock replaces the <wiki-scan> block in content, preserving
// everything outside it byte-for-byte. An absent or malformed open tag appends;
// a missing close tag truncates from the open tag to EOF.
func rewriteScanBlock(content, newBlock string) string {
	openIdx := strings.Index(content, scanMarkerOpen)
	if openIdx == -1 {
		result := content
		if !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
		return result + "\n" + newBlock
	}

	closeTagIdx := strings.Index(content[openIdx:], ">")
	if closeTagIdx == -1 {
		return content + "\n" + newBlock
	}
	afterOpenTag := openIdx + closeTagIdx + 1

	closeIdx := strings.Index(content[afterOpenTag:], scanMarkerClose)
	if closeIdx == -1 {
		before := content[:openIdx]
		return before + newBlock
	}

	blockEnd := afterOpenTag + closeIdx + len(scanMarkerClose)

	before := content[:openIdx]
	after := content[blockEnd:]

	return before + newBlock + after
}
