// graph.go — link graph model for CP4.
//
// BuildLinkGraph walks all *.md files under the realm root, calls
// mdlink.ExtractLinks on each, resolves wikilinks using the
// nearest-then-alphabetical rule, and returns a Graph that answers backlinks,
// outbound links, and orphan queries.
//
// Wikilink resolution rule (documented here as the canonical source):
//  1. Collect all .md files under the realm whose basename (without .md) or
//     full basename matches the wikilink page name.
//  2. If zero matches → broken link.
//  3. If one match → resolved.
//  4. If multiple matches → pick the one with the shortest relative path from
//     the linking file's directory (i.e. fewest path components, which is
//     "nearest by depth"); ties broken alphabetically by the resolved relative
//     path. The Ambiguous field is set to true on the returned edge so the UI
//     can surface the ambiguity.
//
// All paths stored in the graph are relative to the realm root (forward slashes
// on all platforms).
package serve

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
)

// Edge is a directed link from one page to another (or to an unresolved target).
type Edge struct {
	// SourcePage is the realm-root-relative path of the page containing the link.
	SourcePage string

	// Target is the raw link target as written in the source (URL, path, wikilink name).
	Target string

	// Kind is MarkdownLink or Wikilink.
	Kind mdlink.LinkKind

	// ResolvedPath is the realm-root-relative path the link resolves to.
	// Empty when Broken is true.
	ResolvedPath string

	// Broken is true when the link target cannot be resolved to a file in the realm.
	Broken bool

	// Ambiguous is true when a wikilink matched more than one file; ResolvedPath
	// carries the nearest-then-alphabetical winner but the ambiguity is surfaced.
	Ambiguous bool

	// CodeFile is true when the link target is an existing non-.md source file
	// (e.g. a .go, .ts, .py file). The UI renders these as clickable /file/ links
	// that open the code modal, not as broken links.
	CodeFile bool

	// External is true when the link target is a real external URL (http://,
	// https://, or mailto:). Anchor-only links (#section) are NOT external —
	// they jump within the current page and must not open a new tab.
	External bool
}

// NodeMeta holds per-page preview metadata for the hover card and modal.
type NodeMeta struct {
	// Title is the frontmatter `title:` value; falls back to the humanized filename.
	Title string
	// Description is the frontmatter `description:` value (may be empty).
	Description string
	// Snippet is the first prose line of the body, truncated to ~120 chars.
	// Headings, blank lines, list items, and table rows are skipped.
	Snippet string
}

// Graph is the in-memory link graph for a realm.
type Graph struct {
	// nodes is the set of all .md files found in the realm (realm-root-relative).
	nodes []string

	// nodeSet is an O(1) membership index over nodes.
	nodeSet map[string]bool

	// edges are all extracted+resolved links, keyed by source page.
	outbound map[string][]Edge

	// inbound is the inverse index: target → list of source pages.
	inbound map[string][]string

	// nodeTypes maps realm-root-relative page path → short lowercase FE class
	// ("repo"/"concern"/"knowledge"/"bucket"/"external"/"page").
	// Populated once during BuildLinkGraph from frontmatter + path conventions.
	// Non-.md source files are not .md nodes and are never inserted here.
	nodeTypes map[string]string

	// nodeMeta maps realm-root-relative page path → preview metadata.
	// Populated once during BuildLinkGraph alongside nodeTypes.
	nodeMeta map[string]NodeMeta
}

// Nodes returns all page paths (realm-root-relative) in the graph.
func (g *Graph) Nodes() []string {
	return g.nodes
}

// Has reports whether relPath is a known node in the graph (O(1)).
func (g *Graph) Has(relPath string) bool {
	return g.nodeSet[relPath]
}

// Outbound returns all outgoing edges from the given page (realm-root-relative).
func (g *Graph) Outbound(relPath string) []Edge {
	return g.outbound[relPath]
}

// Backlinks returns the realm-root-relative paths of pages that link to relPath.
func (g *Graph) Backlinks(relPath string) []string {
	return g.inbound[relPath]
}

// IsOrphan reports whether relPath has no inbound links from other pages.
func (g *Graph) IsOrphan(relPath string) bool {
	return len(g.inbound[relPath]) == 0
}

