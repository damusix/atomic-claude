// Link graph over the realm's *.md files: backlinks, outbound links, orphans.
// Every path in the graph is realm-root-relative with forward slashes.
//
// Wikilinks resolve by basename; ties break on nearest path depth, then
// alphabetically, with Edge.Ambiguous set so the UI can surface the collision.
package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
)

// Edge is a directed link from one page to another (or to an unresolved target).
// /api/rail marshals []Edge directly.
type Edge struct {
	SourcePage string `json:"-"`

	// Target is the link as written in the source, before resolution.
	Target string `json:"target"`

	Kind mdlink.LinkKind `json:"-"`

	// ResolvedPath is empty when Broken is true.
	ResolvedPath string `json:"resolvedPath"`

	Broken bool `json:"broken"`

	// Dir means the target is a directory with no index file. Still a valid
	// page — the handler serves a listing — not a broken link.
	Dir bool `json:"dir"`

	// Ambiguous means a wikilink matched several files; ResolvedPath holds the
	// nearest-then-alphabetical winner.
	Ambiguous bool `json:"ambiguous"`

	// CodeFile means the target is an existing non-.md source file, rendered as
	// a /file/ link that opens the code modal rather than as a broken link.
	CodeFile bool `json:"codeFile"`

	// External covers http(s) and mailto only. Anchor-only links (#section) are
	// not external — they jump within the page and must not open a new tab.
	External bool `json:"external"`
}

// edgeJSON renders Kind as a string, which a struct tag alone cannot do.
type edgeJSON struct {
	Target       string `json:"target"`
	Kind         string `json:"kind"`
	ResolvedPath string `json:"resolvedPath"`
	Broken       bool   `json:"broken"`
	Dir          bool   `json:"dir"`
	Ambiguous    bool   `json:"ambiguous"`
	CodeFile     bool   `json:"codeFile"`
	External     bool   `json:"external"`
}

// MarshalJSON implements json.Marshaler, emitting Kind as a string.
func (e Edge) MarshalJSON() ([]byte, error) {
	return json.Marshal(edgeJSON{
		Target:       e.Target,
		Kind:         e.Kind.String(),
		ResolvedPath: e.ResolvedPath,
		Broken:       e.Broken,
		Dir:          e.Dir,
		Ambiguous:    e.Ambiguous,
		CodeFile:     e.CodeFile,
		External:     e.External,
	})
}

// NodeMeta holds per-page preview metadata for the hover card and modal.
type NodeMeta struct {
	// Title falls back to the humanized filename when frontmatter has none.
	Title string
	// Description is the frontmatter value, often empty.
	Description string
	// Snippet is the first prose line of the body, capped at 120 chars.
	Snippet string
}

// Graph is the in-memory link graph for a realm.
type Graph struct {
	nodes []string

	// nodeSet is an O(1) membership index over nodes.
	nodeSet map[string]bool

	// outbound holds every resolved link, keyed by source page.
	outbound map[string][]Edge

	// inbound is the inverse index: target → source pages.
	inbound map[string][]string

	// nodeTypes holds only .md pages; non-.md source files are never inserted.
	nodeTypes map[string]string

	nodeMeta map[string]NodeMeta
}

// Nodes returns all page paths in the graph.
func (g *Graph) Nodes() []string {
	return g.nodes
}

// Has reports whether relPath is a known node.
func (g *Graph) Has(relPath string) bool {
	return g.nodeSet[relPath]
}

// Outbound returns the outgoing edges from relPath.
func (g *Graph) Outbound(relPath string) []Edge {
	return g.outbound[relPath]
}

// Backlinks returns the pages linking to relPath.
func (g *Graph) Backlinks(relPath string) []string {
	return g.inbound[relPath]
}

// IsOrphan reports whether relPath has no inbound links.
func (g *Graph) IsOrphan(relPath string) bool {
	return len(g.inbound[relPath]) == 0
}

