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
	// SignalsPath is the absolute path to .claude/project/signals.md when Status == "indexed".
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

// classifyMembers derives the status for each member.
//
// Classification rules:
//  1. If prior status was "summarized" AND the summary file still exists → keep "summarized".
//  2. If .claude/project/signals.md exists → "indexed" (signals are richer than
//     summaries; a leftover summary does not demote a graduated repo).
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

		// Derive from signals presence.
		signalsAbs := filepath.Join(absRepo, ".claude", "project", "signals.md")
		if _, err := os.Lstat(signalsAbs); err == nil {
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
//   - indexed  → [<repo>](../<repo>/.claude/project/signals.md)
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

// buildMembersSection produces the full ## Members managed section string,
// bounded by the HTML-comment markers.
func buildMembersSection(indexDir string, members []Member) string {
	var sb strings.Builder
	sb.WriteString("## Members\n\n")
	sb.WriteString(membersMarkerStart)
	sb.WriteString("\n")
	for _, m := range members {
		name := filepath.Base(m.Path)
		target := memberLinkTarget(indexDir, m)
		fmt.Fprintf(&sb, "- [%s](%s)\n", name, target)
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
		// Link to the repo's signals.md.
		// m.SignalsPath is the absolute path to .claude/project/signals.md in the repo.
		if m.SignalsPath != "" {
			rel, err := filepath.Rel(indexDir, m.SignalsPath)
			if err == nil {
				return rel
			}
		}
		// Fallback: construct from path.
		return "../" + m.Path + "/.claude/project/signals.md"
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