// Meta returns per-page preview metadata for relPath (title, description, snippet).
// Returns a zero NodeMeta (empty strings) for paths not in the graph.
func (g *Graph) Meta(relPath string) NodeMeta {
	return g.nodeMeta[relPath]
}

// NodeType returns the short lowercase FE class for relPath:
// "repo" / "concern" / "knowledge" / "bucket" / "external" / "page".
// Non-.md source files that are not graph nodes return "external".
func (g *Graph) NodeType(relPath string) string {
	if t, ok := g.nodeTypes[relPath]; ok {
		return t
	}
	// Non-.md nodes (source files added by injectProvenanceEdges or similar)
	// that are not in the nodeTypes map default to "external".
	if !strings.HasSuffix(relPath, ".md") {
		return "external"
	}
	return "page"
}

// frontmatterTypeToClass maps human-readable title-case OKF type values
// (as written by the producer) to the short lowercase FE node class used by
// the Cytoscape CSS selectors in layout.html.
//
// Matching is case-insensitive (normalised to lower before lookup).
var frontmatterTypeToClass = map[string]string{
	"knowledge":    "knowledge",
	"concern":      "concern",
	"repo summary": "repo",
	"repo":         "repo",
	"bucket":       "bucket",
}

// resolveNodeType derives the short lowercase FE class for a .md page.
//
// Resolution order:
//  1. Frontmatter `type` key (case-insensitive, mapped via frontmatterTypeToClass).
//  2. Path-convention fallback: path segment `repos/` → "repo",
//     `concerns/` → "concern", `knowledge/` → "knowledge".
//  3. Default: "page".
//
// fileContent is the raw content of the .md file (already read by the caller).
func resolveNodeType(relPath string, fileContent []byte) string {
	// Step 1: frontmatter type.
	if len(fileContent) > 0 {
		meta, _, err := frontmatter.Parse(string(fileContent))
		if err == nil && meta != nil {
			if raw, ok := meta["type"]; ok {
				if s, ok := raw.(string); ok {
					if class, known := frontmatterTypeToClass[strings.ToLower(strings.TrimSpace(s))]; known {
						return class
					}
				}
			}
		}
	}

	// Step 2: path-convention fallback. Check for `/repos/`, `/concerns/`,
	// `/knowledge/` as path segments (forward-slash normalised).
	slashed := filepath.ToSlash(relPath)
	switch {
	case strings.Contains(slashed, "/repos/") || strings.HasPrefix(slashed, "repos/"):
		return "repo"
	case strings.Contains(slashed, "/concerns/") || strings.HasPrefix(slashed, "concerns/"):
		return "concern"
	case strings.Contains(slashed, "/knowledge/") || strings.HasPrefix(slashed, "knowledge/"):
		return "knowledge"
	}

	// Step 3: default.
	return "page"
}

// extractNodeMeta derives per-page preview metadata from raw file content.
//
// title     — frontmatter `title:` string; else the basename without .md,
//
//	with hyphens/underscores replaced by spaces and capitalized.
//
// description — frontmatter `description:` string (empty when absent).
// snippet   — first prose paragraph line in the body (after stripping frontmatter):
//
//	skips blank lines, ATX headings (#…), setext underlines (---/===),
//	list markers (-, *, +, 1.), table rows (|…), fenced-code fences
//	(``` / ~~~), HTML comment lines (<!--…), and blockquote leaders (>).
//	Truncated to 120 chars; empty when none found.
func extractNodeMeta(relPath string, fileContent []byte) NodeMeta {
	var title, description, snippet string

	// ── frontmatter ──────────────────────────────────────────────────────────
	meta, body, err := frontmatter.Parse(string(fileContent))
	if err != nil || meta == nil {
		// No frontmatter — body is the whole file content.
		body = string(fileContent)
	}
	if meta != nil {
		if raw, ok := meta["title"]; ok {
			if s, ok := raw.(string); ok {
				title = strings.TrimSpace(s)
			}
		}
		if raw, ok := meta["description"]; ok {
			if s, ok := raw.(string); ok {
				description = strings.TrimSpace(s)
			}
		}
	}

	// ── title fallback: humanize the filename ─────────────────────────────────
	if title == "" {
		base := filepath.Base(relPath)
		base = strings.TrimSuffix(base, ".md")
		base = strings.ReplaceAll(base, "-", " ")
		base = strings.ReplaceAll(base, "_", " ")
		if len(base) > 0 {
			title = strings.ToUpper(base[:1]) + base[1:]
		}
	}

	// ── snippet: first prose line of the body ────────────────────────────────
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// ATX heading
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Setext underline (all dashes or all equals)
		if isSetextUnderline(trimmed) {
			continue
		}
		// List item (-, *, +, or N.)
		if isListItem(trimmed) {
			continue
		}
		// Table row
		if strings.HasPrefix(trimmed, "|") {
			continue
		}
		// Fenced code block
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			continue
		}
		// HTML comment
		if strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		// Blockquote
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		// Found a prose line.
		if len(trimmed) > 120 {
			trimmed = trimmed[:120]
		}
		snippet = trimmed
		break
	}

	return NodeMeta{Title: title, Description: description, Snippet: snippet}
}

