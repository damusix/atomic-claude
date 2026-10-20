// Package wiki implements the deterministic core of the atomic wiki feature:
// repo discovery, classification, scaffold creation, and idempotent
// <wiki-scan> block writes.
package wiki

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/damusix/atomic-claude/atomic/internal/config"
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
// Refuses a wiki/ it does not own; see checkCollision.
func Scan(root string, opts Options) ([]Member, error) {
	wikiDir := filepath.Join(root, "wiki")

	if err := checkCollision(wikiDir); err != nil {
		return nil, err
	}

	prior, err := parsePriorEntries(filepath.Join(wikiDir, "index.md"))
	if err != nil {
		return nil, fmt.Errorf("wiki scan: parse prior entries: %w", err)
	}

	rawMembers, warns, err := discoverMembers(root, wikiDir)
	for _, w := range warns {
		fmt.Fprintf(os.Stderr, "atomic wiki scan: warning: %s\n", w)
	}
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
	_, rebErrs := RebuildAllBucketIndexes(root, wikiDir)
	reportTreeErrors("atomic wiki scan: bucket index rebuild", rebErrs)

	return classified, nil
}

// checkCollision refuses an existing wiki/ dir that this tool does not own,
// i.e. whose index.md carries no <wiki-scan> marker, or is absent and the dir
// is not exactly what `wiki init --scope realm` writes.
func checkCollision(wikiDir string) error {
	if _, err := os.Lstat(wikiDir); os.IsNotExist(err) {
		return nil
	}

	indexPath := filepath.Join(wikiDir, "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			if onlyInitScaffold(wikiDir) {
				return nil
			}
			return fmt.Errorf("wiki scan: collision: %s exists but index.md is absent — refusing to overwrite", wikiDir)
		}
		return fmt.Errorf("wiki scan: read index.md: %w", err)
	}

	if !strings.Contains(string(data), scanMarkerOpen) {
		return fmt.Errorf("wiki scan: collision: %s lacks a <wiki-scan> marker — refusing to overwrite", indexPath)
	}

	return nil
}

