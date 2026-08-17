// Page-resolution helpers shared by /api/page and /api/rail, so the page body
// and the rail cannot resolve the same relpath differently.
package serve

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// normRelPath resolves "." and ".." segments before the graph lookup, so a
// request like /page/./b.md is not a spurious 404.
func normRelPath(p string) string {
	return filepath.ToSlash(filepath.Clean(strings.TrimPrefix(p, "/")))
}

func readFile(absPath string) ([]byte, error) {
	return os.ReadFile(absPath) //nolint:gosec // caller must validate path before calling
}

func baseName(p string) string {
	return filepath.Base(p)
}

// dirEntry is one file or subfolder in a directory listing.
type dirEntry struct {
	Name    string `json:"name"`
	RelPath string `json:"relpath"`
	Folder  bool   `json:"folder"`

	// Filename keeps the extension Name drops, so a file listing cannot be
	// mistaken for a folder listing.
	Filename string `json:"filename,omitempty"`

	// Title and Summary come from frontmatter, then the document's own heading
	// and opening prose; a directory takes them from its index file. Without
	// them a listing is a column of slugs.
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`

	// Index is the file a folder resolves to, empty when it has none — a
	// folder that opens a page should not look like one that opens a listing.
	Index string `json:"index,omitempty"`
}

// summaryCap keeps a listing a listing.
const summaryCap = 150

// describeEntry reuses the graph hover-card extraction, so a document is
// described identically wherever it appears.
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
	// only restates the name already shown. The first heading beats that
	// fallback, but never an explicit frontmatter title.
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

// firstHeading skips fenced code, where a leading "#" is a comment rather than
// a heading.
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

// truncateRunes cuts at the last word break so a summary never ends mid-word.
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

// listDirEntries returns dirRel's immediate markdown files and subfolders,
// sorted, omitting hidden files and skip-dirs.
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
		// A folder with an index opens that document, so describe it by that.
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

// breadcrumbSeg is one breadcrumb segment. Path is the cumulative ancestor
// prefix, omitted on the final (current-page) segment.
type breadcrumbSeg struct {
	Label  string `json:"label"`
	Path   string `json:"path,omitempty"`
	Folder bool   `json:"folder,omitempty"`
}

// breadcrumbSegmentsData builds the ancestor-then-current segment sequence.
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

// isNilGraphProvider catches a typed-nil boxed into the interface, which
// `g == nil` cannot: the interface equals nil only when both its type and
// value are unset, so a nil *Graph inside it compares non-nil while having
// nothing to read. The Kind switch guards IsNil, which panics on a
// non-nilable kind.
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