// isSetextUnderline returns true for lines that are entirely '=' or '-'
// (setext heading underlines). At least 2 chars required.
func isSetextUnderline(s string) bool {
	if len(s) < 2 {
		return false
	}
	c := s[0]
	if c != '=' && c != '-' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] != c {
			return false
		}
	}
	return true
}

// isListItem returns true for ordered (1.) and unordered (-, *, +) list items.
func isListItem(s string) bool {
	if strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") || strings.HasPrefix(s, "+ ") {
		return true
	}
	// Ordered list: digit(s) followed by '.' and a space.
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i > 0 && i < len(s) && s[i] == '.' && i+1 < len(s) && s[i+1] == ' '
}

// BuildLinkGraph walks all *.md files under root, extracts links, resolves
// wikilinks, and returns the populated Graph.
func BuildLinkGraph(root string) *Graph {
	g := &Graph{
		nodeSet:   make(map[string]bool),
		outbound:  make(map[string][]Edge),
		inbound:   make(map[string][]string),
		nodeTypes: make(map[string]string),
		nodeMeta:  make(map[string]NodeMeta),
	}

	// ── Step 1: discover all .md files and non-.md source files ─────────────
	// sourceFiles is a realm-root-relative path set for existing non-.md files.
	// Used by resolveMarkdownLink to mark code-file links (CodeFile=true) instead
	// of treating them as broken.
	var mdFiles []string
	sourceFiles := make(map[string]bool)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if hiddenFile(d.Name()) {
			// Hidden files (backups, caches) are not navigable content.
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(d.Name(), ".md") {
			mdFiles = append(mdFiles, rel)
		} else {
			sourceFiles[rel] = true
		}
		return nil
	})
	sort.Strings(mdFiles)
	g.nodes = mdFiles
	for _, rel := range mdFiles {
		g.nodeSet[rel] = true
	}

	// ── Step 2: build basename index for wikilink resolution ─────────────────
	// basenameIndex maps "page" (lowercase, without .md) → sorted list of realm-root-relative paths.
	basenameIndex := make(map[string][]string, len(mdFiles))
	for _, rel := range mdFiles {
		base := filepath.ToSlash(strings.TrimSuffix(filepath.Base(rel), ".md"))
		key := strings.ToLower(base)
		basenameIndex[key] = append(basenameIndex[key], rel)
	}
	// Sort each bucket for deterministic ambiguity resolution.
	for k := range basenameIndex {
		sort.Strings(basenameIndex[k])
	}

	// ── Step 3: extract and resolve links per file ───────────────────────────
	for _, relPage := range mdFiles {
		absPath := filepath.Join(root, filepath.FromSlash(relPage))
		data, err := os.ReadFile(absPath) //nolint:gosec // relPage is realm-relative and validated during walk
		if err != nil {
			continue
		}

		// Resolve and cache this page's node type (frontmatter → path → default).
		// The file content is already in `data`; no second read needed.
		g.nodeTypes[relPage] = resolveNodeType(relPage, data)
		// Extract per-page preview metadata (title, description, snippet).
		g.nodeMeta[relPage] = extractNodeMeta(relPage, data)

		links := mdlink.ExtractLinks(string(data))
		pageDir := filepath.Dir(filepath.FromSlash(relPage)) // dir of the source page (relative to root)

		for _, l := range links {
			edge := resolveLink(l, relPage, pageDir, root, basenameIndex, mdFiles, sourceFiles)
			g.outbound[relPage] = append(g.outbound[relPage], edge)
			if !edge.Broken && edge.ResolvedPath != "" {
				g.inbound[edge.ResolvedPath] = append(g.inbound[edge.ResolvedPath], relPage)
			}
		}
	}

	// Deduplicate inbound entries (a page might link to the same target twice).
	for k, v := range g.inbound {
		g.inbound[k] = dedupeStrings(v)
	}

	return g
}

