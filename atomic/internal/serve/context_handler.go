// context_handler.go — shared page resolution helpers for the /api/page
// (api_handlers.go) and /api/rail (rail_handler.go) JSON endpoints.
//
// Both endpoints resolve a relpath against the realm root the same way
// (index-file resolution, directory listing, path-traversal guard) so link
// resolution stays single-sourced between the page body and the rail.
package serve

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// normRelPath converts a URL path segment to the forward-slash form stored in
// the graph (filepath.ToSlash + filepath.Clean). It strips a leading slash and
// cleans the path so that segments like "." and ".." are resolved before the
// graph lookup, preventing spurious 404s on requests like /context/./b.md.
func normRelPath(p string) string {
	return filepath.ToSlash(filepath.Clean(strings.TrimPrefix(p, "/")))
}

// readFile reads the file at absPath.
func readFile(absPath string) ([]byte, error) {
	return os.ReadFile(absPath) //nolint:gosec // caller must validate path before calling
}

// baseName returns the base filename from a path string.
func baseName(p string) string {
	return filepath.Base(p)
}

// dirEntry is one entry (file or subfolder) in a directory listing, used by
// the /api/page JSON handler (api_handlers.go).
type dirEntry struct {
	Name    string `json:"name"`    // display name (subfolder name, or file name with .md stripped)
	RelPath string `json:"relpath"` // realm-root-relative target: "<dir>/<name>/" for a folder, "<dir>/<file>" for a file
	Folder  bool   `json:"folder"`

	// Filename is the entry as it exists on disk, extension included. Name
	// drops the extension for display; a listing that shows only that cannot
	// be told apart from a folder listing.
	Filename string `json:"filename,omitempty"`

	// Title and Summary describe what the entry contains — frontmatter first,
	// then the document's own heading and opening prose. A directory takes
	// them from its index file. Without these a listing is a column of
	// slugs that says nothing about any of them.
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`

	// Index is the index file a folder resolves to, empty when it has none.
	// A folder that opens a page and a folder that opens another listing
	// behave differently and should not look identical.
	Index string `json:"index,omitempty"`
}

// summaryCap is the length a listing summary is trimmed to — long enough to
// say what a document is, short enough that a listing stays a listing.
const summaryCap = 150

// describeEntry fills a listing entry's title and summary from the file's own
// content, reusing the same extraction the graph's hover cards use so a
// document is described identically wherever it appears.
func describeEntry(root, fileRel string) (title, summary string) {
	abs, ok := safeResolve(root, fileRel)
	if !ok {
		return "", ""
	}
	data, err := readFile(abs)
	if err != nil {
		return "", ""
	}

	meta := extractNodeMeta(fileRel, data)
	summary = meta.Description
	if summary == "" {
		summary = meta.Snippet
	}

	// extractNodeMeta falls back to a humanized filename, which in a listing
	// only restates the name already shown. The document's own first heading
	// is the better answer, so it is preferred over that fallback — but never
	// over an explicit frontmatter title.
	title = meta.Title
	if !hasFrontmatterTitle(data) {
		if heading := firstHeading(data); heading != "" {
			title = heading
		}
	}
	return title, truncateRunes(summary, summaryCap)
}

// hasFrontmatterTitle reports whether the file declares its own title.
func hasFrontmatterTitle(data []byte) bool {
	meta, _, err := frontmatter.Parse(string(data))
	if err != nil || meta == nil {
		return false
	}
	raw, ok := meta["title"]
	if !ok {
		return false
	}
	s, ok := raw.(string)
	return ok && strings.TrimSpace(s) != ""
}

// firstHeading returns the text of the file's first ATX heading, ignoring the
// frontmatter block and fenced code (where a "#" is a comment, not a heading).
func firstHeading(data []byte) string {
	_, body, err := frontmatter.Parse(string(data))
	if err != nil {
		body = string(data)
	}

	inFence := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(line, "#") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if heading != "" {
			return heading
		}
	}
	return ""
}

// truncateRunes trims to at most limit runes, cutting at the last word break
// so the summary does not end mid-word.
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	cut := string(runes[:limit])
	if space := strings.LastIndexByte(cut, ' '); space > limit/2 {
		cut = cut[:space]
	}
	return strings.TrimRight(cut, " ,;:—-") + "…"
}

// listDirEntries reads the immediate markdown files and subfolders of
// dirRel (realm-root-relative), sorted, hidden files and skip-dirs omitted.
// ok is false when dirRel cannot be resolved or read.
func listDirEntries(root, dirRel string) (entries []dirEntry, ok bool) {
	abs, resolveOK := safeResolve(root, dirRel)
	if !resolveOK {
		return nil, false
	}
	rawEntries, err := os.ReadDir(abs)
	if err != nil {
		return nil, false
	}

	var dirs, files []string
	for _, e := range rawEntries {
		name := e.Name()
		if e.IsDir() {
			if shouldSkipDir(name) {
				continue
			}
			dirs = append(dirs, name)
		} else if strings.HasSuffix(name, ".md") && !strings.HasPrefix(name, ".") {
			files = append(files, name)
		}
	}
	sort.Strings(dirs)
	sort.Strings(files)

	for _, d := range dirs {
		entry := dirEntry{
			Name:    d,
			RelPath: filepath.ToSlash(filepath.Join(dirRel, d)) + "/",
			Folder:  true,
		}
		// A folder with an index file opens a document, not another listing —
		// describe it by that document.
		if index, found := resolveDirIndex(root, filepath.ToSlash(filepath.Join(dirRel, d))); found {
			entry.Index = index
			entry.Title, entry.Summary = describeEntry(root, index)
		}
		entries = append(entries, entry)
	}
	for _, f := range files {
		fileRel := filepath.ToSlash(filepath.Join(dirRel, f))
		title, summary := describeEntry(root, fileRel)
		entries = append(entries, dirEntry{
			Name:     stripMDExt(f),
			Filename: f,
			RelPath:  fileRel,
			Folder:   false,
			Title:    title,
			Summary:  summary,
		})
	}
	return entries, true
}

// breadcrumbSeg is one segment of a page's breadcrumb, used by the /api/page
// JSON handler (api_handlers.go) — the client renders these into the
// breadcrumb bar.
type breadcrumbSeg struct {
	Label  string `json:"label"`
	Path   string `json:"path,omitempty"` // cumulative ancestor prefix; omitted for the final (current-page) segment
	Folder bool   `json:"folder,omitempty"`
}

// breadcrumbSegmentsData builds the ancestor-then-current segment sequence
// for a page's breadcrumb, as structured data for JSON responses.
func breadcrumbSegmentsData(relPath string) []breadcrumbSeg {
	clean := filepath.ToSlash(strings.TrimPrefix(relPath, "/"))
	parts := strings.Split(clean, "/")
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return []breadcrumbSeg{}
	}

	segs := make([]breadcrumbSeg, 0, len(parts))
	prefix := ""
	for i, p := range parts[:len(parts)-1] {
		if i > 0 {
			prefix += "/"
		}
		prefix += p
		segs = append(segs, breadcrumbSeg{Label: p, Path: prefix, Folder: true})
	}
	segs = append(segs, breadcrumbSeg{Label: parts[len(parts)-1]})
	return segs
}

// isNilGraphProvider reports whether g represents "no graph": a bare nil
// interface, or a typed-nil value (nil *Graph, nil *snapshotStore, or any
// other nilable-kind implementor) boxed into the graphProvider interface. A
// boxed typed-nil value carries a non-nil type descriptor, so a plain
// `g == nil` comparison is always false for it — the interface only equals
// nil when both its type and value are unset — even though the underlying
// value has nothing to read. The Kind switch covers every nilable reflect
// kind (Ptr, Map, Slice, Chan, Func, Interface) so any typed-nil provider
// degrades, not just pointer implementors; IsNil panics on a non-nilable
// kind, which is why the switch guards it.
func isNilGraphProvider(g graphProvider) bool {
	if g == nil {
		return true
	}
	v := reflect.ValueOf(g)
	switch v.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}