// Meta returns relPath's preview metadata, zero-valued for unknown paths.
func (g *Graph) Meta(relPath string) NodeMeta {
	return g.nodeMeta[relPath]
}

// NodeType returns the frontend node class for relPath: "repo", "concern",
// "knowledge", "bucket", "index", "domain", "external", or "page".
func (g *Graph) NodeType(relPath string) string {
	if t, ok := g.nodeTypes[relPath]; ok {
		return t
	}
	if !strings.HasSuffix(relPath, ".md") {
		return "external"
	}
	return "page"
}

// frontmatterTypeToClass maps OKF `type:` values to frontend node classes.
// Lookup keys are lowercased.
var frontmatterTypeToClass = map[string]string{
	"knowledge":    "knowledge",
	"concern":      "concern",
	"repo summary": "repo",
	"repo":         "repo",
	"bucket":       "bucket",
	"index":        "index",
	"domain":       "domain",
}

// resolveNodeType classes a .md page by frontmatter `type`, then by path
// convention, defaulting to "page".
func resolveNodeType(relPath string, fileContent []byte) string {
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

	slashed := filepath.ToSlash(relPath)
	switch {
	case strings.Contains(slashed, "/repos/") || strings.HasPrefix(slashed, "repos/"):
		return "repo"
	case strings.Contains(slashed, "/concerns/") || strings.HasPrefix(slashed, "concerns/"):
		return "concern"
	case strings.Contains(slashed, "/knowledge/") || strings.HasPrefix(slashed, "knowledge/"):
		return "knowledge"
	}

	return "page"
}

// extractNodeMeta derives a page's title, description, and snippet from its
// raw content. The snippet is the body's first line that is prose rather than
// markdown structure.
func extractNodeMeta(relPath string, fileContent []byte) NodeMeta {
	var title, description, snippet string

	meta, body, err := frontmatter.Parse(string(fileContent))
	if err != nil || meta == nil {
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

	if title == "" {
		base := filepath.Base(relPath)
		base = strings.TrimSuffix(base, ".md")
		base = strings.ReplaceAll(base, "-", " ")
		base = strings.ReplaceAll(base, "_", " ")
		if len(base) > 0 {
			title = strings.ToUpper(base[:1]) + base[1:]
		}
	}

	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if isSetextUnderline(trimmed) {
			continue
		}
		if isListItem(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "|") {
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		if len(trimmed) > 120 {
			trimmed = trimmed[:120]
		}
		snippet = trimmed
		break
	}

	return NodeMeta{Title: title, Description: description, Snippet: snippet}
}

// isSetextUnderline reports whether s is a run of at least two '=' or '-'.
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

// isListItem reports whether s opens an ordered or unordered list item.
func isListItem(s string) bool {
	if strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") || strings.HasPrefix(s, "+ ") {
		return true
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i > 0 && i < len(s) && s[i] == '.' && i+1 < len(s) && s[i+1] == ' '
}

// BuildLinkGraph walks every *.md file under root and returns the resolved graph.
func BuildLinkGraph(root string) *Graph {
	g := &Graph{
		nodeSet:   make(map[string]bool),
		outbound:  make(map[string][]Edge),
		inbound:   make(map[string][]string),
		nodeTypes: make(map[string]string),
		nodeMeta:  make(map[string]NodeMeta),
	}

	// sourceFiles lets resolveMarkdownLink mark links to non-.md files as
	// CodeFile rather than broken.
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

	// basenameIndex maps a lowercased page basename to its candidate paths.
	basenameIndex := make(map[string][]string, len(mdFiles))
	for _, rel := range mdFiles {
		base := filepath.ToSlash(strings.TrimSuffix(filepath.Base(rel), ".md"))
		key := strings.ToLower(base)
		basenameIndex[key] = append(basenameIndex[key], rel)
	}
	// Sort each bucket so ambiguity resolves deterministically.
	for k := range basenameIndex {
		sort.Strings(basenameIndex[k])
	}

	for _, relPage := range mdFiles {
		absPath := filepath.Join(root, filepath.FromSlash(relPage))
		data, err := os.ReadFile(absPath) //nolint:gosec // relPage is realm-relative and validated during walk
		if err != nil {
			continue
		}

		g.nodeTypes[relPage] = resolveNodeType(relPage, data)
		g.nodeMeta[relPage] = extractNodeMeta(relPage, data)

		links := mdlink.ExtractLinks(string(data))
		pageDir := filepath.Dir(filepath.FromSlash(relPage))

		for _, l := range links {
			edge := resolveLink(l, relPage, pageDir, root, basenameIndex, mdFiles, sourceFiles)
			g.outbound[relPage] = append(g.outbound[relPage], edge)
			if !edge.Broken && edge.ResolvedPath != "" {
				g.inbound[edge.ResolvedPath] = append(g.inbound[edge.ResolvedPath], relPage)
			}
		}
	}

	// A page may link to the same target more than once.
	for k, v := range g.inbound {
		g.inbound[k] = dedupeStrings(v)
	}

	return g
}

// resolveLink resolves one extracted link. pageDir is the source page's
// directory relative to root ("sub" or ".").
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

	best := nearestCandidate(candidates, pageDir)
	edge.ResolvedPath = best
	edge.Ambiguous = true
	return edge
}