// resolveLink resolves a single extracted link and returns an Edge.
// pageDir is the directory of the source page relative to root (e.g. "sub" or ".").
func resolveLink(
	l mdlink.Link,
	relPage string,
	pageDir string,
	root string,
	basenameIndex map[string][]string,
	allPages []string,
	sourceFiles map[string]bool,
) Edge {
	edge := Edge{
		SourcePage: relPage,
		Target:     l.Target,
		Kind:       l.Kind,
	}

	switch l.Kind {
	case mdlink.Wikilink:
		edge = resolveWikilink(edge, l.Target, pageDir, root, basenameIndex)
	case mdlink.MarkdownLink:
		edge = resolveMarkdownLink(edge, l.Target, pageDir, root, allPages, sourceFiles)
	}

	return edge
}

// resolveWikilink applies the nearest-then-alphabetical resolution rule.
func resolveWikilink(
	edge Edge,
	pageName string,
	pageDir string,
	root string,
	basenameIndex map[string][]string,
) Edge {
	key := strings.ToLower(strings.TrimSpace(pageName))
	candidates, ok := basenameIndex[key]
	if !ok || len(candidates) == 0 {
		edge.Broken = true
		return edge
	}

	if len(candidates) == 1 {
		edge.ResolvedPath = candidates[0]
		return edge
	}

	// Multiple matches: pick nearest by path depth, then alphabetically.
	// "Nearest" = fewest directory separators in the path from pageDir to the candidate.
	// We measure depth as the number of '/' in the candidate's path relative to root.
	// The candidate in the same directory or closest ancestor wins.
	best := nearestCandidate(candidates, pageDir)
	edge.ResolvedPath = best
	edge.Ambiguous = true
	return edge
}

// scoredCandidate holds a path and its computed depth score.
type scoredCandidate struct {
	path  string
	depth int
}

// nearestCandidate picks the candidate with the fewest path depth relative to
// pageDir, breaking ties alphabetically. This implements the
// "nearest-then-alphabetical" wikilink resolution rule.
func nearestCandidate(candidates []string, pageDir string) string {
	items := make([]scoredCandidate, len(candidates))
	for i, c := range candidates {
		items[i] = scoredCandidate{path: c, depth: strings.Count(c, "/")}
	}

	// Prefer candidates closest to pageDir's depth level.
	pageDirDepth := 0
	if pageDir != "." && pageDir != "" {
		pageDirDepth = strings.Count(filepath.ToSlash(pageDir), "/") + 1
	}

	sort.SliceStable(items, func(i, j int) bool {
		di := abs(items[i].depth - pageDirDepth)
		dj := abs(items[j].depth - pageDirDepth)
		if di != dj {
			return di < dj
		}
		return items[i].path < items[j].path
	})

	return items[0].path
}

// resolveRootRelative resolves a path that is already known to be realm-root-
// relative (no pageDir join needed) against the graph's file sets and filesystem.
// It is the shared core used by both resolveMarkdownLink (for leading-slash targets)
// and resolvePageHref (for the OKF §5.1 bundle-root-relative branch) so the two
// functions cannot diverge again.
//
// combined must already be clean and free of a leading slash (i.e. "repos/a.md",
// not "/repos/a.md"). Returns (resolvedPath, codeFile, ok): ok=false means the
// path was not found and the caller should mark the edge Broken.
func resolveRootRelative(
	combined string,
	root string,
	allPages []string,
	sourceFiles map[string]bool,
) (resolvedPath string, codeFile bool, ok bool) {
	// Known .md page (fast path — no stat required).
	for _, p := range allPages {
		if p == combined {
			return combined, false, true
		}
	}
	// Known non-.md source file (fast path).
	if sourceFiles[combined] {
		return combined, true, true
	}
	// Filesystem fallback: walks may have skipped directories that safeResolve
	// can still serve.
	if absPath, canServe := safeResolve(root, combined); canServe {
		if info, statErr := os.Stat(absPath); statErr == nil {
			if info.IsDir() {
				if idx, found := resolveDirIndex(root, combined); found {
					return idx, !strings.HasSuffix(idx, ".md"), true
				}
				return "", false, false // bare directory with no index
			}
			if strings.HasSuffix(combined, ".md") {
				return combined, false, true
			}
			return combined, true, true
		}
	}
	return "", false, false
}