func onlyInitScaffold(wikiDir string) bool {
	entries, err := os.ReadDir(wikiDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "CLAUDE.md" || !entries[0].Type().IsRegular() {
		return false
	}
	data, err := os.ReadFile(filepath.Join(wikiDir, "CLAUDE.md"))
	return err == nil && string(data) == realmSteeringScaffold
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

	return parseBlockMembers(blockContent), nil
}

// parseBlockMembers is the read counterpart to the <wiki-scan> block writer.
func parseBlockMembers(blockContent string) map[string]priorEntry {
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
	return entries
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

// discoverMembers returns sorted root-relative repo paths under the [realm] repos
// roots, or all of root when unset, plus warnings. Root is never a member.
func discoverMembers(root, wikiDir string) ([]string, []string, error) {
	roots, warns := declaredRoots(root, wikiDir)
	if roots == nil {
		members, err := walkRoot(root, wikiDir)
		return members, warns, err
	}

	var members []string
	for _, r := range roots {
		found, err := walkForRepos(root, filepath.Join(root, filepath.FromSlash(r)), wikiDir)
		if err != nil {
			return nil, warns, err
		}
		members = append(members, found...)
	}
	sort.Strings(members)
	return slices.Compact(members), warns, nil
}

// declaredRoots expands [realm] repos into slash-separated roots to walk. nil
// means walk all of root; empty non-nil means no entry was usable.
func declaredRoots(root, wikiDir string) ([]string, []string) {
	cfgPath := config.RepoConfigPath(root)
	cfg, cfgWarns, err := config.LoadRepoConfig(cfgPath)
	if err != nil {
		return nil, []string{fmt.Sprintf("%v; ignoring [realm] repos and scanning all of %s", err, root)}
	}
	var warns []string
	for _, w := range cfgWarns {
		if strings.HasPrefix(w.Key, "realm.") {
			warns = append(warns, fmt.Sprintf("%s in %s", w.Message, cfgPath))
		}
	}
	if cfg.Realm.Repos == nil {
		return nil, warns
	}
	if len(cfg.Realm.Repos) == 0 {
		return nil, append(warns, fmt.Sprintf("[realm] repos is empty; scanning all of %s; list repo paths in %s or remove the key", root, cfgPath))
	}

	warn := func(entry, problem string) {
		warns = append(warns, fmt.Sprintf("[realm] repos entry %q %s (realm root %s); fix or remove it in %s", entry, problem, root, cfgPath))
	}

	wikiRel, _ := filepath.Rel(root, wikiDir)
	wikiRel = filepath.ToSlash(wikiRel)
	fsys := os.DirFS(root)
	var matched []string
	wholeRoot := false
	for _, entry := range cfg.Realm.Repos {
		if entry == "" {
			warn(entry, "is empty")
			continue
		}
		slashed := filepath.ToSlash(entry)
		clean := path.Clean(slashed)
		if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			warn(entry, "must stay inside the realm root")
			continue
		}
		// path.Clean drops a trailing "/", which doublestar needs to match directories only.
		if strings.HasSuffix(slashed, "/") && clean != "." {
			clean += "/"
		}
		hits, err := doublestar.Glob(fsys, clean, doublestar.WithNoFollow())
		if err != nil {
			warn(entry, "is not a valid pattern")
			continue
		}
		if len(hits) == 0 {
			warn(entry, "matches nothing")
			continue
		}
		dirs, problems, whole := classifyHits(root, wikiRel, hits, strings.ContainsAny(clean, "*?[{"))
		for _, p := range problems {
			warn(entry, p)
		}
		wholeRoot = wholeRoot || whole
		matched = append(matched, dirs...)
	}
	if wholeRoot {
		return nil, warns
	}

	sort.Strings(matched)
	roots := []string{}
	for _, r := range matched {
		if !slices.ContainsFunc(roots, func(outer string) bool { return outer == r || strings.HasPrefix(r, outer+"/") }) {
			roots = append(roots, r)
		}
	}
	return roots, warns
}

// classifyHits sorts one entry's glob hits into directory roots and at most one
// problem per kind (symlinks, unreadable, files). wholeRoot reports a "." hit.
func classifyHits(root, wikiRel string, hits []string, isPattern bool) (dirs, problems []string, wholeRoot bool) {
	var files, linked, unreadable []string
	var firstLink string
	var firstErr error
	for _, h := range hits {
		h = strings.TrimSuffix(h, "/")
		if h == "." {
			wholeRoot = true
			continue
		}
		if skippedRoot(h, wikiRel) {
			continue
		}
		link, info, err := firstSymlink(root, h)
		switch {
		case err != nil:
			if unreadable == nil {
				firstErr = err
			}
			unreadable = append(unreadable, h)
		case link != "":
			if linked == nil {
				firstLink = link
			}
			linked = append(linked, h)
		case !info.IsDir():
			files = append(files, h)
		default:
			dirs = append(dirs, h)
		}
	}
	switch {
	case len(linked) == 1 && linked[0] == firstLink:
		problems = append(problems, fmt.Sprintf("matches %s, which is a symbolic link; declare the real path inside the realm root", firstLink))
	case len(linked) == 1:
		problems = append(problems, fmt.Sprintf("matches %s, which passes through the symbolic link %s; declare the real path inside the realm root", linked[0], firstLink))
	case len(linked) > 1 && linked[0] == firstLink:
		problems = append(problems, fmt.Sprintf("matches %d paths through symbolic links (first: %s); declare the real path inside the realm root", len(linked), firstLink))
	case len(linked) > 1:
		problems = append(problems, fmt.Sprintf("matches %d paths through symbolic links (first: %s, through %s); declare the real path inside the realm root", len(linked), linked[0], firstLink))
	}
	switch {
	case len(unreadable) == 1:
		problems = append(problems, fmt.Sprintf("matches %s, which could not be read: %v", unreadable[0], firstErr))
	case len(unreadable) > 1:
		problems = append(problems, fmt.Sprintf("matches %d paths that could not be read (first: %s: %v)", len(unreadable), unreadable[0], firstErr))
	}
	if len(files) > 0 {
		problem := fmt.Sprintf("matches %s, which is not a directory", files[0])
		if len(files) > 1 {
			problem = fmt.Sprintf("matches %d files, not directories (first: %s)", len(files), files[0])
		}
		if isPattern {
			problem += `; end the pattern with "/" to match directories only`
		}
		problems = append(problems, problem)
	}
	return dirs, problems, wholeRoot
}

// firstSymlink returns the shortest prefix of the slash-separated rel that is a
// symbolic link, or "" and rel's own info when no segment is one.
func firstSymlink(root, rel string) (string, fs.FileInfo, error) {
	segs := strings.Split(rel, "/")
	var info fs.FileInfo
	for i := range segs {
		prefix := strings.Join(segs[:i+1], "/")
		var err error
		info, err = os.Lstat(filepath.Join(root, filepath.FromSlash(prefix)))
		if err != nil {
			return "", nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return prefix, info, nil
		}
	}
	return "", info, nil
}

// skippedRoot reports whether a declared root is the wiki itself, under it, or
// inside a directory the whole-root walk would never enter.
func skippedRoot(rel, wikiRel string) bool {
	if rel == wikiRel || strings.HasPrefix(rel, wikiRel+"/") {
		return true
	}
	return slices.ContainsFunc(strings.Split(rel, "/"), func(seg string) bool { return skipDirs[seg] })
}

// walkRoot walks root's children for git repos, returning sorted relative paths.
func walkRoot(root, wikiDir string) ([]string, error) {
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

// scaffold creates wiki/ with repos/, concerns/, README.md and
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
// §6 SHOULD semantics). Always single-line and at most 120 characters; a cut
// lands at a word boundary and ends in "…". Unreadable files return "".
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

// truncate cuts s to at most n runes ending in "…", at a word boundary unless
// the first word alone overflows.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	cut := string(runes[:n-1])
	if runes[n-1] != ' ' {
		if i := strings.LastIndexByte(cut, ' '); i > 0 {
			cut = cut[:i]
		}
	}
	return strings.TrimRight(cut, " ") + "…"
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