type scoredCandidate struct {
	path  string
	depth int
}

// nearestCandidate picks the candidate whose depth is closest to pageDir's,
// breaking ties alphabetically.
func nearestCandidate(candidates []string, pageDir string) string {
	items := make([]scoredCandidate, len(candidates))
	for i, c := range candidates {
		items[i] = scoredCandidate{path: c, depth: strings.Count(c, "/")}
	}

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

// resolveRootRelative probes an already-root-relative path against the page
// index, the source-file set, and finally the filesystem. Shared by the link
// graph and the render path so the two cannot disagree on where a link lands.
//
// combined must be clean and have no leading slash. ok=false means the caller
// should treat the link as broken.
func resolveRootRelative(
	combined string,
	root string,
	allPages []string,
	sourceFiles map[string]bool,
) (resolvedPath string, codeFile bool, isDir bool, ok bool) {
	for _, p := range allPages {
		if p == combined {
			return combined, false, false, true
		}
	}
	// Docs-site link styles routinely omit the .md extension.
	if filepath.Ext(combined) == "" {
		withExt := combined + ".md"
		for _, p := range allPages {
			if p == withExt {
				return withExt, false, false, true
			}
		}
	}
	if sourceFiles[combined] {
		return combined, true, false, true
	}
	// The walk skips directories that safeResolve can still serve.
	if absPath, canServe := safeResolve(root, combined); canServe {
		if info, statErr := os.Stat(absPath); statErr == nil {
			if info.IsDir() {
				if idx, found := resolveDirIndex(root, combined); found {
					return idx, !strings.HasSuffix(idx, ".md"), false, true
				}
				// A directory with no index still resolves — the page handler
				// serves it as a listing.
				return combined, false, true, true
			}
			if strings.HasSuffix(combined, ".md") {
				return combined, false, false, true
			}
			return combined, true, false, true
		}
	}
	return "", false, false, false
}

// resolveMarkdownLink resolves a markdown link target to a file in the realm.
// External URLs and bare anchors keep no ResolvedPath; relative paths resolve
// from pageDir; a leading slash means root-relative (OKF §5.1).
func resolveMarkdownLink(
	edge Edge,
	target string,
	pageDir string,
	root string,
	allPages []string,
	sourceFiles map[string]bool,
) Edge {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
		strings.HasPrefix(target, "mailto:") {
		edge.External = true
		return edge
	}

	cleanTarget := target
	if idx := strings.IndexByte(target, '#'); idx != -1 {
		cleanTarget = target[:idx]
	}
	if cleanTarget == "" {
		return edge
	}

	// Root-relative link. resolvePageHref carries the mirror of this branch;
	// both must land the same way or the rail and the rendered href disagree.
	if filepath.IsAbs(cleanTarget) {
		rootRel := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(cleanTarget, "/")))
		if rootRel == ".." || strings.HasPrefix(rootRel, "../") {
			edge.Broken = true
			return edge
		}
		resolved, codeFile, isDir, ok := resolveRootRelative(rootRel, root, allPages, sourceFiles)
		if !ok {
			// The author's site root may be the docs directory rather than the
			// repo root a repo-scoped serve starts from.
			if docRel, dok := repairDocRootRelative(rootRel, root, allPages, sourceFiles); dok {
				edge.ResolvedPath = docRel
				return edge
			}
			edge.Broken = true
			return edge
		}
		edge.ResolvedPath = resolved
		edge.CodeFile = codeFile
		edge.Dir = isDir
		return edge
	}

	var combined string
	if pageDir == "." || pageDir == "" {
		combined = cleanTarget
	} else {
		combined = filepath.Join(pageDir, cleanTarget)
	}
	combined = filepath.ToSlash(filepath.Clean(combined))

	if strings.HasPrefix(combined, "..") {
		if repaired, ok := repairOverClimbedRelative(root, pageDir, cleanTarget); ok {
			if resolved, codeFile, isDir, rok := resolveRootRelative(repaired, root, allPages, sourceFiles); rok {
				edge.ResolvedPath = resolved
				edge.CodeFile = codeFile
				edge.Dir = isDir
				return edge
			}
		}
		edge.Broken = true
		return edge
	}

	resolved, codeFile, isDir, ok := resolveRootRelative(combined, root, allPages, sourceFiles)
	if !ok {
		if repaired, rok := repairOverClimbedRelative(root, pageDir, cleanTarget); rok {
			if rresolved, rcodeFile, risDir, rrok := resolveRootRelative(repaired, root, allPages, sourceFiles); rrok {
				edge.ResolvedPath = rresolved
				edge.CodeFile = rcodeFile
				edge.Dir = risDir
				return edge
			}
		}
		edge.Broken = true
		return edge
	}
	edge.ResolvedPath = resolved
	edge.CodeFile = codeFile
	edge.Dir = isDir
	return edge
}