// resolveMarkdownLink attempts to resolve a markdown link target to a file
// within the realm. External URLs (http/https) and anchors (#…) are kept as-is
// without a ResolvedPath. Relative paths are resolved from pageDir.
//
// Bundle-root-relative links (OKF §5.1 form: a leading slash) are resolved the
// same way as resolvePageHref: strip the slash, clean, traversal-guard, then
// probe via resolveRootRelative. This keeps the link graph and the render path
// in agreement — a `/path.md` link renders as an in-shell href AND is recorded
// as a non-broken edge with backlink contribution.
//
// When the target resolves to an existing non-.md source file (e.g. a .go, .ts,
// .py file), the edge is marked CodeFile=true and ResolvedPath is set to the
// realm-relative path. These are rendered in the UI as /file/ links that open
// the code modal, not as broken links.
func resolveMarkdownLink(
	edge Edge,
	target string,
	pageDir string,
	root string,
	allPages []string,
	sourceFiles map[string]bool,
) Edge {
	// External URLs are always valid (not broken, no realm path).
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
		strings.HasPrefix(target, "mailto:") {
		// Mark as external so the UI renders target="_blank".
		edge.External = true
		return edge
	}

	// Strip anchor suffix for file resolution.
	cleanTarget := target
	if idx := strings.IndexByte(target, '#'); idx != -1 {
		cleanTarget = target[:idx]
	}
	if cleanTarget == "" {
		// Anchor-only link — not broken, no file path.
		return edge
	}

	// Bundle-root-relative link (OKF §5.1 form): a leading slash means the
	// target is relative to the served root, not to the linking page's directory.
	// Mirrors the identical branch in resolvePageHref — both must resolve the same
	// way so the render path and the link graph agree.
	if filepath.IsAbs(cleanTarget) {
		rootRel := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(cleanTarget, "/")))
		if rootRel == ".." || strings.HasPrefix(rootRel, "../") {
			// Traversal escape → broken (same as resolvePageHref).
			edge.Broken = true
			return edge
		}
		resolved, codeFile, ok := resolveRootRelative(rootRel, root, allPages, sourceFiles)
		if !ok {
			edge.Broken = true
			return edge
		}
		edge.ResolvedPath = resolved
		edge.CodeFile = codeFile
		return edge
	}

	// pageDir is relative to root. Combine with the target to get a root-relative path.
	var combined string
	if pageDir == "." || pageDir == "" {
		combined = cleanTarget
	} else {
		combined = filepath.Join(pageDir, cleanTarget)
	}
	combined = filepath.ToSlash(filepath.Clean(combined))

	// Check it's within the realm.
	if strings.HasPrefix(combined, "..") {
		edge.Broken = true
		return edge
	}

	resolved, codeFile, ok := resolveRootRelative(combined, root, allPages, sourceFiles)
	if !ok {
		edge.Broken = true
		return edge
	}
	edge.ResolvedPath = resolved
	edge.CodeFile = codeFile
	return edge
}

// resolvePageHref rewrites a raw in-page markdown link destination, found on a
// page at pageRelPath (realm-root-relative), into a server route resolved
// against the realm root. It is the render-time counterpart to
// resolveMarkdownLink (which builds the link graph): both resolve a target the
// same way via the shared resolveRootRelative helper; this one returns the URL
// the browser should use.
//
// Returns (href, htmxPage, external):
//   - external (http/https/mailto)                        → (raw, false, true)   new-tab link
//   - empty or anchor-only (#sec)                         → (raw, false, false)  left verbatim
//   - leading-slash that escapes root (/../…) or ".."     → (raw, false, false)  left verbatim
//   - leading-slash that resolves under root (OKF §5.1)   → ("/page/<rel>", true, false) or ("/file/<rel>", false, false)
//   - leading-slash to unresolved path within root        → ("/page/<rel>", true, false)  in-shell 404
//   - directory within realm                              → ("/page/<dir>/", true, false)
//   - *.md within realm (or unresolved .md)               → ("/page/<rel>", true, false)
//   - source file within realm                            → ("/file/<rel>", false, false)
//   - unresolved, non-source extension                    → ("/page/<rel>", true, false)
//
// Routing unresolved-but-in-realm targets through /page/ keeps a dead link
// inside the shell: the page handler serves a graceful htmx 404 fragment rather
// than the browser doing a full-page navigation to a broken URL.
func resolvePageHref(root, pageRelPath, raw string) (href string, htmxPage, external bool) {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") ||
		strings.HasPrefix(raw, "mailto:") {
		return raw, false, true
	}
	if raw == "" || strings.HasPrefix(raw, "#") {
		return raw, false, false
	}

	// Split off an anchor so it survives onto the rewritten URL.
	target, anchor := raw, ""
	if i := strings.IndexByte(raw, '#'); i != -1 {
		target, anchor = raw[:i], raw[i:]
	}
	if target == "" {
		return raw, false, false
	}
	// Bundle-root-relative links (OKF §5.1 form): a leading slash means the
	// target is relative to the served root, not to the filesystem. Strip the
	// slash and resolve from root directly — same downstream logic as a relative
	// link. Only fall through to the raw/external branch when the cleaned path
	// still escapes root after stripping (e.g. "/../../../etc/passwd").
	if filepath.IsAbs(target) {
		rootRel := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(target, "/")))
		if rootRel == ".." || strings.HasPrefix(rootRel, "../") {
			return raw, false, false
		}
		// Re-enter the resolution logic below with the root-relative path.
		// We assign to combined directly and skip the pageDir join since the
		// path is already relative to root.
		combined := rootRel
		if abs, ok := safeResolve(root, combined); ok {
			if info, statErr := os.Stat(abs); statErr == nil {
				if info.IsDir() {
					return "/page/" + combined + "/" + anchor, true, false
				}
				if strings.HasSuffix(combined, ".md") {
					return "/page/" + combined + anchor, true, false
				}
				return "/file/" + combined + anchor, false, false
			}
		}
		// Not found on disk: route in-shell by extension (same as the
		// relative-path unresolved branch below).
		if ext := filepath.Ext(combined); ext != "" && ext != ".md" {
			return "/file/" + combined + anchor, false, false
		}
		return "/page/" + combined + anchor, true, false
	}

	pageDir := filepath.ToSlash(filepath.Dir(pageRelPath))
	combined := target
	if pageDir != "." && pageDir != "" {
		combined = filepath.Join(pageDir, target)
	}
	combined = filepath.ToSlash(filepath.Clean(combined))
	if combined == ".." || strings.HasPrefix(combined, "../") {
		return raw, false, false
	}

	// Classify against the filesystem (the servable surface).
	if abs, ok := safeResolve(root, combined); ok {
		if info, statErr := os.Stat(abs); statErr == nil {
			if info.IsDir() {
				return "/page/" + combined + "/" + anchor, true, false
			}
			if strings.HasSuffix(combined, ".md") {
				return "/page/" + combined + anchor, true, false
			}
			return "/file/" + combined + anchor, false, false
		}
	}

	// Unresolved but within the realm: route by extension so the user stays in
	// the shell (a known source extension opens the code modal; everything else
	// goes through /page/ and gets the in-shell 404 fragment).
	if ext := filepath.Ext(combined); ext != "" && ext != ".md" {
		return "/file/" + combined + anchor, false, false
	}
	return "/page/" + combined + anchor, true, false
}

// resolveDirIndex probes a directory (realm-root-relative) for a servable index
// file and returns its realm-root-relative path. Probe order favors a human
// entry point, then a project signals page. Returns ("", false) when none exist.
func resolveDirIndex(root, dirRel string) (string, bool) {
	for _, candidate := range []string{
		"README.md",
		"readme.md",
		"index.md",
		".claude/project/signals.md",
	} {
		rel := filepath.ToSlash(filepath.Join(dirRel, candidate))
		abs, ok := safeResolve(root, rel)
		if !ok {
			continue
		}
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			return rel, true
		}
	}
	return "", false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func dedupeStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	out := ss[:0]
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