// docRootCandidates are the directory names a documentation site is normally
// rooted at, one level below a repo-scoped serve's root.
var docRootCandidates = []string{"docs", "doc", "documentation"}

// repairDocRootRelative retries a failed root-relative path under each existing
// documentation root, so "/reference/concepts" — correct on the published site
// — still resolves when the same file is served from the repository root.
// Reached only after the path already failed against the real root, so a link
// that resolves normally is never redirected.
func repairDocRootRelative(
	rootRel string,
	root string,
	allPages []string,
	sourceFiles map[string]bool,
) (string, bool) {
	for _, candidate := range docRootCandidates {
		if strings.HasPrefix(rootRel, candidate+"/") {
			continue // already rooted there; the direct attempt covered it
		}
		if info, err := os.Stat(filepath.Join(root, candidate)); err != nil || !info.IsDir() {
			continue
		}
		probe := candidate + "/" + rootRel
		if resolved, _, _, ok := resolveRootRelative(probe, root, allPages, sourceFiles); ok {
			return resolved, true
		}
	}
	return "", false
}

// statDocRootRelative is repairDocRootRelative's filesystem-only twin, for the
// render path — which has no page index to consult, only the disk.
func statDocRootRelative(root, combined string) (string, bool) {
	for _, candidate := range docRootCandidates {
		if strings.HasPrefix(combined, candidate+"/") {
			continue
		}
		base := filepath.Join(root, candidate)
		if info, err := os.Stat(base); err != nil || !info.IsDir() {
			continue
		}
		for _, probe := range []string{candidate + "/" + combined, candidate + "/" + combined + ".md"} {
			abs, ok := safeResolve(root, probe)
			if !ok {
				continue
			}
			if _, err := os.Stat(abs); err == nil {
				return probe, true
			}
		}
	}
	return "", false
}

// repairOverClimbedRelative handles relative links whose ../ run climbs past
// their true target — an authoring slip in docs written against a different
// nesting depth. It strips the leading ../ run and probes the remainder
// against each ancestor of pageDir, deepest first.
func repairOverClimbedRelative(root, pageDir, cleanTarget string) (string, bool) {
	remainder := cleanTarget
	for strings.HasPrefix(remainder, "../") {
		remainder = remainder[3:]
	}
	if remainder == cleanTarget || remainder == "" || remainder == ".." {
		return "", false
	}
	dir := pageDir
	for {
		candidate := remainder
		if dir != "." && dir != "" {
			candidate = filepath.ToSlash(filepath.Join(dir, remainder))
		}
		if abs, ok := safeResolve(root, candidate); ok {
			if _, err := os.Stat(abs); err == nil {
				return candidate, true
			}
		}
		if dir == "." || dir == "" {
			break
		}
		parent := filepath.ToSlash(filepath.Dir(dir))
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// resolvePageHref rewrites a raw in-page link destination into a server route.
// It is the render-time counterpart to resolveMarkdownLink and must classify
// targets identically, or the rendered href and the rail's edge disagree.
//
// An unresolved target still inside the realm is routed through /page/ so a
// dead link stays in the shell and gets the in-app 404 rather than a full-page
// navigation to a broken URL.
func resolvePageHref(root, pageRelPath, raw string) (href string, htmxPage, external bool) {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") ||
		strings.HasPrefix(raw, "mailto:") {
		return raw, false, true
	}
	if raw == "" || strings.HasPrefix(raw, "#") {
		return raw, false, false
	}

	// Keep the anchor so it survives onto the rewritten URL.
	target, anchor := raw, ""
	if i := strings.IndexByte(raw, '#'); i != -1 {
		target, anchor = raw[:i], raw[i:]
	}
	if target == "" {
		return raw, false, false
	}
	// A leading slash is relative to the served root, not the filesystem; a
	// path that still escapes root after stripping is left verbatim.
	if filepath.IsAbs(target) {
		rootRel := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(target, "/")))
		if rootRel == ".." || strings.HasPrefix(rootRel, "../") {
			return raw, false, false
		}
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
		if repaired, ok := statDocRootRelative(root, combined); ok {
			return "/page/" + repaired + anchor, true, false
		}
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
		if repaired, ok := repairOverClimbedRelative(root, pageDir, target); ok {
			if strings.HasSuffix(repaired, ".md") {
				return "/page/" + repaired + anchor, true, false
			}
			if abs, aok := safeResolve(root, repaired); aok {
				if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
					return "/page/" + repaired + "/" + anchor, true, false
				}
			}
			return "/file/" + repaired + anchor, false, false
		}
		return raw, false, false
	}

	// Classify against the filesystem, which is the servable surface.
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

	// An over-climb landing exactly at the root cleans to a plain miss, so it
	// only reaches the ancestor probe here.
	if repaired, ok := repairOverClimbedRelative(root, pageDir, target); ok {
		if strings.HasSuffix(repaired, ".md") {
			return "/page/" + repaired + anchor, true, false
		}
		return "/file/" + repaired + anchor, false, false
	}

	// Route by extension so the user stays in the shell.
	if ext := filepath.Ext(combined); ext != "" && ext != ".md" {
		return "/file/" + combined + anchor, false, false
	}
	return "/page/" + combined + anchor, true, false
}

// resolveDirIndex finds a directory's index page. Probe order favors a human
// entry point over the project signals page.
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
